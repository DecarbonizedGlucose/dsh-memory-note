// Package meta stores workspace identities and owns the per-WID cross-process
// read/write locks.
package meta

import (
	"context"
	stdsql "database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/home"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/statements"
)

const applicationVersion = "1.0"

const (
	lockTTL      = 30 * time.Second
	lockWaitTime = 5 * time.Second
	lockRetry    = 25 * time.Millisecond
)

var errNotInitialized = errors.New("store is not initialized")
var errLockConflict = errors.New("workspace lock conflict")

type Store struct {
	db *stdsql.DB
}

func InitRoot(ctx context.Context, storeRoot string) (bool, error) {
	if err := CheckRoot(storeRoot); err == nil {
		return false, nil
	} else if !errors.Is(err, errNotInitialized) {
		return false, err
	}
	parent := filepath.Dir(storeRoot)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return false, internalError("cannot create store parent")
	}
	temp, err := os.MkdirTemp(parent, ".dsh-memory-note-init-")
	if err != nil {
		return false, internalError("cannot create temporary store")
	}
	defer os.RemoveAll(temp)
	if err := os.Chmod(temp, 0o700); err != nil {
		return false, internalError("cannot protect temporary store")
	}
	if err := os.Mkdir(home.MemoryDir(temp), 0o700); err != nil {
		return false, internalError("cannot create memory directory")
	}
	store, err := create(ctx, home.MetaDB(temp))
	if err != nil {
		return false, err
	}
	if err := store.Close(); err != nil {
		return false, internalError("cannot close meta database")
	}
	if err := syncPath(home.MetaDB(temp)); err != nil {
		return false, err
	}
	if err := syncPath(temp); err != nil {
		return false, err
	}
	if err := os.Rename(temp, storeRoot); err != nil {
		if checkErr := CheckRoot(storeRoot); checkErr == nil {
			return false, nil
		}
		return false, internalError("cannot publish store")
	}
	if err := syncPath(parent); err != nil {
		return false, err
	}
	return true, nil
}

func CheckRoot(storeRoot string) error {
	root, err := os.Lstat(storeRoot)
	if errors.Is(err, os.ErrNotExist) {
		return errNotInitialized
	}
	if err != nil || !root.IsDir() || root.Mode()&os.ModeSymlink != 0 {
		return protocol.NewError(protocol.CodeHomeBroken, "HOME is not a trusted directory")
	}
	metaInfo, metaErr := os.Lstat(home.MetaDB(storeRoot))
	memoryInfo, memoryErr := os.Lstat(home.MemoryDir(storeRoot))
	if metaErr != nil || memoryErr != nil || !metaInfo.Mode().IsRegular() || metaInfo.Mode()&os.ModeSymlink != 0 ||
		!memoryInfo.IsDir() || memoryInfo.Mode()&os.ModeSymlink != 0 {
		return protocol.NewError(protocol.CodeHomeBroken, "HOME structure is incomplete")
	}
	return nil
}

func Open(ctx context.Context, storeRoot string) (*Store, error) {
	if err := CheckRoot(storeRoot); err != nil {
		if errors.Is(err, errNotInitialized) {
			return nil, protocol.NewError(protocol.CodeHomeBroken, "HOME is not initialized")
		}
		return nil, err
	}
	db, err := openDB(home.MetaDB(storeRoot), "rw")
	if err != nil {
		return nil, protocol.NewError(protocol.CodeHomeBroken, "meta database cannot be opened")
	}
	store := &Store{db: db}
	for _, query := range statements.Setup {
		if _, err := db.ExecContext(ctx, query); err != nil {
			db.Close()
			return nil, sqliteError(err, protocol.CodeHomeBroken, "meta database cannot be configured")
		}
	}
	if err := checkHeader(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func create(ctx context.Context, path string) (*Store, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, internalError("cannot create meta database")
	}
	if err := file.Close(); err != nil {
		return nil, internalError("cannot close new meta database")
	}
	db, err := openDB(path, "rw")
	if err != nil {
		return nil, internalError("cannot open new meta database")
	}
	store := &Store{db: db}
	queries := append([]string{}, statements.Setup...)
	queries = append(queries, statements.MetaSchema...)
	for _, query := range queries {
		if _, err := db.ExecContext(ctx, query); err != nil {
			db.Close()
			return nil, internalError("cannot initialize meta database")
		}
	}
	if _, err := db.ExecContext(ctx, statements.InsertMetaInfo, applicationVersion); err != nil {
		db.Close()
		return nil, internalError("cannot write meta database header")
	}
	return store, nil
}

func openDB(path, mode string) (*stdsql.DB, error) {
	uri := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=" + mode}).String()
	db, err := stdsql.Open("sqlite", uri)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func checkHeader(ctx context.Context, db *stdsql.DB) error {
	var applicationID, schemaVersion, recordedVersion int
	if err := db.QueryRowContext(ctx, statements.ReadApplicationID).Scan(&applicationID); err != nil || applicationID != statements.MetaApplicationID {
		return protocol.NewError(protocol.CodeHomeBroken, "meta database application ID is invalid")
	}
	if err := db.QueryRowContext(ctx, statements.ReadSchemaVersion).Scan(&schemaVersion); err != nil {
		return protocol.NewError(protocol.CodeHomeBroken, "meta schema version is unreadable")
	}
	if schemaVersion != statements.SchemaVersion {
		return protocol.NewError(protocol.CodeSchemaMismatch, "meta schema version is not supported")
	}
	if err := db.QueryRowContext(ctx, statements.ReadMetaInfo).Scan(&recordedVersion); err != nil || recordedVersion != statements.SchemaVersion {
		return protocol.NewError(protocol.CodeHomeBroken, "meta database header is invalid")
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// Lock acquires a per-WID read or write lock for the given session, waiting
// up to lockWaitTime. It returns workspace_busy on timeout.
func (s *Store) Lock(ctx context.Context, workspaceID int64, mode, sessionID string) error {
	deadline := time.Now().Add(lockWaitTime)
	for {
		err := s.tryLock(ctx, workspaceID, mode, sessionID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errLockConflict) {
			return err
		}
		if time.Now().After(deadline) {
			return protocol.NewError(protocol.CodeWorkspaceBusy, "workspace lock is held by another process")
		}
		select {
		case <-ctx.Done():
			return protocol.NewError(protocol.CodeWorkspaceBusy, "workspace lock wait was interrupted")
		case <-time.After(lockRetry):
		}
	}
}

func (s *Store) tryLock(ctx context.Context, workspaceID int64, mode, sessionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return internalError("cannot begin lock transaction")
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, statements.DeleteExpiredLocks, now); err != nil {
		return internalError("cannot reclaim expired locks")
	}
	if mode == "write" {
		if err := checkLockConflict(ctx, tx, statements.SelectAnyLock, workspaceID, now); err != nil {
			return err
		}
	} else {
		if err := checkLockConflict(ctx, tx, statements.SelectWriteLock, workspaceID, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, statements.InsertLock, workspaceID, mode, sessionID, now, now+int64(lockTTL.Seconds())); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return errLockConflict
		}
		return internalError("cannot acquire workspace lock")
	}
	if err := tx.Commit(); err != nil {
		return internalError("cannot commit workspace lock")
	}
	return nil
}

func checkLockConflict(ctx context.Context, tx *stdsql.Tx, query string, workspaceID, now int64) error {
	var found int
	err := tx.QueryRowContext(ctx, query, workspaceID, now).Scan(&found)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return internalError("cannot inspect workspace locks")
	}
	return errLockConflict
}

// RenewWriteLock verifies that the session still owns its write lock and
// extends the lease. It must be called before starting a modifying memory
// transaction; losing ownership aborts the operation.
func (s *Store) RenewWriteLock(ctx context.Context, workspaceID int64, sessionID string) error {
	now := time.Now().Unix()
	result, err := s.db.ExecContext(ctx, statements.RenewLock, now+int64(lockTTL.Seconds()), workspaceID, sessionID, now)
	if err != nil {
		return internalError("cannot renew workspace lock")
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return protocol.NewError(protocol.CodeInternal, "workspace lock ownership was lost")
	}
	return nil
}

func (s *Store) Unlock(ctx context.Context, workspaceID int64, sessionID string) error {
	if _, err := s.db.ExecContext(ctx, statements.DeleteLock, workspaceID, sessionID); err != nil {
		return internalError("cannot release workspace lock")
	}
	return nil
}

func (s *Store) Workspace(ctx context.Context, id int64) (protocol.Workspace, error) {
	tx, err := s.db.BeginTx(ctx, &stdsql.TxOptions{ReadOnly: true})
	if err != nil {
		return protocol.Workspace{}, internalError("cannot begin meta read")
	}
	defer tx.Rollback()
	workspace, err := scanWorkspace(tx.QueryRowContext(ctx, statements.SelectWorkspace, id))
	if err != nil {
		return protocol.Workspace{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.Workspace{}, internalError("cannot finish meta read")
	}
	return workspace, nil
}

func (s *Store) Resolve(ctx context.Context, path string) (*protocol.Workspace, error) {
	tx, err := s.db.BeginTx(ctx, &stdsql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, internalError("cannot begin meta read")
	}
	defer tx.Rollback()
	workspace, err := scanWorkspace(tx.QueryRowContext(ctx, statements.SelectWorkspaceByPath, path))
	if appError, ok := err.(*protocol.Error); ok && appError.Code == protocol.CodeWorkspaceNotFound {
		if commitErr := tx.Commit(); commitErr != nil {
			return nil, internalError("cannot finish meta read")
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, internalError("cannot finish meta read")
	}
	return &workspace, nil
}

func (s *Store) Workspaces(ctx context.Context) ([]protocol.Workspace, error) {
	tx, err := s.db.BeginTx(ctx, &stdsql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, internalError("cannot begin meta read")
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, statements.SelectAllWorkspaces)
	if err != nil {
		return nil, internalError("cannot list workspaces")
	}
	defer rows.Close()
	result := make([]protocol.Workspace, 0)
	for rows.Next() {
		workspace, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, workspace)
	}
	if err := rows.Err(); err != nil {
		return nil, internalError("cannot read workspaces")
	}
	if err := tx.Commit(); err != nil {
		return nil, internalError("cannot finish meta read")
	}
	return result, nil
}

func (s *Store) Register(ctx context.Context, path string) (protocol.Workspace, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.Workspace{}, false, internalError("cannot begin workspace registration")
	}
	defer tx.Rollback()
	if existing, err := scanWorkspace(tx.QueryRowContext(ctx, statements.SelectWorkspaceByPath, path)); err == nil {
		if commitErr := tx.Commit(); commitErr != nil {
			return protocol.Workspace{}, false, internalError("cannot finish workspace registration")
		}
		return existing, false, nil
	} else if appError, ok := err.(*protocol.Error); !ok || appError.Code != protocol.CodeWorkspaceNotFound {
		return protocol.Workspace{}, false, err
	}
	now := time.Now()
	nowOffset := offsetMinutes(now)
	result, err := tx.ExecContext(ctx, statements.InsertWorkspace, path, now.Unix(), nowOffset, now.Unix(), nowOffset)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			// A concurrent registrar won the path; release the connection and
			// report the existing mapping.
			_ = tx.Rollback()
			existing, resolveErr := s.Resolve(ctx, path)
			if resolveErr != nil {
				return protocol.Workspace{}, false, resolveErr
			}
			if existing == nil {
				return protocol.Workspace{}, false, internalError("cannot finish workspace registration")
			}
			return *existing, false, nil
		}
		return protocol.Workspace{}, false, internalError("cannot register workspace")
	}
	id, err := result.LastInsertId()
	if err != nil || id < 1 || id > protocol.MaxSafeInteger {
		return protocol.Workspace{}, false, internalError("cannot allocate workspace ID")
	}
	if err := tx.Commit(); err != nil {
		return protocol.Workspace{}, false, internalError("cannot finish workspace registration")
	}
	return protocol.Workspace{ID: id, Path: path, CreatedAt: timestamp(now), UpdatedAt: timestamp(now)}, true, nil
}

func (s *Store) Rebind(ctx context.Context, id int64, path string) (protocol.Workspace, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.Workspace{}, internalError("cannot begin workspace rebind")
	}
	defer tx.Rollback()
	workspace, err := scanWorkspace(tx.QueryRowContext(ctx, statements.SelectWorkspace, id))
	if err != nil {
		return protocol.Workspace{}, err
	}
	if workspace.Path == path {
		if commitErr := tx.Commit(); commitErr != nil {
			return protocol.Workspace{}, internalError("cannot finish workspace rebind")
		}
		return workspace, nil
	}
	if _, err := scanWorkspace(tx.QueryRowContext(ctx, statements.SelectWorkspaceByPath, path)); err == nil {
		return protocol.Workspace{}, protocol.NewError(protocol.CodeWorkspacePathUsed, "workspace path is already registered")
	} else if appError, ok := err.(*protocol.Error); !ok || appError.Code != protocol.CodeWorkspaceNotFound {
		return protocol.Workspace{}, err
	}
	now := time.Now()
	if _, err := tx.ExecContext(ctx, statements.UpdateWorkspacePath, path, now.Unix(), offsetMinutes(now), id); err != nil {
		return protocol.Workspace{}, internalError("cannot rebind workspace")
	}
	if err := tx.Commit(); err != nil {
		return protocol.Workspace{}, internalError("cannot finish workspace rebind")
	}
	workspace.Path, workspace.UpdatedAt = path, timestamp(now)
	return workspace, nil
}

func (s *Store) DeleteWorkspace(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return internalError("cannot begin workspace deletion")
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, statements.DeleteWorkspace, id)
	if err != nil {
		return internalError("cannot delete workspace")
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return protocol.NewError(protocol.CodeWorkspaceNotFound, "workspace not found")
	}
	if err := tx.Commit(); err != nil {
		return internalError("cannot finish workspace deletion")
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanWorkspace(row scanner) (protocol.Workspace, error) {
	var workspace protocol.Workspace
	var created, createdOffset, updated, updatedOffset int64
	if err := row.Scan(&workspace.ID, &workspace.Path, &created, &createdOffset, &updated, &updatedOffset); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return protocol.Workspace{}, protocol.NewError(protocol.CodeWorkspaceNotFound, "workspace not found")
		}
		return protocol.Workspace{}, internalError("cannot read workspace")
	}
	if workspace.ID < 1 || workspace.ID > protocol.MaxSafeInteger ||
		createdOffset < -840 || createdOffset > 840 || updatedOffset < -840 || updatedOffset > 840 ||
		!filepath.IsAbs(workspace.Path) || filepath.Clean(workspace.Path) != workspace.Path {
		return protocol.Workspace{}, protocol.NewError(protocol.CodeHomeBroken, "workspace row is invalid")
	}
	workspace.CreatedAt = timestampAt(created, createdOffset)
	workspace.UpdatedAt = timestampAt(updated, updatedOffset)
	return workspace, nil
}

func CanonicalPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", protocol.Invalid("workspace path must be absolute")
	}
	cleaned := filepath.Clean(path)
	info, err := os.Stat(cleaned)
	if err != nil {
		return "", protocol.Invalid("workspace path does not exist")
	}
	if !info.IsDir() {
		return "", protocol.Invalid("workspace path must be a directory")
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", protocol.Invalid("workspace path cannot be resolved")
	}
	return filepath.Clean(resolved), nil
}

func sqliteError(err error, fallbackCode, message string) error {
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "busy") || strings.Contains(text, "locked") {
		return protocol.NewError(protocol.CodeWorkspaceBusy, "another command is using the store")
	}
	return protocol.NewError(fallbackCode, message)
}

func internalError(message string) error { return protocol.NewError(protocol.CodeInternal, message) }

func syncPath(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return internalError("cannot open path for sync")
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return internalError("cannot sync store")
	}
	return nil
}

func timestamp(value time.Time) protocol.Timestamp { return protocol.Timestamp{Time: value} }

func timestampAt(epochSeconds, offsetMinutes int64) protocol.Timestamp {
	location := time.FixedZone("", int(offsetMinutes)*60)
	return protocol.Timestamp{Time: time.Unix(epochSeconds, 0).In(location)}
}

func offsetMinutes(value time.Time) int64 {
	_, offset := value.Zone()
	return int64(offset / 60)
}
