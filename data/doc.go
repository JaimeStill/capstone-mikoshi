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
//   - [Migrations] and [AppSet]: Mikoshi's migration set, the chunks
//     contract's records and the context tree under migrations/. The
//     migrations use pgvector's vector type and HNSW index method; the
//     composition root declares the extension to the migrator.
package data
