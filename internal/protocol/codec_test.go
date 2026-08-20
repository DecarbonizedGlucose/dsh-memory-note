package protocol

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDecode(t *testing.T) {
	var request MemorySearchRequest
	err := Decode(`{"workspace_id":1,"filter":{"updated_after":"2026-08-21T00:00:00Z"}}`, &request)
	if err != nil || request.WorkspaceID != 1 || request.Filter.UpdatedAfter == nil {
		t.Fatalf("decode request: %#v %v", request, err)
	}

	for _, raw := range []string{
		`{"workspace_id":1,"unknown":true}`,
		`{"workspace_id":1,"filter":{"unknown":true}}`,
		`{"workspace_id":1} {"workspace_id":2}`,
	} {
		if err := Decode(raw, &request); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("expected invalid request for %q, got %v", raw, err)
		}
	}
}

func TestMemoryResponse(t *testing.T) {
	now := time.Date(2026, 8, 21, 1, 2, 3, 0, time.UTC)
	response := MemoryGetResponse{Memory: Memory{
		ID:          "mem_1",
		WorkspaceID: 7,
		Content:     "Use SQLite.",
		State:       MemoryActive,
		Version:     2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"memory":{"memory_id":"mem_1","workspace_id":7,"content":"Use SQLite.","state":"active","version":2,"created_at":"2026-08-21T01:02:03Z","updated_at":"2026-08-21T01:02:03Z"}}`
	if string(raw) != want {
		t.Fatalf("response: %s", raw)
	}
}

func TestFailureCode(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{ErrInvalidRequest, "invalid_request"},
		{ErrHomeBroken, "home_broken"},
		{ErrWorkspaceNotFound, "workspace_not_found"},
		{ErrWorkspacePathUsed, "workspace_path_used"},
		{ErrWorkspaceBusy, "workspace_busy"},
		{ErrMemoryNotFound, "memory_not_found"},
		{ErrVersionConflict, "version_conflict"},
		{ErrInvalidMemoryState, "invalid_memory_state"},
		{errors.New("unknown"), "error"},
	}
	for _, test := range tests {
		reply := Failure(test.err)
		if reply.OK || reply.Error == nil || reply.Error.Code != test.code {
			t.Fatalf("failure for %v: %#v", test.err, reply)
		}
	}
}
