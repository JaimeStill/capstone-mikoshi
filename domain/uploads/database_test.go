package uploads_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/capstone-mikoshi/data"
	"github.com/JaimeStill/capstone-mikoshi/domain/uploads"
)

func reclaimer(t *testing.T, responses ...sqltest.Response) (*uploads.Reclaimer, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	db := data.New(sqlate.Wrap(pool, sqltest.ReturningDialect{}), query.MustCatalog(query.Patterns()))
	return uploads.New(db).Reclaimer(), rec
}

func TestReclaimer_PassDeletesAtMostTheBudgetEarliestExpiryFirst(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		more     bool
	}{
		{"a full batch", 25, true},
		{"a short batch", 7, false},
		{"nothing expired", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, rec := reclaimer(t, sqltest.Response{Affected: c.affected})
			more, err := r.Pass(t.Context(), 25)
			if err != nil {
				t.Fatal(err)
			}
			if more != c.more {
				t.Errorf("more = %t after deleting %d of 25, want %t", more, c.affected, c.more)
			}
			if ops := rec.Ops(); !slices.Equal(ops, []sqltest.Op{sqltest.OpExec}) {
				t.Errorf("ops = %v, want one delete", ops)
			}
			call := rec.Calls()[0]
			for _, want := range []string{"DELETE FROM upload", "expires_at <= CURRENT_TIMESTAMP", "ORDER BY expires_at", "FETCH FIRST"} {
				if !strings.Contains(call.SQL, want) {
					t.Errorf("delete = %q, want it to contain %q", call.SQL, want)
				}
			}
			if !slices.Equal(call.Args, []any{25}) {
				t.Errorf("delete args = %v, want the budget", call.Args)
			}
		})
	}
}

func TestReclaimer_PassReturnsTheDeletesFailure(t *testing.T) {
	failure := errors.New("connection reset")
	r, _ := reclaimer(t, sqltest.Response{Err: failure})
	more, err := r.Pass(t.Context(), 10)
	if !errors.Is(err, failure) || more {
		t.Errorf("Pass = %t, %v; want no more and the delete's failure", more, err)
	}
}
