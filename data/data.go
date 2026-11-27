package data

import (
	"context"
	"embed"
	"slices"
	"sort"
	"sync"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

//go:embed statements/*.sql
var statementFiles embed.FS

// Database is the database as a domain sees it: the session, and the
// catalog of every pattern namespace the composition root registered. A
// domain compiles its statements against Catalog and runs them through
// DB. It also keeps the statements registry: every domain registers its
// compiled inventory at wiring, so the admin service can walk the whole
// service's SQL the way the catalog lists its patterns. Each
// [Database.Register] call, and [Database.Record] for a store that owns
// its statements, also records the store's verifier for
// [Database.Verify], so no store that runs statements is left out of the
// startup check. The package's own statement, under statements/, is the
// lock; it compiles once here and registers under "data".
type Database struct {
	*sqlate.DB
	Catalog *query.Catalog

	lock query.Statement

	mu        sync.Mutex
	registry  map[string]*query.Statements
	verifiers []query.Verifier
}

// New groups a session with the catalog its statements compile against
// and compiles the package's own statements; a compile failure is a
// wiring defect and panics. No I/O happens here.
func New(db *sqlate.DB, catalog *query.Catalog) *Database {
	d := &Database{DB: db, Catalog: catalog, registry: map[string]*query.Statements{}}
	stmts := catalog.MustCompile(statementFiles, "statements", db.Dialect())
	d.lock = stmts.Statement("lock")
	d.Register("data", stmts, stmts)
	return d
}

// Register records a domain's stmts under name at wiring, together with
// verifier, the store that checks those statements (and any projection
// over them) against the live schema when [Database.Verify] runs.
// Registration stays open: the next Verify also checks a store registered
// after the first. Registering a name twice is a wiring defect and
// panics.
func (d *Database) Register(name string, stmts *query.Statements, verifier query.Verifier) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, dup := d.registry[name]; dup {
		panic("data: statements registered twice under " + name)
	}
	d.registry[name] = stmts
	d.verifiers = append(d.verifiers, verifier)
}

// Record adds v to the verifiers [Database.Verify] runs without a
// registry entry, for a store that owns its statements, such as a
// library's the composition root wires.
func (d *Database) Record(v query.Verifier) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.verifiers = append(d.verifiers, v)
}

// Verify checks every store recorded on the database when it runs against
// the live schema: each verifier passed to [Database.Register], the
// package's own statements among them, and each verifier [Database.Record]
// adds. Every failure is reported, joined, each naming its statement. The
// schema node runs it at startup, once the schema is current; it is the
// service's only statement check.
func (d *Database) Verify(ctx context.Context) error {
	d.mu.Lock()
	vs := slices.Clone(d.verifiers)
	d.mu.Unlock()
	return query.Verify(ctx, d.DB, vs...)
}

// Registry returns the statements registry in name order. Database is the
// admin service's Registry.
func (d *Database) Registry() []admin.Entry {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]admin.Entry, 0, len(d.registry))
	for name, stmts := range d.registry {
		out = append(out, admin.Entry{Name: name, Statements: stmts})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
