package sql

import (
	"strings"
	"testing"
	"time"
)

func TestSearchMemoryBuildsFilters(t *testing.T) {
	now := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	query, args := SearchMemory(SearchParams{
		WorkspaceID:  7,
		Terms:        []string{"sqlite", "wal"},
		Types:        []string{"decision"},
		Scopes:       []string{"storage"},
		State:        "active",
		UpdatedAfter: &now,
		Limit:        8,
	})
	for _, part := range []string{
		"workspace_id = ?",
		"state = ?",
		"type IN (?)",
		"scope IN (?)",
		"updated_at >= ?",
		"LIKE ?",
		"LIMIT ?",
	} {
		if !strings.Contains(query, part) {
			t.Fatalf("query does not contain %q: %s", part, query)
		}
	}
	if len(args) != 8 || args[0] != int64(7) || args[len(args)-1] != 8 {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestWorkspaceLockModes(t *testing.T) {
	read := InsertWorkspaceLock(false)
	write := InsertWorkspaceLock(true)
	if !strings.Contains(read, "mode = 'write'") {
		t.Fatalf("read lock query: %s", read)
	}
	if !strings.Contains(write, "1 = 1") {
		t.Fatalf("write lock query: %s", write)
	}
}
