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
7. [x] event-extensions
8. [ ] context-events
9. [ ] sse-codec
10. [ ] sse-writer
11. [ ] go-ai-sse
12. [ ] hybrid-search
13. [ ] context-tools
14. [ ] pgvector-types
15. [ ] ingest
16. [ ] space-switch
17. [ ] standing-analyses
18. [ ] lineage
19. [ ] sse-event-sequence
20. [ ] event-relay
21. [ ] client-design
22. [ ] items-client
23. [ ] chat-client
24. [ ] admin-overlay
25. [ ] signal-flow
26. [ ] image
27. [ ] foundation-notes

## Decisions

- capstone: contracts (`chunks`, `protocol`, `events`) are designed in `.foundation` at capstone-data-intelligence; Bunrei and Mikoshi build against them, and a task that reads one opens its summary with "Waits on".
- capstone: Bunrei's `client` module is released by tag (`client/v0.1.0` at `api`); Mikoshi imports the published version, never a replace directive.
- capstone: the SSE tasks are Mikoshi's (moved from `.bunrei`, which keeps docproc, docinfer, and go-messaging).
- signal boundary: a model's token stream travels as direct SSE inside the one request that asked for it; only committed outcomes become go-messaging events (one per committed turn or analysis, with lineage); live views read events, never streams. Rejected: token deltas on go-messaging (a JetStream ack per publish, an outbox row and transaction, retention, lost cancellation).
- SSE layering: the codec in go-core, the HTTP writer in go-web-sdk, go-ai on go-core's codec, built as Mikoshi's `sse-codec`, `sse-writer`, and `go-ai-sse` before `context-tools`, the writer's first consumer. Rejected: a Mikoshi-local relay package, tmaxmax/go-sse, r3labs/sse.
- context-tools: SSE holds under replicas (request-scoped, no sticky sessions); settle ingress buffering and idle timeout, HTTP/2, and the drain policy for in-flight answers there.
- evidence 15: the browser's transport is Mikoshi's SSE relay (`event-relay`), which also serves
  the job subjects of the jobs Mikoshi submitted from Bunrei's job stream, so the live views see
  Bunrei's stages through Mikoshi; `ingest` re-raises none of them. Rejected: re-raising each
  stage event into Mikoshi's stream (an outbox row per `inference`).
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
  shifts beneath it. `client-design` (after `event-relay`) builds the layout skeleton, with the
  context-tree browser as the side pane whose selection every view reads; `items-client`,
  `chat-client`, `admin-overlay` (an overlay in `sl-settings`' Settings pattern), and
  `signal-flow` follow. New elements live in `web/src/components`, shaped to graduate.
  Rejected: `items-client` right after `items`; admin views as a backlog goal.
- event-extensions runs before `context-events`, so Mikoshi's events carry `correlationid`,
  `causationid`, and `layer` from their first release.
- context-events carries the record's capstone-data-intelligence pending edits on its own branch
  (that repository is `.showcase`'s root, which a plan can't edit), except the relay's line.
- context-tools: each committed turn's event carries the model call's timing (`took_ms`) and
  `usage`, which the signal-flow view's go-ai and Torii rows read.
- event-extensions: Mikoshi sets `layer` once at its composition root, in the recorder's `event.Options.Extensions`.
- event-extensions: go-core names no extension and derives none; `context-events` and `ingest` set `correlationid` and `causationid` in Mikoshi's own code. Rejected: correlation constants and a causing-event helper in go-core.
- event-extensions: go-core's envelope stays standard-library-only. Rejected: CloudEvents sdk-go v2.16.2 (no correlation helper; would replace the envelope every consumer reads).
- event-extensions: released as go-core `v0.13.0`, carrying main's unreleased commits; `sse-codec` releases `v0.14.0`.

## Pending edits

- capstone-data-intelligence · context/events.md, "Mikoshi's events": `context-events` names the
  item-creation types in place of `items`; `.mikoshi`'s Ownership gains `context-events`.
- capstone-data-intelligence · context/dataset.md: Assumptions, Mikoshi checks filter field
  names; "What .showcase demo-load adds", records load without their `annotates` link.
- capstone-data-intelligence · context/upload.md, Ownership: Mikoshi's store keeps chunks in
  PostgreSQL.
- coordinator · context/web-client.md (the backlog entry's summary is at its budget): add webappgen findings from web-shell's inject: when the base is `/`, `mountClient` emits both the `/` not-found branch and the `Mount` branch and a dead `strings` import; inject doesn't add `client.go` to the composition root's `doc.go` layer list; the `# Written by webappgen` header its integration test requires conflicts with comments.md; the browser tier's `ENTRY` and `RENDER_BLOCKING` constants aren't tied to the Go package's `Entry` and `RenderBlocking`; the service's check builds the client twice, and ci.yml's check job builds it again; the root `currency` doesn't run the client's (`mise -C web run currency`), so the client's pins fall behind unreported. Add a go-web-sdk-template finding too: the template ships the older 84-line `scripts/currency.sh`, without the Dockerfile scans or HOLDS; sqlate's and go-database's shared script is current; go-web-sdk also runs the older 80-line script.
- coordinator · retro topic: settle how a document references another repository's document. lint-references can't resolve `context/chunks.md` in capstone-data-intelligence, so Mikoshi's migration headers link it by GitHub URL.
- coordinator · retro topic: go-database's admin service checks statements in `Verify` only through a `Seeder`, so a service with statements and no seed states can't have `POST /admin/database/schema/verify` check them. Mikoshi checks its statements at startup instead (`data.Database.Verify`, docs/admin.md).
- capstone-data-intelligence · context/upload.md, context/protocol.md: name `.mikoshi` `upload` where they name `.bunrei` `upload`; the upload releases alone as go-web-sdk v0.19.0, `sse-writer` at a later minor; tusd is no interop peer (it speaks interop versions 3–6 only, draft-16 is 11).
- capstone-data-intelligence · context/upload.md: add one line that appending nothing from an interrupted append departs deliberately from draft §8.2's SHOULD, and that the in-memory store and `uploadtest` are go-web-sdk's while each service's cross-replica store is its own; context/protocol.md: state that Bunrei's chunk store over its blob storage is Bunrei's own, proven by go-web-sdk's `uploadtest`.
- architecture · standards/go-elemental/principles/construction-and-execution.md: state the exception for an interface that must stay standard-library-only (go-web-sdk's `upload.Store.Create` takes six positional inputs after ctx, since a struct would make implementers import go-web-sdk).
- architecture · principles/rolling-currency.md: add a specification revision a library implements (pinned in its source, read from its registry such as datatracker) to the versions the principle covers.
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
- capstone-data-intelligence · context/events.md, on `context-events`' branch: `context-tools`
  names the analysis creation type, not `standing-analyses`; on `event-relay`'s branch, "The
  browser's transport" says the relay also serves Bunrei's job subjects for Mikoshi's jobs.
- coordinator · docs/data-intelligence.md, "The presentation": `.showcase` `acceptance` owns the
  scripted recovery; and Bunrei's pending prompt-tuning backlog edit is covered by `.mikoshi`
  `admin-overlay`, so Bunrei's sync drops it.
- capstone-data-intelligence · context/events.md, "How the extensions are set" and Ownership: `.mikoshi` `event-extensions` (not a `.bunrei` task) adds extensions to `Raise`; carried on `context-events`' branch.
- coordinator · roadmap/capstone.data-intelligence.mikoshi.toml, `sse-codec`: drop "carries its unreleased main" (v0.13.0 ships it).
- coordinator · roadmap/capstone.data-intelligence.bunrei.toml, `event-stream`: its wait on `event-extensions` holds only if Bunrei raises through an outbox; it appends via `messaging.Journal`, which sets extensions today.
