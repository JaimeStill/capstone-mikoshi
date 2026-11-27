# goal · capstone.data-intelligence.mikoshi

- **State:** idle
- **Task:** none
- **Branch:** none

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

## Pending edits

- coordinator · lands right after web-shell's SHIP as standards-lab PR `plan-sse-foundation`, first of the three roadmap PRs (then Bunrei's `metadata-text`, then `.foundation`'s `upload`; Bunrei relays to `.foundation`); once merged, message the Bunrei session:
  - roadmap/capstone.data-intelligence.bunrei.toml: `repos` adds go-core, go-web-sdk, go-ai; the summary names the SSE codec and writer; `order` adds `sse-codec` (go-core: `text/event-stream` read and written to the WHATWG rules beside `event`, released), `sse-writer` (go-web-sdk: on the codec, `Cache-Control: no-store`, a flush per event, the write deadline reset, ended on the run context so the drain exits 0, released), and `go-ai-sse` (go-ai takes go-core; `format.SSE` reads through the codec in place of `internal/sse`, released) after `messaging-work-queue`, before `state`.
  - roadmap/capstone.data-intelligence.mikoshi.toml: web-shell's summary drops the SSE relay; context-tools' opens "Waits on: `.bunrei` `sse-writer` and `go-ai-sse`" and streams its answer through go-web-sdk's writer, one event per committed turn or analysis.
  - roadmap/mikoshi-web-graduation.toml: "the SSE relay into go-web-sdk" becomes the event relay, if evidence 15 picks it.
  - context/data-intelligence.md: the signal boundary paragraph; go-web-sdk's SSE helper is the chat writer on go-core's codec; "a relayable streaming Generate" reworded; `.bunrei`'s exclusive repos and context-tools' blocking point.
- coordinator · roadmap/web-client-backlog.toml: add webappgen findings from web-shell's inject: when the base is `/`, `mountClient` emits both the `/` not-found branch and the `Mount` branch and a dead `strings` import; inject doesn't add `client.go` to the composition root's `doc.go` layer list; the `# Written by webappgen` header its integration test requires conflicts with comments.md; the browser tier's `ENTRY` and `RENDER_BLOCKING` constants aren't tied to the Go package's `Entry` and `RenderBlocking`; the service's check builds the client twice, and ci.yml's check job builds it again; the root `currency` doesn't run the client's (`mise -C web run currency`), so the client's pins fall behind unreported.
