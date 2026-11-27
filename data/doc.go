// Package data is Mikoshi's database infrastructure as the composition
// root and the domains see it: the content the admin service administers.
// It sits at the root level, so the domains can import it without importing
// internal/*.
//
// The package's API:
//
//   - [Migrations] and [AppSet]: Mikoshi's migration set, the chunks
//     contract's records under migrations/. The migrations use pgvector's
//     vector type and HNSW index method; the composition root declares
//     the extension to the migrator.
package data
