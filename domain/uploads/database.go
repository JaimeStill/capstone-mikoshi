package uploads

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"io"
	"io/fs"
	"time"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

//go:embed statements/*.sql
var files embed.FS

type metaRow struct {
	MediaType string `json:"media_type"`
	Digest    string `json:"digest"`
	Name      string `json:"name"`
}

type progressRow struct {
	Offset    int64     `json:"upload_offset"`
	Length    *int64    `json:"upload_length"`
	State     []byte    `json:"hash_state"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (p progressRow) length() int64 {
	if p.Length == nil {
		return -1
	}
	return *p.Length
}

type store struct {
	db          *data.Database
	stmts       *query.Statements
	create      query.Statement
	reclaim     query.Statement
	metaRows    query.Rows[metaRow]
	progressRow query.Rows[progressRow]
	setLen      query.Returning[progressRow]
	advance     query.Returning[progressRow]
	appendChunk query.Statement
	chunk       query.Rows[[]byte]
	delete      query.Statement
	expired     query.Statement
}

func newStore(db *data.Database) *store {
	stmts := db.Catalog.MustCompile(files, "statements", db.Dialect())
	scan := query.Scanner[progressRow]()
	s := &store{
		db:          db,
		stmts:       stmts,
		create:      stmts.Statement("create"),
		reclaim:     stmts.Statement("reclaim"),
		metaRows:    stmts.Statement("meta").Scan(query.Scanner[metaRow]()),
		progressRow: stmts.Statement("progress").Scan(scan),
		setLen:      stmts.Statement("set_length").Returning(scan),
		advance:     stmts.Statement("advance").Returning(scan),
		appendChunk: stmts.Statement("append_chunk"),
		chunk:       stmts.Statement("chunk").Scan(query.Scalar[[]byte]),
		delete:      stmts.Statement("delete"),
		expired:     stmts.Statement("reclaim_expired"),
	}
	db.Register("uploads", stmts, s)
	return s
}

// Verify prepares every statement against the live schema over sess,
// making the store a query.Verifier.
func (s *store) Verify(ctx context.Context, sess sqlate.Session) error {
	return query.Verify(ctx, sess, s.stmts)
}

func missing(op, id string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return &fs.PathError{Op: op, Path: id, Err: fs.ErrNotExist}
	}
	return err
}

func (s *store) insert(ctx context.Context, id, mediaType, digest, name string, length int64, expires time.Time) error {
	args := query.Args{
		"id":            id,
		"media_type":    mediaType,
		"digest":        digest,
		"name":          name,
		"upload_length": nil,
		"expires_at":    expires,
	}
	if length >= 0 {
		args["upload_length"] = length
	}
	_, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (int64, error) {
		if _, err := s.reclaim.Exec(ctx, tx, query.Args{"id": id}); err != nil {
			return 0, err
		}
		return s.create.Exec(ctx, tx, args)
	}, nil)
	if errors.Is(err, sqlate.ErrUniqueViolation) {
		return &fs.PathError{Op: "create", Path: id, Err: fs.ErrExist}
	}
	return err
}

func (s *store) meta(ctx context.Context, id string) (metaRow, error) {
	m, err := s.metaRows.One(ctx, s.db, query.Args{"id": id})
	return m, missing("meta", id, err)
}

func (s *store) progress(ctx context.Context, id string) (progressRow, error) {
	p, err := s.progressRow.One(ctx, s.db, query.Args{"id": id})
	return p, missing("progress", id, err)
}

func (s *store) setLength(ctx context.Context, id string, length int64) (int64, error) {
	p, _, err := s.setLen.One(ctx, s.db, query.Args{"id": id, "upload_length": length})
	return p.length(), missing("setlength", id, err)
}

type appended struct {
	offset    int64
	committed bool
}

func (s *store) append(ctx context.Context, id string, offset int64, chunk, state []byte) (int64, bool, error) {
	a, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (appended, error) {
		p, changed, err := s.advance.One(ctx, tx, query.Args{
			"id":         id,
			"expected":   offset,
			"next":       offset + int64(len(chunk)),
			"hash_state": state,
		})
		if err != nil || !changed {
			return appended{offset: p.Offset}, err
		}
		if len(chunk) > 0 {
			args := query.Args{"upload_id": id, "chunk_offset": offset, "content": chunk}
			if _, err := s.appendChunk.Exec(ctx, tx, args); err != nil {
				return appended{}, err
			}
		}
		return appended{offset: p.Offset, committed: true}, nil
	}, nil)
	return a.offset, a.committed, missing("append", id, err)
}

func (s *store) open(ctx context.Context, id string) (io.ReadCloser, error) {
	p, err := s.progressRow.One(ctx, s.db, query.Args{"id": id})
	if err != nil {
		return nil, missing("open", id, err)
	}
	return &reader{ctx: ctx, s: s, id: id, end: p.Offset}, nil
}

func (s *store) remove(ctx context.Context, id string) error {
	n, err := s.delete.Exec(ctx, s.db, query.Args{"id": id})
	if err == nil && n == 0 {
		err = sql.ErrNoRows
	}
	return missing("delete", id, err)
}

func (s *store) reclaimExpired(ctx context.Context, budget int) (int64, error) {
	return s.expired.Exec(ctx, s.db, query.Args{"budget": budget})
}

type reader struct {
	ctx      context.Context
	s        *store
	id       string
	pos, end int64
	buf      []byte
}

func (r *reader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		if r.pos >= r.end {
			return 0, io.EOF
		}
		content, err := r.s.chunk.One(r.ctx, r.s.db, query.Args{"upload_id": r.id, "chunk_offset": r.pos})
		if err != nil {
			return 0, missing("read", r.id, err)
		}
		r.pos += int64(len(content))
		r.buf = content
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *reader) Close() error {
	return nil
}
