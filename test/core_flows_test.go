package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestWorkspaceRebind covers path re-binding: successful move (memory DB is
// not touched), same-path no-op, path conflicts, unknown WID, and invalid
// path rejection.
func TestWorkspaceRebind(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	first := t.TempDir()
	second := t.TempDir()
	other := t.TempDir()

	mustRun(t, binary, home, "workspace-register", map[string]any{"path": first})
	created := mustRun(t, binary, home, "memory-create", map[string]any{"workspace_id": 1, "content": "stays put", "kind": "note"})
	id := created.Data["memory"].(map[string]any)["memory_id"].(string)

	// Rebind to a new path.
	rebound := mustRun(t, binary, home, "workspace-rebind", map[string]any{"workspace_id": 1, "path": second})
	if rebound.Data["workspace"].(map[string]any)["path"] != second {
		t.Fatalf("rebind data = %#v", rebound)
	}
	// The memory database is keyed by WID, not path: the memory survives.
	got := mustRun(t, binary, home, "memory-get", map[string]any{"workspace_id": 1, "memory_id": id})
	if got.Data["memory"].(map[string]any)["content"] != "stays put" {
		t.Fatalf("memory after rebind = %#v", got)
	}
	// Old path no longer resolves, new path does.
	resolvedOld := mustRun(t, binary, home, "workspace-resolve", map[string]any{"path": first})
	if resolvedOld.Data["workspace"] != nil {
		t.Fatalf("old path still bound: %#v", resolvedOld)
	}
	resolvedNew := mustRun(t, binary, home, "workspace-resolve", map[string]any{"path": second})
	if resolvedNew.Data["workspace"].(map[string]any)["workspace_id"] != float64(1) {
		t.Fatalf("new path resolve = %#v", resolvedNew)
	}

	// Same-path rebind is a no-op success.
	mustRun(t, binary, home, "workspace-rebind", map[string]any{"workspace_id": 1, "path": second})

	// A path already owned by another WID is rejected.
	mustRun(t, binary, home, "workspace-register", map[string]any{"path": other})
	wantCoreError(t, binary, home, "workspace-rebind", map[string]any{
		"workspace_id": 1, "path": other,
	}, "workspace_path_used")

	// Unknown WID and nonexistent path are rejected.
	wantCoreError(t, binary, home, "workspace-rebind", map[string]any{
		"workspace_id": 99, "path": second,
	}, "workspace_not_found")
	wantCoreError(t, binary, home, "workspace-rebind", map[string]any{
		"workspace_id": 1, "path": filepath.Join(t.TempDir(), "missing"),
	}, "invalid_request")
}

// TestWorkspaceClearThenRead verifies clear removes memories and history while
// keeping the workspace alive and readable.
func TestWorkspaceClearThenRead(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	workspace := t.TempDir()
	mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})

	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		created := mustRun(t, binary, home, "memory-create", map[string]any{
			"workspace_id": 1, "content": fmt.Sprintf("memory %d", i), "kind": "note",
		})
		ids[i] = created.Data["memory"].(map[string]any)["memory_id"].(string)
	}

	cleared := mustRun(t, binary, home, "workspace-clear", map[string]any{"workspace_id": 1})
	if cleared.Data["deleted_count"] != float64(3) {
		t.Fatalf("clear data = %#v", cleared)
	}
	listed := mustRun(t, binary, home, "memory-list", map[string]any{"workspace_id": 1})
	if memories := listed.Data["memories"].([]any); len(memories) != 0 {
		t.Fatalf("list after clear = %#v", listed)
	}
	wantCoreError(t, binary, home, "memory-get", map[string]any{
		"workspace_id": 1, "memory_id": ids[0],
	}, "memory_not_found")
	// The workspace mapping survives.
	resolved := mustRun(t, binary, home, "workspace-resolve", map[string]any{"path": workspace})
	if resolved.Data["workspace"] == nil {
		t.Fatalf("workspace gone after clear: %#v", resolved)
	}
}

// TestMemoryStateAndVersionErrors pins the protocol §8 check order: existence,
// then expected version, then state — and that delete checks version but not
// state.
func TestMemoryStateAndVersionErrors(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	workspace := t.TempDir()
	mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})

	created := mustRun(t, binary, home, "memory-create", map[string]any{"workspace_id": 1, "content": "target", "kind": "note"})
	id := created.Data["memory"].(map[string]any)["memory_id"].(string)

	// Version check comes first.
	wantCoreError(t, binary, home, "memory-invalidate", map[string]any{
		"workspace_id": 1, "memory_id": id, "expected_version": 99,
	}, "version_conflict")

	mustRun(t, binary, home, "memory-invalidate", map[string]any{
		"workspace_id": 1, "memory_id": id, "expected_version": 1,
	})

	// Now version 2 matches but state is invalid: state errors.
	wantCoreError(t, binary, home, "memory-update", map[string]any{
		"workspace_id": 1, "memory_id": id, "expected_version": 2, "content": "nope",
	}, "invalid_memory_state")
	wantCoreError(t, binary, home, "memory-supersede", map[string]any{
		"workspace_id": 1, "memory_id": id, "expected_version": 2,
		"new": map[string]any{"content": "replacement", "kind": "note"},
	}, "invalid_memory_state")

	// Delete checks version, not state.
	wantCoreError(t, binary, home, "memory-delete", map[string]any{
		"workspace_id": 1, "memory_id": id, "expected_version": 99,
	}, "version_conflict")
	mustRun(t, binary, home, "memory-delete", map[string]any{
		"workspace_id": 1, "memory_id": id, "expected_version": 2,
	})
}

// TestMemorySearchFilters pins filter semantics: exact kind/label matches,
// OR within a dimension, AND across dimensions, filter-only score 0, empty
// array rejection, and time-boundary validation.
func TestMemorySearchFilters(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	workspace := t.TempDir()
	mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})

	for _, entry := range []struct {
		content string
		kind    string
		label   string
	}{
		{"alpha one", "fact", "a"},
		{"beta two", "fact", "b"},
		{"gamma three", "note", "a"},
	} {
		mustRun(t, binary, home, "memory-create", map[string]any{
			"workspace_id": 1, "content": entry.content, "kind": entry.kind, "label": entry.label,
		})
	}

	byQuery := mustRun(t, binary, home, "memory-search", map[string]any{"workspace_id": 1, "query": "alpha"})
	if memories := byQuery.Data["memories"].([]any); len(memories) != 1 {
		t.Fatalf("query search = %#v", byQuery)
	}

	byKind := mustRun(t, binary, home, "memory-search", map[string]any{
		"workspace_id": 1, "filter": map[string]any{"kinds": []string{"fact"}},
	})
	hits := byKind.Data["memories"].([]any)
	if len(hits) != 2 || hits[0].(map[string]any)["score"] != float64(0) {
		t.Fatalf("kind-filter search (score must be 0) = %#v", byKind)
	}

	byLabel := mustRun(t, binary, home, "memory-search", map[string]any{
		"workspace_id": 1, "filter": map[string]any{"labels": []string{"a"}},
	})
	if memories := byLabel.Data["memories"].([]any); len(memories) != 2 {
		t.Fatalf("label-filter search = %#v", byLabel)
	}

	combined := mustRun(t, binary, home, "memory-search", map[string]any{
		"workspace_id": 1, "query": "beta", "filter": map[string]any{"kinds": []string{"fact"}},
	})
	if memories := combined.Data["memories"].([]any); len(memories) != 1 {
		t.Fatalf("query+filter search = %#v", combined)
	}

	wantCoreError(t, binary, home, "memory-search", map[string]any{
		"workspace_id": 1, "filter": map[string]any{"kinds": []string{}},
	}, "invalid_request")
	wantCoreError(t, binary, home, "memory-search", map[string]any{
		"workspace_id": 1,
		"filter": map[string]any{
			"created_after": "2099-01-01T00:00:00Z", "created_before": "2000-01-01T00:00:00Z",
		},
	}, "invalid_request")
}

// TestMemoryListPagination exercises cursor pagination across processes.
func TestMemoryListPagination(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	workspace := t.TempDir()
	mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})

	for i := 0; i < 5; i++ {
		mustRun(t, binary, home, "memory-create", map[string]any{
			"workspace_id": 1, "content": fmt.Sprintf("page memory %d", i), "kind": "note",
		})
	}

	seen := map[string]bool{}
	var cursor any
	pages := 0
	for {
		request := map[string]any{"workspace_id": 1, "limit": 2}
		if cursor != nil {
			request["cursor"] = cursor
		}
		page := mustRun(t, binary, home, "memory-list", request)
		for _, item := range page.Data["memories"].([]any) {
			id := item.(map[string]any)["memory_id"].(string)
			if seen[id] {
				t.Fatalf("duplicate memory across pages: %s", id)
			}
			seen[id] = true
		}
		pages++
		if page.Data["next_cursor"] == nil {
			break
		}
		cursor = page.Data["next_cursor"]
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 5 {
		t.Fatalf("pagination returned %d memories, want 5", len(seen))
	}
}

// TestWorkspaceIdNotReused verifies a deleted WID is never reassigned.
func TestWorkspaceIdNotReused(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	workspace := t.TempDir()

	first := mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})
	firstID := first.Data["workspace"].(map[string]any)["workspace_id"].(float64)
	mustRun(t, binary, home, "workspace-delete", map[string]any{"workspace_id": firstID})

	second := mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})
	secondID := second.Data["workspace"].(map[string]any)["workspace_id"].(float64)
	if secondID == firstID {
		t.Fatalf("workspace id reused: %v", secondID)
	}
}

// TestBranchFilterAcrossProcesses verifies branch-scoped memories filter out of
// search across independent core processes.
func TestBranchFilterAcrossProcesses(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	workspace := t.TempDir()
	mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})

	mustRun(t, binary, home, "memory-create", map[string]any{
		"workspace_id": 1, "content": "branch main only", "kind": "note", "branches": []string{"main"},
	})
	mustRun(t, binary, home, "memory-create", map[string]any{
		"workspace_id": 1, "content": "branch all", "kind": "note",
	})

	search := func(branch any) int {
		request := map[string]any{"workspace_id": 1, "query": "branch"}
		if branch != nil {
			request["branch"] = branch
		}
		response := mustRun(t, binary, home, "memory-search", request)
		return len(response.Data["memories"].([]any))
	}
	if got := search(nil); got != 2 {
		t.Fatalf("no branch returned %d, want 2", got)
	}
	if got := search("main"); got != 2 {
		t.Fatalf("branch=main returned %d, want 2", got)
	}
	if got := search("dev"); got != 1 {
		t.Fatalf("branch=dev returned %d, want 1", got)
	}
}

// TestMemoryHistoryAcrossProcesses verifies version history and version-aware
// reads survive across independent core processes.
func TestMemoryHistoryAcrossProcesses(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	workspace := t.TempDir()
	mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})

	created := mustRun(t, binary, home, "memory-create", map[string]any{
		"workspace_id": 1, "content": "Use SQLite.", "kind": "note",
	})
	id := created.Data["memory"].(map[string]any)["memory_id"].(string)

	mustRun(t, binary, home, "memory-update", map[string]any{
		"workspace_id": 1, "memory_id": id, "expected_version": 1, "content": "Use SQLite in WAL mode.",
	})

	history := mustRun(t, binary, home, "memory-history", map[string]any{"workspace_id": 1, "memory_id": id})
	if versions := history.Data["versions"].([]any); len(versions) != 2 {
		t.Fatalf("history = %#v, want 2 versions", history)
	}

	historical := mustRun(t, binary, home, "memory-get", map[string]any{
		"workspace_id": 1, "memory_id": id, "version": 1,
	})
	if historical.Data["memory"].(map[string]any)["content"] != "Use SQLite." {
		t.Fatalf("historical read = %#v", historical)
	}

	// memory-diff compares two versions across processes.
	diff := mustRun(t, binary, home, "memory-diff", map[string]any{
		"workspace_id": 1, "memory_id": id, "from_version": 1, "to_version": 2,
	})
	changes := diff.Data["changes"].([]any)
	if len(changes) != 1 || changes[0].(map[string]any)["field"] != "content" {
		t.Fatalf("diff = %#v, want one content change", diff)
	}
}
