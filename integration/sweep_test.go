//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/sqlate/postgres/pgtest"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/capstone-mikoshi/data"
	"github.com/JaimeStill/capstone-mikoshi/domain/contexts"
	"github.com/JaimeStill/capstone-mikoshi/domain/items"
	"github.com/JaimeStill/capstone-mikoshi/domain/uploads"
	"github.com/JaimeStill/capstone-mikoshi/integration"
)

const staleAge = time.Hour

type sweepFixture struct {
	db      *pgtest.Database
	uploads *uploads.Service
	items   *items.Service
	objects *storage.Store
	bucket  storage.Client
}

func sweepDomains(t *testing.T) sweepFixture {
	t.Helper()
	db := integration.Migrated(t)
	d := data.New(db.Session(), query.MustCatalog(query.Patterns()))
	name := bucketName()
	objects := integration.Objects(t, name)
	f := sweepFixture{
		db:      db,
		uploads: uploads.New(d),
		items:   items.New(d, objects, contexts.New(d, items.FilterFields(d))),
		objects: objects,
		bucket:  integration.Bucket(t, name),
	}
	if err := d.Verify(t.Context()); err != nil {
		t.Fatalf("verify the statements: %v", err)
	}
	return f
}

func (f sweepFixture) upload(t *testing.T, id string, expired bool) {
	t.Helper()
	ctx := t.Context()
	if err := f.uploads.Create(ctx, id, "application/pdf", uploadDigest, "f.pdf", -1, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i, chunk := range [][]byte{[]byte("abc"), []byte("def")} {
		if _, ok, err := f.uploads.Append(ctx, id, int64(3*i), chunk, []byte{byte(i)}); err != nil || !ok {
			t.Fatalf("Append %s at %d = %t, %v", id, 3*i, ok, err)
		}
	}
	if expired {
		if _, err := f.db.Pool.ExecContext(ctx, "UPDATE upload SET expires_at = now() - interval '1 minute' WHERE id = $1", id); err != nil {
			t.Fatal(err)
		}
	}
}

func (f sweepFixture) blob(t *testing.T, content []byte, status string, age time.Duration, withObject bool) {
	t.Helper()
	ctx := t.Context()
	_, err := f.db.Pool.ExecContext(ctx,
		"INSERT INTO blob (sha256, size, status, updated_at) VALUES ($1, $2, $3, now() - make_interval(secs => $4))",
		hexDigest(content), len(content), status, age.Seconds())
	if err != nil {
		t.Fatal(err)
	}
	if withObject {
		if _, err := f.objects.Put(ctx, "sha256/"+hexDigest(content), bytes.NewReader(content), storage.PutOptions{Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
	}
}

func (f sweepFixture) hasObject(t *testing.T, content []byte) bool {
	t.Helper()
	_, err := f.bucket.Stat(t.Context(), "sha256/"+hexDigest(content))
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func (f sweepFixture) blobRows(t *testing.T, content []byte) int {
	t.Helper()
	return countRows(t, f.db, "SELECT COUNT(*) FROM blob WHERE sha256 = $1", hexDigest(content))
}

func TestSweep_ExpiredUploadsAndTheirChunksGoLiveOnesStay(t *testing.T) {
	f := sweepDomains(t)
	f.upload(t, "expired", true)
	f.upload(t, "live", false)

	more, err := f.uploads.Reclaimer().Pass(t.Context(), 10)
	if err != nil || more {
		t.Fatalf("Pass = %t, %v; want no more and no error", more, err)
	}
	for id, want := range map[string]int{"expired": 0, "live": 1} {
		if n := countRows(t, f.db, "SELECT COUNT(*) FROM upload WHERE id = $1", id); n != want {
			t.Errorf("upload %s rows = %d, want %d", id, n, want)
		}
		if n := chunkRows(t, f.db, id); n != 2*want {
			t.Errorf("upload %s chunks = %d, want %d", id, n, 2*want)
		}
	}
}

func TestSweep_ExpiredUploadsGoInBudgetedBatches(t *testing.T) {
	f := sweepDomains(t)
	for i := range 3 {
		f.upload(t, fmt.Sprintf("expired-%d", i), true)
	}
	r := f.uploads.Reclaimer()

	more, err := r.Pass(t.Context(), 2)
	if err != nil || !more {
		t.Fatalf("first Pass = %t, %v; want more after a full batch", more, err)
	}
	if n := countRows(t, f.db, "SELECT COUNT(*) FROM upload"); n != 1 {
		t.Errorf("uploads after a pass of budget 2 = %d, want 1", n)
	}
	more, err = r.Pass(t.Context(), 2)
	if err != nil || more {
		t.Fatalf("second Pass = %t, %v; want no more", more, err)
	}
	if n := countRows(t, f.db, "SELECT COUNT(*) FROM upload_chunk"); n != 0 {
		t.Errorf("chunks after the batches = %d, want none", n)
	}
}

func TestSweep_StalePendingBlobsGoWithTheirObjects(t *testing.T) {
	f := sweepDomains(t)
	stale := []byte("stale pending blob\n")
	staleNoObject := []byte("stale pending blob whose object was never written\n")
	fresh := []byte("fresh pending blob\n")
	available := []byte("stale available blob\n")
	f.blob(t, stale, "pending", 2*staleAge, true)
	f.blob(t, staleNoObject, "pending", 2*staleAge, false)
	f.blob(t, fresh, "pending", 0, true)
	f.blob(t, available, "available", 2*staleAge, true)

	more, err := f.items.PendingReclaimer(staleAge).Pass(t.Context(), 10)
	if err != nil || more {
		t.Fatalf("Pass = %t, %v; want no more and no error", more, err)
	}
	for _, c := range []struct {
		name    string
		content []byte
		kept    bool
	}{
		{"stale pending", stale, false},
		{"stale pending without an object", staleNoObject, false},
		{"fresh pending", fresh, true},
		{"stale available", available, true},
	} {
		want := 0
		if c.kept {
			want = 1
		}
		if n := f.blobRows(t, c.content); n != want {
			t.Errorf("%s blob rows = %d, want %d", c.name, n, want)
		}
		if got := f.hasObject(t, c.content); got != c.kept {
			t.Errorf("%s object stored = %t, want %t", c.name, got, c.kept)
		}
	}
}

func TestSweep_StalePendingBlobsGoInBudgetedPasses(t *testing.T) {
	f := sweepDomains(t)
	for i := range 3 {
		f.blob(t, fmt.Appendf(nil, "stale pending blob %d\n", i), "pending", 2*staleAge, true)
	}
	r := f.items.PendingReclaimer(staleAge)

	more, err := r.Pass(t.Context(), 2)
	if err != nil || !more {
		t.Fatalf("first Pass = %t, %v; want more after reclaiming the budget", more, err)
	}
	if n := countRows(t, f.db, "SELECT COUNT(*) FROM blob"); n != 1 {
		t.Errorf("blob rows after a pass of budget 2 = %d, want 1", n)
	}
	more, err = r.Pass(t.Context(), 2)
	if err != nil || more {
		t.Fatalf("second Pass = %t, %v; want no more", more, err)
	}
	if n := countRows(t, f.db, "SELECT COUNT(*) FROM blob"); n != 0 {
		t.Errorf("blob rows after the passes = %d, want none", n)
	}
}

func TestSweep_CreateRacingAPendingBlobPassNamesAnExistingObject(t *testing.T) {
	f := sweepDomains(t)
	r := f.items.PendingReclaimer(staleAge)
	const rounds = 12

	for round := range rounds {
		content := fmt.Appendf(nil, "created over a stale pending blob, round %d\n", round)
		f.blob(t, content, "pending", 2*staleAge, round%2 == 0)
		file := items.CreateFile{UploadID: uuid.NewV4().String(), Name: "race.txt", MediaType: "text/plain", SHA256: hexDigest(content), Size: int64(len(content))}

		var (
			wg   sync.WaitGroup
			item items.Item
		)
		start := make(chan struct{})
		wg.Go(func() {
			<-start
			var err error
			if item, err = f.items.CreateFile(context.Background(), file, bytes.NewReader(content)); err != nil {
				t.Errorf("round %d: CreateFile = %v", round, err)
			}
		})
		wg.Go(func() {
			<-start
			if _, err := r.Pass(context.Background(), 10); err != nil {
				t.Errorf("round %d: Pass = %v", round, err)
			}
		})
		close(start)
		wg.Wait()

		if item.SHA256 != hexDigest(content) {
			t.Fatalf("round %d: item = %+v, want one over the content", round, item)
		}
		if got := objectOf(t, f.bucket, item.SHA256); !bytes.Equal(got, content) {
			t.Errorf("round %d: object = %q, want the content", round, got)
		}
		if n := countRows(t, f.db, "SELECT COUNT(*) FROM blob WHERE sha256 = $1 AND status = 'available'", item.SHA256); n != 1 {
			t.Errorf("round %d: available blob rows = %d, want 1", round, n)
		}
	}

	if more, err := r.Pass(t.Context(), 10); err != nil || more {
		t.Fatalf("Pass after the races = %t, %v", more, err)
	}
	if n := countRows(t, f.db, "SELECT COUNT(*) FROM item i JOIN blob b ON b.sha256 = i.sha256 WHERE b.status = 'available'"); n != rounds {
		t.Errorf("items over available blobs = %d, want %d", n, rounds)
	}
}

func TestSweep_TheServiceSweepsExpiredUploads(t *testing.T) {
	db := integration.Database(t)
	s := integration.Start(t, integration.Options{Database: db, Env: []string{
		"MIKOSHI_SWEEP_EXPIRED_UPLOADS_INTERVAL=200ms",
	}})
	_, err := db.Pool.ExecContext(t.Context(),
		"INSERT INTO upload (id, media_type, digest, name, expires_at) VALUES ('expired', 'application/pdf', $1, 'f.pdf', now() - interval '1 minute')",
		uploadDigest)
	if err != nil {
		t.Fatal(err)
	}
	s.Await(t, "the expired upload reclaimed", func() bool {
		return countRows(t, db, "SELECT COUNT(*) FROM upload") == 0
	})
}

type occurrence struct {
	part, parent string
	depth        int
	texts        map[string]string
}

func (f sweepFixture) occurrences(t *testing.T, input string, parts ...occurrence) {
	t.Helper()
	ctx := t.Context()
	for _, p := range parts {
		var parent any
		if p.parent != "" {
			parent = p.parent
		}
		_, err := f.db.Pool.ExecContext(ctx,
			"INSERT INTO source_occurrence (input_sha256, part_id, part_sha256, content_type, parent, depth) VALUES ($1, $2, $3, 'text/plain', $4, $5)",
			input, p.part, hexDigest([]byte(input+p.part)), parent, p.depth)
		if err != nil {
			t.Fatal(err)
		}
		for kind, text := range p.texts {
			if _, err := f.db.Pool.ExecContext(ctx,
				"INSERT INTO source (input_sha256, part_id, kind, text_sha256) VALUES ($1, $2, $3, $4)",
				input, p.part, kind, hexDigest([]byte(text))); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func (f sweepFixture) chunker(t *testing.T) int {
	t.Helper()
	var id int
	if err := f.db.Pool.QueryRowContext(t.Context(),
		"INSERT INTO chunker (model, settings) VALUES ('fixture', '{}') RETURNING id").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f sweepFixture) chunks(t *testing.T, chunker int, source string, texts ...string) {
	t.Helper()
	for i, text := range texts {
		if _, err := f.db.Pool.ExecContext(t.Context(),
			"INSERT INTO chunk (source_text_sha256, chunker, chunk_index, text, span_start, span_end, text_sha256) VALUES ($1, $2, $3, $4, 0, $5, $6)",
			hexDigest([]byte(source)), chunker, i, text, len(text), hexDigest([]byte(text))); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool.ExecContext(t.Context(),
			"INSERT INTO embedding (chunk_text_sha256, space, error) VALUES ($1, 1, 'fixture') ON CONFLICT DO NOTHING",
			hexDigest([]byte(text))); err != nil {
			t.Fatal(err)
		}
	}
}

func (f sweepFixture) named(t *testing.T, content []byte, age time.Duration) {
	t.Helper()
	if _, err := f.items.CreateRecord(t.Context(), items.CreateRecord{Name: "named.json"}, content); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.ExecContext(t.Context(),
		"UPDATE blob SET updated_at = now() - make_interval(secs => $2) WHERE sha256 = $1", hexDigest(content), age.Seconds()); err != nil {
		t.Fatal(err)
	}
}

func TestSweep_OrphanedBlobsGoWithTheirObjectsAndRecords(t *testing.T) {
	f := sweepDomains(t)
	orphan := []byte("orphaned available blob\n")
	named := []byte(`{"named": "by an item"}`)
	fresh := []byte("fresh orphaned available blob\n")
	pending := []byte("stale pending blob\n")
	f.blob(t, orphan, "available", 2*staleAge, true)
	f.named(t, named, 2*staleAge)
	f.blob(t, fresh, "available", 0, true)
	f.blob(t, pending, "pending", 2*staleAge, false)

	f.occurrences(t, hexDigest(orphan),
		occurrence{part: "/", depth: 0, texts: map[string]string{"output": "shared text", "metadata": "orphan metadata"}},
		occurrence{part: "/1", parent: "/", depth: 1, texts: map[string]string{"vision": "orphan vision"}},
	)
	f.occurrences(t, hexDigest(named),
		occurrence{part: "/", depth: 0, texts: map[string]string{"output": "shared text"}},
	)
	f.occurrences(t, hexDigest(fresh),
		occurrence{part: "/", depth: 0, texts: map[string]string{"output": "fresh text"}},
	)
	chunker := f.chunker(t)
	f.chunks(t, chunker, "shared text", "shared chunk one", "shared chunk two")
	f.chunks(t, chunker, "orphan metadata", "orphan metadata chunk")
	f.chunks(t, chunker, "orphan vision", "orphan vision chunk", "shared chunk one")
	f.chunks(t, chunker, "fresh text", "fresh chunk")

	more, err := f.items.OrphanReclaimer(staleAge).Pass(t.Context(), 10)
	if err != nil || more {
		t.Fatalf("Pass = %t, %v; want no more and no error", more, err)
	}

	for _, c := range []struct {
		name    string
		content []byte
		kept    bool
		object  bool
	}{
		{"orphaned", orphan, false, false},
		{"named by an item", named, true, true},
		{"fresh orphaned", fresh, true, true},
		{"stale pending", pending, true, false},
	} {
		want := 0
		if c.kept {
			want = 1
		}
		if n := f.blobRows(t, c.content); n != want {
			t.Errorf("%s blob rows = %d, want %d", c.name, n, want)
		}
		if got := f.hasObject(t, c.content); got != c.object {
			t.Errorf("%s object stored = %t, want %t", c.name, got, c.object)
		}
	}
	if n := countRows(t, f.db, "SELECT COUNT(*) FROM source_occurrence WHERE input_sha256 = $1", hexDigest(orphan)); n != 0 {
		t.Errorf("the orphan's occurrences = %d, want none", n)
	}
	if n := countRows(t, f.db, "SELECT COUNT(*) FROM source WHERE input_sha256 = $1", hexDigest(orphan)); n != 0 {
		t.Errorf("the orphan's sources = %d, want none", n)
	}
	for input, want := range map[string]int{hexDigest(named): 1, hexDigest(fresh): 1} {
		if n := countRows(t, f.db, "SELECT COUNT(*) FROM source WHERE input_sha256 = $1", input); n != want {
			t.Errorf("sources of %s = %d, want %d", input, n, want)
		}
	}
	for source, want := range map[string]int{"shared text": 2, "orphan metadata": 0, "orphan vision": 0, "fresh text": 1} {
		if n := countRows(t, f.db, "SELECT COUNT(*) FROM chunk WHERE source_text_sha256 = $1", hexDigest([]byte(source))); n != want {
			t.Errorf("chunks of %q = %d, want %d", source, n, want)
		}
	}
	for text, want := range map[string]int{
		"shared chunk one": 1, "shared chunk two": 1, "fresh chunk": 1,
		"orphan metadata chunk": 0, "orphan vision chunk": 0,
	} {
		if n := countRows(t, f.db, "SELECT COUNT(*) FROM embedding WHERE chunk_text_sha256 = $1", hexDigest([]byte(text))); n != want {
			t.Errorf("embeddings of %q = %d, want %d", text, n, want)
		}
	}
}

func TestSweep_OrphanedBlobsGoInBudgetedPasses(t *testing.T) {
	f := sweepDomains(t)
	for i := range 3 {
		f.blob(t, fmt.Appendf(nil, "orphaned blob %d\n", i), "available", 2*staleAge, true)
	}
	r := f.items.OrphanReclaimer(staleAge)

	more, err := r.Pass(t.Context(), 2)
	if err != nil || !more {
		t.Fatalf("first Pass = %t, %v; want more after reclaiming the budget", more, err)
	}
	if n := countRows(t, f.db, "SELECT COUNT(*) FROM blob"); n != 1 {
		t.Errorf("blob rows after a pass of budget 2 = %d, want 1", n)
	}
	more, err = r.Pass(t.Context(), 2)
	if err != nil || more {
		t.Fatalf("second Pass = %t, %v; want no more", more, err)
	}
	if n := countRows(t, f.db, "SELECT COUNT(*) FROM blob"); n != 0 {
		t.Errorf("blob rows after the passes = %d, want none", n)
	}
}

func TestSweep_DeletingTheLastItemOrphansItsBlob(t *testing.T) {
	f := sweepDomains(t)
	content := []byte(`{"last": "item"}`)
	item, err := f.items.CreateRecord(t.Context(), items.CreateRecord{Name: "last.json"}, content)
	if err != nil {
		t.Fatal(err)
	}
	r := f.items.OrphanReclaimer(staleAge)
	if err := f.items.Delete(t.Context(), item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Pass(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if n := f.blobRows(t, content); n != 1 || !f.hasObject(t, content) {
		t.Fatalf("blob rows = %d, object stored = %t right after the delete; want both kept until the stale age passes", n, f.hasObject(t, content))
	}
	if _, err := f.db.Pool.ExecContext(t.Context(),
		"UPDATE blob SET updated_at = now() - make_interval(secs => $2) WHERE sha256 = $1", item.SHA256, (2 * staleAge).Seconds()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Pass(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if n := f.blobRows(t, content); n != 0 || f.hasObject(t, content) {
		t.Errorf("blob rows = %d, object stored = %t past the stale age; want both gone", n, f.hasObject(t, content))
	}
}

func TestSweep_CreateRacingAnOrphanedBlobPassNamesAnExistingObject(t *testing.T) {
	f := sweepDomains(t)
	r := f.items.OrphanReclaimer(staleAge)
	const rounds = 12

	for round := range rounds {
		content := fmt.Appendf(nil, "created over an orphaned blob, round %d\n", round)
		f.blob(t, content, "available", 2*staleAge, true)
		f.occurrences(t, hexDigest(content), occurrence{part: "/", depth: 0, texts: map[string]string{"output": string(content)}})
		file := items.CreateFile{UploadID: uuid.NewV4().String(), Name: "race.txt", MediaType: "text/plain", SHA256: hexDigest(content), Size: int64(len(content))}

		var (
			wg   sync.WaitGroup
			item items.Item
		)
		start := make(chan struct{})
		wg.Go(func() {
			<-start
			var err error
			if item, err = f.items.CreateFile(context.Background(), file, bytes.NewReader(content)); err != nil {
				t.Errorf("round %d: CreateFile = %v", round, err)
			}
		})
		wg.Go(func() {
			<-start
			if _, err := r.Pass(context.Background(), 10); err != nil {
				t.Errorf("round %d: Pass = %v", round, err)
			}
		})
		close(start)
		wg.Wait()

		if item.SHA256 != hexDigest(content) {
			t.Fatalf("round %d: item = %+v, want one over the content", round, item)
		}
		if got := objectOf(t, f.bucket, item.SHA256); !bytes.Equal(got, content) {
			t.Errorf("round %d: object = %q, want the content", round, got)
		}
		if n := countRows(t, f.db, "SELECT COUNT(*) FROM blob WHERE sha256 = $1 AND status = 'available'", item.SHA256); n != 1 {
			t.Errorf("round %d: available blob rows = %d, want 1", round, n)
		}
	}

	if more, err := r.Pass(t.Context(), 10); err != nil || more {
		t.Fatalf("Pass after the races = %t, %v", more, err)
	}
	if n := countRows(t, f.db, "SELECT COUNT(*) FROM item i JOIN blob b ON b.sha256 = i.sha256 WHERE b.status = 'available'"); n != rounds {
		t.Errorf("items over available blobs = %d, want %d", n, rounds)
	}
}
