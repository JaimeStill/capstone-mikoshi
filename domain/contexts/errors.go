package contexts

import "errors"

var (
	// ErrValidation classifies a refused input: a malformed id, name,
	// kind, or filter. Wrapped with the reason, it reaches the wire as a
	// 400's detail.
	ErrValidation = errors.New("invalid context")

	// ErrParentNotFound reports a create whose parent context does not
	// exist, a 404.
	ErrParentNotFound = errors.New("the parent context does not exist")

	// ErrNameTaken reports a create onto the name a sibling already
	// holds, a 409.
	ErrNameTaken = errors.New("a sibling context already has that name")

	// ErrQueryParent reports a create under a query context, which is a
	// leaf, a 409.
	ErrQueryParent = errors.New("a query context is a leaf; no context is created under one")
)
