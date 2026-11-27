package items

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"slices"
	"uuid"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

//go:embed statements/*.sql
var files embed.FS

type store struct {
	db          *data.Database
	objects     *storage.Store
	stmts       *query.Statements
	findRows    query.Rows[Item]
	uploadRows  query.Rows[Item]
	list        query.Projection[Item]
	covered     query.Projection[Item]
	create      query.Returning[Item]
	touchBlob   query.Returning[blobRow]
	createBlob  query.Statement
	markBlob    query.Statement
	placeRef    query.Statement
	withdrawRef query.Statement
}

func newStore(db *data.Database, objects *storage.Store) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	scan := query.Scanner[Item]()
	s := &store{
		db:          db,
		objects:     objects,
		stmts:       stmts,
		findRows:    stmts.Statement("find").Scan(scan),
		uploadRows:  stmts.Statement("find_upload").Scan(scan),
		list:        stmts.Statement("list").Project(scan),
		covered:     stmts.Statement("covered").Project(scan),
		create:      stmts.Statement("create").Returning(scan),
		touchBlob:   stmts.Statement("touch_blob").Returning(query.Scanner[blobRow]()),
		createBlob:  stmts.Statement("create_blob"),
		markBlob:    stmts.Statement("mark_blob"),
		placeRef:    stmts.Statement("place"),
		withdrawRef: stmts.Statement("withdraw"),
	}
	db.Register("items", stmts, s)
	return s
}

// Verify prepares every statement and the list and covered projections'
// field contracts against the live schema over sess, making the store a
// query.Verifier.
func (s *store) Verify(ctx context.Context, sess sqlate.Session) error {
	return query.Verify(ctx, sess, s.stmts, s.list, s.covered)
}

func filterFields(db *data.Database) []query.Field {
	list := db.Catalog.MustCompile(files, "statements", db.Dialect()).Statement("list")
	return slices.DeleteFunc(list.Fields(), func(f query.Field) bool {
		return slices.Contains(list.Keys(), f.Name)
	})
}

func (s *store) find(ctx context.Context, id string) (Item, error) {
	return s.findRows.One(ctx, s.db, query.Args{"id": id})
}

func (s *store) byUpload(ctx context.Context, uploadID string) (Item, error) {
	return s.uploadRows.One(ctx, s.db, query.Args{"upload_id": uploadID})
}

func (s *store) page(ctx context.Context, q web.Query) ([]Item, web.Paging, error) {
	return read(ctx, s.db, s.list, q)
}

const matchPageSize = 500

func (s *store) matching(ctx context.Context, filters []query.Filter) ([]string, error) {
	d := query.Directives{Filters: filters, Total: query.TotalNone}
	var ids []string
	c, err := s.list.List(ctx, s.db, d, query.Page{Number: 1, Size: matchPageSize})
	for {
		if err != nil {
			return nil, err
		}
		for _, item := range c.Items {
			ids = append(ids, item.ID)
		}
		if c.Next == "" {
			return ids, nil
		}
		c, err = s.list.Continue(ctx, s.db, d, c.Next, matchPageSize)
	}
}

func (s *store) pageCovered(ctx context.Context, contexts, matches []string, q web.Query) ([]Item, web.Paging, error) {
	return read(ctx, s.db, s.covered, q, query.Args{"contexts": contexts, "matches": matches})
}

func read(ctx context.Context, db *data.Database, view query.Projection[Item], q web.Query, base ...query.Args) ([]Item, web.Paging, error) {
	var (
		c   query.Collection[Item]
		err error
	)
	if q.Cursor != "" {
		c, err = view.Continue(ctx, db, query.Directives{}, query.Cursor(q.Cursor), q.Size, base...)
	} else {
		c, err = view.List(ctx, db, query.Directives{}, query.Page{Number: q.Page, Size: q.Size}, base...)
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

type newItem struct {
	CreateFile
	itemType Type
	route    string
	kind     *string
}

func (s *store) insert(ctx context.Context, tx *sqlate.Tx, n newItem) (Item, error) {
	args := query.Args{
		"id":         uuid.NewV7().String(),
		"type":       string(n.itemType),
		"name":       n.Name,
		"media_type": n.MediaType,
		"sha256":     n.SHA256,
		"size":       n.Size,
		"route":      n.route,
		"kind":       nil,
		"upload_id":  nil,
	}
	if n.kind != nil {
		args["kind"] = *n.kind
	}
	if n.UploadID != "" {
		args["upload_id"] = n.UploadID
	}
	created, _, err := s.create.One(ctx, tx, args)
	return created, err
}

const constraintReferenceItem = "fk_context_item_item"

func (s *store) place(ctx context.Context, contextID, itemID string) error {
	_, err := s.placeRef.Exec(ctx, s.db, query.Args{"context_id": contextID, "item_id": itemID})
	if errors.Is(err, sqlate.ErrUniqueViolation) {
		return nil
	}
	if ce, ok := errors.AsType[*sqlate.ConstraintError](err); ok && errors.Is(err, sqlate.ErrForeignKeyViolation) {
		if ce.Constraint == constraintReferenceItem {
			return fmt.Errorf("%w: %s", ErrItemNotFound, itemID)
		}
		return fmt.Errorf("%w: %s", ErrContextNotFound, contextID)
	}
	return err
}

func (s *store) withdraw(ctx context.Context, contextID, itemID string) error {
	n, err := s.withdrawRef.Exec(ctx, s.db, query.Args{"context_id": contextID, "item_id": itemID})
	if err != nil || n > 0 {
		return err
	}
	_, err = s.find(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrItemNotFound, itemID)
	}
	return err
}
