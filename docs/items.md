# Items

An item is Mikoshi's unit of work that contexts hold: a file from a completed upload, a record
from posted JSON, and later an analysis (`context-tools`). The items domain,
[`domain/items`](../domain/items), creates and reads items, deletes them, places them in
contexts, and serves them under `/api/items` and `/api/contexts/{id}/items`. The uploads in
progress belong to [`domain/uploads`](../domain/uploads).

## The model

- A blob is content-keyed bytes: one row per SHA-256 in the `blob` table, its object at
  `sha256/<hex>` in the bucket. Items with the same content share one blob.
- An item is a named entry pointing at a blob. It keeps its type (`file` or `record`), name,
  media type, the content's SHA-256 and size, docproc's route and kind for the content, and a
  duration that stays null until `ingest` fills it.
- A reference places an item in an explicit context. One item may be referenced in many
  contexts.
- `ingest` (planned) links a blob's hash to its chunks, through the chunks contract's source
  occurrences, keyed by content.

```text
upload session ──completes──▶ blob ◀── item ◀── reference ──▶ context
  (temporary)                 sha256/<hex>
```

The tables are `upload` and `upload_chunk`
([`0009_upload`](../data/migrations/0009_upload.up.sql),
[`0010_upload_chunk`](../data/migrations/0010_upload_chunk.up.sql)), `blob`
([`0011_blob`](../data/migrations/0011_blob.up.sql)), `item`
([`0012_item`](../data/migrations/0012_item.up.sql)), and `context_item`
([`0013_context_item`](../data/migrations/0013_context_item.up.sql)). An item names its blob by
a foreign key that does not cascade, so a blob an item names is never deleted.

## Uploads and files

A file arrives as a resumable upload under `/api/uploads`, served by go-web-sdk's `upload`
handler, which implements draft-ietf-httpbis-resumable-upload-16: a `POST` creates the upload,
carrying the whole file's SHA-256 in a `Repr-Digest`, and `HEAD`, `PATCH`, and `DELETE` on
`/api/uploads/{id}` read its offset, append to it, and cancel it. The `uploads` configuration
block sets an upload's `lifetime` from its creation (24h by default) and the `max_append_size`,
the most content one creation or append carries (4 MiB by default), which the handler announces
and enforces. An upload past its lifetime answers 404.

The upload store keeps each upload in PostgreSQL until it completes: its metadata, offset, and
running hash state in an `upload` row, and its content as `upload_chunk` rows of `bytea`, keyed
by offset, so every replica serving one upload shares it. Two appends at one offset are settled
by a conditional update of the offset: one commits, and the other is answered 409.

When the last append completes the file and its SHA-256 matches the creation's, the handler
hands it to the items domain's completion, which creates the file item and answers 201 with the
item and a `Location` of `/api/items/{id}`; the handler then deletes the upload and its chunks.
The completion routes the content with docproc's `route` package, by its signature, file name,
and media type, and keeps the route and kind on the item. It is idempotent on the upload's id,
which the item keeps under a unique constraint, so a repeated or overlapping completion answers
with the item the first created. A completion that fails keeps the upload, so the client may
repeat the completing request.

## Records

`POST /api/items` creates a record from a JSON body, named by the file name of its
`Content-Disposition`. The body is stored byte for byte as an `application/json` blob, by the
same protocol and at the same key as a file's content, and routed as a file is. The body is
bounded by `uploads.max_append_size`, and its connection deadlines follow the server's
`transfer_rate`. It answers 201 with the item and a `Location` of `/api/items/{id}`. A record is
idempotent on nothing: two posts create two items, over one blob when their content is the
same.

## Reads, references, and covered items

An item reads as its `id`, `type`, `name`, `media_type`, `sha256`, `size`, `route`, `kind` (null
when no signature names one), `duration` (null until `ingest` fills it), and `created_at`. The
paged reads, of every item and of the items a context covers, return items in creation order,
take `page` and `size` or the `cursor` the previous page returned, and refuse `sort` and
`filter`.

`PUT /api/contexts/{id}/items/{item}` places an item in an explicit context, and `DELETE` on the
same path withdraws it. Both answer 204 whether or not the reference was in place before. A
query context's contents are what its filter selects, so a reference to one is refused with a
409. A reference reads its context before it writes and takes no tree lock: a context's kind
never changes, a context deleted between the read and the write fails the reference's foreign
key, and one deleted after takes its references with it by cascade.

`GET /api/contexts/{id}/items` pages the items a context covers: the items referenced in every
explicit context of its subtree, the context included, and the items each query context of the
subtree selects from every item, each item once. A query context's filter is evaluated over the
projection that reads every item, whose field contract is `route` and `kind` (text) and `duration`
(a number). The composition root hands that contract to the contexts domain, which refuses a filter
naming any other field when the context is created ([Contexts](contexts.md#filters)).

## Deleting an item

`DELETE /api/items/{id}` deletes the item and its references in one transaction and answers
204. The transaction first moves its blob's `updated_at` to now, then deletes the
`context_item` rows and the item. The blob and its object stay; the orphaned blobs' reclaimer
removes them once no item has named them for its stale age.

## The blob protocol

A create writes its blob in three steps:

1. In one transaction, it touches the blob row, moving its `updated_at`, or inserts it as
   `pending`. A row already `available` has its object, so the item is created in this
   transaction, which holds the row's lock, and nothing is written to the bucket.
2. Otherwise it writes the object at `sha256/<hex>`.
3. In a second transaction, it marks the row `available` and creates the item.

A create that finds the row pending, its own or one left behind, writes the object again. One
that finds the row gone when it marks it starts over, up to three attempts. So an `available`
row always has its object, and a reclaimer that locks a row to remove its content meets any
create over it: the create either waits on the lock and starts over, or touched the row first,
making it fresh, and the reclaimer passes it by.

## Reclamation

go-database's sweeper, a reactor of the composition root
([`internal/app/reactors.go`](../internal/app/reactors.go)), runs three reclaimers, each owned by
the domain whose data it removes, beside the messaging tables' two retentions
([Stack](stack.md#messaging)). Each is scheduled by its block under `sweep`: the least time
between the starts of two of its passes (`interval`) and the most units one pass removes
(`budget`). The sweeper takes a session advisory lock per reclaimer, `sweep.<name>`, a prefix
[`data/locks.go`](../data/locks.go) reserves, so two replicas never run one reclaimer at once. The
sweeper is a readiness check: `/readyz` lists `sweeper`.

| Reclaimer | Owner | What it removes | Defaults |
|-----------|-------|-----------------|----------|
| `expired_uploads` | `uploads.Service.Reclaimer` | Uploads past their expiry, earliest first, and their chunks by cascade | `interval` 1m, `budget` 100 |
| `pending_blobs` | `items.Service.PendingReclaimer` | Blob rows left `pending` past `stale_age` that no item names, and their objects | `interval` 5m, `budget` 100, `stale_age` 1h |
| `orphan_blobs` | `items.Service.OrphanReclaimer` | `available` blobs no item names whose `updated_at` is past `stale_age`, their objects, and the content's records | `interval` 15m, `budget` 100, `stale_age` 24h |

The blob reclaimers follow the blob protocol. Each unit is its own transaction that claims one
row with `FOR UPDATE SKIP LOCKED`, passing over a row a create holds, deletes the object while it
holds the lock (a missing object is success), then deletes the row. A failed object delete rolls
the unit back and ends the pass.

The orphaned blobs' reclaimer also removes what only that content leads to, between the object
and the row: the source occurrences whose input is the blob's SHA-256, with their sources by
cascade; the chunks no remaining source's text names; and the embeddings, in every space, that no
remaining chunk's text names.

Each field has an override, `MIKOSHI_SWEEP_<RECLAIMER>_<FIELD>`, such as
`MIKOSHI_SWEEP_ORPHAN_BLOBS_STALE_AGE`, and each value must be positive.

## The HTTP API

The routes mount under `/api`. Every refusal is a problem response.

| Method | Path | What it does |
|--------|------|--------------|
| `POST` | `/api/uploads` | Create a resumable upload (draft-16); the completing request answers 201 with the file item |
| `HEAD`, `PATCH`, `DELETE` | `/api/uploads/{id}` | Read an upload's offset, append to it, or cancel it |
| `POST` | `/api/items` | Create a record from a JSON body named by `Content-Disposition`; 201 with the item and its `Location` |
| `GET` | `/api/items` | One page of every item, in creation order |
| `GET` | `/api/items/{id}` | The item |
| `DELETE` | `/api/items/{id}` | Delete the item and its references; 204 |
| `GET` | `/api/contexts/{id}/items` | One page of the items the context covers, in creation order |
| `PUT` | `/api/contexts/{id}/items/{item}` | Place the item in the explicit context; 204 |
| `DELETE` | `/api/contexts/{id}/items/{item}` | Withdraw the item's reference from the explicit context; 204 |

The items and reference routes refuse with:

- 400: a malformed id; a `sort` or `filter` on a read; a cursor the read did not issue; a record
  with no name or a body that is not JSON.
- 404: an unknown item, or a reference's unknown context or item.
- 408: a record body slower than the transfer rate.
- 409: a reference to a query context.
- 413: a record body over `uploads.max_append_size`.
- 503: the database is unreachable.
