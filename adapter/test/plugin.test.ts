// Plugin behavior against a fake ctx: tool registration, the approval
// boundary for writes, workspace-id injection from the agent's session cwd,
// and error surfacing. The fake ctx mirrors the Harness surfaces used by the
// plugin (ctx.tools.register, ctx.approval.request).
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import type { Context } from "@deepseek-ai/cordis";
import type { ToolExecution } from "@deepseek-ai/dsh-tools";

import { inject } from "../src/index.js";
import { registerMemoryNoteTools } from "../src/tools.js";
import type { MemoryNoteConfig } from "../src/tools.js";
import { applicationVersion } from "../src/version.js";

const here = path.dirname(fileURLToPath(import.meta.url));
const fakeScript = path.join(here, "fixtures", "fake-binary.mjs");
const workspacePath = "/registered/workspace";

interface RegisteredTool {
  name: string;
  execute(args: Record<string, unknown>, exec: ToolExecution): Promise<unknown>;
  output?: {
    render?: (args: unknown, value: unknown) => Array<{ type: string; text: string }>;
    presentationMeta?: (args: unknown, value: unknown) => unknown;
  };
  presentResult?: (
    args: unknown,
    result: { content: unknown; isError: boolean; meta?: unknown },
  ) => unknown;
}

interface FakeApproval {
  outcome: string;
  asked: Array<{ toolName: string; reason: string }>;
}

function makeContext(
  mode = "scripted",
  outcome = "allowed-once",
  version = applicationVersion,
): {
  ctx: Context;
  approval: FakeApproval;
  tools: RegisteredTool[];
  calls: () => Array<{ subcommand: string; request: Record<string, unknown> }>;
} {
  const approval: FakeApproval = { outcome, asked: [] };
  const tools: RegisteredTool[] = [];
  const logFile = path.join(mkdtempSync(path.join(os.tmpdir(), "dsh-ts-")), "calls.jsonl");
  const ctx = {
    tools: {
      register(tool: RegisteredTool) {
        tools.push(tool);
      },
    },
    approval: {
      async request(req: { toolName: string; reason?: string }) {
        approval.asked.push({ toolName: req.toolName, reason: req.reason ?? "" });
        return approval.outcome;
      },
    },
  } as unknown as Context;
  registerMemoryNoteTools(ctx, {
    binaryPath: fakeScript,
    timeoutMs: 2000,
    env: {
      FAKE_MODE: mode,
      FAKE_WS_PATH: workspacePath,
      FAKE_LOG: logFile,
      FAKE_VERSION: version,
    },
  } satisfies MemoryNoteConfig);
  const calls = () => {
    try {
      return readFileSync(logFile, "utf8")
        .trim()
        .split("\n")
        .filter((line) => line !== "")
        .map((line) => JSON.parse(line) as { subcommand: string; request: Record<string, unknown> });
    } catch {
      return [];
    }
  };
  return { ctx, approval, tools, calls };
}

function fakeExec(cwd?: string): ToolExecution {
  return {
    callId: "call-1",
    name: "",
    arguments: {},
    agent: cwd === undefined ? undefined : ({ session: { header: { cwd } } } as never),
    signal: new AbortController().signal,
  } as unknown as ToolExecution;
}

test("plugin declares the services it consumes", () => {
  assert.deepEqual(inject, ["tools", "approval"]);
});

test("tool renders are human summaries, not raw protocol JSON", () => {
  const { tools } = makeContext();
  const create = tools.find((tool) => tool.name === "memory_create");
  assert.ok(create?.output?.render);
  const text = create.output.render({}, {
    memory: {
      memory_id: "mem_x",
      workspace_id: 7,
      content: "Use SQLite.",
      kind: "note",
      label: null,
      source: [],
      metadata: {},
      state: "active",
      version: 1,
      supersedes: null,
      superseded_by: null,
      created_at: "2026-01-01T00:00:00+00:00",
      updated_at: "2026-01-01T00:00:00+00:00",
    },
  })[0]?.text ?? "";
  assert.match(text, /memory mem_x/);
  assert.match(text, /Use SQLite/);
  // State and dates are natural language; protocol field names stay out.
  assert.match(text, /active/);
  assert.match(text, /created 2026-01-01 00:00/);
  assert.doesNotMatch(text, /workspace_id|created_at|metadata|"state"|"active"/);
});

test("search render carries the FTS rank and matched-term count", () => {
  const { tools } = makeContext();
  const search = tools.find((tool) => tool.name === "memory_search");
  assert.ok(search?.output?.render);
  const text = search.output.render({}, {
    memories: [
      {
        memory_id: "mem_x",
        kind: "fact",
        label: "storage",
        version: 2,
        snippet: "Use SQLite in WAL mode.",
        score: 0.000001,
        matched_terms: 2,
        updated_at: "2026-01-01T00:00:00+00:00",
      },
    ],
  })[0]?.text ?? "";
  assert.match(text, /score 0\.000001/);
  assert.match(text, /matched terms 2/);
  assert.match(text, /Use SQLite in WAL mode/);
});

test("UI cards hide protocol handles from humans", () => {
  const { tools } = makeContext();
  const create = tools.find((tool) => tool.name === "memory_create");
  assert.ok(create?.output?.presentationMeta);
  const value = {
    memory: {
      memory_id: "mem_95f5644050f649e52cae4c94a08730f6",
      content: "Use SQLite.",
      kind: "fact",
      label: null,
      state: "active",
      version: 1,
    },
  };
  // presentationMeta is the persisted source of the result card content: it
  // must never carry ids, versions, or other protocol fields.
  const meta = create.output.presentationMeta({}, value) as { text: string };
  assert.match(meta.text, /Use SQLite/);
  assert.match(meta.text, /fact/);
  assert.doesNotMatch(meta.text, /mem_|version|state|workspace_id/);
});

test("apply registers exactly the 15 business tools", () => {
  const { tools } = makeContext();
  assert.equal(tools.length, 15);
  const names = tools.map((tool) => tool.name);
  assert.ok(names.includes("memory_create") && names.includes("workspace_register"));
  assert.ok(names.includes("memory_history") && names.includes("memory_diff"));
  assert.ok(!names.includes("version") && !names.includes("help"));
});

test("read tools run without asking approval", async () => {
  const { approval, tools } = makeContext();
  const search = tools.find((tool) => tool.name === "memory_search");
  assert.ok(search);
  const value = await search.execute({ query: "sqlite" }, fakeExec(workspacePath));
  assert.deepEqual(value, { memories: [] });
  assert.equal(approval.asked.length, 0);
});

test("memory_search injects the git branch from the environment", async () => {
  const previous = process.env.DSH_MEMORY_NOTE_GIT_BRANCH;
  process.env.DSH_MEMORY_NOTE_GIT_BRANCH = "main";
  try {
    const { tools, calls } = makeContext();
    const search = tools.find((tool) => tool.name === "memory_search");
    assert.ok(search);
    await search.execute({ query: "sqlite" }, fakeExec(workspacePath));
    const searchCalls = calls().filter((entry) => entry.subcommand === "memory-search");
    assert.equal(searchCalls.length, 1);
    assert.equal(searchCalls[0]?.request.branch, "main");
  } finally {
    if (previous === undefined) {
      delete process.env.DSH_MEMORY_NOTE_GIT_BRANCH;
    } else {
      process.env.DSH_MEMORY_NOTE_GIT_BRANCH = previous;
    }
  }
});

test("a v1 core is rejected before any business command", async () => {
  const { approval, tools, calls } = makeContext("scripted", "allowed-once", "1.0.0");
  const create = tools.find((tool) => tool.name === "memory_create");
  assert.ok(create);
  await assert.rejects(
    create.execute({ content: "Use SQLite.", kind: "fact" }, fakeExec(workspacePath)),
    /core version 1\.0\.0.*requires protocol v2.*reinstall/,
  );
  assert.equal(approval.asked.length, 0);
  assert.deepEqual(calls(), []);
});

test("write tools ask approval once and pass the resolved workspace_id", async () => {
  const { approval, tools, calls } = makeContext();
  const create = tools.find((tool) => tool.name === "memory_create");
  assert.ok(create);
  const value = await create.execute({ content: "Use SQLite.", kind: "fact" }, fakeExec(workspacePath));
  assert.equal((value as { memory: { workspace_id: number } }).memory.workspace_id, 7);
  assert.equal(approval.asked.length, 1);
  assert.equal(approval.asked[0]?.toolName, "memory_create");
  const reason = approval.asked[0]?.reason ?? "";
  assert.match(reason, /record new memory/);
  assert.match(reason, /Use SQLite/);
  // The approval reason is a human sentence: no subcommand name, no raw JSON.
  assert.doesNotMatch(reason, /memory_create|workspace_id|"content"/);
  const commands = calls();
  assert.equal(commands[0]?.subcommand, "workspace-resolve");
  assert.equal(commands[1]?.subcommand, "memory-create");
  assert.equal(commands[1]?.request.workspace_id, 7);
});

test("supersede approval identifies the memory by content, never by id", async () => {
  const { approval, tools } = makeContext();
  const supersede = tools.find((tool) => tool.name === "memory_supersede");
  assert.ok(supersede);
  await supersede.execute(
    { memory_id: "mem_old", expected_version: 1, new: { content: "Use PostgreSQL.", kind: "fact" } },
    fakeExec(workspacePath),
  );
  const reason = approval.asked[0]?.reason ?? "";
  assert.match(reason, /old memory content/);
  assert.match(reason, /Use PostgreSQL/);
  assert.doesNotMatch(reason, /mem_old/);
});

test("denied approval fails the write before spawning it", async () => {
  const { approval, tools, calls } = makeContext("scripted", "rejected");
  const create = tools.find((tool) => tool.name === "memory_create");
  assert.ok(create);
  await assert.rejects(create.execute({ content: "blocked", kind: "note" }, fakeExec(workspacePath)), /not granted/);
  assert.equal(approval.asked.length, 1);
  const writes = calls().filter((entry) => entry.subcommand !== "workspace-resolve");
  assert.equal(writes.length, 0);
});

test("write without agent context fails closed", async () => {
  const { tools } = makeContext();
  const create = tools.find((tool) => tool.name === "memory_create");
  assert.ok(create);
  await assert.rejects(create.execute({ content: "x", kind: "note" }, fakeExec()), /no session cwd/);
});

test("unregistered agent cwd fails with guidance", async () => {
  const { tools } = makeContext();
  const list = tools.find((tool) => tool.name === "memory_list");
  assert.ok(list);
  await assert.rejects(list.execute({}, fakeExec("/nowhere/registered")), /not registered/);
});

test("an explicit workspace_id skips injection", async () => {
  const { tools, calls } = makeContext();
  const get = tools.find((tool) => tool.name === "memory_get");
  assert.ok(get);
  const value = await get.execute({ workspace_id: 3, memory_id: "mem_fake" }, fakeExec(workspacePath));
  assert.equal((value as { memory: { workspace_id: number } }).memory.workspace_id, 3);
  assert.deepEqual(calls().map((entry) => entry.subcommand), ["memory-get"]);
});

test("Go protocol errors surface as natural-language messages", async () => {
  const { tools } = makeContext("error");
  const get = tools.find((tool) => tool.name === "memory_get");
  assert.ok(get);
  await assert.rejects(
    get.execute({ workspace_id: 1, memory_id: "mem_x" }, fakeExec(workspacePath)),
    /the memory changed since it was read/,
  );
});
