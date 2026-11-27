package app

import (
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-database/sweep"

	"github.com/JaimeStill/capstone-mikoshi/internal/config"
)

const (
	reclaimExpiredUploads = "expired_uploads"
	reclaimPendingBlobs   = "pending_blobs"
)

func defineReactors(g *graph.Graph, n *Nodes) {
	n.Reactors = g.Group("reactors")
	n.Sweeper = g.Define("sweeper", n.newSweeper, graph.In(n.Reactors), graph.After(n.Schema))
}

func (n *Nodes) newSweeper(s *graph.Scope) (*sweep.Sweeper, error) {
	cfg := s.Use(n.Config)
	policy := func(p config.SweepPolicy) sweep.Policy {
		return sweep.Policy{Interval: p.Interval.Duration(), Budget: *p.Budget}
	}
	regs := []sweep.Registration{
		{
			Name:      reclaimExpiredUploads,
			Reclaimer: s.Use(n.Uploads).Reclaimer(),
			Policy:    policy(cfg.Sweep.ExpiredUploads),
		},
		{
			Name:      reclaimPendingBlobs,
			Reclaimer: s.Use(n.Items).PendingReclaimer(cfg.Sweep.PendingBlobs.StaleAge.Duration()),
			Policy:    policy(cfg.Sweep.PendingBlobs.SweepPolicy),
		},
	}
	return sweep.New(s.Use(n.SQL).DB, regs, cfg.ShutdownTimeout.Duration(), s.Use(n.Logger),
		sweep.Options{Gate: s.Use(n.Gate)})
}
