//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/upload"
	"github.com/standards-lab/go-web-sdk/webtest"
	"github.com/standards-lab/sqlate/postgres/pgtest"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/capstone-mikoshi/data"
	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
	"github.com/JaimeStill/capstone-mikoshi/domain/items"
	"github.com/JaimeStill/capstone-mikoshi/integration"
)

func bucketName() string {
	return "mikoshi-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func pdfOf(size int, seed string) []byte {
	head := []byte("%PDF-1.7\n% " + seed + "\n")
	return append(head, bytes.Repeat([]byte("0123456789abcdef\n"), (size-len(head))/17+1)[:size-len(head)]...)
}

func hexDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

type counting struct {
	base    http.RoundTripper
	mu      sync.Mutex
	methods []string
	drop    func(method string, n int) bool
}

func (c *counting) RoundTrip(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.methods = append(c.methods, req.Method)
	n := 0
	for _, m := range c.methods {
		if m == req.Method {
			n++
		}
	}
	drop := c.drop != nil && c.drop(req.Method, n)
	c.mu.Unlock()
	res, err := c.base.RoundTrip(req)
	if err != nil || !drop {
		return res, err
	}
	_ = res.Body.Close()
	return nil, errors.New("the response was dropped")
}

func (c *counting) count(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.methods {
		if m == method {
			n++
		}
	}
	return n
}

func uploadFile(t *testing.T, s *integration.Service, rt *counting, chunk int64, name, mediaType string, content []byte) items.Item {
	t.Helper()
	if rt == nil {
		rt = &counting{}
	}
	rt.base = http.DefaultTransport
	c := upload.Client{HTTP: &http.Client{Transport: rt, Timeout: 30 * time.Second}, Attempts: 3, ChunkSize: chunk}
	r, err := c.Create(t.Context(), s.URL()+"/api/uploads", mediaType, name, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("create the upload of %s: %v", name, err)
	}
	res, err := r.Send(t.Context())
	if err != nil {
		t.Fatalf("send %s: %v", name, err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("completing %s = %d %s, want 201", name, res.StatusCode, body)
	}
	var item items.Item
	if err := json.Unmarshal(body, &item); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if loc := res.Header.Get("Location"); loc != "/api/items/"+item.ID {
		t.Errorf("Location = %q, want /api/items/%s", loc, item.ID)
	}
	if !strings.Contains(string(body), `"duration":null`) {
		t.Errorf("body %s, want a null duration", body)
	}
	return item
}

func objectOf(t *testing.T, bucket storage.Client, digest string) []byte {
	t.Helper()
	b, err := bucket.Get(t.Context(), "sha256/"+digest, storage.GetOptions{})
	if err != nil {
		t.Fatalf("object sha256/%s: %v", digest, err)
	}
	defer func() { _ = b.Body.Close() }()
	got, err := io.ReadAll(b.Body)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func countRows(t *testing.T, db *pgtest.Database, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestItems_UploadInAppendsCreatesAFileItem(t *testing.T) {
	name := bucketName()
	db := integration.Database(t)
	s := integration.Start(t, integration.Options{Database: db, Env: []string{
		"MIKOSHI_STORAGE_CONTAINER=" + name,
		"MIKOSHI_UPLOADS_MAX_APPEND_SIZE=1024",
	}})
	content := pdfOf(5000, "appends")
	rt := &counting{}

	before := time.Now().UTC().Add(-time.Minute)
	item := uploadFile(t, s, rt, 0, "report.pdf", "application/pdf", content)

	if patches := rt.count(http.MethodPatch); patches != 5 {
		t.Errorf("appends = %d, want 5 of at most the configured 1024 bytes", patches)
	}
	id, err := uuid.Parse(item.ID)
	if err != nil || id[6]>>4 != 7 {
		t.Errorf("id %q, want a version-7 UUID", item.ID)
	}
	kind := "pdf"
	want := items.Item{
		ID: item.ID, Type: items.TypeFile, Name: "report.pdf", MediaType: "application/pdf",
		SHA256: hexDigest(content), Size: int64(len(content)), Route: "document", Kind: &kind, CreatedAt: item.CreatedAt,
	}
	if !sameItem(t, item, want) {
		t.Errorf("item = %+v, want %+v", item, want)
	}
	if item.CreatedAt.Location() != time.UTC || item.CreatedAt.Before(before) {
		t.Errorf("created_at %v, want a recent UTC time", item.CreatedAt)
	}

	read := webtest.Decode[items.Item](t, s.Client().Get(t, "/api/items/"+item.ID), http.StatusOK)
	if !sameItem(t, read, item) {
		t.Errorf("read %+v, want %+v", read, item)
	}
	if got := objectOf(t, integration.Bucket(t, name), item.SHA256); !bytes.Equal(got, content) {
		t.Errorf("object holds %d bytes, want the file's %d", len(got), len(content))
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM blob WHERE sha256 = $1 AND status = 'available'", item.SHA256); n != 1 {
		t.Errorf("available blob rows = %d, want 1", n)
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM upload"); n != 0 {
		t.Errorf("uploads = %d, want the completed upload deleted", n)
	}
}

func sameItem(t *testing.T, a, b items.Item) bool {
	t.Helper()
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(ja, jb)
}

func TestItems_UploadLimitsComeFromConfiguration(t *testing.T) {
	s := integration.Start(t, integration.Options{Env: []string{
		"MIKOSHI_UPLOADS_MAX_APPEND_SIZE=2048",
		"MIKOSHI_UPLOADS_LIFETIME=1h",
	}})
	content := []byte("limits")
	repr, err := upload.FormatDigest(hexDigest(content))
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL()+"/api/uploads", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Upload-Complete", "?0")
	req.Header.Set("Upload-Length", strconv.Itoa(len(content)))
	req.Header.Set("Repr-Digest", repr)
	req.Header.Set("Content-Type", "text/plain")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("creation = %d, want 201", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, "/api/uploads/") {
		t.Errorf("Location = %q, want the upload under /api/uploads", loc)
	}
	limit := res.Header.Get("Upload-Limit")
	var size, age int
	if _, err := fmt.Sscanf(limit, "max-append-size=%d, max-age=%d", &size, &age); err != nil || size != 2048 || age <= 3500 || age > 3600 {
		t.Errorf("Upload-Limit = %q, want max-append-size=2048 and a max-age within the configured hour", limit)
	}
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want no CORS", got)
	}
}

func TestItems_ResumedUploadCreatesTheItem(t *testing.T) {
	name := bucketName()
	s := integration.Start(t, integration.Options{Env: []string{"MIKOSHI_STORAGE_CONTAINER=" + name}})
	content := pdfOf(4096, "resumed")
	rt := &counting{drop: func(method string, n int) bool { return method == http.MethodPatch && n == 2 }}

	item := uploadFile(t, s, rt, 1000, "resumed.pdf", "application/pdf", content)

	if rt.count(http.MethodHead) == 0 {
		t.Error("the client sent no HEAD, want the upload resumed from the server's offset")
	}
	if item.SHA256 != hexDigest(content) || item.Size != int64(len(content)) || item.Route != "document" {
		t.Errorf("item = %+v, want the whole file's", item)
	}
	if got := objectOf(t, integration.Bucket(t, name), item.SHA256); !bytes.Equal(got, content) {
		t.Errorf("object holds %d bytes, want the file's %d", len(got), len(content))
	}
}

func TestItems_IdenticalContentSharesOneBlob(t *testing.T) {
	name := bucketName()
	db := integration.Database(t)
	s := integration.Start(t, integration.Options{Database: db, Env: []string{"MIKOSHI_STORAGE_CONTAINER=" + name}})
	content := []byte("the same notes, twice\n")

	first := uploadFile(t, s, nil, 0, "first.txt", "text/plain", content)
	second := uploadFile(t, s, nil, 0, "second.txt", "text/plain", content)

	if first.ID == second.ID || first.Name != "first.txt" || second.Name != "second.txt" {
		t.Errorf("items %+v and %+v, want two items with their own names", first, second)
	}
	if first.SHA256 != second.SHA256 || first.Route != "text" || first.Kind != nil {
		t.Errorf("items %+v and %+v, want one digest, routed text with no kind", first, second)
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM blob"); n != 1 {
		t.Errorf("blob rows = %d, want 1", n)
	}
	page, err := integration.Bucket(t, name).List(t.Context(), storage.ListOptions{Prefix: "sha256/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 1 || page.Objects[0].Key != "sha256/"+first.SHA256 {
		t.Errorf("objects = %+v, want the one object at sha256/%s", page.Objects, first.SHA256)
	}
}

func recordName(name string) webtest.Header {
	return webtest.Header{Name: "Content-Disposition", Value: `attachment; filename="` + name + `"`}
}

func TestItems_PostRecordCreatesARecordItem(t *testing.T) {
	name := bucketName()
	db := integration.Database(t)
	s := integration.Start(t, integration.Options{Database: db, Env: []string{"MIKOSHI_STORAGE_CONTAINER=" + name}})
	content := []byte("{\"title\":  \"field notes\",\n \"tags\": [\"a\", \"b\"]}\n")

	res := s.Client().Post(t, "/api/items", content, recordName("notes.json"))
	item := webtest.Decode[items.Item](t, res, http.StatusCreated)
	if loc := res.Header.Get("Location"); loc != "/api/items/"+item.ID {
		t.Errorf("Location = %q, want /api/items/%s", loc, item.ID)
	}
	want := items.Item{
		ID: item.ID, Type: items.TypeRecord, Name: "notes.json", MediaType: "application/json",
		SHA256: hexDigest(content), Size: int64(len(content)), Route: "text", CreatedAt: item.CreatedAt,
	}
	if !sameItem(t, item, want) {
		t.Errorf("item = %+v, want %+v", item, want)
	}
	read := webtest.Decode[items.Item](t, s.Client().Get(t, "/api/items/"+item.ID), http.StatusOK)
	if !sameItem(t, read, item) {
		t.Errorf("read %+v, want %+v", read, item)
	}
	if got := objectOf(t, integration.Bucket(t, name), item.SHA256); !bytes.Equal(got, content) {
		t.Errorf("object holds %q, want the posted bytes %q", got, content)
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM item WHERE id = $1 AND upload_id IS NULL", item.ID); n != 1 {
		t.Errorf("records with no upload id = %d, want 1", n)
	}
}

func TestItems_IdenticalRecordsAreTwoItemsOverOneBlob(t *testing.T) {
	name := bucketName()
	db := integration.Database(t)
	s := integration.Start(t, integration.Options{Database: db, Env: []string{"MIKOSHI_STORAGE_CONTAINER=" + name}})
	content := []byte(`{"same": true}`)

	first := webtest.Decode[items.Item](t, s.Client().Post(t, "/api/items", content, recordName("one.json")), http.StatusCreated)
	second := webtest.Decode[items.Item](t, s.Client().Post(t, "/api/items", content, recordName("one.json")), http.StatusCreated)

	if first.ID == second.ID || first.SHA256 != second.SHA256 {
		t.Errorf("items %+v and %+v, want two items over one digest", first, second)
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM item WHERE type = 'record'"); n != 2 {
		t.Errorf("record items = %d, want 2", n)
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM blob"); n != 1 {
		t.Errorf("blob rows = %d, want 1", n)
	}
	page, err := integration.Bucket(t, name).List(t.Context(), storage.ListOptions{Prefix: "sha256/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 1 || page.Objects[0].Key != "sha256/"+first.SHA256 {
		t.Errorf("objects = %+v, want the one object at sha256/%s", page.Objects, first.SHA256)
	}
}

func TestItems_PostRecordRefusals(t *testing.T) {
	db := integration.Database(t)
	s := integration.Start(t, integration.Options{Database: db, Env: []string{"MIKOSHI_UPLOADS_MAX_APPEND_SIZE=1024"}})
	refused := []struct {
		name    string
		body    []byte
		headers []webtest.Header
		status  int
	}{
		{"invalid JSON", []byte(`{"title": `), []webtest.Header{recordName("notes.json")}, http.StatusBadRequest},
		{"no name", []byte(`{}`), nil, http.StatusBadRequest},
		{"an empty name", []byte(`{}`), []webtest.Header{recordName("")}, http.StatusBadRequest},
		{"a body over the configured limit", bytes.Repeat([]byte(" "), 1025), []webtest.Header{recordName("big.json")}, http.StatusRequestEntityTooLarge},
	}
	for _, r := range refused {
		t.Run(r.name, func(t *testing.T) {
			if p := s.Client().Post(t, "/api/items", r.body, r.headers...).Problem(t, r.status); p.Instance != "/api/items" {
				t.Errorf("instance %q, want the request's path", p.Instance)
			}
		})
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM item"); n != 0 {
		t.Errorf("items = %d, want none", n)
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM blob"); n != 0 {
		t.Errorf("blob rows = %d, want none", n)
	}
}

func completing(t *testing.T, s *integration.Service, content []byte) string {
	t.Helper()
	repr, err := upload.FormatDigest(hexDigest(content))
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL()+"/api/uploads", bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Upload-Complete", "?0")
	req.Header.Set("Upload-Length", strconv.Itoa(len(content)))
	req.Header.Set("Repr-Digest", repr)
	req.Header.Set("Content-Digest", repr)
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Content-Disposition", `attachment; filename="overlap.txt"`)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusCreated || res.Header.Get("Upload-Offset") != strconv.Itoa(len(content)) {
		t.Fatalf("creation = %d at offset %q, want 201 with the whole file appended", res.StatusCode, res.Header.Get("Upload-Offset"))
	}
	return s.URL() + res.Header.Get("Location")
}

func TestItems_OverlappingCompletionsCreateOneItem(t *testing.T) {
	db := integration.Database(t)
	s := integration.Start(t, integration.Options{Database: db})
	const rounds, requests = 4, 6

	for round := range rounds {
		content := []byte(fmt.Sprintf("overlapping completions, round %d\n", round))
		location := completing(t, s, content)

		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			created []string
		)
		for range requests {
			wg.Go(func() {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPatch, location, http.NoBody)
				if err != nil {
					t.Error(err)
					return
				}
				req.Header.Set("Content-Type", "application/partial-upload")
				req.Header.Set("Upload-Offset", strconv.Itoa(len(content)))
				req.Header.Set("Upload-Complete", "?1")
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = res.Body.Close() }()
				body, _ := io.ReadAll(res.Body)
				switch res.StatusCode {
				case http.StatusCreated:
					var item items.Item
					if err := json.Unmarshal(body, &item); err != nil {
						t.Errorf("decode %s: %v", body, err)
						return
					}
					mu.Lock()
					created = append(created, item.ID)
					mu.Unlock()
				case http.StatusNotFound:
				default:
					t.Errorf("completion = %d %s, want 201 or, once the upload is deleted, 404", res.StatusCode, body)
				}
			})
		}
		wg.Wait()
		if len(created) == 0 || slices.ContainsFunc(created, func(id string) bool { return id != created[0] }) {
			t.Errorf("round %d: created %v, want every 201 to carry the one item", round, created)
		}
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM item"); n != rounds {
		t.Errorf("items = %d, want one per upload, %d", n, rounds)
	}
}

func itemService(t *testing.T) (*items.Service, *pgtest.Database) {
	t.Helper()
	db := integration.Migrated(t)
	d := data.New(db.Session(), query.MustCatalog(query.Patterns()))
	s := items.New(d, integration.Objects(t, bucketName()), contexts.New(d, items.FilterFields(d)))
	if err := d.Verify(t.Context()); err != nil {
		t.Fatalf("verify the items statements: %v", err)
	}
	return s, db
}

func TestItems_CompleteIsIdempotentOnTheUpload(t *testing.T) {
	s, db := itemService(t)
	content := []byte("completed more than once\n")
	file := items.CreateFile{UploadID: uuid.NewV4().String(), Name: "again.txt", MediaType: "text/plain", SHA256: hexDigest(content), Size: int64(len(content))}

	first, err := s.CreateFile(t.Context(), file, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateFile(t.Context(), file, bytes.NewReader(content))
	if err != nil || again.ID != first.ID {
		t.Errorf("repeated CreateFile = %+v, %v, want %s", again, err, first.ID)
	}

	overlapping := file
	overlapping.UploadID = uuid.NewV4().String()
	ids := make([]string, 8)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() {
			item, err := s.CreateFile(context.Background(), overlapping, bytes.NewReader(content))
			if err != nil {
				t.Errorf("CreateFile %d: %v", i, err)
			}
			ids[i] = item.ID
		})
	}
	wg.Wait()
	if slices.ContainsFunc(ids, func(id string) bool { return id != ids[0] }) {
		t.Errorf("overlapping CreateFile = %v, want one item", ids)
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM item WHERE upload_id = $1", overlapping.UploadID); n != 1 {
		t.Errorf("items of the overlapping upload = %d, want 1", n)
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM blob"); n != 1 {
		t.Errorf("blob rows = %d, want the one both uploads share", n)
	}
}

func TestItems_ReadAndPage(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()
	var ids []string
	for i := range 3 {
		ids = append(ids, uploadFile(t, s, nil, 0, fmt.Sprintf("n%d.txt", i), "text/plain", []byte(fmt.Sprintf("note %d\n", i))).ID)
	}

	first := webtest.Decode[web.Page[items.Item]](t, c.Get(t, "/api/items?size=2"), http.StatusOK)
	if got := itemIDs(first.Items); !slices.Equal(got, ids[:2]) || !first.More || first.Total == nil || *first.Total != 3 || first.Next == "" {
		t.Fatalf("first page = %v more=%v total=%v next=%q, want the first two items in creation order of 3", got, first.More, first.Total, first.Next)
	}
	next := webtest.Decode[web.Page[items.Item]](t, c.Get(t, "/api/items?size=2&cursor="+first.Next), http.StatusOK)
	if got := itemIDs(next.Items); !slices.Equal(got, ids[2:]) || next.More {
		t.Errorf("continued page = %v more=%v, want the third item and no further page", got, next.More)
	}
	if last := webtest.Decode[web.Page[items.Item]](t, c.Get(t, "/api/items?page=2&size=2"), http.StatusOK); !slices.Equal(itemIDs(last.Items), ids[2:]) {
		t.Errorf("page 2 = %v, want the third item", itemIDs(last.Items))
	}

	refused := []struct {
		path   string
		status int
	}{
		{"/api/items?sort=route", http.StatusBadRequest},
		{"/api/items?route=text", http.StatusBadRequest},
		{"/api/items?cursor=not-a-cursor", http.StatusBadRequest},
		{"/api/items/not-a-uuid", http.StatusBadRequest},
		{"/api/items/" + uuid.NewV7().String(), http.StatusNotFound},
	}
	for _, r := range refused {
		if p := c.Get(t, r.path).Problem(t, r.status); p.Instance != strings.SplitN(r.path, "?", 2)[0] {
			t.Errorf("%s: instance %q, want the request's path", r.path, p.Instance)
		}
	}
}

func itemIDs(list []items.Item) []string {
	out := make([]string, len(list))
	for i, item := range list {
		out[i] = item.ID
	}
	return out
}

func TestItems_SchemaHoldsTheItemRules(t *testing.T) {
	db := integration.Migrated(t)
	ctx := t.Context()
	digest := hexDigest([]byte("rules"))
	if _, err := db.Pool.ExecContext(ctx, `INSERT INTO blob (sha256, size, status) VALUES ($1, 5, 'available')`, digest); err != nil {
		t.Fatal(err)
	}
	item := func(id, typ, sha, upload string) string {
		return `INSERT INTO item (id, type, name, media_type, sha256, size, route, upload_id) VALUES ('` + id + `', '` + typ + `', 'n', 'text/plain', '` + sha + `', 5, 'text', ` + upload + `)`
	}
	first := uuid.NewV7().String()
	if _, err := db.Pool.ExecContext(ctx, item(first, "file", digest, "'u1'")); err != nil {
		t.Fatal(err)
	}

	refused := []struct {
		name, constraint, query string
	}{
		{"an analysis item", "cc_item_type", item(uuid.NewV7().String(), "analysis", digest, "NULL")},
		{"an item over no blob", "fk_item_blob", item(uuid.NewV7().String(), "file", hexDigest([]byte("none")), "NULL")},
		{"an item without a media type", "cc_item_media_type", strings.Replace(item(uuid.NewV7().String(), "file", digest, "NULL"), "'text/plain'", "''", 1)},
		{"a second item for one upload", "uq_item_upload", item(uuid.NewV7().String(), "file", digest, "'u1'")},
		{"a blob in another status", "cc_blob_status", `INSERT INTO blob (sha256, size, status) VALUES ('` + hexDigest([]byte("x")) + `', 1, 'deleting')`},
		{"a blob an item names deleted", "fk_item_blob", `DELETE FROM blob WHERE sha256 = '` + digest + `'`},
	}
	for _, r := range refused {
		_, err := db.Pool.ExecContext(ctx, r.query)
		if want := `constraint "` + r.constraint + `"`; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s = %v, want it refused by %s", r.name, err, r.constraint)
		}
	}
}

func postRecord(t *testing.T, c *webtest.Client, name string) items.Item {
	t.Helper()
	return webtest.Decode[items.Item](t, c.Post(t, "/api/items", `{"name":"`+name+`"}`, recordName(name+".json")), http.StatusCreated)
}

func referencePath(contextID, itemID string) string {
	return "/api/contexts/" + contextID + "/items/" + itemID
}

func references(t *testing.T, db *pgtest.Database, contextID string) int {
	t.Helper()
	return countRows(t, db, "SELECT COUNT(*) FROM context_item WHERE context_id = $1", contextID)
}

func TestItems_ReferencePlacedAndWithdrawnIdempotently(t *testing.T) {
	db := integration.Database(t)
	c := integration.Start(t, integration.Options{Database: db, Env: []string{"MIKOSHI_STORAGE_CONTAINER=" + bucketName()}}).Client()
	item := postRecord(t, c, "placed")
	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	path := referencePath(folder.ID, item.ID)

	for range 2 {
		if res := c.Put(t, path, nil).Expect(t, http.StatusNoContent); len(res.Body) != 0 {
			t.Errorf("body = %s, want none", res.Body)
		}
		if n := references(t, db, folder.ID); n != 1 {
			t.Errorf("references = %d, want 1", n)
		}
	}
	c.Put(t, referencePath(contexts.Root, item.ID), nil).Expect(t, http.StatusNoContent)
	if n := countRows(t, db, "SELECT COUNT(*) FROM context_item WHERE item_id = $1", item.ID); n != 2 {
		t.Errorf("references of the item = %d, want one in each of 2 contexts", n)
	}

	for range 2 {
		if res := c.Delete(t, path).Expect(t, http.StatusNoContent); len(res.Body) != 0 {
			t.Errorf("body = %s, want none", res.Body)
		}
		if n := references(t, db, folder.ID); n != 0 {
			t.Errorf("references = %d, want none", n)
		}
	}
	if n := references(t, db, contexts.Root); n != 1 {
		t.Errorf("references in the root = %d, want the one left in place", n)
	}
}

func TestItems_ReferenceRefusals(t *testing.T) {
	db := integration.Database(t)
	c := integration.Start(t, integration.Options{Database: db, Env: []string{"MIKOSHI_STORAGE_CONTAINER=" + bucketName()}}).Client()
	item := postRecord(t, c, "refused")
	saved := createContext(t, c, queryContext(contexts.Root, "saved", `[{"field":"route","op":"eq","value":"text"}]`))
	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	unknownContext := uuid.NewV7().String()
	unknownItem := uuid.NewV7().String()

	refused := []struct {
		name   string
		path   string
		status int
		detail string
	}{
		{"a query context", referencePath(saved.ID, item.ID), http.StatusConflict, items.ErrQueryContext.Error()},
		{"an unknown context", referencePath(unknownContext, item.ID), http.StatusNotFound, items.ErrContextNotFound.Error()},
		{"an unknown item", referencePath(folder.ID, unknownItem), http.StatusNotFound, items.ErrItemNotFound.Error()},
		{"an unknown context and item", referencePath(unknownContext, unknownItem), http.StatusNotFound, items.ErrContextNotFound.Error()},
		{"a malformed context id", referencePath("not-a-uuid", item.ID), http.StatusBadRequest, ""},
		{"a malformed item id", referencePath(folder.ID, "not-a-uuid"), http.StatusBadRequest, ""},
	}
	for _, r := range refused {
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			t.Run(method+" "+r.name, func(t *testing.T) {
				p := c.Do(t, method, r.path, nil).Problem(t, r.status)
				if p.Instance != r.path {
					t.Errorf("instance = %q, want %s", p.Instance, r.path)
				}
				if r.detail != "" && p.Detail != r.detail {
					t.Errorf("detail = %q, want %q", p.Detail, r.detail)
				}
			})
		}
	}
	if n := countRows(t, db, "SELECT COUNT(*) FROM context_item"); n != 0 {
		t.Errorf("references = %d, want none", n)
	}
}

func TestItems_DeletingAContextRemovesItsSubtreesReferences(t *testing.T) {
	db := integration.Database(t)
	c := integration.Start(t, integration.Options{Database: db, Env: []string{"MIKOSHI_STORAGE_CONTAINER=" + bucketName()}}).Client()
	item := postRecord(t, c, "held")
	other := postRecord(t, c, "other")
	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	child := createContext(t, c, explicit(folder.ID, "child"))
	grandchild := createContext(t, c, explicit(child.ID, "grandchild"))
	sibling := createContext(t, c, explicit(contexts.Root, "sibling"))
	for _, id := range []string{folder.ID, child.ID, grandchild.ID, sibling.ID} {
		c.Put(t, referencePath(id, item.ID), nil).Expect(t, http.StatusNoContent)
	}
	c.Put(t, referencePath(grandchild.ID, other.ID), nil).Expect(t, http.StatusNoContent)

	c.Delete(t, "/api/contexts/"+child.ID+"?confirm=1").Expect(t, http.StatusNoContent)
	for _, id := range []string{child.ID, grandchild.ID} {
		if n := references(t, db, id); n != 0 {
			t.Errorf("references in the deleted %s = %d, want none", id, n)
		}
	}
	for _, id := range []string{folder.ID, sibling.ID} {
		if n := references(t, db, id); n != 1 {
			t.Errorf("references in the kept %s = %d, want 1", id, n)
		}
	}

	c.Delete(t, "/api/contexts/"+folder.ID).Expect(t, http.StatusNoContent)
	if n := countRows(t, db, "SELECT COUNT(*) FROM context_item"); n != 1 {
		t.Errorf("references = %d, want the sibling's alone", n)
	}
	for _, id := range []string{item.ID, other.ID} {
		c.Get(t, "/api/items/"+id).Expect(t, http.StatusOK)
	}
}

func TestItems_ReferenceRacingAContextDeleteIsNeverAServerError(t *testing.T) {
	db := integration.Database(t)
	c := integration.Start(t, integration.Options{Database: db, Env: []string{"MIKOSHI_STORAGE_CONTAINER=" + bucketName()}}).Client()
	item := postRecord(t, c, "raced")

	for i := range 20 {
		folder := createContext(t, c, explicit(contexts.Root, fmt.Sprintf("raced %d", i)))
		var placed, deleted int
		var wg sync.WaitGroup
		wg.Go(func() { placed = c.Put(t, referencePath(folder.ID, item.ID), nil).Status })
		wg.Go(func() { deleted = c.Delete(t, "/api/contexts/"+folder.ID).Status })
		wg.Wait()
		if placed != http.StatusNoContent && placed != http.StatusNotFound {
			t.Errorf("round %d: placing = %d, want 204 or 404", i, placed)
		}
		if deleted != http.StatusNoContent {
			t.Errorf("round %d: delete = %d, want 204", i, deleted)
		}
		if n := references(t, db, folder.ID); n != 0 {
			t.Errorf("round %d: references in the deleted context = %d, want none", i, n)
		}
	}
}

func TestItems_SchemaHoldsTheReferenceRules(t *testing.T) {
	db := integration.Migrated(t)
	ctx := t.Context()
	digest := hexDigest([]byte("referenced"))
	itemID := uuid.NewV7().String()
	for _, stmt := range []string{
		`INSERT INTO blob (sha256, size, status) VALUES ('` + digest + `', 10, 'available')`,
		`INSERT INTO item (id, type, name, media_type, sha256, size, route) VALUES ('` + itemID + `', 'record', 'n', 'application/json', '` + digest + `', 10, 'text')`,
		`INSERT INTO context_item (context_id, item_id) VALUES ('` + contexts.Root + `', '` + itemID + `')`,
	} {
		if _, err := db.Pool.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	refused := []struct {
		name, constraint, query string
	}{
		{"a reference placed twice", "pk_context_item", `INSERT INTO context_item (context_id, item_id) VALUES ('` + contexts.Root + `', '` + itemID + `')`},
		{"a reference to no context", "fk_context_item_context", `INSERT INTO context_item (context_id, item_id) VALUES ('` + uuid.NewV7().String() + `', '` + itemID + `')`},
		{"a reference to no item", "fk_context_item_item", `INSERT INTO context_item (context_id, item_id) VALUES ('` + contexts.Root + `', '` + uuid.NewV7().String() + `')`},
		{"a referenced item deleted", "fk_context_item_item", `DELETE FROM item WHERE id = '` + itemID + `'`},
	}
	for _, r := range refused {
		_, err := db.Pool.ExecContext(ctx, r.query)
		if want := `constraint "` + r.constraint + `"`; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s = %v, want it refused by %s", r.name, err, r.constraint)
		}
	}
}

func coveredPath(contextID string) string {
	return "/api/contexts/" + contextID + "/items"
}

func covered(t *testing.T, c *webtest.Client, contextID string) []string {
	t.Helper()
	page := webtest.Decode[web.Page[items.Item]](t, c.Get(t, coveredPath(contextID)+"?size=100"), http.StatusOK)
	if page.More {
		t.Fatalf("%s: a further page, want every covered item on one", contextID)
	}
	return itemIDs(page.Items)
}

func sortedIDs(list ...items.Item) []string {
	return slices.Sorted(slices.Values(itemIDs(list)))
}

func mp4Of(seed string) []byte {
	return append([]byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2"), []byte(seed)...)
}

func TestItems_ContextCoversItsSubtreesReferencesAndQueryMatches(t *testing.T) {
	db := integration.Database(t)
	s := integration.Start(t, integration.Options{Database: db, Env: []string{"MIKOSHI_STORAGE_CONTAINER=" + bucketName()}})
	c := s.Client()
	clip := uploadFile(t, s, nil, 0, "clip.mp4", "video/mp4", mp4Of("clip"))
	film := uploadFile(t, s, nil, 0, "film.mp4", "video/mp4", mp4Of("film"))
	talk := uploadFile(t, s, nil, 0, "talk.wav", "audio/wav", append([]byte("RIFF\x64\x00\x00\x00WAVEfmt "), make([]byte, 32)...))
	photo := uploadFile(t, s, nil, 0, "photo.png", "image/png", append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), make([]byte, 32)...))
	notes := uploadFile(t, s, nil, 0, "notes.txt", "text/plain", []byte("notes\n"))
	record := postRecord(t, c, "record")
	loose := postRecord(t, c, "loose")
	for item, want := range map[*items.Item]string{&clip: "video", &film: "video", &talk: "audio", &photo: "vision", &notes: "text", &record: "text"} {
		if item.Route != want {
			t.Fatalf("%s's route = %q, want %q", item.Name, item.Route, want)
		}
	}

	library := createContext(t, c, explicit(contexts.Root, "library"))
	shelf := createContext(t, c, explicit(library.ID, "shelf"))
	box := createContext(t, c, explicit(shelf.ID, "box"))
	videos := createContext(t, c, queryContext(shelf.ID, "videos", `[{"field":"route","op":"eq","value":"video"}]`))
	long := createContext(t, c, queryContext(library.ID, "long", `[{"field":"route","op":"in","value":["audio","video"]},{"field":"duration","op":"gt","value":120}]`))
	pictures := createContext(t, c, queryContext(contexts.Root, "pictures", `[{"field":"route","op":"in","value":["vision","vision-converted"]}]`))
	elsewhere := createContext(t, c, explicit(contexts.Root, "elsewhere"))
	for _, ref := range []struct{ context, item string }{
		{library.ID, notes.ID},
		{shelf.ID, record.ID},
		{shelf.ID, clip.ID},
		{box.ID, notes.ID},
		{box.ID, photo.ID},
		{elsewhere.ID, loose.ID},
	} {
		c.Put(t, referencePath(ref.context, ref.item), nil).Expect(t, http.StatusNoContent)
	}

	cases := []struct {
		name    string
		context string
		want    []string
	}{
		{"the root", contexts.Root, sortedIDs(clip, film, photo, notes, record, loose)},
		{"an explicit parent", library.ID, sortedIDs(clip, film, photo, notes, record)},
		{"an explicit child", shelf.ID, sortedIDs(clip, film, photo, notes, record)},
		{"an explicit leaf", box.ID, sortedIDs(photo, notes)},
		{"a query context", videos.ID, sortedIDs(clip, film)},
		{"a query context over a null duration", long.ID, nil},
		{"a query context on the root", pictures.ID, sortedIDs(photo)},
		{"a context with no reference", createContext(t, c, explicit(contexts.Root, "empty")).ID, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := covered(t, c, tc.context); !slices.Equal(got, tc.want) {
				t.Errorf("covered = %v, want %v, each item once in creation order", got, tc.want)
			}
		})
	}

	if _, err := db.Pool.ExecContext(t.Context(), "UPDATE item SET duration = 300 WHERE id = $1", talk.ID); err != nil {
		t.Fatal(err)
	}
	if got, want := covered(t, c, long.ID), sortedIDs(talk); !slices.Equal(got, want) {
		t.Errorf("covered by duration gt 120 = %v, want the audio item ingest timed at 300 seconds", got)
	}
	if got, want := covered(t, c, library.ID), sortedIDs(clip, film, talk, photo, notes, record); !slices.Equal(got, want) {
		t.Errorf("library covers %v, want its long query's match too, %v", got, want)
	}
}

func TestItems_ContextCoverPages(t *testing.T) {
	s := integration.Start(t, integration.Options{Env: []string{"MIKOSHI_STORAGE_CONTAINER=" + bucketName()}})
	c := s.Client()
	folder := createContext(t, c, explicit(contexts.Root, "folder"))
	child := createContext(t, c, explicit(folder.ID, "child"))
	createContext(t, c, queryContext(folder.ID, "videos", `[{"field":"route","op":"eq","value":"video"}]`))
	var all []items.Item
	for i := range 3 {
		item := postRecord(t, c, fmt.Sprintf("r%d", i))
		c.Put(t, referencePath(folder.ID, item.ID), nil).Expect(t, http.StatusNoContent)
		c.Put(t, referencePath(child.ID, item.ID), nil).Expect(t, http.StatusNoContent)
		all = append(all, item)
	}
	all = append(all, uploadFile(t, s, nil, 0, "v.mp4", "video/mp4", mp4Of("v")))
	ids := sortedIDs(all...)
	path := coveredPath(folder.ID)

	first := webtest.Decode[web.Page[items.Item]](t, c.Get(t, path+"?size=3"), http.StatusOK)
	if got := itemIDs(first.Items); !slices.Equal(got, ids[:3]) || !first.More || first.Total == nil || *first.Total != 4 || first.Next == "" {
		t.Fatalf("first page = %v more=%v total=%v next=%q, want the first three of 4, each once", got, first.More, first.Total, first.Next)
	}
	next := webtest.Decode[web.Page[items.Item]](t, c.Get(t, path+"?size=3&cursor="+first.Next), http.StatusOK)
	if got := itemIDs(next.Items); !slices.Equal(got, ids[3:]) || next.More {
		t.Errorf("continued page = %v more=%v, want the fourth item and no further page", got, next.More)
	}
	if last := webtest.Decode[web.Page[items.Item]](t, c.Get(t, path+"?page=2&size=3"), http.StatusOK); !slices.Equal(itemIDs(last.Items), ids[3:]) {
		t.Errorf("page 2 = %v, want the fourth item", itemIDs(last.Items))
	}
	if found := webtest.Decode[items.Item](t, c.Get(t, "/api/items/"+ids[3]), http.StatusOK); !sameItem(t, found, next.Items[0]) {
		t.Errorf("covered item = %+v, want the read by id's %+v", next.Items[0], found)
	}
	listed := webtest.Decode[web.Page[items.Item]](t, c.Get(t, "/api/items?size=1"), http.StatusOK)

	refused := []struct {
		path   string
		status int
		detail string
	}{
		{coveredPath(uuid.NewV7().String()), http.StatusNotFound, items.ErrContextNotFound.Error()},
		{coveredPath("not-a-uuid"), http.StatusBadRequest, ""},
		{path + "?sort=route", http.StatusBadRequest, ""},
		{path + "?route=video", http.StatusBadRequest, ""},
		{path + "?cursor=not-a-cursor", http.StatusBadRequest, ""},
		{path + "?cursor=" + listed.Next, http.StatusBadRequest, ""},
	}
	for _, r := range refused {
		p := c.Get(t, r.path).Problem(t, r.status)
		if p.Instance != strings.SplitN(r.path, "?", 2)[0] {
			t.Errorf("%s: instance %q, want the request's path", r.path, p.Instance)
		}
		if r.detail != "" && p.Detail != r.detail {
			t.Errorf("%s: detail %q, want %q", r.path, p.Detail, r.detail)
		}
	}
}
