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

## Task brief · sse-codec

**Problem**

go-web-sdk has to write `text/event-stream`, and go-ai reads it through a decoder of its own that
no other module can import. Under the SSE layering Decision, go-core owns the codec: a stream read
and written to the WHATWG HTML standard's rules, with no HTTP or model concerns. That lets
`sse-writer` write through it and `go-ai-sse` read through it, both before `context-tools` needs
the writer.

**Behaviors**

1. A root-level `sse` package uses only the standard library and imports nothing from HTTP or from
   go-core's other packages. Its package doc says why it belongs in go-core: the pattern is
   process-level and universal, since clients read event streams and servers write them, and it
   serves neither alone.
2. An `Event` has a `Type` string, a `Data []byte`, and an `ID` string. Its events are neutral:
   nothing maps them to or from CloudEvents.
3. `NewDecoder(io.Reader)` returns a `Decoder`. Its `Next() (Event, error)` reads until a blank
   line ends a block that holds a data field, then returns that event. At the stream's end it
   returns `io.EOF`, and it returns a read error unchanged. `Next` is the only way to read; there
   is no iterator form.
4. The reader follows WHATWG's line rules:
   - One leading BOM is stripped, and no other.
   - A line ends at CR, LF, or CRLF, including a CRLF split across two reads. A trailing CR is
     remembered, never read past, so an event dispatches as soon as its bytes arrive.
   - A line with no colon is a field name with an empty value.
   - One space after the colon is removed.
   - A line or event that the stream's end cuts off is discarded.
5. The reader follows WHATWG's field rules:
   - The last `event` field names the type, and the type resets after each block. An empty `Type`
     means the stream named none.
   - Data values are joined with LF. A `data` field with an empty value still dispatches.
   - A block with no data dispatches nothing.
   - An `id` holding NUL is ignored. The last id carries to every later event until an empty id
     clears it.
   - Comments and unknown fields are skipped. A comment is never returned.
6. The reader decodes UTF-8 with replacement, one U+FFFD for each maximal subpart, as the Encoding
   Standard does. Every `Type`, `Data`, and `ID` is valid UTF-8. Each event's `Data` is its own
   slice, never shared with a later read.
7. `Decoder.Retry() (time.Duration, bool)` reports the reconnection time the stream's last valid
   `retry` field set, in milliseconds. A `retry` value is valid when it is one or more ASCII digits
   that fit in a `time.Duration`; any other value is ignored. Before a valid `retry` arrives,
   `Retry` reports false. A `retry` field dispatches no event, and an event carries no retry.
8. The reader holds only the line and event being built, with no size limit. Its doc says the
   caller bounds an untrusted stream through its context or transport.
9. `NewEncoder(io.Writer)` returns an `Encoder` that sends each event, comment, or retry to its
   writer in exactly one `Write` call, so an HTTP writer can flush once per event. A write error is
   returned unchanged.
10. `Encoder.Encode(Event)` writes the following, ending each line with LF:
    - an `event` line when `Type` is set, and an `id` line when `ID` is set;
    - one `data` line for each line of `Data`, split at CR, LF, and CRLF, with an empty `Data`
      written as one empty `data` line;
    - a blank line to end the event.

    Decoding `Encode`'s output returns the same event, except that CR and CRLF in `Data` come back
    as LF.
11. `Encode` returns an error and writes nothing when `Type` or `ID` contains CR or LF, or `ID`
    contains NUL.
12. `Encoder.Comment(string)` writes the text as one comment line for each of its lines, split at
    CR, LF, and CRLF. The reader skips all of them, and no event dispatches.
13. `Encoder.Retry(time.Duration)` writes a `retry` line in whole milliseconds, followed by a blank
    line. A decoder reading it reports that duration from `Retry` and dispatches no event. A
    negative duration is an error and writes nothing.
14. The README's package list, the context's capability map, and the changelog's Unreleased section
    describe `sse`. The changelog records the addition as non-breaking.

**Test seams**

- The `sse` package's exported API: `NewDecoder`, `Decoder.Next`, `Decoder.Retry`, `NewEncoder`,
  `Encoder.Encode`, `Encoder.Comment`, and `Encoder.Retry`. The tests are black-box.
- The decoder runs over a whole reader, a reader that returns one byte at a time, a reader that
  splits every CRLF across two reads, a reader that returns bytes as a test releases them, and a
  reader that fails.
- The encoder writes to a writer that records each `Write` call, and to a writer that fails.

**Slices**

1. **The reader.** go-ai's decoder and its table tests are ported to `sse`, with `Data` as
   `[]byte`. The slice adds the `retry` cases, along with the package doc, the admission
   rationale, the README, the capability map, and the Unreleased changelog entry (behaviors 1–8,
   and 14 for the reader). Demo: the table passes over whole, one-byte, and split reads.
2. **The writer.** `Encoder`, with its rejections and its round trip through `Decoder`, plus its
   doc, README, and changelog lines (behaviors 9–13, and 14 for the writer). Demo: an encoded
   sequence of events, comments, and a retry decodes back to the same events and the same
   `Retry`, with one `Write` per call.

Each slice leaves `mise run check` passing. Release dates the Unreleased section as v0.14.0, as
v0.13.0's was dated.

**Out of scope**

- HTTP: the media type, headers, flushing, and deadlines are `sse-writer`'s.
- go-ai moving onto the codec, which is `go-ai-sse`'s.
- Reconnection, and the `Last-Event-ID` header.
- Size limits and Options.
- A CloudEvents mapping, an iterator reader, and returning comments.
- Defaulting an empty type to `"message"`.

**Door**

One-way for the `v0.14.0` tag and the exported `sse` API it publishes. Everything before the tag
is two-way.

**Release**

go-core `v0.14.0`.

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
- sse-codec: `sse-writer` and `go-ai-sse` build on go-core `v0.14.0`'s `sse` package.
  Rejected: an `event/sse` package, and the name `eventstream`.
- sse-codec: `go-ai-sse` maps `sse.Event` (its `Data` a `[]byte`) onto `format.WireEvent`,
  deletes `internal/sse`, and keeps bounding a reply with go-ai's context and idle timeout, since
  the `Decoder` has no size limit or Options. Rejected: an `iter.Seq2` reader, and `iter.Pull2` (a
  coroutine per stream, and a `stop` go-ai's seam has nowhere to call).
- sse-codec: `sse-writer` flushes after each `Encoder` call and writes keepalives with
  `Encoder.Comment`.

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
- coordinator · roadmap/capstone.data-intelligence.mikoshi.toml, `sse-codec`: drop "carries its unreleased main" (v0.13.0 ships it).
- coordinator · roadmap/capstone.data-intelligence.bunrei.toml, `event-stream`: its wait on `event-extensions` holds only if Bunrei raises through an outbox; it appends via `messaging.Journal`, which sets extensions today.
- coordinator · roadmap/capstone.data-intelligence.mikoshi.toml, `context-events`: repos gain `go-messaging`; the summary reads go-messaging v0.6.0 (nats/v0.6.0, postgres/v0.5.0) with `Setup.Recorder` in place of v0.4.0.
- coordinator · same file, `event-relay`: drop "a database reset" from what recreates the stream; a reset arrives as `mikoshi.database.reset` on a kept stream, and only the root's `stack:reset` recreates it.
- coordinator · backlog: a Mikoshi reset leaves the bucket's objects with no blob rows, which the orphan reclaimer never finds; a candidate fix is an `OnReset` step that clears the bucket.
