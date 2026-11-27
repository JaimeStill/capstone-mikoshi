//go:build integration

package integration_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk"
	mw "github.com/standards-lab/go-web-sdk/middleware"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/JaimeStill/capstone-mikoshi/integration"
)

type check struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

type readiness struct {
	Status string  `json:"status"`
	Checks []check `json:"checks"`
}

var readyRecord = regexp.MustCompile(`msg="server ready" addr=(\S+)`)

func assertProbeHeaders(t *testing.T, path string, header http.Header) {
	t.Helper()
	if got := header.Get("Content-Type"); got != "application/json" {
		t.Errorf("GET %s Content-Type = %q, want application/json", path, got)
	}
	if got := header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("GET %s Cache-Control = %q, want no-store", path, got)
	}
	if header.Get(mw.RequestIDHeader) == "" {
		t.Errorf("GET %s carries no %s response header; RequestID is not wired", path, mw.RequestIDHeader)
	}
}

func TestLifecycle_ServesProbesThenDrains(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()

	c.Get(t, web.HealthPath).Expect(t, http.StatusOK)
	c.Get(t, web.ReadyPath).Expect(t, http.StatusOK)

	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s", code, s.Output())
	}
	out := s.Output()
	if !strings.Contains(out, `msg="server stopped"`) {
		t.Errorf("log carries no stop record after the drain:\n%s", out)
	}
	if !strings.Contains(out, "url.path="+web.HealthPath) {
		t.Errorf("log has no probe request record; the middleware stack is not wired:\n%s", out)
	}
}

func TestLifecycle_HealthzContract(t *testing.T) {
	s := integration.Start(t, integration.Options{})

	res := s.Client().Get(t, web.HealthPath).Expect(t, http.StatusOK)
	assertProbeHeaders(t, web.HealthPath, res.Header)
	if got := strings.TrimSpace(string(res.Body)); got != `{"status":"ok"}` {
		t.Errorf("GET %s body = %q, want {\"status\":\"ok\"}", web.HealthPath, got)
	}
}

func TestLifecycle_ReadyzContract(t *testing.T) {
	s := integration.Start(t, integration.Options{})

	res := s.Client().Get(t, web.ReadyPath).Expect(t, http.StatusOK)
	assertProbeHeaders(t, web.ReadyPath, res.Header)

	var got readiness
	dec := json.NewDecoder(strings.NewReader(string(res.Body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode %s body %q: %v", web.ReadyPath, res.Body, err)
	}
	if got.Status != "ready" {
		t.Errorf("readiness status = %q, want ready", got.Status)
	}
	want := []check{{Name: "lifecycle", Ready: true}, {Name: "database", Ready: true}, {Name: "storage", Ready: true}, {Name: "schema", Ready: true}}
	if !slices.Equal(got.Checks, want) {
		t.Errorf("readiness checks = %+v, want exactly %+v", got.Checks, want)
	}
}

func TestLifecycle_ReadyRecordNamesBoundAddr(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	s.Client().Get(t, web.ReadyPath).Expect(t, http.StatusOK)
	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s", code, s.Output())
	}

	records := readyRecord.FindAllStringSubmatch(s.Output(), -1)
	if len(records) != 1 {
		t.Fatalf("ready records = %d, want 1:\n%s", len(records), s.Output())
	}
	host, port, err := net.SplitHostPort(records[0][1])
	if err != nil {
		t.Fatalf("ready addr %q: %v", records[0][1], err)
	}
	if host != "127.0.0.1" {
		t.Errorf("ready addr host = %q, want the configured 127.0.0.1", host)
	}
	if p, err := strconv.Atoi(port); err != nil || p == 0 {
		t.Errorf("ready addr port = %q, want the bound nonzero port", port)
	}
	if records[0][1] != s.Addr() {
		t.Errorf("ready addr = %q, want the address the service serves on, %s", records[0][1], s.Addr())
	}
}

func TestLifecycle_TakenPortFailsStartup(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}
	defer func() { _ = held.Close() }()
	port := strconv.Itoa(held.Addr().(*net.TCPAddr).Port)

	s := integration.Launch(t, integration.Options{Env: []string{"MIKOSHI_SERVER_PORT=" + port}})
	if code := s.Wait(t); code != 1 {
		t.Errorf("exit = %d, want 1:\n%s", code, s.Output())
	}

	out := s.Output()
	failure := regexp.MustCompile(`msg="service failed" error="[^"]*startup:[^"]*server`)
	if !failure.MatchString(out) {
		t.Errorf("log = %q, want a startup error naming server", out)
	}
	if strings.Contains(out, "server ready") {
		t.Errorf("log = %q, want no ready record", out)
	}
}

func TestLifecycle_UnreachableDatabaseFailsStartup(t *testing.T) {
	closed := strconv.Itoa(processtest.FreePort(t))
	s := integration.Launch(t, integration.Options{Env: []string{"MIKOSHI_DATABASE_PORT=" + closed}})
	if code := s.Wait(t); code != 1 {
		t.Errorf("exit = %d, want 1:\n%s", code, s.Output())
	}

	out := s.Output()
	failure := regexp.MustCompile(`msg="service failed" error="[^"]*startup: database`)
	if !failure.MatchString(out) {
		t.Errorf("log = %q, want a startup error naming the database node", out)
	}
	if strings.Contains(out, "server ready") {
		t.Errorf("log = %q, want no ready record", out)
	}
}

func TestLifecycle_ShutdownBoundedByTimeout(t *testing.T) {
	const timeout = 200 * time.Millisecond
	const margin = 500 * time.Millisecond

	s := integration.Start(t, integration.Options{Env: []string{"MIKOSHI_SHUTDOWN_TIMEOUT=" + timeout.String()}})

	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatalf("dial %s: %v", s.Addr(), err)
	}
	defer func() { _ = conn.Close() }()
	unterminatedHeaders := "GET " + web.HealthPath + " HTTP/1.1\r\nHost: stall\r\n"
	if _, err := io.WriteString(conn, unterminatedHeaders); err != nil {
		t.Fatalf("write partial request: %v", err)
	}
	acceptedAfterTheStall := webtest.NewClient(s.URL())
	acceptedAfterTheStall.Get(t, web.HealthPath).Expect(t, http.StatusOK)

	began := time.Now()
	code := s.Stop(t)
	elapsed := time.Since(began)

	if code != 1 {
		t.Errorf("exit = %d, want 1 when the drain outlives the timeout:\n%s", code, s.Output())
	}
	if elapsed > timeout+margin {
		t.Errorf("exit after %s, want within %s of the %s timeout", elapsed, margin, timeout)
	}
}

func TestLifecycle_TwoInstances(t *testing.T) {
	db := integration.Database(t)
	a := integration.Launch(t, integration.Options{Database: db})
	b := integration.Launch(t, integration.Options{Database: db})
	a.Ready(t)
	b.Ready(t)
	for name, s := range map[string]*integration.Service{"a": a, "b": b} {
		ready := webtest.Decode[readiness](t, s.Client().Get(t, web.ReadyPath), http.StatusOK)
		if ready.Status != "ready" {
			t.Errorf("%s readyz status = %q, want ready", name, ready.Status)
		}
	}
	if code := a.Stop(t); code != 0 {
		t.Errorf("a exit = %d:\n%s", code, a.Output())
	}
	if code := b.Stop(t); code != 0 {
		t.Errorf("b exit = %d:\n%s", code, b.Output())
	}
}

func TestLifecycle_LocalOverlayLoads(t *testing.T) {
	s := integration.Start(t, integration.Options{Env: []string{"MIKOSHI_ENV=local"}})
	if code := s.Stop(t); code != 0 {
		t.Fatalf("exit = %d, want 0:\n%s", code, s.Output())
	}
}
