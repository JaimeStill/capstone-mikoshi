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
