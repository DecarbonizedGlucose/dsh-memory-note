// Spawns the one-shot Go core without a shell and maps its process result
// onto the protocol exit-code contract (protocol §1-2): exit 0/1 carries one
// JSON envelope on stdout, exit 2 is an argv/usage failure, and anything else
// (signal, timeout, non-JSON stdout, missing binary) is an unknown result.
import { spawn } from "node:child_process";

export interface CoreOptions {
  binaryPath: string;
  timeoutMs: number;
  /** When set, exported to the child as DSH_MEMORY_NOTE_HOME. */
  home?: string;
  /** Extra environment merged over process.env for the child. */
  env?: NodeJS.ProcessEnv;
}

/** Core protocol errors plus adapter-side process and compatibility errors. */
export const CoreErrorCode = {
  InvalidRequest: "invalid_request",
  HomeBroken: "home_broken",
  SchemaMismatch: "schema_mismatch",
  WorkspaceNotFound: "workspace_not_found",
  WorkspacePathUsed: "workspace_path_used",
  WorkspaceBusy: "workspace_busy",
  WorkspaceBroken: "workspace_broken",
  MemoryNotFound: "memory_not_found",
  VersionConflict: "version_conflict",
  InvalidMemoryState: "invalid_memory_state",
  InternalError: "internal_error",
  BinaryNotFound: "binary_not_found",
  VersionMismatch: "version_mismatch",
  UnknownResult: "unknown_result",
} as const;

export class CoreError extends Error {
  constructor(
    public readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "CoreError";
  }
}

const MAX_STDOUT_BYTES = 64 * 1024 * 1024;
const MAX_VERSION_BYTES = 256;

export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };

/**
 * Reads the core's application version and checks its protocol major version.
 * This uses the internal `version` command, which prints plain text rather
 * than a JSON envelope.
 */
export function checkCoreVersion(options: CoreOptions, expectedMajor: number): Promise<string> {
  return new Promise((resolve, reject) => {
    const env: NodeJS.ProcessEnv = { ...process.env, ...options.env };
    if (options.home !== undefined) env.DSH_MEMORY_NOTE_HOME = options.home;

    const child = spawn(options.binaryPath, ["version"], {
      env,
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    });

    let stdout = "";
    let stderr = "";
    let settled = false;

    const fail = (code: string, message: string) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      child.kill();
      reject(new CoreError(code, message));
    };

    const timer = setTimeout(
      () => fail(CoreErrorCode.UnknownResult, `core version check exceeded ${options.timeoutMs} ms`),
      options.timeoutMs,
    );

    child.stdout.on("data", (chunk: Buffer) => {
      if (settled) return;
      stdout += chunk.toString();
      if (stdout.length > MAX_VERSION_BYTES) {
        fail(CoreErrorCode.UnknownResult, "core version output exceeded 256 bytes");
      }
    });
    child.stderr.on("data", (chunk: Buffer) => {
      stderr += chunk.toString();
    });
    child.on("error", (err) => {
      fail(CoreErrorCode.BinaryNotFound, `cannot start core: ${err.message}`);
    });
    child.on("close", (code, killedBy) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);

      if (killedBy !== null) {
        reject(new CoreError(CoreErrorCode.UnknownResult, `core version check killed by ${killedBy}${diagnostics(stderr)}`));
        return;
      }
      if (code !== 0) {
        reject(new CoreError(CoreErrorCode.UnknownResult, `core version check exited with code ${code}${diagnostics(stderr)}`));
        return;
      }

      const version = stdout.trim();
      const match = /^(\d+)\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.exec(version);
      if (match === null) {
        reject(new CoreError(CoreErrorCode.UnknownResult, "core version output is invalid"));
        return;
      }
      const actualMajor = Number(match[1]);
      if (actualMajor !== expectedMajor) {
        reject(
          new CoreError(
            CoreErrorCode.VersionMismatch,
            `core version ${version} implements protocol v${actualMajor}, but this adapter requires protocol v${expectedMajor}; reinstall dsh-memory-note and restart the profile`,
          ),
        );
        return;
      }
      resolve(version);
    });
  });
}

/**
 * Runs one subcommand and resolves with the response `data` object. Go
 * protocol errors and unknown results reject with a `CoreError` carrying the
 * protocol code; the tool layer turns rejections into tool errors.
 */
export function runCore(
  options: CoreOptions,
  subcommand: string,
  request: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<Record<string, JsonValue>> {
  const requestJson = JSON.stringify(request);
  return new Promise((resolve, reject) => {
    const env: NodeJS.ProcessEnv = { ...process.env, ...options.env };
    if (options.home !== undefined) env.DSH_MEMORY_NOTE_HOME = options.home;

    const child = spawn(options.binaryPath, [subcommand, requestJson], {
      env,
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    });

    let stdout = "";
    let stderr = "";
    let settled = false;

    const fail = (code: string, message: string) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
      child.kill();
      reject(new CoreError(code, message));
    };

    const timer = setTimeout(
      () => fail(CoreErrorCode.UnknownResult, `core process exceeded ${options.timeoutMs} ms`),
      options.timeoutMs,
    );
    const onAbort = () => fail(CoreErrorCode.UnknownResult, "core process aborted");

    signal?.addEventListener("abort", onAbort, { once: true });

    child.stdout.on("data", (chunk: Buffer) => {
      if (settled) return;
      stdout += chunk.toString();
      if (stdout.length > MAX_STDOUT_BYTES) {
        fail(CoreErrorCode.UnknownResult, "core stdout exceeded 64 MiB");
      }
    });
    child.stderr.on("data", (chunk: Buffer) => {
      stderr += chunk.toString();
    });

    child.on("error", (err) => {
      fail(CoreErrorCode.BinaryNotFound, `cannot start core: ${err.message}`);
    });

    child.on("close", (code, killedBy) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);

      if (killedBy !== null) {
        reject(new CoreError(CoreErrorCode.UnknownResult, `core killed by ${killedBy}${diagnostics(stderr)}`));
        return;
      }
      if (code !== 0 && code !== 1) {
        reject(new CoreError(CoreErrorCode.UnknownResult, `unexpected core exit code ${code}${diagnostics(stderr)}`));
        return;
      }
      let raw: unknown;
      try {
        raw = JSON.parse(stdout);
      } catch {
        reject(new CoreError(CoreErrorCode.UnknownResult, `core stdout is not valid JSON${diagnostics(stderr)}`));
        return;
      }
      const envelope = parseEnvelope(raw);
      if (envelope === null) {
        reject(new CoreError(CoreErrorCode.UnknownResult, `core stdout is not a protocol envelope${diagnostics(stderr)}`));
        return;
      }
      if (envelope.ok) {
        resolve(envelope.data);
        return;
      }
      reject(new CoreError(envelope.error.code, envelope.error.message));
    });
  });
}

/** Protocol envelope; unknown fields are kept, never rejected. */
function parseEnvelope(raw: unknown): { ok: true; data: Record<string, JsonValue> } | { ok: false; error: { code: string; message: string } } | null {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return null;
  const value = raw as Record<string, unknown>;
  if (value.ok === true) {
    const data = value.data;
    if (typeof data !== "object" || data === null || Array.isArray(data) || "error" in value) return null;
    return { ok: true, data: data as Record<string, JsonValue> };
  }
  if (value.ok === false) {
    const error = value.error;
    if (typeof error !== "object" || error === null || Array.isArray(error) || "data" in value) return null;
    const body = error as Record<string, unknown>;
    if (typeof body.code !== "string" || typeof body.message !== "string") return null;
    return { ok: false, error: { code: body.code, message: body.message } };
  }
  return null;
}

function diagnostics(stderr: string): string {
  const trimmed = stderr.trim();
  if (trimmed === "") return "";
  const short = trimmed.length > 500 ? `${trimmed.slice(0, 500)}...` : trimmed;
  return `; stderr: ${short}`;
}
