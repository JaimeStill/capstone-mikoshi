package outboxtest

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/capstone-mikoshi/correlation"
)

// Source, Layer, and ID are what a Stamped recorder stamps on every
// event: the service's source, the value of its layer extension, and an
// id fixed so a test can assert it.
const (
	Source = "mikoshi"
	Layer  = "mikoshi"
	ID     = "00000000-0000-7000-8000-0000000000ee"
)

// Time is the time a Stamped recorder stamps on every event.
var Time = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

// Stamped is the recorder the composition root injects, over sink, with
// the id and the time it stamps fixed to ID and Time.
func Stamped(sink event.Sink[*sqlate.Tx]) *event.Recorder[*sqlate.Tx] {
	return event.NewRecorder(sink, Source, event.Options{
		IDs:        func() string { return ID },
		Clock:      func() time.Time { return Time },
		Extensions: map[string]string{"layer": Layer},
	})
}

// IsWrite reports whether c is the outbox's insert of an event.
func IsWrite(c sqltest.Call) bool {
	return c.Op == sqltest.OpExec && strings.Contains(c.SQL, "messaging_outbox")
}

// Writes is every outbox insert rec recorded, in order, committed or not.
func Writes(rec *sqltest.Recorder) []sqltest.Call {
	var out []sqltest.Call
	for _, c := range rec.Calls() {
		if IsWrite(c) {
			out = append(out, c)
		}
	}
	return out
}

// Decode is the event the outbox insert c wrote: its parameters are the
// source, the id, the header as JSON, and the data, in that order.
func Decode(t testing.TB, c sqltest.Call) event.Event {
	t.Helper()
	if len(c.Args) != 4 {
		t.Fatalf("outbox insert args = %v; want source, id, header, data", c.Args)
	}
	var h event.Header
	if err := json.Unmarshal(bytesOf(t, c.Args[2]), &h); err != nil {
		t.Fatalf("decode the header: %v", err)
	}
	e, err := event.Decode(h, bytesOf(t, c.Args[3]))
	if err != nil {
		t.Fatalf("decode the event: %v", err)
	}
	return e
}

func bytesOf(t testing.TB, arg any) []byte {
	t.Helper()
	switch v := arg.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	case json.RawMessage:
		return v
	}
	t.Fatalf("argument %T is neither bytes nor a string", arg)
	return nil
}

// Committed is every event a committed transaction wrote, in commit order:
// an outbox insert counts once its transaction commits, and one a rollback
// undid never does.
func Committed(t testing.TB, rec *sqltest.Recorder) []event.Event {
	t.Helper()
	var out, open []event.Event
	for _, c := range rec.Calls() {
		switch {
		case IsWrite(c):
			open = append(open, Decode(t, c))
		case c.Op == sqltest.OpRollback:
			open = nil
		case c.Op == sqltest.OpCommit:
			out, open = append(out, open...), nil
		}
	}
	return out
}

// NoneCommitted fails the test if any event committed: a write its
// transaction rolled back is no event.
func NoneCommitted(t testing.TB, rec *sqltest.Recorder) {
	t.Helper()
	if events := Committed(t, rec); len(events) != 0 {
		t.Errorf("events committed = %d; want none (ops %v)", len(events), rec.Ops())
	}
}

// Want fails the test unless e is typ about subject, stamped by a Stamped
// recorder, correlated by correlationID, with JSON data exactly want:
// every field, and no other.
func Want(t testing.TB, e event.Event, typ, subject, correlationID string, want map[string]any) {
	t.Helper()
	WantEnvelope(t, e, typ, subject, correlationID)
	var got map[string]any
	if err := json.Unmarshal(e.Data, &got); err != nil {
		t.Fatalf("decode the data %s: %v", e.Data, err)
	}
	if len(got) != len(want) {
		t.Errorf("data = %v; want exactly %v", got, want)
	}
	for k, v := range want {
		w, ok := got[k]
		if !ok {
			t.Errorf("data[%q] absent; want %v", k, v)
			continue
		}
		gotJSON, _ := json.Marshal(w)
		wantJSON, _ := json.Marshal(v)
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("data[%q] = %s; want %s", k, gotJSON, wantJSON)
		}
	}
}

// WantEnvelope fails the test unless e is typ about subject, stamped by a
// Stamped recorder, with JSON data, whatever its shape, and with exactly
// two extensions: the layer, [Layer], and the correlation id,
// correlationID; so no causation id.
func WantEnvelope(t testing.TB, e event.Event, typ, subject, correlationID string) {
	t.Helper()
	if e.Type != typ || e.Subject != subject || e.Source != Source || e.ID != ID ||
		!e.Time.Equal(Time) || e.DataContentType != "application/json" {
		t.Errorf("event = %+v; want %s about %s, stamped by the recorder", e, typ, subject)
	}
	want := map[string]string{"layer": Layer, correlation.Extension: correlationID}
	if !maps.Equal(e.Extensions, want) {
		t.Errorf("extensions = %v; want exactly %v", e.Extensions, want)
	}
}
