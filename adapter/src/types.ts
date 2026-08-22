// Response data shapes of the Go core (protocol v1). The canonical values
// returned by the tools match these; programmatic consumers can read the
// fields directly instead of parsing rendered text.
export type MemoryState = "active" | "superseded" | "invalid";

export interface Memory {
  memory_id: string;
  workspace_id: number;
  content: string;
  type: string | null;
  scope: string | null;
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
  type: string | null;
  scope: string | null;
  version: number;
  snippet: string;
  score: number;
  updated_at: string;
}

export interface ListItem {
  memory_id: string;
  type: string | null;
  scope: string | null;
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
