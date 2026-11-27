// Package items is the items domain layer: Mikoshi's unit of work that
// contexts hold, served under /api/items as a record create, a read by id,
// a paged read of every item, and a delete by id, and created from a
// completed upload by the Complete function the composition root hands
// go-web-sdk's upload handler. It owns the references that place items in
// explicit contexts, served under /api/contexts/{id}/items/{item}, and the
// paged read of the items a context covers, under /api/contexts/{id}/items,
// and resolves a context through the contexts service, which imports
// nothing from it. An item is a named entry over a blob: content-keyed
// bytes, kept in the object store at sha256/<hex> and shared by every item
// with that digest. An item is a file, from a completed upload, or a
// record, from a posted JSON body stored byte for byte as
// application/json; its route and kind are docproc's route package's
// output for its whole content, and its duration stays null until ingest
// fills it.
//
// A blob row is written pending, then its object, then the row is marked
// available, in the transaction that creates the item over it; content
// whose row is available is not written again, and its item is created in
// the transaction that locks the row, so a reclaimer that locks the row
// to remove the content meets the item. A create that finds the row
// pending, its own or one left behind, writes the object again, and one
// that finds the row gone when it marks it starts over. A row left pending
// past a stale age is removed by the [PendingReclaimer] go-database's
// sweeper runs: it locks the row, skipping one a create holds, deletes the
// object while it holds the lock, then the row, so a create that touches
// the row meanwhile waits and starts over, and one that touched it first
// made it fresh, and the reclaimer passes it by.
//
// Deleting an item deletes its references with it, in one transaction
// that first moves its blob's update time to now; the blob and its object
// stay. Available content that no item has named past a stale age is
// removed by the [OrphanReclaimer], by the same protocol as the pending
// rows: it locks the row, deletes the object while it holds the lock, then
// the content's source occurrences, keyed by the blob's SHA-256 as their
// input, with their sources by cascade, the chunks no remaining source's
// text names, the embeddings no remaining chunk's text names, and the
// row. A create over the content meanwhile waits on the lock and starts
// over, writing the object again, or touched the row first, and the
// reclaimer passes it by.
//
// A file create is idempotent on its upload's id, which the item keeps
// under a unique constraint, so a repeated or overlapping completion
// returns the item the first created. A record create is idempotent on
// nothing.
//
// A context covers the items referenced in every explicit context of its
// subtree and the items each query context of the subtree selects from
// every item. Each query context's filter is read over the list read, its
// matches gathered page by page, and the covered read pages the items
// that either set holds, each once, in creation order. The list read's
// field contract less its key is [FilterFields], which the composition
// root hands the contexts service to check a filter against.
//
// A reference is a row naming one explicit context and one item, placed
// and withdrawn idempotently. Its context's kind never changes, so a
// reference reads the context before it writes and takes no tree lock: a
// context deleted between the two fails the reference's foreign key, and
// one deleted after it takes the reference with it, by the cascade from
// the context.
//
// The package's API:
//
//   - [Service], built by [New]: the reads, the covered read, the file
//     create, the record create, the delete, and the reference's placing
//     and withdrawing.
//   - [PendingReclaimer], from [Service.PendingReclaimer]: the pending
//     blobs' go-database sweep.Reclaimer.
//   - [OrphanReclaimer], from [Service.OrphanReclaimer]: the orphaned
//     blobs' go-database sweep.Reclaimer.
//   - [FilterFields]: the field contract a query context's filter is
//     checked against.
//   - [Routes] and [ReferenceRoutes]: the route groups the composition
//     root mounts.
//   - [Completion]: the upload handler's Complete function.
//   - [Item] and [Type] with [TypeFile] and [TypeRecord]: the read shape
//     and the item types.
//   - [CreateFile] and [CreateRecord]: the create commands' inputs, each
//     validating itself.
//   - [ErrValidation]: a refused input, a 400.
//   - [ErrContextNotFound] and [ErrItemNotFound]: a reference's unknown
//     context or item, a 404; [ErrQueryContext]: a reference to a query
//     context, a 409.
package items
