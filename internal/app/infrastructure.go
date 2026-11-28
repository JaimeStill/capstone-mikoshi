package app

import (
	"log/slog"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/postgres"
	"github.com/standards-lab/go-messaging/nats"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/s3"
	"github.com/standards-lab/sqlate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

func defineInfrastructure(g *graph.Graph, n *Nodes) {
	n.LifecycleConfig = g.Define("lifecycle config", n.newLifecycleConfig)
	n.Logger = g.Define("logger", n.newLogger)
	n.Database = g.Define("database", n.newDatabase)
	n.Storage = g.Define("storage", n.newStorage)
	n.Broker = g.Define("broker", n.newBroker)
	n.SQL = g.Define("sql", n.newSQL)
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

func (n *Nodes) newStorage(s *graph.Scope) (*storage.Store, error) {
	cfg := s.Use(n.Config).Storage
	client, err := s3.New(cfg)
	if err != nil {
		return nil, err
	}
	return storage.New(client, cfg), nil
}

func (n *Nodes) newBroker(s *graph.Scope) (*nats.Broker, error) {
	return nats.New(s.Use(n.Config).NATS)
}

func (n *Nodes) newSQL(s *graph.Scope) (*data.Database, error) {
	session := sqlate.Wrap(s.Use(n.Database).Conn(), pgdialect.Dialect{})
	return data.New(session, query.MustCatalog(query.Patterns())), nil
}
