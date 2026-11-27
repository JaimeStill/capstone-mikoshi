package contexts_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
)

func decode(t *testing.T, body string) contexts.CreateContext {
	t.Helper()
	var c contexts.CreateContext
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return c
}

func TestCreateContext_ValidAccepted(t *testing.T) {
	for _, body := range []string{
		`{"parent_id":"` + contexts.Root + `","name":"e","kind":"explicit"}`,
		`{"parent_id":"` + contexts.Root + `","name":"e","kind":"explicit","filter":null}`,
		`{"parent_id":"` + contexts.Root + `","name":"q","kind":"query","filter":[
			{"field":"a","op":"eq","value":"x"},{"field":"b","op":"ne","value":1},
			{"field":"c","op":"gt","value":1.5},{"field":"d","op":"ge","value":true},
			{"field":"e","op":"lt","value":2},{"field":"f","op":"le","value":"2026-01-01"},
			{"field":"g","op":"like","value":"%x%"},{"field":"h","op":"in","value":["x",2,false]},
			{"field":"i","op":"null"},{"field":"j","op":"notnull"}]}`,
	} {
		if err := decode(t, body).Validate(); err != nil {
			t.Errorf("Validate(%s) = %v, want nil", body, err)
		}
	}
}

func TestCreateContext_RefusalsNameTheirReason(t *testing.T) {
	cond := func(entries string) string {
		return `{"parent_id":"` + contexts.Root + `","name":"q","kind":"query","filter":[` + entries + `]}`
	}
	cases := []struct {
		name, body, want string
	}{
		{"a malformed parent", `{"parent_id":"x","name":"e","kind":"explicit"}`, "parent_id must be a UUID"},
		{"an empty name", `{"parent_id":"` + contexts.Root + `","name":"","kind":"explicit"}`, "name must not be empty"},
		{"an unknown kind", `{"parent_id":"` + contexts.Root + `","name":"e","kind":"saved"}`, `kind must be "explicit" or "query"`},
		{"an explicit context's filter", `{"parent_id":"` + contexts.Root + `","name":"e","kind":"explicit","filter":[]}`, "an explicit context carries no filter"},
		{"a query context without a filter", `{"parent_id":"` + contexts.Root + `","name":"q","kind":"query"}`, "a query context requires a filter"},
		{"an empty filter", cond(``), "filter must hold at least one condition"},
		{"an empty field", cond(`{"field":"a","op":"null"},{"field":"","op":"eq","value":1}`), "filter[1]: field must not be empty"},
		{"an unknown op", cond(`{"field":"a","op":"between","value":1}`), `filter[0]: op "between" is not one of`},
		{"in without a list", cond(`{"field":"a","op":"in","value":1}`), `filter[0]: op "in" takes a non-empty list of values`},
		{"in with an empty list", cond(`{"field":"a","op":"in","value":[]}`), `filter[0]: op "in" takes a non-empty list of values`},
		{"in with a list value", cond(`{"field":"a","op":"in","value":[1,[2]]}`), `filter[0]: op "in": value[1] must be a string, number, or boolean`},
		{"null with a value", cond(`{"field":"a","op":"eq","value":1},{"field":"b","op":"null","value":1}`), `filter[1]: op "null" takes no value`},
		{"notnull with a null value", cond(`{"field":"a","op":"notnull","value":null}`), `filter[0]: op "notnull" takes no value`},
		{"eq without a value", cond(`{"field":"a","op":"eq"}`), `filter[0]: op "eq" takes exactly one value`},
		{"eq with a null value", cond(`{"field":"a","op":"eq","value":null}`), `filter[0]: op "eq" takes exactly one value`},
		{"like with a list", cond(`{"field":"a","op":"like","value":["x"]}`), `filter[0]: op "like" takes exactly one value`},
		{"gt with an object", cond(`{"field":"a","op":"gt","value":{"x":1}}`), `filter[0]: op "gt" takes exactly one value`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := decode(t, c.body).Validate()
			if !errors.Is(err, contexts.ErrValidation) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("Validate = %v, want ErrValidation naming %q", err, c.want)
			}
		})
	}
}

func TestFilter_EveryMalformedEntryNamed(t *testing.T) {
	f := decode(t, `{"filter":[{"field":"","op":"eq","value":1},{"field":"a","op":"eq","value":1},{"field":"b","op":"x"}]}`).Filter
	err := f.Validate()
	if err == nil || !strings.Contains(err.Error(), "filter[0]") || strings.Contains(err.Error(), "filter[1]") || !strings.Contains(err.Error(), "filter[2]") {
		t.Errorf("Validate = %v, want filter[0] and filter[2] named, and filter[1] not", err)
	}
}

var contract = []query.Field{
	{Name: "id", Type: "uuid", NotNull: true},
	{Name: "route", Type: "text", NotNull: true},
	{Name: "kind", Type: "text"},
	{Name: "duration", Type: "double precision"},
	{Name: "public", Type: "boolean"},
}

func TestFilter_CheckAcceptsWhatTheContractDeclares(t *testing.T) {
	for _, filter := range []string{
		`[{"field":"route","op":"eq","value":"video"}]`,
		`[{"field":"route","op":"eq","value":"audio"}]`,
		`[{"field":"route","op":"in","value":["vision","vision-converted"]}]`,
		`[{"field":"route","op":"in","value":["audio","video"]},{"field":"duration","op":"gt","value":120}]`,
		`[{"field":"kind","op":"like","value":"m%"},{"field":"duration","op":"le","value":1.5},{"field":"kind","op":"null"},{"field":"duration","op":"notnull"}]`,
		`[{"field":"id","op":"ne","value":"0190a5d1-7a2b-7c3d-8e4f-000000000001"},{"field":"public","op":"eq","value":true}]`,
	} {
		if err := decode(t, `{"filter":`+filter+`}`).Filter.Check(contract); err != nil {
			t.Errorf("Check(%s) = %v, want nil", filter, err)
		}
	}
}

func TestFilter_CheckRefusesWhatDoesNotFitTheContract(t *testing.T) {
	cases := []struct {
		name, filter, want string
		unknown            bool
	}{
		{"an undeclared field", `[{"field":"route","op":"null"},{"field":"title","op":"eq","value":"x"}]`, `filter[1]: query: unknown filter field "title"`, true},
		{"a number for a text field", `[{"field":"route","op":"eq","value":5}]`, `filter[0]: query: invalid value for "route": a text field takes a string, not 5`, false},
		{"a string for a numeric field", `[{"field":"duration","op":"gt","value":"120"}]`, `filter[0]: query: invalid value for "duration": a double precision field takes a number, not "120"`, false},
		{"a boolean for a text field", `[{"field":"kind","op":"ne","value":true}]`, `filter[0]: query: invalid value for "kind"`, false},
		{"a number for a uuid field", `[{"field":"id","op":"eq","value":1}]`, `filter[0]: query: invalid value for "id"`, false},
		{"a string for a boolean field", `[{"field":"public","op":"eq","value":"yes"}]`, `filter[0]: query: invalid value for "public"`, false},
		{"one wrong value in a list", `[{"field":"duration","op":"in","value":[1,"x"]}]`, `filter[0]: query: invalid value for "duration": a double precision field takes a number, not "x"`, false},
		{"like on a numeric field", `[{"field":"duration","op":"like","value":"1%"}]`, `filter[0]: query: invalid value for "duration": op "like" applies to a text field`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := decode(t, `{"filter":`+c.filter+`}`).Filter.Check(contract)
			if !errors.Is(err, contexts.ErrValidation) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Check = %v, want ErrValidation naming %q", err, c.want)
			}
			if _, ok := errors.AsType[*query.UnknownFieldError](err); ok != c.unknown {
				t.Errorf("Check = %v, an UnknownFieldError: %v, want %v", err, ok, c.unknown)
			}
			if _, ok := errors.AsType[*query.InvalidValueError](err); ok == c.unknown {
				t.Errorf("Check = %v, an InvalidValueError: %v, want %v", err, ok, !c.unknown)
			}
		})
	}
}

func TestFilter_CheckNamesEveryConditionThatDoesNotFit(t *testing.T) {
	f := decode(t, `{"filter":[{"field":"a","op":"null"},{"field":"route","op":"null"},{"field":"duration","op":"eq","value":"x"}]}`).Filter
	err := f.Check(contract)
	if err == nil || !strings.Contains(err.Error(), "filter[0]") || strings.Contains(err.Error(), "filter[1]") || !strings.Contains(err.Error(), "filter[2]") {
		t.Errorf("Check = %v, want filter[0] and filter[2] named, and filter[1] not", err)
	}
}

func TestFilter_QueryMapsOntoSqlateFilters(t *testing.T) {
	f := decode(t, `{"filter":[{"field":"a","op":"eq","value":"x"},{"field":"b","op":"in","value":[1,"y"]},{"field":"c","op":"null"}]}`).Filter
	got, err := f.Query()
	if err != nil {
		t.Fatal(err)
	}
	want := []query.Filter{
		{Field: "a", Op: query.OpEq, Value: "x"},
		{Field: "b", Op: query.OpIn, Value: []any{float64(1), "y"}},
		{Field: "c", Op: query.OpIsNull},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Query = %#v, want %#v", got, want)
	}
}

func TestFilter_RoundTripsItsJSON(t *testing.T) {
	in := `[{"field":"a","op":"in","value":[1,"y"]},{"field":"c","op":"null"}]`
	var f contexts.Filter
	if err := f.Scan([]byte(in)); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(f)
	if err != nil || string(out) != in {
		t.Errorf("Marshal = %s, %v, want %s", out, err, in)
	}
	var none contexts.Filter
	if err := none.Scan(nil); err != nil || none != nil {
		t.Errorf("Scan(nil) = %v, %v, want the nil filter", none, err)
	}
}

func TestMoveContext_Validate(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"a parent alone", `{"parent_id":"` + contexts.Root + `"}`, ""},
		{"a parent and a name", `{"parent_id":"` + contexts.Root + `","name":"n"}`, ""},
		{"a null name", `{"parent_id":"` + contexts.Root + `","name":null}`, ""},
		{"a malformed parent", `{"parent_id":"x"}`, "parent_id must be a UUID"},
		{"no parent", `{"name":"n"}`, "parent_id must be a UUID"},
		{"an empty name", `{"parent_id":"` + contexts.Root + `","name":""}`, "name must not be empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var m contexts.MoveContext
			if err := json.Unmarshal([]byte(c.body), &m); err != nil {
				t.Fatal(err)
			}
			err := m.Validate()
			if c.want == "" && err != nil {
				t.Errorf("Validate = %v, want nil", err)
			}
			if c.want != "" && (!errors.Is(err, contexts.ErrValidation) || !strings.Contains(err.Error(), c.want)) {
				t.Errorf("Validate = %v, want ErrValidation naming %q", err, c.want)
			}
		})
	}
}
