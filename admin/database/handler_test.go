package database_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/reactor"
	godb "github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/capstone-mikoshi/admin/database"
	"github.com/JaimeStill/capstone-mikoshi/data"
)

type dialect struct{ sqltest.Dialect }

func (dialect) ServerVersion() string { return "SELECT version()" }

func module(t *testing.T, responses ...sqltest.Response) (http.Handler, *sqltest.Recorder) {
	t.Helper()
	return gatedModule(t, new(reactor.Gate), responses...)
}

func gatedModule(t *testing.T, gate database.SchemaGate, responses ...sqltest.Response) (http.Handler, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	cfg := godb.Config{Name: "app"}
	if err := cfg.Finalize(""); err != nil {
		t.Fatal(err)
	}
	pdb := godb.New(pool, cfg)
	if err := pdb.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	sdb := sqlate.Wrap(pool, dialect{})
	m, err := migrate.New(sdb, []migrate.Set{data.Migrations()}, migrate.Options{Unlocked: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := admin.New(pdb, sdb, m, query.MustCatalog(query.Patterns()), admin.Options{})
	r := web.NewRouter()
	r.Mount(web.NewModule(database.Routes(svc, gate, slog.New(slog.DiscardHandler))))
	return r, rec
}

func send(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, want, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func count(n int64) sqltest.Response {
	return sqltest.Response{Columns: []string{"count"}, Rows: [][]driver.Value{{n}}}
}

func TestReads_NeedNoDatabase(t *testing.T) {
	h, rec := module(t)

	patterns := decode(t, send(t, h, "GET", "/database/patterns", ""), 200)
	if ns, _ := patterns["namespaces"].([]any); len(ns) != 1 || ns[0] != query.Namespace {
		t.Errorf("namespaces = %v; want sqlate's alone", ns)
	}
	statements := decode(t, send(t, h, "GET", "/database/statements", ""), 200)
	if domains, ok := statements["domains"].([]any); !ok || len(domains) != 0 {
		t.Errorf("domains = %v; want an empty inventory", statements["domains"])
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Errorf("the reads touched the database: %v", rec.Ops())
	}
}

func TestSchema_ReportsAnEmptyHistoryAsPending(t *testing.T) {
	noHistoryTable := count(0)
	h, rec := module(t, noHistoryTable)

	st := decode(t, send(t, h, "GET", "/database/schema", ""), 200)
	sets, _ := st["sets"].([]any)
	if st["ready"] != false || len(sets) != 1 {
		t.Fatalf("status = %v; want not ready with one set", st)
	}
	app, _ := sets[0].(map[string]any)
	if app["name"] != data.AppSet || app["version"] != float64(0) {
		t.Errorf("set = %v; want the app set at version 0", app)
	}
	head := len(data.Migrations().Migrations)
	pending, _ := app["pending"].([]any)
	if len(pending) != head {
		t.Errorf("pending = %v; want every version, 1 through %d", pending, head)
	}
	for i, v := range pending {
		if v != float64(i+1) {
			t.Errorf("pending = %v; want every version, 1 through %d", pending, head)
		}
	}
	if rec.Pending() != 0 {
		t.Errorf("pending responses %d", rec.Pending())
	}
}

func TestStates_ListsNone(t *testing.T) {
	h, rec := module(t)
	res := send(t, h, "GET", "/database/states", "")
	var states []string
	if err := json.Unmarshal(res.Body.Bytes(), &states); err != nil || res.Code != 200 || states == nil || len(states) != 0 {
		t.Errorf("states = %d %s; want an empty list", res.Code, res.Body)
	}
	if len(rec.Calls()) != 0 {
		t.Errorf("the states read touched the database: %v", rec.Ops())
	}
}

func TestSeedAndState_AreForbiddenWithNoStates(t *testing.T) {
	h, rec := module(t)
	for _, c := range []struct{ path, body string }{
		{"/database/seed", ""},
		{"/database/seed", `{"state":"default"}`},
		{"/database/state", `{"state":"default","confirm":true}`},
		{"/database/state", `{"confirm":true}`},
	} {
		res := send(t, h, "POST", c.path, c.body)
		body := decode(t, res, 403)
		if ct := res.Header().Get("Content-Type"); ct != web.ProblemMediaType {
			t.Errorf("%s %s: Content-Type = %q", c.path, c.body, ct)
		}
		if detail, _ := body["detail"].(string); !strings.Contains(detail, "disabled") {
			t.Errorf("%s %s: detail = %q; want the reason carried on a 403", c.path, c.body, detail)
		}
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Errorf("a forbidden verb reached the database: %v", calls)
	}
}

func TestVerbs_RejectBadArgumentsBeforeIO(t *testing.T) {
	cases := map[string]struct {
		path, body, detail string
	}{
		"steps: zero":           {"/database/schema/steps", `{"set":"app","steps":0}`, "non-zero"},
		"steps: unknown key":    {"/database/schema/steps", `{"set":"app","step":1}`, "unknown field"},
		"steps: empty body":     {"/database/schema/steps", " ", "empty body"},
		"steps: no set":         {"/database/schema/steps", `{"steps":1}`, "set"},
		"steps: unknown set":    {"/database/schema/steps", `{"set":"nope","steps":1}`, "nope"},
		"down: negative":        {"/database/schema/down", `{"set":"app","steps":-1}`, "positive"},
		"down: missing body":    {"/database/schema/down", "", "empty body"},
		"down: unknown set":     {"/database/schema/down", `{"set":"nope"}`, "nope"},
		"force: negative":       {"/database/schema/force", `{"set":"app","version":-1}`, "is not in set"},
		"force: not in the set": {"/database/schema/force", `{"set":"app","version":999}`, "version 999 is not in set"},
		"force: missing body":   {"/database/schema/force", "", "empty body"},
		"force: unknown set":    {"/database/schema/force", `{"set":"nope","version":1}`, "nope"},
		"state: unconfirmed":    {"/database/state", `{"state":"default"}`, `"confirm": true`},
		"state: confirm false":  {"/database/state", `{"state":"default","confirm":false}`, `"confirm": true`},
		"state: missing body":   {"/database/state", "", "empty body"},
		"state: unknown key":    {"/database/state", `{"name":"empty","confirm":true}`, "unknown field"},
	}
	h, rec := module(t)
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			body := decode(t, send(t, h, "POST", c.path, c.body), 400)
			if detail, _ := body["detail"].(string); !strings.Contains(detail, c.detail) {
				t.Errorf("detail = %q; want it to contain %q", detail, c.detail)
			}
		})
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Errorf("rejections reached the database: %v", calls)
	}
}

func TestVerify_OnAnEmptyHistoryIsAConflictWithDetail(t *testing.T) {
	h, rec := module(t, count(0))
	body := decode(t, send(t, h, "POST", "/database/schema/verify", ""), 409)
	if detail, _ := body["detail"].(string); !strings.Contains(detail, "pending") {
		t.Errorf("detail = %q; want the pending versions named on a 409", detail)
	}
	if rec.Pending() != 0 {
		t.Errorf("pending responses %d", rec.Pending())
	}
}

func TestDiagnostics_PingsAndReadsTheServerVersion(t *testing.T) {
	h, rec := module(t, sqltest.Response{Columns: []string{"version"}, Rows: [][]driver.Value{{"PostgreSQL 19.0"}}})
	d := decode(t, send(t, h, "GET", "/database/diagnostics", ""), 200)
	if d["dialect"] != "test" || d["server_version"] != "PostgreSQL 19.0" {
		t.Errorf("diagnostics = %v", d)
	}
	if ns, _ := d["namespaces"].([]any); len(ns) != 1 {
		t.Errorf("namespaces = %v", ns)
	}
	if _, ok := d["pool"].(map[string]any); !ok {
		t.Errorf("pool counters missing: %v", d)
	}
	if got := rec.SQL(sqltest.OpQuery); len(got) != 1 || got[0] != "SELECT version()" {
		t.Errorf("queries = %v; want the dialect's version statement", got)
	}
}

func sendWithin(t *testing.T, h http.Handler, d time.Duration, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	r := httptest.NewRequestWithContext(ctx, "POST", path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func exclusiveWaiting(gate *reactor.Gate) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	release, err := gate.Shared(ctx)
	if err != nil {
		return true
	}
	release()
	return false
}

func TestUp_WaitsForTheSharedHolder(t *testing.T) {
	gate := new(reactor.Gate)
	turn, err := gate.Shared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h, rec := gatedModule(t, gate, sqltest.Response{Err: errors.New("scripted: the apply failed")})

	done := make(chan int, 1)
	go func() { done <- send(t, h, "POST", "/database/schema/up", "").Code }()
	deadline := time.Now().Add(2 * time.Second)
	for !exclusiveWaiting(gate) {
		select {
		case code := <-done:
			t.Fatalf("up answered %d under a shared holder, want it waiting", code)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("up never waited for the gate under a shared holder")
		}
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Fatalf("up reached the database under a shared holder: %v", calls)
	}

	turn()
	select {
	case code := <-done:
		if code != http.StatusInternalServerError {
			t.Errorf("up = %d, want the scripted failure's 500", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("up did not run once the holder released the gate")
	}
	if len(rec.Calls()) == 0 {
		t.Error("up reached no statement once the gate was free")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	next, err := gate.Shared(ctx)
	if err != nil {
		t.Fatalf("the next shared turn after up: %v; want the gate released", err)
	}
	next()
}

func TestVerbs_ChangingTheSchemaHoldTheGate(t *testing.T) {
	gate := new(reactor.Gate)
	turn, err := gate.Shared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer turn()
	h, rec := gatedModule(t, gate, count(0))

	for path, body := range map[string]string{
		"/database/schema/up":    "",
		"/database/schema/down":  `{"set":"app"}`,
		"/database/schema/steps": `{"set":"app","steps":1}`,
		"/database/state":        `{"state":"default","confirm":true}`,
	} {
		if code := sendWithin(t, h, 30*time.Millisecond, path, body).Code; code != http.StatusInternalServerError {
			t.Errorf("%s under a shared holder = %d, want the abandoned wait's 500", path, code)
		}
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Fatalf("a verb waiting on the gate reached the database: %v", calls)
	}

	decode(t, send(t, h, "POST", "/database/schema/verify", ""), http.StatusConflict)
	decode(t, send(t, h, "POST", "/database/schema/force", `{"set":"nope","version":1}`), http.StatusBadRequest)
	decode(t, send(t, h, "POST", "/database/seed", ""), http.StatusForbidden)
}
