// Package memory stores one workspace's memories and their archived
// historical versions.
package memory

import (
	"bytes"
	"context"
	stdsql "database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/statements"
)

type Store struct {
	db  *stdsql.DB
	wid int64
}

type Tx struct {
	*stdsql.Tx
	wid int64
}

func CreateFile(ctx context.Context, path string, workspaceID int64) error {
	if _, err := os.Lstat(path); err == nil {
		return protocol.NewError(protocol.CodeWorkspaceBroken, "workspace memory database already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return internalError("cannot inspect memory database path")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return internalError("cannot create memory database")
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if err := file.Close(); err != nil {
		return internalError("cannot close new memory database")
	}
	db, err := openDB(path, "rw")
	if err != nil {
		return internalError("cannot open new memory database")
	}
	queries := append([]string{}, statements.Setup...)
	queries = append(queries, statements.MemorySchema...)
	for _, query := range queries {
		if _, err := db.ExecContext(ctx, query); err != nil {
			db.Close()
			return internalError("cannot initialize memory database")
		}
	}
	if _, err := db.ExecContext(ctx, statements.InsertMemoryInfo, workspaceID); err != nil {
		db.Close()
		return internalError("cannot write memory database header")
	}
	if err := db.Close(); err != nil {
		return internalError("cannot close memory database")
	}
	file, err = os.Open(path)
	if err != nil {
		return internalError("cannot open memory database for sync")
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return internalError("cannot sync memory database")
	}
	complete = true
	return nil
}

func Open(ctx context.Context, path string, workspaceID int64) (*Store, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, protocol.NewError(protocol.CodeWorkspaceBroken, "workspace memory database is missing or untrusted")
	}
	db, err := openDB(path, "rw")
	if err != nil {
		return nil, protocol.NewError(protocol.CodeWorkspaceBroken, "workspace memory database cannot be opened")
	}
	db.SetMaxOpenConns(1)
	for _, query := range statements.Setup {
		if _, err := db.ExecContext(ctx, query); err != nil {
			db.Close()
			return nil, protocol.NewError(protocol.CodeWorkspaceBroken, "workspace memory database cannot be configured")
		}
	}
	if err := checkHeader(ctx, db, workspaceID); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, wid: workspaceID}, nil
}

func openDB(path, mode string) (*stdsql.DB, error) {
	uri := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=" + mode}).String()
	return stdsql.Open("sqlite", uri)
}

func checkHeader(ctx context.Context, db *stdsql.DB, workspaceID int64) error {
	var applicationID, schemaVersion int
	if err := db.QueryRowContext(ctx, statements.ReadApplicationID).Scan(&applicationID); err != nil || applicationID != statements.MemoryApplicationID {
		return protocol.NewError(protocol.CodeWorkspaceBroken, "memory database application ID is invalid")
	}
	if err := db.QueryRowContext(ctx, statements.ReadSchemaVersion).Scan(&schemaVersion); err != nil {
		return protocol.NewError(protocol.CodeWorkspaceBroken, "memory schema version is unreadable")
	}
	if schemaVersion != statements.SchemaVersion {
		return protocol.NewError(protocol.CodeSchemaMismatch, "memory schema version is not supported")
	}
	var storedWorkspaceID int64
	var recordedVersion int
	if err := db.QueryRowContext(ctx, statements.ReadMemoryInfo).Scan(&storedWorkspaceID, &recordedVersion); err != nil ||
		storedWorkspaceID != workspaceID || recordedVersion != statements.SchemaVersion {
		return protocol.NewError(protocol.CodeWorkspaceBroken, "memory database header does not match workspace")
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Begin(ctx context.Context, readOnly bool) (*Tx, error) {
	tx, err := s.db.BeginTx(ctx, &stdsql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return nil, internalError("cannot begin memory transaction")
	}
	return &Tx{Tx: tx, wid: s.wid}, nil
}

func (tx *Tx) Get(ctx context.Context, memoryID string) (protocol.Memory, error) {
	return scanMemory(tx.QueryRowContext(ctx, statements.SelectMemoryByID, tx.wid, memoryID))
}

func (tx *Tx) Active(ctx context.Context) ([]protocol.Memory, error) {
	rows, err := tx.QueryContext(ctx, statements.SelectActive, tx.wid)
	if err != nil {
		return nil, internalError("cannot search memories")
	}
	defer rows.Close()
	result := make([]protocol.Memory, 0)
	for rows.Next() {
		memory, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, internalError("cannot read memories")
	}
	return result, nil
}

// List pages memories in memory-list order; afterUpdated/afterID are the
// keyset cursor from the previous page (nil for the first page).
func (tx *Tx) List(ctx context.Context, afterUpdated *int64, afterID string, limit int) ([]protocol.Memory, error) {
	var (
		rows *stdsql.Rows
		err  error
	)
	if afterUpdated == nil {
		rows, err = tx.QueryContext(ctx, statements.SelectFirstPage, tx.wid, limit)
	} else {
		rows, err = tx.QueryContext(ctx, statements.SelectNextPage, tx.wid, *afterUpdated, *afterUpdated, afterID, limit)
	}
	if err != nil {
		return nil, internalError("cannot list memories")
	}
	defer rows.Close()
	result := make([]protocol.Memory, 0)
	for rows.Next() {
		memory, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, internalError("cannot read memories")
	}
	return result, nil
}

func (tx *Tx) Insert(ctx context.Context, memory protocol.Memory) error {
	source, _ := json.Marshal(memory.Source)
	metadata, _ := json.Marshal(memory.Metadata)
	_, err := tx.ExecContext(ctx, statements.InsertMemory,
		memory.ID, memory.WorkspaceID, memory.Content, nullable(memory.Type), nullable(memory.Scope),
		string(source), string(metadata), memory.State, memory.Version, nullable(memory.Supersedes),
		nullable(memory.SupersededBy), memory.CreatedAt.Unix(), offsetMinutes(memory.CreatedAt),
		memory.UpdatedAt.Unix(), offsetMinutes(memory.UpdatedAt))
	if err != nil {
		return internalError("cannot insert memory")
	}
	return nil
}

// Archive stores the pre-image of a successful update, supersede, or
// invalidate. It must run in the same transaction as the mutation.
func (tx *Tx) Archive(ctx context.Context, memory protocol.Memory, archivedAt int64) error {
	source, _ := json.Marshal(memory.Source)
	metadata, _ := json.Marshal(memory.Metadata)
	_, err := tx.ExecContext(ctx, statements.InsertHistory,
		memory.WorkspaceID, memory.ID, memory.Version, memory.Content, nullable(memory.Type), nullable(memory.Scope),
		string(source), string(metadata), memory.State, nullable(memory.Supersedes), nullable(memory.SupersededBy),
		memory.CreatedAt.Unix(), offsetMinutes(memory.CreatedAt),
		memory.UpdatedAt.Unix(), offsetMinutes(memory.UpdatedAt), archivedAt)
	if err != nil {
		return internalError("cannot archive memory version")
	}
	return nil
}

func (tx *Tx) Update(ctx context.Context, memory protocol.Memory, oldVersion int64) error {
	source, _ := json.Marshal(memory.Source)
	metadata, _ := json.Marshal(memory.Metadata)
	result, err := tx.ExecContext(ctx, statements.UpdateMemory,
		memory.Content, nullable(memory.Type), nullable(memory.Scope), string(source), string(metadata),
		memory.State, memory.Version, nullable(memory.Supersedes), nullable(memory.SupersededBy),
		memory.UpdatedAt.Unix(), offsetMinutes(memory.UpdatedAt), memory.WorkspaceID, memory.ID, oldVersion)
	if err != nil {
		return internalError("cannot update memory")
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return protocol.NewError(protocol.CodeVersionConflict, "memory version changed")
	}
	return nil
}

// Delete removes the memory's full history, clears the other end of any
// supersede relationship (bumping its version), and deletes the current row.
func (tx *Tx) Delete(ctx context.Context, memoryID string, version int64, now protocol.Timestamp) error {
	if _, err := tx.ExecContext(ctx, statements.DeleteMemoryHistory, tx.wid, memoryID); err != nil {
		return internalError("cannot delete memory history")
	}
	if _, err := tx.ExecContext(ctx, statements.ClearRelationships,
		memoryID, memoryID, now.Unix(), offsetMinutes(now), tx.wid, memoryID, memoryID); err != nil {
		return internalError("cannot clear memory relationship")
	}
	result, err := tx.ExecContext(ctx, statements.DeleteMemory, tx.wid, memoryID, version)
	if err != nil {
		return internalError("cannot delete memory")
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return protocol.NewError(protocol.CodeVersionConflict, "memory version changed")
	}
	return nil
}

func (tx *Tx) Clear(ctx context.Context) (int64, error) {
	result, err := tx.ExecContext(ctx, statements.ClearMemory, tx.wid)
	if err != nil {
		return 0, internalError("cannot clear workspace memories")
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, internalError("cannot count cleared memories")
	}
	if _, err := tx.ExecContext(ctx, statements.ClearMemoryHistory, tx.wid); err != nil {
		return 0, internalError("cannot clear memory history")
	}
	return count, nil
}

type scanner interface{ Scan(...any) error }

func scanMemory(row scanner) (protocol.Memory, error) {
	var memory protocol.Memory
	var memoryType, scope, supersedes, supersededBy stdsql.NullString
	var source, metadata string
	var created, createdOffset, updated, updatedOffset int64
	if err := row.Scan(&memory.ID, &memory.WorkspaceID, &memory.Content, &memoryType, &scope, &source, &metadata,
		&memory.State, &memory.Version, &supersedes, &supersededBy, &created, &createdOffset, &updated, &updatedOffset); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return protocol.Memory{}, protocol.NewError(protocol.CodeMemoryNotFound, "memory not found")
		}
		return protocol.Memory{}, internalError("cannot read memory")
	}
	memory.Type, memory.Scope = pointer(memoryType), pointer(scope)
	memory.Supersedes, memory.SupersededBy = pointer(supersedes), pointer(supersededBy)
	if err := json.Unmarshal([]byte(source), &memory.Source); err != nil || memory.Source == nil {
		return protocol.Memory{}, protocol.NewError(protocol.CodeWorkspaceBroken, "memory source is invalid")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(metadata))
	decoder.UseNumber()
	if err := decoder.Decode(&memory.Metadata); err != nil || memory.Metadata == nil {
		return protocol.Memory{}, protocol.NewError(protocol.CodeWorkspaceBroken, "memory metadata is invalid")
	}
	memory.CreatedAt = timestampAt(created, createdOffset)
	memory.UpdatedAt = timestampAt(updated, updatedOffset)
	if memory.Version < 1 || memory.Version > protocol.MaxSafeInteger ||
		createdOffset < -840 || createdOffset > 840 || updatedOffset < -840 || updatedOffset > 840 ||
		(memory.State != protocol.MemoryActive && memory.State != protocol.MemorySuperseded && memory.State != protocol.MemoryInvalid) {
		return protocol.Memory{}, protocol.NewError(protocol.CodeWorkspaceBroken, "memory row is invalid")
	}
	return memory, nil
}

func nullable(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func pointer(value stdsql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func internalError(message string) error { return protocol.NewError(protocol.CodeInternal, message) }

func timestampAt(epochSeconds, offsetMinutes int64) protocol.Timestamp {
	location := time.FixedZone("", int(offsetMinutes)*60)
	return protocol.Timestamp{Time: time.Unix(epochSeconds, 0).In(location)}
}

func offsetMinutes(value protocol.Timestamp) int64 {
	_, offset := value.Zone()
	return int64(offset / 60)
}
