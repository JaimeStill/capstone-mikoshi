# mikoshi

A web client on the standards-lab stack, written by webappgen into a Go web service, which embeds
its build and serves it with go-web-sdk's `NewClient`. It takes each layer by its exact release tag
and pins its toolchain with [mise](https://mise.jdx.dev).

## Tasks

```sh
mise install       # the pinned tools; reads the network
mise run install   # the pins and node_modules, from pnpm-lock.yaml; reads the network
mise run check     # lint, types, the unit tier, the build, its size, and the browser tier
mise run build     # build the app into dist/, which the service embeds
mise run currency  # report every tool, package, and layer pin behind its latest; reads the network
```

Only `install` and `currency` read the network. No other task installs a missing tool; each names
`mise run install` when one is missing. `size` is a report, with no budget. The check needs no Go:
its browser tier serves the build behind the shell `NewClient` renders.

The client has no dev server or preview of its own: the service serves it. In the service's root,
`mise run serve` builds the client, then runs the service, and `mise run dev` builds the client,
then rebuilds it on each change with `vite build --watch` while air rebuilds and restarts the
service.

## Config

The service writes the app's config, `{"apiBase": "<URL>"}`, into the shell as its `#app-config`
element, a `<script type="application/json">`: the API the app calls, a URL or a path resolved
against the page's base. The app reads it once, at start. The service takes it from the
`clientAPIBase` constant in its `internal/app/client.go`, which webappgen wrote from `--api-base`
as `/api/`.

## Deploy

Build the client, then the service: `mise run build` writes the app to `dist/`, and the service's
build embeds it through the Go package beside this README, which exports `Dist`, `Entry`, and
`RenderBlocking` for `NewClient`. The binary carries the client, so a deployment ships it alone.
The Go package compiles only once `dist/` is built; `dist/` is never committed.

## How it's built

- `NewClient` renders the shell from the build's manifest, `dist/.vite/manifest.json`, with an
  empty `<body>`. `src/page.ts` draws the foundation's application page into it before the browser
  first paints: its header's buttons open the Help and Settings dialogs through invoker commands
  (`commandfor` and `command`), their `data-keys` bind ? and S through the foundation's `keys.js`,
  and each glyph is the markup of web-icons' module for its icon, as no build fills a page the
  service renders. The router mounts each view in `<main>`.
- `vite.config.ts` builds three inputs, `src/main.ts`, the entry, and `src/theme.ts` and
  `src/page.ts`, which the Go package's `RenderBlocking` names, so the shell loads them, in that
  order, render-blocking, ahead of the entry: the stored theme is on the root and the page drawn
  before first paint. Its base is relative, `./`, so the build resolves against the shell's
  `<base href>` under any mount, and it has no `index.html` or public directory.
  `rolldownOptions.output.strictExecutionOrder` keeps each module's body in source order across
  chunks: without it, the chunk the Settings dialog shares applies the stored theme before the
  theme chunk registers the profiles it names.
- The components adopt their sheets through CSS module scripts,
  `import sheet from "./x.css" with { type: "css" }`, which Vite doesn't support:
  vite-plugin-standard-css-modules rewrites each such import in the layers' modules to a
  constructed `CSSStyleSheet`, and a plain `import "./x.css"` stays CSS Vite bundles. The plugin
  parses with `rolldown/utils`, which it doesn't declare, so `package.json` declares rolldown at
  the version Vite resolves.
- The build targets es2024 for `src/main.ts`'s top-level await, which every browser the router
  needs supports.
- `mise run currency` holds typescript to typescript-eslint's peer range and rolldown to vite's
  dependency range, so the build runs on the rolldown that vite is tested with.
- `pnpm-workspace.yaml`'s `minimumReleaseAge: 0` turns off pnpm's day-long wait on a new release,
  so a frozen install of a lockfile pinning a release under a day old doesn't fail. The layers,
  `github:` dependencies at the top level, need no setting: pnpm guards only a git dependency
  below the top level.
- `tsconfig.json` names the foundation's ambient `*.css` declaration, so the app's stylesheet
  imports resolve, and skips library checks, as the ESLint plugins' declarations name types they
  don't install.
- Prettier formats the code, the JSON, and the CSS alone; its default run would also rewrite the
  Markdown, the YAML, and `pnpm-lock.yaml`.
- The lint requires a description on every ESLint and TypeScript directive.
- `src/` has three layers, which eslint-plugin-boundaries holds to one direction: `src/components/`,
  elements bound for web-components, imports only itself, lit, marked, the foundation, and the
  icons; `src/sdk/`, stateful client services bound for web-app-sdk, imports only itself; and the
  app, the rest of `src/`, imports itself, both layers, and the declared dependencies.
  `eslint.config.js`'s `layers` is the table, and `test/unit/layers.test.ts` lints a tree of
  forbidden and allowed imports to prove it.
- Chrome for Testing publishes no checksums: a chrome-headless-shell bump takes each platform's
  sha256 and size from its download by hand. mise caches downloads by checksum, so a bump that
  keeps the old sha256 installs the old build; the browser tier refuses a binary whose version
  isn't the pin.
