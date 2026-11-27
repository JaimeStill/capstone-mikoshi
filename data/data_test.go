package data_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/capstone-mikoshi/data"
)

var errBoom = errors.New("boom")

var widgets = fstest.MapFS{
	"statements/widget.sql": {Data: []byte("--| tier: standard\n-- A widget store's read.\nSELECT 1\n")},
}

type verifier struct {
	ran bool
	err error
}

func (v *verifier) Verify(context.Context, sqlate.Session) error {
	v.ran = true
	return v.err
}

func newDatabase(t *testing.T) (*data.Database, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t)
	return data.New(sqlate.Wrap(pool, sqltest.Dialect{}), query.MustCatalog(query.Patterns())), rec
}

func names(db *data.Database) []string {
	var out []string
	for _, e := range db.Registry() {
		out = append(out, e.Name)
	}
	return out
}

func TestNew_RegistersThePackagesOwnStatements(t *testing.T) {
	db, _ := newDatabase(t)
	if got := names(db); !slices.Equal(got, []string{"data"}) {
		t.Errorf("registry = %v, want the package's own statements under data", got)
	}
}

func TestRegister_ListsEveryDomainInNameOrder(t *testing.T) {
	db, _ := newDatabase(t)
	stmts := db.Catalog.MustCompile(widgets, "statements", db.Dialect())
	db.Register("widget", stmts, stmts)
	db.Register("account", stmts, stmts)

	if got, want := names(db), []string{"account", "data", "widget"}; !slices.Equal(got, want) {
		t.Errorf("registry = %v, want %v", got, want)
	}
}

func TestRegister_PanicsOnADuplicateName(t *testing.T) {
	db, _ := newDatabase(t)
	stmts := db.Catalog.MustCompile(widgets, "statements", db.Dialect())
	db.Register("widget", stmts, stmts)
	defer func() {
		if recover() == nil {
			t.Fatal("Register accepted a second registration under widget")
		}
	}()
	db.Register("widget", stmts, stmts)
}

func TestVerify_PreparesEveryRegisteredStatement(t *testing.T) {
	db, rec := newDatabase(t)
	stmts := db.Catalog.MustCompile(widgets, "statements", db.Dialect())
	db.Register("widget", stmts, stmts)

	if err := db.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	prepared := rec.SQL(sqltest.OpPrepare)
	for _, entry := range db.Registry() {
		for _, st := range entry.Statements.Statements() {
			if !slices.Contains(prepared, st.Text()) {
				t.Errorf("%s's statement %s was not verified", entry.Name, st.Name())
			}
		}
	}
}

func TestVerify_ReportsARegisteredStoresFailure(t *testing.T) {
	db, _ := newDatabase(t)
	store := &verifier{err: errBoom}
	db.Register("widget", db.Catalog.MustCompile(widgets, "statements", db.Dialect()), store)

	if err := db.Verify(context.Background()); !store.ran || !errors.Is(err, errBoom) {
		t.Fatalf("Verify = %v, store ran %t; want the store's failure", err, store.ran)
	}
}

func TestVerify_RunsARecordedVerifier(t *testing.T) {
	db, _ := newDatabase(t)
	entries := len(db.Registry())
	store := &verifier{err: errBoom}
	db.Record(store)

	if err := db.Verify(context.Background()); !store.ran || !errors.Is(err, errBoom) {
		t.Fatalf("Verify = %v, store ran %t; want the recorded store's failure", err, store.ran)
	}
	if n := len(db.Registry()); n != entries {
		t.Errorf("registry holds %d entries after Record, want %d", n, entries)
	}
}
