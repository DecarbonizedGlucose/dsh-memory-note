import assert from "node:assert/strict";
import { test } from "node:test";

import { applicationVersion, protocolMajor, protocolMajorFrom } from "../src/version.js";

test("adapter protocol major comes from its package version", () => {
  // Generic, not pinned to a concrete release: re-derive the expected major
  // from the very string the runtime parsed, so the assertion survives any
  // bump without editing two literals in lockstep.
  assert.equal(typeof applicationVersion, "string");
  assert.match(applicationVersion, /^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/);
  assert.equal(protocolMajor, protocolMajorFrom(applicationVersion));
});

test("protocol major accepts release and prerelease versions", () => {
  assert.equal(protocolMajorFrom("3.4.5"), 3);
  assert.equal(protocolMajorFrom("3.4.5-rc.1"), 3);
  assert.equal(protocolMajorFrom("3.4.5+build.7"), 3);
});

test("protocol major rejects malformed versions", () => {
  for (const version of ["", "2", "2.0", "v2.0.0", "development"]) {
    assert.throws(() => protocolMajorFrom(version), /adapter package version is invalid/);
  }
});
