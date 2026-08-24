// Shared-fixture conformance: the valid fixtures must satisfy the tool
// parameter specs, and the invalid fixtures must fail where the TypeScript
// layer owns the rule. Raw-JSON lexical rules (duplicate keys, trailing
// values, control characters, 1.0/1e0 number forms) and format rules are
// enforced by the Go decoder on the raw request text and are documented here
// as Go-only — the plugin does not re-implement them.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { parameterSpecs, type ParamSpec } from "../src/tools.js";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, "..", "..");

interface ValidFixture {
  name: string;
  command: string;
  request: unknown;
}
interface InvalidFixture {
  name: string;
  command: string;
  raw: string;
}

const valid = JSON.parse(
  readFileSync(path.join(root, "protocol", "v2", "fixtures", "valid", "requests.json"), "utf8"),
) as ValidFixture[];
const invalid = JSON.parse(
  readFileSync(path.join(root, "protocol", "v2", "fixtures", "invalid", "requests.json"), "utf8"),
) as InvalidFixture[];

// The implicit parameter root stays open in the Harness tool DSL and format
// rules belong to the Go core, so these fixtures are validated there only.
const goOnly = new Set([
  "duplicate-field",
  "trailing-value",
  "control-character",
  "decimal-integer",
  "exponent-integer",
  "unknown-field",
  "history-unknown-field",
  "old-envelope",
  "old-update-patch",
  "timestamp-no-offset",
  "timestamp-fraction",
]);

function validate(spec: ParamSpec, value: unknown, where: string): string | null {
  if (value === undefined) {
    return spec.required ? `${where} is required` : null;
  }
  switch (spec.type) {
    case "string":
      return typeof value === "string" ? null : `${where} must be a string`;
    case "number":
      return typeof value === "number" && Number.isFinite(value) ? null : `${where} must be a number`;
    case "boolean":
      return typeof value === "boolean" ? null : `${where} must be a boolean`;
    case "array": {
      if (!Array.isArray(value)) return `${where} must be an array`;
      if (spec.items === undefined) return null;
      for (const [index, item] of value.entries()) {
        const error = validate(spec.items, item, `${where}[${index}]`);
        if (error !== null) return error;
      }
      return null;
    }
    case "object": {
      if (typeof value !== "object" || value === null || Array.isArray(value)) return `${where} must be an object`;
      const record = value as Record<string, unknown>;
      if (spec.additionalProperties === false) {
        const allowed = new Set(Object.keys(spec.properties ?? {}));
        for (const key of Object.keys(record)) {
          if (!allowed.has(key)) return `${where} has unknown field ${key}`;
        }
      }
      for (const [key, child] of Object.entries(spec.properties ?? {})) {
        const error = validate(child, record[key], `${where}.${key}`);
        if (error !== null) return error;
      }
      return null;
    }
  }
}

test("shared valid fixtures satisfy the tool parameter specs", () => {
  for (const fixture of valid) {
    const specs = parameterSpecs[fixture.command];
    assert.ok(specs !== undefined, `unknown fixture command ${fixture.command}`);
    const request = fixture.request as Record<string, unknown>;
    for (const [key, spec] of Object.entries(specs)) {
      assert.equal(validate(spec, request[key], `${fixture.name}.${key}`), null);
    }
  }
});

test("shared invalid fixtures fail where the TS layer owns the rule", () => {
  let checked = 0;
  for (const fixture of invalid) {
    if (goOnly.has(fixture.name)) continue;
    const specs = parameterSpecs[fixture.command];
    assert.ok(specs !== undefined, `unknown fixture command ${fixture.command}`);
    const request = JSON.parse(fixture.raw) as Record<string, unknown>;
    let failed = false;
    for (const [key, spec] of Object.entries(specs)) {
      if (validate(spec, request[key], key) !== null) failed = true;
    }
    assert.equal(failed, true, `${fixture.name} unexpectedly passed the parameter specs`);
    checked++;
  }
  assert.ok(checked > 0, "every non-Go-only invalid fixture must be exercised");
});
