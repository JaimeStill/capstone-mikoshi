package configtest

import (
	"net"
	"strconv"
	"testing"

	"github.com/standards-lab/go-core/logging"

	"github.com/JaimeStill/capstone-mikoshi/internal/config"
)

// Minimal returns an unfinalized Config with every block's required fields
// set and nothing else: the base for tests that exercise Finalize
// themselves, under their own prefix.
func Minimal() *config.Config {
	cfg := &config.Config{}
	cfg.Database.Name = "app"
	cfg.Storage.Container = "mikoshi"
	return cfg
}

// Config returns a finalized Config whose composition performs no I/O: the
// server on a loopback ephemeral port, debug logging so requests leave
// records, and the database and the object store each aimed at a closed
// loopback port, so a dial is refused immediately instead of timing out.
// The empty prefix disables environment overrides.
func Config(t *testing.T) *config.Config {
	t.Helper()
	cfg := Minimal()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = new(int)
	cfg.Log.Level = logging.LevelDebug
	cfg.Database.Host = "127.0.0.1"
	cfg.Database.User = "app"
	cfg.Database.Port = new(ClosedPort(t))
	cfg.Database.Options = map[string]string{"sslmode": "disable"}
	cfg.Storage.Endpoint = "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(ClosedPort(t)))
	cfg.Storage.Account = "admin"
	cfg.Storage.Key = "secret"
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("finalize hermetic config: %v", err)
	}
	return cfg
}

// ClosedPort reserves an ephemeral loopback port and releases it, so a
// connection attempt against it is refused immediately.
func ClosedPort(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	if err := lis.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return port
}
