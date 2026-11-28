# Stack

Mikoshi runs on one declared stack, with one provider per capability.

- SQL: PostgreSQL 19 with the pgvector extension, through
  [go-database](https://github.com/standards-lab/go-database) for the pool and its administration
  and [sqlate](https://github.com/standards-lab/sqlate) for the dialect and the migrator. Locally
  it runs in the image the compose file builds from
  [`compose/postgres/Dockerfile`](../compose/postgres/Dockerfile), with pgvector 0.8.7 compiled
  from source.
- Object storage: an S3 bucket, through [go-storage](https://github.com/standards-lab/go-storage)'s
  `s3` provider. Locally it is SeaweedFS's S3 gateway, in the image the compose file builds from
  [`compose/seaweedfs/Dockerfile`](../compose/seaweedfs/Dockerfile), or the capstone stack's
  gateway through `MIKOSHI_STORAGE_ENDPOINT=http://127.0.0.1:8335`. Mikoshi creates its bucket at
  startup.
- Messaging: NATS with JetStream, through
  [go-messaging](https://github.com/standards-lab/go-messaging)'s runtime, its `nats` provider
  for the broker, and its `postgres` engine for the outbox and inbox tables. Locally it is the
  NATS server the compose file builds from [`compose/nats/Dockerfile`](../compose/nats/Dockerfile),
  on `127.0.0.1:4228`, or the capstone stack's through `MIKOSHI_NATS_URL=nats://127.0.0.1:4226`.
  Mikoshi owns the stream `MIKOSHI`, which it creates or updates at startup.

## Standard and native tiers

Each use of a capability runs at one of two tiers. The standard tier uses the technology's
common standard, ISO/IEC 9075 SQL for the database. The native tier uses the provider's own
features. Everything pgvector adds (the `vector` type, the `hnsw` index method, the
`vector_cosine_ops` operator class, and the distance operators) is native tier.

Only these packages import a provider or use pgvector:

- The composition root, `internal/app`. `infrastructure.go` opens the pool through go-database's
  `postgres` provider, its `sql` node wraps the pool in sqlate's `postgres` dialect, its
  `storage` node wraps go-storage's `s3` client in a `storage.Store`, and its `broker` node is
  go-messaging's `nats` broker. `admin.go` declares pgvector to the migrator by name, from
  sqlate's `postgres/pgvector`, and declares go-messaging's `postgres` migration set beneath
  Mikoshi's own. `reactors.go` builds the messaging runtime over the broker and go-messaging's
  `postgres` statements, and the sweeper's two retentions from that engine.
- The migrations, `data/migrations`, which create the `vector` column and space 1's HNSW index.
- The admin domain, `admin/database`, through go-database's admin service alone: it reports
  pgvector's status in the schema status and imports no provider.

Every other package stays provider-free. The admin service receives the pool as a go-database
`database.DB`. The domains receive the `sql` node's `data.Database`, the dialect-wrapped session
and the pattern catalog their statements compile against. The items domain, the one package that
keeps objects, receives the `storage` node's `storage.Store`, go-storage's standard tier. The
integration harness and suite sit outside the service's process and reach the database and the
gateway directly: the harness makes each test's database through sqlate's `postgres/pgtest` and
reaches a bucket through go-storage's `s3`, and the suite names pgvector from `postgres/pgvector`
and runs pgvector queries to prove the index. The package boundary is a convention held in
review.

sqlint holds each statement file to its tier. `mise run check` runs it under
[`sqlint.toml`](../sqlint.toml), which fails a statement file in `domain/*/statements` or
`data/statements` that is headed `tier: standard` and uses a native form PostgreSQL or pgvector
declares. Three statement files are native tier, each headed `tier: native` with the form it
uses and its ports:

- [`data/statements/lock.sql`](../data/statements/lock.sql), the context tree's advisory lock,
  `pg_advisory_xact_lock` over `hashtext`.
- [`domain/items/statements/claim_stale_blob.sql`](../domain/items/statements/claim_stale_blob.sql)
  and [`claim_orphan_blob.sql`](../domain/items/statements/claim_orphan_blob.sql), the blob
  reclaimers' claims, `SELECT ... FOR UPDATE SKIP LOCKED` ([Items](items.md#reclamation)).

sqlint does not check the migrations' forms, so review holds the migrations list below.

## Messaging

The broker is the owner of the stream `MIKOSHI`: its `Start` creates the stream or updates it to
the `nats` block, capturing the subjects `mikoshi.>` and keeping each event for `max_age`, 168h.
Every event type Mikoshi emits is under the `mikoshi` prefix and is published with its type as
the subject. A domain emits through the runtime's recorder in its command's transaction, so the
event is written to the outbox exactly when the change it reports commits; the relay publishes
each committed outbox row to the broker. The recorder stamps the `layer` extension `mikoshi` on
every event, and each raise sets `correlationid` through the
[`correlation`](../correlation/doc.go) package: the request's id, or a fresh UUIDv7 outside a
request. The relay holds the quiesce gate shared for each row, so it pauses while an admin verb
holds the gate. `/readyz` lists `broker` and `relay`.

The domains use go-messaging's base module alone, through the recorder the composition root
hands them; `correlation` uses go-core's `event` package. Only the composition root imports
the `nats` provider or the `postgres` engine, and the integration harness binds the stream as a
consumer, never its owner, through the `nats` provider.

The sweeper also runs the messaging tables' retentions, each scheduled by its block under
`sweep`:

| Reclaimer | What it removes | Defaults |
|-----------|-----------------|----------|
| `outbox_messages` | Published outbox rows published more than `age` ago | `interval` 5m, `budget` 500, `age` 168h |
| `inbox_messages` | Handled inbox rows handled more than `age` ago | `interval` 5m, `budget` 500, `age` 168h |

Each field has an override, `MIKOSHI_SWEEP_<RECLAIMER>_<FIELD>`, such as
`MIKOSHI_SWEEP_OUTBOX_MESSAGES_AGE`. The `messaging` and `nats` blocks take the
`MIKOSHI_MESSAGING_*` and `MIKOSHI_NATS_*` overrides.

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
- `0008_context`: `jsonb` for a query context's filter, checked with `jsonb_typeof` and
  `jsonb_array_length`, and `timestamptz` with its `now()` default for the creation time.
- `0009_upload`: the `~` `CHECK` on the upload's SHA-256, `bytea` for its hash state, and
  `timestamptz` for its expiry.
- `0010_upload_chunk`: `bytea` for a chunk's content.
- `0011_blob`: the `~` `CHECK` on the blob's SHA-256, and `timestamptz` with its `now()` default
  for the creation and update times.
- `0012_item`: the `~` `CHECK` on the item's SHA-256, and `timestamptz` with its `now()` default
  for the creation time.
- `0013_context_item`: `timestamptz` with its `now()` default for the creation time.

A port also changes the native statement files:

- `data/statements/lock.sql`: the advisory lock. Its header names the replacements:
  `sp_getapplock` (SQL Server), `GET_LOCK` (MySQL), `DBMS_LOCK` (Oracle), or a `FOR UPDATE`
  mutex row.
- `domain/items/statements/claim_stale_blob.sql` and `claim_orphan_blob.sql`: the claim that
  locks a row and skips one another transaction holds. Their headers name the replacements:
  `UPDLOCK` with `READPAST` (SQL Server), or `FOR UPDATE SKIP LOCKED` (MySQL 8).
