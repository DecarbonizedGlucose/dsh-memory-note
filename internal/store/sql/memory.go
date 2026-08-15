package sql

import (
	"strings"
	"time"
)

const (
	CreateMemories = `CREATE TABLE IF NOT EXISTS memories (
		memory_id TEXT PRIMARY KEY,
		workspace_id INTEGER NOT NULL,
		content TEXT NOT NULL,
		type TEXT NOT NULL,
		scope TEXT NOT NULL,
		source_json TEXT NOT NULL,
		metadata_json TEXT NOT NULL,
		state TEXT NOT NULL CHECK(state IN ('active', 'superseded', 'invalid')),
		version INTEGER NOT NULL CHECK(version > 0),
		supersedes TEXT,
		superseded_by TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY(supersedes) REFERENCES memories(memory_id) ON DELETE SET NULL,
		FOREIGN KEY(superseded_by) REFERENCES memories(memory_id) ON DELETE SET NULL
	)`

	CreateMemoriesSearch = `CREATE INDEX IF NOT EXISTS memories_search
		ON memories(state, type, scope, updated_at DESC)`

	SelectMemory = `SELECT memory_id, workspace_id, content, type, scope, source_json,
		metadata_json, state, version, COALESCE(supersedes, ''),
		COALESCE(superseded_by, ''), created_at, updated_at FROM memories`

	SelectMemoryByID = SelectMemory + ` WHERE workspace_id = ? AND memory_id = ?`

	InsertMemory = `INSERT INTO memories(memory_id, workspace_id, content, type, scope,
		source_json, metadata_json, state, version, supersedes,
		superseded_by, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)`

	UpdateMemory = `UPDATE memories SET content = ?, type = ?, scope = ?, source_json = ?,
		metadata_json = ?, state = ?, version = ?,
		supersedes = NULLIF(?, ''), superseded_by = NULLIF(?, ''), updated_at = ?
		WHERE workspace_id = ? AND memory_id = ? AND version = ?`

	DeleteMemory = `DELETE FROM memories
		WHERE workspace_id = ? AND memory_id = ? AND version = ?`

	DeleteWorkspaceMemories = `DELETE FROM memories WHERE workspace_id = ?`
)

var MemorySchema = []string{
	CreateMemories,
	CreateMemoriesSearch,
}

type SearchParams struct {
	WorkspaceID   int64
	Terms         []string
	Types         []string
	Scopes        []string
	State         string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	UpdatedAfter  *time.Time
	UpdatedBefore *time.Time
	Limit         int
}

func SearchMemory(params SearchParams) (string, []any) {
	clauses := []string{`workspace_id = ?`, `state = ?`}
	args := []any{params.WorkspaceID, params.State}
	clauses, args = addList(clauses, args, `type`, params.Types)
	clauses, args = addList(clauses, args, `scope`, params.Scopes)
	clauses, args = addTime(clauses, args, `created_at`, `>=`, params.CreatedAfter)
	clauses, args = addTime(clauses, args, `created_at`, `<=`, params.CreatedBefore)
	clauses, args = addTime(clauses, args, `updated_at`, `>=`, params.UpdatedAfter)
	clauses, args = addTime(clauses, args, `updated_at`, `<=`, params.UpdatedBefore)
	if len(params.Terms) > 0 {
		parts := make([]string, len(params.Terms))
		for index, term := range params.Terms {
			parts[index] = `lower(content || ' ' || type || ' ' || scope) LIKE ? ESCAPE '\'`
			args = append(args, `%`+escapeLike(term)+`%`)
		}
		clauses = append(clauses, `(`+strings.Join(parts, ` OR `)+`)`)
	}
	query := SelectMemory + ` WHERE ` + strings.Join(clauses, ` AND `) +
		` ORDER BY updated_at DESC LIMIT ?`
	return query, append(args, params.Limit)
}

func addList(clauses []string, args []any, column string, values []string) ([]string, []any) {
	if len(values) == 0 {
		return clauses, args
	}
	marks := make([]string, len(values))
	for index, value := range values {
		marks[index] = `?`
		args = append(args, value)
	}
	return append(clauses, column+` IN (`+strings.Join(marks, `,`)+`)`), args
}

func addTime(clauses []string, args []any, column, operator string, value *time.Time) ([]string, []any) {
	if value == nil {
		return clauses, args
	}
	stamp := value.UTC().Format(time.RFC3339Nano)
	return append(clauses, column+` `+operator+` ?`), append(args, stamp)
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
