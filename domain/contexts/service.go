package contexts

import (
	"context"
	"fmt"
	"uuid"

	"github.com/standards-lab/go-web-sdk"

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

// Children returns one page of the direct children of the context with
// the given id, ordered by name, and the read's paging, honoring the
// parsed query's page or cursor and size. The read takes no sort or
// filter, and a request naming one is ErrValidation, as is a malformed id.
// An unknown context is sql.ErrNoRows; a query context, a leaf, has an
// empty page. A cursor that did not come from this read unwraps to
// query.ErrDirectives.
func (s *Service) Children(ctx context.Context, id string, q web.Query) ([]Context, web.Paging, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, web.Paging{}, fmt.Errorf("%w: id must be a UUID", ErrValidation)
	}
	if len(q.Sort) > 0 || len(q.Filters) > 0 {
		return nil, web.Paging{}, fmt.Errorf("%w: children are read by name and take no sort or filter", ErrValidation)
	}
	return s.store.listChildren(ctx, id, q)
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

// Rename sets the name of the context with the given id, its parent
// unchanged, and returns it as the read presents it. A malformed id or an
// empty name is ErrValidation, the root ErrRootFixed, an unknown context
// sql.ErrNoRows, and a name a sibling holds ErrNameTaken. Its one
// statement changes no context's parent, and the sibling-name constraint
// holds against every concurrent change, so it takes no tree lock.
func (s *Service) Rename(ctx context.Context, id string, r RenameContext) (Context, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return Context{}, fmt.Errorf("%w: id must be a UUID", ErrValidation)
	}
	if err := r.Validate(); err != nil {
		return Context{}, err
	}
	if parsed.String() == Root {
		return Context{}, ErrRootFixed
	}
	return s.store.setName(ctx, id, r.Name)
}
