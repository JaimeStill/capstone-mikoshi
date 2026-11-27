// Package items is the items domain layer: Mikoshi's unit of work that
// contexts hold, served under /api/items as a read by id and a paged read
// of every item, and created from a completed upload by the Complete
// function the composition root hands go-web-sdk's upload handler. An item
// is a named entry over a blob: content-keyed bytes, kept in the object
// store at sha256/<hex> and shared by every item with that digest. An
// item is a file, from a completed upload, or a record; its route and
// kind are docproc's route package's output for its whole content, and
// its duration stays null until ingest fills it.
//
// A blob row is written pending, then its object, then the row is marked
// available, in the transaction that creates the item over it; content
// whose row is available is not written again, and its item is created in
// the transaction that locks the row, so a reclaimer that locks the row
// to remove the content meets the item. A create that finds the row
// pending, its own or one left behind, writes the object again, and one
// that finds the row gone when it marks it starts over. A file create is
// idempotent on its upload's id, which the item keeps under a unique
// constraint, so a repeated or overlapping completion returns the item
// the first created.
//
// The package's API:
//
//   - [Service], built by [New]: the reads and the file create.
//   - [Routes]: the route group the composition root mounts.
//   - [Completion]: the upload handler's Complete function.
//   - [Item] and [Type] with [TypeFile] and [TypeRecord]: the read shape
//     and the item types.
//   - [CreateFile]: the file create command's input, validating itself.
//   - [ErrValidation]: a refused input, a 400.
package items
