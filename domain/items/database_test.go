package items_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/capstone-mikoshi/data"
	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
	"github.com/JaimeStill/capstone-mikoshi/domain/items"
)

const (
	begin    = sqltest.OpBegin
	q        = sqltest.OpQuery
	x        = sqltest.OpExec
	commit   = sqltest.OpCommit
	rollback = sqltest.OpRollback

	itemID   = "0190a5d1-7a2b-7c3d-8e4f-000000000001"
	uploadID = "6f1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
)

var pdf = []byte("%PDF-1.7\n1 0 obj << >> endobj\ntrailer << >>\n%%EOF\n")

type fixture struct {
	service *items.Service
	rec     *sqltest.Recorder
	fake    *storagetest.Fake
}

func service(t *testing.T, responses ...sqltest.Response) fixture {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	db := data.New(sqlate.Wrap(pool, sqltest.ReturningDialect{}), query.MustCatalog(query.Patterns()))
	fake := storagetest.NewFake(storagetest.Options{})
	cfg := storage.Config{Container: "items"}
	if err := cfg.Finalize(""); err != nil {
		t.Fatal(err)
	}
	objects := storage.New(fake, cfg)
	if err := objects.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = objects.Shutdown(context.Background()) })
	return fixture{service: items.New(db, objects, contexts.New(db, items.FilterFields(db))), rec: rec, fake: fake}
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func file(content []byte) items.CreateFile {
	return items.CreateFile{UploadID: uploadID, Name: "report.pdf", MediaType: "application/pdf", SHA256: digest(content), Size: int64(len(content))}
}

var itemColumns = []string{"id", "type", "name", "media_type", "sha256", "size", "route", "kind", "duration", "created_at"}

func itemRow(content []byte, route string, kind driver.Value) sqltest.Response {
	return sqltest.Response{
		Columns: itemColumns,
		Rows: [][]driver.Value{{itemID, "file", "report.pdf", "application/pdf", digest(content), int64(len(content)), route, kind, nil,
			time.Date(2026, 11, 27, 12, 0, 0, 0, time.FixedZone("x", 3600))}},
	}
}

func none() sqltest.Response { return sqltest.Response{Columns: []string{"id"}} }

func blob(content []byte, status string) sqltest.Response {
	return sqltest.Response{
		Columns: []string{"sha256", "size", "status"},
		Rows:    [][]driver.Value{{digest(content), int64(len(content)), status}},
	}
}

func affected(n int64) sqltest.Response { return sqltest.Response{Affected: n} }

func violation(constraint string) sqltest.Response {
	return sqltest.Response{Err: &sqlate.ConstraintError{Constraint: constraint, Class: sqlate.ErrUniqueViolation, Err: errors.New("duplicate")}}
}

func sameOps(t *testing.T, rec *sqltest.Recorder, want ...sqltest.Op) {
	t.Helper()
	if got := rec.Ops(); !slices.Equal(got, want) {
		t.Errorf("ops = %v, want %v", got, want)
	}
}

func stored(t *testing.T, f fixture, content []byte) []byte {
	t.Helper()
	b, err := f.fake.Get(t.Context(), "sha256/"+digest(content), storage.GetOptions{})
	if err != nil {
		t.Fatalf("object sha256/%s: %v", digest(content), err)
	}
	defer func() { _ = b.Body.Close() }()
	got, err := io.ReadAll(b.Body)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func callsTo(rec *sqltest.Recorder, prefix string) []sqltest.Call {
	var out []sqltest.Call
	for _, c := range rec.Calls() {
		if strings.HasPrefix(c.SQL, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func TestService_CreateFileWritesThePendingRowThenTheObjectThenMarksIt(t *testing.T) {
	f := service(t, none(), none(), none(), affected(1), affected(1), itemRow(pdf, "document", "pdf"))
	item, err := f.service.CreateFile(t.Context(), file(pdf), bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != itemID || item.Type != items.TypeFile || item.Route != "document" || item.Kind == nil || *item.Kind != "pdf" || item.Duration != nil {
		t.Errorf("CreateFile = %+v", item)
	}
	if want := time.Date(2026, 11, 27, 11, 0, 0, 0, time.UTC); item.CreatedAt != want {
		t.Errorf("created_at = %v, want %v", item.CreatedAt, want)
	}
	sameOps(t, f.rec, q, begin, q, q, x, commit, begin, x, q, commit)
	if f.fake.Puts() != 1 || !bytes.Equal(stored(t, f, pdf), pdf) {
		t.Errorf("puts = %d, want the content written once at its digest's key", f.fake.Puts())
	}
	if opts, n := f.fake.LastPut(); opts.Size != int64(len(pdf)) || n != int64(len(pdf)) {
		t.Errorf("put %+v with %d bytes, want the declared size", opts, n)
	}

	calls := f.rec.Calls()
	if lookup := calls[0]; !strings.Contains(lookup.SQL, "WHERE upload_id = $1") || lookup.Args[0] != uploadID {
		t.Errorf("first call = %+v, want the item read by upload id", lookup)
	}
	if touch := callsTo(f.rec, "UPDATE blob SET updated_at"); len(touch) != 1 || touch[0].Args[0] != digest(pdf) {
		t.Errorf("touch = %+v, want the blob row locked by its digest", touch)
	}
	if pending := callsTo(f.rec, "INSERT INTO blob"); len(pending) != 1 || !strings.Contains(pending[0].SQL, "'pending'") || !slices.Equal(pending[0].Args, []any{digest(pdf), int64(len(pdf))}) {
		t.Errorf("blob insert = %+v, want a pending row of the content's digest and size", pending)
	}
	if mark := callsTo(f.rec, "UPDATE blob SET status = 'available'"); len(mark) != 1 || mark[0].Args[0] != digest(pdf) {
		t.Errorf("mark = %+v, want the row marked available", mark)
	}
	insert := callsTo(f.rec, "INSERT INTO item")
	if len(insert) != 1 {
		t.Fatalf("item inserts = %+v, want one", insert)
	}
	id, err := uuid.Parse(insert[0].Args[0].(string))
	if err != nil || id[6]>>4 != 7 {
		t.Errorf("minted id %v (%v), want a version-7 UUID", insert[0].Args[0], err)
	}
	if want := []any{"file", "report.pdf", "application/pdf", digest(pdf), int64(len(pdf)), "document", "pdf", uploadID}; !slices.Equal(insert[0].Args[1:], want) {
		t.Errorf("item insert args = %v, want %v", insert[0].Args[1:], want)
	}
}

func TestService_CreateFileOverAvailableContentWritesNoObject(t *testing.T) {
	f := service(t, none(), blob(pdf, "available"), itemRow(pdf, "document", "pdf"))
	if _, err := f.service.CreateFile(t.Context(), file(pdf), bytes.NewReader(pdf)); err != nil {
		t.Fatal(err)
	}
	sameOps(t, f.rec, q, begin, q, q, commit)
	if f.fake.Puts() != 0 {
		t.Errorf("puts = %d, want none for available content", f.fake.Puts())
	}
	if len(callsTo(f.rec, "INSERT INTO blob")) != 0 || len(callsTo(f.rec, "UPDATE blob SET status")) != 0 {
		t.Error("the blob row was written again for available content")
	}
}

func TestService_CreateFileWritesAPendingBlobsObjectAgain(t *testing.T) {
	f := service(t, none(), blob(pdf, "pending"), affected(1), itemRow(pdf, "document", "pdf"))
	if _, err := f.service.CreateFile(t.Context(), file(pdf), bytes.NewReader(pdf)); err != nil {
		t.Fatal(err)
	}
	sameOps(t, f.rec, q, begin, q, commit, begin, x, q, commit)
	if f.fake.Puts() != 1 || !bytes.Equal(stored(t, f, pdf), pdf) {
		t.Errorf("puts = %d, want the pending blob's object written", f.fake.Puts())
	}
}

type unread struct{ t *testing.T }

func (u unread) Read([]byte) (int, error) {
	u.t.Error("the content was read for an upload whose item exists")
	return 0, io.EOF
}

func TestService_CreateFileRepeatedReturnsTheUploadsItem(t *testing.T) {
	f := service(t, itemRow(pdf, "document", "pdf"))
	item, err := f.service.CreateFile(t.Context(), file(pdf), unread{t})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != itemID {
		t.Errorf("CreateFile = %+v, want the existing item", item)
	}
	sameOps(t, f.rec, q)
	if f.fake.Puts() != 0 {
		t.Errorf("puts = %d, want none", f.fake.Puts())
	}
}

func TestService_CreateFileOverlappingReturnsTheItemTheOtherCreated(t *testing.T) {
	cases := []struct {
		name      string
		responses []sqltest.Response
		ops       []sqltest.Op
	}{
		{"over an available blob", []sqltest.Response{none(), blob(pdf, "available"), violation("uq_item_upload"), itemRow(pdf, "document", "pdf")},
			[]sqltest.Op{q, begin, q, q, rollback, q}},
		{"after writing the blob", []sqltest.Response{none(), blob(pdf, "pending"), affected(1), violation("uq_item_upload"), itemRow(pdf, "document", "pdf")},
			[]sqltest.Op{q, begin, q, commit, begin, x, q, rollback, q}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := service(t, c.responses...)
			item, err := f.service.CreateFile(t.Context(), file(pdf), bytes.NewReader(pdf))
			if err != nil {
				t.Fatal(err)
			}
			if item.ID != itemID {
				t.Errorf("CreateFile = %+v, want the item the other completion created", item)
			}
			sameOps(t, f.rec, c.ops...)
			if last := f.rec.Calls()[len(f.rec.Calls())-1]; !strings.Contains(last.SQL, "WHERE upload_id = $1") || last.Args[0] != uploadID {
				t.Errorf("last call = %+v, want the item read by upload id", last)
			}
		})
	}
}

func TestService_CreateFileStartsOverWhenTheBlobRowChanges(t *testing.T) {
	cases := []struct {
		name      string
		responses []sqltest.Response
		ops       []sqltest.Op
		puts      int64
	}{
		{"a row reclaimed while its object was written", []sqltest.Response{
			none(), none(), none(), affected(1), affected(0),
			none(), none(), affected(1), affected(1), itemRow(pdf, "document", "pdf"),
		}, []sqltest.Op{q, begin, q, q, x, commit, begin, x, rollback, begin, q, q, x, commit, begin, x, q, commit}, 2},
		{"a row inserted concurrently", []sqltest.Response{
			none(), none(), none(), violation("pk_blob"),
			blob(pdf, "pending"), affected(1), itemRow(pdf, "document", "pdf"),
		}, []sqltest.Op{q, begin, q, q, x, rollback, begin, q, commit, begin, x, q, commit}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := service(t, c.responses...)
			item, err := f.service.CreateFile(t.Context(), file(pdf), bytes.NewReader(pdf))
			if err != nil {
				t.Fatal(err)
			}
			if item.ID != itemID {
				t.Errorf("CreateFile = %+v", item)
			}
			sameOps(t, f.rec, c.ops...)
			if f.fake.Puts() != c.puts {
				t.Errorf("puts = %d, want %d", f.fake.Puts(), c.puts)
			}
		})
	}
}

func TestService_CreateFileGivesUpAfterThreeChanges(t *testing.T) {
	var responses []sqltest.Response
	responses = append(responses, none())
	for range 3 {
		responses = append(responses, none(), none(), affected(1), affected(0))
	}
	f := service(t, responses...)
	if _, err := f.service.CreateFile(t.Context(), file(pdf), bytes.NewReader(pdf)); err == nil {
		t.Fatal("CreateFile succeeded although every attempt lost its blob row")
	}
	if f.fake.Puts() != 3 || len(callsTo(f.rec, "INSERT INTO item")) != 0 {
		t.Errorf("puts = %d, item inserts = %d; want three attempts and no item", f.fake.Puts(), len(callsTo(f.rec, "INSERT INTO item")))
	}
}

func TestService_CreateFileFailedPutCreatesNoItem(t *testing.T) {
	f := service(t, none(), none(), none(), affected(1))
	f.fake.FailPut(storage.ErrUnavailable)
	if _, err := f.service.CreateFile(t.Context(), file(pdf), bytes.NewReader(pdf)); !errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("CreateFile = %v, want the Put's failure", err)
	}
	sameOps(t, f.rec, q, begin, q, q, x, commit)
}

func TestService_CreateFileRefusesContentThatIsNotTheUploads(t *testing.T) {
	cases := []struct {
		name    string
		command items.CreateFile
		want    string
	}{
		{"a short file", func() items.CreateFile { c := file(pdf); c.Size++; return c }(), "bytes, not the"},
		{"another file", func() items.CreateFile { c := file(pdf); c.SHA256 = digest([]byte("other")); return c }(), "SHA-256 is"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := service(t, none())
			_, err := f.service.CreateFile(t.Context(), c.command, bytes.NewReader(pdf))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("CreateFile = %v, want a refusal naming %q", err, c.want)
			}
			sameOps(t, f.rec, q)
			if f.fake.Puts() != 0 {
				t.Errorf("puts = %d, want none", f.fake.Puts())
			}
		})
	}
}

func TestService_CreateFileValidatesBeforeTheStore(t *testing.T) {
	cases := []struct {
		name    string
		command items.CreateFile
	}{
		{"no upload id", items.CreateFile{MediaType: "text/plain", SHA256: digest(pdf)}},
		{"no media type", items.CreateFile{UploadID: uploadID, SHA256: digest(pdf)}},
		{"an upper-case digest", items.CreateFile{UploadID: uploadID, MediaType: "text/plain", SHA256: strings.ToUpper(digest(pdf))}},
		{"a short digest", items.CreateFile{UploadID: uploadID, MediaType: "text/plain", SHA256: "abc"}},
		{"a negative size", items.CreateFile{UploadID: uploadID, MediaType: "text/plain", SHA256: digest(pdf), Size: -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := service(t)
			if _, err := f.service.CreateFile(t.Context(), c.command, bytes.NewReader(nil)); !errors.Is(err, items.ErrValidation) {
				t.Errorf("CreateFile = %v, want ErrValidation", err)
			}
			sameOps(t, f.rec)
		})
	}
}

var record = []byte(`{"title": "notes",  "tags": ["a", "b"]}` + "\n")

func recordRow(content []byte) sqltest.Response {
	return sqltest.Response{
		Columns: itemColumns,
		Rows: [][]driver.Value{{itemID, "record", "notes.json", "application/json", digest(content), int64(len(content)), "text", nil, nil,
			time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)}},
	}
}

func TestService_CreateRecordStoresTheBytesAsAJSONBlob(t *testing.T) {
	f := service(t, none(), none(), affected(1), affected(1), recordRow(record))
	item, err := f.service.CreateRecord(t.Context(), items.CreateRecord{Name: "notes.json"}, record)
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != itemID || item.Type != items.TypeRecord || item.MediaType != "application/json" {
		t.Errorf("CreateRecord = %+v", item)
	}
	sameOps(t, f.rec, begin, q, q, x, commit, begin, x, q, commit)
	if f.fake.Puts() != 1 || !bytes.Equal(stored(t, f, record), record) {
		t.Errorf("puts = %d, want the record's exact bytes written once at its digest's key", f.fake.Puts())
	}
	for _, c := range f.rec.Calls() {
		if strings.Contains(c.SQL, "WHERE upload_id") {
			t.Errorf("call %+v, want no read by upload id", c)
		}
	}
	insert := callsTo(f.rec, "INSERT INTO item")
	if len(insert) != 1 {
		t.Fatalf("item inserts = %+v, want one", insert)
	}
	if want := []any{"record", "notes.json", "application/json", digest(record), int64(len(record)), "text", nil, nil}; !slices.Equal(insert[0].Args[1:], want) {
		t.Errorf("item insert args = %v, want %v", insert[0].Args[1:], want)
	}
}

func TestService_CreateRecordTwiceCreatesTwoItemsOverOneBlob(t *testing.T) {
	f := service(t, blob(record, "available"), recordRow(record), blob(record, "available"), recordRow(record))
	for range 2 {
		if _, err := f.service.CreateRecord(t.Context(), items.CreateRecord{Name: "notes.json"}, record); err != nil {
			t.Fatal(err)
		}
	}
	sameOps(t, f.rec, begin, q, q, commit, begin, q, q, commit)
	if n := len(callsTo(f.rec, "INSERT INTO item")); n != 2 {
		t.Errorf("item inserts = %d, want two", n)
	}
	if f.fake.Puts() != 0 {
		t.Errorf("puts = %d, want none over available content", f.fake.Puts())
	}
}

func TestService_CreateRecordValidatesBeforeTheStore(t *testing.T) {
	cases := []struct {
		name    string
		command items.CreateRecord
		content []byte
	}{
		{"no name", items.CreateRecord{}, record},
		{"not JSON", items.CreateRecord{Name: "notes.json"}, []byte(`{"title": `)},
		{"two values", items.CreateRecord{Name: "notes.json"}, []byte(`{} {}`)},
		{"an empty body", items.CreateRecord{Name: "notes.json"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := service(t)
			if _, err := f.service.CreateRecord(t.Context(), c.command, c.content); !errors.Is(err, items.ErrValidation) {
				t.Errorf("CreateRecord = %v, want ErrValidation", err)
			}
			sameOps(t, f.rec)
			if f.fake.Puts() != 0 {
				t.Errorf("puts = %d, want none", f.fake.Puts())
			}
		})
	}
}

func ooxml(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	pad, err := w.CreateHeader(&zip.FileHeader{Name: "padding.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pad.Write(bytes.Repeat([]byte{0}, 64<<10)); err != nil {
		t.Fatal(err)
	}
	types, err := w.Create("[Content_Types].xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := types.Write([]byte(`<?xml version="1.0"?><Types/>`)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestService_CreateFileRoutesTheWholeContent(t *testing.T) {
	cases := []struct {
		name, fileName, mediaType string
		content                   []byte
		route                     string
		kind                      any
	}{
		{"a PDF", "report.pdf", "application/pdf", pdf, "document", "pdf"},
		{"text", "notes.txt", "text/plain", []byte("plain notes\n"), "text", nil},
		{"an OOXML package, by its central directory past the head", "deck.bin", "application/octet-stream", ooxml(t), "office", "zip"},
		{"an empty file", "empty", "application/octet-stream", []byte{}, "text", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := service(t, none(), blob(c.content, "available"), itemRow(c.content, c.route, nil))
			cmd := items.CreateFile{UploadID: uploadID, Name: c.fileName, MediaType: c.mediaType, SHA256: digest(c.content), Size: int64(len(c.content))}
			if _, err := f.service.CreateFile(t.Context(), cmd, bytes.NewReader(c.content)); err != nil {
				t.Fatal(err)
			}
			insert := callsTo(f.rec, "INSERT INTO item")
			if len(insert) != 1 {
				t.Fatalf("item inserts = %+v, want one", insert)
			}
			if got := insert[0].Args[6:8]; got[0] != c.route || got[1] != c.kind {
				t.Errorf("route, kind = %v, want %v, %v", got, c.route, c.kind)
			}
		})
	}
}

func TestService_FindReadsOneItemByID(t *testing.T) {
	f := service(t, itemRow(pdf, "document", "pdf"))
	item, err := f.service.Find(t.Context(), itemID)
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != itemID || item.SHA256 != digest(pdf) || item.Size != int64(len(pdf)) || item.Name != "report.pdf" || item.MediaType != "application/pdf" {
		t.Errorf("Find = %+v", item)
	}
	if call := f.rec.Calls()[0]; !strings.HasSuffix(call.SQL, "FROM item WHERE id = CAST($1 AS uuid)") || call.Args[0] != itemID {
		t.Errorf("find = %+v", call)
	}
}

func TestService_FindRefusals(t *testing.T) {
	f := service(t)
	if _, err := f.service.Find(t.Context(), "x"); !errors.Is(err, items.ErrValidation) {
		t.Errorf("Find = %v, want ErrValidation", err)
	}
	sameOps(t, f.rec)

	f = service(t, none())
	if _, err := f.service.Find(t.Context(), itemID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Find = %v, want sql.ErrNoRows", err)
	}
}

const contextID = "0190a5d1-7a2b-7c3d-8e4f-0000000000c1"

func contextRow(kind contexts.Kind) sqltest.Response {
	var filter driver.Value
	if kind == contexts.KindQuery {
		filter = []byte(`[{"field":"route","op":"eq","value":"text"}]`)
	}
	return sqltest.Response{
		Columns: []string{"id", "parent_id", "name", "kind", "filter", "created_at"},
		Rows:    [][]driver.Value{{contextID, contexts.Root, "c", string(kind), filter, time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)}},
	}
}

func foreignKey(constraint string) sqltest.Response {
	return sqltest.Response{Err: &sqlate.ConstraintError{Constraint: constraint, Class: sqlate.ErrForeignKeyViolation, Err: errors.New("missing")}}
}

func TestService_PlaceReadsTheContextThenInsertsTheReference(t *testing.T) {
	f := service(t, contextRow(contexts.KindExplicit), affected(1))
	if err := f.service.Place(t.Context(), contextID, itemID); err != nil {
		t.Fatal(err)
	}
	sameOps(t, f.rec, q, x)
	if find := f.rec.Calls()[0]; !strings.HasSuffix(find.SQL, "FROM context WHERE id = CAST($1 AS uuid)") || find.Args[0] != contextID {
		t.Errorf("context read = %+v", find)
	}
	insert := callsTo(f.rec, "INSERT INTO context_item")
	if len(insert) != 1 || insert[0].Args[0] != contextID || insert[0].Args[1] != itemID {
		t.Errorf("reference insert = %+v, want the context and the item", insert)
	}
}

func TestService_PlaceRepeatedChangesNothing(t *testing.T) {
	f := service(t, contextRow(contexts.KindExplicit), violation("pk_context_item"))
	if err := f.service.Place(t.Context(), contextID, itemID); err != nil {
		t.Errorf("Place = %v, want the reference left in place", err)
	}
}

func TestService_PlaceRefusals(t *testing.T) {
	cases := []struct {
		name      string
		contextID string
		itemID    string
		responses []sqltest.Response
		want      error
		ops       []sqltest.Op
	}{
		{"a malformed context id", "x", itemID, nil, items.ErrValidation, nil},
		{"a malformed item id", contextID, "x", nil, items.ErrValidation, nil},
		{"an unknown context", contextID, itemID, []sqltest.Response{none()}, items.ErrContextNotFound, []sqltest.Op{q}},
		{"a query context", contextID, itemID, []sqltest.Response{contextRow(contexts.KindQuery)}, items.ErrQueryContext, []sqltest.Op{q}},
		{"an unknown item", contextID, itemID, []sqltest.Response{contextRow(contexts.KindExplicit), foreignKey("fk_context_item_item")}, items.ErrItemNotFound, []sqltest.Op{q, x}},
		{"a context deleted after its read", contextID, itemID, []sqltest.Response{contextRow(contexts.KindExplicit), foreignKey("fk_context_item_context")}, items.ErrContextNotFound, []sqltest.Op{q, x}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := service(t, c.responses...)
			if err := f.service.Place(t.Context(), c.contextID, c.itemID); !errors.Is(err, c.want) {
				t.Errorf("Place = %v, want %v", err, c.want)
			}
			sameOps(t, f.rec, c.ops...)
		})
	}
}

func TestService_WithdrawDeletesTheReference(t *testing.T) {
	f := service(t, contextRow(contexts.KindExplicit), affected(1))
	if err := f.service.Withdraw(t.Context(), contextID, itemID); err != nil {
		t.Fatal(err)
	}
	sameOps(t, f.rec, q, x)
	del := callsTo(f.rec, "DELETE FROM context_item")
	if len(del) != 1 || del[0].Args[0] != contextID || del[0].Args[1] != itemID {
		t.Errorf("reference delete = %+v, want the context and the item", del)
	}
}

func TestService_WithdrawAnAbsentReferenceChangesNothing(t *testing.T) {
	f := service(t, contextRow(contexts.KindExplicit), affected(0), itemRow(pdf, "document", "pdf"))
	if err := f.service.Withdraw(t.Context(), contextID, itemID); err != nil {
		t.Errorf("Withdraw = %v, want the reference left absent", err)
	}
	sameOps(t, f.rec, q, x, q)
}

func TestService_WithdrawRefusals(t *testing.T) {
	cases := []struct {
		name      string
		contextID string
		itemID    string
		responses []sqltest.Response
		want      error
		ops       []sqltest.Op
	}{
		{"a malformed context id", "x", itemID, nil, items.ErrValidation, nil},
		{"a malformed item id", contextID, "x", nil, items.ErrValidation, nil},
		{"an unknown context", contextID, itemID, []sqltest.Response{none()}, items.ErrContextNotFound, []sqltest.Op{q}},
		{"a query context", contextID, itemID, []sqltest.Response{contextRow(contexts.KindQuery)}, items.ErrQueryContext, []sqltest.Op{q}},
		{"an unknown item", contextID, itemID, []sqltest.Response{contextRow(contexts.KindExplicit), affected(0), none()}, items.ErrItemNotFound, []sqltest.Op{q, x, q}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := service(t, c.responses...)
			if err := f.service.Withdraw(t.Context(), c.contextID, c.itemID); !errors.Is(err, c.want) {
				t.Errorf("Withdraw = %v, want %v", err, c.want)
			}
			sameOps(t, f.rec, c.ops...)
		})
	}
}

func page(ids ...string) sqltest.Response {
	r := sqltest.Response{Columns: itemColumns}
	for _, id := range ids {
		r.Rows = append(r.Rows, []driver.Value{id, "file", "f", "text/plain", digest(pdf), int64(1), "text", nil, nil, time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)})
	}
	return r
}

func TestService_ListPagesByIDAndContinuesByCursor(t *testing.T) {
	a, b, c := itemID[:len(itemID)-1]+"a", itemID[:len(itemID)-1]+"b", itemID[:len(itemID)-1]+"c"
	f := service(t, sqltest.WithTotal(page(a, b, c), 3))
	got, paging, err := f.service.List(t.Context(), web.Query{Page: 1, Size: 2, Filters: []web.Filter{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != a || got[1].ID != b || !paging.More || paging.Next == "" || paging.Total == nil || *paging.Total != 3 {
		t.Fatalf("first page = %+v, %+v, want a and b with a further page, a cursor, and a total of 3", got, paging)
	}
	if read := f.rec.Calls()[0]; !strings.Contains(read.SQL, "FROM item") || !strings.Contains(read.SQL, "ORDER BY q.id") {
		t.Errorf("read = %q, want every item ordered by id", read.SQL)
	}

	f.rec.Queue(sqltest.WithTotal(page(c), 3))
	got, paging, err = f.service.List(t.Context(), web.Query{Size: 2, Cursor: paging.Next, Filters: []web.Filter{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != c || paging.More {
		t.Errorf("second page = %+v, %+v, want c and no further page", got, paging)
	}
	if read := f.rec.Calls()[1]; !slices.Contains(read.Args, any(b)) {
		t.Errorf("continued read args = %v, want it past the first page's last id", read.Args)
	}
}

func TestService_ListRefusals(t *testing.T) {
	cases := []struct {
		name  string
		query web.Query
		want  error
		ops   []sqltest.Op
	}{
		{"a sort", web.Query{Page: 1, Size: 5, Sort: []web.Sort{{Field: "route"}}}, items.ErrValidation, nil},
		{"a filter", web.Query{Page: 1, Size: 5, Filters: []web.Filter{{Field: "route", Values: []string{"text"}}}}, items.ErrValidation, nil},
		{"a foreign cursor", web.Query{Size: 5, Cursor: "not-a-cursor", Filters: []web.Filter{}}, query.ErrDirectives, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := service(t)
			if _, _, err := f.service.List(t.Context(), c.query); !errors.Is(err, c.want) {
				t.Errorf("List = %v, want %v", err, c.want)
			}
			sameOps(t, f.rec, c.ops...)
		})
	}
}

func subtree(rows ...[]driver.Value) sqltest.Response {
	return sqltest.Response{Columns: []string{"id", "kind", "filter"}, Rows: rows}
}

func TestService_CoveredGathersEachFiltersMatchesThenPagesTheCoveredItems(t *testing.T) {
	a, b, c := itemID[:len(itemID)-1]+"a", itemID[:len(itemID)-1]+"b", itemID[:len(itemID)-1]+"c"
	childID := contextID[:len(contextID)-1] + "f"
	video, audio := contextID[:len(contextID)-1]+"1", contextID[:len(contextID)-1]+"2"
	f := service(t,
		subtree(
			[]driver.Value{contextID, "explicit", nil},
			[]driver.Value{childID, "explicit", nil},
			[]driver.Value{video, "query", []byte(`[{"field":"route","op":"eq","value":"video"}]`)},
			[]driver.Value{audio, "query", []byte(`[{"field":"route","op":"in","value":["audio","video"]},{"field":"duration","op":"gt","value":120}]`)},
		),
		page(a),
		page(b),
		sqltest.WithTotal(page(a, b, c), 3),
	)
	got, paging, err := f.service.Covered(t.Context(), contextID, web.Query{Page: 1, Size: 2, Filters: []web.Filter{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != a || got[1].ID != b || !paging.More || paging.Next == "" || paging.Total == nil || *paging.Total != 3 {
		t.Fatalf("page = %+v, %+v, want a and b with a further page, a cursor, and a total of 3", got, paging)
	}
	sameOps(t, f.rec, q, q, q, q)
	calls := f.rec.Calls()
	if walk := calls[0]; !strings.HasPrefix(walk.SQL, "WITH RECURSIVE down") || walk.Args[0] != contextID {
		t.Errorf("subtree = %+v", walk)
	}
	if m := calls[1]; !strings.Contains(m.SQL, "q.route = CAST($1 AS text)") || m.Args[0] != "video" {
		t.Errorf("first filter's read = %+v, want route eq video over the list read", m)
	}
	if m := calls[2]; !strings.Contains(m.SQL, "q.route IN (CAST($1 AS text), CAST($2 AS text)) AND q.duration > CAST($3 AS double precision)") || m.Args[2] != float64(120) {
		t.Errorf("second filter's read = %+v, want route in audio, video and duration gt 120", m)
	}
	read := calls[3]
	if !strings.Contains(read.SQL, "FROM context_item") || !strings.Contains(read.SQL, "ORDER BY q.id") {
		t.Errorf("covered read = %q, want the references and the matches ordered by id", read.SQL)
	}
	if contexts, matches := read.Args[0], read.Args[1]; !reflect.DeepEqual(contexts, []string{contextID, childID}) || !reflect.DeepEqual(matches, []string{a, b}) {
		t.Errorf("covered read args = %v, want the explicit contexts and every filter's matches", read.Args)
	}

	f.rec.Queue(subtree([]driver.Value{contextID, "explicit", nil}), sqltest.WithTotal(page(c), 3))
	got, paging, err = f.service.Covered(t.Context(), contextID, web.Query{Size: 2, Cursor: paging.Next, Filters: []web.Filter{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != c || paging.More {
		t.Errorf("continued page = %+v, %+v, want c and no further page", got, paging)
	}
	if read := f.rec.Calls()[5]; !slices.Contains(read.Args, any(b)) {
		t.Errorf("continued read args = %v, want it past the first page's last id", read.Args)
	}
}

func TestService_CoveredReadsEveryPageOfAFiltersMatches(t *testing.T) {
	matches := sqltest.Response{Columns: itemColumns}
	for i := range 501 {
		matches.Rows = append(matches.Rows, page(fmt.Sprintf("0190a5d1-7a2b-7c3d-8e4f-%012d", i)).Rows[0])
	}
	f := service(t,
		subtree([]driver.Value{contextID, "query", []byte(`[{"field":"kind","op":"null"}]`)}),
		matches,
		page("0190a5d1-7a2b-7c3d-8e4f-100000000000"),
		sqltest.WithTotal(page(itemID), 502),
	)
	if _, _, err := f.service.Covered(t.Context(), contextID, web.Query{Page: 1, Size: 1, Filters: []web.Filter{}}); err != nil {
		t.Fatal(err)
	}
	calls := f.rec.Calls()
	if len(calls) != 4 {
		t.Fatalf("calls = %d, want the subtree, two pages of matches, and the covered read", len(calls))
	}
	if matched := calls[3].Args[1].([]string); len(matched) != 501 {
		t.Errorf("matches = %d, want both pages' 501", len(matched))
	}
}

func TestService_CoveredRefusals(t *testing.T) {
	cases := []struct {
		name      string
		contextID string
		query     web.Query
		responses []sqltest.Response
		want      error
		ops       []sqltest.Op
	}{
		{"a malformed id", "x", web.Query{Page: 1, Size: 5}, nil, items.ErrValidation, nil},
		{"a sort", contextID, web.Query{Page: 1, Size: 5, Sort: []web.Sort{{Field: "route"}}}, nil, items.ErrValidation, nil},
		{"a filter", contextID, web.Query{Page: 1, Size: 5, Filters: []web.Filter{{Field: "route", Values: []string{"text"}}}}, nil, items.ErrValidation, nil},
		{"an unknown context", contextID, web.Query{Page: 1, Size: 5}, []sqltest.Response{subtree()}, items.ErrContextNotFound, []sqltest.Op{q}},
		{"a foreign cursor", contextID, web.Query{Size: 5, Cursor: "not-a-cursor"}, []sqltest.Response{subtree([]driver.Value{contextID, "explicit", nil})}, query.ErrDirectives, []sqltest.Op{q}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := service(t, c.responses...)
			if _, _, err := f.service.Covered(t.Context(), c.contextID, c.query); !errors.Is(err, c.want) {
				t.Errorf("Covered = %v, want %v", err, c.want)
			}
			sameOps(t, f.rec, c.ops...)
		})
	}
}

func TestService_CoveredOverAStoredFilterTheContractRefusesIsAServerError(t *testing.T) {
	f := service(t, subtree([]driver.Value{contextID, "query", []byte(`[{"field":"title","op":"null"}]`)}))
	_, _, err := f.service.Covered(t.Context(), contextID, web.Query{Page: 1, Size: 5})
	if err == nil || errors.Is(err, query.ErrDirectives) || errors.Is(err, items.ErrValidation) || !strings.Contains(err.Error(), contextID) {
		t.Errorf("Covered = %v, want an error naming the context that is not a request's refusal", err)
	}
	sameOps(t, f.rec, q)
}

func TestFilterFields_AreTheListContractLessItsKey(t *testing.T) {
	pool, _ := sqltest.Open(t)
	db := data.New(sqlate.Wrap(pool, sqltest.ReturningDialect{}), query.MustCatalog(query.Patterns()))
	want := []query.Field{
		{Name: "route", Type: "text", NotNull: true},
		{Name: "kind", Type: "text"},
		{Name: "duration", Type: "double precision"},
	}
	if got := items.FilterFields(db); !slices.Equal(got, want) {
		t.Errorf("FilterFields = %+v, want %+v", got, want)
	}
}

func TestStore_RegistersAndVerifiesEveryStatement(t *testing.T) {
	pool, rec := sqltest.Open(t)
	db := data.New(sqlate.Wrap(pool, sqltest.ReturningDialect{}), query.MustCatalog(query.Patterns()))
	items.New(db, storage.New(storagetest.NewFake(storagetest.Options{}), func() storage.Config {
		cfg := storage.Config{Container: "items"}
		_ = cfg.Finalize("")
		return cfg
	}()), contexts.New(db, items.FilterFields(db)))

	var entry *query.Statements
	for _, e := range db.Registry() {
		if e.Name == "items" {
			entry = e.Statements
		}
	}
	if entry == nil {
		t.Fatalf("registry = %v, want the items statements registered", db.Registry())
	}
	if err := db.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	prepared := rec.SQL(sqltest.OpPrepare)
	for _, st := range entry.Statements() {
		if !slices.Contains(prepared, st.Text()) {
			t.Errorf("statement %s was not verified", st.Name())
		}
	}
	var fields []string
	for _, field := range entry.Statement("list").Fields() {
		fields = append(fields, field.Name)
	}
	if want := []string{"id", "route", "kind", "duration"}; !slices.Equal(fields, want) {
		t.Errorf("list fields = %v, want %v", fields, want)
	}
}

func staleRow(content []byte) sqltest.Response {
	return sqltest.Response{Columns: []string{"sha256"}, Rows: [][]driver.Value{{digest(content)}}}
}

func putObject(t *testing.T, f fixture, content []byte) {
	t.Helper()
	if _, err := f.fake.Put(t.Context(), "sha256/"+digest(content), bytes.NewReader(content), storage.PutOptions{Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
}

func hasObject(t *testing.T, f fixture, content []byte) bool {
	t.Helper()
	_, err := f.fake.Stat(t.Context(), "sha256/"+digest(content))
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func TestPendingReclaimer_PassLocksTheStaleRowDeletesItsObjectThenTheRow(t *testing.T) {
	f := service(t, staleRow(pdf), affected(1), none())
	putObject(t, f, pdf)
	more, err := f.service.PendingReclaimer(time.Hour).Pass(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if more {
		t.Error("more = true after the claim found no further stale row, want false")
	}
	sameOps(t, f.rec, begin, q, x, commit, begin, q, commit)
	if hasObject(t, f, pdf) {
		t.Error("the stale blob's object is still stored")
	}

	claims := callsTo(f.rec, "SELECT sha256 FROM blob")
	if len(claims) != 2 {
		t.Fatalf("claims = %+v, want two", claims)
	}
	for _, want := range []string{"status = 'pending'", "updated_at < CURRENT_TIMESTAMP - CAST($1 AS interval)", "NOT EXISTS (SELECT 1 FROM item", "FETCH FIRST 1 ROWS ONLY", "FOR UPDATE SKIP LOCKED"} {
		if !strings.Contains(claims[0].SQL, want) {
			t.Errorf("claim = %q, want it to contain %q", claims[0].SQL, want)
		}
	}
	if !slices.Equal(claims[0].Args, []any{time.Hour}) {
		t.Errorf("claim args = %v, want the stale age", claims[0].Args)
	}
	if del := callsTo(f.rec, "DELETE FROM blob"); len(del) != 1 || !strings.Contains(del[0].SQL, "status = 'pending'") || !slices.Equal(del[0].Args, []any{digest(pdf)}) {
		t.Errorf("row delete = %+v, want the claimed pending row deleted by its digest", del)
	}
}

func TestPendingReclaimer_PassStopsAtTheBudget(t *testing.T) {
	other := []byte("%PDF-1.7\nanother stale blob\n%%EOF\n")
	f := service(t, staleRow(pdf), affected(1), staleRow(other), affected(1))
	putObject(t, f, pdf)
	putObject(t, f, other)
	more, err := f.service.PendingReclaimer(time.Hour).Pass(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if !more {
		t.Error("more = false after reclaiming the whole budget, want true")
	}
	sameOps(t, f.rec, begin, q, x, commit, begin, q, x, commit)
	if hasObject(t, f, pdf) || hasObject(t, f, other) {
		t.Error("a reclaimed blob's object is still stored")
	}
}

func TestPendingReclaimer_PassOverAMissingObjectDeletesTheRow(t *testing.T) {
	f := service(t, staleRow(pdf), affected(1), none())
	if more, err := f.service.PendingReclaimer(time.Hour).Pass(t.Context(), 10); err != nil || more {
		t.Fatalf("Pass = %t, %v; want no more and no error", more, err)
	}
	sameOps(t, f.rec, begin, q, x, commit, begin, q, commit)
}

func TestPendingReclaimer_PassWithNothingStaleChangesNothing(t *testing.T) {
	f := service(t, none())
	putObject(t, f, pdf)
	if more, err := f.service.PendingReclaimer(time.Hour).Pass(t.Context(), 10); err != nil || more {
		t.Fatalf("Pass = %t, %v; want no more and no error", more, err)
	}
	sameOps(t, f.rec, begin, q, commit)
	if !hasObject(t, f, pdf) {
		t.Error("an object was deleted with no stale row claimed")
	}
}

func TestPendingReclaimer_PassKeepsTheRowWhenTheObjectDeleteFails(t *testing.T) {
	f := service(t, staleRow(pdf))
	putObject(t, f, pdf)
	f.fake.SetDown(true)
	more, err := f.service.PendingReclaimer(time.Hour).Pass(t.Context(), 10)
	if !errors.Is(err, storage.ErrUnavailable) || more {
		t.Errorf("Pass = %t, %v; want no more and the object delete's failure", more, err)
	}
	sameOps(t, f.rec, begin, q, rollback)
	f.fake.SetDown(false)
	if !hasObject(t, f, pdf) {
		t.Error("the object is gone although the delete failed")
	}
}

func TestService_DeleteTouchesTheBlobThenDeletesTheReferencesAndTheItem(t *testing.T) {
	f := service(t, affected(1), affected(2), affected(1))
	if err := f.service.Delete(t.Context(), itemID); err != nil {
		t.Fatal(err)
	}
	sameOps(t, f.rec, begin, x, x, x, commit)
	calls := f.rec.Calls()
	for i, want := range []string{
		"UPDATE blob SET updated_at = CURRENT_TIMESTAMP WHERE sha256 = (SELECT i.sha256 FROM item i WHERE i.id = CAST($1 AS uuid))",
		"DELETE FROM context_item WHERE item_id = CAST($1 AS uuid)",
		"DELETE FROM item WHERE id = CAST($1 AS uuid)",
	} {
		if !strings.HasPrefix(strings.Join(strings.Fields(calls[i+1].SQL), " "), want) || !slices.Equal(calls[i+1].Args, []any{itemID}) {
			t.Errorf("statement %d = %+v, want %q over the item's id", i, calls[i+1], want)
		}
	}
}

func TestService_DeleteRefusals(t *testing.T) {
	f := service(t)
	if err := f.service.Delete(t.Context(), "x"); !errors.Is(err, items.ErrValidation) {
		t.Errorf("Delete = %v, want ErrValidation", err)
	}
	sameOps(t, f.rec)

	f = service(t, affected(0), affected(0), affected(0))
	if err := f.service.Delete(t.Context(), itemID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Delete = %v, want sql.ErrNoRows", err)
	}
	sameOps(t, f.rec, begin, x, x, x, rollback)
}

func texts(values ...string) sqltest.Response {
	rows := make([][]driver.Value, len(values))
	for i, v := range values {
		rows[i] = []driver.Value{v}
	}
	return sqltest.Response{Columns: []string{"text_sha256"}, Rows: rows}
}

var (
	sourceText = strings.Repeat("a", 64)
	chunkText  = strings.Repeat("b", 64)
)

func TestOrphanReclaimer_PassDeletesTheObjectTheContentsRecordsThenTheRow(t *testing.T) {
	f := service(t, staleRow(pdf), texts(sourceText), affected(1), texts(chunkText), affected(1), affected(1), affected(1), none())
	putObject(t, f, pdf)
	more, err := f.service.OrphanReclaimer(24*time.Hour).Pass(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if more {
		t.Error("more = true after the claim found no further orphan, want false")
	}
	sameOps(t, f.rec, begin, q, q, x, q, x, x, x, commit, begin, q, commit)
	if hasObject(t, f, pdf) {
		t.Error("the orphaned blob's object is still stored")
	}

	claims := callsTo(f.rec, "SELECT sha256 FROM blob")
	if len(claims) != 2 {
		t.Fatalf("claims = %+v, want two", claims)
	}
	for _, want := range []string{"status = 'available'", "updated_at < CURRENT_TIMESTAMP - CAST($1 AS interval)", "NOT EXISTS (SELECT 1 FROM item", "ORDER BY updated_at", "FETCH FIRST 1 ROWS ONLY", "FOR UPDATE SKIP LOCKED"} {
		if !strings.Contains(claims[0].SQL, want) {
			t.Errorf("claim = %q, want it to contain %q", claims[0].SQL, want)
		}
	}
	if !slices.Equal(claims[0].Args, []any{24 * time.Hour}) {
		t.Errorf("claim args = %v, want the stale age", claims[0].Args)
	}

	byDigest := []any{digest(pdf)}
	if c := callsTo(f.rec, "SELECT DISTINCT text_sha256 FROM source"); len(c) != 1 || !slices.Equal(c[0].Args, byDigest) {
		t.Errorf("source texts read = %+v, want one by the digest", c)
	}
	if c := callsTo(f.rec, "DELETE FROM source_occurrence"); len(c) != 1 || !strings.Contains(c[0].SQL, "input_sha256 = $1") || !slices.Equal(c[0].Args, byDigest) {
		t.Errorf("occurrence delete = %+v, want the content's occurrences as an input", c)
	}
	if c := callsTo(f.rec, "SELECT DISTINCT c.text_sha256 FROM chunk"); len(c) != 1 || !reflect.DeepEqual(c[0].Args, []any{[]string{sourceText}}) {
		t.Errorf("chunk texts read = %+v, want one over the source texts", c)
	}
	chunks := callsTo(f.rec, "DELETE FROM chunk")
	if len(chunks) != 1 || !strings.Contains(chunks[0].SQL, "NOT EXISTS (SELECT 1 FROM source s WHERE s.text_sha256 = chunk.source_text_sha256)") || !reflect.DeepEqual(chunks[0].Args, []any{[]string{sourceText}}) {
		t.Errorf("chunk delete = %+v, want the chunks of the source texts no remaining source names", chunks)
	}
	embeds := callsTo(f.rec, "DELETE FROM embedding")
	if len(embeds) != 1 || !strings.Contains(embeds[0].SQL, "NOT EXISTS (SELECT 1 FROM chunk c WHERE c.text_sha256 = embedding.chunk_text_sha256)") || !reflect.DeepEqual(embeds[0].Args, []any{[]string{chunkText}}) {
		t.Errorf("embedding delete = %+v, want the embeddings of the chunk texts no remaining chunk names", embeds)
	}
	if del := callsTo(f.rec, "DELETE FROM blob"); len(del) != 1 || !strings.Contains(del[0].SQL, "status = 'available'") || !slices.Equal(del[0].Args, byDigest) {
		t.Errorf("row delete = %+v, want the claimed available row deleted by its digest", del)
	}
}

func TestOrphanReclaimer_PassWithoutSourcesDeletesTheRow(t *testing.T) {
	f := service(t, staleRow(pdf), texts(), affected(0), affected(1), none())
	if more, err := f.service.OrphanReclaimer(time.Hour).Pass(t.Context(), 10); err != nil || more {
		t.Fatalf("Pass = %t, %v; want no more and no error", more, err)
	}
	sameOps(t, f.rec, begin, q, q, x, x, commit, begin, q, commit)
	if c := callsTo(f.rec, "DELETE FROM chunk"); len(c) != 0 {
		t.Errorf("chunk delete = %+v, want none when the content yielded no source", c)
	}
}

func TestOrphanReclaimer_PassWithoutOrphanedChunksLeavesTheEmbeddings(t *testing.T) {
	f := service(t, staleRow(pdf), texts(sourceText), affected(1), texts(), affected(0), affected(1), none())
	if more, err := f.service.OrphanReclaimer(time.Hour).Pass(t.Context(), 10); err != nil || more {
		t.Fatalf("Pass = %t, %v; want no more and no error", more, err)
	}
	sameOps(t, f.rec, begin, q, q, x, q, x, x, commit, begin, q, commit)
	if c := callsTo(f.rec, "DELETE FROM embedding"); len(c) != 0 {
		t.Errorf("embedding delete = %+v, want none when no chunk went", c)
	}
}

func TestOrphanReclaimer_PassStopsAtTheBudget(t *testing.T) {
	other := []byte("%PDF-1.7\nanother orphaned blob\n%%EOF\n")
	f := service(t, staleRow(pdf), texts(), affected(0), affected(1), staleRow(other), texts(), affected(0), affected(1))
	putObject(t, f, pdf)
	putObject(t, f, other)
	more, err := f.service.OrphanReclaimer(time.Hour).Pass(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if !more {
		t.Error("more = false after reclaiming the whole budget, want true")
	}
	sameOps(t, f.rec, begin, q, q, x, x, commit, begin, q, q, x, x, commit)
	if hasObject(t, f, pdf) || hasObject(t, f, other) {
		t.Error("a reclaimed blob's object is still stored")
	}
}

func TestOrphanReclaimer_PassWithNothingOrphanedChangesNothing(t *testing.T) {
	f := service(t, none())
	putObject(t, f, pdf)
	if more, err := f.service.OrphanReclaimer(time.Hour).Pass(t.Context(), 10); err != nil || more {
		t.Fatalf("Pass = %t, %v; want no more and no error", more, err)
	}
	sameOps(t, f.rec, begin, q, commit)
	if !hasObject(t, f, pdf) {
		t.Error("an object was deleted with no orphan claimed")
	}
}

func TestOrphanReclaimer_PassKeepsEverythingWhenTheObjectDeleteFails(t *testing.T) {
	f := service(t, staleRow(pdf))
	putObject(t, f, pdf)
	f.fake.SetDown(true)
	more, err := f.service.OrphanReclaimer(time.Hour).Pass(t.Context(), 10)
	if !errors.Is(err, storage.ErrUnavailable) || more {
		t.Errorf("Pass = %t, %v; want no more and the object delete's failure", more, err)
	}
	sameOps(t, f.rec, begin, q, rollback)
	f.fake.SetDown(false)
	if !hasObject(t, f, pdf) {
		t.Error("the object is gone although the delete failed")
	}
}
