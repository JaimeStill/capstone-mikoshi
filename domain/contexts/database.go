package contexts

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"uuid"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

//go:embed statements/*.sql
var files embed.FS

type store struct {
	db       *data.Database
	findRows query.Rows[Context]
	children query.Projection[Context]
	create   query.Returning[Context]
	rename   query.Returning[Context]
}

func newStore(db *data.Database) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	scan := query.Scanner[Context]()
	return &store{
		db:       db,
		findRows: stmts.Statement("find").Scan(scan),
		children: stmts.Statement("children").Project(scan),
		create:   stmts.Statement("create").Returning(scan),
		rename:   stmts.Statement("rename").Returning(scan),
	}
}

func (s *store) find(ctx context.Context, sess sqlate.Session, id string) (Context, error) {
	return s.findRows.One(ctx, sess, query.Args{"id": id})
}

func (s *store) listChildren(ctx context.Context, parentID string, q web.Query) ([]Context, web.Paging, error) {
	if _, err := s.find(ctx, s.db, parentID); err != nil {
		return nil, web.Paging{}, err
	}
	d := query.Directives{Sort: []query.Sort{{Field: "name"}}}
	base := query.Args{"parent_id": parentID}
	var (
		c   query.Collection[Context]
		err error
	)
	if q.Cursor != "" {
		c, err = s.children.Continue(ctx, s.db, d, query.Cursor(q.Cursor), q.Size, base)
	} else {
		c, err = s.children.List(ctx, s.db, d, query.Page{Number: q.Page, Size: q.Size}, base)
	}
	if err != nil {
		return nil, web.Paging{}, err
	}
	p := web.Paging{More: c.More, Next: string(c.Next)}
	if c.Total != query.NoTotal {
		p.Total = new(c.Total)
	}
	return c.Items, p, nil
}

func (s *store) setName(ctx context.Context, id, name string) (Context, error) {
	renamed, _, err := s.rename.One(ctx, s.db, query.Args{"id": id, "name": name})
	if errors.Is(err, sqlate.ErrUniqueViolation) {
		return Context{}, fmt.Errorf("%w: %q", ErrNameTaken, name)
	}
	return renamed, err
}

func (s *store) insert(ctx context.Context, c CreateContext) (Context, error) {
	args := query.Args{
		"id":        uuid.NewV7().String(),
		"parent_id": c.ParentID,
		"name":      c.Name,
		"kind":      string(c.Kind),
		"filter":    nil,
	}
	if c.Filter != nil {
		raw, err := json.Marshal(c.Filter)
		if err != nil {
			return Context{}, err
		}
		args["filter"] = string(raw)
	}
	return s.db.Transact(ctx, func(tx *sqlate.Tx) (Context, error) {
		if err := s.db.Lock(ctx, tx, data.LockContextTree); err != nil {
			return Context{}, err
		}
		parent, err := s.find(ctx, tx, c.ParentID)
		if errors.Is(err, sql.ErrNoRows) {
			return Context{}, fmt.Errorf("%w: %s", ErrParentNotFound, c.ParentID)
		}
		if err != nil {
			return Context{}, err
		}
		if parent.Kind == KindQuery {
			return Context{}, fmt.Errorf("%w: %s", ErrQueryParent, c.ParentID)
		}
		created, _, err := s.create.One(ctx, tx, args)
		if errors.Is(err, sqlate.ErrUniqueViolation) {
			return Context{}, fmt.Errorf("%w: %q under %s", ErrNameTaken, c.Name, c.ParentID)
		}
		return created, err
	}, nil)
}
