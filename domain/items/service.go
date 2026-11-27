package items

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"uuid"

	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

// Service is the items domain service: the layer's public API, the reads
// and the file create, every operation delegated to the store.
type Service struct {
	store *store
}

// New constructs the service over the database the domains share and the
// object store its blobs are kept in. Construction compiles and binds the
// statements and performs no I/O.
func New(db *data.Database, objects *storage.Store) *Service {
	return &Service{store: newStore(db, objects)}
}

// Find returns the item with the given id, or sql.ErrNoRows. A malformed
// id is ErrValidation.
func (s *Service) Find(ctx context.Context, id string) (Item, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Item{}, fmt.Errorf("%w: id must be a UUID", ErrValidation)
	}
	return s.store.find(ctx, id)
}

// List returns one page of every item, in the order of their ids, which
// is the order they were created in, and the read's paging, honoring the
// parsed query's page or cursor and size. The read takes no sort or
// filter, and a request naming one is ErrValidation. A cursor that did
// not come from this read unwraps to query.ErrDirectives.
func (s *Service) List(ctx context.Context, q web.Query) ([]Item, web.Paging, error) {
	if len(q.Sort) > 0 || len(q.Filters) > 0 {
		return nil, web.Paging{}, fmt.Errorf("%w: items are read in creation order and take no sort or filter", ErrValidation)
	}
	return s.store.page(ctx, q)
}

// CreateFile creates the file item for a completed upload from its
// content, which it reads once, to its end, into a temporary file, and
// returns the item as the read presents it. It is idempotent on the
// upload id: when an item for the upload exists, it returns that item and
// reads nothing, and of two calls for one upload that overlap, both
// return the item one of them created.
//
// It routes the whole content with docproc's route package, by its
// signature and c's name and media type, and stores the content as the
// blob its SHA-256 names, at sha256/<hex>, unless that blob is available
// already. The blob row is written pending, then the object, then the row
// is marked available in the transaction that creates the item; when the
// row is available, the item is created in the transaction that locks it.
// Content that is not c.Size bytes or does not match c.SHA256 is an error,
// and a malformed command is ErrValidation.
func (s *Service) CreateFile(ctx context.Context, c CreateFile, content io.Reader) (Item, error) {
	if err := c.Validate(); err != nil {
		return Item{}, err
	}
	existing, err := s.store.byUpload(ctx, c.UploadID)
	if !errors.Is(err, sql.ErrNoRows) {
		return existing, err
	}

	tmp, err := os.CreateTemp("", "mikoshi-item-*")
	if err != nil {
		return Item{}, fmt.Errorf("items: hold the content: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), content)
	if err != nil {
		return Item{}, fmt.Errorf("items: hold the content: %w", err)
	}
	if n != c.Size {
		return Item{}, fmt.Errorf("items: the content is %d bytes, not the %d its upload declared", n, c.Size)
	}
	if sum := hex.EncodeToString(hash.Sum(nil)); sum != c.SHA256 {
		return Item{}, fmt.Errorf("items: the content's SHA-256 is %s, not the %s its upload declared", sum, c.SHA256)
	}

	r, kind, err := sniff(tmp, n, c.Name, c.MediaType)
	if err != nil {
		return Item{}, err
	}
	return s.store.createOver(ctx, newItem{CreateFile: c, itemType: TypeFile, route: r, kind: kind}, tmp)
}

func sniff(content io.ReaderAt, size int64, name, mediaType string) (string, *string, error) {
	head := make([]byte, min(size, route.HeadLen))
	if _, err := content.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		return "", nil, fmt.Errorf("items: read the content's head: %w", err)
	}
	r, _, err := route.At(content, size, head, name, mediaType)
	if err != nil {
		return "", nil, fmt.Errorf("items: route the content: %w", err)
	}
	var kind *string
	if k := route.Identify(head); k != route.Unknown {
		kind = new(string(k))
	}
	return string(r), kind, nil
}
