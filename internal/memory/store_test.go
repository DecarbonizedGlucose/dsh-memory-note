package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
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
	old, err := store.Create(ctx, protocol.MemoryCreateRequest{
		WorkspaceID: 1,
		MemoryCreate: protocol.MemoryCreate{
			Content: "The user uses tool B.", Type: "preference", Scope: "editor",
			Source: []string{"chat:1"}, Metadata: map[string]any{"reason": "old"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if old.Version != 1 || old.State != protocol.MemoryActive {
		t.Fatalf("create: %#v", old)
	}
	hits, err := store.Search(ctx, protocol.MemorySearchRequest{
		WorkspaceID: 1,
		Query:       "tool",
		Filter:      protocol.MemoryFilter{Types: []string{"preference"}},
	})
	if err != nil || len(hits) != 1 || hits[0].ID != old.ID {
		t.Fatalf("keyword search: %#v %v", hits, err)
	}
	content := "The user uses tool B with a custom config."
	updated, err := store.Update(ctx, protocol.MemoryUpdateRequest{
		WorkspaceID: 1, MemoryID: old.ID, ExpectedVersion: 1, Content: &content,
	})
	if err != nil || updated.Version != 2 || updated.ID != old.ID {
		t.Fatalf("update: %#v %v", updated, err)
	}
	if _, err := store.Update(ctx, protocol.MemoryUpdateRequest{
		WorkspaceID: 1, MemoryID: old.ID, ExpectedVersion: 1, Content: &content,
	}); !errors.Is(err, protocol.ErrVersionConflict) {
		t.Fatalf("expected stale update conflict, got %v", err)
	}

	replaced, err := store.Supersede(ctx, protocol.MemorySupersedeRequest{
		WorkspaceID:     1,
		MemoryID:        old.ID,
		ExpectedVersion: 2,
		New: protocol.MemoryCreate{
			Content: "The user now uses tool A.", Type: "preference", Scope: "editor",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Old.State != protocol.MemorySuperseded || replaced.Old.SupersededBy != replaced.New.ID ||
		replaced.New.Supersedes != old.ID || replaced.New.ID == old.ID {
		t.Fatalf("supersede relation: %#v", replaced)
	}
	hits, err = store.Search(ctx, protocol.MemorySearchRequest{WorkspaceID: 1, Query: "tool"})
	if err != nil || len(hits) != 1 || hits[0].ID != replaced.New.ID {
		t.Fatalf("default search should only return active memory: %#v %v", hits, err)
	}
}

func TestInvalidateDeleteAndClear(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	first, err := store.Create(ctx, protocol.MemoryCreateRequest{
		WorkspaceID:  1,
		MemoryCreate: protocol.MemoryCreate{Content: "Temporary plan."},
	})
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := store.Invalidate(ctx, protocol.MemoryInvalidateRequest{
		WorkspaceID: 1, MemoryID: first.ID, ExpectedVersion: 1,
	})
	if err != nil || invalid.State != protocol.MemoryInvalid || invalid.Version != 2 {
		t.Fatalf("invalidate: %#v %v", invalid, err)
	}
	if err := store.Delete(ctx, protocol.MemoryDeleteRequest{
		WorkspaceID: 1, MemoryID: first.ID, ExpectedVersion: 1,
	}); !errors.Is(err, protocol.ErrVersionConflict) {
		t.Fatalf("expected delete conflict, got %v", err)
	}
	if err := store.Delete(ctx, protocol.MemoryDeleteRequest{
		WorkspaceID: 1, MemoryID: first.ID, ExpectedVersion: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, protocol.MemoryGetRequest{WorkspaceID: 1, MemoryID: first.ID}); !errors.Is(err, protocol.ErrMemoryNotFound) {
		t.Fatalf("deleted memory still exists: %v", err)
	}
	if _, err := store.Create(ctx, protocol.MemoryCreateRequest{WorkspaceID: 1, MemoryCreate: protocol.MemoryCreate{Content: "One"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, protocol.MemoryCreateRequest{WorkspaceID: 1, MemoryCreate: protocol.MemoryCreate{Content: "Two"}}); err != nil {
		t.Fatal(err)
	}
	count, err := store.Clear(ctx)
	if err != nil || count != 2 {
		t.Fatalf("clear: count=%d err=%v", count, err)
	}
}

func TestSearchNeedsCondition(t *testing.T) {
	store := testStore(t)
	_, err := store.Search(context.Background(), protocol.MemorySearchRequest{})
	if !errors.Is(err, protocol.ErrInvalidRequest) {
		t.Fatalf("expected invalid request, got %v", err)
	}
}
