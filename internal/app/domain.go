package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/upload"

	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
	"github.com/JaimeStill/capstone-mikoshi/domain/items"
	"github.com/JaimeStill/capstone-mikoshi/domain/uploads"
)

const (
	apiPrefix     = "/api"
	itemsPath     = apiPrefix + "/items"
	uploadsPrefix = "/uploads"
)

func defineDomain(g *graph.Graph, n *Nodes) {
	n.Contexts = g.Define("contexts", n.newContexts)
	n.Uploads = g.Define("uploads", n.newUploads)
	n.Items = g.Define("items", n.newItems)
}

func (n *Nodes) newContexts(s *graph.Scope) (*contexts.Service, error) {
	return contexts.New(s.Use(n.SQL)), nil
}

func (n *Nodes) newUploads(s *graph.Scope) (*uploads.Service, error) {
	return uploads.New(s.Use(n.SQL)), nil
}

func (n *Nodes) newItems(s *graph.Scope) (*items.Service, error) {
	return items.New(s.Use(n.SQL), s.Use(n.Storage)), nil
}

func mountAPI(s *graph.Scope, n *Nodes) *web.Group {
	api := web.NewGroup(apiPrefix)
	cfg := s.Use(n.Config)
	logger := s.Use(n.Logger)
	reads := cfg.Reads.Limits()
	reads.Cursor = true
	itemService := s.Use(n.Items)
	transfer := cfg.Server.Transfer(*cfg.Uploads.MaxAppendSize)
	api.Mount(contexts.Routes(s.Use(n.Contexts), reads, logger))
	api.Mount(items.Routes(itemService, reads, transfer, logger))
	api.Mount(upload.Handler{
		Store:    s.Use(n.Uploads),
		Transfer: transfer,
		Lifetime: cfg.Uploads.Lifetime.Duration(),
		Complete: items.Completion(itemService, itemsPath),
		Logger:   logger,
	}.Routes(uploadsPrefix))
	return api
}
