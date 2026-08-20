package rpc

import (
	"context"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/memory"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

func (s Server) MemorySearch(ctx context.Context, raw string) (protocol.MemorySearchResponse, error) {
	var request protocol.MemorySearchRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemorySearchResponse{}, err
	}
	hits, err := withMemoryStore(ctx, s, request.WorkspaceID, "read", func(store *memory.Store) ([]protocol.MemorySearchHit, error) {
		return store.Search(ctx, request)
	})
	return protocol.MemorySearchResponse{Memories: hits}, err
}

func (s Server) MemoryGet(ctx context.Context, raw string) (protocol.MemoryGetResponse, error) {
	var request protocol.MemoryGetRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryGetResponse{}, err
	}
	item, err := withMemoryStore(ctx, s, request.WorkspaceID, "read", func(store *memory.Store) (protocol.Memory, error) {
		return store.Get(ctx, request)
	})
	return protocol.MemoryGetResponse{Memory: item}, err
}

func (s Server) MemoryCreate(ctx context.Context, raw string) (protocol.MemoryCreateResponse, error) {
	var request protocol.MemoryCreateRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryCreateResponse{}, err
	}
	item, err := withMemoryStore(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (protocol.Memory, error) {
		return store.Create(ctx, request)
	})
	return protocol.MemoryCreateResponse{Memory: item}, err
}

func (s Server) MemoryUpdate(ctx context.Context, raw string) (protocol.MemoryUpdateResponse, error) {
	var request protocol.MemoryUpdateRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryUpdateResponse{}, err
	}
	item, err := withMemoryStore(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (protocol.Memory, error) {
		return store.Update(ctx, request)
	})
	return protocol.MemoryUpdateResponse{Memory: item}, err
}

func (s Server) MemorySupersede(ctx context.Context, raw string) (protocol.MemorySupersedeResponse, error) {
	var request protocol.MemorySupersedeRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemorySupersedeResponse{}, err
	}
	response, err := withMemoryStore(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (protocol.MemorySupersedeResponse, error) {
		return store.Supersede(ctx, request)
	})
	return response, err
}

func (s Server) MemoryInvalidate(ctx context.Context, raw string) (protocol.MemoryInvalidateResponse, error) {
	var request protocol.MemoryInvalidateRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryInvalidateResponse{}, err
	}
	item, err := withMemoryStore(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (protocol.Memory, error) {
		return store.Invalidate(ctx, request)
	})
	return protocol.MemoryInvalidateResponse{Memory: item}, err
}

func (s Server) MemoryDelete(ctx context.Context, raw string) (protocol.MemoryDeleteResponse, error) {
	var request protocol.MemoryDeleteRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryDeleteResponse{}, err
	}
	_, err := withMemoryStore(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (struct{}, error) {
		return struct{}{}, store.Delete(ctx, request)
	})
	return protocol.MemoryDeleteResponse{Deleted: err == nil}, err
}
