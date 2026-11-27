# goal · capstone.data-intelligence.mikoshi

- **State:** idle
- **Task:** none
- **Branch:** none

## Tasks

1. [x] web-shell
2. [x] store
3. [x] contexts
4. [x] upload
5. [x] browser-upload
6. [x] items
7. [ ] context-events
8. [ ] sse-codec
9. [ ] event-extensions
10. [ ] sse-writer
11. [ ] go-ai-sse
12. [ ] hybrid-search
13. [ ] context-tools
14. [ ] ingest
15. [ ] pgvector-types
16. [ ] space-switch
17. [ ] standing-analyses
18. [ ] lineage
19. [ ] client-design
20. [ ] items-client
21. [ ] sse-event-sequence
22. [ ] event-relay
23. [ ] signal-flow
24. [ ] image
25. [ ] foundation-notes

## Decisions

- capstone: contracts (`chunks`, `protocol`, `events`) are designed in `.foundation` at capstone-data-intelligence; Bunrei and Mikoshi build against them, and a task that reads one opens its summary with "Waits on".
- capstone: Bunrei's `client` module is released by tag (`client/v0.1.0` at `api`); Mikoshi imports the published version, never a replace directive.
- capstone: the SSE tasks are Mikoshi's (moved from `.bunrei`, which keeps docproc, docinfer, and go-messaging).
- signal boundary: a model's token stream travels as direct SSE inside the one request that asked for it; only committed outcomes become go-messaging events (one per committed turn or analysis, with lineage); live views read events, never streams. Rejected: token deltas on go-messaging (a JetStream ack per publish, an outbox row and transaction, retention, lost cancellation).
- SSE layering: the codec in go-core, the HTTP writer in go-web-sdk, go-ai on go-core's codec, built as Mikoshi's `sse-codec`, `sse-writer`, and `go-ai-sse` before `context-tools`, the writer's first consumer. Rejected: a Mikoshi-local relay package, tmaxmax/go-sse, r3labs/sse.
- context-tools: SSE holds under replicas (request-scoped, no sticky sessions); settle ingress buffering and idle timeout, HTTP/2, and the drain policy for in-flight answers there.
- evidence 15 (the browser's event transport, NATS over WebSocket or a relay) is decided in its own task; a relay would be an SSE-backed `EventSequence`, stateless per replica over JetStream.
- client routes (contexts, items): address by id, or keep the last segment free of dots; go-web-sdk's `NewClient` answers a last segment with an extension as an asset (a 404 problem), never the shell.
- store: each embedding space is registered by its own migration, so adding a space means a Mikoshi release. Rejected: runtime registration issuing DDL outside the migration history.
- store: `ingest` registers chunkers at runtime by inserting into the `chunker` table, whose surrogate ID keys each chunk.
- store: the GIN index on the chunk's `simple` tsvector ships with the chunk table; `hybrid-search` adds no lexical index.
- store: `hybrid-search` owns the nearest-neighbour statement.
- store: currency is sqlate's shared script, verbatim.
- store: below roughly 750–1,000 rows in space 1, the planner reads `pk_embedding` and sorts instead of scanning space 1's HNSW index, even with `enable_seqscan` off; an index proof for `hybrid-search` needs more rows (the store proof uses 2,000).
- store: `ingest` maps docproc's empty parent and where to NULL (the columns refuse `''`), and writes a source's stage name, version, and configuration hash all or none.
- store: the schema check caches its readiness for a few seconds, so `/readyz` trails an out-of-band schema change by that long.
- context-tools: consider a context-bound sub-agent for one-off wider analyses: a tool starts a sub-agent bound to a given context (the root included), whose cited answer is recorded with that boundary in its provenance; settle its approval, budgets, and process model there. Input for `spike-go-harness`.
- browser-upload: web-app-sdk's own `./upload.js` is the draft-16 browser client, and
  `Client.upload` stays the one-request upload. Rejected: tus-js-client (it stops at draft-05 and
  sends no digests), and a method on `Client`.
- browser-upload: the app keeps an upload's Location and resumes it after a reload with
  `open(location, file)`; the handler's Repr-Digest check catches a different file.
- browser-upload: web-app-sdk pins Go and a test-only Go module requiring go-web-sdk's upload
  release, whose handler is the client's interop peer. A draft revision moves go-web-sdk's
  `DraftRevision`, web-app-sdk's `DRAFT_REVISION`, and the module's requirement together.
  Rejected: a test server command in go-web-sdk, and tusd as the peer (v2.10.4 speaks interop
  versions 3–6 only).
- browser-upload: `create` also repeats a creation answered 429 or 5xx, as go-web-sdk's `Client.Create` does (no behavior asked it; flagged at the session brief).
- items: `context-tools` adds the `analysis` item type by a migration that widens `item`'s
  `cc_item_type` check.
- items: `ingest` fills an item's `duration` from docproc's media metadata, and ingests a
  record's `application/json` blob as it does a file's; a record keeps no link to the files it
  annotates.
- items: the covered read collects each query context's matches in Go on every request, paging
  the item projection 500 at a time, because sqlate ANDs filters only; its cost grows with the
  matches, and `hybrid-search` or retrieval may revisit it.
- items: a blob reclaimer claims the row `FOR UPDATE SKIP LOCKED` and deletes the object while it
  holds the lock, so no item names a missing object (behavior 14); `ingest`'s writes for a digest
  respect that row's lock and `updated_at` as a create does (docs/items.md, "The blob protocol").
- items: `ingest` writes `source_occurrence` rows keyed by `input_sha256`, which the orphan
  reclaimer deletes by the blob's digest, with their sources and the chunks and embeddings only
  they lead to.
- context-events (after `items`, before `ingest`) adds go-messaging and the events of contexts'
  and items' commands, the database reset event included; `items` emits none.
- client: the web client's feature work holds until the services are mostly complete, so little
  shifts beneath it. `client-design` (after `lineage`) evaluates the client's UI/UX and layout
  holistically and names its gaps; `items-client` follows it with the browser's upload, item
  views, and the context-tree browser. Rejected: `items-client` right after `items`.

## Pending edits

- coordinator · roadmap/capstone.data-intelligence.mikoshi.toml: add `context-events` to `order`
  after `items`, and `client-design` then `items-client` after `lineage`, with their task entries
  (`client-design`: a holistic UI/UX and layout evaluation of the client that names its gaps;
  `items-client`: the browser's upload, item views, and the context-tree browser); `items`' summary drops the
  browser client and analyses and says its events move to `context-events`; `context-tools`'
  summary gains the analysis item type.
- capstone-data-intelligence · context/events.md, "Mikoshi's events": `context-events` names the
  item-creation types in place of `items`; `.mikoshi`'s Ownership gains `context-events`.
- capstone-data-intelligence · context/dataset.md: Assumptions, Mikoshi checks filter field
  names; "What .showcase demo-load adds", records load without their `annotates` link.
- capstone-data-intelligence · context/upload.md, Ownership: Mikoshi's store keeps chunks in
  PostgreSQL (and roadmap/upload-store-graduation.toml notes it once created).
- coordinator · roadmap/capstone.data-intelligence.showcase.toml: `demo-load` also waits on
  `items-client` if the live upload uses the client.
- coordinator · context/web-client.md (the backlog entry's summary is at its budget): add webappgen findings from web-shell's inject: when the base is `/`, `mountClient` emits both the `/` not-found branch and the `Mount` branch and a dead `strings` import; inject doesn't add `client.go` to the composition root's `doc.go` layer list; the `# Written by webappgen` header its integration test requires conflicts with comments.md; the browser tier's `ENTRY` and `RENDER_BLOCKING` constants aren't tied to the Go package's `Entry` and `RenderBlocking`; the service's check builds the client twice, and ci.yml's check job builds it again; the root `currency` doesn't run the client's (`mise -C web run currency`), so the client's pins fall behind unreported. Add a go-web-sdk-template finding too: the template ships the older 84-line `scripts/currency.sh`, without the Dockerfile scans or HOLDS; sqlate's and go-database's shared script is current; go-web-sdk also runs the older 80-line script.
- coordinator · retro topic: settle how a document references another repository's document. lint-references can't resolve `context/chunks.md` in capstone-data-intelligence, so Mikoshi's migration headers link it by GitHub URL.
- coordinator · roadmap/context-tree-graduation.toml: add `context = ["capstone-mikoshi/docs/contexts.md"]`, the tree's design, including the native tree lock that the library replaces with blobfs's standard-tier fallback.
- coordinator · retro topic: go-database's admin service checks statements in `Verify` only through a `Seeder`, so a service with statements and no seed states can't have `POST /admin/database/schema/verify` check them. Mikoshi checks its statements at startup instead (`data.Database.Verify`, docs/admin.md).
- capstone-data-intelligence · context/upload.md, context/protocol.md, and the foundation record's protocol Decision: name `.mikoshi` `upload` where they name `.bunrei` `upload`; the upload releases alone as go-web-sdk v0.19.0, `sse-writer` at a later minor; tusd is no interop peer (it speaks interop versions 3–6 only, draft-16 is 11).
- capstone-data-intelligence · context/upload.md: add one line that appending nothing from an interrupted append departs deliberately from draft §8.2's SHOULD, and that the in-memory store and `uploadtest` are go-web-sdk's while each service's cross-replica store is its own; context/protocol.md: state that Bunrei's chunk store over its blob storage is Bunrei's own, proven by go-web-sdk's `uploadtest`.
- coordinator · roadmap/capstone.data-intelligence.mikoshi.toml, `upload` summary: replace "tusd as an interop peer only" (tusd lacks interop version 11) and the `ReadUpload` line (the handler reads bodies of unknown length itself).
- coordinator · roadmap/upload-store-graduation.toml: create the backlog goal (a shared cross-replica chunk store, an adjacent library over sqlate, once Bunrei's and Mikoshi's stores exist); roadmap/index.toml: add it to `backlog`.
- architecture · standards/go-elemental/principles/construction-and-execution.md: state the exception for an interface that must stay standard-library-only (go-web-sdk's `upload.Store.Create` takes six positional inputs after ctx, since a struct would make implementers import go-web-sdk).
- architecture · principles/rolling-currency.md: add a specification revision a library implements (pinned in its source, read from its registry such as datatracker) to the versions the principle covers.
- docproc · context/bunrei.md: repoint `chunked-upload-graduation` at go-web-sdk's draft-16 handler and `upload-store-graduation`; replace the stale upload commands (OpenUpload, AppendChunk, AbortUpload, GetUpload) with CompleteUpload and the draft-16 handler.
- capstone-data-intelligence · `context/upload.md`, Ownership: replace the `ReadUpload` and
  `Transfer` at v0.18.0 line. The handler reads bodies of unknown length itself and builds on
  v0.19.0's `Transfer`.
- capstone-data-intelligence · `context/upload.md`, Ownership: add that web-app-sdk's client lives
  in `./upload.js` (from v0.2.0), and that its draft pin is `DRAFT_REVISION`, which web-app-sdk's
  currency compares with datatracker's latest revision, as go-web-sdk's does `DraftRevision`.
- architecture · `principles/rolling-currency.md`: when adding the specification-revision pin
  already pending, name both go-web-sdk's `DraftRevision` and web-app-sdk's `DRAFT_REVISION`.
- coordinator · `context/web-client.md`: record that web-app-sdk now pins Go for its browser tier,
  the stack's first JavaScript library to do so, for a Go peer used only in tests.
