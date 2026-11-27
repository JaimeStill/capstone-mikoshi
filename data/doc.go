// Package data is Mikoshi's database infrastructure as the composition
// root and, later, the domains see it: the content the admin service
// administers. It sits at the root because the domains will import it and
// a root-level package never imports internal/*.
//
// The package's API:
//
//   - [Migrations] and [AppSet]: Mikoshi's migration set, the chunks
//     contract's records under migrations/. The migrations use pgvector's
//     vector type and HNSW index method; the composition root declares
//     the extension to the migrator.
package data
