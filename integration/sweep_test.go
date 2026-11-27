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
