package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type validFixture struct {
	Name    string          `json:"name"`
	Command string          `json:"command"`
	Request json.RawMessage `json:"request"`
}

type invalidFixture struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Raw     string `json:"raw"`
}

func TestSharedRequestFixtures(t *testing.T) {
	var valid []validFixture
	readFixtures(t, "valid", &valid)
	for _, fixture := range valid {
		t.Run("valid/"+fixture.Name, func(t *testing.T) {
			if err := decodeFixture(fixture.Command, string(fixture.Request)); err != nil {
				t.Fatal(err)
			}
		})
	}

	var invalid []invalidFixture
	readFixtures(t, "invalid", &invalid)
	for _, fixture := range invalid {
		t.Run("invalid/"+fixture.Name, func(t *testing.T) {
			if err := decodeFixture(fixture.Command, fixture.Raw); err == nil {
				t.Fatal("expected decode error")
			}
		})
	}
}

func decodeFixture(command, raw string) error {
	switch command {
	case "workspace-resolve":
		return Decode(raw, &WorkspaceResolveRequest{})
	case "workspace-register":
		return Decode(raw, &WorkspaceRegisterRequest{})
	case "workspace-rebind":
		return Decode(raw, &WorkspaceRebindRequest{})
	case "workspace-clear":
		return Decode(raw, &WorkspaceClearRequest{})
	case "workspace-delete":
		return Decode(raw, &WorkspaceDeleteRequest{})
	case "memory-search":
		return Decode(raw, &MemorySearchRequest{})
	case "memory-list":
		return Decode(raw, &MemoryListRequest{})
	case "memory-get":
		return Decode(raw, &MemoryGetRequest{})
	case "memory-create":
		return Decode(raw, &MemoryCreateRequest{})
	case "memory-update":
		return Decode(raw, &MemoryUpdateRequest{})
	case "memory-supersede":
		return Decode(raw, &MemorySupersedeRequest{})
	case "memory-invalidate":
		return Decode(raw, &MemoryInvalidateRequest{})
	case "memory-delete":
		return Decode(raw, &MemoryDeleteRequest{})
	default:
		return Invalid("unknown fixture command")
	}
}

func readFixtures(t *testing.T, kind string, target any) {
	t.Helper()
	path := filepath.Join("..", "..", "protocol", "v1", "fixtures", kind, "requests.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}
