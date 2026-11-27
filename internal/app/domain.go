package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-web-sdk"

	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
)

func defineDomain(g *graph.Graph, n *Nodes) {
	n.Contexts = g.Define("contexts", n.newContexts)
}

func (n *Nodes) newContexts(s *graph.Scope) (*contexts.Service, error) {
	return contexts.New(s.Use(n.SQL)), nil
}

func mountAPI(s *graph.Scope, n *Nodes) *web.Group {
	api := web.NewGroup("/api")
	api.Mount(contexts.Routes(s.Use(n.Contexts), s.Use(n.Logger)))
	return api
}
