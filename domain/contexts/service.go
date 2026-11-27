package contexts

import (
	"context"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

// Service is the contexts domain service: the layer's public API, one
// method per endpoint, every operation delegated to the store. A command
// validates its input before it reaches the store.
type Service struct {
	store *store
}

// New constructs the service over the database the domains share.
// Construction compiles and binds the statements and performs no I/O.
func New(db *data.Database) *Service {
	return &Service{store: newStore(db)}
}

// Find returns the context with the given id, the root's included, or
// sql.ErrNoRows.
func (s *Service) Find(ctx context.Context, id string) (Context, error) {
	return s.store.find(ctx, s.store.db, id)
}

// Create creates a context under the stated parent, with an id the
// service mints, and returns it as the read presents it. It takes the
// tree lock before it reads the parent: a missing parent is
// ErrParentNotFound, a query parent ErrQueryParent, and a name a sibling
// holds ErrNameTaken.
func (s *Service) Create(ctx context.Context, c CreateContext) (Context, error) {
	if err := c.Validate(); err != nil {
		return Context{}, err
	}
	return s.store.insert(ctx, c)
}
