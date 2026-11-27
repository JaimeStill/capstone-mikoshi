# goal · capstone.data-intelligence.mikoshi

- **State:** idle
- **Task:** none
- **Branch:** none

## Tasks

1. [x] web-shell
2. [x] store
3. [x] contexts
4. [ ] upload
5. [ ] browser-upload
6. [ ] items
7. [ ] sse-codec
8. [ ] event-extensions
9. [ ] sse-writer
10. [ ] go-ai-sse
11. [ ] hybrid-search
12. [ ] context-tools
13. [ ] ingest
14. [ ] standing-analyses
15. [ ] lineage
16. [ ] sse-event-sequence
17. [ ] event-relay
18. [ ] signal-flow
19. [ ] image

## Decisions

- capstone: contracts (`chunks`, `protocol`, `events`) are designed in `.foundation` at capstone-data-intelligence; Bunrei and Mikoshi build against them, and a task that reads one opens its summary with "Waits on".
- capstone: Bunrei's `client` module is released by tag (`client/v0.1.0` at `api`); Mikoshi imports the published version, never a replace directive.
- capstone: go-web-sdk's resumable upload and the SSE tasks are Mikoshi's (moved from `.bunrei`, which keeps docproc, docinfer, and go-messaging); `.bunrei` `api` waits on `upload`'s release.
- signal boundary: a model's token stream travels as direct SSE inside the one request that asked for it; only committed outcomes become go-messaging events (one per committed turn or analysis, with lineage); live views read events, never streams. Rejected: token deltas on go-messaging (a JetStream ack per publish, an outbox row and transaction, retention, lost cancellation).
- SSE layering: the codec in go-core, the HTTP writer in go-web-sdk, go-ai on go-core's codec, built as Mikoshi's `sse-codec`, `sse-writer`, and `go-ai-sse` before `context-tools`, the writer's first consumer. Rejected: a Mikoshi-local relay package, tmaxmax/go-sse, r3labs/sse.
- context-tools: SSE holds under replicas (request-scoped, no sticky sessions); settle ingress buffering and idle timeout, HTTP/2, and the drain policy for in-flight answers there.
- evidence 15 (the browser's event transport, NATS over WebSocket or a relay) is decided in its own task; a relay would be an SSE-backed `EventSequence`, stateless per replica over JetStream.
- client routes (contexts, items): address by id, or keep the last segment free of dots; go-web-sdk's `NewClient` answers a last segment with an extension as an asset (a 404 problem), never the shell.
- store: each embedding space is registered by its own migration, so adding a space means a Mikoshi release. Rejected: runtime registration issuing DDL outside the migration history.
- store: `ingest` registers chunkers at runtime by inserting into the `chunker` table, whose surrogate ID keys each chunk.
- store: the GIN index on the chunk's `simple` tsvector ships with the chunk table; `hybrid-search` adds no lexical index.
- store: `hybrid-search` owns the nearest-neighbour statement.
- store: a database reset emits no event; the messaging task adds the reset event.
- store: currency is sqlate's shared script, verbatim.
- store: below roughly 750–1,000 rows in space 1, the planner reads `pk_embedding` and sorts instead of scanning space 1's HNSW index, even with `enable_seqscan` off; an index proof for `hybrid-search` needs more rows (the store proof uses 2,000).
- store: `ingest` maps docproc's empty parent and where to NULL (the columns refuse `''`), and writes a source's stage name, version, and configuration hash all or none.
- store: the schema check caches its readiness for a few seconds, so `/readyz` trails an out-of-band schema change by that long.
- items: plan blob reclamation, and reclamation of orphaned content-keyed sources (their chunks and embeddings), on go-database's sweeper as go-web-service's `FilesReclaimer` does (agreed at contexts' brief).
- context-tools: consider a context-bound sub-agent for one-off wider analyses: a tool starts a sub-agent bound to a given context (the root included), whose cited answer is recorded with that boundary in its provenance; settle its approval, budgets, and process model there. Input for `spike-go-harness`.

## Pending edits

- coordinator · context/web-client.md (the backlog entry's summary is at its budget): add webappgen findings from web-shell's inject: when the base is `/`, `mountClient` emits both the `/` not-found branch and the `Mount` branch and a dead `strings` import; inject doesn't add `client.go` to the composition root's `doc.go` layer list; the `# Written by webappgen` header its integration test requires conflicts with comments.md; the browser tier's `ENTRY` and `RENDER_BLOCKING` constants aren't tied to the Go package's `Entry` and `RenderBlocking`; the service's check builds the client twice, and ci.yml's check job builds it again; the root `currency` doesn't run the client's (`mise -C web run currency`), so the client's pins fall behind unreported.
- coordinator · context/web-client.md: add a go-web-sdk-template finding: the template ships the older 84-line `scripts/currency.sh`, without the Dockerfile scans or HOLDS; sqlate's and go-database's shared script is current.
- coordinator · retro topic: settle how a document references another repository's document. lint-references can't resolve `context/chunks.md` in capstone-data-intelligence, so Mikoshi's migration headers link it by GitHub URL.
- coordinator · roadmap/context-tree-graduation.toml: add `context = ["capstone-mikoshi/docs/contexts.md"]`, the tree's design, including the native tree lock that the library replaces with blobfs's standard-tier fallback.
- coordinator · retro topic: go-database's admin service checks statements in `Verify` only through a `Seeder`, so a service with statements and no seed states can't have `POST /admin/database/schema/verify` check them. Mikoshi checks its statements at startup instead (`data.Database.Verify`, docs/admin.md).
- capstone-data-intelligence · context/upload.md, context/protocol.md, and the foundation record's protocol Decision: name `.mikoshi` `upload` where they name `.bunrei` `upload`.
