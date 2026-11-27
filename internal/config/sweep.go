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
)

// SweepConfig is the service-owned sweep policy: the schedule of each
// reclaimer go-database's sweeper runs. ExpiredUploads is the expired
// uploads' reclaimer's, and PendingBlobs the pending blobs' reclaimer's,
// with the age past which a pending blob row is stale.
type SweepConfig struct {
	ExpiredUploads SweepPolicy        `json:"expired_uploads"`
	PendingBlobs   PendingBlobsPolicy `json:"pending_blobs"`
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

// PendingBlobsPolicy is the pending blobs' reclaimer's schedule and the
// age, since a pending blob row last moved, past which the reclaimer
// removes it and its object.
type PendingBlobsPolicy struct {
	SweepPolicy
	StaleAge *libconfig.Duration `json:"stale_age"`
}

// SweepEnv holds the environment-variable names of the sweep block.
type SweepEnv struct {
	ExpiredUploadsInterval string
	ExpiredUploadsBudget   string
	PendingBlobsInterval   string
	PendingBlobsBudget     string
	PendingBlobsStaleAge   string
}

// Merge overlays src's set fields onto the receiver.
func (c *SweepConfig) Merge(src *SweepConfig) {
	if src == nil {
		return
	}
	c.ExpiredUploads.merge(&src.ExpiredUploads)
	c.PendingBlobs.merge(&src.PendingBlobs.SweepPolicy)
	if src.PendingBlobs.StaleAge != nil {
		v := *src.PendingBlobs.StaleAge
		c.PendingBlobs.StaleAge = &v
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
// minute and the pending blobs' every five minutes, each pass reclaiming
// at most 100, and a pending blob row stale after an hour; reads the
// block's environment overrides; and validates: every interval and the
// stale age positive and every budget at least 1, the bounds the sweeper
// refuses otherwise. An empty prefix composes empty names, which disable
// the overrides.
func (c *SweepConfig) Finalize(envPrefix string) error {
	c.Env = SweepEnv{
		ExpiredUploadsInterval: libconfig.EnvName(envPrefix, "sweep_expired_uploads_interval"),
		ExpiredUploadsBudget:   libconfig.EnvName(envPrefix, "sweep_expired_uploads_budget"),
		PendingBlobsInterval:   libconfig.EnvName(envPrefix, "sweep_pending_blobs_interval"),
		PendingBlobsBudget:     libconfig.EnvName(envPrefix, "sweep_pending_blobs_budget"),
		PendingBlobsStaleAge:   libconfig.EnvName(envPrefix, "sweep_pending_blobs_stale_age"),
	}
	err := c.ExpiredUploads.finalize("expired_uploads", c.Env.ExpiredUploadsInterval, c.Env.ExpiredUploadsBudget,
		defaultExpiredUploadsInterval, defaultExpiredUploadsBudget)
	if err != nil {
		return err
	}
	err = c.PendingBlobs.finalize("pending_blobs", c.Env.PendingBlobsInterval, c.Env.PendingBlobsBudget,
		defaultPendingBlobsInterval, defaultPendingBlobsBudget)
	if err != nil {
		return err
	}
	if c.PendingBlobs.StaleAge == nil {
		c.PendingBlobs.StaleAge = new(libconfig.Duration(defaultPendingBlobsStaleAge))
	}
	if err := libconfig.SetDurationFromEnv(&c.PendingBlobs.StaleAge, c.Env.PendingBlobsStaleAge); err != nil {
		return err
	}
	if *c.PendingBlobs.StaleAge <= 0 {
		return fmt.Errorf("pending_blobs.stale_age must be positive, got %s", *c.PendingBlobs.StaleAge)
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
