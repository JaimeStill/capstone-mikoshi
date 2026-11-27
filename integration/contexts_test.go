//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
	"github.com/JaimeStill/capstone-mikoshi/integration"
)

func createContext(t *testing.T, c *webtest.Client, body string) contexts.Context {
	t.Helper()
	res := c.Post(t, "/api/contexts", body)
	created := webtest.Decode[contexts.Context](t, res, http.StatusCreated)
	if loc := res.Header.Get("Location"); loc != "/api/contexts/"+created.ID {
		t.Errorf("Location = %q, want /api/contexts/%s", loc, created.ID)
	}
	return created
}

func explicit(parent, name string) string {
	return `{"parent_id":"` + parent + `","name":"` + name + `","kind":"explicit"}`
}

func queryContext(parent, name, filter string) string {
	return `{"parent_id":"` + parent + `","name":"` + name + `","kind":"query","filter":` + filter + `}`
}

func TestContexts_RootReadableAtTheNilUUID(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	root := webtest.Decode[contexts.Context](t, c.Get(t, "/api/contexts/"+contexts.Root), http.StatusOK)
	if root.ID != contexts.Root || root.ParentID != nil || root.Kind != contexts.KindExplicit || root.Filter != nil || root.Name == "" {
		t.Errorf("root = %+v, want the explicit root at the nil UUID with no parent", root)
	}
}

func TestContexts_CreateAndReadBack(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()
	filter := `[{"field":"media_type","op":"eq","value":"application/pdf"},{"field":"size","op":"in","value":[1,2]},{"field":"title","op":"notnull"}]`

	before := time.Now().UTC().Add(-time.Minute)
	top := createContext(t, c, explicit(contexts.Root, "projects"))
	saved := createContext(t, c, queryContext(contexts.Root, "pdfs", filter))
	nested := createContext(t, c, explicit(top.ID, "alpha"))
	nestedQuery := createContext(t, c, queryContext(top.ID, "alpha pdfs", filter))

	cases := []struct {
		got    contexts.Context
		parent string
		name   string
		kind   contexts.Kind
	}{
		{top, contexts.Root, "projects", contexts.KindExplicit},
		{saved, contexts.Root, "pdfs", contexts.KindQuery},
		{nested, top.ID, "alpha", contexts.KindExplicit},
		{nestedQuery, top.ID, "alpha pdfs", contexts.KindQuery},
	}
	seen := map[string]bool{}
	for _, w := range cases {
		id, err := uuid.Parse(w.got.ID)
		if err != nil || id[6]>>4 != 7 || seen[w.got.ID] {
			t.Errorf("%s: id %q, want a fresh version-7 UUID", w.name, w.got.ID)
		}
		seen[w.got.ID] = true
		if w.got.ParentID == nil || *w.got.ParentID != w.parent || w.got.Name != w.name || w.got.Kind != w.kind {
			t.Errorf("created %+v, want %q under %s, %s", w.got, w.name, w.parent, w.kind)
		}
		if w.got.CreatedAt.Location() != time.UTC || w.got.CreatedAt.Before(before) {
			t.Errorf("%s: created_at %v, want a recent UTC time", w.name, w.got.CreatedAt)
		}
		if (w.kind == contexts.KindQuery) != (w.got.Filter != nil) {
			t.Errorf("%s: filter %+v for a %s context", w.name, w.got.Filter, w.kind)
		}

		res := c.Get(t, "/api/contexts/"+w.got.ID).Expect(t, http.StatusOK)
		var read contexts.Context
		res.JSON(t, &read)
		if !sameContext(t, read, w.got) {
			t.Errorf("read %+v, want %+v", read, w.got)
		}
		if w.kind == contexts.KindExplicit && !strings.Contains(string(res.Body), `"filter":null`) {
			t.Errorf("%s: body %s, want a null filter", w.name, res.Body)
		}
	}

	if raw, err := json.Marshal(saved.Filter); err != nil || string(raw) != filter {
		t.Errorf("filter = %s, %v, want %s", raw, err, filter)
	}
}

func sameContext(t *testing.T, a, b contexts.Context) bool {
	t.Helper()
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(ja) == string(jb)
}

func TestContexts_SiblingNameConflicts(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	first := createContext(t, c, explicit(contexts.Root, "shared"))
	p := c.Post(t, "/api/contexts", queryContext(contexts.Root, "shared", `[{"field":"a","op":"null"}]`)).Problem(t, http.StatusConflict)
	if p.Detail != contexts.ErrNameTaken.Error() {
		t.Errorf("detail = %q, want %q", p.Detail, contexts.ErrNameTaken.Error())
	}
	createContext(t, c, explicit(first.ID, "shared"))
}

func TestContexts_MissingParentNotFound(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	p := c.Post(t, "/api/contexts", explicit(uuid.NewV7().String(), "orphan")).Problem(t, http.StatusNotFound)
	if p.Detail != contexts.ErrParentNotFound.Error() {
		t.Errorf("detail = %q, want %q", p.Detail, contexts.ErrParentNotFound.Error())
	}
}

func TestContexts_ReadRefusesMalformedAndUnknownIDs(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	if p := c.Get(t, "/api/contexts/not-a-uuid").Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, "not-a-uuid") {
		t.Errorf("detail = %q, want the malformed id named", p.Detail)
	}
	unknown := "/api/contexts/" + uuid.NewV7().String()
	if p := c.Get(t, unknown).Problem(t, http.StatusNotFound); p.Instance != unknown {
		t.Errorf("instance = %q, want %s", p.Instance, unknown)
	}
}

func TestContexts_CreateUnderAQueryContextRefused(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	saved := createContext(t, c, queryContext(contexts.Root, "pdfs", `[{"field":"a","op":"eq","value":1}]`))
	for _, body := range []string{
		explicit(saved.ID, "under"),
		queryContext(saved.ID, "under", `[{"field":"a","op":"eq","value":1}]`),
	} {
		p := c.Post(t, "/api/contexts", body).Problem(t, http.StatusConflict)
		if p.Detail != contexts.ErrQueryParent.Error() {
			t.Errorf("detail = %q, want %q", p.Detail, contexts.ErrQueryParent.Error())
		}
	}
}

func TestContexts_MalformedInputRefused(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	cases := []struct {
		name, body, want string
	}{
		{"a malformed parent", explicit("x", "e"), "parent_id must be a UUID"},
		{"an empty name", explicit(contexts.Root, ""), "name must not be empty"},
		{"an unknown kind", `{"parent_id":"` + contexts.Root + `","name":"e","kind":"saved"}`, "kind must be"},
		{"an explicit context's filter", `{"parent_id":"` + contexts.Root + `","name":"e","kind":"explicit","filter":[{"field":"a","op":"null"}]}`, "an explicit context carries no filter"},
		{"a query context without a filter", `{"parent_id":"` + contexts.Root + `","name":"q","kind":"query"}`, "a query context requires a filter"},
		{"an empty filter", queryContext(contexts.Root, "q", `[]`), "filter must hold at least one condition"},
		{"an empty field", queryContext(contexts.Root, "q", `[{"field":"a","op":"null"},{"field":"","op":"eq","value":1}]`), "filter[1]: field must not be empty"},
		{"an unknown op", queryContext(contexts.Root, "q", `[{"field":"a","op":"between","value":1}]`), `filter[0]: op "between" is not one of`},
		{"in without a list", queryContext(contexts.Root, "q", `[{"field":"a","op":"in","value":1}]`), `filter[0]: op "in" takes a non-empty list`},
		{"null with a value", queryContext(contexts.Root, "q", `[{"field":"a","op":"null","value":1}]`), `filter[0]: op "null" takes no value`},
		{"eq without a value", queryContext(contexts.Root, "q", `[{"field":"a","op":"eq"}]`), `filter[0]: op "eq" takes exactly one value`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if p := c.Post(t, "/api/contexts", tc.body).Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, tc.want) {
				t.Errorf("detail = %q, want the refusal naming %q", p.Detail, tc.want)
			}
		})
	}
}

func TestContexts_SchemaHoldsTheTreeRules(t *testing.T) {
	db := integration.Database(t)
	integration.Start(t, integration.Options{Database: db})
	ctx := context.Background()

	refused := []struct {
		name, constraint, query string
	}{
		{"an explicit context with a filter", "cc_context_filter",
			`INSERT INTO context (id, parent_id, name, kind, filter) VALUES ('0190a5d1-7a2b-7c3d-8e4f-000000000001', '` + contexts.Root + `', 'e', 'explicit', '[{"field":"a","op":"null"}]')`},
		{"a query context without a filter", "cc_context_filter",
			`INSERT INTO context (id, parent_id, name, kind) VALUES ('0190a5d1-7a2b-7c3d-8e4f-000000000001', '` + contexts.Root + `', 'q', 'query')`},
		{"a query context with an empty filter", "cc_context_filter_list",
			`INSERT INTO context (id, parent_id, name, kind, filter) VALUES ('0190a5d1-7a2b-7c3d-8e4f-000000000001', '` + contexts.Root + `', 'q', 'query', '[]')`},
		{"a second parentless context", "cc_context_root",
			`INSERT INTO context (id, parent_id, name, kind) VALUES ('0190a5d1-7a2b-7c3d-8e4f-000000000001', NULL, 'root2', 'explicit')`},
		{"an unknown kind", "cc_context_kind",
			`INSERT INTO context (id, parent_id, name, kind) VALUES ('0190a5d1-7a2b-7c3d-8e4f-000000000001', '` + contexts.Root + `', 'e', 'saved')`},
		{"a sibling's name", "uq_context_sibling_name",
			`INSERT INTO context (id, parent_id, name, kind) VALUES ('0190a5d1-7a2b-7c3d-8e4f-000000000001', '` + contexts.Root + `', 'a', 'explicit'), ('0190a5d1-7a2b-7c3d-8e4f-000000000002', '` + contexts.Root + `', 'a', 'explicit')`},
	}
	for _, r := range refused {
		_, err := db.Pool.ExecContext(ctx, r.query)
		if want := `constraint "` + r.constraint + `"`; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s = %v, want it refused by %s", r.name, err, r.constraint)
		}
	}
}
