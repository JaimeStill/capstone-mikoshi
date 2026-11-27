//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk/upload"
	"github.com/standards-lab/go-web-sdk/upload/uploadtest"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/postgres/pgtest"
	"github.com/standards-lab/sqlate/query"

	"github.com/JaimeStill/capstone-mikoshi/data"
	"github.com/JaimeStill/capstone-mikoshi/domain/uploads"
	"github.com/JaimeStill/capstone-mikoshi/integration"
)

const uploadDigest = "0000000000000000000000000000000000000000000000000000000000000000"

type replicas struct {
	stores [2]*uploads.Service
	next   atomic.Uint64
}

func (r *replicas) pick() *uploads.Service { return r.stores[r.next.Add(1)%2] }

func (r *replicas) Create(ctx context.Context, id, mediaType, digest, name string, length int64, expires time.Time) error {
	return r.pick().Create(ctx, id, mediaType, digest, name, length, expires)
}

func (r *replicas) Meta(ctx context.Context, id string) (string, string, string, error) {
	return r.pick().Meta(ctx, id)
}

func (r *replicas) Progress(ctx context.Context, id string) (int64, int64, []byte, time.Time, error) {
	return r.pick().Progress(ctx, id)
}

func (r *replicas) SetLength(ctx context.Context, id string, length int64) (int64, error) {
	return r.pick().SetLength(ctx, id, length)
}

func (r *replicas) Append(ctx context.Context, id string, offset int64, chunk, state []byte) (int64, bool, error) {
	return r.pick().Append(ctx, id, offset, chunk, state)
}

func (r *replicas) Open(ctx context.Context, id string) (io.ReadCloser, error) {
	return r.pick().Open(ctx, id)
}

func (r *replicas) Delete(ctx context.Context, id string) error {
	return r.pick().Delete(ctx, id)
}

func replicaStore(t *testing.T, db *pgtest.Database) *uploads.Service {
	t.Helper()
	pool, err := sql.Open("pgx", db.DSN)
	if err != nil {
		t.Fatalf("open a replica's pool: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	d := data.New(sqlate.Wrap(pool, postgres.Dialect{}), query.MustCatalog(query.Patterns()))
	s := uploads.New(d)
	if err := d.Verify(t.Context()); err != nil {
		t.Fatalf("verify the uploads statements: %v", err)
	}
	return s
}

func uploadReplicas(t *testing.T) (*replicas, *pgtest.Database) {
	t.Helper()
	db := integration.Migrated(t)
	return &replicas{stores: [2]*uploads.Service{replicaStore(t, db), replicaStore(t, db)}}, db
}

func chunkRows(t *testing.T, db *pgtest.Database, id string) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM upload_chunk WHERE upload_id = $1", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUploads_StoreConformsAcrossReplicas(t *testing.T) {
	uploadtest.Run(t, func() upload.Store {
		r, _ := uploadReplicas(t)
		return r
	})
}

func TestUploads_RacingAppendLoserWritesNoChunk(t *testing.T) {
	r, db := uploadReplicas(t)
	ctx := t.Context()
	for n := range 10 {
		id := "race-" + string(rune('a'+n))
		if err := r.Create(ctx, id, "application/pdf", uploadDigest, "f.pdf", -1, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var committed atomic.Int32
		start := make(chan struct{})
		for i := range 2 {
			s := r.stores[i]
			wg.Go(func() {
				<-start
				_, ok, err := s.Append(ctx, id, 0, []byte{byte(i + 1), byte(i + 1)}, []byte{byte(i)})
				if err != nil {
					t.Errorf("Append from replica %d = %v", i, err)
				}
				if ok {
					committed.Add(1)
				}
			})
		}
		close(start)
		wg.Wait()
		if got := committed.Load(); got != 1 {
			t.Fatalf("racing appends committed %d, want 1", got)
		}
		if got := chunkRows(t, db, id); got != 1 {
			t.Errorf("chunk rows after the race = %d, want the winner's alone", got)
		}
	}
}

func TestUploads_DeleteRemovesTheChunks(t *testing.T) {
	r, db := uploadReplicas(t)
	ctx := t.Context()
	if err := r.Create(ctx, "a", "application/pdf", uploadDigest, "f.pdf", 6, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, at := range []int64{0, 3} {
		if _, ok, err := r.Append(ctx, "a", at, []byte("abc"), nil); err != nil || !ok {
			t.Fatalf("Append at %d = %v, %v", at, ok, err)
		}
	}
	if got := chunkRows(t, db, "a"); got != 2 {
		t.Fatalf("chunk rows before the delete = %d, want 2", got)
	}
	if err := r.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if got := chunkRows(t, db, "a"); got != 0 {
		t.Errorf("chunk rows after the delete = %d, want none", got)
	}
}

func TestUploads_CreateReclaimsAnExpiredUploadsID(t *testing.T) {
	r, db := uploadReplicas(t)
	ctx := t.Context()
	if err := r.Create(ctx, "a", "application/pdf", uploadDigest, "old.pdf", -1, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.ExecContext(ctx, "INSERT INTO upload_chunk (upload_id, chunk_offset, content) VALUES ('a', 0, 'x')"); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(ctx, "a", "application/pdf", uploadDigest, "new.pdf", -1, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Create over an expired upload = %v", err)
	}
	if _, _, name, err := r.Meta(ctx, "a"); err != nil || name != "new.pdf" {
		t.Errorf("Meta = %q, %v; want the new upload's", name, err)
	}
	if got := chunkRows(t, db, "a"); got != 0 {
		t.Errorf("chunk rows of the reclaimed upload = %d, want none", got)
	}
}
