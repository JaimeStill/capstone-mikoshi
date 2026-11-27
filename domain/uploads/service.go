package uploads

import (
	"context"
	"io"
	"time"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

// Service is the uploads store, go-web-sdk's upload.Store, every operation
// delegated to the database store. A missing upload, one deleted or past
// its expiry, is an *fs.PathError wrapping fs.ErrNotExist.
type Service struct {
	store *store
}

// New constructs the service over the database the domains share.
// Construction compiles and binds the statements and performs no I/O.
func New(db *data.Database) *Service {
	return &Service{store: newStore(db)}
}

// Create records a new upload under id at offset 0 with no hash state,
// its length -1 while unknown. It first reclaims an expired upload under
// id; an unexpired one is an *fs.PathError wrapping fs.ErrExist.
func (s *Service) Create(ctx context.Context, id, mediaType, digest, name string, length int64, expires time.Time) error {
	return s.store.insert(ctx, id, mediaType, digest, name, length, expires)
}

// Meta returns the upload's media type, digest, and file name.
func (s *Service) Meta(ctx context.Context, id string) (mediaType, digest, name string, err error) {
	m, err := s.store.meta(ctx, id)
	return m.MediaType, m.Digest, m.Name, err
}

// Progress returns the upload's offset, its length or -1 while unknown,
// the hash state its last committed append stored (empty at offset 0),
// and its expiry.
func (s *Service) Progress(ctx context.Context, id string) (offset, length int64, state []byte, expires time.Time, err error) {
	p, err := s.store.progress(ctx, id)
	return p.Offset, p.length(), p.State, p.ExpiresAt, err
}

// SetLength records the upload's length when none is recorded and
// returns the length recorded, which is the earlier one when there is.
func (s *Service) SetLength(ctx context.Context, id string, length int64) (int64, error) {
	return s.store.setLength(ctx, id, length)
}

// Append commits chunk at offset only when the upload's offset is offset,
// storing offset+len(chunk) and state in the transaction that stores the
// chunk, and returns the upload's offset after the call and whether it
// committed. A refusal writes nothing and is not an error.
func (s *Service) Append(ctx context.Context, id string, offset int64, chunk, state []byte) (current int64, committed bool, err error) {
	return s.store.append(ctx, id, offset, chunk, state)
}

// Open returns the chunks committed when it is called, in offset order,
// as one reader that reads one chunk from the database at a time. A
// delete of the upload before the reader finishes is fs.ErrNotExist from
// Read.
func (s *Service) Open(ctx context.Context, id string) (io.ReadCloser, error) {
	return s.store.open(ctx, id)
}

// Delete removes the upload and, by cascade, its chunks.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.remove(ctx, id)
}
