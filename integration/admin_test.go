//go:build integration

package integration_test

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/event"
	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-database/admin"
	msgpg "github.com/standards-lab/go-messaging/postgres"
	"github.com/standards-lab/go-web-sdk/webtest"
	"github.com/standards-lab/sqlate/migrate"
	"github.com/standards-lab/sqlate/postgres/pgvector"

	dbadmin "github.com/JaimeStill/capstone-mikoshi/admin/database"
	"github.com/JaimeStill/capstone-mikoshi/data"
	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
	"github.com/JaimeStill/capstone-mikoshi/integration"
)

func TestAdmin_SchemaReportsTheSetsCurrentAndPgvector(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()

	st := webtest.Decode[admin.Status](t, c.Get(t, "/admin/database/schema"), http.StatusOK)
	if !st.Ready || len(st.Sets) != 2 {
		t.Fatalf("schema = %+v, want ready with the messaging set and Mikoshi's", st)
	}
	messagingSet, err := msgpg.Migrations()
	if err != nil {
		t.Fatalf("messaging migrations: %v", err)
	}
	for i, want := range []migrate.Set{messagingSet, data.Migrations()} {
		set := st.Sets[i]
		if head := len(want.Migrations); set.Name != want.Name || set.Version != head || len(set.Pending) != 0 || set.Dirty {
			t.Errorf("set %d = %+v, want %s clean at version %d with nothing pending", i, set, want.Name, head)
		}
	}
	if len(st.Extensions) != 1 {
		t.Fatalf("extensions = %+v, want pgvector's status alone", st.Extensions)
	}
	if ext := st.Extensions[0]; ext.Name != pgvector.Extension || ext.Installed == "" || ext.Installed != ext.Default || ext.Pending {
		t.Errorf("extension = %+v, want %s installed at the version the database offers", ext, pgvector.Extension)
	}

	c.Post(t, "/admin/database/schema/verify", nil).Expect(t, http.StatusOK)
}

func TestAdmin_DeclaresTheEmptyStateAlone(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()

	if states := webtest.Decode[[]string](t, c.Get(t, "/admin/database/states"), http.StatusOK); !slices.Equal(states, []string{data.StateEmpty}) {
		t.Errorf("states = %v, want [%s]", states, data.StateEmpty)
	}
	p := c.Post(t, "/admin/database/seed", nil).Problem(t, http.StatusForbidden)
	if !strings.Contains(p.Detail, "disabled") {
		t.Errorf("seed detail = %q, want the disabled seed named", p.Detail)
	}
	if seeded := webtest.Decode[admin.Seeded](t, c.Post(t, "/admin/database/seed", `{"state":"empty"}`), http.StatusOK); len(seeded) != 0 {
		t.Errorf("seeded = %v, want no rows", seeded)
	}
}

func correlated(e event.Event) string { return e.Extensions["correlationid"] }

func TestAdmin_ResetToEmptyEmptiesTheDatabaseAndPublishesTheReset(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()
	stream := integration.Subscribe(t)

	doomed := createContext(t, c, explicit(contexts.Root, "doomed"))

	res := c.Post(t, "/admin/database/state", `{"state":"empty","confirm":true}`)
	tr := webtest.Decode[admin.Transition](t, res, http.StatusOK)
	if tr.State != data.StateEmpty || !tr.Schema.Ready || len(tr.Seeded) != 0 {
		t.Errorf("transition = %+v, want empty, ready, nothing seeded", tr)
	}
	resetID := requestIDOf(t, res.Header)

	c.Get(t, "/api/contexts/"+doomed.ID).Expect(t, http.StatusNotFound)
	c.Get(t, "/api/contexts/"+contexts.Root).Expect(t, http.StatusOK)

	var reset event.Event
	processtest.WaitFor(t, "the reset's event", func() bool {
		for _, e := range stream.Received() {
			if e.Type == dbadmin.StateReset.Type() && correlated(e) == resetID {
				reset = e
				return true
			}
		}
		return false
	})
	want := map[string]string{"layer": "mikoshi", "correlationid": resetID}
	if reset.Subject != data.StateEmpty || reset.Source != "mikoshi" || !maps.Equal(reset.Extensions, want) || string(reset.Data) != `"empty"` {
		t.Errorf("reset event = %+v (data %s); want about empty from mikoshi, extensions exactly %v, data \"empty\"", reset, reset.Data, want)
	}

	verbs := map[string]bool{}
	for _, v := range []struct{ path, body string }{
		{"/admin/database/schema/verify", ""},
		{"/admin/database/schema/down", `{"set":"app","steps":1}`},
		{"/admin/database/schema/up", ""},
		{"/admin/database/schema/steps", `{"set":"app","steps":-1}`},
		{"/admin/database/schema/steps", `{"set":"app","steps":1}`},
		{"/admin/database/schema/force", fmt.Sprintf(`{"set":"app","version":%d}`, len(data.Migrations().Migrations))},
	} {
		verbs[requestIDOf(t, c.Post(t, v.path, v.body).Expect(t, http.StatusOK).Header)] = true
	}
	marker := createContext(t, c, explicit(contexts.Root, "marker"))
	for _, e := range stream.Await(t, marker.ID) {
		if verbs[correlated(e)] {
			t.Errorf("%s about %s published by a schema verb; want none", e.Type, e.Subject)
		}
	}
}

func TestAdmin_StatementsListEveryRegisteredDomain(t *testing.T) {
	s := integration.Start(t, integration.Options{})

	inv := webtest.Decode[admin.Inventory](t, s.Client().Get(t, "/admin/database/statements"), http.StatusOK)
	got := map[string]int{}
	for _, d := range inv.Domains {
		got[d.Name] = len(d.Statements)
	}
	if got["contexts"] == 0 || got["data"] == 0 || got["items"] == 0 || got["uploads"] == 0 || len(got) != 4 {
		t.Errorf("statements by domain = %v, want contexts', data's, items', and uploads' inventories", got)
	}
}
