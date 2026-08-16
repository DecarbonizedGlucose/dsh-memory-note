package rpc

import (
	"context"

	"github.com/decglu/dsh-memory-note/internal/data"
	"github.com/decglu/dsh-memory-note/internal/memory"
)

type WorkspaceRegisterRequest struct {
	Path string `json:"path"`
}

type WorkspaceRegisterResponse struct {
	WorkspaceID int64  `json:"workspace_id"`
	Path        string `json:"path"`
	Created     bool   `json:"created"`
}

func (s Server) WorkspaceRegister(ctx context.Context, raw string) (WorkspaceRegisterResponse, error) {
	var request WorkspaceRegisterRequest
	if err := decode(raw, &request); err != nil {
		return WorkspaceRegisterResponse{}, err
	}
	metaStore, err := s.openMeta(ctx)
	if err != nil {
		return WorkspaceRegisterResponse{}, err
	}
	defer metaStore.Close()
	workspace, created, err := metaStore.Register(ctx, request.Path)
	if err != nil {
		return WorkspaceRegisterResponse{}, err
	}
	lock, err := metaStore.LockWrite(ctx, workspace.ID, s.lockOwner)
	if err != nil {
		return WorkspaceRegisterResponse{}, err
	}
	defer lock.Release(context.Background())
	path, err := data.MemoryDB(s.home, workspace.ID)
	if err != nil {
		return WorkspaceRegisterResponse{}, err
	}
	store, err := memory.Open(ctx, path, workspace.ID)
	if err != nil {
		return WorkspaceRegisterResponse{}, err
	}
	if err := store.Close(); err != nil {
		return WorkspaceRegisterResponse{}, err
	}
	return WorkspaceRegisterResponse{WorkspaceID: workspace.ID, Path: workspace.Path, Created: created}, nil
}

type WorkspaceRebindRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	Path        string `json:"path"`
}

type WorkspaceRebindResponse struct {
	WorkspaceID int64  `json:"workspace_id"`
	Path        string `json:"path"`
}

func (s Server) WorkspaceRebind(ctx context.Context, raw string) (WorkspaceRebindResponse, error) {
	var request WorkspaceRebindRequest
	if err := decode(raw, &request); err != nil {
		return WorkspaceRebindResponse{}, err
	}
	if err := checkWorkspaceID(request.WorkspaceID); err != nil {
		return WorkspaceRebindResponse{}, err
	}
	metaStore, err := s.openMeta(ctx)
	if err != nil {
		return WorkspaceRebindResponse{}, err
	}
	defer metaStore.Close()
	lock, err := metaStore.LockWrite(ctx, request.WorkspaceID, s.lockOwner)
	if err != nil {
		return WorkspaceRebindResponse{}, err
	}
	defer lock.Release(context.Background())
	workspace, err := metaStore.Rebind(ctx, request.WorkspaceID, request.Path)
	if err != nil {
		return WorkspaceRebindResponse{}, err
	}
	return WorkspaceRebindResponse{WorkspaceID: workspace.ID, Path: workspace.Path}, nil
}

type WorkspaceClearRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
}

type WorkspaceClearResponse struct {
	Deleted int64 `json:"deleted"`
}

func (s Server) WorkspaceClear(ctx context.Context, raw string) (WorkspaceClearResponse, error) {
	var request WorkspaceClearRequest
	if err := decode(raw, &request); err != nil {
		return WorkspaceClearResponse{}, err
	}
	count, err := withMemory(ctx, s, request.WorkspaceID, "write", func(store *memory.Store) (int64, error) {
		return store.Clear(ctx)
	})
	return WorkspaceClearResponse{Deleted: count}, err
}

type WorkspaceDeleteRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
}

type WorkspaceDeleteResponse struct {
	Deleted bool `json:"deleted"`
}

func (s Server) WorkspaceDelete(ctx context.Context, raw string) (WorkspaceDeleteResponse, error) {
	var request WorkspaceDeleteRequest
	if err := decode(raw, &request); err != nil {
		return WorkspaceDeleteResponse{}, err
	}
	if err := checkWorkspaceID(request.WorkspaceID); err != nil {
		return WorkspaceDeleteResponse{}, err
	}
	metaStore, err := s.openMeta(ctx)
	if err != nil {
		return WorkspaceDeleteResponse{}, err
	}
	defer metaStore.Close()
	lock, err := metaStore.LockWrite(ctx, request.WorkspaceID, s.lockOwner)
	if err != nil {
		return WorkspaceDeleteResponse{}, err
	}
	defer lock.Release(context.Background())
	// Delete the data file while the workspace is still protected by its lock.
	if err := removeMemoryDB(s.home, request.WorkspaceID); err != nil {
		return WorkspaceDeleteResponse{}, err
	}
	if err := metaStore.Delete(ctx, request.WorkspaceID); err != nil {
		return WorkspaceDeleteResponse{}, err
	}
	return WorkspaceDeleteResponse{Deleted: true}, nil
}
