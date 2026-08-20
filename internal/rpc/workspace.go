package rpc

import (
	"context"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/data"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/memory"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

func (s Server) WorkspaceRegister(ctx context.Context, raw string) (protocol.WorkspaceRegisterResponse, error) {
	var request protocol.WorkspaceRegisterRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.WorkspaceRegisterResponse{}, err
	}
	metaStore, err := s.openMeta(ctx)
	if err != nil {
		return protocol.WorkspaceRegisterResponse{}, err
	}
	defer metaStore.Close()
	workspace, created, err := metaStore.Register(ctx, request.Path)
	if err != nil {
		return protocol.WorkspaceRegisterResponse{}, err
	}
	lock, err := metaStore.LockWrite(ctx, workspace.ID, s.lockOwner)
	if err != nil {
		return protocol.WorkspaceRegisterResponse{}, err
	}
	defer lock.Release(context.Background())
	path, err := data.MemoryDB(s.home, workspace.ID)
	if err != nil {
		return protocol.WorkspaceRegisterResponse{}, err
	}
	store, err := memory.Open(ctx, path, workspace.ID)
	if err != nil {
		return protocol.WorkspaceRegisterResponse{}, err
	}
	if err := store.Close(); err != nil {
		return protocol.WorkspaceRegisterResponse{}, err
	}
	return protocol.WorkspaceRegisterResponse{
		WorkspaceID: workspace.ID,
		Path:        workspace.Path,
		Created:     created,
	}, nil
}

func (s Server) WorkspaceRebind(ctx context.Context, raw string) (protocol.WorkspaceRebindResponse, error) {
	var request protocol.WorkspaceRebindRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.WorkspaceRebindResponse{}, err
	}
	if err := checkWorkspaceID(request.WorkspaceID); err != nil {
		return protocol.WorkspaceRebindResponse{}, err
	}
	metaStore, err := s.openMeta(ctx)
	if err != nil {
		return protocol.WorkspaceRebindResponse{}, err
	}
	defer metaStore.Close()
	lock, err := metaStore.LockWrite(ctx, request.WorkspaceID, s.lockOwner)
	if err != nil {
		return protocol.WorkspaceRebindResponse{}, err
	}
	defer lock.Release(context.Background())
	workspace, err := metaStore.Rebind(ctx, request.WorkspaceID, request.Path)
	if err != nil {
		return protocol.WorkspaceRebindResponse{}, err
	}
	return protocol.WorkspaceRebindResponse{
		WorkspaceID: workspace.ID,
		Path:        workspace.Path,
	}, nil
}

func (s Server) WorkspaceClear(ctx context.Context, raw string) (protocol.WorkspaceClearResponse, error) {
	var request protocol.WorkspaceClearRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.WorkspaceClearResponse{}, err
	}
	count, err := withMemoryStore(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (int64, error) {
		return store.Clear(ctx)
	})
	return protocol.WorkspaceClearResponse{Deleted: count}, err
}

func (s Server) WorkspaceDelete(ctx context.Context, raw string) (protocol.WorkspaceDeleteResponse, error) {
	var request protocol.WorkspaceDeleteRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.WorkspaceDeleteResponse{}, err
	}
	if err := checkWorkspaceID(request.WorkspaceID); err != nil {
		return protocol.WorkspaceDeleteResponse{}, err
	}
	metaStore, err := s.openMeta(ctx)
	if err != nil {
		return protocol.WorkspaceDeleteResponse{}, err
	}
	defer metaStore.Close()
	lock, err := metaStore.LockWrite(ctx, request.WorkspaceID, s.lockOwner)
	if err != nil {
		return protocol.WorkspaceDeleteResponse{}, err
	}
	defer lock.Release(context.Background())
	if err := removeMemoryDB(s.home, request.WorkspaceID); err != nil {
		return protocol.WorkspaceDeleteResponse{}, err
	}
	if err := metaStore.Delete(ctx, request.WorkspaceID); err != nil {
		return protocol.WorkspaceDeleteResponse{}, err
	}
	return protocol.WorkspaceDeleteResponse{Deleted: true}, nil
}
