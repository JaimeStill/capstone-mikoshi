package items

import (
	"context"
	"database/sql"
	"errors"
	"io"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

const (
	blobPrefix   = "sha256/"
	blobAttempts = 3

	constraintBlobKey    = "pk_blob"
	constraintItemUpload = "uq_item_upload"
)

type blobStatus string

const blobAvailable blobStatus = "available"

type blobRow struct {
	SHA256 string     `json:"sha256"`
	Size   int64      `json:"size"`
	Status blobStatus `json:"status"`
}

func blobKey(digest string) string {
	return blobPrefix + digest
}

type claim struct {
	item  Item
	write bool
}

func (s *store) createOver(ctx context.Context, n newItem, content io.ReaderAt) (Item, error) {
	for range blobAttempts {
		item, err := s.attempt(ctx, n, content)
		if errors.Is(err, errBlobRaced) {
			continue
		}
		return item, err
	}
	return Item{}, errBlobContended
}

func (s *store) attempt(ctx context.Context, n newItem, content io.ReaderAt) (Item, error) {
	args := query.Args{"sha256": n.SHA256, "size": n.Size}
	c, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (claim, error) {
		b, _, err := s.touchBlob.One(ctx, tx, args)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := s.createBlob.Exec(ctx, tx, args); err != nil {
				return claim{}, err
			}
			return claim{write: true}, nil
		case err != nil:
			return claim{}, err
		case b.Status != blobAvailable:
			return claim{write: true}, nil
		}
		item, err := s.insert(ctx, tx, n)
		return claim{item: item}, err
	}, nil)
	if err != nil {
		return s.settle(ctx, n, err)
	}
	if !c.write {
		return c.item, nil
	}

	body := io.NewSectionReader(content, 0, n.Size)
	if _, err := s.objects.Put(ctx, blobKey(n.SHA256), body, storage.PutOptions{Size: n.Size}); err != nil {
		return Item{}, err
	}

	item, err := s.db.Transact(ctx, func(tx *sqlate.Tx) (Item, error) {
		marked, err := s.markBlob.Exec(ctx, tx, args)
		if err != nil {
			return Item{}, err
		}
		if marked == 0 {
			return Item{}, errBlobRaced
		}
		return s.insert(ctx, tx, n)
	}, nil)
	if err != nil {
		return s.settle(ctx, n, err)
	}
	return item, nil
}

func (s *store) settle(ctx context.Context, n newItem, err error) (Item, error) {
	if c, ok := errors.AsType[*sqlate.ConstraintError](err); ok {
		switch c.Constraint {
		case constraintItemUpload:
			return s.byUpload(ctx, n.UploadID)
		case constraintBlobKey:
			return Item{}, errBlobRaced
		}
	}
	return Item{}, err
}
