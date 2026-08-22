// Core client behavior against a scripted stand-in binary: argv shape (no
// shell, one JSON argv), the exit-code contract, envelope mapping, timeout,
// missing-binary handling, and HOME export.
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { CoreError, CoreErrorCode, runCore } from "../src/core.js";

const here = path.dirname(fileURLToPath(import.meta.url));
const fakeScript = path.join(here, "fixtures", "fake-binary.mjs");

test("success resolves data and passes the JSON as a single argv", async () => {
  const echoFile = path.join(mkdtempSync(path.join(os.tmpdir(), "dsh-ts-")), "argv.json");
  const data = await runCore(
    { binaryPath: fakeScript, timeoutMs: 2000, env: { FAKE_MODE: "ok", FAKE_ECHO_FILE: echoFile } },
    "memory-get",
    { workspace_id: 1, memory_id: "mem_x" },
  );
  assert.deepEqual(data, { argv: ["memory-get", '{"workspace_id":1,"memory_id":"mem_x"}'] });
  const argv = JSON.parse(readFileSync(echoFile, "utf8")) as unknown[];
  assert.deepEqual(argv, ["memory-get", '{"workspace_id":1,"memory_id":"mem_x"}']);
});

test("exit 1 with an error envelope rejects with a typed CoreError", async () => {
  await assert.rejects(
    runCore({ binaryPath: fakeScript, timeoutMs: 2000, env: { FAKE_MODE: "error" } }, "memory-get", {}),
    (err: unknown) => err instanceof CoreError && err.code === "version_conflict" && err.message === "nope",
  );
});

test("exit 2, non-JSON stdout, and timeouts reject as unknown results", async () => {
  const cases: Array<{ mode: string; timeoutMs?: number }> = [
    { mode: "usage" },
    { mode: "garbage" },
    { mode: "hang", timeoutMs: 150 },
  ];
  for (const entry of cases) {
    await assert.rejects(
      runCore(
        { binaryPath: fakeScript, timeoutMs: entry.timeoutMs ?? 2000, env: { FAKE_MODE: entry.mode } },
        "memory-get",
        {},
      ),
      (err: unknown) => err instanceof CoreError && err.code === CoreErrorCode.UnknownResult,
      entry.mode,
    );
  }
});

test("a missing binary rejects as binary_not_found", async () => {
  await assert.rejects(
    runCore({ binaryPath: "/definitely/missing/binary", timeoutMs: 2000 }, "memory-get", {}),
    (err: unknown) => err instanceof CoreError && err.code === CoreErrorCode.BinaryNotFound,
  );
});

test("home config is exported to the child as DSH_MEMORY_NOTE_HOME", async () => {
  const data = await runCore(
    { binaryPath: fakeScript, timeoutMs: 2000, home: "/configured/home", env: { FAKE_MODE: "home" } },
    "workspace-resolve",
    {},
  );
  assert.deepEqual(data, { home: "/configured/home" });
});
