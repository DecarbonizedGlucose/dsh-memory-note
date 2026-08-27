package statements

import (
	"strings"
	"testing"
)

func TestMemorySearchStatement(t *testing.T) {
	query := MemorySearch(true, 2, 1, true, true, true, true)
	if !strings.Contains(query, "memory_fts MATCH ?") || !strings.Contains(query, "bm25(memory_fts)") {
		t.Fatalf("query does not use FTS5 rank: %s", query)
	}
	if !strings.Contains(query, "m.kind IN (?, ?)") || !strings.Contains(query, "m.label IN (?)") {
		t.Fatalf("query does not contain bound exact filters: %s", query)
	}
	if count := strings.Count(query, "?"); count != 10 {
		t.Fatalf("placeholder count = %d, want 10: %s", count, query)
	}

	filterOnly := MemorySearch(false, 0, 0, false, false, false, false)
	if strings.Contains(filterOnly, "memory_fts") || strings.Contains(filterOnly, "bm25") {
		t.Fatalf("filter-only query unexpectedly uses FTS5: %s", filterOnly)
	}
	if count := strings.Count(filterOnly, "?"); count != 2 {
		t.Fatalf("filter-only placeholder count = %d, want 2: %s", count, filterOnly)
	}
}
