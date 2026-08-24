// dsh-memory-note: DeepSeek Harness plugin exposing the one-shot Go memory
// core as tools. Registration and configuration follow the Harness plugin
// conventions (develop/basic): export name/inject/apply, a Schemastery
// Config, and package as a bundle via cordis.patch.yml.
import type { Context } from "@deepseek-ai/cordis";
import Schema from "@deepseek-ai/schemastery";
import { registerMemoryNoteTools } from "./tools.js";

export const name = "memory-note";

export const inject = ["tools", "approval"];

export interface Config {
  /** The Go core executable; resolved through PATH unless absolute. */
  binaryPath: string;
  /** Per-call timeout for the one-shot core process. */
  timeoutMs: number;
  /** When set, exported to the core as DSH_MEMORY_NOTE_HOME. */
  home?: string;
}

export const Config: Schema<Config> = Schema.object({
  binaryPath: Schema.string().default("dsh-memory-note"),
  timeoutMs: Schema.number().default(30000),
  home: Schema.string(),
});

export function apply(ctx: Context, config: Config) {
  registerMemoryNoteTools(ctx, {
    binaryPath: config.binaryPath,
    timeoutMs: config.timeoutMs,
    home: config.home,
  });
  console.log("[memory-note] plugin loaded: 15 tools registered");
}
