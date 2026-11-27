// The lint the check runs: the recommended rules over the code, a description on every ESLint and
// TypeScript directive, the browser's globals in src/, and node's in the config, the scripts, and
// the tests, and the layers' imports in src/. test/unit/layers.test.ts runs it over
// test/unit/layers-tree.ts.
// See README.md

import { readFileSync } from "node:fs";

import comments from "@eslint-community/eslint-plugin-eslint-comments";
import js from "@eslint/js";
import { defineConfig, globalIgnores } from "eslint/config";
import boundaries from "eslint-plugin-boundaries";
import importX from "eslint-plugin-import-x";
import globals from "globals";
import tseslint from "typescript-eslint";

const code = ["**/*.js", "**/*.ts"];

const manifest = JSON.parse(readFileSync(new URL("package.json", import.meta.url), "utf8"));

const foundation = "@standards-lab/web-foundation";
const icons = "@standards-lab/web-icons";
/** The layer table, in import order: the components, elements bound for web-components, which may
 * import themselves and the packages web-components may, lit, marked, the foundation, and the
 * icons; the SDK, stateful client services bound for web-app-sdk, which may import nothing but
 * themselves; and the app, the rest of src/, which may import itself, both layers, and the
 * packages the manifest declares as dependencies. */
export const layers = [
  {
    name: "components",
    path: "src/components",
    may: ["components", "lit", "marked", foundation, icons],
  },
  { name: "sdk", path: "src/sdk", may: ["sdk"] },
  {
    name: "app",
    path: "src",
    may: ["app", "components", "sdk", ...Object.keys(manifest.dependencies)],
  },
];

const names = layers.map(({ name }) => name);

const elements = layers.map(({ name, path }) => ({
  type: name,
  pattern: [path, `${path}/**/.*`],
  partialMatch: false,
}));

const policies = layers.map(({ name, may }) => ({
  from: { element: { type: name } },
  allow: {
    to: may.map((target) =>
      names.includes(target)
        ? { element: { type: target } }
        : { module: { origin: "external", source: target } },
    ),
  },
}));

const declarations = {
  disallow: { to: { internalPath: "**/*.d.ts" }, dependency: { kind: "value" } },
  message:
    "A .d.ts file is a declaration with no runtime: import it with `import type` (or export " +
    "from it with `export type`), never as a value, a side effect, or an import().",
};

export default defineConfig([
  globalIgnores(["dist/"]),
  { files: code, extends: [js.configs.recommended, tseslint.configs.recommended] },
  {
    files: code,
    plugins: { "@eslint-community/eslint-comments": comments },
    rules: {
      "@eslint-community/eslint-comments/require-description": "error",
      "@typescript-eslint/ban-ts-comment": [
        "error",
        {
          "ts-check": false,
          "ts-expect-error": "allow-with-description",
          "ts-ignore": "allow-with-description",
          "ts-nocheck": "allow-with-description",
        },
      ],
    },
  },
  { files: code, languageOptions: { globals: globals.browser } },
  {
    files: ["test/**", "scripts/**", "*.config.js", "*.config.ts"],
    languageOptions: { globals: globals.node },
  },
  {
    files: ["src/**"],
    plugins: { boundaries, "import-x": importX },
    settings: {
      "import/resolver": { typescript: true },
      "import-x/resolver": { typescript: true },
      "boundaries/elements": elements,
    },
    rules: {
      "import-x/no-unresolved": "error",
      "import-x/no-extraneous-dependencies": [
        "error",
        { devDependencies: false, includeInternal: true, includeTypes: true },
      ],
      "import-x/no-relative-packages": "error",
      "boundaries/dependencies": [
        "error",
        {
          default: "disallow",
          checkAllOrigins: true,
          checkUnknownLocals: true,
          checkInternals: true,
          policies: [...policies, declarations],
        },
      ],
    },
  },
]);
