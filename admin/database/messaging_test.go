package database_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"uuid"

	"github.com/standards-lab/go-core/reactor"
	godb "github.com/standards-lab/go-database"
	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-messaging/outbox"
	msgpg "github.com/standards-lab/go-messaging/postgres"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/migrate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/capstone-mikoshi/admin/database"
	"github.com/JaimeStill/capstone-mikoshi/correlation"
	"github.com/JaimeStill/capstone-mikoshi/data"
	"github.com/JaimeStill/capstone-mikoshi/internal/outboxtest"
)

const requestID = "req-0190a5d1"

func recorder(t *testing.T) *database.Recorder {
	t.Helper()
	ob, err := outbox.New(msgpg.Outbox())
	if err != nil {
		t.Fatal(err)
	}
	return outboxtest.Stamped(ob.Sink())
}

func TestOnReset_CommitsStateResetInItsOwnTransaction(t *testing.T) {
	pool, rec := sqltest.Open(t, sqltest.Response{Affected: 1})
	hook := database.OnReset(sqlate.Wrap(pool, dialect{}), recorder(t))

	if err := hook(web.WithRequestID(context.Background(), requestID), data.StateEmpty); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if ops := rec.Ops(); !slices.Equal(ops, []sqltest.Op{sqltest.OpBegin, sqltest.OpExec, sqltest.OpCommit}) {
		t.Fatalf("ops = %v; want the outbox write alone in one transaction", ops)
	}
	events := outboxtest.Committed(t, rec)
	if len(events) != 1 {
		t.Fatalf("events committed = %d; want one", len(events))
	}
	outboxtest.WantEnvelope(t, events[0], "mikoshi.database.reset", "empty", requestID)
	if string(events[0].Data) != `"empty"` {
		t.Errorf("data = %s; want the state's name, \"empty\"", events[0].Data)
	}
}

func TestOnReset_OutsideARequestCorrelatesByAFreshUUIDv7(t *testing.T) {
	pool, rec := sqltest.Open(t, sqltest.Response{Affected: 1})
	hook := database.OnReset(sqlate.Wrap(pool, dialect{}), recorder(t))

	if err := hook(context.Background(), data.StateEmpty); err != nil {
		t.Fatalf("hook: %v", err)
	}
	events := outboxtest.Committed(t, rec)
	if len(events) != 1 {
		t.Fatalf("events committed = %d; want one", len(events))
	}
	id := events[0].Extensions[correlation.Extension]
	u, err := uuid.Parse(id)
	if err != nil || u[6]>>4 != 7 {
		t.Errorf("correlationid = %q (%v); want a version-7 UUID", id, err)
	}
	outboxtest.WantEnvelope(t, events[0], database.StateReset.Type(), data.StateEmpty, id)
}

func TestOnReset_AFailedWriteFailsTheHook(t *testing.T) {
	boom := errors.New("scripted: the outbox write failed")
	pool, rec := sqltest.Open(t, sqltest.Response{Err: boom})
	hook := database.OnReset(sqlate.Wrap(pool, dialect{}), recorder(t))

	if err := hook(context.Background(), data.StateEmpty); !errors.Is(err, boom) {
		t.Fatalf("hook = %v; want the write's error", err)
	}
	if ops := rec.Ops(); !slices.Equal(ops, []sqltest.Op{sqltest.OpBegin, sqltest.OpExec, sqltest.OpRollback}) {
		t.Errorf("ops = %v; want the write rolled back", ops)
	}
	outboxtest.NoneCommitted(t, rec)
}

var oneTable = []migrate.Set{{Name: "app", Migrations: []migrate.Migration{
	{Version: 1, Name: "a", Up: "CREATE TABLE a (x int)", Down: "DROP TABLE a", Transactional: true},
}}}

var historyCols = []string{"version", "name", "dirty"}

func resetScript(write sqltest.Response) []sqltest.Response {
	return []sqltest.Response{
		{}, {Columns: historyCols, Rows: [][]driver.Value{{int64(1), "a", false}}}, {}, {}, {},
		{}, {Columns: historyCols}, {}, {},
		write,
	}
}

func resetModule(t *testing.T, responses ...sqltest.Response) (http.Handler, *sqltest.Recorder) {
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
	m, err := migrate.New(sdb, oneTable, migrate.Options{Unlocked: true})
	if err != nil {
		t.Fatal(err)
	}
	svc := admin.New(pdb, sdb, m, query.MustCatalog(query.Patterns()), admin.Options{
		Seeder: data.Seeder{}, OnReset: database.OnReset(sdb, recorder(t)),
	})
	r := web.NewRouter()
	r.Mount(web.NewModule(database.Routes(svc, new(reactor.Gate), slog.New(slog.DiscardHandler))))
	return r, rec
}

func TestState_ResetToEmptyCommitsStateReset(t *testing.T) {
	status := []sqltest.Response{count(1), {Columns: historyCols, Rows: [][]driver.Value{{int64(1), "a", false}}}}
	h, rec := resetModule(t, slices.Concat(resetScript(sqltest.Response{Affected: 1}), status)...)

	body := decode(t, send(t, h, "POST", "/database/state", `{"state":"empty","confirm":true}`), http.StatusOK)
	if body["state"] != "empty" {
		t.Errorf("state = %v; want empty", body["state"])
	}
	events := outboxtest.Committed(t, rec)
	if len(events) != 1 {
		t.Fatalf("events committed = %d; want the reset's one", len(events))
	}
	if e := events[0]; e.Type != database.StateReset.Type() || e.Subject != data.StateEmpty || string(e.Data) != `"empty"` {
		t.Errorf("event = %s about %q with %s; want StateReset about empty", e.Type, e.Subject, e.Data)
	}
}

func TestState_AFailedEventWriteFailsTheReset(t *testing.T) {
	h, rec := resetModule(t, resetScript(sqltest.Response{Err: errors.New("scripted: the outbox write failed")})...)

	if code := send(t, h, "POST", "/database/state", `{"state":"empty","confirm":true}`).Code; code != http.StatusInternalServerError {
		t.Errorf("reset = %d; want the failed write's 500", code)
	}
	outboxtest.NoneCommitted(t, rec)
}
