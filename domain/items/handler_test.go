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
