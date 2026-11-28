# Admin

The database admin service is mounted under `/admin/database`. Each endpoint calls go-database's
admin service, the one startup runs its verify and up through, and the schema verbs answer with
the resulting schema status.

Once the schema is current, startup also prepares every registered statement against it, and a
statement that does not fit fails startup with an error naming it. The `verify` endpoint does not
check statements: go-database's admin service checks them only through a seeder, and Mikoshi's
seeder checks none.

| Method | Path | What it does |
|--------|------|--------------|
| `GET` | `/admin/database/diagnostics` | Ping latency, server version, pool counters |
| `GET` | `/admin/database/schema` | Each migration set's history against its embedded migrations, and pgvector's status |
| `GET` | `/admin/database/patterns` | The pattern catalog |
| `GET` | `/admin/database/statements` | The compiled statements, by registered domain |
| `GET` | `/admin/database/states` | The named states, `["empty"]` |
| `POST` | `/admin/database/schema/verify` | Verify the sets' histories and pgvector's declaration |
| `POST` | `/admin/database/schema/up` | Apply the pending migrations |
| `POST` | `/admin/database/schema/down` | Revert `{"set": "app", "steps": 1}`, one step by default |
| `POST` | `/admin/database/schema/steps` | Apply or revert `{"set": "app", "steps": n}`, negative to revert |
| `POST` | `/admin/database/schema/force` | Set the history to `{"set": "app", "version": v}` without running a file |
| `POST` | `/admin/database/seed` | Apply `{"state": "empty"}`, which writes nothing; 403 with no state named |
| `POST` | `/admin/database/state` | Reset to `{"state": "empty", "confirm": true}`; 400 without the confirmation |

The schema is two migration sets: `app`, Mikoshi's [`data/migrations`](../data/migrations), and
beneath it `messaging`, go-messaging's outbox and inbox tables ([Stack](stack.md#messaging)). The
schema status lists pgvector beside them: the version installed and the version the image offers.
`down`, `steps`, and `force` name the set they act on and are refused with 400 when it is missing
or undeclared.

Mikoshi declares one seed state, `empty`, which writes nothing. Startup seeds no state, so
`seed` and `state` with no state named answer 403 with the disabled seed as the detail, and a
state Mikoshi does not declare is refused with 400.

A confirmed reset to `empty` emits one event, `mikoshi.database.reset`, and no other. The reset
reverts every set and applies every set again, which drops every row, the outbox's unpublished
events among them. It then commits the event into the recreated outbox, before the seed, which
writes nothing. The event's subject is the state, `empty`, its data the state's name as a JSON
string, `"empty"`, and its `correlationid` the reset request's `X-Request-Id`. The reset keeps the
`MIKOSHI` stream, so the event reaches every subscriber; it emits none for the entities the revert
drops, so a consumer holding a copy of Mikoshi's entities discards it on
`mikoshi.database.reset`. The reset holds the gate, so the relay pauses and publishes the event
once the reset releases it. The schema verbs, `verify`, `up`, `down`, `steps`, and `force`, emit
nothing.

`up`, `down`, `steps`, and `state` change the schema, so each holds the process's quiesce gate
exclusively. They exclude one another and the sweeper, which holds the gate shared for each
of its passes ([Items](items.md#reclamation)). The gate is per process.

The admin mount serves on the API listener, unauthenticated. It is not for a public deployment
until a management listener gives it a listener of its own.
