// Response data shapes of the Go core (protocol v2). The canonical values
// returned by the tools match these; programmatic consumers can read the
// fields directly instead of parsing rendered text.
export type MemoryState = "active" | "superseded" | "invalid";

export interface Memory {
  memory_id: string;
  workspace_id: number;
  content: string;
  kind: "fact" | "note";
  label: string | null;
  source: string[];
  metadata: Record<string, unknown>;
  state: MemoryState;
  version: number;
  supersedes: string | null;
  superseded_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface SearchHit {
  memory_id: string;
  kind: "fact" | "note";
  label: string | null;
  version: number;
  snippet: string;
  score: number;
  matched_terms: number;
  updated_at: string;
}

export interface ListItem {
  memory_id: string;
  kind: "fact" | "note";
  label: string | null;
  state: MemoryState;
  version: number;
  supersedes: string | null;
  superseded_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface Workspace {
  workspace_id: number;
  path: string;
  created_at: string;
  updated_at: string;
}
