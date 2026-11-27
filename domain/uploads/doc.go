// Package uploads is Mikoshi's store of resumable uploads in progress:
// go-web-sdk's upload.Store over PostgreSQL, so every replica serving one
// upload shares it. An upload's metadata, offset, length, hash state, and
// expiry are one row; its chunks are rows of their own, keyed by the
// offset each starts at, and live only until the upload is deleted or
// reclaimed.
//
// An append commits by a conditional update of the upload's offset from
// the offset the caller expects, in one transaction with the chunk's
// insert: of two appends racing at one offset, whichever replicas run
// them, exactly one commits, and the other writes nothing and returns the
// winner's offset. The store takes no lock or lease of its own. An upload
// past its expiry, by the database's clock, is missing to every
// operation; a create reclaims an expired upload under its id, and the
// expired uploads' rows stay until they are reclaimed, by such a create
// or by the [Reclaimer] go-database's sweeper runs, which deletes them,
// earliest expiry first, in budgeted batches.
//
// The package's API:
//
//   - [Service], built by [New]: the upload.Store, each method returning
//     an error that matches fs.ErrNotExist for a missing upload.
//   - [Reclaimer], from [Service.Reclaimer]: the expired uploads'
//     go-database sweep.Reclaimer.
package uploads
