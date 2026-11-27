package items

import "errors"

var (
	// ErrValidation classifies a refused input: a malformed id, a read
	// naming a sort or filter, or a malformed create command. Wrapped with
	// the reason, it reaches the wire as a 400's detail.
	ErrValidation = errors.New("invalid item request")

	errBlobRaced     = errors.New("items: the blob row changed between the transactions that write it")
	errBlobContended = errors.New("items: the blob row changed under every attempt to write it")
)
