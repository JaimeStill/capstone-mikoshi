package contexts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/standards-lab/sqlate/query"
)

// Root is the id of the tree's root, the nil UUID: the one context with no
// parent, seeded by the migration that creates the tree.
const Root = "00000000-0000-0000-0000-000000000000"

// Kind is a context's kind, fixed at its creation.
type Kind string

const (
	// KindExplicit is a context whose contents are placed in it; it
	// carries no filter.
	KindExplicit Kind = "explicit"
	// KindQuery is a context whose contents are what its filter selects;
	// it is a leaf of the tree.
	KindQuery Kind = "query"
)

// Context is one node of the context tree, as the read presents it.
// ParentID is nil at the root alone. Filter is nil for an explicit
// context. The json tags are the scan contract: the read's columns carry
// the same names.
type Context struct {
	ID        string    `json:"id"`
	ParentID  *string   `json:"parent_id"`
	Name      string    `json:"name"`
	Kind      Kind      `json:"kind"`
	Filter    Filter    `json:"filter"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateContext is the create command's input: the parent the context is
// created under, its name and kind, and, for a query context, its filter.
type CreateContext struct {
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
	Kind     Kind   `json:"kind"`
	Filter   Filter `json:"filter"`
}

// Validate rejects what the command can know from its own fields: a
// parent id that is not a UUID, an empty name, an unknown kind, a filter
// on an explicit context, and a query context's missing or malformed
// filter. The parent's existence and kind and the name's uniqueness among
// its siblings are the store's.
func (c CreateContext) Validate() error {
	var errs []error
	if _, err := uuid.Parse(c.ParentID); err != nil {
		errs = append(errs, fmt.Errorf("%w: parent_id must be a UUID", ErrValidation))
	}
	if c.Name == "" {
		errs = append(errs, fmt.Errorf("%w: name must not be empty", ErrValidation))
	}
	switch c.Kind {
	case KindExplicit:
		if c.Filter != nil {
			errs = append(errs, fmt.Errorf("%w: an explicit context carries no filter", ErrValidation))
		}
	case KindQuery:
		if c.Filter == nil {
			errs = append(errs, fmt.Errorf("%w: a query context requires a filter", ErrValidation))
		} else if err := c.Filter.Validate(); err != nil {
			errs = append(errs, err)
		}
	default:
		errs = append(errs, fmt.Errorf("%w: kind must be %q or %q, not %q", ErrValidation, KindExplicit, KindQuery, c.Kind))
	}
	return errors.Join(errs...)
}

// RenameContext is the rename command's input: the context's new name.
type RenameContext struct {
	Name string `json:"name"`
}

// Validate rejects an empty name. The name's uniqueness among the
// context's siblings is the store's.
func (r RenameContext) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrValidation)
	}
	return nil
}

// MoveContext is the move command's input: the context's new parent and,
// when set, its new name; a nil name keeps the one it has.
type MoveContext struct {
	ParentID string  `json:"parent_id"`
	Name     *string `json:"name"`
}

// Validate rejects a parent id that is not a UUID and a set but empty
// name. The parent's existence and kind, the cycle check, and the name's
// uniqueness among the new siblings are the store's.
func (m MoveContext) Validate() error {
	var errs []error
	if _, err := uuid.Parse(m.ParentID); err != nil {
		errs = append(errs, fmt.Errorf("%w: parent_id must be a UUID", ErrValidation))
	}
	if m.Name != nil && *m.Name == "" {
		errs = append(errs, fmt.Errorf("%w: name must not be empty", ErrValidation))
	}
	return errors.Join(errs...)
}

// Node is one context of a subtree as its resolution presents it: its id,
// kind, and filter, nil for an explicit context. The json tags are the
// scan contract.
type Node struct {
	ID     string `json:"id"`
	Kind   Kind   `json:"kind"`
	Filter Filter `json:"filter"`
}

// Filter is a query context's filter: its conditions, every one of which
// an item must meet. It is stored as a JSON array, and its zero value, nil,
// is an explicit context's absent filter.
type Filter []Condition

// Condition is one entry of a [Filter]: a field, an operator, and the
// operator's value, in the shape of sqlate's query.Filter. in takes a
// list of values, null and notnull take none, and every other operator
// takes exactly one. A value is a JSON string, number, or boolean.
type Condition struct {
	Field string          `json:"field"`
	Op    query.Op        `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}

// Validate rejects an empty filter and every malformed condition, each
// refusal naming the condition's index: an empty field, an unknown
// operator, and a value the operator does not take. [Filter.Check] checks
// the fields against a field contract.
func (f Filter) Validate() error {
	if len(f) == 0 {
		return fmt.Errorf("%w: filter must hold at least one condition", ErrValidation)
	}
	var errs []error
	for i, c := range f {
		if err := c.validate(); err != nil {
			errs = append(errs, fmt.Errorf("%w: filter[%d]: %w", ErrValidation, i, err))
		}
	}
	return errors.Join(errs...)
}

func (c Condition) validate() error {
	if c.Field == "" {
		return errors.New("field must not be empty")
	}
	switch c.Op {
	case query.OpEq, query.OpNe, query.OpGt, query.OpGe, query.OpLt, query.OpLe, query.OpLike:
		if !scalar(c.Value) {
			return fmt.Errorf("op %q takes exactly one value, a string, number, or boolean", c.Op)
		}
	case query.OpIn:
		var values []json.RawMessage
		if json.Unmarshal(c.Value, &values) != nil || len(values) == 0 {
			return fmt.Errorf("op %q takes a non-empty list of values", c.Op)
		}
		for j, v := range values {
			if !scalar(v) {
				return fmt.Errorf("op %q: value[%d] must be a string, number, or boolean", c.Op, j)
			}
		}
	case query.OpIsNull, query.OpIsNotNull:
		if len(c.Value) != 0 {
			return fmt.Errorf("op %q takes no value", c.Op)
		}
	default:
		return fmt.Errorf("op %q is not one of eq, ne, gt, ge, lt, le, like, in, null, notnull", c.Op)
	}
	return nil
}

// Check rejects every condition of a well-formed filter that does not fit
// fields, a field contract, each refusal naming the condition's index: a
// field the contract does not declare, a sqlate UnknownFieldError; a value
// whose JSON type is not its field's, a number for a numeric field, a
// boolean for a boolean one, and a string for any other; and like on a
// field that is not text, each a sqlate InvalidValueError. Check takes a
// filter Validate accepted.
func (f Filter) Check(fields []query.Field) error {
	declared := make(map[string]query.Field, len(fields))
	for _, field := range fields {
		declared[field.Name] = field
	}
	var errs []error
	for i, c := range f {
		if err := c.check(declared); err != nil {
			errs = append(errs, fmt.Errorf("%w: filter[%d]: %w", ErrValidation, i, err))
		}
	}
	return errors.Join(errs...)
}

func (c Condition) check(declared map[string]query.Field) error {
	field, ok := declared[c.Field]
	if !ok {
		return &query.UnknownFieldError{Field: c.Field, Use: query.FieldUseFilter}
	}
	want, text := fieldType(field.Type)
	if c.Op == query.OpLike && !text {
		return &query.InvalidValueError{Field: c.Field, Err: fmt.Errorf("op %q applies to a text field, not a %s field", c.Op, field.Type)}
	}
	var values []json.RawMessage
	switch c.Op {
	case query.OpIsNull, query.OpIsNotNull:
	case query.OpIn:
		if err := json.Unmarshal(c.Value, &values); err != nil {
			return err
		}
	default:
		values = []json.RawMessage{c.Value}
	}
	for _, v := range values {
		if valueType(v) != want {
			return &query.InvalidValueError{Field: c.Field, Err: fmt.Errorf("a %s field takes a %s, not %s", field.Type, want, v)}
		}
	}
	return nil
}

type jsonType string

const (
	jsonString  jsonType = "string"
	jsonNumber  jsonType = "number"
	jsonBoolean jsonType = "boolean"
)

func fieldType(sqlType string) (jsonType, bool) {
	base, _, _ := strings.Cut(strings.ToLower(sqlType), "(")
	switch strings.TrimSpace(base) {
	case "text", "character varying", "varchar", "character", "char":
		return jsonString, true
	case "smallint", "integer", "int", "bigint", "real", "double precision", "float", "numeric", "decimal":
		return jsonNumber, false
	case "boolean":
		return jsonBoolean, false
	}
	return jsonString, false
}

func valueType(v json.RawMessage) jsonType {
	switch bytes.TrimSpace(v)[0] {
	case '"':
		return jsonString
	case 't', 'f':
		return jsonBoolean
	}
	return jsonNumber
}

func scalar(v json.RawMessage) bool {
	v = bytes.TrimSpace(v)
	if len(v) == 0 {
		return false
	}
	switch v[0] {
	case '[', '{', 'n':
		return false
	}
	return json.Valid(v)
}

// Query maps the filter onto sqlate's filter declarations, in order: a
// value decodes to its Go form, and in's list to a []any.
func (f Filter) Query() ([]query.Filter, error) {
	out := make([]query.Filter, len(f))
	for i, c := range f {
		out[i] = query.Filter{Field: c.Field, Op: c.Op}
		if len(c.Value) == 0 {
			continue
		}
		if err := json.Unmarshal(c.Value, &out[i].Value); err != nil {
			return nil, fmt.Errorf("filter[%d]: %w", i, err)
		}
	}
	return out, nil
}

// Scan reads the filter from its stored JSON array; a NULL is the nil
// filter.
func (f *Filter) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*f = nil
		return nil
	case []byte:
		return json.Unmarshal(v, f)
	case string:
		return json.Unmarshal([]byte(v), f)
	}
	return fmt.Errorf("contexts: cannot scan %T into a filter", src)
}
