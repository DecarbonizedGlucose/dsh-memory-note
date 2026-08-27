// Unit tests for bounded rendering: truncation is UTF-8 safe and respects the
// byte budget; the whole-search cap stops at the budget instead of dropping
// everything.
import assert from "node:assert/strict";
import { test } from "node:test";

import { boundTotal, RENDER_BUDGET, truncateUtf8 } from "../src/render-bounds.js";

test("truncateUtf8 leaves short text unchanged", () => {
  assert.equal(truncateUtf8("hello", 100), "hello");
  assert.equal(truncateUtf8("", 10), "");
});

test("truncateUtf8 cuts long text and appends an ellipsis within the budget", () => {
  const long = "a".repeat(5000);
  const out = truncateUtf8(long, 100);
  assert.ok(out.endsWith("…"));
  assert.ok(Buffer.byteLength(out, "utf8") <= 100);
  assert.ok(out.length < 100);
});

test("truncateUtf8 never splits a multi-byte code point", () => {
  // "你" is 3 UTF-8 bytes; with a 6-byte budget the ellipsis leaves 3 bytes,
  // so the two leading ASCII "ab" fit but a broken half of "你" must not.
  const out = truncateUtf8("ab你好世界", 6);
  assert.equal(out, "ab…");
  assert.ok(!out.includes("\uFFFD"));
});

test("boundTotal keeps lines under the total budget and marks truncation", () => {
  const lines = ["x".repeat(50), "y".repeat(50), "z".repeat(50)];
  const out = boundTotal(lines, 90);
  assert.ok(Buffer.byteLength(out, "utf8") <= 90 + 1);
  assert.ok(out.endsWith("…"));
});

test("boundTotal returns everything when it fits", () => {
  const out = boundTotal(["a", "b", "c"], 1000);
  assert.equal(out, "a\nb\nc");
});

test("the default budget is large enough for a normal search", () => {
  assert.ok(RENDER_BUDGET.maxItems > 0);
  assert.ok(RENDER_BUDGET.maxItemBytes >= RENDER_BUDGET.maxTotalBytes / RENDER_BUDGET.maxItems);
});
