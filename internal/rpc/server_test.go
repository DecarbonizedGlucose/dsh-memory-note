package rpc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/meta"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

func testServer(t *testing.T) (Server, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if _, err := meta.PrepareHome(context.Background(), home, "test-session"); err != nil {
		t.Fatal(err)
	}
	return NewServer(home, "test-session"), home
}

func TestWorkspaceAndMemoryRPC(t *testing.T) {
	ctx := context.Background()
	server, home := testServer(t)
	workspacePath := filepath.Join(t.TempDir(), "project")

	registered, err := server.WorkspaceRegister(ctx, fmt.Sprintf(`{"path":%q}`, workspacePath))
	if err != nil || !registered.Created || registered.WorkspaceID < 0 {
		t.Fatalf("register: %#v %v", registered, err)
	}
	if _, err := os.Stat(filepath.Join(home, "meta.db")); err != nil {
		t.Fatal("meta.db was not created:", err)
	}
	memoryPath := filepath.Join(home, "memory", "workspace-"+strconv.FormatInt(registered.WorkspaceID, 10)+"-memory.db")
	if _, err := os.Stat(memoryPath); err != nil {
		t.Fatal("workspace memory db was not created:", err)
	}

	createJSON := fmt.Sprintf(`{"workspace_id":%d,"content":"Use SQLite.","type":"decision","scope":"storage"}`,
		registered.WorkspaceID)
	created, err := server.MemoryCreate(ctx, createJSON)
	if err != nil || created.Memory.ID == "" {
		t.Fatalf("create: %#v %v", created, err)
	}
	searchJSON := fmt.Sprintf(`{"workspace_id":%d,"query":"SQLite"}`, registered.WorkspaceID)
	found, err := server.MemorySearch(ctx, searchJSON)
	if err != nil || len(found.Memories) != 1 || found.Memories[0].ID != created.Memory.ID {
		t.Fatalf("search: %#v %v", found, err)
	}
	getJSON := fmt.Sprintf(`{"workspace_id":%d,"memory_id":%q}`, registered.WorkspaceID, created.Memory.ID)
	got, err := server.MemoryGet(ctx, getJSON)
	if err != nil || got.Memory.Content != "Use SQLite." {
		t.Fatalf("get: %#v %v", got, err)
	}

	movedPath := filepath.Join(t.TempDir(), "moved")
	moved, err := server.WorkspaceRebind(ctx, fmt.Sprintf(
		`{"workspace_id":%d,"path":%q}`, registered.WorkspaceID, movedPath))
	if err != nil || moved.WorkspaceID != registered.WorkspaceID || moved.Path != movedPath {
		t.Fatalf("rebind: %#v %v", moved, err)
	}
	got, err = server.MemoryGet(ctx, getJSON)
	if err != nil || got.Memory.ID != created.Memory.ID {
		t.Fatalf("memory changed after rebind: %#v %v", got, err)
	}

	cleared, err := server.WorkspaceClear(ctx, fmt.Sprintf(`{"workspace_id":%d}`, registered.WorkspaceID))
	if err != nil || cleared.Deleted != 1 {
		t.Fatalf("clear: %#v %v", cleared, err)
	}
	deleted, err := server.WorkspaceDelete(ctx, fmt.Sprintf(`{"workspace_id":%d}`, registered.WorkspaceID))
	if err != nil || !deleted.Deleted {
		t.Fatalf("delete workspace: %#v %v", deleted, err)
	}
	if _, err := os.Stat(memoryPath); !os.IsNotExist(err) {
		t.Fatalf("memory db still exists: %v", err)
	}
}

func TestStrictSubcommandProtocol(t *testing.T) {
	server, _ := testServer(t)
	_, err := server.WorkspaceRegister(context.Background(), `{"path":"/tmp/example","extra":true}`)
	if err == nil {
		t.Fatal("unknown field was accepted")
	}
	_, err = server.MemoryGet(context.Background(), `{"workspace_id":-1,"memory_id":"mem_x"}`)
	if err == nil {
		t.Fatal("negative workspace id was accepted")
	}
	_, err = server.MemorySearch(context.Background(), `{"workspace_id":1,"embedding":[1,0]}`)
	if err == nil {
		t.Fatal("removed embedding field was accepted")
	}
	_, err = server.MemorySearch(context.Background(), `{"workspace_id":1,"filter":{"states":["active"]}}`)
	if err == nil {
		t.Fatal("removed state filter was accepted")
	}
	_, err = server.WorkspaceDelete(context.Background(), `{"workspace_id":1,"delete_memory_db":false}`)
	if err == nil {
		t.Fatal("removed delete_memory_db field was accepted")
	}
}

func TestServe(t *testing.T) {
	server, _ := testServer(t)
	ctx := context.Background()

	result, err := server.Serve(ctx, []string{"workspace-register", `{"path":"."}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.(protocol.WorkspaceRegisterResponse); !ok {
		t.Fatalf("unexpected response type %T", result)
	}

	if _, err := server.Serve(ctx, []string{"unknown", `{}`}); err == nil {
		t.Fatal("expected unknown subcommand error")
	}
}
