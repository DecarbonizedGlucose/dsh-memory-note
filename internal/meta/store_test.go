package meta

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceIdentityAndRebind(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	root := t.TempDir()
	firstPath := filepath.Join(root, "one", "same-name")
	secondPath := filepath.Join(root, "two", "same-name")
	first, created, err := store.Register(ctx, firstPath)
	if err != nil || !created {
		t.Fatalf("register first: %#v %v", first, err)
	}
	again, created, err := store.Register(ctx, firstPath)
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("repeat register: %#v %v", again, err)
	}
	second, created, err := store.Register(ctx, secondPath)
	if err != nil || !created || second.ID == first.ID {
		t.Fatalf("same name at another path: %#v %v", second, err)
	}
	newPath := filepath.Join(root, "moved")
	moved, err := store.Rebind(ctx, first.ID, newPath)
	if err != nil || moved.ID != first.ID || moved.Path != newPath {
		t.Fatalf("rebind: %#v %v", moved, err)
	}
}

func TestWorkspaceReadWriteLock(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "meta.db")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	workspace, _, err := first.Register(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	readOne, err := first.LockRead(ctx, workspace.ID, "session_one")
	if err != nil {
		t.Fatal(err)
	}
	readTwo, err := second.LockRead(ctx, workspace.ID, "session_two")
	if err != nil {
		t.Fatal("second reader should be allowed:", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
	defer cancel()
	if _, err := second.LockWrite(waitCtx, workspace.ID, "session_writer"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("writer passed active readers: %v", err)
	}
	if err := readTwo.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if err := readOne.Release(ctx); err != nil {
		t.Fatal(err)
	}
	write, err := second.LockWrite(ctx, workspace.ID, "session_writer")
	if err != nil {
		t.Fatal("writer should succeed after readers leave:", err)
	}
	defer write.Release(ctx)
	waitCtx, cancel = context.WithTimeout(ctx, 60*time.Millisecond)
	defer cancel()
	if _, err := first.LockRead(waitCtx, workspace.ID, "session_three"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("reader passed active writer: %v", err)
	}
}
