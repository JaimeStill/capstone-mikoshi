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
	db        *data.Database
	findRows  query.Rows[Context]
	children  query.Projection[Context]
	create    query.Returning[Context]
	rename    query.Returning[Context]
	move      query.Returning[Context]
	subtree   query.Rows[Node]
	inSubtree query.Rows[int64]
	size      query.Rows[int64]
	delete    query.Statement
}

func newStore(db *data.Database) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	scan := query.Scanner[Context]()
	return &store{
		db:        db,
		findRows:  stmts.Statement("find").Scan(scan),
		children:  stmts.Statement("children").Project(scan),
		create:    stmts.Statement("create").Returning(scan),
		rename:    stmts.Statement("rename").Returning(scan),
		move:      stmts.Statement("move").Returning(scan),
		subtree:   stmts.Statement("subtree").Scan(query.Scanner[Node]()),
		inSubtree: stmts.Statement("in_subtree").Scan(query.Scalar[int64]),
		size:      stmts.Statement("subtree_size").Scan(query.Scalar[int64]),
		delete:    stmts.Statement("delete"),
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

func (s *store) resolveSubtree(ctx context.Context, id string) ([]Node, error) {
	nodes, err := s.subtree.All(ctx, s.db, query.Args{"id": id})
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, sql.ErrNoRows
	}
	return nodes, nil
}

func (s *store) relocate(ctx context.Context, id string, m MoveContext) (Context, error) {
	return s.db.Transact(ctx, func(tx *sqlate.Tx) (Context, error) {
		if err := s.db.Lock(ctx, tx, data.LockContextTree); err != nil {
			return Context{}, err
		}
		moved, err := s.find(ctx, tx, id)
		if err != nil {
			return Context{}, err
		}
		parent, err := s.find(ctx, tx, m.ParentID)
		if errors.Is(err, sql.ErrNoRows) {
			return Context{}, fmt.Errorf("%w: %s", ErrParentNotFound, m.ParentID)
		}
		if err != nil {
			return Context{}, err
		}
		if parent.Kind == KindQuery {
			return Context{}, fmt.Errorf("%w: %s", ErrQueryParent, m.ParentID)
		}
		n, err := s.inSubtree.One(ctx, tx, query.Args{"node": id, "candidate": m.ParentID})
		if err != nil {
			return Context{}, err
		}
		if n > 0 {
			return Context{}, fmt.Errorf("%w: %s is in the subtree of %s", ErrCycle, m.ParentID, id)
		}
		name := moved.Name
		if m.Name != nil {
			name = *m.Name
		}
		updated, _, err := s.move.One(ctx, tx, query.Args{"id": id, "parent_id": m.ParentID, "name": name})
		if errors.Is(err, sqlate.ErrUniqueViolation) {
			return Context{}, fmt.Errorf("%w: %q under %s", ErrNameTaken, name, m.ParentID)
		}
		return updated, err
	}, nil)
}

func (s *store) remove(ctx context.Context, id string, confirm int64) error {
	_, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (int64, error) {
		if err := s.db.Lock(ctx, tx, data.LockContextTree); err != nil {
			return 0, err
		}
		n, err := s.size.One(ctx, tx, query.Args{"id": id})
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, sql.ErrNoRows
		}
		if descendants := n - 1; descendants != confirm {
			return 0, &DescendantsError{Count: descendants}
		}
		return s.delete.Exec(ctx, tx, query.Args{"id": id})
	}, nil)
	return err
}
