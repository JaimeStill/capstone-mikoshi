package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JaimeStill/capstone-mikoshi/internal/config"
)

func inDir(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	t.Chdir(dir)
	t.Setenv("MIKOSHI_ENV", "")
	t.Setenv("MIKOSHI_SHUTDOWN_TIMEOUT", "")
	t.Setenv("MIKOSHI_DATABASE_PASSWORD", "")
	t.Setenv("MIKOSHI_STORAGE_KEY", "")
}

func TestLoad_ShutdownTimeoutIsTopLevelKey(t *testing.T) {
	inDir(t, map[string]string{"config.json": `{"messaging": {"source": "mikoshi"}, "nats": {"stream": "MIKOSHI", "prefix": "mikoshi"}, "storage": {"container": "mikoshi"}, "database": {"name": "app"}, "shutdown_timeout": "3s"}`})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ShutdownTimeout.Duration(); got != 3*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 3s from the file", got)
	}
}

func TestLoad_ShutdownTimeoutDefaultsTo10s(t *testing.T) {
	inDir(t, map[string]string{"config.json": `{"messaging": {"source": "mikoshi"}, "nats": {"stream": "MIKOSHI", "prefix": "mikoshi"}, "storage": {"container": "mikoshi"}, "database": {"name": "app"}}`})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ShutdownTimeout.Duration(); got != 10*time.Second {
		t.Errorf("ShutdownTimeout = %s, want the 10s default", got)
	}
}

func TestLoad_EnvOverridesFileShutdownTimeout(t *testing.T) {
	inDir(t, map[string]string{"config.json": `{"messaging": {"source": "mikoshi"}, "nats": {"stream": "MIKOSHI", "prefix": "mikoshi"}, "storage": {"container": "mikoshi"}, "database": {"name": "app"}, "shutdown_timeout": "3s"}`})
	t.Setenv("MIKOSHI_SHUTDOWN_TIMEOUT", "4s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ShutdownTimeout.Duration(); got != 4*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 4s from MIKOSHI_SHUTDOWN_TIMEOUT", got)
	}
}

func TestLoad_OverlayShutdownTimeout(t *testing.T) {
	cases := map[string]struct {
		overlay string
		want    time.Duration
	}{
		"overlay sets it":   {`{"shutdown_timeout": "7s"}`, 7 * time.Second},
		"overlay leaves it": {`{"log": {"level": "debug"}}`, 3 * time.Second},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			inDir(t, map[string]string{
				"config.json":      `{"messaging": {"source": "mikoshi"}, "nats": {"stream": "MIKOSHI", "prefix": "mikoshi"}, "storage": {"container": "mikoshi"}, "database": {"name": "app"}, "shutdown_timeout": "3s"}`,
				"config.test.json": tc.overlay,
			})
			t.Setenv("MIKOSHI_ENV", "test")

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := cfg.ShutdownTimeout.Duration(); got != tc.want {
				t.Errorf("ShutdownTimeout = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestLoad_RejectsUnknownTopLevelKeys(t *testing.T) {
	for _, key := range []string{"Config", "Env"} {
		t.Run(key, func(t *testing.T) {
			inDir(t, map[string]string{"config.json": `{"` + key + `": {}}`})

			_, err := config.Load()
			if err == nil {
				t.Fatalf("Load accepted a %q key", key)
			}
			if want := `unknown field "` + key + `"`; !strings.Contains(err.Error(), want) {
				t.Errorf("error = %v, want %s", err, want)
			}
		})
	}
}

func TestLoad_PasswordFromSecretsFile(t *testing.T) {
	inDir(t, map[string]string{
		"config.json":  `{"messaging": {"source": "mikoshi"}, "nats": {"stream": "MIKOSHI", "prefix": "mikoshi"}, "storage": {"container": "mikoshi"}, "database": {"name": "app", "user": "app"}}`,
		"secrets.json": `{"database": {"password": "from-secrets"}}`,
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Password != "from-secrets" {
		t.Errorf("Database.Password = %q, want the secrets file's", cfg.Database.Password)
	}
	if cfg.Database.User != "app" {
		t.Errorf("Database.User = %q, want the config file's app", cfg.Database.User)
	}
}

func TestLoad_EnvOverridesSecretsPassword(t *testing.T) {
	inDir(t, map[string]string{
		"config.json":  `{"messaging": {"source": "mikoshi"}, "nats": {"stream": "MIKOSHI", "prefix": "mikoshi"}, "storage": {"container": "mikoshi"}, "database": {"name": "app"}}`,
		"secrets.json": `{"database": {"password": "from-secrets"}}`,
	})
	t.Setenv("MIKOSHI_DATABASE_PASSWORD", "from-env")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Password != "from-env" {
		t.Errorf("Database.Password = %q, want MIKOSHI_DATABASE_PASSWORD's", cfg.Database.Password)
	}
}

func TestLoad_StorageKeyFromSecretsFile(t *testing.T) {
	inDir(t, map[string]string{
		"config.json":  `{"messaging": {"source": "mikoshi"}, "nats": {"stream": "MIKOSHI", "prefix": "mikoshi"}, "storage": {"container": "mikoshi", "account": "admin"}, "database": {"name": "app"}}`,
		"secrets.json": `{"storage": {"key": "from-secrets"}}`,
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.Key != "from-secrets" {
		t.Errorf("Storage.Key = %q, want the secrets file's", cfg.Storage.Key)
	}
	if cfg.Storage.Account != "admin" {
		t.Errorf("Storage.Account = %q, want the config file's admin", cfg.Storage.Account)
	}
}

func TestLoad_EnvOverridesSecretsStorageKey(t *testing.T) {
	inDir(t, map[string]string{
		"config.json":  `{"messaging": {"source": "mikoshi"}, "nats": {"stream": "MIKOSHI", "prefix": "mikoshi"}, "storage": {"container": "mikoshi"}, "database": {"name": "app"}}`,
		"secrets.json": `{"storage": {"key": "from-secrets"}}`,
	})
	t.Setenv("MIKOSHI_STORAGE_KEY", "from-env")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.Key != "from-env" {
		t.Errorf("Storage.Key = %q, want MIKOSHI_STORAGE_KEY's", cfg.Storage.Key)
	}
}

func TestLoad_RepositoryConfigOwnsTheStream(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "config.json"))
	if err != nil {
		t.Fatalf("read the repository's config.json: %v", err)
	}
	inDir(t, map[string]string{"config.json": string(body)})
	for _, name := range []string{"MESSAGING_SOURCE", "NATS_URL", "NATS_STREAM", "NATS_PREFIX", "NATS_OWNER", "NATS_MAX_AGE"} {
		t.Setenv("MIKOSHI_"+name, "")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Messaging.Source != "mikoshi" {
		t.Errorf("Messaging.Source = %q, want mikoshi", cfg.Messaging.Source)
	}
	n := cfg.NATS
	if n.Stream != "MIKOSHI" || n.Prefix != "mikoshi" {
		t.Errorf("NATS stream, prefix = %q, %q, want MIKOSHI, mikoshi", n.Stream, n.Prefix)
	}
	if n.Owner == nil || !*n.Owner || n.MaxAge.Duration() != 168*time.Hour {
		t.Errorf("NATS owner, max_age = %v, %s, want the stream's owner with a 168h max_age", n.Owner, n.MaxAge)
	}
}
