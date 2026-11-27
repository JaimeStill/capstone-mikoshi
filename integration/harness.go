package integration

import (
	"net"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk/webtest"
	"github.com/standards-lab/sqlate/postgres/pgtest"
)

// DSNEnv names the environment variable that holds the admin DSN of the
// PostgreSQL server the suite makes its throwaway databases on; the
// integration task sets it to the integration stack on POSTGRES_PORT.
const DSNEnv = "MIKOSHI_INTEGRATION_DSN"

const databasePrefix = "mikoshi"

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
// overrides.
func Launch(t testing.TB, opts Options) *Service {
	t.Helper()
	db := opts.Database
	if db == nil {
		db = Database(t)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(processtest.FreePort(t)))
	s := &Service{addr: addr, client: webtest.NewClient("http://" + addr)}
	s.Process = processtest.Launch(t, environment(t, opts, addr, db.DSN)...)
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

func environment(t testing.TB, opts Options, addr, dsn string) []string {
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
