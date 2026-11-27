// The layers' import direction, components then SDK then app: ESLint, under the client's config,
// over a scratch tree in TMPDIR (layers-tree.ts) flags each forbidden import and no allowed one.

import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, describe, test } from "node:test";

import { allowed, lint, sources, violations, watched, writeTree } from "./layers-tree.ts";

describe("layer imports", () => {
  const files = { ...sources(violations), ...allowed };
  let dir: string;
  let found: Record<string, string[]>;
  before(() => {
    dir = mkdtempSync(join(tmpdir(), "layers-"));
    const code = writeTree(dir, files);
    const report = lint(dir);
    assert.deepEqual(
      report.map((file) => file.filePath.slice(dir.length + 1)).sort(),
      code.sort(),
      "ESLint lints every code file under src/",
    );
    const fatal = report.filter((file) => file.messages.some((m) => m.fatal));
    assert.deepEqual(
      fatal.map((file) => file.filePath),
      [],
      "every file parses, so each import is judged",
    );
    found = {};
    for (const file of report) {
      const rules = [...new Set(file.messages.map((m) => m.ruleId))].filter(
        (id): id is string => id !== null && watched.includes(id),
      );
      if (rules.length > 0) found[file.filePath.slice(dir.length + 1)] = rules.sort();
    }
  });
  after(() => rmSync(dir, { recursive: true, force: true }));

  test("every forbidden import draws exactly the rules its case names", () => {
    const expected = Object.fromEntries(
      Object.entries(violations).map(([path, { rules }]) => [path, rules]),
    );
    const caught = Object.fromEntries(Object.keys(expected).map((path) => [path, found[path]]));
    assert.deepEqual(caught, expected);
  });

  test("a tree of allowed imports draws none", () => {
    assert.deepEqual(
      Object.fromEntries(
        Object.keys(allowed).flatMap((path) => (path in found ? [[path, found[path]]] : [])),
      ),
      {},
    );
  });

  test("no file outside the cases draws a rule", () => {
    assert.deepEqual(
      Object.keys(found).filter((path) => !(path in files)),
      [],
    );
  });
});
