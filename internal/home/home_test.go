package home

import (
	"path/filepath"
	"testing"
)

func TestRootPriority(t *testing.T) {
	wanted := filepath.Join(t.TempDir(), "store")
	t.Setenv(Env, wanted)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	got, err := Root()
	if err != nil || got != wanted {
		t.Fatalf("Root() = %q, %v", got, err)
	}
	if MemoryDB(got, 12) != filepath.Join(wanted, "memory", "workspace-12-memory.db") {
		t.Fatal("unexpected memory database path")
	}
}

func TestRootRejectsRelativeOverride(t *testing.T) {
	t.Setenv(Env, "relative/path")
	if _, err := Root(); err == nil {
		t.Fatal("expected an error")
	}
}
