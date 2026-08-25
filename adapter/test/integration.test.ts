// Optional end-to-end test against the real Go core. Run with:
//
//   pnpm test:integration
//
// It builds the Go binary into a temp directory, so `go` must be available
// and its build cache writable (set GOCACHE if the default is read-only).
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import type { Context } from "@deepseek-ai/cordis";
import type { ToolExecution } from "@deepseek-ai/dsh-tools";

import { apply } from "../src/index.js";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..");
const enabled = process.env.RUN_INTEGRATION === "1";

interface RegisteredTool {
  name: string;
  execute(args: Record<string, unknown>, exec: ToolExecution): Promise<Record<string, unknown>>;
}

test(
  "full lifecycle against the Go core",
  { skip: !enabled },
  async () => {
    const binary = path.join(mkdtempSync(path.join(os.tmpdir(), "dsh-core-")), "dsh-memory-note");
    execFileSync("go", ["build", "-o", binary, "./cmd/dsh-memory-note"], {
      cwd: repoRoot,
      env: process.env,
      stdio: "pipe",
    });

    const home = path.join(mkdtempSync(path.join(os.tmpdir(), "dsh-home-")), "home");
    const workspace = mkdtempSync(path.join(os.tmpdir(), "dsh-workspace-"));

    const tools: RegisteredTool[] = [];
    const approvals: string[] = [];
    const ctx = {
      tools: { register: (tool: RegisteredTool) => tools.push(tool) },
      approval: {
        async request(req: { toolName: string }) {
          approvals.push(req.toolName);
          return "allowed-once";
        },
      },
    } as unknown as Context;
    apply(ctx, { binaryPath: binary, timeoutMs: 30_000, home });

    const exec = (cwd: string): ToolExecution =>
      ({
        callId: "call-1",
        name: "",
        arguments: {},
        agent: { session: { header: { cwd } } },
        signal: new AbortController().signal,
      }) as unknown as ToolExecution;
    const byName = (name: string): RegisteredTool => {
      const tool = tools.find((candidate) => candidate.name === name);
      assert.ok(tool, `tool ${name} registered`);
      return tool;
    };

    const resolved = await byName("workspace_resolve").execute({}, exec(workspace));
    assert.equal((resolved as { workspace: null }).workspace, null);

    const registered = await byName("workspace_register").execute({ path: workspace }, exec(workspace));
    assert.equal((registered as { created: boolean }).created, true);

    const created = await byName("memory_create").execute(
      { content: "Use SQLite for local storage.", kind: "fact" },
      exec(workspace),
    );
    const memory = (created as { memory: { memory_id: string; version: number } }).memory;
    assert.equal(memory.version, 1);

    // §4: a search hit carries a citation for version-safe writes.
    const searched = await byName("memory_search").execute({ query: "sqlite" }, exec(workspace));
    const hits = (searched as { memories: Array<{ memory_id: string; citation: { memory_id: string; version: number } }> }).memories;
    assert.equal(hits.length, 1);
    assert.equal(hits[0].citation.version, 1);
    assert.equal(hits[0].citation.memory_id, memory.memory_id);

    const listed = await byName("memory_list").execute({}, exec(workspace));
    assert.equal((listed as { memories: unknown[] }).memories.length, 1);

    const updated = await byName("memory_update").execute(
      { memory_id: memory.memory_id, expected_version: 1, content: "Use SQLite in WAL mode." },
      exec(workspace),
    );
    assert.equal((updated as { memory: { version: number } }).memory.version, 2);

    // §1: version history and a version-aware read.
    const history = await byName("memory_history").execute({ memory_id: memory.memory_id }, exec(workspace));
    const versions = (history as { versions: Array<{ version: number }> }).versions;
    assert.equal(versions.length, 2);
    const historical = await byName("memory_get").execute(
      { memory_id: memory.memory_id, version: 1 },
      exec(workspace),
    );
    assert.equal((historical as { memory: { content: string } }).memory.content, "Use SQLite for local storage.");

    const deleted = await byName("workspace_delete").execute({}, exec(workspace));
    assert.equal((deleted as { deleted: boolean }).deleted, true);

    assert.deepEqual(
      approvals.sort(),
      ["memory_create", "memory_update", "workspace_delete", "workspace_register"].sort(),
    );
  },
);
