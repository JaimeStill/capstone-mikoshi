// The layers' import cases as one file tree in the client's shape: a file per forbidden import and
// form, each with the rules that catch it, and the imports each layer may make in the same layout,
// over stub packages under the tree's node_modules.
// See README.md

import { spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

import type { ESLint } from "eslint";

const root = join(import.meta.dirname, "../..");

const manifest: { dependencies: Record<string, string> } = JSON.parse(
  readFileSync(join(root, "package.json"), "utf8"),
);

/** The layer table's rule. */
export const table = "boundaries/dependencies";
/** The rule against a package the manifest doesn't declare. */
export const extraneous = "import-x/no-extraneous-dependencies";
/** The rules the lint flags the tree with, no-unresolved among them, which no case may draw. */
export const watched = [
  table,
  extraneous,
  "import-x/no-relative-packages",
  "import-x/no-unresolved",
];

function forms(base: string, specifier: string): Record<string, string> {
  return {
    [`${base}.ts`]: `import { x } from "${specifier}";`,
    [`${base}-type.ts`]: `import type { X } from "${specifier}";`,
    [`${base}-export.ts`]: `export * from "${specifier}";`,
    [`${base}-dynamic.ts`]: `await import("${specifier}");`,
  };
}

/** A forbidden import's file: its source and the rules that flag it. */
export interface Case {
  source: string;
  rules: string[];
}

function by(rules: string[], files: Record<string, string>): Record<string, Case> {
  return Object.fromEntries(
    Object.entries(files).map(([path, source]) => [path, { source, rules: [...rules].sort() }]),
  );
}

const sdk = "@standards-lab/web-app-sdk";
const components = "@standards-lab/web-components";
const foundation = "@standards-lab/web-foundation";
const icons = "@standards-lab/web-icons";

/** Each forbidden import's file, named for what it imports, with the rules that flag it. */
export const violations: Record<string, Case> = {
  ...by([table], {
    ...forms("src/components/to-sdk", "../sdk/stream.ts"),
    ...forms("src/components/to-app", "../config.ts"),
    ...forms("src/components/to-view", "../views/home.ts"),
    ...forms("src/components/nested/to-sdk", "../../sdk/stream.ts"),
    ...forms("src/sdk/to-app", "../config.ts"),
    ...forms("src/sdk/to-components", "../components/badge.ts"),
    ...forms("src/sdk/nested/to-components", "../../components/badge.ts"),
    "src/components/stack-sdk.ts": `import { x } from "${sdk}/router.js";`,
    "src/components/stack-components.ts": `import "${components}/settings.js";`,
    "src/sdk/stack-components.ts": `import "${components}/settings.js";`,
    "src/sdk/foundation.ts": `import { x } from "${foundation}/dom.js";`,
    "src/views/item-value.ts": `import { x } from "./item.js";`,
  }),
  ...by([extraneous], {
    "src/components/lit.ts": `import { x } from "lit";`,
    "src/components/marked.ts": `import { x } from "marked";`,
  }),
  ...by([table, extraneous], {
    "src/sdk/lit.ts": `import { x } from "lit";`,
    "src/typescript.ts": `import { x } from "typescript";`,
  }),
};

/** `cases`, each path to its source alone. */
export const sources = (cases: Record<string, Case>): Record<string, string> =>
  Object.fromEntries(Object.entries(cases).map(([path, { source }]) => [path, source]));

/** Every import each layer may make, in each form, each file named for what it imports. */
export const allowed: Record<string, string> = {
  ...forms("src/components/own", "./badge.ts"),
  ...forms("src/components/nested/own", "../badge.ts"),
  "src/components/packages.ts": [
    `import { x } from "${foundation}/dom.js";`,
    `import type { X } from "${icons}/octicon/x-16";`,
  ].join("\n"),
  ...forms("src/sdk/own", "./stream.ts"),
  ...forms("src/sdk/nested/own", "../stream.ts"),
  ...forms("src/to-components", "./components/badge.ts"),
  ...forms("src/to-sdk", "./sdk/stream.ts"),
  ...forms("src/views/to-app", "../config.ts"),
  ...forms("src/views/to-components", "../components/nested/badge.ts"),
  ...forms("src/views/to-sdk", "../sdk/nested/stream.ts"),
  "src/packages.ts": [
    `import { x } from "${sdk}/router.js";`,
    `import "${components}/settings.js";`,
    `import { x as y } from "${foundation}/dom.js";`,
    `import type { X } from "${icons}/octicon/x-16";`,
  ].join("\n"),
  "src/views/item-type.ts": `import type { X } from "./item.js";`,
};

const target = "export const x = 1;\nexport interface X {}";

const targets = {
  "src/config.ts": target,
  "src/views/home.ts": target,
  "src/views/item.d.ts": "export declare const x: number;\nexport interface X {}",
  "src/components/badge.ts": target,
  "src/components/nested/badge.ts": target,
  "src/sdk/stream.ts": target,
  "src/sdk/nested/stream.ts": target,
};

const stubs = [...Object.keys(manifest.dependencies), "lit", "marked", "typescript"];
const stub = (name: string): Record<string, string> => ({
  "package.json": JSON.stringify({
    name,
    type: "module",
    exports: {
      ".": { types: "./index.d.ts", default: "./index.js" },
      "./*": { types: "./index.d.ts", default: "./index.js" },
    },
  }),
  "index.js": "export const x = 1;",
  "index.d.ts": "export declare const x: number;\nexport interface X {}",
});

/** Writes `files` and everything they import under `dir`, returning the code files under src/. */
export function writeTree(dir: string, files: Record<string, string>): string[] {
  const written: Record<string, string> = {
    ...files,
    ...targets,
    "package.json": JSON.stringify(manifest),
  };
  for (const name of stubs) {
    for (const [path, source] of Object.entries(stub(name))) {
      written[join("node_modules", name, path)] = source;
    }
  }
  for (const [path, source] of Object.entries(written)) {
    mkdirSync(dirname(join(dir, path)), { recursive: true });
    writeFileSync(join(dir, path), `${source}\n`);
  }
  return Object.keys(written).filter((path) => path.startsWith("src/") && /\.ts$/.test(path));
}

/** ESLint's JSON report on `dir`'s src/ under the client's config; throws when ESLint fails. */
export function lint(dir: string): ESLint.LintResult[] {
  const eslint = join(root, "node_modules", ".bin", "eslint");
  const run = spawnSync(eslint, ["-c", join(root, "eslint.config.js"), "-f", "json", "src"], {
    cwd: dir,
    encoding: "utf8",
  });
  if (run.error) throw new Error(`eslint didn't run: ${run.error}`);
  if (run.status !== 0 && run.status !== 1) {
    throw new Error(`eslint exited ${run.status}: ${run.stderr}`);
  }
  return JSON.parse(run.stdout);
}
