package data

import (
	"embed"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

//go:embed statements/*.sql
var statementFiles embed.FS

// Database is the database as a domain sees it: the session, and the
// catalog of every pattern namespace the composition root registered. A
// domain compiles its statements against Catalog and runs them through
// DB. The package's own statement, under statements/, is the lock, which
// compiles once here.
type Database struct {
	*sqlate.DB
	Catalog *query.Catalog

	lock query.Statement
}

// New groups a session with the catalog its statements compile against
// and compiles the package's own statements; a compile failure is a
// wiring defect and panics. No I/O happens here.
func New(db *sqlate.DB, catalog *query.Catalog) *Database {
	stmts := catalog.MustCompile(statementFiles, "statements", db.Dialect())
	return &Database{DB: db, Catalog: catalog, lock: stmts.Statement("lock")}
}
