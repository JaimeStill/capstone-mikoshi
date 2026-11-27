# Admin

The database admin service is mounted under `/admin/database`. Each endpoint calls go-database's
admin service, the one startup runs its verify and up through, and the schema verbs answer with
the resulting schema status.

Once the schema is current, startup also prepares every registered statement against it, and a
statement that does not fit fails startup with an error naming it. The `verify` endpoint does not
check statements: go-database's admin service checks them only through a seeder, and Mikoshi
configures none.

| Method | Path | What it does |
|--------|------|--------------|
| `GET` | `/admin/database/diagnostics` | Ping latency, server version, pool counters |
| `GET` | `/admin/database/schema` | The migration set's history against its embedded migrations, and pgvector's status |
| `GET` | `/admin/database/patterns` | The pattern catalog |
| `GET` | `/admin/database/statements` | The compiled statements, by registered domain |
| `GET` | `/admin/database/states` | The named states, an empty list |
| `POST` | `/admin/database/schema/verify` | Verify the set's history and pgvector's declaration |
| `POST` | `/admin/database/schema/up` | Apply the pending migrations |
| `POST` | `/admin/database/schema/down` | Revert `{"set": "app", "steps": 1}`, one step by default |
| `POST` | `/admin/database/schema/steps` | Apply or revert `{"set": "app", "steps": n}`, negative to revert |
| `POST` | `/admin/database/schema/force` | Set the history to `{"set": "app", "version": v}` without running a file |
| `POST` | `/admin/database/seed` | 403: no seed is configured |
| `POST` | `/admin/database/state` | 403 once confirmed with `{"state": "…", "confirm": true}`; 400 without the confirmation |

The schema is one migration set, `app`, Mikoshi's [`data/migrations`](../data/migrations). The
schema status lists pgvector beside it: the version installed and the version the image offers.
`down`, `steps`, and `force` name the set they act on and are refused with 400 when it is missing
or undeclared.

Mikoshi declares no seed states, so `states` lists none, and `seed` and a confirmed `state`
answer 403 with the disabled seed as the detail. A reset emits no event.

`up`, `down`, `steps`, and `state` change the schema, so each holds the process's quiesce gate
exclusively. They exclude one another and the sweeper, which holds the gate shared for each
of its passes ([Items](items.md#reclamation)). The gate is per process.

The admin mount serves on the API listener, unauthenticated. It is not for a public deployment
until a management listener gives it a listener of its own.
