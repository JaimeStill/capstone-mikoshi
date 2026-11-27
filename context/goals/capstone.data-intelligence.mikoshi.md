# goal · capstone.data-intelligence.mikoshi

- **State:** building
- **Task:** contexts
- **Branch:** contexts

## Tasks

1. [x] web-shell
2. [x] store
3. [ ] contexts
4. [ ] browser-upload
5. [ ] items
6. [ ] ingest
7. [ ] hybrid-search
8. [ ] context-tools
9. [ ] standing-analyses
10. [ ] lineage
11. [ ] signal-flow

## Task brief · capstone.data-intelligence.mikoshi · contexts

**Problem**

Mikoshi has chunk records but nothing to group them into contexts. Every later task (`items`,
`hybrid-search`, `context-tools`, `standing-analyses`) needs the context tree. That means explicit
and query nodes, coverage of descendants, moves, and a defined path from a context to the chunks of
its items. The tree is built in Mikoshi in a shape that can later become a library of its own
(`context-tree-graduation`), the way blobfs holds a file tree. Items don't exist yet, so this task
builds the tree and designs the parts that need items. `items` builds those parts.

**Behaviors**

1. **Storage.** The tree is an adjacency list: each context names its parent. The descendant and
   ancestor walks are standard-tier `WITH RECURSIVE` statements, and they terminate even if a cycle
   exists. Context ids are version-7 UUIDs. A migration seeds one root at the nil UUID. The root
   can't be renamed, moved or deleted.
2. **Kinds.** A context is explicit or query, set when it is created and never changed. A query
   context carries a filter; an explicit context carries none. A query context is a leaf: creating
   or moving a context under one is refused.
3. **Filter.** A filter is a non-empty JSON list of `{field, op, value}` entries, ANDed, in the
   shape of sqlate's `query.Filter`.
   - The operators are eq, ne, gt, ge, lt, le, like, in, null and notnull.
   - `in` takes a list; `null` and `notnull` take no value; every other operator takes one value.
   - The field must be non-empty. Its name isn't checked here.
   - A malformed filter is refused as invalid input, and the refusal names the failing entry.
4. **Names.** A name is non-empty text and unique among its siblings. A create, rename or move onto
   a name a sibling already holds is refused as a conflict.
5. **Create** takes a parent id, a name, a kind, and a filter for a query context. It returns the
   new context: id, parent id, name, kind, filter, and creation time. A missing parent is not
   found.
6. **Read** returns one context by id. An unknown id is not found; a malformed id is invalid input.
7. **List children** returns one page of a context's direct children, ordered by name, within the
   configured read limits.
8. **Rename** changes a context's name and leaves its parent unchanged.
9. **Move** sets a context's parent, and optionally its name, and carries the whole subtree with
   it.
   - It is refused when the new parent is the context itself or one of its descendants, when the
     new parent is a query context or missing, and for the root.
   - Every change to the tree's shape (create, move, delete) first takes a transaction-scoped
     PostgreSQL advisory lock, the tree lock. So two opposing moves (A under B, B under A) can't
     both commit.
10. **Subtree resolution.** The domain service resolves a context to the ids of its subtree, the
    context included, with each one's kind. `items` builds membership resolution on this.
11. **Delete** removes a context and its whole subtree.
    - A context with no descendants is deleted on the first request.
    - A context with descendants first gets a conflict problem carrying `descendants`, the number
      of descendant contexts.
    - Repeating the request with `confirm=<count>` deletes the subtree if the count still matches.
      If it no longer matches, the conflict comes back with the current count.
    - The delete runs as one statement under the tree lock.
12. **Errors.** Every refusal is a problem response: invalid input, not found, or conflict.
13. **sqlint** runs in the check over every statement file, with pgvector declared under its
    `extensions`. The tree lock is Mikoshi's one native-tier statement and is declared native.
14. **Design note.** A note in Mikoshi's docs states:
    - the model: explicit and query nodes, membership by reference, coverage of descendants;
    - the filter format, and that `items` checks the fields against its item projection and
      evaluates the filters;
    - that `items` owns membership, and a context's memberships are deleted with it;
    - how retrieval narrows chunks: a context resolves to an item-id set, made of the explicit
      members of every explicit context in the subtree plus each query context's matches over all
      items. The chunk statements then join item → input SHA-256 → source occurrence → source →
      chunk;
    - move and delete semantics, the tree lock, and, for graduation, blobfs's standard-tier
      fallback (serializable isolation with retry);
    - the rejected alternatives: `ltree` (a second native extension, and a move rewrites every
      descendant path), a closure table (a move rewrites subtree × depth rows), and blobfs as a
      dependency (a file sits in one directory, with no references or saved filters).

**Test seams**

- The HTTP API at `/api/contexts` on the composed service, in the integration tier against
  PostgreSQL.
- The contexts domain service, for subtree resolution and the concurrency tests: opposing
  concurrent moves, and a move racing a delete.

**Slices**

1. **Web pins current.** The web client's currency (`mise -C web run currency`) exits 0 and the
   check passes. Covers chrome-headless-shell, @types/node, eslint-plugin-boundaries,
   typescript-eslint, and vite.
2. **Create and read.** The context table and its seeded root; the contexts domain package, wired
   through the composition root; create and read for both kinds; filter validation; sibling-name
   conflicts; sqlint in the check.
3. **List children and rename.** Paged children, and rename with its conflicts. Renaming the root
   is refused.
4. **Subtree and move.** Subtree resolution, the tree lock, and move with its refusals: a cycle,
   under a query context, a missing parent, the root, and a name already taken. Includes the
   concurrent opposing-moves test.
5. **Delete.** Subtree delete with the confirmation count. Covers deleting a leaf on the first
   request, the refusal with its count, confirm, a stale count, the root, and a move racing a
   delete.
6. **Design note.** Membership, filter evaluation, the retrieval join, move and lock semantics,
   and the rejected alternatives, matching what was built.

**Out of scope**

- Items, the membership table, evaluating query filters, and the item → chunk join (`items`).
- Retrieval statements and facets (`hybrid-search`).
- Editing a query context's filter after it is created.
- OR or nested filters.
- Any web UI for contexts.
- Context-activity events.
- The graduation itself.

**Door**

Two-way. Nothing is released, the migration has a down, the API has no consumers yet, and the tree
stays ready to graduate.

## Progress

slices 0/6 committed · standards — · spec — · editor —


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
- store: each embedding space is registered by its own migration, so adding a space means a Mikoshi release. Rejected: runtime registration issuing DDL outside the migration history.
- store: `ingest` registers chunkers at runtime by inserting into the `chunker` table, whose surrogate ID keys each chunk.
- store: the GIN index on the chunk's `simple` tsvector ships with the chunk table; `hybrid-search` adds no lexical index.
- store: ships no statements; `hybrid-search` owns the nearest-neighbour statement. sqlint, with pgvector under `extensions`, arrives with the first statement file.
- store: a database reset emits no event; the messaging task adds the reset event.
- store: currency is sqlate's shared script, verbatim.
- store: below roughly 750–1,000 rows in space 1, the planner reads `pk_embedding` and sorts instead of scanning space 1's HNSW index, even with `enable_seqscan` off; an index proof for `hybrid-search` needs more rows (the store proof uses 2,000).
- store: `ingest` maps docproc's empty parent and where to NULL (the columns refuse `''`), and writes a source's stage name, version, and configuration hash all or none.
- store: the schema check caches its readiness for a few seconds, so `/readyz` trails an out-of-band schema change by that long.
- contexts: the tree is an adjacency list walked by standard-tier `WITH RECURSIVE`. Rejected: `ltree` (a second native extension; a move rewrites descendant paths), a closure table (a move rewrites subtree × depth rows), blobfs as a dependency (a file sits in one directory; no references or saved filters).
- contexts: a query context is a leaf, and its filter matches every item, so `standing-analyses`' "item added" check needs no tree walk.
- contexts: a filter is a jsonb list of sqlate `query.Filter` `{field, op, value}` entries, ANDed, with no OR; `contexts` checks shape and operators, `items` checks fields against its item projection and evaluates filters.
- contexts: retrieval resolves a context to an item-id set (the subtree's explicit members plus each query context's matches); `hybrid-search` and `context-tools` take that set and join item → input SHA-256 → source occurrence → source → chunk.
- contexts: `items` owns membership, whose rows are deleted with their context; a subtree delete is one statement under the tree lock.
- contexts: every change to the tree's shape takes the tree lock, `pg_advisory_xact_lock`; graduation keeps blobfs's standard-tier fallback (serializable isolation with retry).
- contexts: deleting a context with descendants needs a matching `confirm=<count>` after a 409 conflict carrying `descendants`.
- contexts: context ids are UUIDv7; the root is the nil UUID and can't be renamed, moved, or deleted.

## Pending edits

- coordinator · context/web-client.md (the backlog entry's summary is at its budget): add webappgen findings from web-shell's inject: when the base is `/`, `mountClient` emits both the `/` not-found branch and the `Mount` branch and a dead `strings` import; inject doesn't add `client.go` to the composition root's `doc.go` layer list; the `# Written by webappgen` header its integration test requires conflicts with comments.md; the browser tier's `ENTRY` and `RENDER_BLOCKING` constants aren't tied to the Go package's `Entry` and `RenderBlocking`; the service's check builds the client twice, and ci.yml's check job builds it again; the root `currency` doesn't run the client's (`mise -C web run currency`), so the client's pins fall behind unreported.
- coordinator · context/web-client.md: add a go-web-sdk-template finding: the template ships the older 84-line `scripts/currency.sh`, without the Dockerfile scans or HOLDS; sqlate's and go-database's shared script is current.
- coordinator · retro topic: settle how a document references another repository's document. lint-references can't resolve `context/chunks.md` in capstone-data-intelligence, so Mikoshi's migration headers link it by GitHub URL.
