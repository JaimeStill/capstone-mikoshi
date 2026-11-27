# capstone-mikoshi context

Mikoshi (神輿, the portable shrine that carries a kami) is capstone.data-intelligence's knowledge
service, the authoritative store people work in: a hierarchy of data contexts over files,
records, and analyses, with hybrid search, a chat whose tools see only the selected context,
standing analyses, lineage, and a live view of every signal crossing the architecture.

It is generated from go-web-sdk-template. Its goal, `capstone.data-intelligence.mikoshi`, lives in the coordinator's
roadmap, and its record is [`goals/capstone.data-intelligence.mikoshi.md`](goals/capstone.data-intelligence.mikoshi.md). The capstone's
architecture and scope are `standards-lab/context/data-intelligence.md`, and its design detail
`standards-lab/docs/data-intelligence.md`.

The spike-html-apps repositories (`~/experiments/spike-html-apps`, `-components`, `-foundation`,
`-noctis`, `-sdk`, `-tooling`) are read-only references for the web shell; the design follows
the capstone's domain, not theirs.

## Capability map

- **Items: files, records, and analyses** (planned): the `capstone.data-intelligence.mikoshi` goal, `standards-lab/roadmap/capstone.data-intelligence.mikoshi.toml`.
- **Batch commit** (planned): `ingest` commits chunks and their embeddings into the records **Database** holds; the `capstone.data-intelligence.mikoshi` goal, `standards-lab/roadmap/capstone.data-intelligence.mikoshi.toml`.
- **Hybrid search** (planned): the `capstone.data-intelligence.mikoshi` goal, `standards-lab/roadmap/capstone.data-intelligence.mikoshi.toml`.
- **Context tools and chat** (planned): the `capstone.data-intelligence.mikoshi` goal, `standards-lab/roadmap/capstone.data-intelligence.mikoshi.toml`.
- **Standing analyses** (planned): the `capstone.data-intelligence.mikoshi` goal, `standards-lab/roadmap/capstone.data-intelligence.mikoshi.toml`.
- **Lineage** (planned): the `capstone.data-intelligence.mikoshi` goal, `standards-lab/roadmap/capstone.data-intelligence.mikoshi.toml`.
- **Signal-flow view** (planned): the `capstone.data-intelligence.mikoshi` goal, `standards-lab/roadmap/capstone.data-intelligence.mikoshi.toml`.
- **The web boundary** (built): the browser client in [`web/`](../web/README.md), which the binary embeds and serves at `/` (`internal/app/client.go`), its `src/` layers held to one import direction by its lint.
- **Database** (built): PostgreSQL 19 with pgvector, which the composition root opens and migrates at startup and then checks every registered statement against (`internal/app/infrastructure.go`, `internal/app/admin.go`); the migrations in [`data/migrations`](../data/migrations) create the chunks contract's records, set A's embedding space, ID 1, and the context table, and [Stack](../docs/stack.md) names the packages that may use pgvector and how sqlint holds each statement file to its tier.
- **Context tree** (built): the `domain/contexts` package serves explicit and query contexts under `/api/contexts` and resolves a context to its subtree ([Contexts](../docs/contexts.md)).
- **Database administration** (built): the `admin/database` package serves go-database's admin operations at `/admin/database` ([Admin](../docs/admin.md)).
