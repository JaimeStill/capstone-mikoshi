package items

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
	"uuid"

	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/capstone-mikoshi/data"
	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
)

// Service is the items domain service: the layer's public API, the reads,
// the file create, the record create, and the placing and withdrawing of
// an item's reference in a context, every operation delegated to the
// store.
type Service struct {
	store *store
	tree  *contexts.Service
}

// New constructs the service over the database the domains share, the
// object store its blobs are kept in, and the contexts service it resolves
// a reference's context through. Construction compiles and binds the
// statements and performs no I/O.
func New(db *data.Database, objects *storage.Store, tree *contexts.Service) *Service {
	return &Service{store: newStore(db, objects), tree: tree}
}

// FilterFields returns the field contract a query context's filter is
// checked against, which the composition root passes to the contexts
// service: the fields of the item read a filter may name, route, kind, and
// duration, each with the SQL type a value is cast to. It is the list
// read's declared contract less its key, compiled over db's catalog and
// dialect, and performs no I/O.
func FilterFields(db *data.Database) []query.Field {
	return filterFields(db)
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

// Covered returns one page of the items the context with the given id
// covers, in the order of their ids, and the read's paging, honoring the
// parsed query's page or cursor and size: the items referenced in every
// explicit context of its subtree, the context included, and the items
// each query context of the subtree selects from every item, each item
// once. A query context's filter is read over the list read, and the
// matches of every filter are gathered before the page is read. The read
// takes no sort or filter, and a request naming one is ErrValidation, as
// is a malformed id. An unknown context is ErrContextNotFound. A cursor
// that did not come from this read unwraps to query.ErrDirectives.
func (s *Service) Covered(ctx context.Context, contextID string, q web.Query) ([]Item, web.Paging, error) {
	if _, err := uuid.Parse(contextID); err != nil {
		return nil, web.Paging{}, fmt.Errorf("%w: context id must be a UUID", ErrValidation)
	}
	if len(q.Sort) > 0 || len(q.Filters) > 0 {
		return nil, web.Paging{}, fmt.Errorf("%w: a context's items are read in creation order and take no sort or filter", ErrValidation)
	}
	nodes, err := s.tree.Subtree(ctx, contextID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, web.Paging{}, fmt.Errorf("%w: %s", ErrContextNotFound, contextID)
	}
	if err != nil {
		return nil, web.Paging{}, err
	}
	var explicit, matches []string
	for _, n := range nodes {
		if n.Kind != contexts.KindQuery {
			explicit = append(explicit, n.ID)
			continue
		}
		ids, err := s.matching(ctx, n)
		if err != nil {
			return nil, web.Paging{}, err
		}
		matches = append(matches, ids...)
	}
	return s.store.pageCovered(ctx, explicit, matches, q)
}

func (s *Service) matching(ctx context.Context, n contexts.Node) ([]string, error) {
	filters, err := n.Filter.Query()
	if err != nil {
		return nil, fmt.Errorf("items: the stored filter of context %s: %w", n.ID, err)
	}
	ids, err := s.store.matching(ctx, filters)
	if errors.Is(err, query.ErrDirectives) {
		return nil, fmt.Errorf("items: the stored filter of context %s: %v", n.ID, err)
	}
	return ids, err
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

// CreateRecord creates a record item from content, a posted JSON value,
// and returns the item as the read presents it. The content is stored
// byte for byte as an application/json blob, by the protocol and at the
// key a file's content is, and routed by docproc's route package as a
// file's is. A record is idempotent on nothing: two calls create two
// items, over one blob when their content is the same. An empty name or
// content that is not valid JSON is ErrValidation.
func (s *Service) CreateRecord(ctx context.Context, c CreateRecord, content []byte) (Item, error) {
	if err := c.Validate(); err != nil {
		return Item{}, err
	}
	if !json.Valid(content) {
		return Item{}, fmt.Errorf("%w: the record is not valid JSON", ErrValidation)
	}
	sum := sha256.Sum256(content)
	n := newItem{
		Name:      c.Name,
		MediaType: recordMediaType,
		SHA256:    hex.EncodeToString(sum[:]),
		Size:      int64(len(content)),
		itemType:  TypeRecord,
	}
	body := bytes.NewReader(content)
	r, kind, err := sniff(body, n.Size, n.Name, n.MediaType)
	if err != nil {
		return Item{}, err
	}
	n.route, n.kind = r, kind
	return s.store.createOver(ctx, n, body)
}

// Place places the item with the given id in the explicit context with the
// given id, by reference. A reference already in place is left as it is,
// so a repeated call changes nothing. A malformed id is ErrValidation, an
// unknown context ErrContextNotFound, a query context ErrQueryContext, and
// an unknown item ErrItemNotFound. A context deleted between its read and
// the placing is ErrContextNotFound, and a context deleted after the
// placing takes the reference with it.
func (s *Service) Place(ctx context.Context, contextID, itemID string) error {
	if err := s.explicit(ctx, contextID, itemID); err != nil {
		return err
	}
	return s.store.place(ctx, contextID, itemID)
}

// Withdraw removes the reference of the item with the given id from the
// explicit context with the given id. A reference not in place is left
// absent, so a repeated call changes nothing. A malformed id is
// ErrValidation, an unknown context ErrContextNotFound, a query context
// ErrQueryContext, and an unknown item ErrItemNotFound.
func (s *Service) Withdraw(ctx context.Context, contextID, itemID string) error {
	if err := s.explicit(ctx, contextID, itemID); err != nil {
		return err
	}
	return s.store.withdraw(ctx, contextID, itemID)
}

func (s *Service) explicit(ctx context.Context, contextID, itemID string) error {
	if _, err := uuid.Parse(contextID); err != nil {
		return fmt.Errorf("%w: context id must be a UUID", ErrValidation)
	}
	if _, err := uuid.Parse(itemID); err != nil {
		return fmt.Errorf("%w: item id must be a UUID", ErrValidation)
	}
	c, err := s.tree.Find(ctx, contextID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrContextNotFound, contextID)
	}
	if err != nil {
		return err
	}
	if c.Kind == contexts.KindQuery {
		return fmt.Errorf("%w: %s", ErrQueryContext, contextID)
	}
	return nil
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

// PendingReclaimer returns the pending blobs' reclaimer over the service's
// store, which go-database's sweeper runs: it reclaims a blob row left
// pending since more than staleAge ago.
func (s *Service) PendingReclaimer(staleAge time.Duration) *PendingReclaimer {
	return &PendingReclaimer{store: s.store, staleAge: staleAge}
}

// PendingReclaimer is the pending blobs' reclaimer, a go-database
// sweep.Reclaimer, which it satisfies structurally: each pass removes the
// blob rows a create left pending, and their objects, once they are stale.
type PendingReclaimer struct {
	store    *store
	staleAge time.Duration
}

// Pass reclaims at most budget blob rows left pending since more than the
// stale age ago, by the database's clock, that no item names, the stalest
// first, each in a transaction of its own: it locks the row, skipping one
// a create holds, deletes its object at sha256/<hex>, a missing object
// included, while it holds the lock, and then deletes the row. A create
// that touches the row meanwhile waits, then finds no row and starts
// over; one that touched it first has made it fresh, and the pass skips
// it. A failed object delete leaves the row for a later pass and ends
// this one with the error. Pass reports more when it reclaimed budget
// rows, so a further pass may find more.
func (r *PendingReclaimer) Pass(ctx context.Context, budget int) (more bool, err error) {
	n, err := r.store.reclaimPending(ctx, r.staleAge, budget)
	return err == nil && n >= budget, err
}
