package items_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
	"github.com/JaimeStill/capstone-mikoshi/domain/items"
)

const recordLimit = 64

func routes(t *testing.T, responses ...sqltest.Response) (http.Handler, fixture) {
	t.Helper()
	f := service(t, responses...)
	r := web.NewRouter()
	r.Mount(web.NewModule(items.Routes(f.service, web.Limits{DefaultSize: 10, MaxSize: 100}, web.NewTransfer(recordLimit, 1<<20, time.Second), slog.New(slog.DiscardHandler))))
	return r, f
}

func referenceRoutes(t *testing.T, responses ...sqltest.Response) (http.Handler, fixture) {
	t.Helper()
	f := service(t, responses...)
	r := web.NewRouter()
	r.Mount(web.NewModule(items.ReferenceRoutes(f.service, slog.New(slog.DiscardHandler))))
	return r, f
}

func serve(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func post(t *testing.T, h http.Handler, disposition string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/items", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if disposition != "" {
		r.Header.Set("Content-Disposition", disposition)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestRoutes_PostRecordCreatesARecordItem(t *testing.T) {
	h, f := routes(t, none(), none(), affected(1), affected(1), recordRow(record))
	res := post(t, h, `attachment; filename="notes.json"`, record)
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", res.Code, res.Body)
	}
	if loc := res.Header().Get("Location"); loc != "/items/"+itemID {
		t.Errorf("Location = %q, want /items/%s", loc, itemID)
	}
	var item items.Item
	if err := json.Unmarshal(res.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.ID != itemID || item.Type != items.TypeRecord {
		t.Errorf("item = %+v, want the created record", item)
	}
	if !bytes.Equal(stored(t, f, record), record) {
		t.Error("the stored object is not the posted bytes")
	}
	if insert := callsTo(f.rec, "INSERT INTO item"); len(insert) != 1 || insert[0].Args[2] != "notes.json" {
		t.Errorf("item insert = %+v, want the Content-Disposition file name", insert)
	}
}

func TestRoutes_PostRecordReadsAnEncodedFileName(t *testing.T) {
	h, f := routes(t, none(), none(), affected(1), affected(1), recordRow(record))
	if res := post(t, h, `attachment; filename*=UTF-8''%E7%A5%9E%E8%BC%BF.json`, record); res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", res.Code, res.Body)
	}
	if insert := callsTo(f.rec, "INSERT INTO item"); len(insert) != 1 || insert[0].Args[2] != "神輿.json" {
		t.Errorf("item insert = %+v, want the decoded file name", insert)
	}
}

func TestRoutes_PostRecordRefusals(t *testing.T) {
	cases := []struct {
		name        string
		disposition string
		body        []byte
		status      int
	}{
		{"no Content-Disposition", "", record, http.StatusBadRequest},
		{"no file name", "attachment", record, http.StatusBadRequest},
		{"an empty file name", `attachment; filename=""`, record, http.StatusBadRequest},
		{"a malformed Content-Disposition", `attachment; filename="notes.json`, record, http.StatusBadRequest},
		{"a body that is not JSON", `attachment; filename="notes.json"`, []byte(`{"title": `), http.StatusBadRequest},
		{"an empty body", `attachment; filename="notes.json"`, nil, http.StatusBadRequest},
		{"a body over the limit", `attachment; filename="notes.json"`, bytes.Repeat([]byte(" "), recordLimit+1), http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, f := routes(t)
			res := post(t, h, c.disposition, c.body)
			if res.Code != c.status || res.Header().Get("Content-Type") != "application/problem+json" {
				t.Errorf("status = %d (%s), want a %d problem", res.Code, res.Header().Get("Content-Type"), c.status)
			}
			sameOps(t, f.rec)
			if f.fake.Puts() != 0 {
				t.Errorf("puts = %d, want none", f.fake.Puts())
			}
		})
	}
}

func TestReferenceRoutes_PlaceAndWithdrawAnswerNoContent(t *testing.T) {
	path := "/contexts/" + contextID + "/items/" + itemID
	cases := []struct {
		name      string
		method    string
		responses []sqltest.Response
	}{
		{"a placing", http.MethodPut, []sqltest.Response{contextRow(contexts.KindExplicit), affected(1)}},
		{"a repeated placing", http.MethodPut, []sqltest.Response{contextRow(contexts.KindExplicit), violation("pk_context_item")}},
		{"a withdrawing", http.MethodDelete, []sqltest.Response{contextRow(contexts.KindExplicit), affected(1)}},
		{"a withdrawing of an absent reference", http.MethodDelete, []sqltest.Response{contextRow(contexts.KindExplicit), affected(0), itemRow(pdf, "document", "pdf")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, _ := referenceRoutes(t, c.responses...)
			if res := serve(h, c.method, path); res.Code != http.StatusNoContent || res.Body.Len() != 0 {
				t.Errorf("status = %d (body %s), want 204 with no body", res.Code, res.Body)
			}
		})
	}
}

func TestReferenceRoutes_Refusals(t *testing.T) {
	path := "/contexts/" + contextID + "/items/" + itemID
	cases := []struct {
		name      string
		method    string
		path      string
		responses []sqltest.Response
		status    int
		detail    string
	}{
		{"a malformed context id", http.MethodPut, "/contexts/x/items/" + itemID, nil, http.StatusBadRequest, ""},
		{"a malformed item id", http.MethodDelete, "/contexts/" + contextID + "/items/x", nil, http.StatusBadRequest, ""},
		{"an unknown context", http.MethodPut, path, []sqltest.Response{none()}, http.StatusNotFound, items.ErrContextNotFound.Error()},
		{"an unknown item", http.MethodPut, path, []sqltest.Response{contextRow(contexts.KindExplicit), foreignKey("fk_context_item_item")}, http.StatusNotFound, items.ErrItemNotFound.Error()},
		{"a context deleted after its read", http.MethodPut, path, []sqltest.Response{contextRow(contexts.KindExplicit), foreignKey("fk_context_item_context")}, http.StatusNotFound, items.ErrContextNotFound.Error()},
		{"a query context", http.MethodPut, path, []sqltest.Response{contextRow(contexts.KindQuery)}, http.StatusConflict, items.ErrQueryContext.Error()},
		{"a withdrawing from a query context", http.MethodDelete, path, []sqltest.Response{contextRow(contexts.KindQuery)}, http.StatusConflict, items.ErrQueryContext.Error()},
		{"a withdrawing of an unknown item", http.MethodDelete, path, []sqltest.Response{contextRow(contexts.KindExplicit), affected(0), none()}, http.StatusNotFound, items.ErrItemNotFound.Error()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, _ := referenceRoutes(t, c.responses...)
			res := serve(h, c.method, c.path)
			if res.Code != c.status || res.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatalf("status = %d (%s), want a %d problem", res.Code, res.Header().Get("Content-Type"), c.status)
			}
			var p web.Problem
			if err := json.Unmarshal(res.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
			if c.detail != "" && p.Detail != c.detail {
				t.Errorf("detail = %q, want %q", p.Detail, c.detail)
			}
		})
	}
}
