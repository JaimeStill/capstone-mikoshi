package contexts

import "errors"

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

	// ErrRootFixed reports a rename or a move of the root, whose name and
	// place are fixed, a 409.
	ErrRootFixed = errors.New("the root context is fixed; it is not renamed or moved")

	// ErrCycle reports a move under the context itself or one of its
	// descendants, a 409.
	ErrCycle = errors.New("a context is not moved into its own subtree")
)
