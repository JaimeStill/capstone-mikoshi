# goal · capstone.data-intelligence.mikoshi

- **State:** building
- **Task:** store
- **Branch:** store

## Tasks

1. [x] web-shell
2. [ ] store
3. [ ] contexts
4. [ ] items
5. [ ] ingest
6. [ ] hybrid-search
7. [ ] context-tools
8. [ ] standing-analyses
9. [ ] lineage
10. [ ] signal-flow

## Task brief · store

**Problem**

Mikoshi has no database. `contexts`, `items`, `ingest`, `hybrid-search`, and `lineage` all read or write the chunks contract's records, and nothing holds them yet. pgvector is the capstone's one native-tier extension. Its declaration, image pin, readiness, and the packages allowed to use it (evidence 10) have to be settled before anything builds on them. The per-space index recipe has been proven only in sqlate's own tests, never against Mikoshi's schema.

**Behaviors**

1. Mikoshi reads a `database` configuration block, with `MIKOSHI_DATABASE_*` overrides, and opens its pool through go-database's PostgreSQL provider. The password comes from the secrets layer or its environment variable, never from a committed configuration file. Startup fails, naming the database node, when the database can't be reached.
2. Mikoshi declares pgvector to its migrator by name only. On an empty database, startup installs the version the image offers, then applies Mikoshi's migration set. Startup fails before any migration when the image doesn't offer pgvector.
3. The migration set creates the contract's records:
   - **Embedding spaces:** a fixed integer ID; a unique identity (served model, settings JSON, dimensions); the metric; and the query prefix.
   - **Chunkers:** a surrogate ID and a unique identity (model, settings JSON). `ingest` registers new chunkers at runtime by insert.
   - **Source occurrences:** keyed by the input's SHA-256 and the part ID. Each holds the part's SHA-256, content type, parent, depth, and where it was found.
   - **Sources:** a child of their occurrence, keyed by occurrence, kind, and text SHA-256. Each also holds its stage identity, and sources are indexed on the text's SHA-256.
   - **Chunks:** keyed by source text SHA-256, chunker, and index. Each holds its text, span, text SHA-256 (indexed), and a stored generated `to_tsvector('simple', text)` with a GIN index.
   - **Embeddings:** keyed by chunk text SHA-256 and space, in one table with an untyped vector. Each row holds exactly one of a vector or an error.
4. Each space is registered by its own migration: its row at a fixed ID plus a partial HNSW index over the cast to its dimensions, restricted to its rows. Set A's space is ID 1:
   - capability `embeddings`, served model `intfloat/multilingual-e5-large`;
   - settings `{"request":"1","input_type":"document","prefix":"passage: "}`;
   - 1,024 dimensions, cosine (`vector_cosine_ops`), query prefix `query: `.
5. `/readyz` lists the schema check. It is ready once every migration and the pgvector declaration match the database, and not ready while either is pending or mismatched.
6. `/admin/database` serves go-database's admin operations on the API listener: diagnostics, schema status (pgvector's status included), patterns, statements, states, verify, up, down, steps, force, seed, and state.
   - Schema-changing verbs (up, down, steps, state) hold the process's quiesce gate exclusively.
   - Mikoshi declares no seed states: states lists none, and seed and state answer 403.
   - Resets emit no event.
   - The documentation states that the mount isn't for public deployment until a management listener exists.
7. On space 1, a cosine nearest-neighbour query that orders by the cast scans space 1's index. Inserting a vector of other dimensions into space 1 is refused.
8. The design documentation names the packages allowed to use pgvector: the composition root, the migrations, and the admin domain through the admin service. It classes SQL as schema-bound and lists the migrations as the port list.
9. The repository runs PostgreSQL 19 with pgvector 0.8.7 built from source:
   - a development stack with persistent data on port 5440;
   - an isolated integration stack with no persistent data on port 5441;
   - both ports moved by `POSTGRES_PORT`.

   The integration task starts its stack, prints the stack's logs on failure, and tears it down. CI's integration job runs that same task.
10. Currency uses sqlate's shared script. It reports the image and pgvector pins alongside the existing scans, and `upgrade` runs its upgrade mode.

**Test seams**

- **The integration tier:** the built service as a subprocess against its own empty throwaway database per test, through `/readyz`, `/healthz`, and `/admin/database`. The test connects to the database directly for fixture rows and runs `EXPLAIN` with sequential scans disabled. The probe, drain, port, and shutdown contract tests move here from the unit tier.
- **The unit tier:** the configuration block's finalization, the graph's shape (the admin service between the pool and the server), startup failing without a database, and the admin domain's handlers over its gate.

**Slices**

1. **Containers and currency:** the compose image, the development and integration stacks and their tasks, the CI job running the integration task, and the shared currency script. The existing suite passes against the started stack (behaviors 9, 10).
2. **The pool:** the database configuration block, the pool node, and a throwaway database per integration process. The six boot-the-service tests move to the integration tier (behavior 1).
3. **Extension, readiness, and spaces:** the pgvector declaration, the admin service with the migration set, the spaces migration, and set A's space with its index. Readiness covers the schema (behaviors 2, 4, 5).
4. **The admin domain:** the `/admin/database` route group over the admin service and the quiesce gate, with no seed states (behavior 6).
5. **The contract's records:** chunkers, occurrences, sources, chunks with the tsvector and GIN index, and embeddings. The proof: on an empty database Mikoshi migrates and reports ready, and a nearest-neighbour query over fixture chunks in space 1 scans space 1's index. A wrong-dimension vector is refused (behaviors 3, 7).
6. **Documentation:** the stack and tier declaration, the admin surface, the capability map, and the README's database and integration sections (behavior 8).

**Out of scope**

- Read and write statements, and the sqlint configuration: the first task with a statement file adds both.
- pgvector-go and decoding vectors (`ingest`).
- Writes, de-duplication, and the outbox (`ingest`).
- The reset event and messaging hook (the messaging task).
- The management listener and admin authentication.
- Seed states, and the demo dataset (`.foundation` `demo-dataset`).
- Spaces beyond set A, and the blue/green switch.
- The capstone's compose stack (`.foundation` `stack`).
- `web/`'s trailing pins.

**Door**

Two-way. Nothing is released. The schema lives only in disposable databases until `ingest` writes real data; a migration added after that point is one-way for any deployed database.

## Progress

slices 6/6 committed · standards — · spec — · editor —

## Decisions

- capstone: contracts (`chunks`, `protocol`, `events`) are designed in `.foundation` at capstone-data-intelligence; Bunrei and Mikoshi build against them, and a task that reads one opens its summary with "Waits on".
- capstone: Bunrei's `client` module is released by tag (`client/v0.1.0` at `api`); Mikoshi imports the published version, never a replace directive.
- capstone: only `.bunrei` touches the exclusive group (docproc, docinfer, go-messaging); `.mikoshi` stays out of it, so its SSE relay and context tree live in Mikoshi, shaped to graduate.
- capstone: items join contexts by reference and a context covers its descendants; chunks and embeddings stay keyed by content and carry no context.
- signal boundary: a model's token stream travels as direct SSE inside the one request that asked for it; only committed outcomes become go-messaging events (one per committed turn or analysis, with lineage); live views read events, never streams. Rejected: token deltas on go-messaging (a JetStream ack per publish, an outbox row and transaction, retention, lost cancellation).
- SSE layering: the codec in go-core, the HTTP writer in go-web-sdk, go-ai on go-core's codec, built as `.bunrei` tasks (it holds the exclusive group); context-tools waits on them and is the writer's first consumer. Rejected: a Mikoshi-local relay package, tmaxmax/go-sse, r3labs/sse.
- context-tools: SSE holds under replicas (request-scoped, no sticky sessions); settle ingress buffering and idle timeout, HTTP/2, and the drain policy for in-flight answers there.
- evidence 15 (the browser's event transport, NATS over WebSocket or a relay) is decided in its own task; a relay would be an SSE-backed `EventSequence`, stateless per replica over JetStream.
- client routes (contexts, items): address by id, or keep the last segment free of dots; go-web-sdk's `NewClient` answers a last segment with an extension as an asset (a 404 problem), never the shell.
- store: each embedding space is registered by its own migration (its row at a fixed ID plus a partial HNSW index over `embedding::vector(n)`); set A's space is ID 1. Adding a space means a Mikoshi release. Rejected: runtime registration issuing DDL outside the migration history.
- store: set A's space uses cosine (`vector_cosine_ops`, `<=>`) with query prefix `query: `.
- store: chunkers live in their own table with a surrogate ID, and a chunk is keyed by source text SHA-256, chunker ID, and index; `ingest` registers chunkers at runtime by insert.
- store: a source occurrence's sources are a child table indexed on the text SHA-256, which `contexts` and `lineage` join through. Rejected: a JSON array column.
- store: the GIN index on the chunk's `simple` tsvector ships with the chunk table; `hybrid-search` adds no lexical index.
- store: ships no statements; `hybrid-search` owns the nearest-neighbour statement. sqlint, with pgvector under `extensions`, arrives with the first statement file.
- store: `/admin/database` is served on the API listener until a management listener lands, and is documented as not for public deployment. The schema verbs hold the process's quiesce gate, there are no seed states, and the reset event is left to the messaging task.
- store: integration tests run against compose PostgreSQL 19 with pgvector 0.8.7 built from source, on 5440 (development) and 5441 (integration), each test on its own `pgtest` database.
- store: currency is sqlate's shared script, verbatim.

## Pending edits

- coordinator · context/web-client.md (the backlog entry's summary is at its budget): add webappgen findings from web-shell's inject: when the base is `/`, `mountClient` emits both the `/` not-found branch and the `Mount` branch and a dead `strings` import; inject doesn't add `client.go` to the composition root's `doc.go` layer list; the `# Written by webappgen` header its integration test requires conflicts with comments.md; the browser tier's `ENTRY` and `RENDER_BLOCKING` constants aren't tied to the Go package's `Entry` and `RenderBlocking`; the service's check builds the client twice, and ci.yml's check job builds it again; the root `currency` doesn't run the client's (`mise -C web run currency`), so the client's pins fall behind unreported.
