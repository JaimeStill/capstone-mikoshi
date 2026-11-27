# Contexts

A context is a named node in one tree, and the tree is how Mikoshi groups what it holds. The
contexts domain, [`domain/contexts`](../domain/contexts), builds the tree, serves it under
`/api/contexts`, and resolves a context to its subtree for the domains that build on it. The
table is `context`, created by [`0008_context`](../data/migrations/0008_context.up.sql).

## The model

The tree has one root, at the nil UUID (`00000000-0000-0000-0000-000000000000`), which the
migration seeds as an explicit context named `root`. It is the one context without a parent, and
its name and place are fixed: it is never renamed, moved, or deleted. Every other context names
its parent, and its name is unique among its siblings.

A context is explicit or query, fixed at its creation:

- An explicit context's contents are placed in it. It carries no filter, and contexts of either
  kind may sit under it.
- A query context's contents are what its filter selects. It is a leaf: no context is created or
  moved under one.

An item belongs to a context by reference, so one item may belong to many contexts. A context
covers everything beneath it: what its descendants hold, it holds too.

## Filters

A query context's filter is a non-empty JSON list of conditions, every one of which an item must
meet, in the shape of sqlate's `query.Filter`:

```json
[
  {"field": "content_type", "op": "eq", "value": "application/pdf"},
  {"field": "year", "op": "in", "value": [2025, 2026]}
]
```

A condition's `field` is not empty, and its `op` decides its `value`:

| Op | Value |
|----|-------|
| `eq`, `ne`, `gt`, `ge`, `lt`, `le`, `like` | Exactly one string, number, or boolean |
| `in` | A non-empty list of strings, numbers, or booleans |
| `null`, `notnull` | None |

The conditions are ANDed, and there is no OR: to select either of two sets, place two query
contexts under one explicit parent, which covers both. A filter selects from every item, not only
the items its parent covers. A filter is set when its context is created and is never edited; a
move or a rename leaves it as it is. The table stores it as `jsonb`, and its constraints hold it
to a non-empty array on a query context and absent on an explicit one.

The contexts domain checks a filter's shape and operators, then checks it against the item read's
field contract, which the composition root passes it from the items domain: a field other than
`route`, `kind`, or `duration`, a value whose JSON type is not its field's (a string for `route`
and `kind`, a number for `duration`), or `like` on `duration`. It refuses a filter that fails either
check with a 400 that names the failing condition's index. The items domain evaluates the filters.

## Membership and retrieval

Membership belongs to the items domain, which places an item in an explicit context with `PUT
/api/contexts/{id}/items/{item}`, withdraws it with `DELETE` on the same path, and pages the items
a context covers with `GET /api/contexts/{id}/items` ([Items](items.md)). Its table,
`context_item`, cascades from `context`, so a context's memberships are deleted with the context,
by the delete of its subtree.

Retrieval narrows chunks through the tree. A context resolves to an item-id set: the explicit
members of every explicit context in its subtree, plus each query context's matches over all
items. `Service.Subtree` gives the ids, kinds, and filters this starts from, the context included, by a
recursive walk down the parent references. The planned chunk statements of `hybrid-search` and
`context-tools` will take that set and join item → input SHA-256 → source occurrence → source →
chunk. Chunks and embeddings stay keyed by content and carry no context, as the chunks contract
keeps them, so a change to a context never rewrites a chunk or an embedding.

## The HTTP API

The routes mount under `/api`. A context reads as its `id`, `parent_id` (null at the root alone),
`name`, `kind`, `filter` (null on an explicit context), and `created_at`.

| Method | Path | What it does |
|--------|------|--------------|
| `POST` | `/api/contexts` | Create `{"parent_id", "name", "kind", "filter"}`; 201 with the context and its `Location` |
| `GET` | `/api/contexts/{id}` | The context, the root's included |
| `GET` | `/api/contexts/{id}/children` | One page of the context's direct children, ordered by name |
| `PUT` | `/api/contexts/{id}` | Rename to `{"name"}`; the context as renamed |
| `POST` | `/api/contexts/{id}/move` | Move under `{"parent_id"}`, renaming when `"name"` is set; the context as moved |
| `DELETE` | `/api/contexts/{id}?confirm=<count>` | Delete the context and its subtree; 204 |

The children read takes `page` and `size`, or the `cursor` the previous page returned, and
refuses `sort` and `filter`. A query context's children are an empty page.

The items domain mounts its routes under the same prefix: `GET /api/contexts/{id}/items`, and
`PUT` and `DELETE` on `/api/contexts/{id}/items/{item}` ([Items](items.md#the-http-api)).

Every refusal is a problem response:

- 400: a malformed id, name, kind, filter, or `confirm`; a `sort` or `filter` on the children
  read; a cursor the read did not issue.
- 404: an unknown context, or a create or move under a missing parent.
- 409: a name a sibling already holds; a create or move under a query context; a rename, move, or
  delete of the root; a move into the context's own subtree; a delete whose `confirm` is not the
  number of the context's descendants.
- 503: the database is unreachable.

## Move and delete

A move sets one context's parent, and its name when the request sets one. Its subtree goes with
it because each descendant names its own parent: only the moved context's row changes. Before it writes, the move walks the new
parent's ancestors up to the root and refuses the move when the context is among them, so no
move makes a cycle.

A delete removes the context and its whole subtree in one statement, a recursive walk down from
the context. A context with no descendants is deleted on the first request, with `confirm`
absent. A delete of a context with descendants is refused with a 409 whose `descendants` member
counts them, the context itself excluded. Repeating the request with `confirm` set to that count
deletes the subtree if the count still matches; otherwise the 409 comes back with the current
count.

## The tree lock

Create, move, and delete each take the tree lock first, in the transaction that makes the
change, because each reads the tree before it writes: the parent's existence and kind, the
ancestor walk, the descendant count. The lock keeps any concurrent change from landing between
the read and the write, so two opposing moves (A under B, B under A) cannot both commit. The lock is `pg_advisory_xact_lock` over the
name `context.tree`, from the lock registry in [`data/locks.go`](../data/locks.go), released
when the transaction ends. Its statement, [`data/statements/lock.sql`](../data/statements/lock.sql),
is native tier ([Stack](stack.md#standard-and-native-tiers)).

A rename takes no lock. Its one statement changes no context's parent, so it cannot make a cycle
or orphan a subtree, and the sibling-name constraint refuses a duplicate name against every
concurrent change.

The tree is built so it can move into a library of its own (`context-tree-graduation`), as blobfs
holds a file tree. A library cannot rely on a native lock, so the library is planned to use
blobfs's standard-tier fallback instead: serializable isolation with retry, where a conflicting
transaction fails and is run again.

## Rejected alternatives

- `ltree`, a materialized path per context: a second native extension beside pgvector, and a
  move rewrites every descendant's path.
- A closure table, a row per ancestor and descendant pair: a move rewrites roughly subtree × depth
  rows.
- blobfs as a dependency: a file sits in exactly one directory, and blobfs has no references or
  saved filters, so it holds neither membership by reference nor query contexts.
