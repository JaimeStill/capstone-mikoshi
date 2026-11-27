package config_test

import (
	"strings"
	"testing"
	"time"

	libconfig "github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/logging"
	"github.com/standards-lab/go-web-sdk"

	"github.com/JaimeStill/capstone-mikoshi/internal/config"
	"github.com/JaimeStill/capstone-mikoshi/internal/config/configtest"
)

func TestConfig_MergeOverlaysSetFields(t *testing.T) {
	base := &config.Config{ShutdownTimeout: libconfig.Duration(10 * time.Second)}
	base.Log.Level = logging.LevelInfo
	base.Server.Host = "0.0.0.0"

	overlay := &config.Config{}
	overlay.Log.Level = logging.LevelDebug
	overlay.Server.Host = "127.0.0.1"

	base.Merge(overlay)

	if base.Log.Level != logging.LevelDebug {
		t.Errorf("Log.Level = %s, want debug", base.Log.Level)
	}
	if base.Server.Host != "127.0.0.1" {
		t.Errorf("Server.Host = %s, want 127.0.0.1", base.Server.Host)
	}
	if got := base.ShutdownTimeout.Duration(); got != 10*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 10s", got)
	}
}

func TestConfig_FinalizeDefaults(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.ShutdownTimeout.Duration(); got != 10*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 10s", got)
	}
	if cfg.Log.Level != logging.LevelInfo {
		t.Errorf("Log.Level = %s, want info", cfg.Log.Level)
	}
	if got := cfg.Server.Addr(); got != "0.0.0.0:8080" {
		t.Errorf("Server.Addr() = %s, want 0.0.0.0:8080", got)
	}
}

func TestConfig_FinalizeReadsEnvUnderPrefix(t *testing.T) {
	t.Setenv("SVC_LOG_LEVEL", "debug")
	t.Setenv("SVC_SERVER_PORT", "9090")
	t.Setenv("MIKOSHI_LOG_LEVEL", "error")
	t.Setenv("MIKOSHI_SERVER_PORT", "7070")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("svc"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if cfg.Log.Level != logging.LevelDebug {
		t.Errorf("Log.Level = %s, want debug from SVC_LOG_LEVEL", cfg.Log.Level)
	}
	if cfg.Server.Port == nil || *cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %v, want 9090 from SVC_SERVER_PORT", cfg.Server.Port)
	}
}

func TestConfig_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv("MIKOSHI_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("MIKOSHI_LOG_LEVEL", "debug")
	t.Setenv("MIKOSHI_SERVER_PORT", "9090")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("mikoshi"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.ShutdownTimeout.Duration(); got != 30*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 30s", got)
	}
	if cfg.Log.Level != logging.LevelDebug {
		t.Errorf("Log.Level = %s, want debug", cfg.Log.Level)
	}
	if cfg.Server.Port == nil || *cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %v, want 9090", cfg.Server.Port)
	}
}

func TestConfig_FinalizeRejectsNonPositiveShutdownTimeout(t *testing.T) {
	for _, v := range []string{"-5s", "0s"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("MIKOSHI_SHUTDOWN_TIMEOUT", v)

			cfg := configtest.Minimal()
			err := cfg.Finalize("mikoshi")
			if err == nil {
				t.Fatalf("Finalize accepted shutdown_timeout %s", v)
			}
			if !strings.Contains(err.Error(), "shutdown_timeout must be positive") {
				t.Errorf("error = %v, want \"shutdown_timeout must be positive\"", err)
			}
		})
	}
}

func TestConfig_FinalizeRejectsNonPositiveFileShutdownTimeout(t *testing.T) {
	cfg := configtest.Minimal()
	cfg.ShutdownTimeout = libconfig.Duration(-time.Second)
	err := cfg.Finalize("")
	if err == nil || !strings.Contains(err.Error(), "shutdown_timeout must be positive") {
		t.Errorf("Finalize = %v, want \"shutdown_timeout must be positive\"", err)
	}
}

func TestConfig_FinalizeRejectsUnparsableShutdownTimeout(t *testing.T) {
	t.Setenv("MIKOSHI_SHUTDOWN_TIMEOUT", "soon")

	cfg := configtest.Minimal()
	err := cfg.Finalize("mikoshi")
	if err == nil {
		t.Fatal("Finalize accepted an unparsable MIKOSHI_SHUTDOWN_TIMEOUT")
	}
	if !strings.Contains(err.Error(), "MIKOSHI_SHUTDOWN_TIMEOUT") {
		t.Errorf("error = %v, want it to name MIKOSHI_SHUTDOWN_TIMEOUT", err)
	}
}

func TestConfig_FinalizeEmptyPrefixReadsNoShutdownOverride(t *testing.T) {
	t.Setenv("MIKOSHI_SHUTDOWN_TIMEOUT", "30s")
	t.Setenv("SHUTDOWN_TIMEOUT", "30s")

	cfg := configtest.Minimal()
	cfg.ShutdownTimeout = libconfig.Duration(3 * time.Second)
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if got := cfg.ShutdownTimeout.Duration(); got != 3*time.Second {
		t.Errorf("ShutdownTimeout = %s, want the configured 3s", got)
	}
}

func TestConfig_MergeOverlayShutdownTimeoutReplacesBase(t *testing.T) {
	base := &config.Config{ShutdownTimeout: libconfig.Duration(10 * time.Second)}
	base.Merge(&config.Config{ShutdownTimeout: libconfig.Duration(3 * time.Second)})

	if got := base.ShutdownTimeout.Duration(); got != 3*time.Second {
		t.Errorf("ShutdownTimeout = %s, want the overlay's 3s", got)
	}
}

func TestConfig_FinalizeWrapsChildErrors(t *testing.T) {
	t.Setenv("MIKOSHI_LOG_LEVEL", "verbose")

	cfg := configtest.Minimal()
	err := cfg.Finalize("mikoshi")
	if err == nil {
		t.Fatal("Finalize accepted an invalid log level")
	}
	if !strings.Contains(err.Error(), "log:") {
		t.Errorf("error = %v, want the log block wrap", err)
	}
}

func TestReads_FinalizeDefaults(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.Reads.Limits(); got != (web.Limits{DefaultSize: 20, MaxSize: 100}) {
		t.Errorf("Reads.Limits() = %+v, want {20 100}", got)
	}
	if cfg.Reads.Env != (config.ReadsEnv{}) {
		t.Errorf("Reads.Env = %+v, want empty under the empty prefix", cfg.Reads.Env)
	}
}

func TestReads_MergeOverlaysSetFields(t *testing.T) {
	base := &config.Config{}
	base.Reads.DefaultSize = new(10)
	base.Reads.MaxSize = new(50)

	overlay := &config.Config{}
	overlay.Reads.MaxSize = new(200)

	base.Merge(overlay)

	if got := *base.Reads.MaxSize; got != 200 {
		t.Errorf("Reads.MaxSize = %d, want 200", got)
	}
	if got := *base.Reads.DefaultSize; got != 10 {
		t.Errorf("Reads.DefaultSize = %d, want 10", got)
	}
}

func TestReads_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv("MIKOSHI_READS_DEFAULT_SIZE", "25")
	t.Setenv("MIKOSHI_READS_MAX_SIZE", "250")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("mikoshi"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.Reads.Limits(); got != (web.Limits{DefaultSize: 25, MaxSize: 250}) {
		t.Errorf("Reads.Limits() = %+v, want {25 250}", got)
	}
	want := config.ReadsEnv{DefaultSize: "MIKOSHI_READS_DEFAULT_SIZE", MaxSize: "MIKOSHI_READS_MAX_SIZE"}
	if cfg.Reads.Env != want {
		t.Errorf("Reads.Env = %+v, want %+v", cfg.Reads.Env, want)
	}
}

func TestReads_FinalizeRejectsInvalidPolicy(t *testing.T) {
	cases := map[string]struct {
		defaultSize, maxSize string
		want                 string
	}{
		"default below one":    {"0", "100", "default_size"},
		"max below default":    {"20", "10", "max_size"},
		"default not a number": {"twenty", "100", "MIKOSHI_READS_DEFAULT_SIZE"},
		"max not a number":     {"20", "many", "MIKOSHI_READS_MAX_SIZE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MIKOSHI_READS_DEFAULT_SIZE", tc.defaultSize)
			t.Setenv("MIKOSHI_READS_MAX_SIZE", tc.maxSize)

			cfg := configtest.Minimal()
			err := cfg.Finalize("mikoshi")
			if err == nil {
				t.Fatal("Finalize accepted an invalid reads policy")
			}
			if !strings.Contains(err.Error(), "reads:") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want the reads block wrap naming %s", err, tc.want)
			}
		})
	}
}

func TestUploads_FinalizeDefaults(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.Uploads.Lifetime.Duration(); got != 24*time.Hour {
		t.Errorf("Uploads.Lifetime = %s, want 24h", got)
	}
	if got := *cfg.Uploads.MaxAppendSize; got != 4<<20 {
		t.Errorf("Uploads.MaxAppendSize = %d, want 4 MiB", got)
	}
	if cfg.Uploads.Env != (config.UploadsEnv{}) {
		t.Errorf("Uploads.Env = %+v, want empty under the empty prefix", cfg.Uploads.Env)
	}
}

func TestUploads_MergeOverlaysSetFields(t *testing.T) {
	base := &config.Config{}
	base.Uploads.Lifetime = new(libconfig.Duration(time.Hour))
	base.Uploads.MaxAppendSize = new(int64(1024))

	overlay := &config.Config{}
	overlay.Uploads.MaxAppendSize = new(int64(2048))

	base.Merge(overlay)

	if got := *base.Uploads.MaxAppendSize; got != 2048 {
		t.Errorf("Uploads.MaxAppendSize = %d, want 2048", got)
	}
	if got := base.Uploads.Lifetime.Duration(); got != time.Hour {
		t.Errorf("Uploads.Lifetime = %s, want 1h", got)
	}
}

func TestUploads_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv("MIKOSHI_UPLOADS_LIFETIME", "90m")
	t.Setenv("MIKOSHI_UPLOADS_MAX_APPEND_SIZE", "65536")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("mikoshi"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if got := cfg.Uploads.Lifetime.Duration(); got != 90*time.Minute {
		t.Errorf("Uploads.Lifetime = %s, want 1h30m", got)
	}
	if got := *cfg.Uploads.MaxAppendSize; got != 65536 {
		t.Errorf("Uploads.MaxAppendSize = %d, want 65536", got)
	}
	want := config.UploadsEnv{Lifetime: "MIKOSHI_UPLOADS_LIFETIME", MaxAppendSize: "MIKOSHI_UPLOADS_MAX_APPEND_SIZE"}
	if cfg.Uploads.Env != want {
		t.Errorf("Uploads.Env = %+v, want %+v", cfg.Uploads.Env, want)
	}
}

func TestUploads_FinalizeRejectsInvalidPolicy(t *testing.T) {
	cases := map[string]struct {
		lifetime, maxAppendSize string
		want                    string
	}{
		"lifetime zero":                {"0s", "1024", "lifetime"},
		"lifetime negative":            {"-1h", "1024", "lifetime"},
		"lifetime not a duration":      {"soon", "1024", "MIKOSHI_UPLOADS_LIFETIME"},
		"max-append-size zero":         {"1h", "0", "max_append_size"},
		"max-append-size past Integer": {"1h", "1000000000000000", "max_append_size"},
		"max-append-size not a number": {"1h", "lots", "MIKOSHI_UPLOADS_MAX_APPEND_SIZE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MIKOSHI_UPLOADS_LIFETIME", tc.lifetime)
			t.Setenv("MIKOSHI_UPLOADS_MAX_APPEND_SIZE", tc.maxAppendSize)

			cfg := configtest.Minimal()
			err := cfg.Finalize("mikoshi")
			if err == nil {
				t.Fatal("Finalize accepted an invalid uploads policy")
			}
			if !strings.Contains(err.Error(), "uploads:") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want the uploads block wrap naming %s", err, tc.want)
			}
		})
	}
}

func TestDatabase_FinalizeDefaults(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if !cfg.Database.Finalized() {
		t.Error("Database.Finalized() = false after Finalize")
	}
	if cfg.Database.Host != "localhost" {
		t.Errorf("Database.Host = %q, want the localhost default", cfg.Database.Host)
	}
	if cfg.Database.Password != "" {
		t.Errorf("Database.Password = %q, want none without a secrets layer", cfg.Database.Password)
	}
}

func TestDatabase_MergeOverlaysSetFields(t *testing.T) {
	base := configtest.Minimal()
	base.Database.User = "app"
	base.Database.Options = map[string]string{"sslmode": "disable"}

	overlay := &config.Config{}
	overlay.Database.Host = "127.0.0.1"
	overlay.Database.Port = new(5440)
	overlay.Database.Password = "secret"

	base.Merge(overlay)

	if base.Database.Host != "127.0.0.1" || base.Database.Port == nil || *base.Database.Port != 5440 {
		t.Errorf("Database host:port = %s:%v, want 127.0.0.1:5440", base.Database.Host, base.Database.Port)
	}
	if base.Database.Password != "secret" {
		t.Errorf("Database.Password = %q, want the overlay's", base.Database.Password)
	}
	if base.Database.Name != "app" || base.Database.User != "app" || base.Database.Options["sslmode"] != "disable" {
		t.Errorf("Database = %+v, want the base's name, user, and options kept", base.Database)
	}
}

func TestDatabase_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv("MIKOSHI_DATABASE_HOST", "db.internal")
	t.Setenv("MIKOSHI_DATABASE_PORT", "5441")
	t.Setenv("MIKOSHI_DATABASE_NAME", "mikoshi_test")
	t.Setenv("MIKOSHI_DATABASE_USER", "tester")
	t.Setenv("MIKOSHI_DATABASE_PASSWORD", "from-env")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("mikoshi"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	db := cfg.Database
	if db.Host != "db.internal" || db.Port == nil || *db.Port != 5441 {
		t.Errorf("Database host:port = %s:%v, want db.internal:5441", db.Host, db.Port)
	}
	if db.Name != "mikoshi_test" || db.User != "tester" || db.Password != "from-env" {
		t.Errorf("Database name, user, password = %q, %q, %q, want the MIKOSHI_DATABASE_* values", db.Name, db.User, db.Password)
	}
	if db.Env.Password != "MIKOSHI_DATABASE_PASSWORD" {
		t.Errorf("Database.Env.Password = %q, want MIKOSHI_DATABASE_PASSWORD", db.Env.Password)
	}
}

func TestDatabase_FinalizeRequiresName(t *testing.T) {
	cfg := &config.Config{}
	err := cfg.Finalize("")
	if err == nil {
		t.Fatal("Finalize accepted a config with no database name")
	}
	if !strings.Contains(err.Error(), "database:") {
		t.Errorf("error = %v, want the database block wrap", err)
	}
}

func TestDatabase_FinalizeWrapsInvalidPort(t *testing.T) {
	t.Setenv("MIKOSHI_DATABASE_PORT", "five")

	cfg := configtest.Minimal()
	err := cfg.Finalize("mikoshi")
	if err == nil {
		t.Fatal("Finalize accepted an unparsable MIKOSHI_DATABASE_PORT")
	}
	if !strings.Contains(err.Error(), "database:") || !strings.Contains(err.Error(), "MIKOSHI_DATABASE_PORT") {
		t.Errorf("error = %v, want the database block wrap naming MIKOSHI_DATABASE_PORT", err)
	}
}

func TestStorage_FinalizeDefaults(t *testing.T) {
	cfg := configtest.Minimal()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if !cfg.Storage.Finalized() {
		t.Error("Storage.Finalized() = false after Finalize")
	}
	if got := cfg.Storage.RequestTimeout.Duration(); got != 10*time.Second {
		t.Errorf("Storage.RequestTimeout = %s, want the 10s default", got)
	}
	if cfg.Storage.Key != "" {
		t.Errorf("Storage.Key = %q, want none without a secrets layer", cfg.Storage.Key)
	}
}

func TestStorage_FinalizeEnvOverrides(t *testing.T) {
	t.Setenv("MIKOSHI_STORAGE_ENDPOINT", "http://127.0.0.1:8337")
	t.Setenv("MIKOSHI_STORAGE_CONTAINER", "mikoshi-test")
	t.Setenv("MIKOSHI_STORAGE_ACCOUNT", "tester")
	t.Setenv("MIKOSHI_STORAGE_KEY", "from-env")
	t.Setenv("MIKOSHI_STORAGE_OPTIONS_REGION", "eu-west-1")

	cfg := configtest.Minimal()
	if err := cfg.Finalize("mikoshi"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	st := cfg.Storage
	if st.Endpoint != "http://127.0.0.1:8337" || st.Container != "mikoshi-test" {
		t.Errorf("Storage endpoint, container = %q, %q, want the MIKOSHI_STORAGE_* values", st.Endpoint, st.Container)
	}
	if st.Account != "tester" || st.Key != "from-env" {
		t.Errorf("Storage account, key = %q, %q, want the MIKOSHI_STORAGE_* values", st.Account, st.Key)
	}
	if st.Options["region"] != "eu-west-1" {
		t.Errorf("Storage.Options[region] = %q, want MIKOSHI_STORAGE_OPTIONS_REGION's", st.Options["region"])
	}
}

func TestStorage_FinalizeRequiresContainer(t *testing.T) {
	cfg := configtest.Minimal()
	cfg.Storage.Container = ""
	err := cfg.Finalize("")
	if err == nil {
		t.Fatal("Finalize accepted a config with no storage container")
	}
	if !strings.Contains(err.Error(), "storage:") || !strings.Contains(err.Error(), "container") {
		t.Errorf("error = %v, want the storage block wrap naming the container", err)
	}
}
