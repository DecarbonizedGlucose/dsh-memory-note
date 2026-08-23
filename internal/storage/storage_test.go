package storage

import (
	"path/filepath"
	"testing"
)

func TestHomePriority(t *testing.T) {
	wanted := filepath.Join(t.TempDir(), "store")
	t.Setenv(Env, wanted)
	got, err := Home()
	if err != nil || got != wanted {
		t.Fatalf("Home() = %q, %v", got, err)
	}
	if MemoryDB(got, 12) != filepath.Join(wanted, "memory", "workspace-12-memory.db") {
		t.Fatal("unexpected memory database path")
	}
}

func TestHomeRejectsRelativeOverride(t *testing.T) {
	t.Setenv(Env, "relative/path")
	if _, err := Home(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestCanonicalPath(t *testing.T) {
	dir := t.TempDir()
	got, err := CanonicalPath(dir)
	if err != nil || got != normalizeCase(filepath.Clean(dir)) {
		t.Fatalf("CanonicalPath(%q) = %q, %v", dir, got, err)
	}
	if _, err := CanonicalPath(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected error for a missing path")
	}
	if _, err := CanonicalPath("relative/path"); err == nil {
		t.Fatal("expected error for a relative path")
	}
}
