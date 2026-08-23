package command

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	memorystore "github.com/DecarbonizedGlucose/dsh-memory-note/internal/memory"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

func memorySearch(ctx context.Context, storeRoot, sessionID, raw string) (protocol.MemorySearchData, error) {
	var request protocol.MemorySearchRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemorySearchData{}, err
	}
	terms, types, scopes, limit, err := checkSearch(request)
	if err != nil {
		return protocol.MemorySearchData{}, err
	}

	run, err := begin(ctx, storeRoot, sessionID, request.WorkspaceID, lockRead, true)
	if err != nil {
		return protocol.MemorySearchData{}, err
	}
	defer run.Release()
	store, err := run.memoryStore(ctx, request.WorkspaceID)
	if err != nil {
		return protocol.MemorySearchData{}, err
	}
	defer store.Close()
	tx, err := store.Begin(ctx, true)
	if err != nil {
		return protocol.MemorySearchData{}, err
	}
	defer tx.Rollback()
	memories, err := tx.Active(ctx)
	if err != nil {
		return protocol.MemorySearchData{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.MemorySearchData{}, protocol.NewError(protocol.CodeInternal, "cannot finish memory search")
	}

	hits := make([]protocol.SearchHit, 0)
	for _, item := range memories {
		if !matchesFilter(item, request.Filter, types, scopes) {
			continue
		}
		score, matched := matchTerms(item, terms)
		if len(terms) != 0 && !matched {
			continue
		}
		hits = append(hits, protocol.SearchHit{
			ID: item.ID, Type: item.Type, Scope: item.Scope, Version: item.Version,
			Snippet: snippet(item.Content), Score: score, UpdatedAt: item.UpdatedAt,
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if !hits[i].UpdatedAt.Equal(hits[j].UpdatedAt.Time) {
			return hits[i].UpdatedAt.After(hits[j].UpdatedAt.Time)
		}
		return hits[i].ID < hits[j].ID
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	if err := run.Commit(ctx); err != nil {
		return protocol.MemorySearchData{}, err
	}
	return protocol.MemorySearchData{Memories: hits}, nil
}

func memoryList(ctx context.Context, storeRoot, sessionID, raw string) (protocol.MemoryListData, error) {
	var request protocol.MemoryListRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryListData{}, err
	}
	if err := validWorkspaceID(request.WorkspaceID); err != nil {
		return protocol.MemoryListData{}, err
	}
	limit := 50
	if request.Limit != nil {
		limit = *request.Limit
	}
	if limit < 1 || limit > 200 {
		return protocol.MemoryListData{}, protocol.Invalid("limit must be between 1 and 200")
	}

	run, err := begin(ctx, storeRoot, sessionID, request.WorkspaceID, lockRead, true)
	if err != nil {
		return protocol.MemoryListData{}, err
	}
	defer run.Release()
	// The cursor is authenticated with the per-HOME key, so it must be read
	// from meta.db before any cursor is decoded or encoded.
	cursorKey, err := run.metaStore.CursorKey(ctx)
	if err != nil {
		return protocol.MemoryListData{}, err
	}

	var cursor *listCursor
	if request.Cursor != nil {
		decoded, err := decodeCursor(cursorKey, *request.Cursor)
		if err != nil {
			return protocol.MemoryListData{}, err
		}
		cursor = decoded
	}

	store, err := run.memoryStore(ctx, request.WorkspaceID)
	if err != nil {
		return protocol.MemoryListData{}, err
	}
	defer store.Close()
	tx, err := store.Begin(ctx, true)
	if err != nil {
		return protocol.MemoryListData{}, err
	}
	defer tx.Rollback()

	var afterUpdated *int64
	var afterID string
	if cursor != nil {
		value := cursor.UpdatedAt
		afterUpdated = &value
		afterID = cursor.MemoryID
	}
	rows, err := tx.List(ctx, afterUpdated, afterID, limit+1)
	if err != nil {
		return protocol.MemoryListData{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.MemoryListData{}, protocol.NewError(protocol.CodeInternal, "cannot finish memory list")
	}

	data := protocol.MemoryListData{Memories: make([]protocol.ListItem, 0, limit)}
	hasMore := false
	if len(rows) > limit {
		hasMore = true
		rows = rows[:limit]
	}
	for _, item := range rows {
		data.Memories = append(data.Memories, protocol.ListItem{
			ID: item.ID, Type: item.Type, Scope: item.Scope, State: item.State, Version: item.Version,
			Supersedes: item.Supersedes, SupersededBy: item.SupersededBy,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		})
	}
	if hasMore {
		last := rows[len(rows)-1]
		next := encodeCursor(cursorKey, last.UpdatedAt.Unix(), last.ID)
		data.NextCursor = &next
	}
	if err := run.Commit(ctx); err != nil {
		return protocol.MemoryListData{}, err
	}
	return data, nil
}

type listCursor struct {
	UpdatedAt int64  `json:"updated_at"`
	MemoryID  string `json:"memory_id"`
}

// encodeCursor signs the keyset payload with the per-HOME HMAC key so that a
// caller cannot forge or alter a cursor and silently change the pagination
// starting point.
func encodeCursor(key []byte, updatedAt int64, memoryID string) string {
	raw, _ := json.Marshal(listCursor{UpdatedAt: updatedAt, MemoryID: memoryID})
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// decodeCursor verifies the HMAC before decoding the payload. Any modification
// to the payload fails verification and returns invalid_request.
func decodeCursor(key []byte, value string) (*listCursor, error) {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return nil, protocol.Invalid("cursor is invalid")
	}
	payload, signature := parts[0], parts[1]
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(provided, mac.Sum(nil)) {
		return nil, protocol.Invalid("cursor is invalid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, protocol.Invalid("cursor is invalid")
	}
	var cursor listCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.MemoryID == "" {
		return nil, protocol.Invalid("cursor is invalid")
	}
	return &cursor, nil
}

func memoryGet(ctx context.Context, storeRoot, sessionID, raw string) (protocol.MemoryGetData, error) {
	var request protocol.MemoryGetRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryGetData{}, err
	}
	if err := checkMemoryRef(request.WorkspaceID, request.MemoryID); err != nil {
		return protocol.MemoryGetData{}, err
	}
	item, run, store, tx, err := getMemory(ctx, storeRoot, sessionID, request.WorkspaceID, request.MemoryID, true)
	if err != nil {
		return protocol.MemoryGetData{}, err
	}
	defer run.Release()
	defer store.Close()
	defer tx.Rollback()
	if err := tx.Commit(); err != nil {
		return protocol.MemoryGetData{}, protocol.NewError(protocol.CodeInternal, "cannot finish memory read")
	}
	if err := run.Commit(ctx); err != nil {
		return protocol.MemoryGetData{}, err
	}
	return protocol.MemoryGetData{MemoryResult: protocol.MemoryResult{Memory: item}}, nil
}

func memoryCreate(ctx context.Context, storeRoot, sessionID, raw string) (protocol.MemoryCreateData, error) {
	var request protocol.MemoryCreateRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryCreateData{}, err
	}
	if err := validWorkspaceID(request.WorkspaceID); err != nil {
		return protocol.MemoryCreateData{}, err
	}
	input, err := cleanInput(request.MemoryInput)
	if err != nil {
		return protocol.MemoryCreateData{}, err
	}
	run, store, tx, err := beginMemory(ctx, storeRoot, sessionID, request.WorkspaceID, false)
	if err != nil {
		return protocol.MemoryCreateData{}, err
	}
	defer run.Release()
	defer store.Close()
	defer tx.Rollback()
	item, err := newMemory(request.WorkspaceID, input, nil)
	if err != nil {
		return protocol.MemoryCreateData{}, err
	}
	if err := tx.Insert(ctx, item); err != nil {
		return protocol.MemoryCreateData{}, err
	}
	if err := commitMemory(ctx, run, tx); err != nil {
		return protocol.MemoryCreateData{}, err
	}
	return protocol.MemoryCreateData{MemoryResult: protocol.MemoryResult{Memory: item}}, nil
}

func memoryUpdate(ctx context.Context, storeRoot, sessionID, raw string) (protocol.MemoryUpdateData, error) {
	var request protocol.MemoryUpdateRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryUpdateData{}, err
	}
	if err := checkTarget(request.WorkspaceID, request.MemoryID, request.ExpectedVersion); err != nil {
		return protocol.MemoryUpdateData{}, err
	}
	if request.Content == nil && request.Type == nil && request.Scope == nil && request.Source == nil && request.Metadata == nil {
		return protocol.MemoryUpdateData{}, protocol.Invalid("at least one memory field is required")
	}
	if err := cleanUpdate(&request); err != nil {
		return protocol.MemoryUpdateData{}, err
	}
	item, run, store, tx, err := getMemory(ctx, storeRoot, sessionID, request.WorkspaceID, request.MemoryID, false)
	if err != nil {
		return protocol.MemoryUpdateData{}, err
	}
	defer run.Release()
	defer store.Close()
	defer tx.Rollback()
	if err := checkWritable(item, request.ExpectedVersion); err != nil {
		return protocol.MemoryUpdateData{}, err
	}
	previous := item
	applyUpdate(&item, request)
	item.Version++
	item.UpdatedAt = now()
	if err := tx.Archive(ctx, previous, time.Now().Unix()); err != nil {
		return protocol.MemoryUpdateData{}, err
	}
	if err := tx.Update(ctx, item, previous.Version); err != nil {
		return protocol.MemoryUpdateData{}, err
	}
	if err := commitMemory(ctx, run, tx); err != nil {
		return protocol.MemoryUpdateData{}, err
	}
	return protocol.MemoryUpdateData{MemoryResult: protocol.MemoryResult{Memory: item}}, nil
}

func memorySupersede(ctx context.Context, storeRoot, sessionID, raw string) (protocol.MemorySupersedeData, error) {
	var request protocol.MemorySupersedeRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	if err := checkTarget(request.WorkspaceID, request.MemoryID, request.ExpectedVersion); err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	input, err := cleanInput(request.New)
	if err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	old, run, store, tx, err := getMemory(ctx, storeRoot, sessionID, request.WorkspaceID, request.MemoryID, false)
	if err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	defer run.Release()
	defer store.Close()
	defer tx.Rollback()
	if err := checkWritable(old, request.ExpectedVersion); err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	newItem, err := newMemory(request.WorkspaceID, input, &old.ID)
	if err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	previous := old
	old.State = protocol.MemorySuperseded
	old.Version++
	old.SupersededBy = &newItem.ID
	old.UpdatedAt = newItem.CreatedAt
	if err := tx.Archive(ctx, previous, time.Now().Unix()); err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	if err := tx.Insert(ctx, newItem); err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	if err := tx.Update(ctx, old, previous.Version); err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	if err := commitMemory(ctx, run, tx); err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	return protocol.MemorySupersedeData{Old: old, New: newItem}, nil
}

func memoryInvalidate(ctx context.Context, storeRoot, sessionID, raw string) (protocol.MemoryInvalidateData, error) {
	var request protocol.MemoryInvalidateRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryInvalidateData{}, err
	}
	if err := checkTarget(request.WorkspaceID, request.MemoryID, request.ExpectedVersion); err != nil {
		return protocol.MemoryInvalidateData{}, err
	}
	item, run, store, tx, err := getMemory(ctx, storeRoot, sessionID, request.WorkspaceID, request.MemoryID, false)
	if err != nil {
		return protocol.MemoryInvalidateData{}, err
	}
	defer run.Release()
	defer store.Close()
	defer tx.Rollback()
	if err := checkWritable(item, request.ExpectedVersion); err != nil {
		return protocol.MemoryInvalidateData{}, err
	}
	previous := item
	item.State = protocol.MemoryInvalid
	item.Version++
	item.UpdatedAt = now()
	if err := tx.Archive(ctx, previous, time.Now().Unix()); err != nil {
		return protocol.MemoryInvalidateData{}, err
	}
	if err := tx.Update(ctx, item, previous.Version); err != nil {
		return protocol.MemoryInvalidateData{}, err
	}
	if err := commitMemory(ctx, run, tx); err != nil {
		return protocol.MemoryInvalidateData{}, err
	}
	return protocol.MemoryInvalidateData{MemoryResult: protocol.MemoryResult{Memory: item}}, nil
}

func memoryDelete(ctx context.Context, storeRoot, sessionID, raw string) (protocol.MemoryDeleteData, error) {
	var request protocol.MemoryDeleteRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryDeleteData{}, err
	}
	if err := checkTarget(request.WorkspaceID, request.MemoryID, request.ExpectedVersion); err != nil {
		return protocol.MemoryDeleteData{}, err
	}
	item, run, store, tx, err := getMemory(ctx, storeRoot, sessionID, request.WorkspaceID, request.MemoryID, false)
	if err != nil {
		return protocol.MemoryDeleteData{}, err
	}
	defer run.Release()
	defer store.Close()
	defer tx.Rollback()
	if item.Version != request.ExpectedVersion {
		return protocol.MemoryDeleteData{}, protocol.NewError(protocol.CodeVersionConflict, "memory version does not match")
	}
	if err := tx.Delete(ctx, item.ID, item.Version, now()); err != nil {
		return protocol.MemoryDeleteData{}, err
	}
	if err := commitMemory(ctx, run, tx); err != nil {
		return protocol.MemoryDeleteData{}, err
	}
	return protocol.MemoryDeleteData{Deleted: true}, nil
}

func beginMemory(ctx context.Context, storeRoot, sessionID string, workspaceID int64, readOnly bool) (*execution, *memorystore.Store, *memorystore.Tx, error) {
	lockMode := lockRead
	if !readOnly {
		lockMode = lockWrite
	}
	run, err := begin(ctx, storeRoot, sessionID, workspaceID, lockMode, true)
	if err != nil {
		return nil, nil, nil, err
	}
	if !readOnly {
		if err := run.verifyWriteLock(ctx); err != nil {
			run.Release()
			return nil, nil, nil, err
		}
	}
	store, err := run.memoryStore(ctx, workspaceID)
	if err != nil {
		run.Release()
		return nil, nil, nil, err
	}
	tx, err := store.Begin(ctx, readOnly)
	if err != nil {
		store.Close()
		run.Release()
		return nil, nil, nil, err
	}
	return run, store, tx, nil
}

func getMemory(ctx context.Context, storeRoot, sessionID string, workspaceID int64, memoryID string, readOnly bool) (protocol.Memory, *execution, *memorystore.Store, *memorystore.Tx, error) {
	run, store, tx, err := beginMemory(ctx, storeRoot, sessionID, workspaceID, readOnly)
	if err != nil {
		return protocol.Memory{}, nil, nil, nil, err
	}
	item, err := tx.Get(ctx, memoryID)
	if err != nil {
		tx.Rollback()
		store.Close()
		run.Release()
		return protocol.Memory{}, nil, nil, nil, err
	}
	return item, run, store, tx, nil
}

func commitMemory(ctx context.Context, run *execution, tx *memorystore.Tx) error {
	if err := tx.Commit(); err != nil {
		return protocol.NewError(protocol.CodeInternal, "cannot commit memory transaction")
	}
	return run.Commit(ctx)
}

func newMemory(workspaceID int64, input protocol.MemoryInput, supersedes *string) (protocol.Memory, error) {
	id, err := newMemoryID()
	if err != nil {
		return protocol.Memory{}, err
	}
	created := now()
	return protocol.Memory{
		ID: id, WorkspaceID: workspaceID, Content: input.Content, Type: input.Type, Scope: input.Scope,
		Source: input.Source, Metadata: input.Metadata, State: protocol.MemoryActive, Version: 1,
		Supersedes: supersedes, CreatedAt: created, UpdatedAt: created,
	}, nil
}

func newMemoryID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", protocol.NewError(protocol.CodeInternal, "cannot create memory ID")
	}
	return "mem_" + hex.EncodeToString(raw[:]), nil
}

func now() protocol.Timestamp { return protocol.Timestamp{Time: time.Now()} }

func checkMemoryRef(workspaceID int64, memoryID string) error {
	if err := validWorkspaceID(workspaceID); err != nil {
		return err
	}
	return validMemoryID(memoryID)
}

func checkTarget(workspaceID int64, memoryID string, version int64) error {
	if err := checkMemoryRef(workspaceID, memoryID); err != nil {
		return err
	}
	return validVersion(version)
}

func checkWritable(item protocol.Memory, expectedVersion int64) error {
	if item.Version != expectedVersion {
		return protocol.NewError(protocol.CodeVersionConflict, "memory version does not match")
	}
	if item.State != protocol.MemoryActive {
		return protocol.NewError(protocol.CodeInvalidMemoryState, "memory is not active")
	}
	return nil
}

func cleanUpdate(request *protocol.MemoryUpdateRequest) error {
	if request.Content != nil {
		cleaned, err := cleanInput(protocol.MemoryInput{Content: *request.Content})
		if err != nil {
			return err
		}
		request.Content = &cleaned.Content
	}
	var err error
	if request.Type != nil {
		_, err = cleanLabel(request.Type, 128, "type")
		if err != nil {
			return err
		}
	}
	if request.Scope != nil {
		_, err = cleanLabel(request.Scope, 256, "scope")
		if err != nil {
			return err
		}
	}
	if request.Source != nil {
		cleaned, err := cleanSource(*request.Source)
		if err != nil {
			return err
		}
		request.Source = &cleaned
	}
	if request.Metadata != nil {
		encoded, err := json.Marshal(*request.Metadata)
		if err != nil || len(encoded) > 16*1024 {
			return protocol.Invalid("metadata is invalid or too large")
		}
	}
	return nil
}

func applyUpdate(item *protocol.Memory, request protocol.MemoryUpdateRequest) {
	if request.Content != nil {
		item.Content = *request.Content
	}
	if request.Type != nil {
		item.Type, _ = cleanLabel(request.Type, 128, "type")
	}
	if request.Scope != nil {
		item.Scope, _ = cleanLabel(request.Scope, 256, "scope")
	}
	if request.Source != nil {
		item.Source = *request.Source
	}
	if request.Metadata != nil {
		item.Metadata = *request.Metadata
	}
}

func checkSearch(request protocol.MemorySearchRequest) ([]string, []string, []string, int, error) {
	if err := validWorkspaceID(request.WorkspaceID); err != nil {
		return nil, nil, nil, 0, err
	}
	terms := words(request.Query)
	types, scopes := []string(nil), []string(nil)
	if request.Filter != nil {
		if request.Filter.Types != nil {
			types = cleanLabels(request.Filter.Types)
			if len(types) == 0 {
				return nil, nil, nil, 0, protocol.Invalid("filter types must not be empty")
			}
		}
		if request.Filter.Scopes != nil {
			scopes = cleanLabels(request.Filter.Scopes)
			if len(scopes) == 0 {
				return nil, nil, nil, 0, protocol.Invalid("filter scopes must not be empty")
			}
		}
		if invalidRange(request.Filter.CreatedAfter, request.Filter.CreatedBefore) || invalidRange(request.Filter.UpdatedAfter, request.Filter.UpdatedBefore) {
			return nil, nil, nil, 0, protocol.Invalid("search time range is invalid")
		}
	}
	if len(terms) == 0 && len(types) == 0 && len(scopes) == 0 && !hasTimeFilter(request.Filter) {
		return nil, nil, nil, 0, protocol.Invalid("query or filter is required")
	}
	limit := 8
	if request.Limit != nil {
		limit = *request.Limit
	}
	if limit < 1 || limit > 20 {
		return nil, nil, nil, 0, protocol.Invalid("limit must be between 1 and 20")
	}
	return terms, types, scopes, limit, nil
}

func invalidRange(after, before *protocol.Timestamp) bool {
	return after != nil && before != nil && after.After(before.Time)
}

func hasTimeFilter(filter *protocol.SearchFilter) bool {
	return filter != nil && (filter.CreatedAfter != nil || filter.CreatedBefore != nil || filter.UpdatedAfter != nil || filter.UpdatedBefore != nil)
}

func matchesFilter(item protocol.Memory, filter *protocol.SearchFilter, types, scopes []string) bool {
	if filter == nil {
		return true
	}
	if len(types) != 0 && !containsLabel(types, item.Type) || len(scopes) != 0 && !containsLabel(scopes, item.Scope) {
		return false
	}
	return within(item.CreatedAt, filter.CreatedAfter, filter.CreatedBefore) && within(item.UpdatedAt, filter.UpdatedAfter, filter.UpdatedBefore)
}

func containsLabel(values []string, value *string) bool {
	if value == nil {
		return false
	}
	for _, candidate := range values {
		if candidate == *value {
			return true
		}
	}
	return false
}

func within(value protocol.Timestamp, after, before *protocol.Timestamp) bool {
	return (after == nil || !value.Before(after.Time)) && (before == nil || !value.After(before.Time))
}

func matchTerms(item protocol.Memory, terms []string) (float64, bool) {
	if len(terms) == 0 {
		return 0, true
	}
	text := asciiFold(item.Content)
	if item.Type != nil {
		text += "\n" + asciiFold(*item.Type)
	}
	if item.Scope != nil {
		text += "\n" + asciiFold(*item.Scope)
	}
	matched := 0
	for _, term := range terms {
		if strings.Contains(text, term) {
			matched++
		}
	}
	return float64(matched) / float64(len(terms)), matched != 0
}

func snippet(content string) string {
	if len(content) <= 240 {
		return content
	}
	end := 240
	for end > 0 && !utf8.RuneStart(content[end]) {
		end--
	}
	return content[:end]
}
