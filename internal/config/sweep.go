package config

import (
	"fmt"
	"strconv"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
)

const (
	defaultExpiredUploadsInterval = time.Minute
	defaultExpiredUploadsBudget   = 100
	defaultPendingBlobsInterval   = 5 * time.Minute
	defaultPendingBlobsBudget     = 100
	defaultPendingBlobsStaleAge   = time.Hour
	defaultOrphanBlobsInterval    = 15 * time.Minute
	defaultOrphanBlobsBudget      = 100
	defaultOrphanBlobsStaleAge    = 24 * time.Hour
	defaultMessagesInterval       = 5 * time.Minute
	defaultMessagesBudget         = 500
	defaultMessagesAge            = 168 * time.Hour
)

// SweepConfig is the service-owned sweep policy: the schedule of each
// reclaimer go-database's sweeper runs. ExpiredUploads is the expired
// uploads' reclaimer's; PendingBlobs the pending blobs' reclaimer's, with
// the age past which a pending blob row is stale; OrphanBlobs the
// orphaned blobs' reclaimer's, with the age past which content no item
// names is stale; OutboxMessages the outbox's retention, with the age past
// which a published outbox row is removed; and InboxMessages the inbox's
// retention, with the age past which a handled inbox row is removed.
type SweepConfig struct {
	ExpiredUploads SweepPolicy     `json:"expired_uploads"`
	PendingBlobs   StalePolicy     `json:"pending_blobs"`
	OrphanBlobs    StalePolicy     `json:"orphan_blobs"`
	OutboxMessages RetentionPolicy `json:"outbox_messages"`
	InboxMessages  RetentionPolicy `json:"inbox_messages"`
	// Env records the environment-variable names Finalize composed and read.
	Env SweepEnv `json:"-"`
}

// SweepPolicy is one reclaimer's schedule: the least time between the
// starts of two of its cycles, and the most units one of its passes
// reclaims. Pointer fields distinguish unset from zero.
type SweepPolicy struct {
	Interval *libconfig.Duration `json:"interval"`
	Budget   *int                `json:"budget"`
}

// StalePolicy is the schedule of a reclaimer that removes a blob row once
// it is stale, and the age, since the row last moved, past which the
// reclaimer removes it and what it keeps.
type StalePolicy struct {
	SweepPolicy
	StaleAge *libconfig.Duration `json:"stale_age"`
}

// RetentionPolicy is the schedule of a reclaimer that removes a messaging
// table's rows once they are done with, and the age, since a row was
// published or handled, past which the reclaimer removes it.
type RetentionPolicy struct {
	SweepPolicy
	Age *libconfig.Duration `json:"age"`
}

// SweepEnv holds the environment-variable names of the sweep block.
type SweepEnv struct {
	ExpiredUploadsInterval string
	ExpiredUploadsBudget   string
	PendingBlobsInterval   string
	PendingBlobsBudget     string
	PendingBlobsStaleAge   string
	OrphanBlobsInterval    string
	OrphanBlobsBudget      string
	OrphanBlobsStaleAge    string
	OutboxMessagesInterval string
	OutboxMessagesBudget   string
	OutboxMessagesAge      string
	InboxMessagesInterval  string
	InboxMessagesBudget    string
	InboxMessagesAge       string
}

// Merge overlays src's set fields onto the receiver.
func (c *SweepConfig) Merge(src *SweepConfig) {
	if src == nil {
		return
	}
	c.ExpiredUploads.merge(&src.ExpiredUploads)
	c.PendingBlobs.merge(&src.PendingBlobs)
	c.OrphanBlobs.merge(&src.OrphanBlobs)
	c.OutboxMessages.merge(&src.OutboxMessages)
	c.InboxMessages.merge(&src.InboxMessages)
}

func (p *RetentionPolicy) merge(src *RetentionPolicy) {
	p.SweepPolicy.merge(&src.SweepPolicy)
	if src.Age != nil {
		v := *src.Age
		p.Age = &v
	}
}

func (p *StalePolicy) merge(src *StalePolicy) {
	p.SweepPolicy.merge(&src.SweepPolicy)
	if src.StaleAge != nil {
		v := *src.StaleAge
		p.StaleAge = &v
	}
}

func (p *SweepPolicy) merge(src *SweepPolicy) {
	if src.Interval != nil {
		v := *src.Interval
		p.Interval = &v
	}
	if src.Budget != nil {
		v := *src.Budget
		p.Budget = &v
	}
}

// Finalize applies the defaults, the expired uploads' reclaimer every
// minute, the pending blobs' every five minutes, and the orphaned blobs'
// every fifteen minutes, each pass reclaiming at most 100, a pending blob
// row stale after an hour, and content no item names stale after a day;
// and the outbox's and the inbox's retentions every five minutes, each
// pass removing at most 500 rows older than a week; reads the block's
// environment overrides; and validates: every interval and age positive
// and every budget at least 1, the bounds the sweeper and the retentions
// refuse otherwise. An empty prefix composes empty names, which
// disable the overrides.
func (c *SweepConfig) Finalize(envPrefix string) error {
	c.Env = SweepEnv{
		ExpiredUploadsInterval: libconfig.EnvName(envPrefix, "sweep_expired_uploads_interval"),
		ExpiredUploadsBudget:   libconfig.EnvName(envPrefix, "sweep_expired_uploads_budget"),
		PendingBlobsInterval:   libconfig.EnvName(envPrefix, "sweep_pending_blobs_interval"),
		PendingBlobsBudget:     libconfig.EnvName(envPrefix, "sweep_pending_blobs_budget"),
		PendingBlobsStaleAge:   libconfig.EnvName(envPrefix, "sweep_pending_blobs_stale_age"),
		OrphanBlobsInterval:    libconfig.EnvName(envPrefix, "sweep_orphan_blobs_interval"),
		OrphanBlobsBudget:      libconfig.EnvName(envPrefix, "sweep_orphan_blobs_budget"),
		OrphanBlobsStaleAge:    libconfig.EnvName(envPrefix, "sweep_orphan_blobs_stale_age"),
		OutboxMessagesInterval: libconfig.EnvName(envPrefix, "sweep_outbox_messages_interval"),
		OutboxMessagesBudget:   libconfig.EnvName(envPrefix, "sweep_outbox_messages_budget"),
		OutboxMessagesAge:      libconfig.EnvName(envPrefix, "sweep_outbox_messages_age"),
		InboxMessagesInterval:  libconfig.EnvName(envPrefix, "sweep_inbox_messages_interval"),
		InboxMessagesBudget:    libconfig.EnvName(envPrefix, "sweep_inbox_messages_budget"),
		InboxMessagesAge:       libconfig.EnvName(envPrefix, "sweep_inbox_messages_age"),
	}
	err := c.ExpiredUploads.finalize("expired_uploads", c.Env.ExpiredUploadsInterval, c.Env.ExpiredUploadsBudget,
		defaultExpiredUploadsInterval, defaultExpiredUploadsBudget)
	if err != nil {
		return err
	}
	err = c.PendingBlobs.finalize("pending_blobs", c.Env.PendingBlobsInterval, c.Env.PendingBlobsBudget, c.Env.PendingBlobsStaleAge,
		defaultPendingBlobsInterval, defaultPendingBlobsBudget, defaultPendingBlobsStaleAge)
	if err != nil {
		return err
	}
	err = c.OrphanBlobs.finalize("orphan_blobs", c.Env.OrphanBlobsInterval, c.Env.OrphanBlobsBudget, c.Env.OrphanBlobsStaleAge,
		defaultOrphanBlobsInterval, defaultOrphanBlobsBudget, defaultOrphanBlobsStaleAge)
	if err != nil {
		return err
	}
	err = c.OutboxMessages.finalize("outbox_messages", c.Env.OutboxMessagesInterval, c.Env.OutboxMessagesBudget, c.Env.OutboxMessagesAge)
	if err != nil {
		return err
	}
	return c.InboxMessages.finalize("inbox_messages", c.Env.InboxMessagesInterval, c.Env.InboxMessagesBudget, c.Env.InboxMessagesAge)
}

func (p *RetentionPolicy) finalize(name, intervalEnv, budgetEnv, ageEnv string) error {
	if err := p.SweepPolicy.finalize(name, intervalEnv, budgetEnv, defaultMessagesInterval, defaultMessagesBudget); err != nil {
		return err
	}
	if p.Age == nil {
		p.Age = new(libconfig.Duration(defaultMessagesAge))
	}
	if err := libconfig.SetDurationFromEnv(&p.Age, ageEnv); err != nil {
		return err
	}
	if *p.Age <= 0 {
		return fmt.Errorf("%s.age must be positive, got %s", name, *p.Age)
	}
	return nil
}

func (p *StalePolicy) finalize(name, intervalEnv, budgetEnv, staleAgeEnv string, interval time.Duration, budget int, staleAge time.Duration) error {
	if err := p.SweepPolicy.finalize(name, intervalEnv, budgetEnv, interval, budget); err != nil {
		return err
	}
	if p.StaleAge == nil {
		p.StaleAge = new(libconfig.Duration(staleAge))
	}
	if err := libconfig.SetDurationFromEnv(&p.StaleAge, staleAgeEnv); err != nil {
		return err
	}
	if *p.StaleAge <= 0 {
		return fmt.Errorf("%s.stale_age must be positive, got %s", name, *p.StaleAge)
	}
	return nil
}

func (p *SweepPolicy) finalize(name, intervalEnv, budgetEnv string, interval time.Duration, budget int) error {
	if p.Interval == nil {
		p.Interval = new(libconfig.Duration(interval))
	}
	if p.Budget == nil {
		p.Budget = new(budget)
	}
	if err := libconfig.SetDurationFromEnv(&p.Interval, intervalEnv); err != nil {
		return err
	}
	if err := libconfig.SetFromEnv(&p.Budget, budgetEnv, strconv.Atoi); err != nil {
		return err
	}
	if *p.Interval <= 0 {
		return fmt.Errorf("%s.interval must be positive, got %s", name, *p.Interval)
	}
	if *p.Budget < 1 {
		return fmt.Errorf("%s.budget must be at least 1, got %d", name, *p.Budget)
	}
	return nil
}
