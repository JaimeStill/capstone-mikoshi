package app

import (
	"log/slog"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/postgres"
)

func defineInfrastructure(g *graph.Graph, n *Nodes) {
	n.LifecycleConfig = g.Define("lifecycle config", n.newLifecycleConfig)
	n.Logger = g.Define("logger", n.newLogger)
	n.Database = g.Define("database", n.newDatabase)
}

func (n *Nodes) newLifecycleConfig(s *graph.Scope) (lifecycle.Config, error) {
	return s.Use(n.Config).Config, nil
}

func (n *Nodes) newLogger(s *graph.Scope) (*slog.Logger, error) {
	return logging.New(s.Use(n.Writer), s.Use(n.Config).Log), nil
}

func (n *Nodes) newDatabase(s *graph.Scope) (*database.DB, error) {
	return postgres.New(s.Use(n.Config).Database)
}
