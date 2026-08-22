// Package main holds process-level integration tests for the core binary:
// the one-shot executable is built once into /tmp (removed after the run) and
// every test drives real processes with DSH_MEMORY_NOTE_HOME pointing into a
// t.TempDir() home (also under /tmp, cleaned up by the framework).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var coreBinary string

func TestMain(m *testing.M) {
	repoRoot, err := filepath.Abs(filepath.Join(".."))
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve repo root:", err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "dsh-memory-note-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create binary dir:", err)
		os.Exit(1)
	}
	coreBinary = filepath.Join(dir, "dsh-memory-note")
	build := exec.Command("go", "build", "-o", coreBinary, "./cmd/dsh-memory-note")
	build.Dir = repoRoot
	build.Env = os.Environ()
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build core: %v\n%s", err, output)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type processResponse struct {
	OK    bool           `json:"ok"`
	Data  map[string]any `json:"data"`
	Error map[string]any `json:"error"`
}

func buildCore(t *testing.T) string {
	t.Helper()
	if coreBinary != "" {
		return coreBinary
	}
	repoRoot, err := filepath.Abs(filepath.Join(".."))
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

// runCore executes one core process with DSH_MEMORY_NOTE_HOME pointed at a
// home directory under /tmp. It is goroutine-safe: callers assert the result
// themselves. Exit code 1 with a valid protocol envelope is a normal error
// response; only unparseable output (exit 2 usage, crashes, garbage) counts
// as a failure here.
func runCore(binary, home, name string, request any) (processResponse, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return processResponse{}, err
	}
	process := exec.Command(binary, name, string(raw))
	process.Env = append(os.Environ(), "DSH_MEMORY_NOTE_HOME="+home)
	output, err := process.CombinedOutput()
	var response processResponse
	if decodeErr := json.Unmarshal(output, &response); decodeErr != nil {
		return processResponse{}, fmt.Errorf("decode %s response (exit err %v): %w", name, err, decodeErr)
	}
	return response, nil
}

func mustRun(t *testing.T, binary, home, name string, request any) processResponse {
	t.Helper()
	response, err := runCore(binary, home, name, request)
	if err != nil {
		t.Fatalf("%s process failed: %v", name, err)
	}
	if !response.OK {
		t.Fatalf("%s response = %#v", name, response)
	}
	return response
}

func wantCoreError(t *testing.T, binary, home, name string, request any, code string) {
	t.Helper()
	response, err := runCore(binary, home, name, request)
	if err != nil {
		t.Fatalf("%s process failed: %v", name, err)
	}
	if response.OK || response.Error["code"] != code {
		t.Fatalf("%s response = %#v, want error %q", name, response, code)
	}
}

func tmpHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if !strings.HasPrefix(home, os.TempDir()) {
		t.Fatalf("home %q must live under %q", home, os.TempDir())
	}
	return home
}

func TestConcurrentProcessesUseOneStore(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	project := t.TempDir()
	mustRun(t, binary, home, "workspace-register", map[string]any{"path": project})

	responses := make(chan processResponse, 2)
	var group sync.WaitGroup
	for _, content := range []string{"first process memory", "second process memory"} {
		group.Add(1)
		go func(content string) {
			defer group.Done()
			response, err := runCore(binary, home, "memory-create", map[string]any{
				"workspace_id": 1, "content": content,
			})
			if err == nil {
				responses <- response
			}
		}(content)
	}
	group.Wait()
	close(responses)
	for response := range responses {
		if !response.OK {
			t.Fatalf("create response = %#v", response)
		}
	}

	search := mustRun(t, binary, home, "memory-search", map[string]any{
		"workspace_id": 1, "query": "process memory", "limit": 8,
	})
	if memories, ok := search.Data["memories"].([]any); !ok || len(memories) != 2 {
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

// TestCoreLifecycleWithTmpHome drives every business subcommand through the
// real binary with HOME under /tmp: resolve (auto-init), register
// (idempotent), create/get/search/list, version-conflict, update, supersede,
// delete (relationship cleanup), invalidate, clear, workspace delete, and the
// home_broken gate for a damaged HOME.
func TestCoreLifecycleWithTmpHome(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	workspace := t.TempDir()

	resolved := mustRun(t, binary, home, "workspace-resolve", map[string]any{"path": workspace})
	if resolved.Data["workspace"] != nil {
		t.Fatalf("resolve on fresh store = %#v", resolved)
	}

	registered := mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})
	if registered.Data["created"] != true {
		t.Fatalf("register data = %#v", registered)
	}
	again := mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})
	if again.Data["created"] != false {
		t.Fatalf("idempotent register = %#v", again)
	}

	created := mustRun(t, binary, home, "memory-create", map[string]any{
		"workspace_id": 1, "content": "Use SQLite.", "type": "decision", "scope": "storage",
	})
	memory := created.Data["memory"].(map[string]any)
	id := memory["memory_id"].(string)
	if memory["version"] != float64(1) || memory["state"] != "active" {
		t.Fatalf("created memory = %#v", memory)
	}

	wantCoreError(t, binary, home, "memory-update", map[string]any{
		"workspace_id": 1, "memory_id": id, "expected_version": 9, "content": "stale",
	}, "version_conflict")

	updated := mustRun(t, binary, home, "memory-update", map[string]any{
		"workspace_id": 1, "memory_id": id, "expected_version": 1, "content": "Use WAL mode.",
	})
	if updated.Data["memory"].(map[string]any)["version"] != float64(2) {
		t.Fatalf("updated memory = %#v", updated)
	}

	superseded := mustRun(t, binary, home, "memory-supersede", map[string]any{
		"workspace_id": 1, "memory_id": id, "expected_version": 2,
		"new": map[string]any{"content": "Use PostgreSQL."},
	})
	old := superseded.Data["old"].(map[string]any)
	newItem := superseded.Data["new"].(map[string]any)
	newID := newItem["memory_id"].(string)
	if old["state"] != "superseded" || newItem["supersedes"] != id {
		t.Fatalf("supersede data = %#v", superseded)
	}

	mustRun(t, binary, home, "memory-delete", map[string]any{
		"workspace_id": 1, "memory_id": id, "expected_version": 3,
	})
	gotNew := mustRun(t, binary, home, "memory-get", map[string]any{
		"workspace_id": 1, "memory_id": newID,
	})
	newAfterDelete := gotNew.Data["memory"].(map[string]any)
	if newAfterDelete["supersedes"] != nil || newAfterDelete["version"] != float64(2) {
		t.Fatalf("relationship cleanup = %#v", newAfterDelete)
	}

	mustRun(t, binary, home, "memory-invalidate", map[string]any{
		"workspace_id": 1, "memory_id": newID, "expected_version": 2,
	})
	search := mustRun(t, binary, home, "memory-search", map[string]any{"workspace_id": 1, "query": "sqlite"})
	if memories := search.Data["memories"].([]any); len(memories) != 0 {
		t.Fatalf("search returned inactive memory: %#v", search)
	}
	listed := mustRun(t, binary, home, "memory-list", map[string]any{"workspace_id": 1})
	if memories := listed.Data["memories"].([]any); len(memories) != 1 || memories[0].(map[string]any)["state"] != "invalid" {
		t.Fatalf("list data = %#v", listed)
	}

	cleared := mustRun(t, binary, home, "workspace-clear", map[string]any{"workspace_id": 1})
	if cleared.Data["deleted_count"] != float64(1) {
		t.Fatalf("clear data = %#v", cleared)
	}

	mustRun(t, binary, home, "workspace-delete", map[string]any{"workspace_id": 1})
	wantCoreError(t, binary, home, "memory-get", map[string]any{
		"workspace_id": 1, "memory_id": newID,
	}, "workspace_not_found")

	// Break HOME: meta.db replaced by a directory must fail closed, never be
	// rebuilt silently.
	metaDB := filepath.Join(home, "meta.db")
	if err := os.Remove(metaDB); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(metaDB, 0o700); err != nil {
		t.Fatal(err)
	}
	wantCoreError(t, binary, home, "workspace-resolve", map[string]any{"path": workspace}, "home_broken")
}

// TestWorkspaceDeleteRacesReads runs workspace-delete against a live reader
// loop. Every read must either succeed or fail with a workspace-level error —
// never hang, never crash, never memory_not_found from a half-deleted state.
func TestWorkspaceDeleteRacesReads(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	workspace := t.TempDir()

	mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})
	created := mustRun(t, binary, home, "memory-create", map[string]any{"workspace_id": 1, "content": "race target"})
	id := created.Data["memory"].(map[string]any)["memory_id"].(string)

	const reads = 40
	results := make(chan processResponse, reads)
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		for i := 0; i < reads; i++ {
			response, err := runCore(binary, home, "memory-get", map[string]any{"workspace_id": 1, "memory_id": id})
			if err == nil {
				results <- response
			}
		}
	}()

	time.Sleep(20 * time.Millisecond)
	deleted := mustRun(t, binary, home, "workspace-delete", map[string]any{"workspace_id": 1})
	if deleted.Data["deleted"] != true {
		t.Fatalf("delete data = %#v", deleted)
	}
	group.Wait()
	close(results)

	for response := range results {
		if response.OK {
			continue
		}
		code := response.Error["code"].(string)
		if code != "workspace_not_found" && code != "workspace_busy" && code != "workspace_broken" {
			t.Fatalf("read during delete returned %#v", response)
		}
	}
	wantCoreError(t, binary, home, "memory-get", map[string]any{
		"workspace_id": 1, "memory_id": id,
	}, "workspace_not_found")
}

// TestManyAgentsOneWorkspace hammers one workspace with concurrent writers
// and readers across real processes, then verifies version checks stay
// deterministic under contention: exactly one of N updates wins.
func TestManyAgentsOneWorkspace(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)
	workspace := t.TempDir()
	mustRun(t, binary, home, "workspace-register", map[string]any{"path": workspace})

	const agents = 8
	const perAgent = 5
	var group sync.WaitGroup
	writeErrors := make(chan string, agents*perAgent)
	for agent := 0; agent < agents; agent++ {
		group.Add(1)
		go func(agent int) {
			defer group.Done()
			for i := 0; i < perAgent; i++ {
				response, err := runCore(binary, home, "memory-create", map[string]any{
					"workspace_id": 1, "content": fmt.Sprintf("agent-%d-note-%d", agent, i),
				})
				if err != nil || !response.OK {
					writeErrors <- fmt.Sprintf("agent %d create %d: err=%v response=%#v", agent, i, err, response)
				}
			}
		}(agent)
	}
	readErrors := make(chan string, 4)
	for reader := 0; reader < 4; reader++ {
		group.Add(1)
		go func(reader int) {
			defer group.Done()
			response, err := runCore(binary, home, "memory-search", map[string]any{"workspace_id": 1, "query": "agent"})
			if err != nil || !response.OK {
				readErrors <- fmt.Sprintf("reader %d: err=%v response=%#v", reader, err, response)
			}
		}(reader)
	}
	group.Wait()
	close(writeErrors)
	close(readErrors)
	for entry := range writeErrors {
		t.Error(entry)
	}
	for entry := range readErrors {
		t.Error(entry)
	}

	listed := mustRun(t, binary, home, "memory-list", map[string]any{"workspace_id": 1, "limit": 200})
	if memories := listed.Data["memories"].([]any); len(memories) != agents*perAgent {
		t.Fatalf("list count = %d, want %d", len(memories), agents*perAgent)
	}

	// Contended update: exactly one winner, the rest version_conflict.
	contended := mustRun(t, binary, home, "memory-create", map[string]any{"workspace_id": 1, "content": "contended"})
	id := contended.Data["memory"].(map[string]any)["memory_id"].(string)
	const updaters = 8
	outcomes := make(chan processResponse, updaters)
	var updateGroup sync.WaitGroup
	for i := 0; i < updaters; i++ {
		updateGroup.Add(1)
		go func() {
			defer updateGroup.Done()
			response, err := runCore(binary, home, "memory-update", map[string]any{
				"workspace_id": 1, "memory_id": id, "expected_version": 1, "content": "winner",
			})
			if err == nil {
				outcomes <- response
			}
		}()
	}
	updateGroup.Wait()
	close(outcomes)
	winners, conflicts := 0, 0
	for response := range outcomes {
		if response.OK {
			winners++
		} else if response.Error["code"] == "version_conflict" {
			conflicts++
		} else {
			t.Fatalf("unexpected update outcome: %#v", response)
		}
	}
	if winners != 1 || conflicts != updaters-1 {
		t.Fatalf("winners=%d conflicts=%d, want 1/%d", winners, conflicts, updaters-1)
	}
}

// TestManyAgentsSeparateWorkspaces registers several workspaces concurrently
// and lets each agent work its own workspace in parallel; memories must stay
// isolated per WID and registration IDs must be distinct and non-reused.
func TestManyAgentsSeparateWorkspaces(t *testing.T) {
	binary := buildCore(t)
	home := tmpHome(t)

	const agents = 6
	paths := make([]string, agents)
	ids := make([]int64, agents)
	var registerGroup sync.WaitGroup
	registerErrors := make(chan string, agents)
	for agent := 0; agent < agents; agent++ {
		paths[agent] = t.TempDir()
		registerGroup.Add(1)
		go func(agent int) {
			defer registerGroup.Done()
			response, err := runCore(binary, home, "workspace-register", map[string]any{"path": paths[agent]})
			if err != nil || !response.OK {
				registerErrors <- fmt.Sprintf("register %d: err=%v response=%#v", agent, err, response)
				return
			}
			ids[agent] = int64(response.Data["workspace"].(map[string]any)["workspace_id"].(float64))
		}(agent)
	}
	registerGroup.Wait()
	close(registerErrors)
	for entry := range registerErrors {
		t.Error(entry)
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id < 1 || id > agents || seen[id] {
			t.Fatalf("workspace ids not distinct 1..%d: %v", agents, ids)
		}
		seen[id] = true
	}

	var workGroup sync.WaitGroup
	workErrors := make(chan string, agents)
	for agent := 0; agent < agents; agent++ {
		workGroup.Add(1)
		go func(agent int) {
			defer workGroup.Done()
			for i := 0; i < 3; i++ {
				// Single-token per-agent prefix: hyphenated or multi-word
				// content would be split into shared keywords.
				response, err := runCore(binary, home, "memory-create", map[string]any{
					"workspace_id": ids[agent], "content": fmt.Sprintf("agent%d mem %d", agent, i),
				})
				if err != nil || !response.OK {
					workErrors <- fmt.Sprintf("agent %d create %d: err=%v response=%#v", agent, i, err, response)
					return
				}
			}
		}(agent)
	}
	workGroup.Wait()
	close(workErrors)
	for entry := range workErrors {
		t.Error(entry)
	}

	for agent := 0; agent < agents; agent++ {
		listed := mustRun(t, binary, home, "memory-list", map[string]any{"workspace_id": ids[agent], "limit": 200})
		if memories := listed.Data["memories"].([]any); len(memories) != 3 {
			t.Fatalf("workspace %d list count = %d, want 3", ids[agent], len(memories))
		}
		searched := mustRun(t, binary, home, "memory-search", map[string]any{
			"workspace_id": ids[agent], "query": fmt.Sprintf("agent%d", agent),
		})
		if memories := searched.Data["memories"].([]any); len(memories) != 3 {
			t.Fatalf("workspace %d search count = %d, want 3", ids[agent], len(memories))
		}
		// Cross-workspace isolation: another workspace's memories never leak.
		other := (agent + 1) % agents
		foreign := mustRun(t, binary, home, "memory-search", map[string]any{
			"workspace_id": ids[agent], "query": fmt.Sprintf("agent%d", other),
		})
		if memories := foreign.Data["memories"].([]any); len(memories) != 0 {
			t.Fatalf("workspace %d sees workspace %d memories: %#v", ids[agent], ids[other], foreign)
		}
	}
}
