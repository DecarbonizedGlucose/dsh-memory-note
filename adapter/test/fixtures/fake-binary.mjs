#!/usr/bin/env node
// Scripted stand-in for the Go binary used by unit tests. Executed directly
// (shebang), so its argv (process.argv.slice(2)) is exactly
// [subcommand, request-json] — the same argv shape the real binary receives.
import { appendFileSync, writeFileSync } from "node:fs";

const mode = process.env.FAKE_MODE ?? "ok";
const args = process.argv.slice(2);
const [subcommand, raw] = args;
let request = {};
try {
  request = raw === undefined ? {} : JSON.parse(raw);
} catch {
  // invalid JSON: scripted mode only cares about well-formed calls
}

if (process.env.FAKE_ECHO_FILE) {
  writeFileSync(process.env.FAKE_ECHO_FILE, JSON.stringify(args));
}
if (process.env.FAKE_LOG) {
  appendFileSync(process.env.FAKE_LOG, JSON.stringify({ subcommand, request }) + "\n");
}

function respond(data) {
  process.stdout.write(JSON.stringify({ ok: true, data }));
  process.exit(0);
}
function fail(code, message) {
  process.stdout.write(JSON.stringify({ ok: false, error: { code, message } }));
  process.exit(1);
}

const memory = (req) => ({
  memory_id: "mem_fake",
  workspace_id: req.workspace_id,
  content: req.content ?? "",
  type: null,
  scope: null,
  source: [],
  metadata: {},
  state: "active",
  version: 1,
  supersedes: null,
  superseded_by: null,
  created_at: "2026-01-01T00:00:00+00:00",
  updated_at: "2026-01-01T00:00:00+00:00",
});
const workspace = {
  workspace_id: 7,
  path: process.env.FAKE_WS_PATH ?? "/fake",
  created_at: "2026-01-01T00:00:00+00:00",
  updated_at: "2026-01-01T00:00:00+00:00",
};

if (mode === "usage") {
  process.stderr.write("usage text\n");
  process.exit(2);
} else if (mode === "garbage") {
  process.stdout.write("not json");
  process.exit(0);
} else if (mode === "hang") {
  setInterval(() => {}, 1000);
} else if (mode === "error") {
  fail("version_conflict", "nope");
} else if (mode === "home") {
  respond({ home: process.env.DSH_MEMORY_NOTE_HOME ?? null });
} else if (mode === "scripted") {
  switch (subcommand) {
    case "workspace-resolve":
      if (request.path === process.env.FAKE_WS_PATH) respond({ workspace });
      respond({ workspace: null });
      break;
    case "workspace-register":
      respond({ workspace, created: true });
      break;
    case "memory-create":
      respond({ memory: memory(request) });
      break;
    case "memory-list":
      respond({ memories: [] });
      break;
    case "memory-search":
      respond({ memories: [] });
      break;
    case "memory-get":
      respond({ memory: memory({ ...request, content: "旧记忆内容" }) });
      break;
    default:
      respond({});
  }
} else {
  respond({ argv: args });
}
