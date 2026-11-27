package app

import (
	"fmt"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/reactor"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/migrate"
	"github.com/standards-lab/sqlate/postgres/pgvector"

	dbadmin "github.com/JaimeStill/capstone-mikoshi/admin/database"
	"github.com/JaimeStill/capstone-mikoshi/data"
)

func defineAdmin(g *graph.Graph, n *Nodes) {
	n.Gate = g.Fresh[reactor.Gate]("gate")
	n.Schema = g.Define("schema", n.newSchema)
}

func (n *Nodes) newSchema(s *graph.Scope) (*admin.Service, error) {
	sql := s.Use(n.SQL)
	logger := s.Use(n.Logger)

	migrator, err := migrate.New(sql.DB, []migrate.Set{data.Migrations()}, migrate.Options{
		Logger:     logger,
		Extensions: []migrate.Extension{{Name: pgvector.Extension}},
	})
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return admin.New(s.Use(n.Database), sql.DB, migrator, sql.Catalog, admin.Options{
		Logger: logger,
	}), nil
}

func mountAdmin(s *graph.Scope, n *Nodes) *web.Group {
	g := web.NewGroup("/admin")
	g.Mount(dbadmin.Routes(s.Use(n.Schema), s.Use(n.Gate), s.Use(n.Logger)))
	return g
}
