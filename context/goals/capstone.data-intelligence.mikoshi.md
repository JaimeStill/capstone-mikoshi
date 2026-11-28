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
8. [x] context-events
9. [x] sse-codec
10. [x] sse-writer
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

## Task brief · sse-writer

**Problem**

`context-tools` streams a model's answer as SSE inside the request that asked for it, and
`event-relay` serves Mikoshi's events to the browser the same way, but go-web-sdk has no writer for
`text/event-stream`. Under the SSE layering Decision, the writer belongs to go-web-sdk and sits on
go-core `v0.14.0`'s `sse` codec. A stream runs longer than the server's `WriteTimeout`. Because
`http.Server.Shutdown` waits for a stream that is still running, the stream can also outlast the
drain's budget. Unless the writer ends the stream on the run context, the drain fails and
`Service.Run` exits 1.

**Behaviors**

1. `Server` keeps the context its `Start` receives, which is the Coordinator's run context. It
   carries that context as a value in every request's context through `http.Server.BaseContext`.
   Request contexts aren't cancelled along with it, so an ordinary request still drains to
   completion. `Start`'s doc says so.
2. `Config.EventStream(EventStreamOptions)` returns an `EventStream`. It is built once at wiring
   and is safe for concurrent use.
   - It panics on an unfinalized `Config` and on a negative `EventStreamOptions.KeepAlive`.
   - A zero `KeepAlive` means 15 seconds.
   - Every method of a zero `EventStream` panics, as `Transfer`'s methods do.
3. `EventStream.Serve(w, r, fn)` sets `Content-Type: text/event-stream` and
   `Cache-Control: no-store`, writes the 200 status, and flushes the headers before it calls `fn`.
   If the writer can't flush, `Serve` returns an error and never calls `fn`.
4. `Serve` calls `fn` with a context and a sender. The context ends when either the request's
   context or the run context from behavior 1 ends. When the request carries no run context, as
   under a plain `httptest` server, the context ends with the request alone.
5. The sender writes through go-core's `sse.Encoder` and flushes after each write.
   - `Send(sse.Event)` writes one event.
   - `Retry(time.Duration)` writes a `retry` field.
   - If the `Encoder` rejects an event or a duration, the sender returns its error, writes
     nothing, and leaves the stream open.
   - A write or flush error is returned.
6. Before each write, the writer sets the connection's write deadline to now plus the `Config`'s
   `WriteTimeout`, through `http.ResponseController`. Each write means an event, a retry, a
   keepalive, or the headers.
   - If the `Config` disables `WriteTimeout` with an explicit zero, the deadline is left alone.
   - `http.ErrNotSupported` is tolerated.
   - A stream can therefore last past the `WriteTimeout`, while a client that stalls on one write
     fails within it.
7. While `fn` runs, the writer writes a keepalive comment through `Encoder.Comment` whenever
   `KeepAlive` passes with nothing written. Each send restarts the interval. A quiet stream gets
   comments at the interval, a busy one gets none, and no event reaches a client because of them.
8. Sends and keepalives never interleave. Each write reaches the client whole, and the race
   detector passes with sends from `fn` running against the ticker.
9. Once the context from behavior 4 ends, or a keepalive write fails, every later `Send` and
   `Retry` returns an error and writes nothing, which ends a producer's loop. The keepalive
   stops before `Serve` returns.
10. When `fn` returns, `Serve` returns its error. The writer writes nothing at the end of a stream:
    no closing event, comment, or retry.
11. When the run context of a `Service` whose handler streams through `Serve` ends, the service
    drains. The stream's context ends, `fn` returns, the server shuts down within
    `ShutdownTimeout`, and `Service.Run` returns `process.ExitOK`.
12. `EventStream`'s doc says a stream route must not sit under `middleware.Timeout`, whose duration
    would end the stream. `Transfer`'s doc gives the same warning for a transfer.
13. The docs describe `EventStream` and the run context in the request. This covers the README,
    `doc.go`'s inventory, the context's capability map, the server and requests-and-responses
    docs pages, and `STANDARDS.md`'s timeouts and lifecycle lines. The changelog's Unreleased
    section adds `EventStream`. It marks the go-core `v0.14.0` requirement as **Breaking**, because
    that version pulls in `v0.13.0`'s `EventKind.Raise` change, which the SDK doesn't call.

**Test seams**

- `Config.EventStream`, `EventStream.Serve`, and the sender, tested black-box. Tests run them over
  an `httptest` server whose client reads the body with go-core's `sse.Decoder`, and over a
  recorder that doesn't support deadlines.
- `Server` started with a context the test cancels, for behaviors 1 and 4.
- `Service.Run` with a streaming handler and a cancelled context, for behavior 11.
- A short `KeepAlive` and a short `WriteTimeout` keep the timing tests fast.

**Slices**

1. **Upgrade.** Both modules require go-core `v0.14.0`, `middleware/rate-limit` requires
   go-web-sdk `v0.19.0`, and each module is tidied. The base changelog marks the go-core bump as
   Breaking, and rate-limit's Unreleased section records its two bumps. The slice is done when
   `mise run currency` exits 0 and `mise run check` passes.
2. **The run context in the request.** `Server` carries its run context into every request's
   context, with its doc and the server docs page (behavior 1). Demo: a request served after the
   test cancels `Start`'s context sees that context ended while its own is still live.
3. **The stream.** `Config.EventStream`, `Serve`, the sender, the per-write deadline, the ending on
   either context, and the drain test (behaviors 2–6 and 9–12), with their doc, README,
   capability map, docs page, and changelog lines. Demo:
   - A streamed sequence decodes back with one flush per event.
   - The stream outlasts a short `WriteTimeout`.
   - A cancelled `Service.Run` exits 0 with a stream open.
4. **Keepalives.** The ticker, its restart on each send, and its serialization with sends
   (behaviors 7 and 8), with their doc and changelog lines. Demo: under the race detector, a quiet
   stream decodes no event while its comments arrive at the interval.

Each slice leaves `mise run check` passing. The release dates the Unreleased section as `v0.20.0`,
as `v0.19.0`'s section was dated.

**Out of scope**

- The `Last-Event-ID` header, which the handler reads itself.
- Ingress buffering headers such as `X-Accel-Buffering`, which `context-tools` settles.
- An end-of-stream event, and the drain policy for answers still in flight. Both belong to
  `context-tools`.
- A `middleware/rate-limit` release. Its changes stay Unreleased.
- HTTP/2 specifics beyond what `http.ResponseController` already covers.
- go-ai's move onto the codec, which belongs to `go-ai-sse`.

**Door**

One-way for the `v0.20.0` tag, which publishes the exported `EventStream`, `EventStreamOptions`,
and sender API, and request contexts that carry the run context. Everything before the tag is
two-way.

**Release**

go-web-sdk `v0.20.0`.

## Decisions

- capstone: contracts (`chunks`, `protocol`, `events`) are designed in `.foundation` at capstone-data-intelligence; Bunrei and Mikoshi build against them, and a task that reads one opens its summary with "Waits on".
- capstone: Bunrei's `client` module is released by tag (`client/v0.1.0` at `api`); Mikoshi imports the published version, never a replace directive.
- capstone: the SSE tasks are Mikoshi's (moved from `.bunrei`, which keeps docproc, docinfer, and go-messaging).
- signal boundary: a model's token stream travels as direct SSE inside the one request that asked for it; only committed outcomes become go-messaging events (one per committed turn or analysis, with lineage); live views read events, never streams. Rejected: token deltas on go-messaging (a JetStream ack per publish, an outbox row and transaction, retention, lost cancellation).
- SSE layering: the codec is go-core's `sse` package, the HTTP writer is go-web-sdk's `EventStream`, and `go-ai-sse` moves go-ai onto the codec before `context-tools`. Rejected: a Mikoshi-local relay package, tmaxmax/go-sse, r3labs/sse.
- context-tools: SSE holds under replicas (request-scoped, no sticky sessions); settle ingress buffering and idle timeout, HTTP/2, any end-of-stream event (go-web-sdk's writer sends none), and the drain policy for in-flight answers there.
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
- client: the web client's feature work holds until the services are mostly complete, so little
  shifts beneath it. `client-design` (after `event-relay`) builds the layout skeleton, with the
  context-tree browser as the side pane whose selection every view reads; `items-client`,
  `chat-client`, `admin-overlay` (an overlay in `sl-settings`' Settings pattern), and
  `signal-flow` follow. New elements live in `web/src/components`, shaped to graduate.
  Rejected: `items-client` right after `items`; admin views as a backlog goal.
- context-tools: each committed turn's event carries the model call's timing (`took_ms`) and
  `usage`, which the signal-flow view's go-ai and Torii rows read.
- event-extensions: go-core names no extension and derives none; `ingest` sets `causationid` from the batch in Mikoshi's own code, as the `correlation` package sets `correlationid`. Rejected: correlation constants and a causing-event helper in go-core.
- event-extensions: go-core's envelope stays standard-library-only. Rejected: CloudEvents sdk-go v2.16.2 (no correlation helper; would replace the envelope every consumer reads).
- context-events: an item's creation is `mikoshi.item.created` whatever its type; `context-tools`' analyses emit it and add no type.
- context-events: `standing-analyses` resolves a deleted item's contexts itself, since `mikoshi.item.deleted` carries only `{"id"}` and the references it withdraws emit none.
- sse-codec: `go-ai-sse` maps `sse.Event` (its `Data` a `[]byte`) onto `format.WireEvent`,
  deletes `internal/sse`, and keeps bounding a reply with go-ai's context and idle timeout, since
  the `Decoder` has no size limit or Options. Rejected: an `iter.Seq2` reader, and `iter.Pull2` (a
  coroutine per stream, and a `stop` go-ai's seam has nowhere to call).
- sse-writer: a handler on `EventStream.Serve` must return once its context ends, or the drain outlasts `ShutdownTimeout` and `Service.Run` exits 1. Rejected: a run context the caller passes to `Serve`.
- sse-writer: `Last-Event-ID` is left for the handler to read. `event-relay` and `sse-event-sequence` settle how the relay's cursor travels.
- sse-writer: `middleware/rate-limit`'s go-core `v0.14.0` and go-web-sdk `v0.19.0` bumps stay Unreleased, and no rate-limit tag ships with `v0.20.0`.

## Pending edits

- coordinator · context/web-client.md (the backlog entry's summary is at its budget): add webappgen findings from web-shell's inject: when the base is `/`, `mountClient` emits both the `/` not-found branch and the `Mount` branch and a dead `strings` import; inject doesn't add `client.go` to the composition root's `doc.go` layer list; the `# Written by webappgen` header its integration test requires conflicts with comments.md; the browser tier's `ENTRY` and `RENDER_BLOCKING` constants aren't tied to the Go package's `Entry` and `RenderBlocking`; the service's check builds the client twice, and ci.yml's check job builds it again; the root `currency` doesn't run the client's (`mise -C web run currency`), so the client's pins fall behind unreported. Add a go-web-sdk-template finding too: the template ships the older 84-line `scripts/currency.sh`, without the Dockerfile scans or HOLDS; sqlate's and go-database's shared script is current; go-web-sdk also runs the older 80-line script.
- coordinator · retro topic: settle how a document references another repository's document. lint-references can't resolve `context/chunks.md` in capstone-data-intelligence, so Mikoshi's migration headers link it by GitHub URL.
- coordinator · retro topic: go-database's admin service checks statements in `Verify` only through a `Seeder`, so `POST /admin/database/schema/verify` checks no statements unless the service's seeder does. Mikoshi's `data.Seeder` checks none; Mikoshi checks its statements at startup instead (`data.Database.Verify`, docs/admin.md).
- architecture · standards/go-elemental/principles/construction-and-execution.md: state the exception for an interface that must stay standard-library-only (go-web-sdk's `upload.Store.Create` takes six positional inputs after ctx, since a struct would make implementers import go-web-sdk).
- architecture · principles/rolling-currency.md: add a specification revision a library implements (pinned in its source, read from its registry such as datatracker) to the versions the principle covers.
- architecture · `principles/rolling-currency.md`: when adding the specification-revision pin
  already pending, name both go-web-sdk's `DraftRevision` and web-app-sdk's `DRAFT_REVISION`.
- coordinator · `context/web-client.md`: record that web-app-sdk now pins Go for its browser tier,
  the stack's first JavaScript library to do so, for a Go peer used only in tests.
- capstone-data-intelligence · context/events.md, on `event-relay`'s branch: "The browser's
  transport" says the relay also serves Bunrei's job subjects for Mikoshi's jobs.
- coordinator · docs/data-intelligence.md, "The presentation": `.showcase` `acceptance` owns the
  scripted recovery; and Bunrei's pending prompt-tuning backlog edit is covered by `.mikoshi`
  `admin-overlay`, so Bunrei's sync drops it.
- coordinator · roadmap/capstone.data-intelligence.mikoshi.toml, `sse-codec`: the summary's last sentence reads "Released as go-core v0.14.0." in place of "which carries its unreleased main" (v0.13.0 shipped that main).
- coordinator · roadmap/capstone.data-intelligence.bunrei.toml, `event-stream`: its wait on `event-extensions` holds only if Bunrei raises through an outbox; it appends via `messaging.Journal`, which sets extensions today.
- coordinator · roadmap/capstone.data-intelligence.mikoshi.toml, `context-events`: repos gain `go-messaging`; the summary reads go-messaging v0.6.0 (nats/v0.6.0, postgres/v0.5.0) with `Setup.Recorder` in place of v0.4.0.
- coordinator · same file, `event-relay`: drop "a database reset" from what recreates the stream; a reset arrives as `mikoshi.database.reset` on a kept stream, and only the root's `stack:reset` recreates it.
- coordinator · backlog: a Mikoshi reset leaves the bucket's objects with no blob rows, which the orphan reclaimer never finds; a candidate fix is an `OnReset` step that clears the bucket.
