// The tool catalog: one Harness tool per business subcommand of the Go core
// (protocol §5). version and help are internal core commands, not tools.
//
// - Read tools execute directly.
// - Write tools ask the user through ctx.approval (the Harness approval
//   mechanism) before spawning the core; only 'allowed-once' grants.
// - workspace_id / path may be omitted in tool arguments: the adapter fills
//   them from the agent's session cwd, so models never fabricate WIDs.
import type { Context } from "@deepseek-ai/cordis";
import { defineTool, type ToolExecution } from "@deepseek-ai/dsh-tools";
import type { ApprovalOutcome } from "@deepseek-ai/dsh-user-approval";
import { readFileSync } from "node:fs";
import path from "node:path";
import { checkCoreVersion, CoreError, CoreErrorCode, runCore, type CoreOptions, type JsonValue } from "./core.js";
import { protocolMajor } from "./version.js";

export interface MemoryNoteConfig {
  binaryPath: string;
  timeoutMs: number;
  home?: string;
  /** Extra environment merged over process.env for core children (tests). */
  env?: NodeJS.ProcessEnv;
}

function coreOptions(config: MemoryNoteConfig): CoreOptions {
  return {
    binaryPath: config.binaryPath,
    timeoutMs: config.timeoutMs,
    home: config.home,
    env: config.env,
  };
}

// ---------------------------------------------------------------------------
// Parameter specs (shared with the fixture-conformance test)

export interface ParamSpec {
  type: "string" | "number" | "boolean" | "array" | "object";
  required?: boolean;
  description?: string;
  items?: ParamSpec;
  properties?: Record<string, ParamSpec>;
  additionalProperties?: boolean;
}

const workspaceId: ParamSpec = {
  type: "number",
  description:
    "Workspace ID. Omit to use the agent's current workspace; the tool resolves it automatically.",
};

const memoryId: ParamSpec = {
  type: "string",
  required: true,
  description: "The memory ID returned by memory_get / memory_create.",
};

const expectedVersion: ParamSpec = {
  type: "number",
  required: true,
  description: "The version the caller read last; a mismatch fails with version_conflict.",
};

const memoryInputParams: Record<string, ParamSpec> = {
  content: { type: "string", required: true, description: "The fact to remember." },
  kind: {
    type: "string",
    required: true,
    description: "The memory's track: fact (a durable conclusion) or note (a transient note).",
  },
  label: { type: "string", description: "An open caller-defined tag; empty means unset." },
  branches: {
    type: "array",
    items: { type: "string" },
    description: "Git branches this memory is limited to; omit (or empty) for all branches.",
  },
  source: { type: "array", items: { type: "string" }, description: "Source identifiers." },
  metadata: {
    type: "object",
    additionalProperties: true,
    description: "Caller-defined extension information.",
  },
};

const memoryInput: ParamSpec = {
  type: "object",
  properties: memoryInputParams,
  additionalProperties: false,
  description: "Memory content, track, and metadata.",
};

const searchFilter: ParamSpec = {
  type: "object",
  properties: {
    kinds: { type: "array", items: { type: "string" }, description: "Exact kind matches (OR)." },
    labels: { type: "array", items: { type: "string" }, description: "Exact label matches (OR)." },
    created_after: { type: "string", description: "RFC 3339 lower bound, inclusive." },
    created_before: { type: "string", description: "RFC 3339 upper bound, inclusive." },
    updated_after: { type: "string", description: "RFC 3339 lower bound, inclusive." },
    updated_before: { type: "string", description: "RFC 3339 upper bound, inclusive." },
  },
  additionalProperties: false,
  description: "Exact-match filter dimensions.",
};

export const parameterSpecs: Record<string, Record<string, ParamSpec>> = {
  "workspace-resolve": {
    path: { type: "string", description: "Absolute workspace path. Defaults to the agent's cwd." },
  },
  "workspace-register": {
    path: { type: "string", required: true, description: "Absolute workspace path to register." },
  },
  "workspace-rebind": {
    workspace_id: workspaceId,
    path: { type: "string", required: true, description: "New absolute path after a move or rename." },
  },
  "workspace-clear": { workspace_id: workspaceId },
  "workspace-delete": { workspace_id: workspaceId },
  "memory-search": {
    workspace_id: workspaceId,
    query: { type: "string", description: "Free-text terms matched by the local FTS5 index." },
    filter: searchFilter,
    limit: { type: "number", description: "1..20, default 8." },
    branch: {
      type: "string",
      description:
        "Restrict results to memories visible on this git branch. Omit to auto-detect the agent's current branch; a memory with no branch restriction is always visible.",
    },
  },
  "memory-list": {
    workspace_id: workspaceId,
    limit: { type: "number", description: "1..200, default 50." },
    cursor: { type: "string", description: "Opaque pagination cursor; pass back verbatim." },
  },
  "memory-get": {
    workspace_id: workspaceId,
    memory_id: memoryId,
    version: { type: "number", description: "Read that exact historical version instead of the current one." },
  },
  "memory-history": { workspace_id: workspaceId, memory_id: memoryId },
  "memory-diff": {
    workspace_id: workspaceId,
    memory_id: memoryId,
    from_version: { type: "number", required: true, description: "First version to compare." },
    to_version: { type: "number", required: true, description: "Second version to compare; must differ." },
  },
  "memory-create": {
    workspace_id: workspaceId,
    content: { type: "string", required: true, description: "The fact to remember." },
    kind: {
      type: "string",
      required: true,
      description: "The memory's track: fact (a durable conclusion) or note (a transient note).",
    },
    label: { type: "string", description: "An open caller-defined tag." },
    branches: {
      type: "array",
      items: { type: "string" },
      description: "Git branches this memory is limited to; omit (or empty) for all branches.",
    },
    source: { type: "array", items: { type: "string" }, description: "Source identifiers." },
    metadata: {
      type: "object",
      additionalProperties: true,
      description: "Caller-defined extension information.",
    },
    reason: { type: "string", description: "Why this is being recorded (audit only)." },
  },
  "memory-update": {
    workspace_id: workspaceId,
    memory_id: memoryId,
    expected_version: expectedVersion,
    content: { type: "string", description: "Whole replacement of the content." },
    kind: { type: "string", description: "Whole replacement; one of fact or note." },
    label: { type: "string", description: 'Whole replacement; "" clears.' },
    branches: {
      type: "array",
      items: { type: "string" },
      description: "Whole replacement; [] clears the restriction (all branches).",
    },
    source: { type: "array", items: { type: "string" }, description: "Whole replacement; [] clears." },
    metadata: {
      type: "object",
      additionalProperties: true,
      description: "Whole replacement; {} clears.",
    },
    reason: { type: "string", description: "Why this revision is made (audit only)." },
  },
  "memory-supersede": {
    workspace_id: workspaceId,
    memory_id: memoryId,
    expected_version: expectedVersion,
    new: { ...memoryInput, required: true },
    reason: { type: "string", description: "Why the old fact is replaced (audit only)." },
  },
  "memory-invalidate": {
    workspace_id: workspaceId,
    memory_id: memoryId,
    expected_version: expectedVersion,
    reason: { type: "string", description: "Why the fact is no longer true (audit only)." },
  },
  "memory-delete": { workspace_id: workspaceId, memory_id: memoryId, expected_version: expectedVersion },
};

const WRITE_TOOLS = new Set([
  "workspace_register",
  "workspace_rebind",
  "workspace_clear",
  "workspace_delete",
  "memory_create",
  "memory_update",
  "memory_supersede",
  "memory_invalidate",
  "memory_delete",
]);

const TOOL_NAMES: Record<string, { name: string; description: string }> = {
  "workspace-resolve": {
    name: "workspace_resolve",
    description:
      "Look up the workspace ID and metadata registered for an absolute path. Returns workspace: null when unregistered.",
  },
  "workspace-register": {
    name: "workspace_register",
    description:
      "Register an absolute workspace path, assigning it a stable workspace ID and preparing its memory database. Idempotent per canonical path.",
  },
  "workspace-rebind": {
    name: "workspace_rebind",
    description:
      "Bind an existing workspace ID to a new absolute path after the directory moved or was renamed. The memory database is not moved.",
  },
  "workspace-clear": {
    name: "workspace_clear",
    description:
      "Remove every memory (and all archived versions) of a workspace while keeping its ID, path binding, and database files.",
  },
  "workspace-delete": {
    name: "workspace_delete",
    description:
      "Delete a workspace mapping and its memory database and sidecar files. The user's workspace directory is not touched; the ID is never reused.",
  },
  "memory-search": {
    name: "memory_search",
    description:
      "Retrieve candidate active memories with local FTS5 lexical search and/or exact kind, label, and time filters. A higher score is a better BM25 match; this is candidate retrieval, not a relevance verdict. Each hit carries a citation (memory_id + version); pass citation.memory_id and citation.version as a later mutation's memory_id and expected_version so a change between search and write surfaces as version_conflict.",
  },
  "memory-list": {
    name: "memory_list",
    description:
      "List memories of a workspace in any state as compact rows with cursor pagination. Use memory_get for full content.",
  },
  "memory-get": {
    name: "memory_get",
    description:
      "Read one memory precisely by ID, including content, version, source, state, and replacement relationship. An optional version reads that exact historical version (the preview step before a rollback).",
  },
  "memory-history": {
    name: "memory_history",
    description:
      "List the version history of one memory: each version's number, producing action, state, and times, without content. Use memory_get with a version to read a full historical version.",
  },
  "memory-diff": {
    name: "memory_diff",
    description:
      "Compare two existing versions of one memory and return only the fields that changed, each with from and to values.",
  },
  "memory-create": {
    name: "memory_create",
    description: "Record a new fact as a new active memory with version 1.",
  },
  "memory-update": {
    name: "memory_update",
    description:
      "Revise the same fact: keep the memory ID, require expected_version, and increment the version. Fields replace wholesale; missing fields keep their value.",
  },
  "memory-supersede": {
    name: "memory_supersede",
    description:
      "Replace an old fact with a new one: the old record becomes superseded, a new active memory is created, and the replacement relationship is recorded.",
  },
  "memory-invalidate": {
    name: "memory_invalidate",
    description:
      "Mark a memory invalid when no new fact replaces it. The record stays retrievable for audit.",
  },
  "memory-delete": {
    name: "memory_delete",
    description:
      "Physically delete a memory row and all of its archived versions. For explicit user deletion requests or sensitive content, not for normal staleness.",
  },
};

type Renderer = (args: unknown, value: Record<string, JsonValue>) => Array<{ type: "text"; text: string }>;

// Natural-language labels for displayable protocol values (design principles).
const STATE_LABELS: Record<string, string> = {
  active: "active",
  superseded: "superseded",
  invalid: "invalid",
};

const ACTION_LABELS: Record<string, string> = {
  create: "create",
  update: "update",
  supersede: "supersede",
  invalidate: "invalidate",
};

const KIND_LABELS: Record<string, string> = {
  fact: "fact",
  note: "note",
};

const ERROR_LABELS: Record<string, string> = {
  workspace_busy: "the workspace is locked by another process; retry shortly",
  workspace_not_found: "the workspace is not registered",
  workspace_path_used: "the path is already bound to another workspace",
  workspace_broken: "the workspace memory database is missing or corrupt",
  memory_not_found: "no such memory",
  version_conflict: "the memory changed since it was read; re-read before retrying",
  invalid_memory_state: "the memory state does not allow this operation",
  home_broken: "the memory store directory is corrupt",
  schema_mismatch: "the memory store format is unsupported",
};

// formatDate keeps the printed wall time but drops the machine suffix (T and
// UTC offset) for a natural reading. Pure and replay-safe.
function formatDate(value: unknown): string {
  if (typeof value !== "string") return "";
  const match = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2})/.exec(value);
  return match ? `${match[1]} ${match[2]}` : value;
}

// renderMemory keeps exactly two protocol handles the model must echo back
// verbatim — memory_id and version — and renders every other displayable
// value in natural language: state, dates, and replacement relationships
// expressed as the related memory's content, never as an id.
function renderMemory(memory: Record<string, JsonValue>): string {
  let text = `memory ${String(memory.memory_id ?? "")} (${STATE_LABELS[String(memory.state ?? "")] ?? String(memory.state ?? "")}, version ${memory.version ?? ""})`;
  if (typeof memory.content === "string") text += `\ncontent: ${memory.content}`;
  if (typeof memory.kind === "string" && memory.kind !== "") text += `\nkind: ${KIND_LABELS[memory.kind] ?? memory.kind}`;
  if (typeof memory.label === "string" && memory.label !== "") text += `\nlabel: ${memory.label}`;
  if (Array.isArray(memory.branches) && memory.branches.length > 0) text += `\nbranches: ${memory.branches.join(", ")}`;
  if (Array.isArray(memory.source) && memory.source.length > 0) text += `\nsource: ${memory.source.join(", ")}`;
  if (typeof memory.supersedes_content === "string" && memory.supersedes_content !== "") {
    text += `\nsupersedes an earlier memory: "${memory.supersedes_content}"`;
  } else if (typeof memory.supersedes === "string") {
    text += `\nsupersedes an earlier memory`;
  }
  if (typeof memory.superseded_by_content === "string" && memory.superseded_by_content !== "") {
    text += `\nsuperseded by a newer memory: "${memory.superseded_by_content}"`;
  } else if (typeof memory.superseded_by === "string") {
    text += `\nsuperseded by a newer memory`;
  }
  const created = formatDate(memory.created_at);
  const updated = formatDate(memory.updated_at);
  if (created !== "") text += `\ncreated ${created}`;
  if (updated !== "" && updated !== created) text += `\nupdated ${updated}`;
  return text;
}

function renderWorkspace(workspace: Record<string, JsonValue> | null | undefined): string {
  if (workspace === null || workspace === undefined) return "unregistered";
  return `workspace path: ${workspace.path}`;
}

const renderers: Record<string, Renderer> = {
  "workspace-resolve": (_args, value) => [
    { type: "text", text: renderWorkspace(value.workspace as Record<string, JsonValue> | null | undefined) },
  ],
  "workspace-register": (_args, value) => [
    {
      type: "text",
      text: `${renderWorkspace(value.workspace as Record<string, JsonValue> | null | undefined)} (${value.created ? "new" : "existing"})`,
    },
  ],
  "workspace-rebind": (_args, value) => [
    { type: "text", text: renderWorkspace(value.workspace as Record<string, JsonValue> | null | undefined) },
  ],
  "workspace-clear": (_args, value) => [{ type: "text", text: `removed ${value.deleted_count ?? 0} memories` }],
  "workspace-delete": () => [{ type: "text", text: "workspace deleted (mapping and memory database)" }],
  "memory-search": (_args, value) => {
    const hits = (value.memories as Array<Record<string, JsonValue>>) ?? [];
    const lines = hits.map((hit) => {
      const citation = hit.citation as Record<string, JsonValue> | undefined;
      const cite =
        typeof citation?.memory_id === "string" && typeof citation?.version === "number"
          ? ` cite ${citation.memory_id}@${citation.version}`
          : "";
      return [
        `- ${hit.memory_id} score ${hit.score ?? ""} matched terms ${hit.matched_terms ?? 0} version ${hit.version ?? ""}${cite}`,
        typeof hit.kind === "string" ? ` kind ${KIND_LABELS[hit.kind] ?? hit.kind}` : "",
        hit.label ? ` label ${hit.label}` : "",
        `\n  ${hit.snippet ?? ""}`,
      ].join("");
    });
    return [{ type: "text", text: lines.length > 0 ? lines.join("\n") : "no matching memories" }];
  },
  "memory-list": (_args, value) => {
    const items = (value.memories as Array<Record<string, JsonValue>>) ?? [];
    const lines = items.map((item) =>
      [
        `- ${item.memory_id} ${STATE_LABELS[String(item.state ?? "")] ?? String(item.state ?? "")} version ${item.version ?? ""}`,
        typeof item.kind === "string" ? ` kind ${KIND_LABELS[item.kind] ?? item.kind}` : "",
        item.label ? ` label ${item.label}` : "",
      ].join(""),
    );
    return [{ type: "text", text: lines.length > 0 ? lines.join("\n") : "no memories in this workspace" }];
  },
  "memory-get": (_args, value) => [
    { type: "text", text: renderMemory(value.memory as Record<string, JsonValue>) },
  ],
  "memory-history": (_args, value) => {
    const versions = (value.versions as Array<Record<string, JsonValue>>) ?? [];
    const lines = versions.map((item) => {
      const version = item.version ?? "";
      const action = ACTION_LABELS[String(item.action ?? "")] ?? String(item.action ?? "");
      const state = STATE_LABELS[String(item.state ?? "")] ?? String(item.state ?? "");
      const updated = formatDate(item.updated_at);
      const archived = item.archived_at === null ? null : formatDate(item.archived_at);
      const tail = archived ? `, superseded at ${archived}` : " (current)";
      return `- version ${version} ${action}, ${state}, ${updated}${tail}`;
    });
    return [{ type: "text", text: lines.length > 0 ? lines.join("\n") : "no version history" }];
  },
  "memory-diff": (_args, value) => {
    const changes = (value.changes as Array<Record<string, JsonValue>>) ?? [];
    const lines = changes.map((change) => {
      const field = String(change.field ?? "");
      const from = JSON.stringify(change.from ?? null);
      const to = JSON.stringify(change.to ?? null);
      return `- ${field}: ${from} -> ${to}`;
    });
    const header = `version ${value.from_version ?? ""} vs ${value.to_version ?? ""} differences`;
    return [{ type: "text", text: lines.length > 0 ? `${header}\n${lines.join("\n")}` : `${header}\nno differences` }];
  },
  "memory-create": (_args, value) => [
    { type: "text", text: `recorded:\n${renderMemory(value.memory as Record<string, JsonValue>)}` },
  ],
  "memory-update": (_args, value) => [
    { type: "text", text: `updated:\n${renderMemory(value.memory as Record<string, JsonValue>)}` },
  ],
  "memory-supersede": (_args, value) => [
    {
      type: "text",
      text: `old:\n${renderMemory(value.old as Record<string, JsonValue>)}\nnew:\n${renderMemory(value.new as Record<string, JsonValue>)}`,
    },
  ],
  "memory-invalidate": (_args, value) => [
    { type: "text", text: `invalidated:\n${renderMemory(value.memory as Record<string, JsonValue>)}` },
  ],
  "memory-delete": () => [{ type: "text", text: "memory deleted (including all historical versions)" }],
};

// ---- Presentation (human-facing UI cards, replay-safe and pure) ----
// The model-facing render keeps the memory_id handle; the UI card derives a
// clean summary from the canonical value and never shows ids, versions,
// timestamps, or other protocol fields.

function cleanMemoryText(memory: Record<string, JsonValue>): string {
  const content = typeof memory.content === "string" ? memory.content : "";
  const tags: string[] = [];
  if (typeof memory.kind === "string" && memory.kind !== "") tags.push(KIND_LABELS[memory.kind] ?? memory.kind);
  if (typeof memory.label === "string" && memory.label !== "") tags.push(memory.label);
  return tags.length > 0 ? `${content}\n[${tags.join(" · ")}]` : content;
}

const resultTitles: Record<string, string> = {
  "workspace-resolve": "Workspace",
  "workspace-register": "Workspace Registered",
  "workspace-rebind": "Workspace Rebound",
  "workspace-clear": "Workspace Cleared",
  "workspace-delete": "Workspace Deleted",
  "memory-search": "Memory Search",
  "memory-list": "Memory List",
  "memory-get": "Memory Detail",
  "memory-history": "Version History",
  "memory-diff": "Version Diff",
  "memory-create": "Memory Recorded",
  "memory-update": "Memory Updated",
  "memory-supersede": "Memory Superseded",
  "memory-invalidate": "Memory Invalidated",
  "memory-delete": "Memory Deleted",
};

function presentationMeta(subcommand: string, value: Record<string, JsonValue>): { text: string } {
  const workspace = (value.workspace as Record<string, JsonValue> | null | undefined) ?? null;
  switch (subcommand) {
    case "workspace-resolve":
      return { text: workspace ? `path ${String(workspace.path ?? "")}` : "this path is not a registered workspace" };
    case "workspace-register":
    case "workspace-rebind":
      return { text: `path ${String(workspace?.path ?? "")}` };
    case "workspace-clear":
      return { text: `removed ${value.deleted_count ?? 0} memories` };
    case "workspace-delete":
      return { text: "removed mapping and memory database" };
    case "memory-search": {
      const hits = (value.memories as Array<Record<string, JsonValue>>) ?? [];
      const lines = hits.map((hit) => String(hit.snippet ?? ""));
      return { text: lines.length > 0 ? lines.join("\n") : "no matching memories" };
    }
    case "memory-list": {
      const items = (value.memories as Array<Record<string, JsonValue>>) ?? [];
      return { text: `${items.length} memories` };
    }
    case "memory-history": {
      const versions = (value.versions as Array<Record<string, JsonValue>>) ?? [];
      return { text: `${versions.length} versions` };
    }
    case "memory-diff": {
      const changes = (value.changes as Array<Record<string, JsonValue>>) ?? [];
      return { text: `${changes.length} changes` };
    }
    case "memory-supersede":
      return { text: cleanMemoryText(value.new as Record<string, JsonValue>) };
    case "memory-delete":
      return { text: "deleted (including all historical versions)" };
    default:
      return { text: cleanMemoryText(value.memory as Record<string, JsonValue>) };
  }
}

function cardKind(subcommand: string): "read" | "search" | undefined {
  if (subcommand === "memory-search") return "search";
  if (
    subcommand === "workspace-resolve" ||
    subcommand === "memory-get" ||
    subcommand === "memory-list" ||
    subcommand === "memory-history" ||
    subcommand === "memory-diff"
  )
    return "read";
  return undefined;
}

export function registerMemoryNoteTools(ctx: Context, config: MemoryNoteConfig): void {
  let coreCheck: Promise<string> | undefined;
  const ensureCore = async (): Promise<void> => {
    coreCheck ??= checkCoreVersion(coreOptions(config), protocolMajor);
    try {
      await coreCheck;
    } catch (err) {
      // Allow recovery after the core is reinstalled without restarting a
      // long-running profile. Successful checks stay cached.
      coreCheck = undefined;
      throw err;
    }
  };

  for (const subcommand of Object.keys(parameterSpecs)) {
    const spec = TOOL_NAMES[subcommand];
    const parameters = parameterSpecs[subcommand];
    const needsWorkspaceId = "workspace_id" in parameters;
    const defaultsPath = subcommand === "workspace-resolve";

    ctx.tools.register(
      defineTool({
        name: spec.name,
        description: spec.description,
        parameters: parameters as ParametersOf,
        output: {
          // The canonical value stays complete for programmatic consumers
          // (Code Mode); the render produces only the model-facing summary.
          schema: { type: "object", additionalProperties: true },
          render: renderers[subcommand] ?? ((_args, value) => [{ type: "text", text: JSON.stringify(value) }]),
          presentationMeta: (_args, value) => presentationMeta(subcommand, value as Record<string, JsonValue>),
        },
        presentCall: () => ({ card: "generic", title: resultTitles[subcommand], kind: cardKind(subcommand) }),
        presentResult: (_args, result) => {
          const meta = result.meta as { text?: string } | undefined;
          return meta?.text !== undefined
            ? { card: "generic", title: resultTitles[subcommand], content: [{ type: "text", text: meta.text }] }
            : undefined;
        },
        async execute(args, exec) {
          try {
            await ensureCore();
          } catch (err) {
            throw friendlyError(err);
          }
          const request: Record<string, unknown> = { ...(args as Record<string, unknown>) };
          if (defaultsPath && request.path === undefined) {
            const cwd = agentCwd(exec);
            if (cwd === undefined) {
              throw new Error("path is required when the agent has no session cwd");
            }
            request.path = cwd;
          }
          if (needsWorkspaceId && request.workspace_id === undefined) {
            request.workspace_id = await resolveWorkspaceId(exec, config);
          }
          if (subcommand === "memory-search" && request.branch === undefined) {
            const branch = resolveBranch(exec, config);
            if (branch !== undefined) request.branch = branch;
          }
          if (WRITE_TOOLS.has(spec.name)) {
            await requireApproval(ctx, exec, spec.name, request, config);
          }
          try {
            const value = await runCore(coreOptions(config), subcommand, request, exec.signal);
            await enrichRelationships(subcommand, value, exec, config);
            return value;
          } catch (err) {
            throw friendlyError(err);
          }
        },
      }),
    );
  }
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type ParametersOf = any;

/** The agent's session cwd (agent.session.header.cwd), per the tool-bash
 * convention. Sessions created without a cwd leave it undefined; callers must
 * then use an explicit workspace_id or path. */
function agentCwd(exec: ToolExecution): string | undefined {
  const agent = exec.agent as unknown as { session?: { header?: { cwd?: unknown } } } | undefined;
  const cwd = agent?.session?.header?.cwd;
  return typeof cwd === "string" ? cwd : undefined;
}

/** The agent's current git branch: an explicit env override wins, otherwise
 * the cwd's `.git/HEAD` is read. Non-git and detached-HEAD yield undefined,
 * meaning "no branch filter". */
function resolveBranch(exec: ToolExecution, config: MemoryNoteConfig): string | undefined {
  const envBranch = config.env?.["DSH_MEMORY_NOTE_GIT_BRANCH"] ?? process.env.DSH_MEMORY_NOTE_GIT_BRANCH;
  if (envBranch) return envBranch;
  const cwd = agentCwd(exec);
  if (cwd === undefined) return undefined;
  try {
    const head = readFileSync(path.join(cwd, ".git", "HEAD"), "utf8").trim();
    const match = /^ref: refs\/heads\/(.+)$/.exec(head);
    return match ? match[1] : undefined;
  } catch {
    return undefined;
  }
}

async function resolveWorkspaceId(exec: ToolExecution, config: MemoryNoteConfig): Promise<number> {
  const cwd = agentCwd(exec);
  if (cwd === undefined) {
    throw new Error("workspace_id is required when the agent has no session cwd");
  }
  const data = await runCore(coreOptions(config), "workspace-resolve", { path: cwd }, exec.signal);
  const workspace = data.workspace as { workspace_id?: unknown } | null | undefined;
  if (workspace === null || workspace === undefined || typeof workspace.workspace_id !== "number") {
    throw new Error(`workspace ${cwd} is not registered; run workspace_register first`);
  }
  return workspace.workspace_id;
}

// friendlyError maps protocol error codes to natural-language messages for the
// model; codes stay available to programmatic consumers via CoreError.
function friendlyError(err: unknown): Error {
  if (err instanceof CoreError) {
    const label = ERROR_LABELS[err.code];
    if (label !== undefined) return new Error(label);
  }
  return err instanceof Error ? err : new Error(String(err));
}

const RELATION_COMMANDS = new Set(["memory-get", "memory-create", "memory-update", "memory-invalidate"]);

// enrichRelationships resolves supersede ids to their content so the render
// can describe relationships in natural language (design principles: the
// related memory's content, never its id). It only runs when a relationship
// exists and fails soft (empty content) on lookup errors.
async function enrichRelationships(
  subcommand: string,
  value: Record<string, JsonValue>,
  exec: ToolExecution,
  config: MemoryNoteConfig,
): Promise<void> {
  if (!RELATION_COMMANDS.has(subcommand)) return;
  const memory = value.memory as Record<string, JsonValue> | undefined;
  if (memory === undefined) return;
  if (typeof memory.supersedes === "string") {
    memory.supersedes_content = await fetchContent(memory.supersedes, memory.workspace_id, exec, config);
  }
  if (typeof memory.superseded_by === "string") {
    memory.superseded_by_content = await fetchContent(memory.superseded_by, memory.workspace_id, exec, config);
  }
}

async function fetchContent(
  memoryId: string,
  workspaceId: unknown,
  exec: ToolExecution,
  config: MemoryNoteConfig,
): Promise<string> {
  try {
    const data = await runCore(
      coreOptions(config),
      "memory-get",
      { workspace_id: workspaceId, memory_id: memoryId },
      exec.signal,
    );
    const memory = data.memory as Record<string, JsonValue> | undefined;
    return typeof memory?.content === "string" ? memory.content : "";
  } catch {
    return "";
  }
}

async function requireApproval(
  ctx: Context,
  exec: ToolExecution,
  toolName: string,
  request: Record<string, unknown>,
  config: MemoryNoteConfig,
): Promise<void> {
  if (exec.agent === undefined) {
    throw new Error(`user approval is required for ${toolName}, but no agent context is available`);
  }
  const outcome: ApprovalOutcome = await ctx.approval.request({
    agent: exec.agent,
    toolName,
    callId: exec.callId,
    reason: await describeApproval(toolName, request, exec, config),
    signal: exec.signal,
  });
  if (outcome !== "allowed-once") {
    throw new Error(`user approval for ${toolName} was not granted (${outcome})`);
  }
}

// currentContent reads the target memory's content so the approval reason can
// identify it by what it says instead of by its opaque id. It is a read, so it
// runs before any approval prompt.
async function currentContent(
  request: Record<string, unknown>,
  exec: ToolExecution,
  config: MemoryNoteConfig,
): Promise<string> {
  const data = await runCore(
    coreOptions(config),
    "memory-get",
    { workspace_id: request.workspace_id, memory_id: request.memory_id },
    exec.signal,
  );
  const memory = data.memory as Record<string, JsonValue> | undefined;
  return typeof memory?.content === "string" ? memory.content : "";
}

// describeApproval builds the user-facing reason for the approval prompt: a
// human sentence per command. Neither the subcommand name, the request JSON,
// nor memory ids are shown; a target memory is identified by its content.
async function describeApproval(
  toolName: string,
  request: Record<string, unknown>,
  exec: ToolExecution,
  config: MemoryNoteConfig,
): Promise<string> {
  const text = (value: unknown): string => (typeof value === "string" ? value : "");
  const short = (value: string, limit = 80): string =>
    value.length > limit ? `${value.slice(0, limit)}…` : value;

  switch (toolName) {
    case "workspace_register":
      return `register workspace: ${text(request.path)}`;
    case "workspace_rebind":
      return `rebind workspace to: ${text(request.path)}`;
    case "workspace_clear":
      return `clear all memories and history of this workspace`;
    case "workspace_delete":
      return `delete this workspace's mapping and memory database (the user's directory is untouched)`;
    case "memory_create":
      return `record new memory: "${short(text(request.content))}"`;
    case "memory_update": {
      const previous = await currentContent(request, exec, config);
      const parts: string[] = [];
      if (typeof request.content === "string") parts.push(`content → "${short(request.content)}"`);
      if (request.kind !== undefined) parts.push(`kind → "${text(request.kind)}"`);
      if (request.label !== undefined) parts.push(`label ${text(request.label) === "" ? "cleared" : `→ "${text(request.label)}"`}`);
      if (request.branches !== undefined) {
        const branches = Array.isArray(request.branches) ? request.branches.join(", ") : "";
        parts.push(branches === "" ? "branch restriction cleared" : `branches → "${branches}"`);
      }
      if (request.source !== undefined) parts.push("source updated");
      if (request.metadata !== undefined) parts.push("metadata updated");
      return `update memory "${short(previous)}": ${parts.join(", ")}`;
    }
    case "memory_supersede": {
      const previous = await currentContent(request, exec, config);
      const replacement = (request.new as Record<string, unknown> | undefined)?.content;
      return `replace memory "${short(previous)}" with: "${short(text(replacement))}"`;
    }
    case "memory_invalidate": {
      const previous = await currentContent(request, exec, config);
      return `mark memory "${short(previous)}" as invalid`;
    }
    case "memory_delete": {
      const previous = await currentContent(request, exec, config);
      return `delete memory: "${short(previous)}"`;
    }
    default:
      return toolName;
  }
}

export { CoreError, CoreErrorCode, runCore };
