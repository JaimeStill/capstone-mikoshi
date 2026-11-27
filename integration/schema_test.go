//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"net/http"
	"slices"
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
