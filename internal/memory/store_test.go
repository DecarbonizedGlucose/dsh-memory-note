package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "memory.db"), 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestMemoryLifecycle(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	old, err := store.Create(ctx, CreateInput{
		Content: "The user uses tool B.", Type: "preference", Scope: "editor",
		Source: []string{"chat:1"}, Metadata: map[string]any{"reason": "old"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if old.Version != 1 || old.State != Active {
		t.Fatalf("create: %#v", old)
	}
	hits, err := store.Search(ctx, SearchInput{Query: "tool", Filter: Filter{Types: []string{"preference"}}})
	if err != nil || len(hits) != 1 || hits[0].ID != old.ID {
		t.Fatalf("keyword search: %#v %v", hits, err)
	}
	content := "The user uses tool B with a custom config."
	updated, err := store.Update(ctx, UpdateInput{ID: old.ID, ExpectedVersion: 1, Content: &content})
	if err != nil || updated.Version != 2 || updated.ID != old.ID {
		t.Fatalf("update: %#v %v", updated, err)
	}
	if _, err := store.Update(ctx, UpdateInput{ID: old.ID, ExpectedVersion: 1, Content: &content}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected stale update conflict, got %v", err)
	}

	replaced, err := store.Supersede(ctx, old.ID, 2, CreateInput{
		Content: "The user now uses tool A.", Type: "preference", Scope: "editor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Old.State != Superseded || replaced.Old.SupersededBy != replaced.New.ID ||
		replaced.New.Supersedes != old.ID || replaced.New.ID == old.ID {
		t.Fatalf("supersede relation: %#v", replaced)
	}
	hits, err = store.Search(ctx, SearchInput{Query: "tool"})
	if err != nil || len(hits) != 1 || hits[0].ID != replaced.New.ID {
		t.Fatalf("default search should only return active memory: %#v %v", hits, err)
	}
}

func TestInvalidateDeleteAndClear(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	first, err := store.Create(ctx, CreateInput{Content: "Temporary plan."})
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := store.Invalidate(ctx, first.ID, 1)
	if err != nil || invalid.State != Invalid || invalid.Version != 2 {
		t.Fatalf("invalidate: %#v %v", invalid, err)
	}
	if err := store.Delete(ctx, first.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected delete conflict, got %v", err)
	}
	if err := store.Delete(ctx, first.ID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted memory still exists: %v", err)
	}
	if _, err := store.Create(ctx, CreateInput{Content: "One"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, CreateInput{Content: "Two"}); err != nil {
		t.Fatal(err)
	}
	count, err := store.Clear(ctx)
	if err != nil || count != 2 {
		t.Fatalf("clear: count=%d err=%v", count, err)
	}
}

func TestSearchNeedsCondition(t *testing.T) {
	store := testStore(t)
	_, err := store.Search(context.Background(), SearchInput{})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid request, got %v", err)
	}
}
