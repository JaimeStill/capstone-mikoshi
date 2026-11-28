package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-messaging"
	"github.com/standards-lab/go-messaging/nats"
)

const (
	// EventStream is the JetStream stream every service process owns and
	// publishes Mikoshi's events to.
	EventStream = "MIKOSHI"
	// EventPrefix is the namespace of Mikoshi's event types, the subjects
	// [EventStream] captures under.
	EventPrefix = "mikoshi"
)

// Broker returns a started go-messaging NATS broker on the server
// [NATSEnv] names, bound to [EventStream] as a consumer, never its owner,
// so it writes nothing to the stream and its Start fails when the stream
// is missing or does not capture [EventPrefix]. The stream must exist: the
// service, its owner, creates it as it starts. The broker is shut down
// when the test ends.
func Broker(t testing.TB) *nats.Broker {
	t.Helper()
	cfg := nats.Config{URL: natsURL(t), Name: "integration-harness", Stream: EventStream, Prefix: EventPrefix}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("broker config: %v", err)
	}
	b, err := nats.New(cfg)
	if err != nil {
		t.Fatalf("broker: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), processtest.Failsafe)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("start the harness's broker on %s: %v", cfg.URL, err)
	}
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), processtest.Failsafe)
		defer cancel()
		_ = b.Shutdown(sctx)
	})
	return b
}

// Stream is a durable consumer of [EventStream], held by the harness, and
// the events it has received in their order.
type Stream struct {
	mu  sync.Mutex
	got []event.Event
}

// Subscribe binds [EventStream] as a consumer through [Broker] and returns
// once its durable consumer is created, so it receives every event of the
// given types the stream captures from then on, and none before. The
// durable is named for the test and the moment, so no two tests share
// one. A service must have started, so the stream exists. The consumer
// stops when the test ends, before the broker shuts down.
func Subscribe(t testing.TB, types ...string) *Stream {
	t.Helper()
	b := Broker(t)
	sub := messaging.Subscription{Name: durableName(t), Types: types, Start: messaging.StartNew}
	src, err := b.Subscribe(sub, func(_ context.Context, err error) { t.Logf("the harness's subscriber survived: %v", err) })
	if err != nil {
		t.Fatalf("subscribe %s: %v", sub.Name, err)
	}

	s := &Stream{}
	ctx, stop := context.WithCancel(context.Background())
	var rerr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		rerr = src.Receive(ctx, func(_ context.Context, e event.Event) error {
			s.mu.Lock()
			s.got = append(s.got, e)
			s.mu.Unlock()
			return nil
		})
	}()
	t.Cleanup(func() {
		stop()
		<-done
		if rerr != nil {
			t.Errorf("the harness's subscriber: %v", rerr)
		}
	})
	processtest.WaitFor(t, "the harness's subscriber ready", func() bool {
		select {
		case <-done:
			t.Fatalf("the harness's subscriber ended before it was ready: %v", rerr)
		default:
		}
		return src.Ready()
	})
	return s
}

func durableName(t testing.TB) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, t.Name())
	return fmt.Sprintf("it_%s_%d", name, time.Now().UnixNano())
}

// Received is the events received so far, in their order.
func (s *Stream) Received() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.Event(nil), s.got...)
}

// Await waits until the stream has received an event whose subject is
// subject, and returns every event received by then, failing the test if
// none arrives within the failsafe. The relay publishes in commit order,
// so an event committed before the awaited one has arrived by then.
func (s *Stream) Await(t testing.TB, subject string) []event.Event {
	t.Helper()
	var got []event.Event
	processtest.WaitFor(t, "the event about "+subject, func() bool {
		got = s.Received()
		for _, e := range got {
			if e.Subject == subject {
				return true
			}
		}
		return false
	})
	return got
}
