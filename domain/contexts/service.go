package contexts

import (
	"context"
	"fmt"
	"uuid"

	"github.com/standards-lab/go-web-sdk"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

// Service is the contexts domain service: the layer's public API, one
// method per endpoint and one for the subtree resolution, every operation
// delegated to the store. A command validates its input before it reaches
// the store.
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

// Move sets the parent of the context with the given id and, when the
// command names one, its name, and returns it as the read presents it; its
// subtree moves with it. In one transaction it takes the tree lock, reads
// the context and the new parent, and walks the new parent's ancestors
// before it writes. A malformed id or command is ErrValidation, the root
// ErrRootFixed, an unknown context sql.ErrNoRows, a missing parent
// ErrParentNotFound, a query parent ErrQueryParent, a parent that is the
// context or one of its descendants ErrCycle, and a name a sibling under
// the new parent holds ErrNameTaken.
func (s *Service) Move(ctx context.Context, id string, m MoveContext) (Context, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return Context{}, fmt.Errorf("%w: id must be a UUID", ErrValidation)
	}
	if err := m.Validate(); err != nil {
		return Context{}, err
	}
	if parsed.String() == Root {
		return Context{}, ErrRootFixed
	}
	return s.store.relocate(ctx, parsed.String(), m)
}

// Subtree resolves the context with the given id to its subtree, the
// context included, each node with its kind, in no stated order. A
// malformed id is ErrValidation and an unknown context sql.ErrNoRows.
func (s *Service) Subtree(ctx context.Context, id string) ([]Node, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("%w: id must be a UUID", ErrValidation)
	}
	return s.store.resolveSubtree(ctx, id)
}

// Delete removes the context with the given id and its whole subtree when
// confirm is the number of its descendants, the context itself excluded:
// zero, a leaf's count, needs no confirmation from the caller. In one
// transaction it takes the tree lock, counts the descendants, and deletes
// the subtree in one statement. A malformed id or a negative confirm is
// ErrValidation, the root ErrRootFixed, an unknown context sql.ErrNoRows,
// and a confirm that is not the count a *DescendantsError carrying it.
func (s *Service) Delete(ctx context.Context, id string, confirm int64) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("%w: id must be a UUID", ErrValidation)
	}
	if confirm < 0 {
		return fmt.Errorf("%w: confirm must be a non-negative integer", ErrValidation)
	}
	if parsed.String() == Root {
		return ErrRootFixed
	}
	return s.store.remove(ctx, parsed.String(), confirm)
}
