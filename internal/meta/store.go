package meta

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/data"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
	storesql "github.com/DecarbonizedGlucose/dsh-memory-note/internal/store/sql"
)

type Workspace struct {
	ID        int64
	Path      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Store struct {
	db *stdsql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	dsn := (&url.URL{Scheme: "file", Path: path}).String()
	db, err := stdsql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open meta db: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.init(ctx); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) init(ctx context.Context) error {
	queries := append(append([]string{}, storesql.Setup...), storesql.MetaSchema...)
	for _, query := range queries {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("init meta db: %w", err)
		}
	}
	return nil
}

// PrepareHome creates a new data home or validates an existing one.
// Existing incomplete homes are never repaired automatically.
func PrepareHome(ctx context.Context, home, initID string) (bool, error) {
	info, err := os.Stat(home)
	if err == nil {
		if !info.IsDir() {
			return false, protocol.ErrHomeBroken
		}
		return false, checkHome(home)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("check data home: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(home), 0o700); err != nil {
		return false, fmt.Errorf("create data parent: %w", err)
	}
	temp := home + ".init-" + initID
	if err := os.Mkdir(temp, 0o700); err != nil {
		return false, fmt.Errorf("create temporary data home: %w", err)
	}
	defer os.RemoveAll(temp)
	if err := os.Mkdir(data.MemoryDir(temp), 0o700); err != nil {
		return false, fmt.Errorf("create memory directory: %w", err)
	}
	store, err := Open(ctx, data.MetaDB(temp))
	if err != nil {
		return false, err
	}
	if err := store.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(temp, home); err != nil {
		// Another one-shot process may have initialized the same home.
		if checkErr := checkHome(home); checkErr == nil {
			return false, nil
		}
		return false, fmt.Errorf("install data home: %w", err)
	}
	return true, nil
}

func checkHome(home string) error {
	metaInfo, metaErr := os.Stat(data.MetaDB(home))
	memoryInfo, memoryErr := os.Stat(data.MemoryDir(home))
	if metaErr != nil || memoryErr != nil || !metaInfo.Mode().IsRegular() || !memoryInfo.IsDir() {
		return protocol.ErrHomeBroken
	}
	return nil
}

func (s *Store) Register(ctx context.Context, path string) (Workspace, bool, error) {
	path, err := cleanPath(path)
	if err != nil {
		return Workspace{}, false, err
	}
	if current, err := s.FindPath(ctx, path); err == nil {
		return current, false, nil
	} else if !errors.Is(err, protocol.ErrWorkspaceNotFound) {
		return Workspace{}, false, err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, storesql.InsertWorkspace, path, stamp(now), stamp(now))
	if err != nil {
		// Another process may have registered the same path.
		if current, findErr := s.FindPath(ctx, path); findErr == nil {
			return current, false, nil
		}
		return Workspace{}, false, fmt.Errorf("register workspace: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Workspace{}, false, fmt.Errorf("read workspace id: %w", err)
	}
	return Workspace{ID: id, Path: path, CreatedAt: now, UpdatedAt: now}, true, nil
}

func (s *Store) Find(ctx context.Context, id int64) (Workspace, error) {
	return scanWorkspace(s.db.QueryRowContext(ctx, storesql.SelectWorkspace, id))
}

func (s *Store) FindPath(ctx context.Context, path string) (Workspace, error) {
	path, err := cleanPath(path)
	if err != nil {
		return Workspace{}, err
	}
	return scanWorkspace(s.db.QueryRowContext(ctx, storesql.SelectWorkspaceByPath, path))
}

func (s *Store) Rebind(ctx context.Context, id int64, path string) (Workspace, error) {
	path, err := cleanPath(path)
	if err != nil {
		return Workspace{}, err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, storesql.UpdateWorkspacePath, path, stamp(now), id)
	if err != nil {
		return Workspace{}, protocol.ErrWorkspacePathUsed
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return Workspace{}, protocol.ErrWorkspaceNotFound
	}
	return s.Find(ctx, id)
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, storesql.DeleteWorkspace, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return protocol.ErrWorkspaceNotFound
	}
	return nil
}

type Lock struct {
	store *Store
	wid   int64
	owner string
}

// LockRead allows other readers. LockWrite waits until no other lock exists.
func (s *Store) LockRead(ctx context.Context, wid int64, owner string) (*Lock, error) {
	return s.lock(ctx, wid, owner, "read")
}

func (s *Store) LockWrite(ctx context.Context, wid int64, owner string) (*Lock, error) {
	return s.lock(ctx, wid, owner, "write")
}

func (s *Store) lock(ctx context.Context, wid int64, owner, mode string) (*Lock, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, fmt.Errorf("lock owner is required")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		now := time.Now()
		_, _ = s.db.ExecContext(ctx, storesql.DeleteExpiredLocks, now.UnixMilli())
		query := storesql.InsertWorkspaceLock(mode == "write")
		result, err := s.db.ExecContext(ctx, query,
			wid, owner, mode, now.Add(15*time.Second).UnixMilli(), wid, wid)
		if err != nil {
			return nil, fmt.Errorf("lock workspace: %w", err)
		}
		if count, _ := result.RowsAffected(); count == 1 {
			return &Lock{store: s, wid: wid, owner: owner}, nil
		}
		if _, err := s.Find(ctx, wid); err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, protocol.ErrWorkspaceBusy
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (l *Lock) Release(ctx context.Context) error {
	_, err := l.store.db.ExecContext(ctx, storesql.DeleteWorkspaceLock, l.wid, l.owner)
	return err
}

func cleanPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("make path absolute: %w", err)
	}
	return filepath.Clean(abs), nil
}

func stamp(value time.Time) string { return value.Format(time.RFC3339Nano) }

type rowScanner interface{ Scan(...any) error }

func scanWorkspace(row rowScanner) (Workspace, error) {
	var workspace Workspace
	var created, updated string
	if err := row.Scan(&workspace.ID, &workspace.Path, &created, &updated); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return Workspace{}, protocol.ErrWorkspaceNotFound
		}
		return Workspace{}, err
	}
	workspace.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	workspace.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return workspace, nil
}
