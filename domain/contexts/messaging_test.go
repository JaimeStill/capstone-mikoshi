package contexts_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/capstone-mikoshi/correlation"
	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
	"github.com/JaimeStill/capstone-mikoshi/internal/outboxtest"
)

const requestID = "req-0190a5d1"

func TestCommands_EmitTheirEventInTheirTransaction(t *testing.T) {
	ctx := web.WithRequestID(context.Background(), requestID)
	filter := `[{"field":"kind","op":"eq","value":"pdf"}]`
	renamed := "renamed"
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*contexts.Service) error
		ops       []sqltest.Op
		typ       string
		data      map[string]any
	}{
		"create an explicit context": {
			[]sqltest.Response{treeLocked(), root(), row(childID, contexts.Root, "e", contexts.KindExplicit, nil), eventWritten()},
			func(s *contexts.Service) error {
				_, err := s.Create(ctx, contexts.CreateContext{ParentID: contexts.Root, Name: "e", Kind: contexts.KindExplicit})
				return err
			},
			[]sqltest.Op{begin, x, q, q, x, commit},
			contexts.ContextCreated.Type(),
			map[string]any{"id": childID, "parent_id": contexts.Root, "name": "e", "kind": "explicit", "filter": nil},
		},
		"create a query context": {
			[]sqltest.Response{treeLocked(), root(), row(childID, contexts.Root, "q", contexts.KindQuery, []byte(filter)), eventWritten()},
			func(s *contexts.Service) error {
				_, err := s.Create(ctx, contexts.CreateContext{
					ParentID: contexts.Root, Name: "q", Kind: contexts.KindQuery,
					Filter: contexts.Filter{{Field: "kind", Op: query.OpEq, Value: []byte(`"pdf"`)}},
				})
				return err
			},
			[]sqltest.Op{begin, x, q, q, x, commit},
			contexts.ContextCreated.Type(),
			map[string]any{"id": childID, "parent_id": contexts.Root, "name": "q", "kind": "query",
				"filter": []any{map[string]any{"field": "kind", "op": "eq", "value": "pdf"}}},
		},
		"rename": {
			[]sqltest.Response{row(childID, contexts.Root, "renamed", contexts.KindExplicit, nil), eventWritten()},
			func(s *contexts.Service) error {
				_, err := s.Rename(ctx, childID, contexts.RenameContext{Name: "renamed"})
				return err
			},
			[]sqltest.Op{begin, q, x, commit},
			contexts.ContextRenamed.Type(),
			map[string]any{"id": childID, "parent_id": contexts.Root, "name": "renamed", "kind": "explicit", "filter": nil},
		},
		"move": {
			[]sqltest.Response{treeLocked(),
				row(childID, contexts.Root, "kept", contexts.KindExplicit, nil),
				row(parentID, contexts.Root, "folder", contexts.KindExplicit, nil),
				count(0),
				row(childID, parentID, "kept", contexts.KindExplicit, nil),
				eventWritten()},
			func(s *contexts.Service) error {
				_, err := s.Move(ctx, childID, contexts.MoveContext{ParentID: parentID})
				return err
			},
			[]sqltest.Op{begin, x, q, q, q, q, x, commit},
			contexts.ContextMoved.Type(),
			map[string]any{"id": childID, "parent_id": parentID, "name": "kept", "kind": "explicit", "filter": nil},
		},
		"move with a rename": {
			[]sqltest.Response{treeLocked(),
				row(childID, contexts.Root, "old", contexts.KindExplicit, nil),
				row(parentID, contexts.Root, "folder", contexts.KindExplicit, nil),
				count(0),
				row(childID, parentID, "renamed", contexts.KindExplicit, nil),
				eventWritten()},
			func(s *contexts.Service) error {
				_, err := s.Move(ctx, childID, contexts.MoveContext{ParentID: parentID, Name: &renamed})
				return err
			},
			[]sqltest.Op{begin, x, q, q, q, q, x, commit},
			contexts.ContextMoved.Type(),
			map[string]any{"id": childID, "parent_id": parentID, "name": "renamed", "kind": "explicit", "filter": nil},
		},
		"rename by a move under the same parent": {
			[]sqltest.Response{treeLocked(),
				row(childID, contexts.Root, "old", contexts.KindExplicit, nil),
				root(),
				count(0),
				row(childID, contexts.Root, "renamed", contexts.KindExplicit, nil),
				eventWritten()},
			func(s *contexts.Service) error {
				_, err := s.Move(ctx, childID, contexts.MoveContext{ParentID: contexts.Root, Name: &renamed})
				return err
			},
			[]sqltest.Op{begin, x, q, q, q, q, x, commit},
			contexts.ContextMoved.Type(),
			map[string]any{"id": childID, "parent_id": contexts.Root, "name": "renamed", "kind": "explicit", "filter": nil},
		},
		"delete a subtree": {
			[]sqltest.Response{treeLocked(), count(4), {Affected: 4}, eventWritten()},
			func(s *contexts.Service) error { return s.Delete(ctx, childID, 3) },
			[]sqltest.Op{begin, x, q, x, x, commit},
			contexts.ContextDeleted.Type(),
			map[string]any{"id": childID},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec := service(t, c.responses...)
			if err := c.run(s); err != nil {
				t.Fatalf("= %v", err)
			}
			sameOps(t, rec, c.ops...)
			writes := outboxtest.Writes(rec)
			if len(writes) != 1 {
				t.Fatalf("events written = %d; want one", len(writes))
			}
			calls := rec.Calls()
			if last := calls[len(calls)-2]; last.SQL != writes[0].SQL {
				t.Errorf("the call before the commit is %q; want the event's write", last.SQL)
			}
			events := outboxtest.Committed(t, rec)
			if len(events) != 1 {
				t.Fatalf("events committed = %d; want one", len(events))
			}
			outboxtest.Want(t, events[0], c.typ, childID, requestID, c.data)
		})
	}
}

func TestCommands_OutsideARequestCorrelateByAFreshUUIDv7(t *testing.T) {
	s, rec := service(t, treeLocked(), root(), row(childID, contexts.Root, "e", contexts.KindExplicit, nil), eventWritten())
	if _, err := s.Create(context.Background(), contexts.CreateContext{ParentID: contexts.Root, Name: "e", Kind: contexts.KindExplicit}); err != nil {
		t.Fatal(err)
	}
	events := outboxtest.Committed(t, rec)
	if len(events) != 1 {
		t.Fatalf("events committed = %d; want one", len(events))
	}
	id := events[0].Extensions[correlation.Extension]
	u, err := uuid.Parse(id)
	if err != nil || u[6]>>4 != 7 {
		t.Errorf("correlationid = %q (%v); want a version-7 UUID", id, err)
	}
	outboxtest.WantEnvelope(t, events[0], contexts.ContextCreated.Type(), childID, id)
}

func TestCommands_ThatChangeNothingEmitNothing(t *testing.T) {
	ctx := web.WithRequestID(context.Background(), requestID)
	none := sqltest.Response{Columns: []string{"id"}}
	kept := "kept"
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*contexts.Service) (contexts.Context, error)
		ops       []sqltest.Op
	}{
		"a rename to the name it has": {
			[]sqltest.Response{none, row(childID, contexts.Root, "kept", contexts.KindExplicit, nil)},
			func(s *contexts.Service) (contexts.Context, error) {
				return s.Rename(ctx, childID, contexts.RenameContext{Name: "kept"})
			},
			[]sqltest.Op{begin, q, q, commit},
		},
		"a move to the parent it has": {
			[]sqltest.Response{treeLocked(),
				row(childID, contexts.Root, "kept", contexts.KindExplicit, nil),
				root(),
				count(0),
				none,
				row(childID, contexts.Root, "kept", contexts.KindExplicit, nil)},
			func(s *contexts.Service) (contexts.Context, error) {
				return s.Move(ctx, childID, contexts.MoveContext{ParentID: contexts.Root})
			},
			[]sqltest.Op{begin, x, q, q, q, q, q, commit},
		},
		"a move to the parent and the name it has": {
			[]sqltest.Response{treeLocked(),
				row(childID, contexts.Root, "kept", contexts.KindExplicit, nil),
				root(),
				count(0),
				none,
				row(childID, contexts.Root, "kept", contexts.KindExplicit, nil)},
			func(s *contexts.Service) (contexts.Context, error) {
				return s.Move(ctx, childID, contexts.MoveContext{ParentID: contexts.Root, Name: &kept})
			},
			[]sqltest.Op{begin, x, q, q, q, q, q, commit},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec := service(t, c.responses...)
			got, err := c.run(s)
			if err != nil {
				t.Fatalf("= %v; want the context as it stands", err)
			}
			if got.ID != childID || got.Name != "kept" || got.ParentID == nil || *got.ParentID != contexts.Root {
				t.Errorf("= %+v; want the context unchanged", got)
			}
			sameOps(t, rec, c.ops...)
			if w := outboxtest.Writes(rec); len(w) != 0 {
				t.Errorf("events written = %d; want none", len(w))
			}
		})
	}
}

func TestCommands_ARefusedOrFailedCommandCommitsNoEvent(t *testing.T) {
	ctx := web.WithRequestID(context.Background(), requestID)
	unique := sqltest.Response{Err: &sqlate.ConstraintError{Constraint: "uq_context_sibling_name", Class: sqlate.ErrUniqueViolation, Err: errors.New("duplicate")}}
	refused := sqltest.Response{Err: errors.New("the outbox refused the row")}
	none := sqltest.Response{Columns: []string{"id"}}
	moved := row(childID, contexts.Root, "m", contexts.KindExplicit, nil)
	folder := row(parentID, contexts.Root, "folder", contexts.KindExplicit, nil)
	explicitChild := contexts.CreateContext{ParentID: contexts.Root, Name: "e", Kind: contexts.KindExplicit}
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*contexts.Service) error
		ops       []sqltest.Op
	}{
		"create, an invalid command": {
			nil,
			func(s *contexts.Service) error {
				_, err := s.Create(ctx, contexts.CreateContext{ParentID: contexts.Root, Kind: contexts.KindExplicit})
				return err
			},
			nil,
		},
		"create, a missing parent": {
			[]sqltest.Response{treeLocked(), none},
			func(s *contexts.Service) error { _, err := s.Create(ctx, explicitChild); return err },
			[]sqltest.Op{begin, x, q, rollback},
		},
		"create, a sibling's name": {
			[]sqltest.Response{treeLocked(), root(), unique},
			func(s *contexts.Service) error { _, err := s.Create(ctx, explicitChild); return err },
			[]sqltest.Op{begin, x, q, q, rollback},
		},
		"create, the event's write refused": {
			[]sqltest.Response{treeLocked(), root(), row(childID, contexts.Root, "e", contexts.KindExplicit, nil), refused},
			func(s *contexts.Service) error { _, err := s.Create(ctx, explicitChild); return err },
			[]sqltest.Op{begin, x, q, q, x, rollback},
		},
		"rename, the root": {
			nil,
			func(s *contexts.Service) error {
				_, err := s.Rename(ctx, contexts.Root, contexts.RenameContext{Name: "n"})
				return err
			},
			nil,
		},
		"rename, a sibling's name": {
			[]sqltest.Response{unique},
			func(s *contexts.Service) error {
				_, err := s.Rename(ctx, childID, contexts.RenameContext{Name: "n"})
				return err
			},
			[]sqltest.Op{begin, q, rollback},
		},
		"rename, an unknown context": {
			[]sqltest.Response{none, none},
			func(s *contexts.Service) error {
				_, err := s.Rename(ctx, childID, contexts.RenameContext{Name: "n"})
				return err
			},
			[]sqltest.Op{begin, q, q, rollback},
		},
		"rename, the event's write refused": {
			[]sqltest.Response{row(childID, contexts.Root, "n", contexts.KindExplicit, nil), refused},
			func(s *contexts.Service) error {
				_, err := s.Rename(ctx, childID, contexts.RenameContext{Name: "n"})
				return err
			},
			[]sqltest.Op{begin, q, x, rollback},
		},
		"move, a cycle": {
			[]sqltest.Response{treeLocked(), moved, folder, count(1)},
			func(s *contexts.Service) error {
				_, err := s.Move(ctx, childID, contexts.MoveContext{ParentID: parentID})
				return err
			},
			[]sqltest.Op{begin, x, q, q, q, rollback},
		},
		"move, a sibling's name": {
			[]sqltest.Response{treeLocked(), moved, folder, count(0), unique},
			func(s *contexts.Service) error {
				_, err := s.Move(ctx, childID, contexts.MoveContext{ParentID: parentID})
				return err
			},
			[]sqltest.Op{begin, x, q, q, q, q, rollback},
		},
		"move, the event's write refused": {
			[]sqltest.Response{treeLocked(), moved, folder, count(0), row(childID, parentID, "m", contexts.KindExplicit, nil), refused},
			func(s *contexts.Service) error {
				_, err := s.Move(ctx, childID, contexts.MoveContext{ParentID: parentID})
				return err
			},
			[]sqltest.Op{begin, x, q, q, q, q, x, rollback},
		},
		"delete, an unknown context": {
			[]sqltest.Response{treeLocked(), count(0)},
			func(s *contexts.Service) error { return s.Delete(ctx, childID, 0) },
			[]sqltest.Op{begin, x, q, rollback},
		},
		"delete, descendants unconfirmed": {
			[]sqltest.Response{treeLocked(), count(4)},
			func(s *contexts.Service) error { return s.Delete(ctx, childID, 0) },
			[]sqltest.Op{begin, x, q, rollback},
		},
		"delete, the event's write refused": {
			[]sqltest.Response{treeLocked(), count(1), {Affected: 1}, refused},
			func(s *contexts.Service) error { return s.Delete(ctx, childID, 0) },
			[]sqltest.Op{begin, x, q, x, x, rollback},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec := service(t, c.responses...)
			if err := c.run(s); err == nil {
				t.Fatal("= nil; want the command's failure")
			}
			sameOps(t, rec, c.ops...)
			outboxtest.NoneCommitted(t, rec)
		})
	}
}
