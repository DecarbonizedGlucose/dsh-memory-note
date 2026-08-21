package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentProcessesUseOneStore(t *testing.T) {
	binary := buildCore(t)
	storeRoot := filepath.Join(t.TempDir(), "store")
	project := t.TempDir()
	wantProcessOK(t, callCore(t, binary, storeRoot, "workspace-register", map[string]any{"path": project}))

	var group sync.WaitGroup
	responses := make(chan processResponse, 2)
	for _, content := range []string{"first process memory", "second process memory"} {
		group.Add(1)
		go func() {
			defer group.Done()
			responses <- callCore(t, binary, storeRoot, "memory-create", map[string]any{
				"workspace_id": 1, "content": content,
			})
		}()
	}
	group.Wait()
	close(responses)
	for response := range responses {
		wantProcessOK(t, response)
	}

	search := callCore(t, binary, storeRoot, "memory-search", map[string]any{
		"workspace_id": 1, "query": "process memory", "limit": 8,
	})
	wantProcessOK(t, search)
	memories, ok := search.Data["memories"].([]any)
	if !ok || len(memories) != 2 {
		t.Fatalf("search response = %#v", search)
	}
}

func TestWrongArgumentCountUsesExitTwo(t *testing.T) {
	binary := buildCore(t)
	process := exec.Command(binary)
	var stdout bytes.Buffer
	process.Stdout = &stdout
	err := process.Run()
	exitError, ok := err.(*exec.ExitError)
	if !ok || exitError.ExitCode() != 2 || stdout.Len() != 0 {
		t.Fatalf("error = %v, stdout = %q", err, stdout.String())
	}
}

type processResponse struct {
	OK    bool           `json:"ok"`
	Data  map[string]any `json:"data"`
	Error map[string]any `json:"error"`
}

func buildCore(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "dsh-memory-note")
	process := exec.Command("go", "build", "-o", binary, "./cmd/dsh-memory-note")
	process.Dir = repoRoot
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("build core: %v\n%s", err, output)
	}
	return binary
}

func callCore(t *testing.T, binary, storeRoot, name string, request any) processResponse {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	process := exec.Command(binary, name, string(raw))
	process.Env = append(os.Environ(), "DSH_MEMORY_NOTE_HOME="+storeRoot)
	output, err := process.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, output)
	}
	var response processResponse
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("decode %s response: %v\n%s", name, err, output)
	}
	return response
}

func wantProcessOK(t *testing.T, response processResponse) {
	t.Helper()
	if !response.OK {
		t.Fatalf("response = %#v", response)
	}
}
