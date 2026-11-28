//go:build integration

package integration_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
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
