package app

import (
	"fmt"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/postgres/pgvector"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

func defineAdmin(g *graph.Graph, n *Nodes) {
	n.Schema = g.Define("schema", n.newSchema)
}

func (n *Nodes) newSchema(s *graph.Scope) (*admin.Service, error) {
	pool := s.Use(n.Database)
	logger := s.Use(n.Logger)

	db := sqlate.Wrap(pool.Conn(), pgdialect.Dialect{})
	migrator, err := migrate.New(db, []migrate.Set{data.Migrations()}, migrate.Options{
		Logger:     logger,
		Extensions: []migrate.Extension{{Name: pgvector.Extension}},
	})
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return admin.New(pool, db, migrator, query.MustCatalog(query.Patterns()), admin.Options{
		Logger: logger,
	}), nil
}

func mountAdmin(s *graph.Scope, n *Nodes) *web.Group {
	return web.NewGroup("/admin")
}
