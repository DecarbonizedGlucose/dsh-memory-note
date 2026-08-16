package rpc

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/decglu/dsh-memory-note/internal/data"
	"github.com/decglu/dsh-memory-note/internal/memory"
	"github.com/decglu/dsh-memory-note/internal/meta"
)

type Server struct {
	home      string
	lockOwner string
}

func NewServer(home, lockOwner string) Server {
	return Server{home: home, lockOwner: lockOwner}
}

func (s Server) openMeta(ctx context.Context) (*meta.Store, error) {
	return meta.Open(ctx, data.MetaDB(s.home))
}

func withMemory[T any](ctx context.Context, server Server, wid int64, mode string, run func(*memory.Store) (T, error)) (T, error) {
	var zero T
	if err := checkWorkspaceID(wid); err != nil {
		return zero, err
	}
	metaStore, err := server.openMeta(ctx)
	if err != nil {
		return zero, err
	}
	defer metaStore.Close()
	if _, err := metaStore.Find(ctx, wid); err != nil {
		return zero, err
	}
	var lock *meta.Lock
	if mode == "read" {
		lock, err = metaStore.LockRead(ctx, wid, server.lockOwner)
	} else {
		lock, err = metaStore.LockWrite(ctx, wid, server.lockOwner)
	}
	if err != nil {
		return zero, err
	}
	defer lock.Release(context.Background())
	path, err := data.MemoryDB(server.home, wid)
	if err != nil {
		return zero, err
	}
	store, err := memory.Open(ctx, path, wid)
	if err != nil {
		return zero, err
	}
	defer store.Close()
	return run(store)
}

func checkWorkspaceID(wid int64) error {
	if wid < 0 {
		return fmt.Errorf("%w: workspace_id must be a non-negative integer", errBadRequest)
	}
	return nil
}

func removeMemoryDB(home string, wid int64) error {
	path, err := data.MemoryDB(home, wid)
	if err != nil {
		return err
	}
	for _, file := range []string{path, path + "-shm", path + "-wal"} {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete memory db: %w", err)
		}
	}
	return nil
}
