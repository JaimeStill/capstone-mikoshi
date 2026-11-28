//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/standards-lab/go-core/process/processtest"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/webtest"
	"github.com/standards-lab/sqlate/postgres/pgtest"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/capstone-mikoshi/data"
	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
	"github.com/JaimeStill/capstone-mikoshi/domain/items"
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
	filter := `[{"field":"kind","op":"eq","value":"pdf"},{"field":"duration","op":"in","value":[1,2]},{"field":"route","op":"notnull"}]`

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
	p := c.Post(t, "/api/contexts", queryContext(contexts.Root, "shared", `[{"field":"kind","op":"null"}]`)).Problem(t, http.StatusConflict)
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

	saved := createContext(t, c, queryContext(contexts.Root, "pdfs", `[{"field":"duration","op":"eq","value":1}]`))
	for _, body := range []string{
		explicit(saved.ID, "under"),
		queryContext(saved.ID, "under", `[{"field":"duration","op":"eq","value":1}]`),
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
		{"an explicit context's filter", `{"parent_id":"` + contexts.Root + `","name":"e","kind":"explicit","filter":[{"field":"kind","op":"null"}]}`, "an explicit context carries no filter"},
		{"a query context without a filter", `{"parent_id":"` + contexts.Root + `","name":"q","kind":"query"}`, "a query context requires a filter"},
		{"an empty filter", queryContext(contexts.Root, "q", `[]`), "filter must hold at least one condition"},
		{"an empty field", queryContext(contexts.Root, "q", `[{"field":"kind","op":"null"},{"field":"","op":"eq","value":1}]`), "filter[1]: field must not be empty"},
		{"an unknown op", queryContext(contexts.Root, "q", `[{"field":"a","op":"between","value":1}]`), `filter[0]: op "between" is not one of`},
		{"in without a list", queryContext(contexts.Root, "q", `[{"field":"a","op":"in","value":1}]`), `filter[0]: op "in" takes a non-empty list`},
		{"null with a value", queryContext(contexts.Root, "q", `[{"field":"a","op":"null","value":1}]`), `filter[0]: op "null" takes no value`},
		{"eq without a value", queryContext(contexts.Root, "q", `[{"field":"a","op":"eq"}]`), `filter[0]: op "eq" takes exactly one value`},
		{"a field the item read does not declare", queryContext(contexts.Root, "q", `[{"field":"route","op":"eq","value":"video"},{"field":"media_type","op":"eq","value":"video/mp4"}]`), `filter[1]: query: unknown filter field "media_type"`},
		{"the item read's key", queryContext(contexts.Root, "q", `[{"field":"id","op":"notnull"}]`), `filter[0]: query: unknown filter field "id"`},
		{"a non-number duration", queryContext(contexts.Root, "q", `[{"field":"duration","op":"gt","value":"120"}]`), `filter[0]: query: invalid value for "duration"`},
		{"a non-string route", queryContext(contexts.Root, "q", `[{"field":"route","op":"in","value":["video",1]}]`), `filter[0]: query: invalid value for "route"`},
		{"like on duration", queryContext(contexts.Root, "q", `[{"field":"duration","op":"like","value":"1%"}]`), `filter[0]: query: invalid value for "duration"`},
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
			`INSERT INTO context (id, parent_id, name, kind, filter) VALUES ('0190a5d1-7a2b-7c3d-8e4f-000000000001', '` + contexts.Root + `', 'e', 'explicit', '[{"field":"kind","op":"null"}]')`},
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
		queryContext(folder.ID, "e", `[{"field":"kind","op":"null"}]`),
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

	saved := createContext(t, c, queryContext(contexts.Root, "pdfs", `[{"field":"kind","op":"null"}]`))
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

func domainService(t *testing.T) (*contexts.Service, *pgtest.Database, *webtest.Client) {
	t.Helper()
	db := integration.Database(t)
	c := integration.Start(t, integration.Options{Database: db}).Client()
	d := data.New(db.Session(), query.MustCatalog(query.Patterns()))
	return contexts.New(d, items.FilterFields(d), integration.Recorder(t)), db, c
}

func move(t *testing.T, c *webtest.Client, id, body string) contexts.Context {
	t.Helper()
	return webtest.Decode[contexts.Context](t, c.Post(t, "/api/contexts/"+id+"/move", body), http.StatusOK)
}

func moveTo(parent string) string { return `{"parent_id":"` + parent + `"}` }

func subtree(t *testing.T, s *contexts.Service, id string) map[string]contexts.Kind {
	t.Helper()
	nodes, err := s.Subtree(t.Context(), id)
	if err != nil {
		t.Fatalf("Subtree(%s) = %v", id, err)
	}
	out := make(map[string]contexts.Kind, len(nodes))
	for _, n := range nodes {
		if _, seen := out[n.ID]; seen {
			t.Errorf("Subtree(%s) holds %s twice", id, n.ID)
		}
		out[n.ID] = n.Kind
	}
	return out
}

func TestContexts_MoveCarriesTheSubtree(t *testing.T) {
	s, _, c := domainService(t)

	from := createContext(t, c, explicit(contexts.Root, "from"))
	to := createContext(t, c, explicit(contexts.Root, "to"))
	moved := createContext(t, c, explicit(from.ID, "moved"))
	grandchild := createContext(t, c, explicit(moved.ID, "grandchild"))
	leaf := createContext(t, c, queryContext(moved.ID, "leaf", `[{"field":"kind","op":"null"}]`))
	deep := createContext(t, c, explicit(grandchild.ID, "deep"))

	got := move(t, c, moved.ID, moveTo(to.ID))
	if got.ID != moved.ID || got.Name != "moved" || got.ParentID == nil || *got.ParentID != to.ID || !got.CreatedAt.Equal(moved.CreatedAt) {
		t.Errorf("moved = %+v, want %s named moved under %s", got, moved.ID, to.ID)
	}
	if read := webtest.Decode[contexts.Context](t, c.Get(t, "/api/contexts/"+moved.ID), http.StatusOK); !sameContext(t, read, got) {
		t.Errorf("read %+v, want %+v", read, got)
	}

	if names := names(childPage(t, c, "/api/contexts/"+from.ID+"/children").Items); len(names) != 0 {
		t.Errorf("children of the old parent = %v, want none", names)
	}
	if names := names(childPage(t, c, "/api/contexts/"+to.ID+"/children").Items); !slices.Equal(names, []string{"moved"}) {
		t.Errorf("children of the new parent = %v, want [moved]", names)
	}
	if names := names(childPage(t, c, "/api/contexts/"+moved.ID+"/children").Items); !slices.Equal(names, []string{"grandchild", "leaf"}) {
		t.Errorf("children of the moved context = %v, want [grandchild leaf]", names)
	}
	under := childPage(t, c, "/api/contexts/"+grandchild.ID+"/children").Items
	if len(under) != 1 || under[0].ID != deep.ID || *under[0].ParentID != grandchild.ID {
		t.Errorf("children of the grandchild = %+v, want deep under it", under)
	}

	want := map[string]contexts.Kind{
		to.ID: contexts.KindExplicit, moved.ID: contexts.KindExplicit, grandchild.ID: contexts.KindExplicit,
		leaf.ID: contexts.KindQuery, deep.ID: contexts.KindExplicit,
	}
	if got := subtree(t, s, to.ID); !maps.Equal(got, want) {
		t.Errorf("subtree of the new parent = %v, want %v", got, want)
	}
	if got := subtree(t, s, grandchild.ID); !maps.Equal(got, map[string]contexts.Kind{grandchild.ID: contexts.KindExplicit, deep.ID: contexts.KindExplicit}) {
		t.Errorf("subtree of the grandchild = %v, want it and deep", got)
	}
	if got := subtree(t, s, from.ID); !maps.Equal(got, map[string]contexts.Kind{from.ID: contexts.KindExplicit}) {
		t.Errorf("subtree of the old parent = %v, want it alone", got)
	}
}

func TestContexts_MoveWithRename(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	to := createContext(t, c, explicit(contexts.Root, "to"))
	createContext(t, c, explicit(to.ID, "draft"))
	draft := createContext(t, c, explicit(contexts.Root, "draft"))

	got := move(t, c, draft.ID, `{"parent_id":"`+to.ID+`","name":"final"}`)
	if got.Name != "final" || got.ParentID == nil || *got.ParentID != to.ID {
		t.Errorf("moved = %+v, want final under %s", got, to.ID)
	}
	if names := names(childPage(t, c, "/api/contexts/"+to.ID+"/children").Items); !slices.Equal(names, []string{"draft", "final"}) {
		t.Errorf("children = %v, want [draft final]", names)
	}

	back := move(t, c, draft.ID, `{"parent_id":"`+contexts.Root+`","name":null}`)
	if back.Name != "final" || *back.ParentID != contexts.Root {
		t.Errorf("moved back = %+v, want final under the root, its name kept", back)
	}
}

func TestContexts_MoveRefusals(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	child := createContext(t, c, explicit(folder.ID, "child"))
	grandchild := createContext(t, c, explicit(child.ID, "grandchild"))
	saved := createContext(t, c, queryContext(contexts.Root, "saved", `[{"field":"kind","op":"null"}]`))
	createContext(t, c, explicit(contexts.Root, "taken"))
	missing := uuid.NewV7().String()

	conflicts := []struct {
		name, id, body string
		want           error
	}{
		{"under itself", folder.ID, moveTo(folder.ID), contexts.ErrCycle},
		{"under its child", folder.ID, moveTo(child.ID), contexts.ErrCycle},
		{"under its grandchild", folder.ID, moveTo(grandchild.ID), contexts.ErrCycle},
		{"under a query context", child.ID, moveTo(saved.ID), contexts.ErrQueryParent},
		{"the root", contexts.Root, moveTo(folder.ID), contexts.ErrRootFixed},
		{"onto a sibling's name", child.ID, `{"parent_id":"` + contexts.Root + `","name":"taken"}`, contexts.ErrNameTaken},
	}
	for _, r := range conflicts {
		t.Run(r.name, func(t *testing.T) {
			if p := c.Post(t, "/api/contexts/"+r.id+"/move", r.body).Problem(t, http.StatusConflict); p.Detail != r.want.Error() {
				t.Errorf("detail = %q, want %q", p.Detail, r.want.Error())
			}
		})
	}

	if p := c.Post(t, "/api/contexts/"+child.ID+"/move", moveTo(missing)).Problem(t, http.StatusNotFound); p.Detail != contexts.ErrParentNotFound.Error() {
		t.Errorf("missing parent: detail = %q, want %q", p.Detail, contexts.ErrParentNotFound.Error())
	}
	unknown := "/api/contexts/" + missing + "/move"
	if p := c.Post(t, unknown, moveTo(folder.ID)).Problem(t, http.StatusNotFound); p.Instance != unknown {
		t.Errorf("unknown context: instance = %q, want %s", p.Instance, unknown)
	}

	invalid := []struct{ name, id, body, want string }{
		{"a malformed id", "not-a-uuid", moveTo(folder.ID), "not-a-uuid"},
		{"a malformed parent", child.ID, moveTo("x"), "parent_id must be a UUID"},
		{"no parent", child.ID, `{"name":"n"}`, "parent_id must be a UUID"},
		{"an empty name", child.ID, `{"parent_id":"` + contexts.Root + `","name":""}`, "name must not be empty"},
	}
	for _, r := range invalid {
		if p := c.Post(t, "/api/contexts/"+r.id+"/move", r.body).Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, r.want) {
			t.Errorf("%s: detail = %q, want it naming %q", r.name, p.Detail, r.want)
		}
	}

	for _, w := range []struct{ ctx, parent, name string }{
		{folder.ID, contexts.Root, "folder"}, {child.ID, folder.ID, "child"}, {grandchild.ID, child.ID, "grandchild"},
	} {
		read := webtest.Decode[contexts.Context](t, c.Get(t, "/api/contexts/"+w.ctx), http.StatusOK)
		if read.ParentID == nil || *read.ParentID != w.parent || read.Name != w.name {
			t.Errorf("after the refusals %+v, want %s under %s", read, w.name, w.parent)
		}
	}
}

func TestContexts_SubtreeResolution(t *testing.T) {
	s, _, c := domainService(t)

	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	leaf := createContext(t, c, explicit(folder.ID, "leaf"))
	saved := createContext(t, c, queryContext(folder.ID, "saved", `[{"field":"kind","op":"null"}]`))

	if got := subtree(t, s, leaf.ID); !maps.Equal(got, map[string]contexts.Kind{leaf.ID: contexts.KindExplicit}) {
		t.Errorf("subtree of an explicit leaf = %v, want it alone", got)
	}
	if got := subtree(t, s, saved.ID); !maps.Equal(got, map[string]contexts.Kind{saved.ID: contexts.KindQuery}) {
		t.Errorf("subtree of a query context = %v, want it alone, a query", got)
	}
	want := map[string]contexts.Kind{folder.ID: contexts.KindExplicit, leaf.ID: contexts.KindExplicit, saved.ID: contexts.KindQuery}
	if got := subtree(t, s, folder.ID); !maps.Equal(got, want) {
		t.Errorf("subtree of the folder = %v, want %v", got, want)
	}
	if _, err := s.Subtree(t.Context(), uuid.NewV7().String()); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Subtree of an unknown context = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.Subtree(t.Context(), "x"); !errors.Is(err, contexts.ErrValidation) {
		t.Errorf("Subtree of a malformed id = %v, want ErrValidation", err)
	}
}

func TestContexts_WalksTerminateOnACycle(t *testing.T) {
	s, db, c := domainService(t)
	ctx, cancel := context.WithTimeout(t.Context(), processtest.Failsafe)
	defer cancel()

	a := createContext(t, c, explicit(contexts.Root, "a"))
	b := createContext(t, c, explicit(a.ID, "b"))
	outside := createContext(t, c, explicit(contexts.Root, "outside"))
	if _, err := db.Pool.ExecContext(ctx, `UPDATE context SET parent_id = $1 WHERE id = $2`, b.ID, a.ID); err != nil {
		t.Fatalf("force the cycle: %v", err)
	}

	nodes, err := s.Subtree(ctx, a.ID)
	if err != nil {
		t.Fatalf("descendant walk on a cycle = %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("descendant walk on a cycle = %+v, want a and b once each", nodes)
	}

	if _, err := s.Move(ctx, a.ID, contexts.MoveContext{ParentID: b.ID}); !errors.Is(err, contexts.ErrCycle) {
		t.Errorf("ancestor walk on a cycle, a under b = %v, want ErrCycle", err)
	}
	got, err := s.Move(ctx, outside.ID, contexts.MoveContext{ParentID: a.ID})
	if err != nil || got.ParentID == nil || *got.ParentID != a.ID {
		t.Errorf("ancestor walk on a cycle, outside under a = %+v, %v, want it moved", got, err)
	}
}

func TestContexts_OpposingMovesCommitOne(t *testing.T) {
	s, db, _ := domainService(t)
	ctx := t.Context()

	const pairs = 100
	for i := range pairs {
		a, err := s.Create(ctx, contexts.CreateContext{ParentID: contexts.Root, Name: fmt.Sprintf("a%d", i), Kind: contexts.KindExplicit})
		if err != nil {
			t.Fatal(err)
		}
		b, err := s.Create(ctx, contexts.CreateContext{ParentID: contexts.Root, Name: fmt.Sprintf("b%d", i), Kind: contexts.KindExplicit})
		if err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for j, m := range [][2]string{{a.ID, b.ID}, {b.ID, a.ID}} {
			wg.Go(func() {
				<-start
				_, errs[j] = s.Move(ctx, m[0], contexts.MoveContext{ParentID: m[1]})
			})
		}
		close(start)
		wg.Wait()

		committed, cycles := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				committed++
			case errors.Is(err, contexts.ErrCycle):
				cycles++
			default:
				t.Errorf("pair %d: move = %v, want it committed or ErrCycle", i, err)
			}
		}
		if committed != 1 || cycles != 1 {
			t.Errorf("pair %d: %d committed and %d cycles, want one of each", i, committed, cycles)
		}
	}

	var rows int
	if err := db.Pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM context`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if reached := subtree(t, s, contexts.Root); len(reached) != rows {
		t.Errorf("the walk from the root reaches %d of %d contexts, want every one: a move made a cycle", len(reached), rows)
	}
}

func descendants(t *testing.T, p web.Problem) int64 {
	t.Helper()
	if p.Detail != contexts.ErrUnconfirmed.Error() {
		t.Errorf("detail = %q, want %q", p.Detail, contexts.ErrUnconfirmed.Error())
	}
	n, ok := p.Extras["descendants"].(float64)
	if !ok {
		t.Fatalf("extras = %v, want a numeric descendants member", p.Extras)
	}
	return int64(n)
}

func gone(t *testing.T, c *webtest.Client, ids ...string) {
	t.Helper()
	for _, id := range ids {
		path := "/api/contexts/" + id
		if p := c.Get(t, path).Problem(t, http.StatusNotFound); p.Instance != path {
			t.Errorf("instance = %q, want %s", p.Instance, path)
		}
	}
}

func present(t *testing.T, c *webtest.Client, ids ...string) {
	t.Helper()
	for _, id := range ids {
		c.Get(t, "/api/contexts/"+id).Expect(t, http.StatusOK)
	}
}

func TestContexts_DeleteALeafOnTheFirstRequest(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	leaf := createContext(t, c, explicit(folder.ID, "leaf"))
	saved := createContext(t, c, queryContext(folder.ID, "saved", `[{"field":"kind","op":"null"}]`))
	kept := createContext(t, c, explicit(folder.ID, "kept"))

	if res := c.Delete(t, "/api/contexts/"+leaf.ID).Expect(t, http.StatusNoContent); len(res.Body) != 0 {
		t.Errorf("body = %s, want none", res.Body)
	}
	c.Delete(t, "/api/contexts/"+saved.ID+"?confirm=0").Expect(t, http.StatusNoContent)
	gone(t, c, leaf.ID, saved.ID)
	if got := names(childPage(t, c, "/api/contexts/"+folder.ID+"/children").Items); !slices.Equal(got, []string{"kept"}) {
		t.Errorf("children = %v, want [kept]", got)
	}

	if n := descendants(t, c.Delete(t, "/api/contexts/"+kept.ID+"?confirm=2").Problem(t, http.StatusConflict)); n != 0 {
		t.Errorf("descendants = %d, want 0 for a leaf", n)
	}
	present(t, c, kept.ID)
}

func TestContexts_DeleteASubtreeOnConfirmation(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	child := createContext(t, c, explicit(folder.ID, "child"))
	saved := createContext(t, c, queryContext(folder.ID, "saved", `[{"field":"kind","op":"null"}]`))
	grandchild := createContext(t, c, explicit(child.ID, "grandchild"))
	deep := createContext(t, c, explicit(grandchild.ID, "deep"))
	sibling := createContext(t, c, explicit(contexts.Root, "sibling"))
	all := []string{folder.ID, child.ID, saved.ID, grandchild.ID, deep.ID}
	path := "/api/contexts/" + folder.ID

	for _, query := range []string{"", "?confirm=0", "?confirm=3", "?confirm=5"} {
		p := c.Delete(t, path+query).Problem(t, http.StatusConflict)
		if n := descendants(t, p); n != 4 {
			t.Errorf("%q: descendants = %d, want 4", query, n)
		}
		if p.Instance != path {
			t.Errorf("instance = %q, want %s", p.Instance, path)
		}
	}
	present(t, c, all...)

	c.Delete(t, path+"?confirm=4").Expect(t, http.StatusNoContent)
	gone(t, c, all...)
	present(t, c, sibling.ID)
	if got := names(childPage(t, c, "/api/contexts/"+contexts.Root+"/children").Items); !slices.Equal(got, []string{"sibling"}) {
		t.Errorf("children of the root = %v, want [sibling]", got)
	}
}

func TestContexts_DeleteWithAStaleCountConflicts(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	child := createContext(t, c, explicit(folder.ID, "child"))
	path := "/api/contexts/" + folder.ID

	if n := descendants(t, c.Delete(t, path).Problem(t, http.StatusConflict)); n != 1 {
		t.Fatalf("descendants = %d, want 1", n)
	}
	added := createContext(t, c, explicit(child.ID, "added"))
	if n := descendants(t, c.Delete(t, path+"?confirm=1").Problem(t, http.StatusConflict)); n != 2 {
		t.Errorf("descendants after the addition = %d, want 2", n)
	}
	present(t, c, folder.ID, child.ID, added.ID)

	c.Delete(t, path+"?confirm=2").Expect(t, http.StatusNoContent)
	gone(t, c, folder.ID, child.ID, added.ID)
}

func TestContexts_DeleteRefusals(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()

	child := createContext(t, c, explicit(contexts.Root, "child"))
	path := "/api/contexts/" + child.ID

	for _, query := range []string{"", "?confirm=0", "?confirm=1"} {
		if p := c.Delete(t, "/api/contexts/"+contexts.Root+query).Problem(t, http.StatusConflict); p.Detail != contexts.ErrRootFixed.Error() {
			t.Errorf("root%s: detail = %q, want %q", query, p.Detail, contexts.ErrRootFixed.Error())
		}
	}
	unknown := "/api/contexts/" + uuid.NewV7().String()
	if p := c.Delete(t, unknown).Problem(t, http.StatusNotFound); p.Instance != unknown {
		t.Errorf("instance = %q, want %s", p.Instance, unknown)
	}
	if p := c.Delete(t, "/api/contexts/not-a-uuid").Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, "not-a-uuid") {
		t.Errorf("detail = %q, want the malformed id named", p.Detail)
	}
	for _, query := range []string{"?confirm=", "?confirm=-1", "?confirm=%2B1", "?confirm=x", "?confirm=1.5", "?confirm=0&confirm=0", "?confirm=99999999999999999999"} {
		if p := c.Delete(t, path+query).Problem(t, http.StatusBadRequest); !strings.Contains(p.Detail, "confirm must be one non-negative integer") {
			t.Errorf("%s: detail = %q, want the malformed confirm named", query, p.Detail)
		}
	}
	present(t, c, contexts.Root, child.ID)
}

func TestContexts_DeleteRacingAMoveCommitsOne(t *testing.T) {
	s, db, _ := domainService(t)
	ctx := t.Context()

	create := func(parent, name string) string {
		t.Helper()
		c, err := s.Create(ctx, contexts.CreateContext{ParentID: parent, Name: name, Kind: contexts.KindExplicit})
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}

	const trees = 100
	for i := range trees {
		doomed := create(contexts.Root, fmt.Sprintf("doomed%d", i))
		inside := create(doomed, fmt.Sprintf("inside%d", i))
		outside := create(contexts.Root, fmt.Sprintf("outside%d", i))
		into := i%2 == 0
		moved, to := outside, doomed
		if !into {
			moved, to = inside, contexts.Root
		}

		start := make(chan struct{})
		var deleteErr, moveErr error
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			deleteErr = s.Delete(ctx, doomed, 1)
		})
		wg.Go(func() {
			<-start
			_, moveErr = s.Move(ctx, moved, contexts.MoveContext{ParentID: to})
		})
		close(start)
		wg.Wait()

		d, conflicted := errors.AsType[*contexts.DescendantsError](deleteErr)
		switch {
		case deleteErr == nil && into && errors.Is(moveErr, contexts.ErrParentNotFound):
		case deleteErr == nil && !into && errors.Is(moveErr, sql.ErrNoRows):
		case conflicted && moveErr == nil && into && d.Count == 2:
		case conflicted && moveErr == nil && !into && d.Count == 0:
		default:
			t.Errorf("tree %d (move into %v): delete = %v, move = %v; want the delete committed and the move refused, or the move committed and the delete conflicting with the new count", i, into, deleteErr, moveErr)
			continue
		}

		var remaining int
		if err := db.Pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM context WHERE id IN ($1, $2, $3)`, doomed, inside, outside).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if want := map[bool]int{true: 1, false: 3}[deleteErr == nil]; remaining != want {
			t.Errorf("tree %d: %d of the tree's 3 contexts remain, want %d", i, remaining, want)
		}
	}

	var rows, orphans int
	if err := db.Pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM context`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM context c WHERE c.parent_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM context p WHERE p.id = c.parent_id)`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("%d contexts name a missing parent, want none", orphans)
	}
	if reached := subtree(t, s, contexts.Root); len(reached) != rows {
		t.Errorf("the walk from the root reaches %d of %d contexts, want every one", len(reached), rows)
	}
}

func TestContexts_CommandsPublishTheirEvents(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()
	s := integration.Subscribe(t, contexts.ContextCreated.Type(), contexts.ContextRenamed.Type(), contexts.ContextMoved.Type(), contexts.ContextDeleted.Type())

	res := c.Post(t, "/api/contexts", explicit(contexts.Root, "folder"))
	folder := webtest.Decode[contexts.Context](t, res, http.StatusCreated)
	wantEvent(t, s.AwaitType(t, contexts.ContextCreated.Type(), folder.ID), requestIDOf(t, res.Header),
		map[string]any{"id": folder.ID, "parent_id": contexts.Root, "name": "folder", "kind": "explicit", "filter": nil})

	res = c.Post(t, "/api/contexts", queryContext(contexts.Root, "saved", `[{"field":"kind","op":"null"}]`))
	saved := webtest.Decode[contexts.Context](t, res, http.StatusCreated)
	wantEvent(t, s.AwaitType(t, contexts.ContextCreated.Type(), saved.ID), requestIDOf(t, res.Header),
		map[string]any{"id": saved.ID, "parent_id": contexts.Root, "name": "saved", "kind": "query", "filter": []any{map[string]any{"field": "kind", "op": "null"}}})

	res = c.Put(t, "/api/contexts/"+saved.ID, `{"name":"kept"}`)
	webtest.Decode[contexts.Context](t, res, http.StatusOK)
	wantEvent(t, s.AwaitType(t, contexts.ContextRenamed.Type(), saved.ID), requestIDOf(t, res.Header),
		map[string]any{"id": saved.ID, "parent_id": contexts.Root, "name": "kept", "kind": "query", "filter": []any{map[string]any{"field": "kind", "op": "null"}}})

	res = c.Post(t, "/api/contexts/"+saved.ID+"/move", moveTo(folder.ID))
	webtest.Decode[contexts.Context](t, res, http.StatusOK)
	wantEvent(t, s.AwaitType(t, contexts.ContextMoved.Type(), saved.ID), requestIDOf(t, res.Header),
		map[string]any{"id": saved.ID, "parent_id": folder.ID, "name": "kept", "kind": "query", "filter": []any{map[string]any{"field": "kind", "op": "null"}}})

	res = c.Delete(t, "/api/contexts/"+folder.ID+"?confirm=1").Expect(t, http.StatusNoContent)
	wantEvent(t, s.AwaitType(t, contexts.ContextDeleted.Type(), folder.ID), requestIDOf(t, res.Header), map[string]any{"id": folder.ID})

	var deleted []string
	for _, e := range s.Received() {
		if e.Type == contexts.ContextDeleted.Type() {
			deleted = append(deleted, e.Subject)
		}
	}
	if !slices.Equal(deleted, []string{folder.ID}) {
		t.Errorf("deleted events about %v; want one, about %s, and none about its descendant %s", deleted, folder.ID, saved.ID)
	}
}

func TestContexts_CommandsThatChangeNothingPublishNothing(t *testing.T) {
	c := integration.Start(t, integration.Options{}).Client()
	s := integration.Subscribe(t, contexts.ContextRenamed.Type(), contexts.ContextMoved.Type(), contexts.ContextCreated.Type())

	child := createContext(t, c, explicit(contexts.Root, "child"))
	webtest.Decode[contexts.Context](t, c.Put(t, "/api/contexts/"+child.ID, `{"name":"child"}`), http.StatusOK)
	move(t, c, child.ID, moveTo(contexts.Root))
	c.Put(t, "/api/contexts/"+child.ID, `{"name":""}`).Expect(t, http.StatusBadRequest)
	marker := createContext(t, c, explicit(contexts.Root, "marker"))

	got := s.Await(t, marker.ID)
	for _, e := range got {
		if e.Subject == child.ID && e.Type != contexts.ContextCreated.Type() {
			t.Errorf("%s about %s; want nothing published for a command that changed nothing", e.Type, child.ID)
		}
	}
}
