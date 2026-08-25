package statements

import (
	"strings"
)

// MemorySearch builds the search statement from structural choices only.
// Caller values are always supplied separately as bound parameters.
func MemorySearch(withQuery bool, kindCount, labelCount int, createdAfter, createdBefore, updatedAfter, updatedBefore bool) string {
	var query strings.Builder
	if withQuery {
		query.WriteString(`SELECT m.memory_id, m.workspace_id, m.content, m.kind, m.label, m.source_json, m.metadata_json,
			m.state, m.version, m.supersedes, m.superseded_by, m.created_at, m.created_offset,
			m.updated_at, m.updated_offset, bm25(memory_fts)
			FROM memory_fts JOIN memories AS m ON m.rowid = memory_fts.rowid
			WHERE memory_fts MATCH ? AND m.workspace_id = ? AND m.state = 'active'`)
	} else {
		query.WriteString(`SELECT m.memory_id, m.workspace_id, m.content, m.kind, m.label, m.source_json, m.metadata_json,
			m.state, m.version, m.supersedes, m.superseded_by, m.created_at, m.created_offset,
			m.updated_at, m.updated_offset, 0.0
			FROM memories AS m WHERE m.workspace_id = ? AND m.state = 'active'`)
	}
	writeIn(&query, "m.kind", kindCount)
	writeIn(&query, "m.label", labelCount)
	writeBound(&query, "m.created_at", ">=", createdAfter)
	writeBound(&query, "m.created_at", "<=", createdBefore)
	writeBound(&query, "m.updated_at", ">=", updatedAfter)
	writeBound(&query, "m.updated_at", "<=", updatedBefore)
	if withQuery {
		query.WriteString(" ORDER BY bm25(memory_fts) ASC, m.updated_at DESC, m.memory_id ASC LIMIT ?")
	} else {
		query.WriteString(" ORDER BY m.updated_at DESC, m.memory_id ASC LIMIT ?")
	}
	return query.String()
}

func writeIn(query *strings.Builder, column string, count int) {
	if count == 0 {
		return
	}
	query.WriteString(" AND ")
	query.WriteString(column)
	query.WriteString(" IN (")
	for index := 0; index < count; index++ {
		if index > 0 {
			query.WriteString(", ")
		}
		query.WriteByte('?')
	}
	query.WriteByte(')')
}

func writeBound(query *strings.Builder, column, operator string, present bool) {
	if !present {
		return
	}
	query.WriteString(" AND ")
	query.WriteString(column)
	query.WriteByte(' ')
	query.WriteString(operator)
	query.WriteString(" ?")
}
