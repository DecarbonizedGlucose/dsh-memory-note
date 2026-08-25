// Package memory stores one workspace's memories and their archived
// historical versions.
package memory

import (
	"bytes"
	"context"
	stdsql "database/sql"
	"encoding/json"
	"errors"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/statements"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/storage"
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
	if err := storage.SyncFile(path); err != nil {
		return err
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
	return stdsql.Open("sqlite", storage.SQLiteURI(path, mode))
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

type SearchOptions struct {
	Query         string
	Kinds         []string
	Labels        []string
	CreatedAfter  *protocol.Timestamp
	CreatedBefore *protocol.Timestamp
	UpdatedAfter  *protocol.Timestamp
	UpdatedBefore *protocol.Timestamp
	Limit         int
}

type SearchCandidate struct {
	Memory protocol.Memory
	Rank   float64
}

// Search reads a bounded candidate set. FTS MATCH is used when Query is set;
// exact filters are ordinary bound SQL conditions in either mode.
func (tx *Tx) Search(ctx context.Context, options SearchOptions) ([]SearchCandidate, error) {
	query := statements.MemorySearch(
		options.Query != "", len(options.Kinds), len(options.Labels),
		options.CreatedAfter != nil, options.CreatedBefore != nil,
		options.UpdatedAfter != nil, options.UpdatedBefore != nil,
	)
	args := make([]any, 0, 3+len(options.Kinds)+len(options.Labels)+4)
	if options.Query != "" {
		args = append(args, options.Query)
	}
	args = append(args, tx.wid)
	for _, kind := range options.Kinds {
		args = append(args, kind)
	}
	for _, label := range options.Labels {
		args = append(args, label)
	}
	for _, value := range []*protocol.Timestamp{options.CreatedAfter, options.CreatedBefore, options.UpdatedAfter, options.UpdatedBefore} {
		if value != nil {
			args = append(args, value.Unix())
		}
	}
	args = append(args, options.Limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, internalError("cannot search memories")
	}
	defer rows.Close()
	result := make([]SearchCandidate, 0)
	for rows.Next() {
		var rank float64
		memory, err := scanMemoryWithExtra(rows, &rank)
		if err != nil {
			return nil, err
		}
		result = append(result, SearchCandidate{Memory: memory, Rank: rank})
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
		memory.ID, memory.WorkspaceID, memory.Content, memory.Kind, nullable(memory.Label), branchesJSON(memory.Branches),
		string(source), string(metadata), memory.State, memory.Version, nullable(memory.Supersedes),
		nullable(memory.SupersededBy), memory.CreatedAt.Unix(), offsetMinutes(memory.CreatedAt),
		memory.UpdatedAt.Unix(), offsetMinutes(memory.UpdatedAt))
	if err != nil {
		return internalError("cannot insert memory")
	}
	return nil
}

// Archive stores the pre-image of a successful update, supersede, or
// invalidate, tagged with the action that produced it. It must run in the
// same transaction as the mutation.
func (tx *Tx) Archive(ctx context.Context, memory protocol.Memory, action string, archivedAt int64) error {
	source, _ := json.Marshal(memory.Source)
	metadata, _ := json.Marshal(memory.Metadata)
	_, err := tx.ExecContext(ctx, statements.InsertHistory,
		memory.WorkspaceID, memory.ID, memory.Version, action, memory.Content, memory.Kind, nullable(memory.Label), branchesJSON(memory.Branches),
		string(source), string(metadata), memory.State, nullable(memory.Supersedes), nullable(memory.SupersededBy),
		memory.CreatedAt.Unix(), offsetMinutes(memory.CreatedAt),
		memory.UpdatedAt.Unix(), offsetMinutes(memory.UpdatedAt), archivedAt)
	if err != nil {
		return internalError("cannot archive memory version")
	}
	return nil
}

// InsertEvent writes one lightweight change-log row in the same transaction
// as its mutation. It carries no content.
func (tx *Tx) InsertEvent(ctx context.Context, workspaceID int64, memoryID, action string, fromVersion, toVersion *int64, relatedMemoryID, reason string, at time.Time) error {
	_, err := tx.ExecContext(ctx, statements.InsertEvent,
		workspaceID, memoryID, action, nullableInt(fromVersion), nullableInt(toVersion),
		nullable(&relatedMemoryID), nullable(&reason), at.Unix(), offsetMinutes(protocol.Timestamp{Time: at}))
	if err != nil {
		return internalError("cannot record memory event")
	}
	return nil
}

// Version reads one exact historical version from the archive. It returns
// memory_not_found when that version never existed.
func (tx *Tx) Version(ctx context.Context, workspaceID int64, memoryID string, version int64) (protocol.Memory, error) {
	return scanMemory(tx.QueryRowContext(ctx, statements.SelectHistoryVersion, workspaceID, memoryID, version))
}

// HistoryItem is one row of a memory's version history.
type HistoryItem struct {
	Version    int64
	Action     string
	State      string
	UpdatedAt  protocol.Timestamp
	ArchivedAt *protocol.Timestamp
}

// Versions lists the archived versions of one memory, ascending.
func (tx *Tx) Versions(ctx context.Context, workspaceID int64, memoryID string) ([]HistoryItem, error) {
	rows, err := tx.QueryContext(ctx, statements.SelectHistoryList, workspaceID, memoryID)
	if err != nil {
		return nil, internalError("cannot list memory versions")
	}
	defer rows.Close()
	result := make([]HistoryItem, 0)
	for rows.Next() {
		var item HistoryItem
		var updated, updatedOffset, archived int64
		if err := rows.Scan(&item.Version, &item.Action, &item.State, &updated, &updatedOffset, &archived); err != nil {
			return nil, internalError("cannot read memory version")
		}
		item.UpdatedAt = timestampAt(updated, updatedOffset)
		archivedAt := timestampAt(archived, 0)
		item.ArchivedAt = &archivedAt
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, internalError("cannot read memory versions")
	}
	return result, nil
}

// LatestAction returns the action of the newest event for a memory, used as
// the current version's action in memory-history.
func (tx *Tx) LatestAction(ctx context.Context, workspaceID int64, memoryID string) (string, error) {
	var action string
	err := tx.QueryRowContext(ctx, statements.SelectLatestEventAction, workspaceID, memoryID).Scan(&action)
	if errors.Is(err, stdsql.ErrNoRows) {
		return "create", nil
	}
	if err != nil {
		return "", internalError("cannot read memory event")
	}
	return action, nil
}

func (tx *Tx) Update(ctx context.Context, memory protocol.Memory, oldVersion int64) error {
	source, _ := json.Marshal(memory.Source)
	metadata, _ := json.Marshal(memory.Metadata)
	result, err := tx.ExecContext(ctx, statements.UpdateMemory,
		memory.Content, memory.Kind, nullable(memory.Label), branchesJSON(memory.Branches), string(source), string(metadata),
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

// Delete removes the memory's full history, its event rows, clears the other
// end of any supersede relationship (bumping its version), and deletes the
// current row. A delete is a total erasure: no audit row remains.
func (tx *Tx) Delete(ctx context.Context, memoryID string, version int64, now protocol.Timestamp) error {
	if _, err := tx.ExecContext(ctx, statements.DeleteMemoryHistory, tx.wid, memoryID); err != nil {
		return internalError("cannot delete memory history")
	}
	if _, err := tx.ExecContext(ctx, statements.DeleteMemoryEvents, tx.wid, memoryID); err != nil {
		return internalError("cannot delete memory events")
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
	if _, err := tx.ExecContext(ctx, statements.ClearMemoryEvents, tx.wid); err != nil {
		return 0, internalError("cannot clear memory events")
	}
	return count, nil
}

type scanner interface{ Scan(...any) error }

func scanMemory(row scanner) (protocol.Memory, error) {
	return scanMemoryWithExtra(row)
}

func scanMemoryWithExtra(row scanner, extra ...any) (protocol.Memory, error) {
	var memory protocol.Memory
	var label, branches, supersedes, supersededBy stdsql.NullString
	var source, metadata string
	var created, createdOffset, updated, updatedOffset int64
	targets := []any{&memory.ID, &memory.WorkspaceID, &memory.Content, &memory.Kind, &label, &branches, &source, &metadata,
		&memory.State, &memory.Version, &supersedes, &supersededBy, &created, &createdOffset, &updated, &updatedOffset}
	targets = append(targets, extra...)
	if err := row.Scan(targets...); err != nil {
		if errors.Is(err, stdsql.ErrNoRows) {
			return protocol.Memory{}, protocol.NewError(protocol.CodeMemoryNotFound, "memory not found")
		}
		return protocol.Memory{}, internalError("cannot read memory")
	}
	memory.Label = pointer(label)
	memory.Supersedes, memory.SupersededBy = pointer(supersedes), pointer(supersededBy)
	if branches.Valid {
		if err := json.Unmarshal([]byte(branches.String), &memory.Branches); err != nil {
			return protocol.Memory{}, protocol.NewError(protocol.CodeWorkspaceBroken, "memory branches are invalid")
		}
	}
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
		(memory.State != protocol.MemoryActive && memory.State != protocol.MemorySuperseded && memory.State != protocol.MemoryInvalid) ||
		!protocol.ValidMemoryKind(memory.Kind) {
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

func nullableInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

// branchesJSON serializes a branch list for the branches_json column: nil or
// empty means "visible on all branches" and is stored as SQL NULL.
func branchesJSON(branches []string) any {
	if len(branches) == 0 {
		return nil
	}
	raw, _ := json.Marshal(branches)
	return string(raw)
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
