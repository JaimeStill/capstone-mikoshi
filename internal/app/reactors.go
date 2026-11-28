package app

import (
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/reactor"
	"github.com/standards-lab/go-database/sweep"
	"github.com/standards-lab/go-messaging"
	msgpg "github.com/standards-lab/go-messaging/postgres"

	"github.com/JaimeStill/capstone-mikoshi/internal/config"
)

const (
	reclaimExpiredUploads = "expired_uploads"
	reclaimPendingBlobs   = "pending_blobs"
	reclaimOrphanBlobs    = "orphan_blobs"
	reclaimOutbox         = "outbox_messages"
	reclaimInbox          = "inbox_messages"
)

const layer = "mikoshi"

func defineReactors(g *graph.Graph, n *Nodes) {
	n.Reactors = g.Group("reactors")
	n.Sweeper = g.Define("sweeper", n.newSweeper, graph.In(n.Reactors), graph.After(n.Schema))
	n.Runtime = g.Define("runtime", n.newRuntime)
	n.Relay = g.Define("relay", n.newRelay, graph.In(n.Reactors), graph.After(n.Schema))
}

func (n *Nodes) newSweeper(s *graph.Scope) (*sweep.Sweeper, error) {
	cfg := s.Use(n.Config)
	db := s.Use(n.SQL).DB
	policy := func(p config.SweepPolicy) sweep.Policy {
		return sweep.Policy{Interval: p.Interval.Duration(), Budget: *p.Budget}
	}
	outbox, err := msgpg.OutboxRetention(db, cfg.Sweep.OutboxMessages.Age.Duration())
	if err != nil {
		return nil, err
	}
	inbox, err := msgpg.InboxRetention(db, cfg.Sweep.InboxMessages.Age.Duration())
	if err != nil {
		return nil, err
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
		{
			Name:      reclaimOrphanBlobs,
			Reclaimer: s.Use(n.Items).OrphanReclaimer(cfg.Sweep.OrphanBlobs.StaleAge.Duration()),
			Policy:    policy(cfg.Sweep.OrphanBlobs.SweepPolicy),
		},
		{
			Name:      reclaimOutbox,
			Reclaimer: outbox,
			Policy:    policy(cfg.Sweep.OutboxMessages.SweepPolicy),
		},
		{
			Name:      reclaimInbox,
			Reclaimer: inbox,
			Policy:    policy(cfg.Sweep.InboxMessages.SweepPolicy),
		},
	}
	return sweep.New(db, regs, cfg.ShutdownTimeout.Duration(), s.Use(n.Logger),
		sweep.Options{Gate: s.Use(n.Gate)})
}

func (n *Nodes) newRuntime(s *graph.Scope) (*messaging.Runtime, error) {
	cfg := s.Use(n.Config)
	return messaging.Setup{
		Config:   cfg.Messaging,
		Broker:   s.Use(n.Broker),
		Outbox:   msgpg.Outbox(),
		Inbox:    msgpg.Inbox(),
		Shutdown: cfg.ShutdownTimeout.Duration(),
		Logger:   s.Use(n.Logger),
		Recorder: event.Options{Extensions: map[string]string{"layer": layer}},
		Gate:     s.Use(n.Gate),
	}.Build()
}

func (n *Nodes) newRelay(s *graph.Scope) (*reactor.Reactor[event.Event], error) {
	return s.Use(n.Runtime).Relay(s.Use(n.SQL).DB), nil
}
