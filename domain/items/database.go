package items

import (
	"context"
	"embed"
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
	db         *data.Database
	objects    *storage.Store
	stmts      *query.Statements
	findRows   query.Rows[Item]
	uploadRows query.Rows[Item]
	list       query.Projection[Item]
	create     query.Returning[Item]
	touchBlob  query.Returning[blobRow]
	createBlob query.Statement
	markBlob   query.Statement
}

func newStore(db *data.Database, objects *storage.Store) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	scan := query.Scanner[Item]()
	s := &store{
		db:         db,
		objects:    objects,
		stmts:      stmts,
		findRows:   stmts.Statement("find").Scan(scan),
		uploadRows: stmts.Statement("find_upload").Scan(scan),
		list:       stmts.Statement("list").Project(scan),
		create:     stmts.Statement("create").Returning(scan),
		touchBlob:  stmts.Statement("touch_blob").Returning(query.Scanner[blobRow]()),
		createBlob: stmts.Statement("create_blob"),
		markBlob:   stmts.Statement("mark_blob"),
	}
	db.Register("items", stmts, s)
	return s
}

// Verify prepares every statement and the list projection's field
// contract against the live schema over sess, making the store a
// query.Verifier.
func (s *store) Verify(ctx context.Context, sess sqlate.Session) error {
	return query.Verify(ctx, sess, s.stmts, s.list)
}

func (s *store) find(ctx context.Context, id string) (Item, error) {
	return s.findRows.One(ctx, s.db, query.Args{"id": id})
}

func (s *store) byUpload(ctx context.Context, uploadID string) (Item, error) {
	return s.uploadRows.One(ctx, s.db, query.Args{"upload_id": uploadID})
}

func (s *store) page(ctx context.Context, q web.Query) ([]Item, web.Paging, error) {
	var (
		c   query.Collection[Item]
		err error
	)
	if q.Cursor != "" {
		c, err = s.list.Continue(ctx, s.db, query.Directives{}, query.Cursor(q.Cursor), q.Size)
	} else {
		c, err = s.list.List(ctx, s.db, query.Directives{}, query.Page{Number: q.Page, Size: q.Size})
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
