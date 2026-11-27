//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/standards-lab/go-web-sdk"
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

func childPage(t *testing.T, c *webtest.Client, path string) web.Page[contexts.Context] {
	t.Helper()
	return webtest.Decode[web.Page[contexts.Context]](t, c.Get(t, path), http.StatusOK)
}

func names(items []contexts.Context) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Name
	}
	return out
}

func TestContexts_ChildrenPagedByName(t *testing.T) {
	c := integration.Start(t, integration.Options{Env: []string{"MIKOSHI_READS_DEFAULT_SIZE=2", "MIKOSHI_READS_MAX_SIZE=3"}}).Client()

	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	for _, body := range []string{
		explicit(folder.ID, "c"), explicit(folder.ID, "a"),
		queryContext(folder.ID, "e", `[{"field":"a","op":"null"}]`),
		explicit(folder.ID, "b"), explicit(folder.ID, "d"),
	} {
		createContext(t, c, body)
	}
	under := createContext(t, c, explicit(folder.ID, "x"))
	createContext(t, c, explicit(under.ID, "grandchild"))
	path := "/api/contexts/" + folder.ID + "/children"

	first := childPage(t, c, path)
	if got := names(first.Items); !slices.Equal(got, []string{"a", "b"}) || first.Size != 2 || first.Page != 1 || first.Total == nil || *first.Total != 6 || !first.More {
		t.Errorf("first page = %v %+v, want [a b] of 6 at the default size 2", got, first)
	}
	for _, item := range first.Items {
		if item.ParentID == nil || *item.ParentID != folder.ID {
			t.Errorf("child %+v, want it under %s", item, folder.ID)
		}
	}
	if last := childPage(t, c, path+"?page=2&size=3"); !slices.Equal(names(last.Items), []string{"d", "e", "x"}) || last.More {
		t.Errorf("page 2 of size 3 = %v more=%v, want [d e x] and no further page", names(last.Items), last.More)
	}

	var walked []string
	next := path
	for range 4 {
		p := childPage(t, c, next)
		walked = append(walked, names(p.Items)...)
		if !p.More {
			break
		}
		next = path + "?cursor=" + p.Next
	}
	if !slices.Equal(walked, []string{"a", "b", "c", "d", "e", "x"}) {
		t.Errorf("walked by cursor = %v, want every child once, by name", walked)
	}

	refused := []struct{ query, want string }{
		{"?size=4", "must be at most 3"},
		{"?page=0", "must be an integer of at least 1"},
		{"?sort=-name", "take no sort or filter"},
		{"?kind=query", "take no sort or filter"},
		{"?cursor=forged", "cursor"},
	}
	for _, r := range refused {
		if p := c.Get(t, path+r.query).Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, r.want) {
			t.Errorf("%s: detail = %q, want it naming %q", r.query, p.Detail, r.want)
		}
	}
}

func TestContexts_ChildrenOfALeafIsAnEmptyPage(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	saved := createContext(t, c, queryContext(contexts.Root, "pdfs", `[{"field":"a","op":"null"}]`))
	res := c.Get(t, "/api/contexts/"+saved.ID+"/children").Expect(t, http.StatusOK)
	var p web.Page[contexts.Context]
	res.JSON(t, &p)
	if len(p.Items) != 0 || p.More || p.Total == nil || *p.Total != 0 || !strings.Contains(string(res.Body), `"items":[]`) {
		t.Errorf("children of a query context = %s, want an empty page", res.Body)
	}
}

func TestContexts_ChildrenRefusesMalformedAndUnknownParents(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	if p := c.Get(t, "/api/contexts/not-a-uuid/children").Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, "not-a-uuid") {
		t.Errorf("detail = %q, want the malformed id named", p.Detail)
	}
	unknown := "/api/contexts/" + uuid.NewV7().String() + "/children"
	if p := c.Get(t, unknown).Problem(t, http.StatusNotFound); p.Instance != unknown {
		t.Errorf("instance = %q, want %s", p.Instance, unknown)
	}
}

func TestContexts_RenameReadsBack(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	child := createContext(t, c, explicit(folder.ID, "draft"))
	renamed := webtest.Decode[contexts.Context](t, c.Put(t, "/api/contexts/"+child.ID, `{"name":"final"}`), http.StatusOK)
	if renamed.ID != child.ID || renamed.Name != "final" || renamed.ParentID == nil || *renamed.ParentID != folder.ID || !renamed.CreatedAt.Equal(child.CreatedAt) {
		t.Errorf("renamed = %+v, want %s named final under %s", renamed, child.ID, folder.ID)
	}
	read := webtest.Decode[contexts.Context](t, c.Get(t, "/api/contexts/"+child.ID), http.StatusOK)
	if !sameContext(t, read, renamed) {
		t.Errorf("read %+v, want %+v", read, renamed)
	}
	if got := names(childPage(t, c, "/api/contexts/"+folder.ID+"/children").Items); !slices.Equal(got, []string{"final"}) {
		t.Errorf("children = %v, want [final]", got)
	}

	webtest.Decode[contexts.Context](t, c.Put(t, "/api/contexts/"+child.ID, `{"name":"final"}`), http.StatusOK)
}

func TestContexts_RenameOntoASiblingsNameConflicts(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	createContext(t, c, explicit(contexts.Root, "taken"))
	sibling := createContext(t, c, explicit(contexts.Root, "free"))
	p := c.Put(t, "/api/contexts/"+sibling.ID, `{"name":"taken"}`).Problem(t, http.StatusConflict)
	if p.Detail != contexts.ErrNameTaken.Error() {
		t.Errorf("detail = %q, want %q", p.Detail, contexts.ErrNameTaken.Error())
	}
	if read := webtest.Decode[contexts.Context](t, c.Get(t, "/api/contexts/"+sibling.ID), http.StatusOK); read.Name != "free" {
		t.Errorf("refused rename left the name %q, want free", read.Name)
	}

	cousin := createContext(t, c, explicit(folder.ID, "other"))
	if got := webtest.Decode[contexts.Context](t, c.Put(t, "/api/contexts/"+cousin.ID, `{"name":"taken"}`), http.StatusOK); got.Name != "taken" {
		t.Errorf("rename under another parent = %+v, want it named taken", got)
	}
}

func TestContexts_RenameRefusals(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	child := createContext(t, c, explicit(contexts.Root, "child"))
	if p := c.Put(t, "/api/contexts/"+child.ID, `{"name":""}`).Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, "name must not be empty") {
		t.Errorf("detail = %q, want the empty name named", p.Detail)
	}
	if p := c.Put(t, "/api/contexts/"+contexts.Root, `{"name":"top"}`).Problem(t, http.StatusConflict); p.Detail != contexts.ErrRootFixed.Error() {
		t.Errorf("detail = %q, want %q", p.Detail, contexts.ErrRootFixed.Error())
	}
	if root := webtest.Decode[contexts.Context](t, c.Get(t, "/api/contexts/"+contexts.Root), http.StatusOK); root.Name != "root" {
		t.Errorf("root named %q after a refused rename, want root", root.Name)
	}
	unknown := "/api/contexts/" + uuid.NewV7().String()
	if p := c.Put(t, unknown, `{"name":"n"}`).Problem(t, http.StatusNotFound); p.Instance != unknown {
		t.Errorf("instance = %q, want %s", p.Instance, unknown)
	}
	if p := c.Put(t, "/api/contexts/not-a-uuid", `{"name":"n"}`).Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, "not-a-uuid") {
		t.Errorf("detail = %q, want the malformed id named", p.Detail)
	}
}
