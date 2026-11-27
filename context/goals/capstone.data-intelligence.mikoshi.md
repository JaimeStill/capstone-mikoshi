# goal · capstone.data-intelligence.mikoshi

- **State:** brief ready
- **Task:** web-shell
- **Branch:** web-shell

## Tasks

1. [ ] web-shell
2. [ ] store
3. [ ] contexts
4. [ ] items
5. [ ] ingest
6. [ ] hybrid-search
7. [ ] context-tools
8. [ ] standing-analyses
9. [ ] lineage
10. [ ] signal-flow

## Task brief · web-shell

**Problem**

Mikoshi has only an API and its probes. Every view the goal plans, including the context
hierarchy, chat, analyses, and the live signal view, needs a browser client. That client has to
ship inside the service binary and keep its internal layering. This task puts the client in
place first, before any domain view depends on it. A dry run of webappgen v0.2.1's
`inject service-app` against this repository applied every edit cleanly, and the service's check
then passed. The client added nothing yet enforces that its source imports in one direction only,
so a wrong-direction import would go unnoticed until views pile up on it.

**Behaviors**

1. The built service serves the client's shell at `/`: an HTML document titled Mikoshi that
   loads the client's entry module and carries the client's config. The config gives the API base
   as `/api/`.
2. A deep link, meaning a path under `/` that is neither a built file nor an API or probe route,
   gets the same shell. A reload or a direct visit therefore reaches the client's router.
3. A built asset that the shell references is served with its content type and with the cache
   headers go-web-sdk's client handler assigns.
4. A request for a missing file in the build's asset directory gets a 404 RFC 9457 problem, not
   the shell.
5. The probes are unchanged: `/healthz` and `/readyz` still answer with their JSON bodies, and
   routes under `/api/` are still served by the API rather than the client.
6. The client project is one boundary. Only the service's composition root imports the client's
   Go package, which embeds the build.
7. The service's check builds the client and runs the client's own check, which covers lint,
   types, unit tests, and browser tests. CI installs and builds the client before its check and
   integration jobs. Neither one passes if the client fails.
8. The client's source layers import in one direction only: the components layer, then the
   app-SDK layer, then the app tiers. This mirrors the layering in web-components and web-app-sdk.
   The client's lint enforces it with eslint-plugin-boundaries, configured the way those two
   repositories configure it. An import in the wrong direction fails the lint, and with it the
   check. A fixture or a rule test that the check runs demonstrates the failure.
9. The repository is current. `mise run currency` reports nothing, or only held items. The
   marathon plugin is at 0.24.1 and jdx/mise-action at v5.3.0.

**Test seams**

- The service's HTTP surface, through the integration harness against the built binary
  (behaviors 1–5).
- The client's own check, which covers its unit and browser tiers and its lint, including the
  layer-rule demonstration (behaviors 7–8).

**Slices**

The template's rule puts upgrade slices first. This brief overrides it on purpose: inject's
anchors match the template's exact pins, so the client is injected before the pins move.

1. **Inject the client.** Inject the client under `web/` with webappgen v0.2.1 and install it.
   Add integration tests for behaviors 1–5 and confirm behavior 6. The check and CI build and test
   the client (behavior 7). Demo: run `serve`, open `/` and a deep link, and see the shell; a
   missing asset returns a 404 problem; `/healthz` still returns JSON.
2. **Currency upgrade.** Move claude-plugins/marathon from 0.22.0 to 0.24.1 and jdx/mise-action
   from v5.2.10 to v5.3.0. The slice is done when `mise run currency` reports nothing, or only
   held items, and the check passes (behavior 9).
3. **Layer rule.** Add eslint-plugin-boundaries to the client's lint, with the
   components → app-SDK → app element types and the direction rule modeled on web-components and
   web-app-sdk. Add the demonstration that an import in the wrong direction fails (behavior 8).
   Demo: the check passes on the starter, and the demonstration shows the rule rejecting a
   cross-layer import.

**Out of scope**

- Model token streams and SSE of any kind: no Go stream writer, no relay, no event route, and no
  heartbeat. The goal-wide signal boundary governs these. A token stream travels as direct SSE
  inside the request that asked for it, only committed outcomes become go-messaging events, and
  live views read events. The Go SSE writer is built in context-tools.
- The browser event transport (evidence 15), which is decided in its own task.
- Any domain view beyond the injected starter.
- Auth.

**Door**

Two-way: there is no release and no migration, and every change can be reverted with a commit.

## Progress

slices 3/3 committed · standards ✓ (3d1c3e4, 76758a3) · spec ✓ (no branch gap; dotted deep links → brief) · editor ✓

## Decisions

- capstone: contracts (`chunks`, `protocol`, `events`) are designed in `.foundation` at capstone-data-intelligence; Bunrei and Mikoshi build against them, and a task that reads one opens its summary with "Waits on".
- capstone: Bunrei's `client` module is released by tag (`client/v0.1.0` at `api`); Mikoshi imports the published version, never a replace directive.
- capstone: only `.bunrei` touches the exclusive group (docproc, docinfer, go-messaging); `.mikoshi` stays out of it, so its SSE relay and context tree live in Mikoshi, shaped to graduate.
- capstone: items join contexts by reference and a context covers its descendants; chunks and embeddings stay keyed by content and carry no context.
- signal boundary: a model's token stream travels as direct SSE inside the one request that asked for it; only committed outcomes become go-messaging events (one per committed turn or analysis, with lineage); live views read events, never streams. Rejected: token deltas on go-messaging (a JetStream ack per publish, an outbox row and transaction, retention, lost cancellation).
- SSE layering: the codec in go-core, the HTTP writer in go-web-sdk, go-ai on go-core's codec, built as `.bunrei` tasks (it holds the exclusive group); context-tools waits on them and is the writer's first consumer. Rejected: a Mikoshi-local relay package, tmaxmax/go-sse, r3labs/sse.
- context-tools: SSE holds under replicas (request-scoped, no sticky sessions); settle ingress buffering and idle timeout, HTTP/2, and the drain policy for in-flight answers there.
- evidence 15 (the browser's event transport, NATS over WebSocket or a relay) is decided in its own task; a relay would be an SSE-backed `EventSequence`, stateless per replica over JetStream.

## Pending edits

- coordinator · lands at web-shell's SHIP as standards-lab PR `plan-sse-foundation`, after Bunrei's `plan-capstone.data-intelligence.bunrei` merges (architect's order: Bunrei's, then this, then `.foundation`'s `upload` edit), approved with this task's brief; once merged, message the Bunrei and the capstone-data-intelligence (`.foundation` protocol) sessions:
  - roadmap/capstone.data-intelligence.bunrei.toml: `repos` adds go-core, go-web-sdk, go-ai; the summary names the SSE codec and writer; `order` adds `sse-codec` (go-core: `text/event-stream` read and written to the WHATWG rules beside `event`, released), `sse-writer` (go-web-sdk: on the codec, `Cache-Control: no-store`, a flush per event, the write deadline reset, ended on the run context so the drain exits 0, released), and `go-ai-sse` (go-ai takes go-core; `format.SSE` reads through the codec in place of `internal/sse`, released) after `messaging-work-queue`, before `state`.
  - roadmap/capstone.data-intelligence.mikoshi.toml: web-shell's summary drops the SSE relay; context-tools' opens "Waits on: `.bunrei` `sse-writer` and `go-ai-sse`" and streams its answer through go-web-sdk's writer, one event per committed turn or analysis.
  - roadmap/mikoshi-web-graduation.toml: "the SSE relay into go-web-sdk" becomes the event relay, if evidence 15 picks it.
  - context/data-intelligence.md: the signal boundary paragraph; go-web-sdk's SSE helper is the chat writer on go-core's codec; "a relayable streaming Generate" reworded; `.bunrei`'s exclusive repos and context-tools' blocking point.
- coordinator · roadmap/web-client-backlog.toml: add webappgen findings from web-shell's inject: when the base is `/`, `mountClient` emits both the `/` not-found branch and the `Mount` branch and a dead `strings` import; inject doesn't add `client.go` to the composition root's `doc.go` layer list; the `# Written by webappgen` header its integration test requires conflicts with comments.md; the browser tier's `ENTRY` and `RENDER_BLOCKING` constants aren't tied to the Go package's `Entry` and `RenderBlocking`; the service's check builds the client twice, and ci.yml's check job builds it again; the root `currency` doesn't run the client's (`mise -C web run currency`), so the client's pins fall behind unreported.
