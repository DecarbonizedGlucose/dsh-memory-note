package memory

import (
	"context"
	"crypto/rand"
	stdsql "database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	_ "modernc.org/sqlite"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
	storesql "github.com/DecarbonizedGlucose/dsh-memory-note/internal/store/sql"
)

type Store struct {
	db  *stdsql.DB
	wid int64
}

func Open(ctx context.Context, path string, workspaceID int64) (*Store, error) {
	dsn := (&url.URL{Scheme: "file", Path: path}).String()
	db, err := stdsql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open memory db: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, wid: workspaceID}
	if err := store.init(ctx); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) init(ctx context.Context) error {
	queries := append(append([]string{}, storesql.Setup...), storesql.MemorySchema...)
	for _, query := range queries {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("init memory db: %w", err)
		}
	}
	return nil
}

func (s *Store) Create(ctx context.Context, request protocol.MemoryCreateRequest) (protocol.Memory, error) {
	input := request.MemoryCreate
	input, err := cleanCreate(input)
	if err != nil {
		return protocol.Memory{}, err
	}
	id, err := newID()
	if err != nil {
		return protocol.Memory{}, err
	}
	now := time.Now().UTC()
	item := protocol.Memory{
		ID: id, WorkspaceID: s.wid, Content: input.Content, Type: input.Type, Scope: input.Scope,
		Source: input.Source, Metadata: input.Metadata,
		State: protocol.MemoryActive, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := insert(ctx, s.db, item); err != nil {
		return protocol.Memory{}, err
	}
	return item, nil
}

func (s *Store) Get(ctx context.Context, request protocol.MemoryGetRequest) (protocol.Memory, error) {
	if strings.TrimSpace(request.MemoryID) == "" {
		return protocol.Memory{}, fmt.Errorf("%w: memory_id is required", protocol.ErrInvalidRequest)
	}
	return get(ctx, s.db, s.wid, request.MemoryID)
}

func (s *Store) Update(ctx context.Context, request protocol.MemoryUpdateRequest) (protocol.Memory, error) {
	if request.ExpectedVersion < 1 || strings.TrimSpace(request.MemoryID) == "" {
		return protocol.Memory{}, fmt.Errorf("%w: memory_id and expected_version are required", protocol.ErrInvalidRequest)
	}
	if request.Content == nil && request.Type == nil && request.Scope == nil && request.Source == nil && request.Metadata == nil {
		return protocol.Memory{}, fmt.Errorf("%w: at least one update field is required", protocol.ErrInvalidRequest)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.Memory{}, err
	}
	defer tx.Rollback()
	current, err := get(ctx, tx, s.wid, request.MemoryID)
	if err != nil {
		return protocol.Memory{}, err
	}
	if current.State != protocol.MemoryActive {
		return protocol.Memory{}, protocol.ErrInvalidMemoryState
	}
	if current.Version != request.ExpectedVersion {
		return protocol.Memory{}, protocol.ErrVersionConflict
	}
	if request.Content != nil {
		current.Content = *request.Content
	}
	if request.Type != nil {
		current.Type = *request.Type
	}
	if request.Scope != nil {
		current.Scope = *request.Scope
	}
	if request.Source != nil {
		current.Source = *request.Source
	}
	if request.Metadata != nil {
		current.Metadata = *request.Metadata
	}
	cleaned, err := cleanCreate(protocol.MemoryCreate{
		Content: current.Content, Type: current.Type, Scope: current.Scope,
		Source: current.Source, Metadata: current.Metadata,
	})
	if err != nil {
		return protocol.Memory{}, err
	}
	current.Content, current.Type, current.Scope = cleaned.Content, cleaned.Type, cleaned.Scope
	current.Source, current.Metadata = cleaned.Source, cleaned.Metadata
	current.Version++
	current.UpdatedAt = time.Now().UTC()
	if err := updateRow(ctx, tx, current, request.ExpectedVersion); err != nil {
		return protocol.Memory{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.Memory{}, err
	}
	return current, nil
}

func (s *Store) Supersede(ctx context.Context, request protocol.MemorySupersedeRequest) (protocol.MemorySupersedeResponse, error) {
	if request.ExpectedVersion < 1 || strings.TrimSpace(request.MemoryID) == "" {
		return protocol.MemorySupersedeResponse{}, fmt.Errorf("%w: memory_id and expected_version are required", protocol.ErrInvalidRequest)
	}
	input, err := cleanCreate(request.New)
	if err != nil {
		return protocol.MemorySupersedeResponse{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.MemorySupersedeResponse{}, err
	}
	defer tx.Rollback()
	old, err := get(ctx, tx, s.wid, request.MemoryID)
	if err != nil {
		return protocol.MemorySupersedeResponse{}, err
	}
	if old.State != protocol.MemoryActive {
		return protocol.MemorySupersedeResponse{}, protocol.ErrInvalidMemoryState
	}
	if old.Version != request.ExpectedVersion {
		return protocol.MemorySupersedeResponse{}, protocol.ErrVersionConflict
	}
	newID, err := newID()
	if err != nil {
		return protocol.MemorySupersedeResponse{}, err
	}
	now := time.Now().UTC()
	next := protocol.Memory{
		ID: newID, WorkspaceID: s.wid, Content: input.Content, Type: input.Type, Scope: input.Scope,
		Source: input.Source, Metadata: input.Metadata,
		State: protocol.MemoryActive, Version: 1, Supersedes: old.ID, CreatedAt: now, UpdatedAt: now,
	}
	if err := insert(ctx, tx, next); err != nil {
		return protocol.MemorySupersedeResponse{}, err
	}
	old.State, old.SupersededBy, old.Version, old.UpdatedAt = protocol.MemorySuperseded, next.ID, old.Version+1, now
	if err := updateRow(ctx, tx, old, request.ExpectedVersion); err != nil {
		return protocol.MemorySupersedeResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.MemorySupersedeResponse{}, err
	}
	return protocol.MemorySupersedeResponse{Old: old, New: next}, nil
}

func (s *Store) Invalidate(ctx context.Context, request protocol.MemoryInvalidateRequest) (protocol.Memory, error) {
	if request.ExpectedVersion < 1 || strings.TrimSpace(request.MemoryID) == "" {
		return protocol.Memory{}, fmt.Errorf("%w: memory_id and expected_version are required", protocol.ErrInvalidRequest)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.Memory{}, err
	}
	defer tx.Rollback()
	current, err := get(ctx, tx, s.wid, request.MemoryID)
	if err != nil {
		return protocol.Memory{}, err
	}
	if current.State != protocol.MemoryActive {
		return protocol.Memory{}, protocol.ErrInvalidMemoryState
	}
	if current.Version != request.ExpectedVersion {
		return protocol.Memory{}, protocol.ErrVersionConflict
	}
	current.State, current.Version, current.UpdatedAt = protocol.MemoryInvalid, current.Version+1, time.Now().UTC()
	if err := updateRow(ctx, tx, current, request.ExpectedVersion); err != nil {
		return protocol.Memory{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.Memory{}, err
	}
	return current, nil
}

func (s *Store) Delete(ctx context.Context, request protocol.MemoryDeleteRequest) error {
	if request.ExpectedVersion < 1 || strings.TrimSpace(request.MemoryID) == "" {
		return fmt.Errorf("%w: memory_id and expected_version are required", protocol.ErrInvalidRequest)
	}
	result, err := s.db.ExecContext(ctx, storesql.DeleteMemory, s.wid, request.MemoryID, request.ExpectedVersion)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 1 {
		return nil
	}
	if _, err := get(ctx, s.db, s.wid, request.MemoryID); err != nil {
		return err
	}
	return protocol.ErrVersionConflict
}

func (s *Store) Clear(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, storesql.DeleteWorkspaceMemories, s.wid)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) Search(ctx context.Context, request protocol.MemorySearchRequest) ([]protocol.MemorySearchHit, error) {
	terms := words(request.Query)
	if len(terms) == 0 && !hasFilter(request.Filter) {
		return nil, fmt.Errorf("%w: query or filter is required", protocol.ErrInvalidRequest)
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}
	query, args := storesql.SearchMemory(storesql.SearchParams{
		WorkspaceID:   s.wid,
		Terms:         terms,
		Types:         request.Filter.Types,
		Scopes:        request.Filter.Scopes,
		State:         string(protocol.MemoryActive),
		CreatedAfter:  request.Filter.CreatedAfter,
		CreatedBefore: request.Filter.CreatedBefore,
		UpdatedAfter:  request.Filter.UpdatedAfter,
		UpdatedBefore: request.Filter.UpdatedBefore,
		Limit:         limit,
	})
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := make([]protocol.MemorySearchHit, 0, limit)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		score := wordScore(item, terms)
		hits = append(hits, protocol.MemorySearchHit{
			ID: item.ID, Type: item.Type, Scope: item.Scope, State: item.State,
			Version: item.Version, Snippet: snippet(item.Content), Score: score, UpdatedAt: item.UpdatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].UpdatedAt.After(hits[j].UpdatedAt)
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (stdsql.Result, error)
	QueryRowContext(context.Context, string, ...any) *stdsql.Row
}

func insert(ctx context.Context, db sqlRunner, item protocol.Memory) error {
	source, _ := json.Marshal(item.Source)
	metadata, _ := json.Marshal(item.Metadata)
	_, err := db.ExecContext(ctx, storesql.InsertMemory,
		item.ID, item.WorkspaceID, item.Content, item.Type, item.Scope, string(source), string(metadata),
		item.State, item.Version, item.Supersedes, item.SupersededBy, stamp(item.CreatedAt), stamp(item.UpdatedAt))
	return err
}

func updateRow(ctx context.Context, db sqlRunner, item protocol.Memory, oldVersion int) error {
	source, _ := json.Marshal(item.Source)
	metadata, _ := json.Marshal(item.Metadata)
	result, err := db.ExecContext(ctx, storesql.UpdateMemory,
		item.Content, item.Type, item.Scope, string(source), string(metadata), item.State,
		item.Version, item.Supersedes, item.SupersededBy, stamp(item.UpdatedAt), item.WorkspaceID, item.ID, oldVersion)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return protocol.ErrVersionConflict
	}
	return nil
}

func get(ctx context.Context, db sqlRunner, wid int64, id string) (protocol.Memory, error) {
	return scan(db.QueryRowContext(ctx, storesql.SelectMemoryByID, wid, id))
}

type scanner interface{ Scan(...any) error }

func scan(row scanner) (protocol.Memory, error) {
	var item protocol.Memory
	var source, metadata, state, created, updated string
	err := row.Scan(&item.ID, &item.WorkspaceID, &item.Content, &item.Type, &item.Scope, &source,
		&metadata, &state, &item.Version, &item.Supersedes, &item.SupersededBy, &created, &updated)
	if errors.Is(err, stdsql.ErrNoRows) {
		return protocol.Memory{}, protocol.ErrMemoryNotFound
	}
	if err != nil {
		return protocol.Memory{}, err
	}
	item.State = protocol.MemoryState(state)
	_ = json.Unmarshal([]byte(source), &item.Source)
	_ = json.Unmarshal([]byte(metadata), &item.Metadata)
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return item, nil
}

func cleanCreate(input protocol.MemoryCreate) (protocol.MemoryCreate, error) {
	input.Content = strings.TrimSpace(strings.ReplaceAll(input.Content, "\r\n", "\n"))
	input.Type = strings.TrimSpace(input.Type)
	input.Scope = strings.TrimSpace(input.Scope)
	input.Source = cleanStrings(input.Source)
	if input.Content == "" || !utf8.ValidString(input.Content) || len(input.Content) > 64*1024 {
		return protocol.MemoryCreate{}, fmt.Errorf("%w: content must be valid UTF-8 and at most 64 KiB", protocol.ErrInvalidRequest)
	}
	if len(input.Type) > 128 || len(input.Scope) > 256 {
		return protocol.MemoryCreate{}, fmt.Errorf("%w: type or scope is too long", protocol.ErrInvalidRequest)
	}
	metadata, err := json.Marshal(input.Metadata)
	if err != nil || len(metadata) > 16*1024 {
		return protocol.MemoryCreate{}, fmt.Errorf("%w: invalid or oversized metadata", protocol.ErrInvalidRequest)
	}
	return input, nil
}

func words(value string) []string {
	parts := strings.FieldsFunc(strings.ToLower(value), func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsNumber(char)
	})
	return cleanStrings(parts)
}

func cleanStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
		if len(result) == 64 {
			break
		}
	}
	return result
}

func hasFilter(filter protocol.MemoryFilter) bool {
	return len(filter.Types) > 0 || len(filter.Scopes) > 0 ||
		filter.CreatedAfter != nil || filter.CreatedBefore != nil || filter.UpdatedAfter != nil || filter.UpdatedBefore != nil
}

func wordScore(item protocol.Memory, terms []string) float64 {
	if len(terms) == 0 {
		return 0
	}
	text := strings.ToLower(item.Content + " " + item.Type + " " + item.Scope)
	matched := 0
	for _, term := range terms {
		if strings.Contains(text, term) {
			matched++
		}
	}
	return float64(matched) / float64(len(terms))
}

func snippet(content string) string {
	const maxRunes = 240
	value := []rune(content)
	if len(value) <= maxRunes {
		return content
	}
	return string(value[:maxRunes]) + "…"
}

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("create memory id: %w", err)
	}
	return "mem_" + hex.EncodeToString(raw[:]), nil
}

func stamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
