package data

import (
	"context"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

// This block is the advisory-lock name registry. An advisory lock is
// database-global, so every name the service takes is declared here, in
// one list, as <domain>.<structure>, and taken through Lock; the engine
// hashes the name. Two domains that need the same serialization share the
// one name.
const (
	// LockContextTree serializes every change to the context tree's
	// shape, so a change that reads the tree before it writes, as a create
	// reads its parent, meets no concurrent change between the two.
	LockContextTree = "context.tree"

	// LockSweepPrefix begins the names go-database's sweeper claims, one
	// per reclaimer, "sweep.<name>" ("sweep.pending_blobs"), so no name
	// declared here may begin with it. The sweeper takes them itself,
	// without Lock: each is a session lock its cycle tries without waiting
	// on a pinned connection, so two replicas never run one reclaimer's
	// cycle at once.
	LockSweepPrefix = "sweep."
)

// Lock takes the named advisory lock for the rest of the transaction s
// runs in, released automatically when s commits or rolls back. A
// transaction that must not interleave with another on the same
// structure calls Lock first, before the work it guards, so a concurrent
// transaction on the same name blocks until the first one ends. Outside
// a transaction the call fails: lock.sql declares "transaction:
// required", and sqlate enforces it. lock.sql is the one native
// statement behind this mechanism, so a provider port touches locking in
// one file.
func (d *Database) Lock(ctx context.Context, s sqlate.Session, name string) error {
	_, err := d.lock.Exec(ctx, s, query.Args{"name": name})
	return err
}
