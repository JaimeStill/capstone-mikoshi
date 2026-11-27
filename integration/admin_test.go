//go:build integration

package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-database/admin"
	"github.com/standards-lab/go-web-sdk/webtest"
	"github.com/standards-lab/sqlate/postgres/pgvector"

	"github.com/JaimeStill/capstone-mikoshi/data"
	"github.com/JaimeStill/capstone-mikoshi/integration"
)

func TestAdmin_SchemaReportsTheSetCurrentAndPgvector(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()

	st := webtest.Decode[admin.Status](t, c.Get(t, "/admin/database/schema"), http.StatusOK)
	if !st.Ready || len(st.Sets) != 1 {
		t.Fatalf("schema = %+v, want ready with one set", st)
	}
	set := st.Sets[0]
	if head := len(data.Migrations().Migrations); set.Name != data.AppSet || set.Version != head || len(set.Pending) != 0 || set.Dirty {
		t.Errorf("set = %+v, want %s clean at version %d with nothing pending", set, data.AppSet, head)
	}
	if len(st.Extensions) != 1 {
		t.Fatalf("extensions = %+v, want pgvector's status alone", st.Extensions)
	}
	if ext := st.Extensions[0]; ext.Name != pgvector.Extension || ext.Installed == "" || ext.Installed != ext.Default || ext.Pending {
		t.Errorf("extension = %+v, want %s installed at the version the database offers", ext, pgvector.Extension)
	}

	c.Post(t, "/admin/database/schema/verify", nil).Expect(t, http.StatusOK)
}

func TestAdmin_DeclaresNoSeedStates(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()

	if states := webtest.Decode[[]string](t, c.Get(t, "/admin/database/states"), http.StatusOK); states == nil || len(states) != 0 {
		t.Errorf("states = %v, want an empty list", states)
	}
	p := c.Post(t, "/admin/database/seed", nil).Problem(t, http.StatusForbidden)
	if !strings.Contains(p.Detail, "disabled") {
		t.Errorf("seed detail = %q, want the disabled seed named", p.Detail)
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
