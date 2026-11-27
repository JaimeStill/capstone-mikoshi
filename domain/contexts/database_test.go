package contexts_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/capstone-mikoshi/data"
	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
)

const (
	begin    = sqltest.OpBegin
	q        = sqltest.OpQuery
	x        = sqltest.OpExec
	commit   = sqltest.OpCommit
	rollback = sqltest.OpRollback

	childID = "0190a5d1-7a2b-7c3d-8e4f-000000000001"
)

func service(t *testing.T, responses ...sqltest.Response) (*contexts.Service, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	db := data.New(sqlate.Wrap(pool, sqltest.ReturningDialect{}), query.MustCatalog(query.Patterns()))
	return contexts.New(db), rec
}

func row(id string, parent driver.Value, name string, kind contexts.Kind, filter driver.Value) sqltest.Response {
	return sqltest.Response{
		Columns: []string{"id", "parent_id", "name", "kind", "filter", "created_at"},
		Rows:    [][]driver.Value{{id, parent, name, string(kind), filter, time.Date(2026, 11, 27, 12, 0, 0, 0, time.FixedZone("x", 3600))}},
	}
}

func root() sqltest.Response {
	return row(contexts.Root, nil, "root", contexts.KindExplicit, nil)
}

func treeLocked() sqltest.Response { return sqltest.Response{Affected: 0} }

func sameOps(t *testing.T, rec *sqltest.Recorder, want ...sqltest.Op) {
	t.Helper()
	if got := rec.Ops(); !slices.Equal(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func TestService_FindReadsOneContextByID(t *testing.T) {
	s, rec := service(t, row(childID, contexts.Root, "q", contexts.KindQuery, []byte(`[{"field":"kind","op":"eq","value":"pdf"}]`)))
	c, err := s.Find(context.Background(), childID)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != childID || c.ParentID == nil || *c.ParentID != contexts.Root || c.Kind != contexts.KindQuery {
		t.Errorf("Find = %+v", c)
	}
	if len(c.Filter) != 1 || c.Filter[0].Field != "kind" || c.Filter[0].Op != query.OpEq || string(c.Filter[0].Value) != `"pdf"` {
		t.Errorf("filter = %+v", c.Filter)
	}
	if c.CreatedAt.Location() != time.UTC {
		t.Errorf("created_at in %v, want UTC", c.CreatedAt.Location())
	}
	if call := rec.Calls()[0]; call.SQL != "SELECT id, parent_id, name, kind, filter, created_at FROM context WHERE id = CAST($1 AS uuid)" || call.Args[0] != childID {
		t.Errorf("find = %+v", call)
	}
}

func TestService_FindUnknownIsNoRows(t *testing.T) {
	s, _ := service(t, sqltest.Response{Columns: []string{"id"}})
	if _, err := s.Find(context.Background(), childID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Find = %v, want sql.ErrNoRows", err)
	}
}

func TestService_CreateTakesTheTreeLockThenReadsTheParent(t *testing.T) {
	filter := `[{"field":"kind","op":"eq","value":"pdf"}]`
	s, rec := service(t, treeLocked(), root(), row(childID, contexts.Root, "q", contexts.KindQuery, []byte(filter)))
	c, err := s.Create(context.Background(), contexts.CreateContext{
		ParentID: contexts.Root, Name: "q", Kind: contexts.KindQuery,
		Filter: contexts.Filter{{Field: "kind", Op: query.OpEq, Value: []byte(`"pdf"`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != childID || c.Kind != contexts.KindQuery || len(c.Filter) != 1 {
		t.Errorf("Create = %+v", c)
	}
	sameOps(t, rec, begin, x, q, q, commit)
	calls := rec.Calls()
	if lock := calls[1]; lock.SQL != "SELECT pg_advisory_xact_lock(hashtext($1))" || lock.Args[0] != data.LockContextTree {
		t.Errorf("create did not take the tree lock first: %+v", lock)
	}
	if parent := calls[2]; !strings.HasPrefix(parent.SQL, "SELECT id, parent_id") || parent.Args[0] != contexts.Root {
		t.Errorf("create did not read its parent: %+v", parent)
	}
	insert := calls[3]
	if !strings.HasPrefix(insert.SQL, "INSERT INTO context (id, parent_id, name, kind, filter)") {
		t.Fatalf("insert = %q", insert.SQL)
	}
	id, err := uuid.Parse(insert.Args[0].(string))
	if err != nil || id[6]>>4 != 7 {
		t.Errorf("minted id %v (%v), want a version-7 UUID", insert.Args[0], err)
	}
	if insert.Args[1] != contexts.Root || insert.Args[2] != "q" || insert.Args[3] != "query" || insert.Args[4] != filter {
		t.Errorf("insert args = %v", insert.Args)
	}
}

func TestService_CreateExplicitBindsNoFilter(t *testing.T) {
	s, rec := service(t, treeLocked(), root(), row(childID, contexts.Root, "e", contexts.KindExplicit, nil))
	c, err := s.Create(context.Background(), contexts.CreateContext{ParentID: contexts.Root, Name: "e", Kind: contexts.KindExplicit})
	if err != nil {
		t.Fatal(err)
	}
	if c.Filter != nil {
		t.Errorf("filter = %+v, want none", c.Filter)
	}
	if insert := rec.Calls()[3]; insert.Args[4] != nil {
		t.Errorf("filter bound as %v, want NULL", insert.Args[4])
	}
}

func TestService_CreateRefusals(t *testing.T) {
	queryParent := row(childID, contexts.Root, "q", contexts.KindQuery, []byte(`[{"field":"f","op":"null"}]`))
	unique := &sqlate.ConstraintError{Constraint: "uq_context_sibling_name", Class: sqlate.ErrUniqueViolation, Err: errors.New("duplicate")}
	cases := []struct {
		name      string
		responses []sqltest.Response
		want      error
		ops       []sqltest.Op
	}{
		{"a missing parent", []sqltest.Response{treeLocked(), {Columns: []string{"id"}}}, contexts.ErrParentNotFound, []sqltest.Op{begin, x, q, rollback}},
		{"a query parent", []sqltest.Response{treeLocked(), queryParent}, contexts.ErrQueryParent, []sqltest.Op{begin, x, q, rollback}},
		{"a sibling's name", []sqltest.Response{treeLocked(), root(), {Err: unique}}, contexts.ErrNameTaken, []sqltest.Op{begin, x, q, q, rollback}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, rec := service(t, c.responses...)
			_, err := s.Create(context.Background(), contexts.CreateContext{ParentID: childID, Name: "e", Kind: contexts.KindExplicit})
			if !errors.Is(err, c.want) {
				t.Errorf("Create = %v, want %v", err, c.want)
			}
			sameOps(t, rec, c.ops...)
		})
	}
}

func TestService_CreateValidatesBeforeTheStore(t *testing.T) {
	s, rec := service(t)
	_, err := s.Create(context.Background(), contexts.CreateContext{ParentID: contexts.Root, Name: "", Kind: contexts.KindExplicit})
	if !errors.Is(err, contexts.ErrValidation) {
		t.Errorf("Create = %v, want ErrValidation", err)
	}
	sameOps(t, rec)
}

func children(names ...string) sqltest.Response {
	r := sqltest.Response{Columns: []string{"id", "parent_id", "name", "kind", "filter", "created_at"}}
	for i, name := range names {
		id := childID[:len(childID)-1] + string(rune('a'+i))
		r.Rows = append(r.Rows, []driver.Value{id, contexts.Root, name, string(contexts.KindExplicit), nil, time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)})
	}
	return r
}

func names(items []contexts.Context) []string {
	out := make([]string, len(items))
	for i, c := range items {
		out[i] = c.Name
	}
	return out
}

func page(number, size int) web.Query {
	return web.Query{Page: number, Size: size, Filters: []web.Filter{}}
}

func TestService_ChildrenReadsTheParentThenOnePageByName(t *testing.T) {
	s, rec := service(t, root(), sqltest.WithTotal(children("a", "b"), 2))
	items, paging, err := s.Children(context.Background(), contexts.Root, page(1, 5))
	if err != nil {
		t.Fatal(err)
	}
	if got := names(items); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("names = %v, want [a b]", got)
	}
	if paging.Total == nil || *paging.Total != 2 || paging.More {
		t.Errorf("paging = %+v, want a total of 2 and no further page", paging)
	}
	sameOps(t, rec, q, q)
	calls := rec.Calls()
	if parent := calls[0]; !strings.HasPrefix(parent.SQL, "SELECT id, parent_id") || parent.Args[0] != contexts.Root {
		t.Errorf("children did not read the parent first: %+v", parent)
	}
	read := calls[1]
	if !strings.Contains(read.SQL, "FROM context WHERE parent_id = CAST($1 AS uuid)") || read.Args[0] != contexts.Root {
		t.Errorf("read = %+v, want the parent's children", read)
	}
	if !strings.Contains(read.SQL, "ORDER BY q.name, q.id") {
		t.Errorf("read = %q, want it ordered by name", read.SQL)
	}
}

func TestService_ChildrenPagesBySizeAndContinuesByCursor(t *testing.T) {
	s, rec := service(t, root(), sqltest.WithTotal(children("a", "b", "c"), 3))
	items, paging, err := s.Children(context.Background(), contexts.Root, page(1, 2))
	if err != nil {
		t.Fatal(err)
	}
	if got := names(items); !slices.Equal(got, []string{"a", "b"}) || !paging.More || paging.Next == "" {
		t.Fatalf("first page = %v, %+v, want [a b] with a further page and a cursor", got, paging)
	}
	if read := rec.Calls()[1]; !slices.Equal(read.Args, []any{contexts.Root, 0, 3}) {
		t.Errorf("read args = %v, want offset 0 and one row past the size", read.Args)
	}

	rec.Queue(root(), sqltest.WithTotal(children("c"), 3))
	next := web.Query{Size: 2, Cursor: paging.Next, Filters: []web.Filter{}}
	items, paging, err = s.Children(context.Background(), contexts.Root, next)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(items); !slices.Equal(got, []string{"c"}) || paging.More {
		t.Errorf("second page = %v, %+v, want [c] and no further page", got, paging)
	}
	if read := rec.Calls()[3]; !slices.Contains(read.Args, any("b")) {
		t.Errorf("continued read args = %v, want it past the first page's last name", read.Args)
	}
}

func TestService_ChildrenRefusals(t *testing.T) {
	cases := []struct {
		name      string
		id        string
		query     web.Query
		responses []sqltest.Response
		want      error
		ops       []sqltest.Op
	}{
		{"a malformed id", "x", page(1, 5), nil, contexts.ErrValidation, nil},
		{"a sort", contexts.Root, web.Query{Page: 1, Size: 5, Sort: []web.Sort{{Field: "created_at"}}}, nil, contexts.ErrValidation, nil},
		{"a filter", contexts.Root, web.Query{Page: 1, Size: 5, Filters: []web.Filter{{Field: "kind", Values: []string{"query"}}}}, nil, contexts.ErrValidation, nil},
		{"an unknown parent", childID, page(1, 5), []sqltest.Response{{Columns: []string{"id"}}}, sql.ErrNoRows, []sqltest.Op{q}},
		{"a foreign cursor", contexts.Root, web.Query{Size: 5, Cursor: "not-a-cursor", Filters: []web.Filter{}}, []sqltest.Response{root()}, query.ErrDirectives, []sqltest.Op{q}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, rec := service(t, c.responses...)
			if _, _, err := s.Children(context.Background(), c.id, c.query); !errors.Is(err, c.want) {
				t.Errorf("Children = %v, want %v", err, c.want)
			}
			sameOps(t, rec, c.ops...)
		})
	}
}

func TestService_RenameSetsTheNameInOneStatement(t *testing.T) {
	s, rec := service(t, row(childID, contexts.Root, "renamed", contexts.KindExplicit, nil))
	c, err := s.Rename(context.Background(), childID, contexts.RenameContext{Name: "renamed"})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != childID || c.Name != "renamed" || c.ParentID == nil || *c.ParentID != contexts.Root {
		t.Errorf("Rename = %+v", c)
	}
	sameOps(t, rec, q)
	call := rec.Calls()[0]
	if !strings.HasPrefix(call.SQL, "UPDATE context SET name = $1 WHERE id = CAST($2 AS uuid)") || call.Args[0] != "renamed" || call.Args[1] != childID {
		t.Errorf("rename = %+v", call)
	}
	if strings.Contains(call.SQL, "parent_id =") {
		t.Errorf("rename = %q, want the parent left as it is", call.SQL)
	}
}

func TestService_RenameRefusals(t *testing.T) {
	unique := &sqlate.ConstraintError{Constraint: "uq_context_sibling_name", Class: sqlate.ErrUniqueViolation, Err: errors.New("duplicate")}
	cases := []struct {
		name      string
		id        string
		rename    string
		responses []sqltest.Response
		want      error
		ops       []sqltest.Op
	}{
		{"a malformed id", "x", "n", nil, contexts.ErrValidation, nil},
		{"an empty name", childID, "", nil, contexts.ErrValidation, nil},
		{"the root", contexts.Root, "n", nil, contexts.ErrRootFixed, nil},
		{"the root in another spelling", "{" + contexts.Root + "}", "n", nil, contexts.ErrRootFixed, nil},
		{"a sibling's name", childID, "n", []sqltest.Response{{Err: unique}}, contexts.ErrNameTaken, []sqltest.Op{q}},
		{"an unknown context", childID, "n", []sqltest.Response{{Columns: []string{"id"}}, {Columns: []string{"id"}}}, sql.ErrNoRows, []sqltest.Op{q, q}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, rec := service(t, c.responses...)
			if _, err := s.Rename(context.Background(), c.id, contexts.RenameContext{Name: c.rename}); !errors.Is(err, c.want) {
				t.Errorf("Rename = %v, want %v", err, c.want)
			}
			sameOps(t, rec, c.ops...)
		})
	}
}
