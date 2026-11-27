// Package data is Mikoshi's database infrastructure as the composition
// root and the domains see it: the session grouped with the pattern
// catalog, and the content the admin service administers. It sits at the
// root level, so the domains can import it without importing internal/*.
//
// The package's API:
//
//   - [Database], built by [New]: the session and the catalog the domains
//     compile their statements against; [Database.Lock] takes an advisory
//     lock named in the lock registry ([LockContextTree]).
//   - The statements registry on [Database]: each domain's store calls
//     [Database.Register] at wiring with its compiled inventory and itself
//     as the verifier, and [Database.Record] adds a verifier with no
//     registry entry. [Database.Verify] prepares every recorded store's
//     statements against the live schema; the composition root's schema
//     node runs it at startup, once the schema is current, and
//     [Database.Registry] is the admin service's statements inventory.
//   - [Migrations] and [AppSet]: Mikoshi's migration set, the chunks
//     contract's records, the context tree, the resumable uploads in
//     progress, and the items and the content-keyed blobs they name, under
//     migrations/. The
//     migrations use pgvector's vector type and HNSW index method; the
//     composition root declares the extension to the migrator.
package data
