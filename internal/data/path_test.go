package data

import (
	"path/filepath"
	"testing"
)

func TestPaths(t *testing.T) {
	home := t.TempDir()
	path, err := MemoryDB(home, 123)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "memory", "workspace-123-memory.db")
	if path != want {
		t.Fatalf("got %q, want %q", path, want)
	}
	if _, err := MemoryDB(home, -1); err == nil {
		t.Fatal("negative workspace id was accepted")
	}
}
