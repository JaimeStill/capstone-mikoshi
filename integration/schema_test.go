//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/JaimeStill/capstone-mikoshi/integration"
)

func TestSchema_EmptyDatabaseMigratesReady(t *testing.T) {
	db := integration.Database(t)
	s := integration.Start(t, integration.Options{Database: db})

	ready := webtest.Decode[readiness](t, s.Client().Get(t, web.ReadyPath), http.StatusOK)
	if !slices.Contains(ready.Checks, check{Name: "schema", Ready: true}) {
		t.Errorf("readiness checks = %+v, want the schema check ready", ready.Checks)
	}

	ctx := context.Background()
	var installed, offered string
	err := db.Pool.QueryRowContext(ctx,
		"SELECT e.extversion, a.default_version FROM pg_extension e JOIN pg_available_extensions a ON a.name = e.extname WHERE e.extname = 'vector'").
		Scan(&installed, &offered)
	switch {
	case err == sql.ErrNoRows:
		t.Errorf("pgvector is not installed")
	case err != nil:
		t.Fatalf("read pgvector's versions: %v", err)
	case installed != offered:
		t.Errorf("pgvector installed at %s, want the image's %s", installed, offered)
	}
}

func TestSchema_SpaceOneRegisteredWithItsIndex(t *testing.T) {
	db := integration.Database(t)
	integration.Start(t, integration.Options{Database: db})
	ctx := context.Background()

	type space struct {
		capability, servedModel, settings string
		dimensions                        int
		metric, queryPrefix               string
	}
	want := space{
		capability:  "embeddings",
		servedModel: "intfloat/multilingual-e5-large",
		settings:    `{"prefix": "passage: ", "request": "1", "input_type": "document"}`,
		dimensions:  1024,
		metric:      "cosine",
		queryPrefix: "query: ",
	}
	var got space
	err := db.Pool.QueryRowContext(ctx,
		"SELECT capability, served_model, settings::text, dimensions, metric, query_prefix FROM embedding_space WHERE id = 1").
		Scan(&got.capability, &got.servedModel, &got.settings, &got.dimensions, &got.metric, &got.queryPrefix)
	if err != nil {
		t.Fatalf("read space 1: %v", err)
	}
	if got != want {
		t.Errorf("space 1 = %+v, want %+v", got, want)
	}

	var def string
	err = db.Pool.QueryRowContext(ctx,
		"SELECT indexdef FROM pg_indexes WHERE tablename = 'embedding' AND indexname = 'ix_embedding_space_1'").Scan(&def)
	if err != nil {
		t.Fatalf("read space 1's index: %v", err)
	}
	for _, part := range []string{"USING hnsw", "(embedding)::vector(1024)", "vector_cosine_ops", "WHERE (space = 1)"} {
		if !strings.Contains(def, part) {
			t.Errorf("space 1's index = %q, want it to contain %q", def, part)
		}
	}
}

const (
	spaceOneIndex                 = "ix_embedding_space_1"
	fixturesAboveTheSortThreshold = 2000
)

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func vectorLiteral(dims int, seed float64) string {
	parts := make([]string, dims)
	for i := range parts {
		parts[i] = strconv.FormatFloat(float64((i+1)%7)+seed/float64(i+1), 'f', 6, 64)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func exec(t *testing.T, db execer, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func registerChunker(t *testing.T, db *sql.DB) int {
	t.Helper()
	var id int
	err := db.QueryRowContext(context.Background(),
		"INSERT INTO chunker (model, settings) VALUES ($1, $2) RETURNING id",
		"intfloat/multilingual-e5-large", `{"version":"2","bound":512,"runes":2000,"model":"intfloat/multilingual-e5-large"}`).
		Scan(&id)
	if err != nil {
		t.Fatalf("register a chunker: %v", err)
	}
	return id
}

func insertChunk(t *testing.T, db execer, chunker, index int, source, text string) string {
	t.Helper()
	sum := digest(text)
	exec(t, db,
		"INSERT INTO chunk (source_text_sha256, chunker, chunk_index, text, span_start, span_end, text_sha256) VALUES ($1, $2, $3, $4, $5, $6, $7)",
		digest(source), chunker, index, text, 0, len(text), sum)
	return sum
}

func TestSchema_RecordsHoldTheContract(t *testing.T) {
	db := integration.Database(t)
	integration.Start(t, integration.Options{Database: db})
	ctx := context.Background()

	input, page := digest("input"), digest("page")
	exec(t, db.Pool,
		"INSERT INTO source_occurrence (input_sha256, part_id, part_sha256, content_type, parent, depth, where_found) VALUES ($1, '/', $1, 'application/pdf', NULL, 0, NULL), ($1, '/page%2F1', $2, 'image/png', '/', 1, 'page/1')",
		input, page)
	exec(t, db.Pool,
		"INSERT INTO source (input_sha256, part_id, kind, text_sha256, stage_name, stage_version, stage_config) VALUES ($1, '/', 'output', $2, 'pdf', '1.0.0', $3), ($1, '/page%2F1', 'vision', $2, NULL, NULL, NULL)",
		input, digest("source text"), digest("config"))
	chunker := registerChunker(t, db.Pool)
	insertChunk(t, db.Pool, chunker, 0, "source text", "source text")

	var parts int
	err := db.Pool.QueryRowContext(ctx,
		"SELECT count(*) FROM source s JOIN source_occurrence o USING (input_sha256, part_id) JOIN chunk c ON c.source_text_sha256 = s.text_sha256 WHERE s.text_sha256 = $1",
		digest("source text")).Scan(&parts)
	if err != nil {
		t.Fatalf("join sources to their occurrences and chunks: %v", err)
	}
	if parts != 2 {
		t.Errorf("the source text reaches %d occurrence chunks, want 2", parts)
	}

	refused := []struct {
		name, constraint, query string
		args                    []any
	}{
		{"a chunker identity twice", "uq_chunker_identity", "INSERT INTO chunker (model, settings) VALUES ($1, $2)",
			[]any{"intfloat/multilingual-e5-large", `{"model":"intfloat/multilingual-e5-large","runes":2000,"bound":512,"version":"2"}`}},
		{"an uppercase SHA-256", "cc_source_occurrence_input_sha256", "INSERT INTO source_occurrence (input_sha256, part_id, part_sha256, content_type, depth) VALUES ($1, '/', $1, 'text/plain', 0)",
			[]any{strings.ToUpper(digest("other"))}},
		{"a found part without a parent", "cc_source_occurrence_parent_depth", "INSERT INTO source_occurrence (input_sha256, part_id, part_sha256, content_type, depth) VALUES ($1, '/x', $1, 'text/plain', 1)",
			[]any{input}},
		{"a source of no occurrence", "fk_source_occurrence", "INSERT INTO source (input_sha256, part_id, kind, text_sha256) VALUES ($1, '/', 'output', $1)",
			[]any{digest("other")}},
		{"a source of an unknown kind", "cc_source_kind", "INSERT INTO source (input_sha256, part_id, kind, text_sha256) VALUES ($1, '/', 'summary', $1)",
			[]any{input}},
		{"a chunk of no chunker", "fk_chunk_chunker", "INSERT INTO chunk (source_text_sha256, chunker, chunk_index, text, span_start, span_end, text_sha256) VALUES ($1, $2, 0, 't', 0, 1, $1)",
			[]any{digest("t"), chunker + 1}},
		{"a chunk whose span ends before it starts", "cc_chunk_span", "INSERT INTO chunk (source_text_sha256, chunker, chunk_index, text, span_start, span_end, text_sha256) VALUES ($1, $2, 1, 't', 2, 1, $1)",
			[]any{digest("t"), chunker}},
	}
	for _, r := range refused {
		_, err := db.Pool.ExecContext(ctx, r.query, r.args...)
		if want := `constraint "` + r.constraint + `"`; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s = %v, want it refused by %s", r.name, err, r.constraint)
		}
	}
}

func TestSchema_ChunkTextSearchMatches(t *testing.T) {
	db := integration.Database(t)
	integration.Start(t, integration.Options{Database: db})
	ctx := context.Background()

	chunker := registerChunker(t, db.Pool)
	want := insertChunk(t, db.Pool, chunker, 0, "source", "The quick brown fox jumps over the lazy dog")
	insertChunk(t, db.Pool, chunker, 1, "source", "A slow green turtle")

	var got []string
	rows, err := db.Pool.QueryContext(ctx, "SELECT text_sha256 FROM chunk WHERE text_search @@ to_tsquery('simple', 'quick & fox')")
	if err != nil {
		t.Fatalf("search the chunks' text: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var sum string
		if err := rows.Scan(&sum); err != nil {
			t.Fatalf("read a match: %v", err)
		}
		got = append(got, sum)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("search the chunks' text: %v", err)
	}
	if !slices.Equal(got, []string{want}) {
		t.Errorf("matches = %v, want the fox's chunk %s alone", got, want)
	}

	var def string
	err = db.Pool.QueryRowContext(ctx,
		"SELECT indexdef FROM pg_indexes WHERE tablename = 'chunk' AND indexname = 'ix_chunk_text_search'").Scan(&def)
	if err != nil {
		t.Fatalf("read the text search index: %v", err)
	}
	if !strings.Contains(def, "USING gin (text_search)") {
		t.Errorf("text search index = %q, want a GIN index over text_search", def)
	}
}

func TestSchema_NearestNeighbourInSpaceOneScansItsIndex(t *testing.T) {
	db := integration.Database(t)
	integration.Start(t, integration.Options{Database: db})
	ctx := context.Background()

	chunker := registerChunker(t, db.Pool)
	fixtures, err := db.Pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for i := range fixturesAboveTheSortThreshold {
		sum := insertChunk(t, fixtures, chunker, i, "source", fmt.Sprintf("chunk %d", i))
		exec(t, fixtures, "INSERT INTO embedding (chunk_text_sha256, space, embedding) VALUES ($1, 1, $2)",
			sum, vectorLiteral(1024, float64(i)))
	}
	if err := fixtures.Commit(); err != nil {
		t.Fatalf("commit the fixtures: %v", err)
	}
	exec(t, db.Pool, "ANALYZE embedding")

	const nearest = "SELECT chunk_text_sha256 FROM embedding WHERE space = 1 ORDER BY embedding::vector(1024) <=> $1::vector(1024) LIMIT 5"
	probe := vectorLiteral(1024, 7)

	tx, err := db.Pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("SET LOCAL enable_seqscan: %v", err)
	}
	rows, err := tx.QueryContext(ctx, "EXPLAIN "+nearest, probe)
	if err != nil {
		t.Fatalf("EXPLAIN %s: %v", nearest, err)
	}
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("read the plan: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("EXPLAIN %s: %v", nearest, err)
	}
	_ = rows.Close()
	if got := strings.Join(plan, "\n"); !strings.Contains(got, "Index Scan using "+spaceOneIndex+" ") {
		t.Errorf("plan of %s does not scan %s:\n%s", nearest, spaceOneIndex, got)
	}

	var n int
	err = tx.QueryRowContext(ctx, "SELECT count(*) FROM ("+nearest+") AS n", probe).Scan(&n)
	if err != nil {
		t.Fatalf("%s: %v", nearest, err)
	}
	if n != 5 {
		t.Errorf("%s returned %d rows, want 5", nearest, n)
	}
}

func TestSchema_WrongDimensionInSpaceOneIsRefused(t *testing.T) {
	db := integration.Database(t)
	integration.Start(t, integration.Options{Database: db})
	ctx := context.Background()

	exec(t, db.Pool,
		"INSERT INTO embedding_space (id, capability, served_model, settings, dimensions, metric, query_prefix) VALUES (2, 'embeddings', 'unindexed', '{}', 3, 'cosine', '')")
	exec(t, db.Pool, "INSERT INTO embedding (chunk_text_sha256, space, embedding) VALUES ($1, 2, $2)",
		digest("unindexed"), vectorLiteral(3, 1))

	_, err := db.Pool.ExecContext(ctx, "INSERT INTO embedding (chunk_text_sha256, space, embedding) VALUES ($1, 1, $2)",
		digest("wrong"), vectorLiteral(3, 1))
	if want := "expected 1024 dimensions"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("insert of 3 dimensions into space 1 = %v, want an error containing %q", err, want)
	}
}
