package integration

import (
	"net"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/s3"
	"github.com/standards-lab/go-web-sdk/webtest"
	"github.com/standards-lab/sqlate/migrate"
	"github.com/standards-lab/sqlate/postgres/pgtest"
	"github.com/standards-lab/sqlate/postgres/pgvector"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

// DSNEnv names the environment variable that holds the admin DSN of the
// PostgreSQL server the suite makes its throwaway databases on; the
// integration task sets it to the integration stack on POSTGRES_PORT.
const DSNEnv = "MIKOSHI_INTEGRATION_DSN"

const databasePrefix = "mikoshi"

// StorageEnv names the environment variable that holds the endpoint of the
// SeaweedFS S3 gateway every service process stores its objects in; the
// integration task sets it to the integration stack on SEAWEEDFS_PORT.
const StorageEnv = "MIKOSHI_INTEGRATION_STORAGE"

const (
	storageAccount = "admin"
	storageKey     = "secret"
)

// ServiceZone is the time zone every service process runs in, through TZ:
// one whose offset is never zero (+05:30 all year, with no daylight
// saving), so a time the service encodes in time.Local rather than
// time.UTC shows an offset in its JSON on any date. The process reads the
// host's zone database, so a host without the zone fails the launch
// rather than running the service in UTC.
const ServiceZone = "Asia/Kolkata"

// Main is the suite's TestMain: it builds cmd/server once, with the race
// detector so the service runs under it too, runs the tests, and removes
// the build.
func Main(m *testing.M) {
	processtest.Main(m, "./cmd/server")
}

// Options shapes one service process. The zero value runs the service on
// the base configuration file and the harness's own variables alone,
// against a throwaway database of its own.
type Options struct {
	// Database is the throwaway database the service connects to, so
	// several processes can share one; nil makes a new one for the process.
	Database *pgtest.Database
	// Env appends further KEY=VALUE overrides, applied last.
	Env []string
}

// Service is one running service process: its address, its captured
// output, and its exit.
type Service struct {
	*processtest.Process
	addr   string
	client *webtest.Client
}

// Database creates an empty throwaway database on the server [DSNEnv]
// names, dropped when the test ends, for one or more service processes to
// share through [Options].
func Database(t testing.TB) *pgtest.Database {
	t.Helper()
	return pgtest.Create(t, DSNEnv, databasePrefix)
}

// Migrated creates a throwaway database as [Database] does and applies
// Mikoshi's migration set to it, declaring pgvector as the service's
// migrator does, for a test that runs a domain's store against the schema
// without a service process.
func Migrated(t testing.TB) *pgtest.Database {
	t.Helper()
	opts := migrate.Options{Extensions: []migrate.Extension{{Name: pgvector.Extension}}}
	return pgtest.MigratedWith(t, DSNEnv, databasePrefix, opts, data.Migrations())
}

// Start runs the service with opts and returns once it is live: Launch
// then Ready.
func Start(t testing.TB, opts Options) *Service {
	t.Helper()
	return Launch(t, opts).Ready(t)
}

// Launch runs the service with opts on a reserved loopback port and returns
// without waiting, so a test can start several processes at once; Ready
// waits for one. The service connects to opts.Database, or to a new
// throwaway database when it is nil, through the MIKOSHI_DATABASE_*
// overrides, and to the gateway [StorageEnv] names through the
// MIKOSHI_STORAGE_* overrides, in the configured bucket unless opts.Env
// names another.
func Launch(t testing.TB, opts Options) *Service {
	t.Helper()
	db := opts.Database
	if db == nil {
		db = Database(t)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(processtest.FreePort(t)))
	s := &Service{addr: addr, client: webtest.NewClient("http://" + addr)}
	s.Process = processtest.Launch(t, environment(t, opts, addr, db.DSN, storageEndpoint(t))...)
	return s
}

// Ready waits until the service's liveness probe answers, failing the test
// with the captured output if the process exits or the failsafe elapses
// first. The server is alone in the top layer of the service's graph, so a
// live probe means every node beneath it started.
func (s *Service) Ready(t testing.TB) *Service {
	t.Helper()
	s.Await(t, "liveness", func() bool { return webtest.Live(s.URL()) })
	return s
}

func storageEndpoint(t testing.TB) string {
	t.Helper()
	endpoint := os.Getenv(StorageEnv)
	if endpoint == "" {
		t.Fatalf("%s is not set; run the suite through mise run integration", StorageEnv)
	}
	return endpoint
}

// Bucket returns a client of the bucket name on the gateway [StorageEnv]
// names, with the credentials the harness hands every service process, so
// a test can check what the service keeps there. It creates nothing.
func Bucket(t testing.TB, name string) storage.Client {
	t.Helper()
	cfg := storage.Config{
		Endpoint:  storageEndpoint(t),
		Container: name,
		Account:   storageAccount,
		Key:       storageKey,
	}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("finalize the bucket's storage config: %v", err)
	}
	client, err := s3.New(cfg)
	if err != nil {
		t.Fatalf("new s3 client: %v", err)
	}
	return client
}

func environment(t testing.TB, opts Options, addr, dsn, storageEndpoint string) []string {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse database DSN: %v", err)
	}
	password, _ := u.User.Password()
	if _, err := time.LoadLocation(ServiceZone); err != nil {
		t.Fatalf("load the service zone from the host's zone database: %v", err)
	}
	env := []string{
		"TZ=" + ServiceZone,
		"MIKOSHI_ENV=",
		"MIKOSHI_LOG_LEVEL=debug",
		"MIKOSHI_LOG_FORMAT=text",
		"MIKOSHI_SERVER_HOST=" + host,
		"MIKOSHI_SERVER_PORT=" + port,
		"MIKOSHI_DATABASE_HOST=" + u.Hostname(),
		"MIKOSHI_DATABASE_PORT=" + u.Port(),
		"MIKOSHI_DATABASE_NAME=" + u.Path[1:],
		"MIKOSHI_DATABASE_USER=" + u.User.Username(),
		"MIKOSHI_DATABASE_PASSWORD=" + password,
		"MIKOSHI_STORAGE_ENDPOINT=" + storageEndpoint,
		"MIKOSHI_STORAGE_ACCOUNT=" + storageAccount,
		"MIKOSHI_STORAGE_KEY=" + storageKey,
	}
	return append(env, opts.Env...)
}

// Addr is the service's address, host:port.
func (s *Service) Addr() string { return s.addr }

// URL is the service's base URL.
func (s *Service) URL() string { return "http://" + s.addr }

// Client returns the client bound to the service, one per process so its
// connection is reused across calls.
func (s *Service) Client() *webtest.Client { return s.client }
