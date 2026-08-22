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
import { CoreError, CoreErrorCode, runCore, type CoreOptions, type JsonValue } from "./core.js";

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
  type: { type: "string", description: "Caller-defined category; empty means unset." },
  scope: { type: "string", description: "Caller-defined scope; empty means unset." },
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
  description: "Memory content and metadata.",
};

const searchFilter: ParamSpec = {
  type: "object",
  properties: {
    types: { type: "array", items: { type: "string" }, description: "Exact type matches (OR)." },
    scopes: { type: "array", items: { type: "string" }, description: "Exact scope matches (OR)." },
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
    query: { type: "string", description: "Free-text keywords (ASCII case-insensitive substring)." },
    filter: searchFilter,
    limit: { type: "number", description: "1..20, default 8." },
  },
  "memory-list": {
    workspace_id: workspaceId,
    limit: { type: "number", description: "1..200, default 50." },
    cursor: { type: "string", description: "Opaque pagination cursor; pass back verbatim." },
  },
  "memory-get": { workspace_id: workspaceId, memory_id: memoryId },
  "memory-create": {
    workspace_id: workspaceId,
    content: { type: "string", required: true, description: "The fact to remember." },
    type: { type: "string", description: "Caller-defined category." },
    scope: { type: "string", description: "Caller-defined scope." },
    source: { type: "array", items: { type: "string" }, description: "Source identifiers." },
    metadata: {
      type: "object",
      additionalProperties: true,
      description: "Caller-defined extension information.",
    },
  },
  "memory-update": {
    workspace_id: workspaceId,
    memory_id: memoryId,
    expected_version: expectedVersion,
    content: { type: "string", description: "Whole replacement of the content." },
    type: { type: "string", description: 'Whole replacement; "" clears.' },
    scope: { type: "string", description: 'Whole replacement; "" clears.' },
    source: { type: "array", items: { type: "string" }, description: "Whole replacement; [] clears." },
    metadata: {
      type: "object",
      additionalProperties: true,
      description: "Whole replacement; {} clears.",
    },
  },
  "memory-supersede": {
    workspace_id: workspaceId,
    memory_id: memoryId,
    expected_version: expectedVersion,
    new: { ...memoryInput, required: true },
  },
  "memory-invalidate": { workspace_id: workspaceId, memory_id: memoryId, expected_version: expectedVersion },
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
      "Retrieve candidate active memories by keyword query and/or exact type/scope/time filters, ordered by score then recency. Candidate retrieval, not a relevance verdict.",
  },
  "memory-list": {
    name: "memory_list",
    description:
      "List memories of a workspace in any state as compact rows with cursor pagination. Use memory_get for full content.",
  },
  "memory-get": {
    name: "memory_get",
    description:
      "Read one memory precisely by ID, including content, version, source, state, and replacement relationship. The fine-grained read before a version-sensitive write.",
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
  active: "生效中",
  superseded: "已被取代",
  invalid: "已失效",
};

const ERROR_LABELS: Record<string, string> = {
  workspace_busy: "该工作区正被其他进程占用，请稍后重试",
  workspace_not_found: "工作区未注册",
  workspace_path_used: "该路径已绑定到另一个工作区",
  workspace_broken: "工作区记忆数据库缺失或损坏",
  memory_not_found: "没有找到该记忆",
  version_conflict: "该记忆已被其他操作更新，请先重新读取后再试",
  invalid_memory_state: "该记忆当前状态不允许此操作",
  home_broken: "记忆存储目录损坏，无法访问",
  schema_mismatch: "记忆存储格式不受支持",
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
  let text = `记忆 ${String(memory.memory_id ?? "")}（${STATE_LABELS[String(memory.state ?? "")] ?? String(memory.state ?? "")}，版本 ${memory.version ?? ""}）`;
  if (typeof memory.content === "string") text += `\n内容：${memory.content}`;
  if (typeof memory.type === "string" && memory.type !== "") text += `\n分类：${memory.type}`;
  if (typeof memory.scope === "string" && memory.scope !== "") text += `\n范围：${memory.scope}`;
  if (Array.isArray(memory.source) && memory.source.length > 0) text += `\n来源：${memory.source.join(", ")}`;
  if (typeof memory.supersedes_content === "string" && memory.supersedes_content !== "") {
    text += `\n取代了更早的记忆：「${memory.supersedes_content}」`;
  } else if (typeof memory.supersedes === "string") {
    text += `\n取代了更早的一条记忆`;
  }
  if (typeof memory.superseded_by_content === "string" && memory.superseded_by_content !== "") {
    text += `\n已被新记忆取代：「${memory.superseded_by_content}」`;
  } else if (typeof memory.superseded_by === "string") {
    text += `\n已被一条新记忆取代`;
  }
  const created = formatDate(memory.created_at);
  const updated = formatDate(memory.updated_at);
  if (created !== "") text += `\n创建于 ${created}`;
  if (updated !== "" && updated !== created) text += `\n修改于 ${updated}`;
  return text;
}

function renderWorkspace(workspace: Record<string, JsonValue> | null | undefined): string {
  if (workspace === null || workspace === undefined) return "未注册";
  return `工作区路径：${workspace.path}`;
}

const renderers: Record<string, Renderer> = {
  "workspace-resolve": (_args, value) => [
    { type: "text", text: renderWorkspace(value.workspace as Record<string, JsonValue> | null | undefined) },
  ],
  "workspace-register": (_args, value) => [
    {
      type: "text",
      text: `${renderWorkspace(value.workspace as Record<string, JsonValue> | null | undefined)}（${value.created ? "新建" : "已存在"}）`,
    },
  ],
  "workspace-rebind": (_args, value) => [
    { type: "text", text: renderWorkspace(value.workspace as Record<string, JsonValue> | null | undefined) },
  ],
  "workspace-clear": (_args, value) => [{ type: "text", text: `已删除 ${value.deleted_count ?? 0} 条记忆` }],
  "workspace-delete": () => [{ type: "text", text: "工作区已删除（映射与记忆数据库）" }],
  "memory-search": (_args, value) => {
    const hits = (value.memories as Array<Record<string, JsonValue>>) ?? [];
    const lines = hits.map((hit) =>
      [
        `- ${hit.memory_id} 得分 ${hit.score ?? ""} 版本 ${hit.version ?? ""}`,
        hit.type ? ` 分类 ${hit.type}` : "",
        hit.scope ? ` 范围 ${hit.scope}` : "",
        `\n  ${hit.snippet ?? ""}`,
      ].join(""),
    );
    return [{ type: "text", text: lines.length > 0 ? lines.join("\n") : "没有匹配的记忆" }];
  },
  "memory-list": (_args, value) => {
    const items = (value.memories as Array<Record<string, JsonValue>>) ?? [];
    const lines = items.map((item) =>
      [
        `- ${item.memory_id} ${STATE_LABELS[String(item.state ?? "")] ?? String(item.state ?? "")} 版本 ${item.version ?? ""}`,
        item.type ? ` 分类 ${item.type}` : "",
        item.scope ? ` 范围 ${item.scope}` : "",
      ].join(""),
    );
    return [{ type: "text", text: lines.length > 0 ? lines.join("\n") : "该工作区没有记忆" }];
  },
  "memory-get": (_args, value) => [
    { type: "text", text: renderMemory(value.memory as Record<string, JsonValue>) },
  ],
  "memory-create": (_args, value) => [
    { type: "text", text: `已记录：\n${renderMemory(value.memory as Record<string, JsonValue>)}` },
  ],
  "memory-update": (_args, value) => [
    { type: "text", text: `已更新：\n${renderMemory(value.memory as Record<string, JsonValue>)}` },
  ],
  "memory-supersede": (_args, value) => [
    {
      type: "text",
      text: `旧记忆：\n${renderMemory(value.old as Record<string, JsonValue>)}\n新记忆：\n${renderMemory(value.new as Record<string, JsonValue>)}`,
    },
  ],
  "memory-invalidate": (_args, value) => [
    { type: "text", text: `已标记无效：\n${renderMemory(value.memory as Record<string, JsonValue>)}` },
  ],
  "memory-delete": () => [{ type: "text", text: "记忆已删除（含全部历史版本）" }],
};

// ---- Presentation (human-facing UI cards, replay-safe and pure) ----
// The model-facing render keeps the memory_id handle; the UI card derives a
// clean summary from the canonical value and never shows ids, versions,
// timestamps, or other protocol fields.

function cleanMemoryText(memory: Record<string, JsonValue>): string {
  const content = typeof memory.content === "string" ? memory.content : "";
  const tags: string[] = [];
  if (typeof memory.type === "string" && memory.type !== "") tags.push(memory.type);
  if (typeof memory.scope === "string" && memory.scope !== "") tags.push(memory.scope);
  return tags.length > 0 ? `${content}\n[${tags.join(" · ")}]` : content;
}

const resultTitles: Record<string, string> = {
  "workspace-resolve": "工作区",
  "workspace-register": "工作区注册",
  "workspace-rebind": "工作区重绑定",
  "workspace-clear": "工作区清空",
  "workspace-delete": "工作区删除",
  "memory-search": "记忆检索",
  "memory-list": "记忆列表",
  "memory-get": "记忆详情",
  "memory-create": "已记录记忆",
  "memory-update": "已更新记忆",
  "memory-supersede": "已取代记忆",
  "memory-invalidate": "已标记无效",
  "memory-delete": "已删除记忆",
};

function presentationMeta(subcommand: string, value: Record<string, JsonValue>): { text: string } {
  const workspace = (value.workspace as Record<string, JsonValue> | null | undefined) ?? null;
  switch (subcommand) {
    case "workspace-resolve":
      return { text: workspace ? `路径 ${String(workspace.path ?? "")}` : "该路径未注册工作区" };
    case "workspace-register":
    case "workspace-rebind":
      return { text: `路径 ${String(workspace?.path ?? "")}` };
    case "workspace-clear":
      return { text: `已删除 ${value.deleted_count ?? 0} 条记忆` };
    case "workspace-delete":
      return { text: "已删除映射与记忆数据库" };
    case "memory-search": {
      const hits = (value.memories as Array<Record<string, JsonValue>>) ?? [];
      const lines = hits.map((hit) => String(hit.snippet ?? ""));
      return { text: lines.length > 0 ? lines.join("\n") : "没有匹配的记忆" };
    }
    case "memory-list": {
      const items = (value.memories as Array<Record<string, JsonValue>>) ?? [];
      return { text: `共 ${items.length} 条记忆` };
    }
    case "memory-supersede":
      return { text: cleanMemoryText(value.new as Record<string, JsonValue>) };
    case "memory-delete":
      return { text: "已删除（含全部历史版本）" };
    default:
      return { text: cleanMemoryText(value.memory as Record<string, JsonValue>) };
  }
}

function cardKind(subcommand: string): "read" | "search" | undefined {
  if (subcommand === "memory-search") return "search";
  if (subcommand === "workspace-resolve" || subcommand === "memory-get" || subcommand === "memory-list") return "read";
  return undefined;
}

export function registerMemoryNoteTools(ctx: Context, config: MemoryNoteConfig): void {  for (const subcommand of Object.keys(parameterSpecs)) {
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
      return `注册工作区：${text(request.path)}`;
    case "workspace_rebind":
      return `将工作区重绑定到：${text(request.path)}`;
    case "workspace_clear":
      return `清空该工作区的全部记忆与历史版本`;
    case "workspace_delete":
      return `删除该工作区的映射与记忆数据库（不影响用户目录）`;
    case "memory_create":
      return `记录新记忆：「${short(text(request.content))}」`;
    case "memory_update": {
      const previous = await currentContent(request, exec, config);
      const parts: string[] = [];
      if (typeof request.content === "string") parts.push(`内容改为「${short(request.content)}」`);
      if (request.type !== undefined) parts.push(`分类${text(request.type) === "" ? "清除" : `改为「${text(request.type)}」`}`);
      if (request.scope !== undefined) parts.push(`范围${text(request.scope) === "" ? "清除" : `改为「${text(request.scope)}」`}`);
      if (request.source !== undefined) parts.push("来源已更新");
      if (request.metadata !== undefined) parts.push("元数据已更新");
      return `更新记忆「${short(previous)}」：${parts.join("，")}`;
    }
    case "memory_supersede": {
      const previous = await currentContent(request, exec, config);
      const replacement = (request.new as Record<string, unknown> | undefined)?.content;
      return `用新记忆取代「${short(previous)}」：「${short(text(replacement))}」`;
    }
    case "memory_invalidate": {
      const previous = await currentContent(request, exec, config);
      return `将记忆「${short(previous)}」标记为无效`;
    }
    case "memory_delete": {
      const previous = await currentContent(request, exec, config);
      return `删除记忆：「${short(previous)}」`;
    }
    default:
      return toolName;
  }
}

export { CoreError, CoreErrorCode, runCore };
