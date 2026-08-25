package statements

const (
	SetMemoryApplicationID = `PRAGMA application_id = 1146309975`

	CreateMemoryInfo = `CREATE TABLE memory_info (
		id INTEGER PRIMARY KEY CHECK(id = 1),
		workspace_id INTEGER NOT NULL CHECK(workspace_id BETWEEN 1 AND 9007199254740991),
		schema_version INTEGER NOT NULL CHECK(schema_version = 4)
	)`
	InsertMemoryInfo = `INSERT INTO memory_info(id, workspace_id, schema_version) VALUES(1, ?, 4)`
	ReadMemoryInfo   = `SELECT workspace_id, schema_version FROM memory_info WHERE id = 1`

	CreateMemories = `CREATE TABLE memories (
		workspace_id INTEGER NOT NULL CHECK(workspace_id BETWEEN 1 AND 9007199254740991),
		memory_id TEXT NOT NULL,
		content TEXT NOT NULL,
		kind TEXT NOT NULL CHECK(kind IN ('fact', 'note')),
		label TEXT,
		source_json TEXT NOT NULL,
		metadata_json TEXT NOT NULL,
		state TEXT NOT NULL CHECK(state IN ('active', 'superseded', 'invalid')),
		version INTEGER NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
		supersedes TEXT,
		superseded_by TEXT,
		created_at INTEGER NOT NULL,
		created_offset INTEGER NOT NULL CHECK(created_offset BETWEEN -840 AND 840),
		updated_at INTEGER NOT NULL,
		updated_offset INTEGER NOT NULL CHECK(updated_offset BETWEEN -840 AND 840),
		PRIMARY KEY (workspace_id, memory_id),
		FOREIGN KEY (workspace_id, supersedes) REFERENCES memories(workspace_id, memory_id),
		FOREIGN KEY (workspace_id, superseded_by) REFERENCES memories(workspace_id, memory_id)
	)`

	CreateMemoryHistory = `CREATE TABLE memory_history (
		workspace_id INTEGER NOT NULL,
		memory_id TEXT NOT NULL,
		version INTEGER NOT NULL,
		action TEXT NOT NULL,
		content TEXT NOT NULL,
		kind TEXT NOT NULL CHECK(kind IN ('fact', 'note')),
		label TEXT,
		source_json TEXT NOT NULL,
		metadata_json TEXT NOT NULL,
		state TEXT NOT NULL,
		supersedes TEXT,
		superseded_by TEXT,
		created_at INTEGER NOT NULL,
		created_offset INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		updated_offset INTEGER NOT NULL,
		archived_at INTEGER NOT NULL,
		PRIMARY KEY (workspace_id, memory_id, version)
	)`

	CreateMemoryEvents = `CREATE TABLE memory_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		workspace_id INTEGER NOT NULL,
		memory_id TEXT NOT NULL,
		action TEXT NOT NULL CHECK(action IN ('create', 'update', 'supersede', 'invalidate')),
		from_version INTEGER,
		to_version INTEGER,
		related_memory_id TEXT,
		reason TEXT,
		created_at INTEGER NOT NULL,
		created_offset INTEGER NOT NULL CHECK(created_offset BETWEEN -840 AND 840)
	)`
	CreateMemoryEventsIndex = `CREATE INDEX memory_events_by_memory ON memory_events(workspace_id, memory_id, id)`

	CreateMemoriesSearch = `CREATE INDEX memories_search ON memories(state, kind, label, updated_at, memory_id)`
	CreateMemoriesList   = `CREATE INDEX memories_list ON memories(workspace_id, updated_at, memory_id)`

	SelectMemoryColumns = `SELECT memory_id, workspace_id, content, kind, label, source_json, metadata_json,
		state, version, supersedes, superseded_by, created_at, created_offset, updated_at, updated_offset FROM memories`
	SelectMemoryByID = SelectMemoryColumns + ` WHERE workspace_id = ? AND memory_id = ?`
	SelectActive     = SelectMemoryColumns + ` WHERE workspace_id = ? AND state = 'active'`
	SelectFirstPage  = SelectMemoryColumns + ` WHERE workspace_id = ? ORDER BY updated_at DESC, memory_id ASC LIMIT ?`
	SelectNextPage   = SelectMemoryColumns + ` WHERE workspace_id = ? AND (updated_at < ? OR (updated_at = ? AND memory_id > ?))
		ORDER BY updated_at DESC, memory_id ASC LIMIT ?`

	InsertMemory = `INSERT INTO memories(memory_id, workspace_id, content, kind, label, source_json,
		metadata_json, state, version, supersedes, superseded_by, created_at, created_offset, updated_at, updated_offset)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	InsertHistory = `INSERT INTO memory_history(workspace_id, memory_id, version, action, content, kind, label,
		source_json, metadata_json, state, supersedes, superseded_by, created_at, created_offset,
		updated_at, updated_offset, archived_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	SelectHistoryColumns = `SELECT memory_id, workspace_id, content, kind, label, source_json, metadata_json,
		state, version, supersedes, superseded_by, created_at, created_offset, updated_at, updated_offset FROM memory_history`
	SelectHistoryVersion = SelectHistoryColumns + ` WHERE workspace_id = ? AND memory_id = ? AND version = ?`

	InsertEvent = `INSERT INTO memory_events(workspace_id, memory_id, action, from_version, to_version,
		related_memory_id, reason, created_at, created_offset)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`

	SelectHistoryList = `SELECT version, action, state, updated_at, updated_offset, archived_at
		FROM memory_history WHERE workspace_id = ? AND memory_id = ? ORDER BY version`

	SelectLatestEventAction = `SELECT action FROM memory_events WHERE workspace_id = ? AND memory_id = ?
		ORDER BY id DESC LIMIT 1`

	DeleteMemoryEvents = `DELETE FROM memory_events WHERE workspace_id = ? AND memory_id = ?`
	ClearMemoryEvents  = `DELETE FROM memory_events WHERE workspace_id = ?`

	UpdateMemory = `UPDATE memories SET content = ?, kind = ?, label = ?, source_json = ?, metadata_json = ?,
		state = ?, version = ?, supersedes = ?, superseded_by = ?, updated_at = ?, updated_offset = ?
		WHERE workspace_id = ? AND memory_id = ? AND version = ?`

	ClearRelationships = `UPDATE memories SET
		supersedes = CASE WHEN supersedes = ? THEN NULL ELSE supersedes END,
		superseded_by = CASE WHEN superseded_by = ? THEN NULL ELSE superseded_by END,
		version = version + 1, updated_at = ?, updated_offset = ?
		WHERE workspace_id = ? AND (supersedes = ? OR superseded_by = ?)`

	DeleteMemoryHistory = `DELETE FROM memory_history WHERE workspace_id = ? AND memory_id = ?`
	DeleteMemory        = `DELETE FROM memories WHERE workspace_id = ? AND memory_id = ? AND version = ?`
	ClearMemory         = `DELETE FROM memories WHERE workspace_id = ?`
	ClearMemoryHistory  = `DELETE FROM memory_history WHERE workspace_id = ?`
)

var MemorySchema = []string{
	SetMemoryApplicationID,
	SetSchemaVersion,
	CreateMemoryInfo,
	CreateMemories,
	CreateMemoryHistory,
	CreateMemoryEvents,
	CreateMemoryEventsIndex,
	CreateMemoriesSearch,
	CreateMemoriesList,
}
