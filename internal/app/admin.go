package app

import (
	"context"
	"fmt"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/reactor"
	"github.com/standards-lab/go-database/admin"
	msgpg "github.com/standards-lab/go-messaging/postgres"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	"github.com/standards-lab/sqlate/postgres/pgvector"

	dbadmin "github.com/JaimeStill/capstone-mikoshi/admin/database"
	"github.com/JaimeStill/capstone-mikoshi/data"
)

func defineAdmin(g *graph.Graph, n *Nodes) {
	n.Gate = g.Fresh[reactor.Gate]("gate")
	n.Schema = g.Define("schema", n.newSchema)
}

// Schema is the schema node's value: go-database's admin service over the
// pool, whose Start also prepares every statement registered on the sql
// node against the schema it has just made current.
type Schema struct {
	*admin.Service
	sql *data.Database
}

// Start brings the schema to its head through the admin service, then
// runs data.Database.Verify, so a statement that does not fit the
// migrated schema fails startup with an error naming it.
func (s *Schema) Start(ctx context.Context) error {
	if err := s.Service.Start(ctx); err != nil {
		return err
	}
	return s.sql.Verify(ctx)
}

func (n *Nodes) newSchema(s *graph.Scope) (*Schema, error) {
	sql := s.Use(n.SQL)
	logger := s.Use(n.Logger)

	messagingSet, err := msgpg.Migrations()
	if err != nil {
		return nil, fmt.Errorf("messaging migrations: %w", err)
	}
	migrator, err := migrate.New(sql.DB, []migrate.Set{messagingSet, data.Migrations()}, migrate.Options{
		Logger:     logger,
		Extensions: []migrate.Extension{{Name: pgvector.Extension}},
	})
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	sql.Record(verifier(msgpg.Verify))

	s.Use(n.Contexts)
	s.Use(n.Uploads)
	s.Use(n.Items)

	return &Schema{
		Service: admin.New(s.Use(n.Database), sql.DB, migrator, sql.Catalog, admin.Options{
			Registry: sql,
			Logger:   logger,
		}),
		sql: sql,
	}, nil
}

type verifier func(ctx context.Context, sess sqlate.Session) error

// Verify runs v against the schema sess reaches.
func (v verifier) Verify(ctx context.Context, sess sqlate.Session) error { return v(ctx, sess) }

func mountAdmin(s *graph.Scope, n *Nodes) *web.Group {
	g := web.NewGroup("/admin")
	g.Mount(dbadmin.Routes(s.Use(n.Schema).Service, s.Use(n.Gate), s.Use(n.Logger)))
	return g
}
