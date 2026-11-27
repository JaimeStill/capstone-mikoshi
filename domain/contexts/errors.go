package contexts

import (
	"errors"
	"fmt"
)

var (
	// ErrValidation classifies a refused input: a malformed id, name,
	// kind, or filter. Wrapped with the reason, it reaches the wire as a
	// 400's detail.
	ErrValidation = errors.New("invalid context")

	// ErrParentNotFound reports a create or a move whose parent context
	// does not exist, a 404.
	ErrParentNotFound = errors.New("the parent context does not exist")

	// ErrNameTaken reports a create, a rename, or a move onto the name a
	// sibling under the parent already holds, a 409.
	ErrNameTaken = errors.New("a sibling context already has that name")

	// ErrQueryParent reports a create or a move under a query context,
	// which is a leaf, a 409.
	ErrQueryParent = errors.New("a query context is a leaf; no context is placed under one")

	// ErrRootFixed reports a rename, a move, or a delete of the root, whose
	// name and place are fixed, a 409.
	ErrRootFixed = errors.New("the root context is fixed; it is not renamed, moved, or deleted")

	// ErrCycle reports a move under the context itself or one of its
	// descendants, a 409.
	ErrCycle = errors.New("a context is not moved into its own subtree")

	// ErrUnconfirmed reports a delete whose confirmation does not match
	// the number of the context's descendants, a 409 carrying the count. A
	// [DescendantsError] unwraps to it.
	ErrUnconfirmed = errors.New("the delete's confirmation does not match the context's descendants; repeat it with confirm set to their count")
)

// DescendantsError reports a delete refused because its confirmation does
// not match Count, the number of the context's descendants, the context
// itself excluded, as the delete counted them under the tree lock. It
// unwraps to [ErrUnconfirmed].
type DescendantsError struct {
	Count int64
}

// Error names the count the confirmation must carry.
func (e *DescendantsError) Error() string {
	return fmt.Sprintf("%v: the context has %d descendants", ErrUnconfirmed, e.Count)
}

// Unwrap returns [ErrUnconfirmed].
func (e *DescendantsError) Unwrap() error { return ErrUnconfirmed }
