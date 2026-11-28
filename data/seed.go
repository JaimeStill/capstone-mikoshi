package data

import (
	"context"

	"github.com/standards-lab/go-database/admin"
)

// StateEmpty is the one seed state Mikoshi declares: the migrated schema
// with no rows, which seeding writes nothing to reach.
const StateEmpty = "empty"

// Seeder is the admin service's Seeder over Mikoshi's one state,
// [StateEmpty]. Its Verify checks nothing, since the schema node prepares
// every registered statement at startup ([Database.Verify]), and its Seed
// writes nothing, so a reset to StateEmpty leaves the schema it reapplied
// empty.
type Seeder struct{}

// Verify checks nothing.
func (Seeder) Verify(context.Context) error { return nil }

// States lists [StateEmpty] alone.
func (Seeder) States() []string { return []string{StateEmpty} }

// Seed writes nothing and counts no rows.
func (Seeder) Seed(context.Context, string) (admin.Seeded, error) { return admin.Seeded{}, nil }
