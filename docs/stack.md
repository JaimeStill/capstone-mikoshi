# Stack

Mikoshi runs on one declared stack, with one provider per capability.

- SQL: PostgreSQL 19 with the pgvector extension, through
  [go-database](https://github.com/standards-lab/go-database) for the pool and its administration
  and [sqlate](https://github.com/standards-lab/sqlate) for the dialect and the migrator. Locally
  it runs from the compose file, an image built from
  [`compose/postgres/Dockerfile`](../compose/postgres/Dockerfile) with pgvector 0.8.7 compiled
  from source.

## Standard and native tiers

Each use of a capability runs at one of two tiers. The standard tier uses the technology's
common standard, ISO/IEC 9075 SQL for the database. The native tier uses the provider's own
features. Everything pgvector adds (the `vector` type, the `hnsw` index method, the
`vector_cosine_ops` operator class, and the distance operators) is native tier.

Only these packages import a provider or use pgvector:

- The composition root, `internal/app`. `infrastructure.go` opens the pool through go-database's
  `postgres` provider. `admin.go` wraps the pool in sqlate's `postgres` dialect and declares
  pgvector to the migrator by name, from sqlate's `postgres/pgvector`.
- The migrations, `data/migrations`, which create the `vector` column and space 1's HNSW index.
- The admin domain, `admin/database`, through go-database's admin service alone: it reports
  pgvector's status in the schema status and imports no provider.

Every other package receives the pool as a go-database `database.DB` and stays provider-free.
The integration harness and suite sit outside the service's process and reach the database
directly: the harness makes each test's database through sqlate's `postgres/pgtest`, and the
suite names pgvector from `postgres/pgvector` and runs pgvector queries to prove the index. The
boundary is a convention held in review; no linter enforces it.

## What a provider swap changes

SQL is schema-bound: moving to another SQL engine is a port, because Mikoshi owns a schema
written for PostgreSQL. A port changes the provider imports in `internal/app` and the
migrations below, and leaves the configuration's shape and the other packages as they are.

- `0001_embedding_space`: `jsonb` for a space's settings.
- `0002_embedding`: pgvector's untyped `vector` column, and the `~` regular-expression `CHECK`
  on the SHA-256 key. Another engine stores the vector as an array or in a separate vector
  store.
- `0003_space_a`: the partial `hnsw` index with `vector_cosine_ops` over the cast to
  `vector(1024)`. Another engine expresses a per-space nearest-neighbour index differently or
  not at all.
- `0004_chunker`: `jsonb` for a chunker's settings.
- `0005_source_occurrence`, `0006_source`: the `~` `CHECK`s on the SHA-256 columns.
- `0007_chunk`: the `~` `CHECK`s, and the stored generated `to_tsvector('simple', text)` column
  with its GIN index. Another engine uses its own full-text index.

Mikoshi has no statement files yet; each one the domains add joins this list when it uses a
native form.
