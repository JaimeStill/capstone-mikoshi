package config

import (
	"fmt"
	"strconv"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-web-sdk/upload"
)

const (
	defaultUploadsLifetime      = 24 * time.Hour
	defaultUploadsMaxAppendSize = 4 << 20
)

// UploadsConfig is the service-owned policy of the resumable upload
// handler: how long an upload lives from its creation, and the
// max-append-size, the most content one creation or append carries, which
// the handler announces and enforces, and which bounds a posted record's
// body too. Pointer fields distinguish unset from zero.
type UploadsConfig struct {
	Lifetime      *libconfig.Duration `json:"lifetime"`
	MaxAppendSize *int64              `json:"max_append_size"`
	// Env records the environment-variable names Finalize composed and read.
	Env UploadsEnv `json:"-"`
}

// UploadsEnv holds the environment-variable names of the uploads block.
type UploadsEnv struct {
	Lifetime      string
	MaxAppendSize string
}

// Merge overlays src's set fields onto the receiver.
func (c *UploadsConfig) Merge(src *UploadsConfig) {
	if src == nil {
		return
	}
	if src.Lifetime != nil {
		v := *src.Lifetime
		c.Lifetime = &v
	}
	if src.MaxAppendSize != nil {
		v := *src.MaxAppendSize
		c.MaxAppendSize = &v
	}
}

// Finalize applies the defaults, a 24h lifetime and a 4 MiB
// max-append-size, reads the block's environment overrides, and
// validates: a positive lifetime, and a max-append-size of at least 1 that
// a structured field Integer can carry, which the upload handler panics
// on, caught here as configuration rather than at wiring. An empty prefix
// composes empty names, which disable the overrides.
func (c *UploadsConfig) Finalize(envPrefix string) error {
	if c.Lifetime == nil {
		c.Lifetime = new(libconfig.Duration(defaultUploadsLifetime))
	}
	if c.MaxAppendSize == nil {
		c.MaxAppendSize = new(int64(defaultUploadsMaxAppendSize))
	}
	c.Env.Lifetime = libconfig.EnvName(envPrefix, "uploads_lifetime")
	c.Env.MaxAppendSize = libconfig.EnvName(envPrefix, "uploads_max_append_size")
	if err := libconfig.SetDurationFromEnv(&c.Lifetime, c.Env.Lifetime); err != nil {
		return err
	}
	parse := func(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }
	if err := libconfig.SetFromEnv(&c.MaxAppendSize, c.Env.MaxAppendSize, parse); err != nil {
		return err
	}
	if *c.Lifetime <= 0 {
		return fmt.Errorf("lifetime must be positive, got %s", *c.Lifetime)
	}
	if _, err := upload.FormatInteger(*c.MaxAppendSize); err != nil || *c.MaxAppendSize < 1 {
		return fmt.Errorf("max_append_size must be a positive structured field Integer, got %d", *c.MaxAppendSize)
	}
	return nil
}
