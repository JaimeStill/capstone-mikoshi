// Package contexts is the contexts domain layer: the context tree, a
// hierarchy under one root at the nil UUID, each context's name unique
// among its siblings, served under /api/contexts as a create and a read by
// id.
//
// A context is explicit or query, fixed at its creation. A query context
// carries a filter, a non-empty list of conditions in the shape of
// sqlate's query.Filter, and is a leaf: no context is created under one.
// An explicit context carries no filter. Every change to the tree's shape
// takes the tree lock ([data.LockContextTree]) first, in the transaction
// that makes it.
//
// The package's API:
//
//   - [Service], built by [New]: the layer's operations, one per endpoint.
//   - [Routes]: the route group the composition root mounts.
//   - [Context], [Kind] with [KindExplicit] and [KindQuery], and [Root]:
//     the read shape, the kinds, and the root's id.
//   - [CreateContext]: the create command's input, validating itself.
//   - [Filter] and [Condition]: a query context's filter, validating
//     itself and mapping onto sqlate's filter declarations.
//   - [ErrValidation]: a refused input, a 400.
//   - [ErrParentNotFound]: a create under a missing parent, a 404.
//   - [ErrNameTaken] and [ErrQueryParent]: a create onto a sibling's name
//     or under a query context, each a 409.
package contexts
