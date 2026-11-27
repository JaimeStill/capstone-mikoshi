//go:build integration

package integration_test

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/JaimeStill/capstone-mikoshi/integration"
)

func TestStorage_StartCreatesTheBucket(t *testing.T) {
	name := "mikoshi-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	bucket := integration.Bucket(t, name)
	if err := bucket.Probe(t.Context()); !errors.Is(err, storage.ErrContainerNotFound) {
		t.Fatalf("probe of %s before start = %v, want %v", name, err, storage.ErrContainerNotFound)
	}

	s := integration.Start(t, integration.Options{Env: []string{"MIKOSHI_STORAGE_CONTAINER=" + name}})

	if err := bucket.Probe(t.Context()); err != nil {
		t.Errorf("probe of %s after start = %v, want the bucket created", name, err)
	}
	ready := webtest.Decode[readiness](t, s.Client().Get(t, web.ReadyPath), http.StatusOK)
	var storageCheck *check
	for i, c := range ready.Checks {
		if c.Name == "storage" {
			storageCheck = &ready.Checks[i]
		}
	}
	if storageCheck == nil || !storageCheck.Ready {
		t.Errorf("readiness checks = %+v, want storage ready", ready.Checks)
	}
}
