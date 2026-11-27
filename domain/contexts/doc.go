// Package contexts is the contexts domain layer: the context tree, a
// hierarchy under one root at the nil UUID, each context's name unique
// among its siblings, served under /api/contexts as a create, a read by
// id, a paged read of a context's children by name, a rename, a move, and
// a delete of a context's subtree.
// The service also resolves a context to its subtree, each context with
// its kind and filter, for the domains that build on the tree.
//
// A context is explicit or query, fixed at its creation. A query context
// carries a filter, a non-empty list of conditions in the shape of
// sqlate's query.Filter, checked on create against the field contract the
// service is built with, and is a leaf: no context is placed under one.
// An explicit context carries no filter. Every change to the tree's shape
// takes the tree lock ([data.LockContextTree]) first, in the transaction
// that makes it, so a move's cycle check meets no concurrent move. A move
// carries the context's subtree with it, by adjacency. A delete removes
// the context's whole subtree in one statement once the caller confirms
// the number of its descendants, which a refused delete reports. A rename
// leaves the shape as it is and takes no lock; the root's name and place
// are fixed.
// The descendant and ancestor walks discard a context they have visited,
// so each terminates even on a cycle in the table.
//
// The package's API:
//
//   - [Service], built by [New] over a field contract: the layer's
//     operations, one per endpoint, and the subtree resolution.
//   - [Routes]: the route group the composition root mounts.
//   - [Context], [Kind] with [KindExplicit] and [KindQuery], and [Root]:
//     the read shape, the kinds, and the root's id.
//   - [CreateContext], [RenameContext], and [MoveContext]: the create,
//     rename, and move commands' inputs, each validating itself.
//   - [Node]: one context of a resolved subtree, its id, kind, and filter.
//   - [Filter] and [Condition]: a query context's filter, validating
//     itself, checking itself against a field contract, and mapping onto
//     sqlate's filter declarations.
//   - [ErrValidation]: a refused input, a 400.
//   - [ErrParentNotFound]: a create or move under a missing parent, a 404.
//   - [ErrNameTaken], [ErrQueryParent], [ErrRootFixed], and [ErrCycle]: a
//     create, rename, or move onto a sibling's name, a create or move
//     under a query context, a rename, move, or delete of the root, and a
//     move into the context's own subtree, each a 409.
//   - [ErrUnconfirmed] and [DescendantsError]: a delete whose confirmation
//     is not the number of the context's descendants, a 409 carrying the
//     count.
package contexts
