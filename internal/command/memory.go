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
	terms, kinds, labels, limit, err := checkSearch(request)
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
		if !matchesFilter(item, request.Filter, kinds, labels) {
			continue
		}
		score, matched := matchTerms(item, terms)
		if len(terms) != 0 && !matched {
			continue
		}
		hits = append(hits, protocol.SearchHit{
			ID: item.ID, Kind: item.Kind, Label: item.Label, Version: item.Version,
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
			ID: item.ID, Kind: item.Kind, Label: item.Label, State: item.State, Version: item.Version,
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
	if request.Version != nil && (*request.Version < 1 || *request.Version > protocol.MaxSafeInteger) {
		return protocol.MemoryGetData{}, protocol.Invalid("version must be a positive safe integer")
	}
	current, run, store, tx, err := getMemory(ctx, storeRoot, sessionID, request.WorkspaceID, request.MemoryID, true)
	if err != nil {
		return protocol.MemoryGetData{}, err
	}
	defer run.Release()
	defer store.Close()
	defer tx.Rollback()
	item := current
	if request.Version != nil && *request.Version != current.Version {
		item, err = tx.Version(ctx, request.WorkspaceID, request.MemoryID, *request.Version)
		if err != nil {
			return protocol.MemoryGetData{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return protocol.MemoryGetData{}, protocol.NewError(protocol.CodeInternal, "cannot finish memory read")
	}
	if err := run.Commit(ctx); err != nil {
		return protocol.MemoryGetData{}, err
	}
	return protocol.MemoryGetData{MemoryResult: protocol.MemoryResult{Memory: item}}, nil
}

func memoryHistory(ctx context.Context, storeRoot, sessionID, raw string) (protocol.MemoryHistoryData, error) {
	var request protocol.MemoryHistoryRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryHistoryData{}, err
	}
	if err := checkMemoryRef(request.WorkspaceID, request.MemoryID); err != nil {
		return protocol.MemoryHistoryData{}, err
	}
	current, run, store, tx, err := getMemory(ctx, storeRoot, sessionID, request.WorkspaceID, request.MemoryID, true)
	if err != nil {
		return protocol.MemoryHistoryData{}, err
	}
	defer run.Release()
	defer store.Close()
	defer tx.Rollback()
	archived, err := tx.Versions(ctx, request.WorkspaceID, request.MemoryID)
	if err != nil {
		return protocol.MemoryHistoryData{}, err
	}
	action, err := tx.LatestAction(ctx, request.WorkspaceID, request.MemoryID)
	if err != nil {
		return protocol.MemoryHistoryData{}, err
	}
	versions := make([]protocol.HistoryItem, 0, len(archived)+1)
	// An archive row's action is the operation that archived it — which is
	// the operation that produced the next version. So version k's producing
	// action is the archive action of the row before it, and v1 is create.
	for index, item := range archived {
		producing := "create"
		if index > 0 {
			producing = archived[index-1].Action
		}
		versions = append(versions, protocol.HistoryItem{
			Version: item.Version, Action: producing, State: item.State,
			UpdatedAt: item.UpdatedAt, ArchivedAt: item.ArchivedAt,
		})
	}
	versions = append(versions, protocol.HistoryItem{
		Version: current.Version, Action: action, State: current.State,
		UpdatedAt: current.UpdatedAt, ArchivedAt: nil,
	})
	if err := tx.Commit(); err != nil {
		return protocol.MemoryHistoryData{}, protocol.NewError(protocol.CodeInternal, "cannot finish memory history")
	}
	if err := run.Commit(ctx); err != nil {
		return protocol.MemoryHistoryData{}, err
	}
	return protocol.MemoryHistoryData{Versions: versions}, nil
}

func memoryDiff(ctx context.Context, storeRoot, sessionID, raw string) (protocol.MemoryDiffData, error) {
	var request protocol.MemoryDiffRequest
	if err := protocol.Decode(raw, &request); err != nil {
		return protocol.MemoryDiffData{}, err
	}
	if err := checkMemoryRef(request.WorkspaceID, request.MemoryID); err != nil {
		return protocol.MemoryDiffData{}, err
	}
	if request.FromVersion == request.ToVersion {
		return protocol.MemoryDiffData{}, protocol.Invalid("from_version and to_version must differ")
	}
	for _, value := range []int64{request.FromVersion, request.ToVersion} {
		if value < 1 || value > protocol.MaxSafeInteger {
			return protocol.MemoryDiffData{}, protocol.Invalid("version must be a positive safe integer")
		}
	}
	current, run, store, tx, err := getMemory(ctx, storeRoot, sessionID, request.WorkspaceID, request.MemoryID, true)
	if err != nil {
		return protocol.MemoryDiffData{}, err
	}
	defer run.Release()
	defer store.Close()
	defer tx.Rollback()
	resolve := func(version int64) (protocol.Memory, error) {
		if version == current.Version {
			return current, nil
		}
		return tx.Version(ctx, request.WorkspaceID, request.MemoryID, version)
	}
	from, err := resolve(request.FromVersion)
	if err != nil {
		return protocol.MemoryDiffData{}, err
	}
	to, err := resolve(request.ToVersion)
	if err != nil {
		return protocol.MemoryDiffData{}, err
	}
	changes := diffMemories(from, to)
	if err := tx.Commit(); err != nil {
		return protocol.MemoryDiffData{}, protocol.NewError(protocol.CodeInternal, "cannot finish memory diff")
	}
	if err := run.Commit(ctx); err != nil {
		return protocol.MemoryDiffData{}, err
	}
	return protocol.MemoryDiffData{FromVersion: request.FromVersion, ToVersion: request.ToVersion, Changes: changes}, nil
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
	reason, err := cleanReason(request.Reason)
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
	if err := tx.InsertEvent(ctx, request.WorkspaceID, item.ID, "create", nil, &item.Version, "", reason, item.CreatedAt.Time); err != nil {
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
	if request.Content == nil && request.Kind == nil && request.Label == nil && request.Source == nil && request.Metadata == nil {
		return protocol.MemoryUpdateData{}, protocol.Invalid("at least one memory field is required")
	}
	if err := cleanUpdate(&request); err != nil {
		return protocol.MemoryUpdateData{}, err
	}
	reason, err := cleanReason(request.Reason)
	if err != nil {
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
	if err := tx.Archive(ctx, previous, "update", time.Now().Unix()); err != nil {
		return protocol.MemoryUpdateData{}, err
	}
	if err := tx.InsertEvent(ctx, request.WorkspaceID, item.ID, "update", &previous.Version, &item.Version, "", reason, item.UpdatedAt.Time); err != nil {
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
	reason, err := cleanReason(request.Reason)
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
	if err := tx.Archive(ctx, previous, "supersede", time.Now().Unix()); err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	if err := tx.Insert(ctx, newItem); err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	if err := tx.InsertEvent(ctx, request.WorkspaceID, old.ID, "supersede", &previous.Version, &old.Version, newItem.ID, reason, old.UpdatedAt.Time); err != nil {
		return protocol.MemorySupersedeData{}, err
	}
	if err := tx.InsertEvent(ctx, request.WorkspaceID, newItem.ID, "create", nil, &newItem.Version, "", "", newItem.CreatedAt.Time); err != nil {
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
	reason, err := cleanReason(request.Reason)
	if err != nil {
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
	if err := tx.Archive(ctx, previous, "invalidate", time.Now().Unix()); err != nil {
		return protocol.MemoryInvalidateData{}, err
	}
	if err := tx.InsertEvent(ctx, request.WorkspaceID, item.ID, "invalidate", &previous.Version, &item.Version, "", reason, item.UpdatedAt.Time); err != nil {
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
		ID: id, WorkspaceID: workspaceID, Content: input.Content, Kind: input.Kind, Label: input.Label,
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
		if !utf8.ValidString(*request.Content) || strings.TrimSpace(*request.Content) == "" ||
			strings.ContainsRune(*request.Content, 0) || len(*request.Content) > 64*1024 {
			return protocol.Invalid("content is invalid")
		}
	}
	var err error
	if request.Kind != nil {
		if !protocol.ValidMemoryKind(strings.TrimSpace(*request.Kind)) {
			return protocol.Invalid("kind must be fact or note")
		}
		trimmed := strings.TrimSpace(*request.Kind)
		request.Kind = &trimmed
	}
	if request.Label != nil {
		_, err = cleanLabel(request.Label, 256, "label")
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
	if request.Kind != nil {
		item.Kind = *request.Kind
	}
	if request.Label != nil {
		item.Label, _ = cleanLabel(request.Label, 256, "label")
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
	kinds, labels := []string(nil), []string(nil)
	if request.Filter != nil {
		if request.Filter.Kinds != nil {
			kinds = cleanLabels(request.Filter.Kinds)
			if len(kinds) == 0 {
				return nil, nil, nil, 0, protocol.Invalid("filter kinds must not be empty")
			}
		}
		if request.Filter.Labels != nil {
			labels = cleanLabels(request.Filter.Labels)
			if len(labels) == 0 {
				return nil, nil, nil, 0, protocol.Invalid("filter labels must not be empty")
			}
		}
		if invalidRange(request.Filter.CreatedAfter, request.Filter.CreatedBefore) || invalidRange(request.Filter.UpdatedAfter, request.Filter.UpdatedBefore) {
			return nil, nil, nil, 0, protocol.Invalid("search time range is invalid")
		}
	}
	if len(terms) == 0 && len(kinds) == 0 && len(labels) == 0 && !hasTimeFilter(request.Filter) {
		return nil, nil, nil, 0, protocol.Invalid("query or filter is required")
	}
	limit := 8
	if request.Limit != nil {
		limit = *request.Limit
	}
	if limit < 1 || limit > 20 {
		return nil, nil, nil, 0, protocol.Invalid("limit must be between 1 and 20")
	}
	return terms, kinds, labels, limit, nil
}

func invalidRange(after, before *protocol.Timestamp) bool {
	return after != nil && before != nil && after.After(before.Time)
}

func hasTimeFilter(filter *protocol.SearchFilter) bool {
	return filter != nil && (filter.CreatedAfter != nil || filter.CreatedBefore != nil || filter.UpdatedAfter != nil || filter.UpdatedBefore != nil)
}

func matchesFilter(item protocol.Memory, filter *protocol.SearchFilter, kinds, labels []string) bool {
	if filter == nil {
		return true
	}
	if len(kinds) != 0 && !containsKind(kinds, item.Kind) {
		return false
	}
	if len(labels) != 0 && !containsLabel(labels, item.Label) {
		return false
	}
	return within(item.CreatedAt, filter.CreatedAfter, filter.CreatedBefore) && within(item.UpdatedAt, filter.UpdatedAfter, filter.UpdatedBefore)
}

func containsKind(values []string, kind string) bool {
	for _, candidate := range values {
		if candidate == kind {
			return true
		}
	}
	return false
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
	text += "\n" + asciiFold(item.Kind)
	if item.Label != nil {
		text += "\n" + asciiFold(*item.Label)
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
