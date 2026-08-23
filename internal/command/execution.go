package command

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/memory"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/meta"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/storage"
)

const (
	lockRead  = "read"
	lockWrite = "write"
)

type execution struct {
	storeRoot   string
	metaStore   *meta.Store
	sessionID   string
	workspaceID int64
	lockMode    string
	done        bool
}

// begin initializes HOME if needed, opens meta.db, acquires the per-WID lock
// when lockMode is set, and verifies the memory directory layout. It covers
// the Session lifecycle steps 4-6 of the overall design; the locks are
// recorded under the owning Session's ID.
func begin(ctx context.Context, storeRoot, sessionID string, workspaceID int64, lockMode string, checkDir bool) (*execution, error) {
	if _, err := meta.InitRoot(ctx, storeRoot); err != nil {
		return nil, err
	}
	store, err := meta.Open(ctx, storeRoot)
	if err != nil {
		return nil, err
	}
	run := &execution{storeRoot: storeRoot, metaStore: store, sessionID: sessionID, workspaceID: workspaceID, lockMode: lockMode}
	if lockMode != "" {
		if err := store.Lock(ctx, workspaceID, lockMode, sessionID); err != nil {
			store.Close()
			return nil, err
		}
	}
	if checkDir {
		if err := run.checkMemoryDir(ctx); err != nil {
			run.Release()
			return nil, err
		}
	}
	return run, nil
}

// Commit finishes a successful command: it releases the per-WID lock and
// closes meta.db. Memory database transactions are committed by the commands
// themselves before this is called.
func (e *execution) Commit(ctx context.Context) error {
	if e.done {
		return nil
	}
	e.done = true
	if e.lockMode != "" {
		if err := e.metaStore.Unlock(ctx, e.workspaceID, e.sessionID); err != nil {
			_ = e.metaStore.Close()
			return err
		}
	}
	return e.metaStore.Close()
}

// Release cleans up an execution that did not reach Commit. Unlocking uses a
// background context so cleanup still works after the command context ends.
func (e *execution) Release() {
	if e.done {
		return
	}
	e.done = true
	if e.lockMode != "" {
		_ = e.metaStore.Unlock(context.Background(), e.workspaceID, e.sessionID)
	}
	_ = e.metaStore.Close()
}

// verifyWriteLock renews the write lock and confirms ownership before a
// modifying memory transaction starts. Losing ownership aborts the command.
func (e *execution) verifyWriteLock(ctx context.Context) error {
	if e.lockMode != lockWrite {
		return nil
	}
	return e.metaStore.RenewWriteLock(ctx, e.workspaceID, e.sessionID)
}

func (e *execution) memoryStore(ctx context.Context, workspaceID int64) (*memory.Store, error) {
	if _, err := e.metaStore.Workspace(ctx, workspaceID); err != nil {
		return nil, err
	}
	return memory.Open(ctx, storage.MemoryDB(e.storeRoot, workspaceID), workspaceID)
}

// checkMemoryDir enforces the memory/ directory layout: every entry must be a
// regular non-symlink file whose name matches the plugin's own
// workspace-{WID}-memory.db pattern. Files of unregistered WIDs are tolerated
// as removable residue and are never opened.
func (e *execution) checkMemoryDir(ctx context.Context) error {
	entries, err := os.ReadDir(storage.MemoryDir(e.storeRoot))
	if err != nil {
		return protocol.NewError(protocol.CodeHomeBroken, "memory directory cannot be read")
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return protocol.NewError(protocol.CodeWorkspaceBroken, "memory directory contains an untrusted file")
		}
		if !validMemoryFileName(entry.Name()) {
			return protocol.NewError(protocol.CodeWorkspaceBroken, "memory directory contains an unexpected file")
		}
	}
	return nil
}

func validMemoryFileName(name string) bool {
	for _, suffix := range []string{"-memory.db", "-memory.db-wal", "-memory.db-shm"} {
		if strings.HasSuffix(name, suffix) {
			wid := strings.TrimSuffix(strings.TrimPrefix(name, "workspace-"), suffix)
			if wid == "" {
				return false
			}
			value, err := strconv.ParseInt(wid, 10, 64)
			return err == nil && value >= 1
		}
	}
	return false
}
