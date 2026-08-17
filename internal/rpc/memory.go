package rpc

import (
	"context"

	"github.com/decglu/dsh-memory-note/internal/memory"
)

type MemorySearchRequest struct {
	WorkspaceID int64         `json:"workspace_id"`
	Query       string        `json:"query,omitempty"`
	Filter      memory.Filter `json:"filter,omitempty"`
	Limit       int           `json:"limit,omitempty"`
}

type MemorySearchResponse struct {
	Memories []memory.SearchHit `json:"memories"`
}

func (s Server) MemorySearch(ctx context.Context, raw string) (MemorySearchResponse, error) {
	var request MemorySearchRequest
	if err := decode(raw, &request); err != nil {
		return MemorySearchResponse{}, err
	}
	hits, err := withMemory(ctx, s, request.WorkspaceID, "read", func(store *memory.Store) ([]memory.SearchHit, error) {
		return store.Search(ctx, memory.SearchInput{
			Query: request.Query, Filter: request.Filter, Limit: request.Limit,
		})
	})
	return MemorySearchResponse{Memories: hits}, err
}

type MemoryGetRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	MemoryID    string `json:"memory_id"`
}

type MemoryGetResponse struct {
	Memory memory.Memory `json:"memory"`
}

func (s Server) MemoryGet(ctx context.Context, raw string) (MemoryGetResponse, error) {
	var request MemoryGetRequest
	if err := decode(raw, &request); err != nil {
		return MemoryGetResponse{}, err
	}
	item, err := withMemory(ctx, s, request.WorkspaceID, "read", func(store *memory.Store) (memory.Memory, error) {
		return store.Get(ctx, request.MemoryID)
	})
	return MemoryGetResponse{Memory: item}, err
}

type MemoryCreateRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
	memory.CreateInput
}

type MemoryCreateResponse struct {
	Memory memory.Memory `json:"memory"`
}

func (s Server) MemoryCreate(ctx context.Context, raw string) (MemoryCreateResponse, error) {
	var request MemoryCreateRequest
	if err := decode(raw, &request); err != nil {
		return MemoryCreateResponse{}, err
	}
	item, err := withMemory(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (memory.Memory, error) {
		return store.Create(ctx, request.CreateInput)
	})
	return MemoryCreateResponse{Memory: item}, err
}

type MemoryUpdateRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
	memory.UpdateInput
}

type MemoryUpdateResponse struct {
	Memory memory.Memory `json:"memory"`
}

func (s Server) MemoryUpdate(ctx context.Context, raw string) (MemoryUpdateResponse, error) {
	var request MemoryUpdateRequest
	if err := decode(raw, &request); err != nil {
		return MemoryUpdateResponse{}, err
	}
	item, err := withMemory(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (memory.Memory, error) {
		return store.Update(ctx, request.UpdateInput)
	})
	return MemoryUpdateResponse{Memory: item}, err
}

type MemorySupersedeRequest struct {
	WorkspaceID     int64              `json:"workspace_id"`
	MemoryID        string             `json:"memory_id"`
	ExpectedVersion int                `json:"expected_version"`
	New             memory.CreateInput `json:"new"`
}

type MemorySupersedeResponse struct {
	Old memory.Memory `json:"old"`
	New memory.Memory `json:"new"`
}

func (s Server) MemorySupersede(ctx context.Context, raw string) (MemorySupersedeResponse, error) {
	var request MemorySupersedeRequest
	if err := decode(raw, &request); err != nil {
		return MemorySupersedeResponse{}, err
	}
	result, err := withMemory(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (memory.SupersedeResult, error) {
		return store.Supersede(ctx, request.MemoryID, request.ExpectedVersion, request.New)
	})
	return MemorySupersedeResponse{Old: result.Old, New: result.New}, err
}

type MemoryInvalidateRequest struct {
	WorkspaceID     int64  `json:"workspace_id"`
	MemoryID        string `json:"memory_id"`
	ExpectedVersion int    `json:"expected_version"`
}

type MemoryInvalidateResponse struct {
	Memory memory.Memory `json:"memory"`
}

func (s Server) MemoryInvalidate(ctx context.Context, raw string) (MemoryInvalidateResponse, error) {
	var request MemoryInvalidateRequest
	if err := decode(raw, &request); err != nil {
		return MemoryInvalidateResponse{}, err
	}
	item, err := withMemory(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (memory.Memory, error) {
		return store.Invalidate(ctx, request.MemoryID, request.ExpectedVersion)
	})
	return MemoryInvalidateResponse{Memory: item}, err
}

type MemoryDeleteRequest struct {
	WorkspaceID     int64  `json:"workspace_id"`
	MemoryID        string `json:"memory_id"`
	ExpectedVersion int    `json:"expected_version"`
}

type MemoryDeleteResponse struct {
	Deleted bool `json:"deleted"`
}

func (s Server) MemoryDelete(ctx context.Context, raw string) (MemoryDeleteResponse, error) {
	var request MemoryDeleteRequest
	if err := decode(raw, &request); err != nil {
		return MemoryDeleteResponse{}, err
	}
	_, err := withMemory(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (struct{}, error) {
		return struct{}{}, store.Delete(ctx, request.MemoryID, request.ExpectedVersion)
	})
	return MemoryDeleteResponse{Deleted: err == nil}, err
}
