package app_test

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-core/graph"

	"github.com/JaimeStill/capstone-mikoshi/internal/app"
	"github.com/JaimeStill/capstone-mikoshi/internal/config/configtest"
)

const failsafe = 2 * time.Second

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRun_FailsStartupWithoutDatabaseOrBroker(t *testing.T) {
	buf, stderr := &syncBuffer{}, &syncBuffer{}
	a := app.New(configtest.Config(t), buf)
	fakeStorage(t, a)

	done := make(chan int, 1)
	go func() { done <- a.Run(context.Background(), stderr) }()

	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("Run = %d, want 1 on a failed startup", code)
		}
	case <-time.After(failsafe):
		t.Fatal("timed out waiting for Run to fail startup")
	}

	out := buf.String()
	failure := regexp.MustCompile(`msg="service failed" error="[^"]*startup: (?:[^"]*\\n)?(database|broker): `)
	if !failure.MatchString(out) {
		t.Errorf("log = %q, want a service failure naming the database's or the broker's startup", out)
	}
	if strings.Contains(out, "server ready") {
		t.Error("log carries a ready record despite the failed startup")
	}
	if got := stderr.String(); got != "" {
		t.Errorf("stderr = %q, want nothing: a startup failure is logged", got)
	}
}

func TestRun_TwicePanics(t *testing.T) {
	a := app.New(configtest.Config(t), io.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := a.Run(ctx, io.Discard); code != 0 {
		t.Fatalf("first Run = %d, want 0", code)
	}

	built := 0
	a.Graph().Observe(func(graph.Ref) { built++ })
	defer func() {
		if recover() == nil {
			t.Error("a second Run did not panic")
		}
		if built != 0 {
			t.Errorf("the second Run built %d nodes before panicking, want none", built)
		}
	}()
	_ = a.Run(ctx, io.Discard)
}
