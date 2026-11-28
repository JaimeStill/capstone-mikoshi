package items_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/capstone-mikoshi/correlation"
	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
	"github.com/JaimeStill/capstone-mikoshi/domain/items"
	"github.com/JaimeStill/capstone-mikoshi/internal/outboxtest"
)

const requestID = "req-0190a5d1"

func fileData(content []byte) map[string]any {
	return map[string]any{
		"id": itemID, "type": "file", "name": "report.pdf", "media_type": "application/pdf",
		"sha256": digest(content), "size": len(content), "route": "document", "kind": "pdf", "duration": nil,
	}
}

func TestCommands_EmitTheirEventInTheirTransaction(t *testing.T) {
	ctx := web.WithRequestID(context.Background(), requestID)
	reference := map[string]any{"context_id": contextID, "item_id": itemID}
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*items.Service) error
		ops       []sqltest.Op
		typ       string
		subject   string
		data      map[string]any
	}{
		"create a file over new content": {
			[]sqltest.Response{none(), none(), none(), affected(1), affected(1), itemRow(pdf, "document", "pdf"), eventWritten()},
			func(s *items.Service) error {
				_, err := s.CreateFile(ctx, file(pdf), bytes.NewReader(pdf))
				return err
			},
			[]sqltest.Op{q, begin, q, q, x, commit, begin, x, q, x, commit},
			items.ItemCreated.Type(), itemID, fileData(pdf),
		},
		"create a file over available content": {
			[]sqltest.Response{none(), blob(pdf, "available"), itemRow(pdf, "document", "pdf"), eventWritten()},
			func(s *items.Service) error {
				_, err := s.CreateFile(ctx, file(pdf), bytes.NewReader(pdf))
				return err
			},
			[]sqltest.Op{q, begin, q, q, x, commit},
			items.ItemCreated.Type(), itemID, fileData(pdf),
		},
		"create a record": {
			[]sqltest.Response{blob(record, "available"), recordRow(record), eventWritten()},
			func(s *items.Service) error {
				_, err := s.CreateRecord(ctx, items.CreateRecord{Name: "notes.json"}, record)
				return err
			},
			[]sqltest.Op{begin, q, q, x, commit},
			items.ItemCreated.Type(), itemID,
			map[string]any{
				"id": itemID, "type": "record", "name": "notes.json", "media_type": "application/json",
				"sha256": digest(record), "size": len(record), "route": "text", "kind": nil, "duration": nil,
			},
		},
		"delete an item and its references": {
			[]sqltest.Response{affected(1), affected(2), affected(1), eventWritten()},
			func(s *items.Service) error { return s.Delete(ctx, itemID) },
			[]sqltest.Op{begin, x, x, x, x, commit},
			items.ItemDeleted.Type(), itemID, map[string]any{"id": itemID},
		},
		"place a reference": {
			[]sqltest.Response{contextRow(contexts.KindExplicit), affected(1), eventWritten()},
			func(s *items.Service) error { return s.Place(ctx, contextID, itemID) },
			[]sqltest.Op{q, begin, x, x, commit},
			items.ContextItemAdded.Type(), contextID, reference,
		},
		"withdraw a reference": {
			[]sqltest.Response{contextRow(contexts.KindExplicit), affected(1), eventWritten()},
			func(s *items.Service) error { return s.Withdraw(ctx, contextID, itemID) },
			[]sqltest.Op{q, begin, x, x, commit},
			items.ContextItemRemoved.Type(), contextID, reference,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := service(t, c.responses...)
			if err := c.run(f.service); err != nil {
				t.Fatalf("= %v", err)
			}
			sameOps(t, f.rec, c.ops...)
			writes := outboxtest.Writes(f.rec)
			if len(writes) != 1 {
				t.Fatalf("events written = %d; want one", len(writes))
			}
			calls := f.rec.Calls()
			if last := calls[len(calls)-2]; last.SQL != writes[0].SQL {
				t.Errorf("the call before the commit is %q; want the event's write", last.SQL)
			}
			events := outboxtest.Committed(t, f.rec)
			if len(events) != 1 {
				t.Fatalf("events committed = %d; want one", len(events))
			}
			outboxtest.Want(t, events[0], c.typ, c.subject, requestID, c.data)
		})
	}
}

func TestCommands_OutsideARequestCorrelateByAFreshUUIDv7(t *testing.T) {
	f := service(t, blob(record, "available"), recordRow(record), eventWritten())
	if _, err := f.service.CreateRecord(context.Background(), items.CreateRecord{Name: "notes.json"}, record); err != nil {
		t.Fatal(err)
	}
	events := outboxtest.Committed(t, f.rec)
	if len(events) != 1 {
		t.Fatalf("events committed = %d; want one", len(events))
	}
	id := events[0].Extensions[correlation.Extension]
	u, err := uuid.Parse(id)
	if err != nil || u[6]>>4 != 7 {
		t.Errorf("correlationid = %q (%v); want a version-7 UUID", id, err)
	}
	outboxtest.WantEnvelope(t, events[0], items.ItemCreated.Type(), itemID, id)
}

func TestCommands_ThatChangeNothingEmitNothing(t *testing.T) {
	ctx := web.WithRequestID(context.Background(), requestID)
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*items.Service) error
		ops       []sqltest.Op
	}{
		"a repeated completion": {
			[]sqltest.Response{itemRow(pdf, "document", "pdf")},
			func(s *items.Service) error {
				_, err := s.CreateFile(ctx, file(pdf), unread{t})
				return err
			},
			[]sqltest.Op{q},
		},
		"an overlapping completion over available content": {
			[]sqltest.Response{none(), blob(pdf, "available"), violation("uq_item_upload"), itemRow(pdf, "document", "pdf")},
			func(s *items.Service) error {
				_, err := s.CreateFile(ctx, file(pdf), bytes.NewReader(pdf))
				return err
			},
			[]sqltest.Op{q, begin, q, q, rollback, q},
		},
		"an overlapping completion after writing the blob": {
			[]sqltest.Response{none(), blob(pdf, "pending"), affected(1), violation("uq_item_upload"), itemRow(pdf, "document", "pdf")},
			func(s *items.Service) error {
				_, err := s.CreateFile(ctx, file(pdf), bytes.NewReader(pdf))
				return err
			},
			[]sqltest.Op{q, begin, q, commit, begin, x, q, rollback, q},
		},
		"a placing of a reference in place": {
			[]sqltest.Response{contextRow(contexts.KindExplicit), violation("pk_context_item")},
			func(s *items.Service) error { return s.Place(ctx, contextID, itemID) },
			[]sqltest.Op{q, begin, x, rollback},
		},
		"a withdrawing of a reference not in place": {
			[]sqltest.Response{contextRow(contexts.KindExplicit), affected(0), itemRow(pdf, "document", "pdf")},
			func(s *items.Service) error { return s.Withdraw(ctx, contextID, itemID) },
			[]sqltest.Op{q, begin, x, q, commit},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := service(t, c.responses...)
			if err := c.run(f.service); err != nil {
				t.Fatalf("= %v; want the command's success", err)
			}
			sameOps(t, f.rec, c.ops...)
			if w := outboxtest.Writes(f.rec); len(w) != 0 {
				t.Errorf("events written = %d; want none", len(w))
			}
		})
	}
}

func TestCommands_ARefusedOrFailedCommandCommitsNoEvent(t *testing.T) {
	ctx := web.WithRequestID(context.Background(), requestID)
	refused := sqltest.Response{Err: errors.New("the outbox refused the row")}
	cases := map[string]struct {
		responses []sqltest.Response
		run       func(*items.Service) error
		ops       []sqltest.Op
	}{
		"create a file, an invalid command": {
			nil,
			func(s *items.Service) error {
				_, err := s.CreateFile(ctx, items.CreateFile{}, bytes.NewReader(pdf))
				return err
			},
			nil,
		},
		"create a file, the event's write refused": {
			[]sqltest.Response{none(), blob(pdf, "available"), itemRow(pdf, "document", "pdf"), refused},
			func(s *items.Service) error {
				_, err := s.CreateFile(ctx, file(pdf), bytes.NewReader(pdf))
				return err
			},
			[]sqltest.Op{q, begin, q, q, x, rollback},
		},
		"create a record, not JSON": {
			nil,
			func(s *items.Service) error {
				_, err := s.CreateRecord(ctx, items.CreateRecord{Name: "notes.json"}, []byte(`{`))
				return err
			},
			nil,
		},
		"create a record, the event's write refused": {
			[]sqltest.Response{blob(record, "available"), recordRow(record), refused},
			func(s *items.Service) error {
				_, err := s.CreateRecord(ctx, items.CreateRecord{Name: "notes.json"}, record)
				return err
			},
			[]sqltest.Op{begin, q, q, x, rollback},
		},
		"delete, an unknown item": {
			[]sqltest.Response{affected(0), affected(0), affected(0)},
			func(s *items.Service) error { return s.Delete(ctx, itemID) },
			[]sqltest.Op{begin, x, x, x, rollback},
		},
		"delete, the event's write refused": {
			[]sqltest.Response{affected(1), affected(0), affected(1), refused},
			func(s *items.Service) error { return s.Delete(ctx, itemID) },
			[]sqltest.Op{begin, x, x, x, x, rollback},
		},
		"place, a query context": {
			[]sqltest.Response{contextRow(contexts.KindQuery)},
			func(s *items.Service) error { return s.Place(ctx, contextID, itemID) },
			[]sqltest.Op{q},
		},
		"place, an unknown item": {
			[]sqltest.Response{contextRow(contexts.KindExplicit), foreignKey("fk_context_item_item")},
			func(s *items.Service) error { return s.Place(ctx, contextID, itemID) },
			[]sqltest.Op{q, begin, x, rollback},
		},
		"place, the event's write refused": {
			[]sqltest.Response{contextRow(contexts.KindExplicit), affected(1), refused},
			func(s *items.Service) error { return s.Place(ctx, contextID, itemID) },
			[]sqltest.Op{q, begin, x, x, rollback},
		},
		"withdraw, an unknown item": {
			[]sqltest.Response{contextRow(contexts.KindExplicit), affected(0), none()},
			func(s *items.Service) error { return s.Withdraw(ctx, contextID, itemID) },
			[]sqltest.Op{q, begin, x, q, rollback},
		},
		"withdraw, the event's write refused": {
			[]sqltest.Response{contextRow(contexts.KindExplicit), affected(1), refused},
			func(s *items.Service) error { return s.Withdraw(ctx, contextID, itemID) },
			[]sqltest.Op{q, begin, x, x, rollback},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := service(t, c.responses...)
			if err := c.run(f.service); err == nil {
				t.Fatal("= nil; want the command's failure")
			}
			sameOps(t, f.rec, c.ops...)
			outboxtest.NoneCommitted(t, f.rec)
		})
	}
}
