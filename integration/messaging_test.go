//go:build integration

package integration_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/JaimeStill/capstone-mikoshi/integration"
)

func TestMessaging_ServiceProvisionsItsStream(t *testing.T) {
	s := integration.Start(t, integration.Options{})

	ready := webtest.Decode[readiness](t, s.Client().Get(t, web.ReadyPath), http.StatusOK)
	for _, want := range []check{{Name: "broker", Ready: true}, {Name: "relay", Ready: true}} {
		if !slices.Contains(ready.Checks, want) {
			t.Errorf("readiness checks = %+v, want %s ready", ready.Checks, want.Name)
		}
	}

	js, err := jetstream.New(integration.Broker(t).Conn())
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	stream, err := js.Stream(t.Context(), integration.EventStream)
	if err != nil {
		t.Fatalf("stream %s: %v", integration.EventStream, err)
	}
	cfg := stream.CachedInfo().Config
	if want := []string{integration.EventPrefix + ".>"}; !slices.Equal(cfg.Subjects, want) {
		t.Errorf("%s subjects = %v, want %v", integration.EventStream, cfg.Subjects, want)
	}
	if cfg.MaxAge != 168*time.Hour {
		t.Errorf("%s max_age = %s, want 168h", integration.EventStream, cfg.MaxAge)
	}
}

func requestIDOf(t *testing.T, h http.Header) string {
	t.Helper()
	id := h.Get("X-Request-Id")
	if id == "" {
		t.Fatal("the response carries no X-Request-Id")
	}
	return id
}

func wantEvent(t *testing.T, e event.Event, requestID string, data map[string]any) {
	t.Helper()
	want := map[string]string{"layer": "mikoshi", "correlationid": requestID}
	if e.Source != "mikoshi" || !maps.Equal(e.Extensions, want) {
		t.Errorf("%s: source %q, extensions %v; want mikoshi and exactly %v", e.Type, e.Source, e.Extensions, want)
	}
	var got map[string]any
	if err := json.Unmarshal(e.Data, &got); err != nil {
		t.Fatalf("%s: data %s: %v", e.Type, e.Data, err)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(data)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("%s: data = %s; want %s", e.Type, gotJSON, wantJSON)
	}
}
