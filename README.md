# Mikoshi

capstone.data-intelligence's knowledge service: a hierarchy of data contexts over files, records,
and analyses, with hybrid search, context-bound AI chat and analyses, and a live view of every
signal crossing the architecture.

Generated from [go-web-sdk-template](https://github.com/standards-lab/go-web-sdk-template). The
capstone's architecture is in the coordinator, `standards-lab/context/data-intelligence.md`; this
repository's working context is [`context/README.md`](context/README.md).

## Getting started

[mise](https://mise.jdx.dev/) provisions the toolchain and runs the tasks:

```sh
mise trust && mise install
mise -C web install && mise -C web run install
mise run serve
```

`serve` builds the web client in [`web/`](web/README.md) first; the service embeds the build and
serves its shell at `/`. The service logs `server ready` on `localhost:8080` (the `local` overlay
binds loopback and runs debug logging). From a second shell:

```sh
curl localhost:8080/healthz   # 200 {"status":"ok"}
curl localhost:8080/readyz    # 200 {"status":"ready","checks":[...]}
```

Ctrl-C drains in-flight requests and exits with `server stopped`.

## Tasks

Each task wraps a plain command, so the repository works without mise:

| Task | Command | What it does |
|------|---------|--------------|
| `mise run check` | `client:build`, `client:check`, then build, vet, `gofmt -l`, `go fix -diff`, `go mod tidy -diff`, `go test -race`, `golangci-lint run`, `lint-context`, `lint-references` | The check, as CI runs it; it writes nothing but the client's build, `web/dist/`, and the lints run at the project root and, when the project is nested in a git repository, at its top level |
| `mise run currency` | `bash scripts/currency.sh` | Report requirements, Go version, tools, and action pins behind their latest; the client's report is `mise -C web run currency` |
| `mise run upgrade` | `go mod edit -go=<current minor> -toolchain=none`, `go get <direct>@latest`, `go mod tidy`, `mise upgrade --bump --local` | Upgrade the go directive, requirements, and tools, rewriting `go.mod` and `mise.toml` |
| `mise run build` | `go build ./...` | Build the module |
| `mise run vet` | `go vet -tags integration ./...` | Compile-check and vet, the integration suite included |
| `mise run serve` | `go run ./cmd/server` | Run the service locally, after `client:build` |
| `mise run test` | `go test -race ./...` | Run the unit tier, after `client:build` |
| `mise run integration` | `go test -race -count=1 -tags integration ./integration/` | Run the integration tier against the built service, after `client:build` |
| `mise run client:build` | `mise -C web run build` | Build the web client into `web/dist/`, which the service embeds |
| `mise run client:check` | `mise -C web run check` | Run the web client's own check: lint, types, unit, and browser tests |
| `mise run dev` | `vite build --watch` in `web/` beside `air` | Rebuild the client and restart the service on each change (`.air.toml`) |
| `mise run fmt` | `gofmt -w .` | Format the source |
| `mise run tidy` | `go mod tidy` | Reconcile module requirements |
| `mise run lint` | `golangci-lint run --build-tags integration ./...` | Lint, the integration suite included |

## Testing

Two tiers. The unit tier is hermetic and runs on every pull request: `go test -race ./...`,
touching no service, network, or disk. The integration tier is the `integration` package: an
untagged harness that builds `cmd/server` once, runs it as a subprocess configured by `MIKOSHI_*`
variables on a port it reserved, and drives it through its HTTP surface, over the toolkit the
SDKs ship for the purpose (go-core's `process/processtest`, go-web-sdk's `webtest`). Under the
`integration` build tag, the suite asserts the service's behavior through its API. It runs on
merge to main and on demand, below the per-PR rate by design; a release is cut only from a main
whose run has passed.

Tests are black-box: golangci-lint's `testpackage` keeps every test in its package's `_test`
package, driving the exported API. `export_test.go` is the one white-box file allowed, and only
to export a clock or probe hook the API cannot reach deterministically.

`internal/app`'s graph tests check the composition itself. Each `Nodes` field names its node
(go-core's `graphtest.CheckNames`). A Build of the root set, `App.Roots` plus the lifecycle
configuration node, builds exactly what `Run` builds; in it the server is alone in the top layer,
a reactor sits below the server by its membership of the reactors group alone, and no order
between nodes is dropped but one: the server's order after the reactors group, while the group
is empty. Each check is shown failing on a fixture that breaks it, with the layer table.

The template's suite asserts the baseline: boot, both probes, and the drain. The client's cases
assert the shell at `/` and at a deep link, a built asset's caching, a missing asset's 404
problem, and that the probes and `/api/` are not answered by the shell. As the build points
fill in, each capability adds its cases beside them, and the first backing service brings its
compose stack: the task then boots that stack as an isolated compose project on its own port,
runs the suite, and tears it down with its volume on exit, so a developer's stack and data are
never touched. The reference service shows the shape.

## Configuration

Configuration layers in a fixed precedence, later sources winning:

1. `config.json` — the base file, every knob at its default.
2. `config.<MIKOSHI_ENV>.json` — the environment overlay; `mise.toml` sets `MIKOSHI_ENV=local`, which
   activates the committed `config.local.json` (loopback host, debug logging).
3. `secrets.json`, `secrets.<MIKOSHI_ENV>.json` — gitignored secret layers.
4. `MIKOSHI_*` environment variables — the final override:
   - `MIKOSHI_LOG_LEVEL`, `MIKOSHI_LOG_FORMAT`
   - `MIKOSHI_SERVER_HOST`, `MIKOSHI_SERVER_PORT`, the four server timeout variables, and
     `MIKOSHI_SERVER_TRANSFER_RATE`
   - `MIKOSHI_READS_DEFAULT_SIZE`, `MIKOSHI_READS_MAX_SIZE`
   - `MIKOSHI_SHUTDOWN_TIMEOUT`

Every file is optional — a deployment can run on the base file and environment variables alone,
or on environment variables only. Each file present decodes strictly: a key the `Config` types do
not declare fails the load.

## Building out the service

The composition root, `internal/app`, is laid out as one file per layer of the architecture,
and those files are the build points:

- `infrastructure.go` for the services the application composes on
- `admin.go` for the administrative services
- `domain.go` for the domain services
- `reactors.go` for the process-lifetime entry points an occurrence drives
- `middleware.go` for the router-level middleware

[Building out the service](docs/building-out.md) covers how the layer files define and mount
their nodes, and walks through adding a domain service, an infrastructure service, an admin
service, a reactor, middleware, and configuration.
