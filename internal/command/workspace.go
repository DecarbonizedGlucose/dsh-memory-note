package command

import (
	"context"
	"errors"
	"os"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/storage"
)

func workspaceResolve(ctx context.Context, storeRoot, sessionID, raw string) (protocol.WorkspaceResolveData, error) {
	var request protocol.WorkspaceResolveRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.WorkspaceResolveData{}, err
	}
	path, err := storage.CanonicalPath(request.Path)
	if err != nil {
		return protocol.WorkspaceResolveData{}, err
	}
	run, err := begin(ctx, storeRoot, sessionID, 0, "", true)
	if err != nil {
		return protocol.WorkspaceResolveData{}, err
	}
	defer run.Release()
	workspace, err := run.metaStore.Resolve(ctx, path)
	if err != nil {
		return protocol.WorkspaceResolveData{}, err
	}
	if err := run.Commit(ctx); err != nil {
		return protocol.WorkspaceResolveData{}, err
	}
	return protocol.WorkspaceResolveData{Workspace: workspace}, nil
}

func workspaceRegister(ctx context.Context, storeRoot, sessionID, raw string) (protocol.WorkspaceRegisterData, error) {
	var request protocol.WorkspaceRegisterRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.WorkspaceRegisterData{}, err
	}
	path, err := storage.CanonicalPath(request.Path)
	if err != nil {
		return protocol.WorkspaceRegisterData{}, err
	}
	run, err := begin(ctx, storeRoot, sessionID, 0, "", true)
	if err != nil {
		return protocol.WorkspaceRegisterData{}, err
	}
	defer run.Release()
	workspace, created, err := run.metaStore.Register(ctx, path, storeRoot)
	if err != nil {
		return protocol.WorkspaceRegisterData{}, err
	}
	if err := run.Commit(ctx); err != nil {
		return protocol.WorkspaceRegisterData{}, err
	}
	return protocol.WorkspaceRegisterData{Workspace: workspace, Created: created}, nil
}

func workspaceRebind(ctx context.Context, storeRoot, sessionID, raw string) (protocol.WorkspaceRebindData, error) {
	var request protocol.WorkspaceRebindRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.WorkspaceRebindData{}, err
	}
	if err := validWorkspaceID(request.WorkspaceID); err != nil {
		return protocol.WorkspaceRebindData{}, err
	}
	path, err := storage.CanonicalPath(request.Path)
	if err != nil {
		return protocol.WorkspaceRebindData{}, err
	}
	run, err := begin(ctx, storeRoot, sessionID, request.WorkspaceID, lockWrite, true)
	if err != nil {
		return protocol.WorkspaceRebindData{}, err
	}
	defer run.Release()
	workspace, err := run.metaStore.Rebind(ctx, request.WorkspaceID, path)
	if err != nil {
		return protocol.WorkspaceRebindData{}, err
	}
	if err := run.Commit(ctx); err != nil {
		return protocol.WorkspaceRebindData{}, err
	}
	return protocol.WorkspaceRebindData{Workspace: workspace}, nil
}

func workspaceClear(ctx context.Context, storeRoot, sessionID, raw string) (protocol.WorkspaceClearData, error) {
	var request protocol.WorkspaceClearRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.WorkspaceClearData{}, err
	}
	if err := validWorkspaceID(request.WorkspaceID); err != nil {
		return protocol.WorkspaceClearData{}, err
	}
	run, err := begin(ctx, storeRoot, sessionID, request.WorkspaceID, lockWrite, true)
	if err != nil {
		return protocol.WorkspaceClearData{}, err
	}
	defer run.Release()
	if err := run.verifyWriteLock(ctx); err != nil {
		return protocol.WorkspaceClearData{}, err
	}
	store, err := run.memoryStore(ctx, request.WorkspaceID)
	if err != nil {
		return protocol.WorkspaceClearData{}, err
	}
	defer store.Close()
	tx, err := store.Begin(ctx, false)
	if err != nil {
		return protocol.WorkspaceClearData{}, err
	}
	defer tx.Rollback()
	count, err := tx.Clear(ctx)
	if err != nil {
		return protocol.WorkspaceClearData{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.WorkspaceClearData{}, protocol.NewError(protocol.CodeInternal, "cannot commit workspace clear")
	}
	if err := run.Commit(ctx); err != nil {
		return protocol.WorkspaceClearData{}, err
	}
	return protocol.WorkspaceClearData{DeletedCount: count}, nil
}

func workspaceDelete(ctx context.Context, storeRoot, sessionID, raw string) (protocol.WorkspaceDeleteData, error) {
	var request protocol.WorkspaceDeleteRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.WorkspaceDeleteData{}, err
	}
	if err := validWorkspaceID(request.WorkspaceID); err != nil {
		return protocol.WorkspaceDeleteData{}, err
	}
	// The directory hygiene check is skipped: workspace-delete is the
	// designated cleanup path for workspaces whose memory database is missing
	// or corrupt.
	run, err := begin(ctx, storeRoot, sessionID, request.WorkspaceID, lockWrite, false)
	if err != nil {
		return protocol.WorkspaceDeleteData{}, err
	}
	defer run.Release()
	if _, err := run.metaStore.Workspace(ctx, request.WorkspaceID); err != nil {
		return protocol.WorkspaceDeleteData{}, err
	}
	if err := run.metaStore.DeleteWorkspace(ctx, request.WorkspaceID); err != nil {
		return protocol.WorkspaceDeleteData{}, err
	}
	if err := removeMemoryFiles(storeRoot, request.WorkspaceID); err != nil {
		return protocol.WorkspaceDeleteData{}, err
	}
	if err := run.Commit(ctx); err != nil {
		return protocol.WorkspaceDeleteData{}, err
	}
	return protocol.WorkspaceDeleteData{Deleted: true}, nil
}

func removeMemoryFiles(storeRoot string, workspaceID int64) error {
	path := storage.MemoryDB(storeRoot, workspaceID)
	// Best-effort sidecars first, then the main file, which must go away.
	for _, target := range []string{path + "-wal", path + "-shm", path + "-journal"} {
		if info, err := os.Lstat(target); err == nil {
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return protocol.NewError(protocol.CodeWorkspaceBroken, "workspace memory file is not trusted")
			}
			if err := os.Remove(target); err != nil {
				return protocol.NewError(protocol.CodeInternal, "cannot delete workspace memory sidecar")
			}
		}
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return protocol.NewError(protocol.CodeWorkspaceBroken, "workspace memory file is not trusted")
		}
		if err := os.Remove(path); err != nil {
			return protocol.NewError(protocol.CodeInternal, "cannot delete workspace memory database")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return protocol.NewError(protocol.CodeInternal, "cannot inspect workspace memory database")
	}
	return storage.SyncDir(storage.MemoryDir(storeRoot))
}
