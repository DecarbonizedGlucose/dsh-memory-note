package command

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/meta"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

func TestWorkspaceAndMemoryLifecycle(t *testing.T) {
	ctx := context.Background()
	storeRoot := filepath.Join(t.TempDir(), "store")
	firstPath := t.TempDir()
	secondPath := t.TempDir()
	thirdPath := t.TempDir()

	// A read command on a fresh store initializes HOME and reports an
	// unregistered workspace as null.
	fresh := wantData[protocol.WorkspaceResolveData](t,
		call(t, ctx, storeRoot, "workspace-resolve", map[string]any{"path": firstPath}))
	if fresh.Workspace != nil {
		t.Fatalf("resolve on fresh store = %#v", fresh)
	}

	registered := call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": firstPath})
	registerData := wantData[protocol.WorkspaceRegisterData](t, registered)
	if registerData.Workspace.ID != 1 || !registerData.Created {
		t.Fatalf("register data = %#v", registerData)
	}
	wid := registerData.Workspace.ID
	wantError(t, call(t, ctx, storeRoot, "memory-create", map[string]any{"workspace_id": wid}), protocol.CodeInvalidRequest)

	again := wantData[protocol.WorkspaceRegisterData](t,
		call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": firstPath}))
	if again.Created || again.Workspace.ID != wid {
		t.Fatalf("idempotent register = %#v", again)
	}

	created := wantData[protocol.MemoryCreateData](t, call(t, ctx, storeRoot, "memory-create", map[string]any{
		"workspace_id": wid,
		"content":      "Use SQLite for local storage.",
		"kind":         " fact ",
		"label":        "storage",
		"source":       []string{" chat:1 ", "chat:1", ""},
		"metadata":     map[string]any{"reason": "local", "optional": nil},
	}))
	item := created.Memory
	if item.State != protocol.MemoryActive || item.Version != 1 || item.Kind != "fact" || len(item.Source) != 1 {
		t.Fatalf("created memory = %#v", item)
	}

	got := wantData[protocol.MemoryGetData](t, call(t, ctx, storeRoot, "memory-get", map[string]any{
		"workspace_id": wid, "memory_id": item.ID,
	}))
	if got.Memory.ID != item.ID || got.Memory.Metadata["optional"] != nil {
		t.Fatalf("get data = %#v", got)
	}

	search := wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "query": "sqlite missing", "filter": map[string]any{"kinds": []string{"fact"}},
	}))
	if len(search.Memories) != 1 || search.Memories[0].Score <= 0 || search.Memories[0].MatchedTerms != 1 {
		t.Fatalf("search data = %#v", search)
	}
	// The hit's citation seeds a later mutation's memory_id + expected_version.
	if search.Memories[0].Citation.MemoryID != item.ID || search.Memories[0].Citation.Version != item.Version {
		t.Fatalf("citation = %#v, want %s@%d", search.Memories[0].Citation, item.ID, item.Version)
	}

	// ASCII case folding is applied before the query reaches FTS5.
	filterOnly := wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "query": "SQLITE", "filter": map[string]any{"labels": []string{"storage"}},
	}))
	if len(filterOnly.Memories) != 1 || filterOnly.Memories[0].Score <= 0 || filterOnly.Memories[0].MatchedTerms != 1 {
		t.Fatalf("filter-only search data = %#v", filterOnly)
	}
	scored := wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "filter": map[string]any{"labels": []string{"storage"}},
	}))
	if len(scored.Memories) != 1 || scored.Memories[0].Score != 0 || scored.Memories[0].MatchedTerms != 0 {
		t.Fatalf("filter-only score = %#v", scored)
	}
	wantError(t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "filter": map[string]any{"kinds": []string{}},
	}), protocol.CodeInvalidRequest)
	wantError(t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "filter": map[string]any{"kinds": []string{"other"}},
	}), protocol.CodeInvalidRequest)

	updated := wantData[protocol.MemoryUpdateData](t, call(t, ctx, storeRoot, "memory-update", map[string]any{
		"workspace_id": wid, "memory_id": item.ID, "expected_version": 1,
		"content": "Use SQLite in WAL mode.", "label": "", "source": []string{}, "metadata": map[string]any{},
	}))
	item = updated.Memory
	if item.Version != 2 || item.Label != nil || len(item.Source) != 0 || len(item.Metadata) != 0 {
		t.Fatalf("updated memory = %#v", item)
	}
	wantError(t, call(t, ctx, storeRoot, "memory-update", map[string]any{
		"workspace_id": wid, "memory_id": item.ID, "expected_version": 1, "label": "other",
	}), protocol.CodeVersionConflict)

	superseded := wantData[protocol.MemorySupersedeData](t, call(t, ctx, storeRoot, "memory-supersede", map[string]any{
		"workspace_id": wid, "memory_id": item.ID, "expected_version": 2,
		"new": map[string]any{"content": "Use PostgreSQL for shared storage.", "kind": "fact"},
	}))
	if superseded.Old.State != protocol.MemorySuperseded || superseded.Old.Version != 3 ||
		superseded.New.Supersedes == nil || *superseded.New.Supersedes != item.ID {
		t.Fatalf("supersede data = %#v", superseded)
	}

	deleted := wantData[protocol.MemoryDeleteData](t, call(t, ctx, storeRoot, "memory-delete", map[string]any{
		"workspace_id": wid, "memory_id": item.ID, "expected_version": 3,
	}))
	if !deleted.Deleted {
		t.Fatal("memory was not deleted")
	}
	newItem := wantData[protocol.MemoryGetData](t, call(t, ctx, storeRoot, "memory-get", map[string]any{
		"workspace_id": wid, "memory_id": superseded.New.ID,
	})).Memory
	if newItem.Supersedes != nil || newItem.Version != 2 {
		t.Fatalf("relationship cleanup = %#v", newItem)
	}

	invalidated := wantData[protocol.MemoryInvalidateData](t, call(t, ctx, storeRoot, "memory-invalidate", map[string]any{
		"workspace_id": wid, "memory_id": newItem.ID, "expected_version": 2,
	})).Memory
	if invalidated.State != protocol.MemoryInvalid || invalidated.Version != 3 {
		t.Fatalf("invalidated memory = %#v", invalidated)
	}
	wantError(t, call(t, ctx, storeRoot, "memory-invalidate", map[string]any{
		"workspace_id": wid, "memory_id": newItem.ID, "expected_version": 3,
	}), protocol.CodeInvalidMemoryState)
	search = wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "query": "PostgreSQL",
	}))
	if len(search.Memories) != 0 {
		t.Fatalf("search returned inactive memory: %#v", search)
	}

	// memory-list sees every state as compact rows without content.
	listed := wantData[protocol.MemoryListData](t, call(t, ctx, storeRoot, "memory-list", map[string]any{"workspace_id": wid}))
	if len(listed.Memories) != 1 || listed.Memories[0].ID != newItem.ID || listed.Memories[0].State != protocol.MemoryInvalid || listed.NextCursor != nil {
		t.Fatalf("list data = %#v", listed)
	}

	rebound := wantData[protocol.WorkspaceRebindData](t, call(t, ctx, storeRoot, "workspace-rebind", map[string]any{
		"workspace_id": wid, "path": secondPath,
	}))
	if rebound.Workspace.Path != secondPath {
		t.Fatalf("rebind data = %#v", rebound)
	}
	cleared := wantData[protocol.WorkspaceClearData](t, call(t, ctx, storeRoot, "workspace-clear", map[string]any{"workspace_id": wid}))
	if cleared.DeletedCount != 1 {
		t.Fatalf("clear data = %#v", cleared)
	}
	journal := filepath.Join(storeRoot, "memory", "workspace-1-memory.db-journal")
	if err := os.WriteFile(journal, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	wantData[protocol.WorkspaceDeleteData](t, call(t, ctx, storeRoot, "workspace-delete", map[string]any{"workspace_id": wid}))
	if _, err := os.Lstat(journal); !os.IsNotExist(err) {
		t.Fatalf("rollback journal survived workspace-delete: %v", err)
	}

	next := wantData[protocol.WorkspaceRegisterData](t,
		call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": thirdPath}))
	if next.Workspace.ID != 2 {
		t.Fatalf("workspace ID was reused: %#v", next)
	}
}

func TestMemorySearchFTS(t *testing.T) {
	ctx := context.Background()
	storeRoot := filepath.Join(t.TempDir(), "store")
	project := t.TempDir()
	wid := wantData[protocol.WorkspaceRegisterData](t,
		call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": project})).Workspace.ID

	strong := wantData[protocol.MemoryCreateData](t, call(t, ctx, storeRoot, "memory-create", map[string]any{
		"workspace_id": wid, "content": "sqlite sqlite sqlite", "kind": "fact", "label": "database",
	})).Memory
	weak := wantData[protocol.MemoryCreateData](t, call(t, ctx, storeRoot, "memory-create", map[string]any{
		"workspace_id": wid, "content": "sqlite", "kind": "note", "label": "scratch",
	})).Memory

	// BM25 ranks repeated evidence first, while matched_terms reports distinct
	// prepared terms rather than term frequency.
	result := wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "query": "sqlite missing", "limit": 2,
	}))
	if len(result.Memories) != 2 || result.Memories[0].ID != strong.ID ||
		result.Memories[0].Score <= result.Memories[1].Score || result.Memories[0].MatchedTerms != 1 {
		t.Fatalf("ranked FTS result = %#v", result)
	}

	// FTS token matching is not the old application substring scan.
	substring := wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "query": "lite",
	}))
	if len(substring.Memories) != 0 {
		t.Fatalf("substring unexpectedly matched FTS token: %#v", substring)
	}

	// FTS operators in caller text become ordinary quoted terms. NOT does not
	// exclude the row that matches sqlite.
	literal := wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "query": "sqlite NOT missing",
	}))
	if len(literal.Memories) != 2 {
		t.Fatalf("caller FTS syntax was interpreted: %#v", literal)
	}

	// Exact filters are pushed into SQL and remain independent dimensions.
	filtered := wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "query": "database", "filter": map[string]any{"kinds": []string{"fact"}},
	}))
	if len(filtered.Memories) != 1 || filtered.Memories[0].ID != strong.ID {
		t.Fatalf("indexed label and kind filter = %#v", filtered)
	}

	// The update trigger removes old indexed text and inserts the new value.
	wantData[protocol.MemoryUpdateData](t, call(t, ctx, storeRoot, "memory-update", map[string]any{
		"workspace_id": wid, "memory_id": strong.ID, "expected_version": 1, "content": "postgres postgres",
	}))
	afterUpdate := wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "query": "sqlite",
	}))
	if len(afterUpdate.Memories) != 1 || afterUpdate.Memories[0].ID != weak.ID {
		t.Fatalf("stale FTS text after update = %#v", afterUpdate)
	}
	postgres := wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "query": "postgres",
	}))
	if len(postgres.Memories) != 1 || postgres.Memories[0].ID != strong.ID {
		t.Fatalf("updated FTS text missing = %#v", postgres)
	}

	// Indexed rows may remain physically present after a state-only change,
	// but inactive rows never enter the candidate set.
	wantData[protocol.MemoryInvalidateData](t, call(t, ctx, storeRoot, "memory-invalidate", map[string]any{
		"workspace_id": wid, "memory_id": strong.ID, "expected_version": 2,
	}))
	postgres = wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", map[string]any{
		"workspace_id": wid, "query": "postgres",
	}))
	if len(postgres.Memories) != 0 {
		t.Fatalf("inactive memory entered FTS candidates: %#v", postgres)
	}
}

func TestMemoryListPagination(t *testing.T) {
	ctx := context.Background()
	storeRoot := filepath.Join(t.TempDir(), "store")
	project := t.TempDir()
	wid := wantData[protocol.WorkspaceRegisterData](t,
		call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": project})).Workspace.ID
	for index := 0; index < 3; index++ {
		wantData[protocol.MemoryCreateData](t, call(t, ctx, storeRoot, "memory-create", map[string]any{
			"workspace_id": wid, "content": "memory number", "kind": "note", "label": string(rune('a' + index)),
		}))
	}
	first := wantData[protocol.MemoryListData](t, call(t, ctx, storeRoot, "memory-list", map[string]any{
		"workspace_id": wid, "limit": 2,
	}))
	if len(first.Memories) != 2 || first.NextCursor == nil {
		t.Fatalf("first page = %#v", first)
	}
	second := wantData[protocol.MemoryListData](t, call(t, ctx, storeRoot, "memory-list", map[string]any{
		"workspace_id": wid, "limit": 2, "cursor": *first.NextCursor,
	}))
	if len(second.Memories) != 1 || second.NextCursor != nil {
		t.Fatalf("second page = %#v", second)
	}
	wantError(t, call(t, ctx, storeRoot, "memory-list", map[string]any{
		"workspace_id": wid, "cursor": "garbage",
	}), protocol.CodeInvalidRequest)
}

func TestMemoryHistoryAndRollback(t *testing.T) {
	ctx := context.Background()
	storeRoot := filepath.Join(t.TempDir(), "store")
	project := t.TempDir()
	wid := wantData[protocol.WorkspaceRegisterData](t,
		call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": project})).Workspace.ID

	created := wantData[protocol.MemoryCreateData](t, call(t, ctx, storeRoot, "memory-create", map[string]any{
		"workspace_id": wid, "content": "Use SQLite.", "kind": "note", "reason": "initial choice",
	})).Memory
	first := created.ID

	updated := wantData[protocol.MemoryUpdateData](t, call(t, ctx, storeRoot, "memory-update", map[string]any{
		"workspace_id": wid, "memory_id": first, "expected_version": 1,
		"content": "Use SQLite in WAL mode.", "reason": "concurrency",
	})).Memory
	updatedAgain := wantData[protocol.MemoryUpdateData](t, call(t, ctx, storeRoot, "memory-update", map[string]any{
		"workspace_id": wid, "memory_id": first, "expected_version": 2,
		"content": "Use SQLite in WAL mode, busy_timeout 1000.",
	})).Memory
	if updatedAgain.Version != 3 {
		t.Fatalf("version = %d, want 3", updatedAgain.Version)
	}
	_ = updated

	// Version history: create, update, update; current version has no archived_at.
	history := wantData[protocol.MemoryHistoryData](t, call(t, ctx, storeRoot, "memory-history", map[string]any{
		"workspace_id": wid, "memory_id": first,
	}))
	if len(history.Versions) != 3 {
		t.Fatalf("versions = %#v", history.Versions)
	}
	if history.Versions[0].Action != "create" || history.Versions[0].Version != 1 {
		t.Fatalf("versions[0] = %#v", history.Versions[0])
	}
	if history.Versions[1].Action != "update" || history.Versions[2].Action != "update" {
		t.Fatalf("versions = %#v", history.Versions)
	}
	if history.Versions[2].ArchivedAt != nil {
		t.Fatalf("current version archived_at = %#v", history.Versions[2].ArchivedAt)
	}
	if history.Versions[0].ArchivedAt == nil {
		t.Fatalf("v1 should be archived")
	}

	// Read an exact historical version.
	historical := wantData[protocol.MemoryGetData](t, call(t, ctx, storeRoot, "memory-get", map[string]any{
		"workspace_id": wid, "memory_id": first, "version": 1,
	}))
	if historical.Memory.Content != "Use SQLite." || historical.Memory.Version != 1 {
		t.Fatalf("historical = %#v", historical.Memory)
	}

	// A version that never existed.
	wantError(t, call(t, ctx, storeRoot, "memory-get", map[string]any{
		"workspace_id": wid, "memory_id": first, "version": 99,
	}), protocol.CodeMemoryNotFound)

	// Rollback: an ordinary update copying the v1 content, append-only.
	rolled := wantData[protocol.MemoryUpdateData](t, call(t, ctx, storeRoot, "memory-update", map[string]any{
		"workspace_id": wid, "memory_id": first, "expected_version": 3,
		"content": "Use SQLite.", "reason": "rollback to v1",
	})).Memory
	if rolled.Version != 4 || rolled.Content != "Use SQLite." {
		t.Fatalf("rolled = %#v", rolled)
	}
	afterRollback := wantData[protocol.MemoryHistoryData](t, call(t, ctx, storeRoot, "memory-history", map[string]any{
		"workspace_id": wid, "memory_id": first,
	}))
	if len(afterRollback.Versions) != 4 {
		t.Fatalf("versions after rollback = %#v", afterRollback.Versions)
	}
}

func TestMemoryDiff(t *testing.T) {
	ctx := context.Background()
	storeRoot := filepath.Join(t.TempDir(), "store")
	project := t.TempDir()
	wid := wantData[protocol.WorkspaceRegisterData](t,
		call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": project})).Workspace.ID
	created := wantData[protocol.MemoryCreateData](t, call(t, ctx, storeRoot, "memory-create", map[string]any{
		"workspace_id": wid, "content": "Use SQLite.", "kind": "note",
	})).Memory
	wantData[protocol.MemoryUpdateData](t, call(t, ctx, storeRoot, "memory-update", map[string]any{
		"workspace_id": wid, "memory_id": created.ID, "expected_version": 1,
		"content": "Use SQLite in WAL mode.", "kind": "fact", "branches": []string{"main"},
	}))

	diff := wantData[protocol.MemoryDiffData](t, call(t, ctx, storeRoot, "memory-diff", map[string]any{
		"workspace_id": wid, "memory_id": created.ID, "from_version": 1, "to_version": 2,
	}))
	fields := make(map[string]bool)
	for _, change := range diff.Changes {
		fields[change.Field] = true
	}
	if !fields["content"] || !fields["kind"] || !fields["branches"] {
		t.Fatalf("changes = %#v", diff.Changes)
	}
	if len(diff.Changes) != 3 {
		t.Fatalf("changes = %#v", diff.Changes)
	}

	wantError(t, call(t, ctx, storeRoot, "memory-diff", map[string]any{
		"workspace_id": wid, "memory_id": created.ID, "from_version": 1, "to_version": 1,
	}), protocol.CodeInvalidRequest)
	wantError(t, call(t, ctx, storeRoot, "memory-diff", map[string]any{
		"workspace_id": wid, "memory_id": created.ID, "from_version": 1, "to_version": 99,
	}), protocol.CodeMemoryNotFound)
}

func TestDeleteErasesHistory(t *testing.T) {
	ctx := context.Background()
	storeRoot := filepath.Join(t.TempDir(), "store")
	project := t.TempDir()
	wid := wantData[protocol.WorkspaceRegisterData](t,
		call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": project})).Workspace.ID
	created := wantData[protocol.MemoryCreateData](t, call(t, ctx, storeRoot, "memory-create", map[string]any{
		"workspace_id": wid, "content": "Use SQLite.", "kind": "note",
	})).Memory
	wantData[protocol.MemoryUpdateData](t, call(t, ctx, storeRoot, "memory-update", map[string]any{
		"workspace_id": wid, "memory_id": created.ID, "expected_version": 1, "content": "v2",
	}))
	wantData[protocol.MemoryDeleteData](t, call(t, ctx, storeRoot, "memory-delete", map[string]any{
		"workspace_id": wid, "memory_id": created.ID, "expected_version": 2,
	}))
	// After a total erasure, history and historical versions are gone.
	wantError(t, call(t, ctx, storeRoot, "memory-history", map[string]any{
		"workspace_id": wid, "memory_id": created.ID,
	}), protocol.CodeMemoryNotFound)
	wantError(t, call(t, ctx, storeRoot, "memory-get", map[string]any{
		"workspace_id": wid, "memory_id": created.ID, "version": 1,
	}), protocol.CodeMemoryNotFound)
}

func TestCursorTamperRejected(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	cursor := encodeCursor(key, 1000, "mem_abc")
	decoded, err := decodeCursor(key, cursor)
	if err != nil || decoded.UpdatedAt != 1000 || decoded.MemoryID != "mem_abc" {
		t.Fatalf("round trip = %#v, %v", decoded, err)
	}

	// A cursor signed by a different key is rejected.
	otherKey := make([]byte, 32)
	otherKey[0] = 0xff
	if _, err := decodeCursor(otherKey, cursor); err == nil {
		t.Fatalf("wrong-key cursor was accepted")
	}

	// A tampered payload (still valid base64 + JSON, but carrying the old
	// signature) is rejected rather than silently changing the page start.
	parts := strings.SplitN(cursor, ".", 2)
	raw, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var forged listCursor
	if err := json.Unmarshal(raw, &forged); err != nil {
		t.Fatal(err)
	}
	forged.MemoryID = "mem_forged"
	payload, _ := json.Marshal(forged)
	forgedCursor := base64.RawURLEncoding.EncodeToString(payload) + "." + parts[1]
	if _, err := decodeCursor(key, forgedCursor); err == nil {
		t.Fatalf("tampered cursor was accepted")
	}
}

func TestUnexpectedMemoryFileFailsClosed(t *testing.T) {
	ctx := context.Background()
	storeRoot := filepath.Join(t.TempDir(), "store")
	project := t.TempDir()
	wantData[protocol.WorkspaceRegisterData](t,
		call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": project}))
	if err := os.WriteFile(filepath.Join(storeRoot, "memory", "residue"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	response := call(t, ctx, storeRoot, "workspace-resolve", map[string]any{"path": project})
	wantError(t, response, protocol.CodeWorkspaceBroken)
	if strings.Contains(response.Error.Message, "residue") {
		t.Fatalf("error exposed internal directory entry: %q", response.Error.Message)
	}
}

func TestMemoryFileNames(t *testing.T) {
	valid := []string{
		"workspace-1-memory.db",
		"workspace-1-memory.db-wal",
		"workspace-1-memory.db-shm",
		"workspace-1-memory.db-journal",
	}
	for _, name := range valid {
		if !validMemoryFileName(name) {
			t.Errorf("validMemoryFileName(%q) = false", name)
		}
	}
	for _, name := range []string{"workspace-0-memory.db-journal", "workspace-x-memory.db-journal", "workspace-1-memory.db.tmp"} {
		if validMemoryFileName(name) {
			t.Errorf("validMemoryFileName(%q) = true", name)
		}
	}
}

func TestBranchFilter(t *testing.T) {
	ctx := context.Background()
	storeRoot := filepath.Join(t.TempDir(), "store")
	project := t.TempDir()
	wid := wantData[protocol.WorkspaceRegisterData](t,
		call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": project})).Workspace.ID

	wantData[protocol.MemoryCreateData](t, call(t, ctx, storeRoot, "memory-create", map[string]any{
		"workspace_id": wid, "content": "branchtest main only", "kind": "note", "branches": []string{"main"},
	}))
	wantData[protocol.MemoryCreateData](t, call(t, ctx, storeRoot, "memory-create", map[string]any{
		"workspace_id": wid, "content": "branchtest dev only", "kind": "note", "branches": []string{"dev"},
	}))
	allBranches := wantData[protocol.MemoryCreateData](t, call(t, ctx, storeRoot, "memory-create", map[string]any{
		"workspace_id": wid, "content": "branchtest all branches", "kind": "note",
	})).Memory
	if allBranches.Branches != nil {
		t.Fatalf("unrestricted branches = %#v, want nil", allBranches.Branches)
	}

	search := func(branch *string) int {
		request := map[string]any{"workspace_id": wid, "query": "branchtest"}
		if branch != nil {
			request["branch"] = *branch
		}
		return len(wantData[protocol.MemorySearchData](t, call(t, ctx, storeRoot, "memory-search", request)).Memories)
	}

	if got := search(nil); got != 3 {
		t.Fatalf("no branch filter returned %d, want 3", got)
	}
	main := "main"
	if got := search(&main); got != 2 {
		t.Fatalf("branch=main returned %d, want 2", got)
	}
	dev := "dev"
	if got := search(&dev); got != 2 {
		t.Fatalf("branch=dev returned %d, want 2", got)
	}
	feature := "feature"
	if got := search(&feature); got != 1 {
		t.Fatalf("branch=feature returned %d, want 1 (only all-branches)", got)
	}
}

func TestWorkspaceDeleteCleansBrokenWorkspace(t *testing.T) {
	ctx := context.Background()
	storeRoot := filepath.Join(t.TempDir(), "store")
	project := t.TempDir()
	wid := wantData[protocol.WorkspaceRegisterData](t,
		call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": project})).Workspace.ID
	memoryDB := filepath.Join(storeRoot, "memory", "workspace-1-memory.db")
	if err := os.Remove(memoryDB); err != nil {
		t.Fatal(err)
	}
	// Memory commands see the broken workspace; delete is the cleanup path.
	wantError(t, call(t, ctx, storeRoot, "memory-get", map[string]any{
		"workspace_id": wid, "memory_id": "mem_x",
	}), protocol.CodeWorkspaceBroken)
	wantData[protocol.WorkspaceDeleteData](t, call(t, ctx, storeRoot, "workspace-delete", map[string]any{"workspace_id": wid}))
}

func TestWorkspaceLockReturnsBusy(t *testing.T) {
	ctx := context.Background()
	storeRoot := filepath.Join(t.TempDir(), "store")
	project := t.TempDir()
	wid := wantData[protocol.WorkspaceRegisterData](t,
		call(t, ctx, storeRoot, "workspace-register", map[string]any{"path": project})).Workspace.ID

	store, err := meta.Open(ctx, storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Lock(ctx, wid, "write", "test-session"); err != nil {
		t.Fatal(err)
	}
	defer store.Unlock(ctx, wid, "test-session")

	wantError(t, call(t, ctx, storeRoot, "memory-create", map[string]any{
		"workspace_id": wid, "content": "blocked", "kind": "note",
	}), protocol.CodeWorkspaceBusy)
}

func call(t *testing.T, ctx context.Context, storeRoot, name string, request any) protocol.Response {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return Run(ctx, storeRoot, "test-session", name, string(raw))
}

func wantData[T any](t *testing.T, response protocol.Response) T {
	t.Helper()
	if !response.OK {
		t.Fatalf("unexpected error response: %#v", response.Error)
	}
	data, ok := response.Data.(T)
	if !ok {
		t.Fatalf("data type = %T", response.Data)
	}
	return data
}

func wantError(t *testing.T, response protocol.Response, code string) {
	t.Helper()
	if response.OK || response.Error == nil || response.Error.Code != code {
		t.Fatalf("response = %#v, want error %q", response, code)
	}
}
