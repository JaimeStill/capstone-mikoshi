package items

import "errors"

var (
	// ErrValidation classifies a refused input: a malformed id, a read
	// naming a sort or filter, or a malformed create command. Wrapped with
	// the reason, it reaches the wire as a 400's detail.
	ErrValidation = errors.New("invalid item request")

	// ErrContextNotFound reports a reference whose context does not
	// exist, a 404.
	ErrContextNotFound = errors.New("the context does not exist")

	// ErrItemNotFound reports a reference whose item does not exist, a
	// 404.
	ErrItemNotFound = errors.New("the item does not exist")

	// ErrQueryContext reports a reference to a query context, whose
	// contents its filter selects, so no item is placed in one, a 409.
	ErrQueryContext = errors.New("a query context's contents are what its filter selects; no item is placed in one")

	errBlobRaced     = errors.New("items: the blob row changed between the transactions that write it")
	errBlobContended = errors.New("items: the blob row changed under every attempt to write it")
)
