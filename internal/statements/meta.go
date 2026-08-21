package statements

const (
	SetMetaApplicationID = `PRAGMA application_id = 1146309965`
	SetSchemaVersion     = `PRAGMA user_version = 1`

	CreateMetaInfo = `CREATE TABLE meta_info (
		id INTEGER PRIMARY KEY CHECK(id = 1),
		application_version TEXT NOT NULL,
		schema_version INTEGER NOT NULL CHECK(schema_version = 1)
	)`
	InsertMetaInfo = `INSERT INTO meta_info(id, application_version, schema_version) VALUES(1, ?, 1)`
	ReadMetaInfo   = `SELECT schema_version FROM meta_info WHERE id = 1`

	CreateWorkspaces = `CREATE TABLE workspaces (
		workspace_id INTEGER PRIMARY KEY AUTOINCREMENT CHECK(workspace_id BETWEEN 1 AND 9007199254740991),
		path TEXT NOT NULL UNIQUE,
		created_at INTEGER NOT NULL,
		created_offset INTEGER NOT NULL CHECK(created_offset BETWEEN -840 AND 840),
		updated_at INTEGER NOT NULL,
		updated_offset INTEGER NOT NULL CHECK(updated_offset BETWEEN -840 AND 840)
	)`

	CreateWorkspaceLocks = `CREATE TABLE workspace_locks (
		workspace_id INTEGER NOT NULL,
		mode TEXT NOT NULL CHECK(mode IN ('read', 'write')),
		session_id TEXT NOT NULL,
		acquired_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		PRIMARY KEY (workspace_id, session_id)
	)`
	CreateLocksSingleWriter = `CREATE UNIQUE INDEX workspace_locks_single_writer ON workspace_locks(workspace_id) WHERE mode = 'write'`
	CreateLocksExpiry       = `CREATE INDEX workspace_locks_expiry ON workspace_locks(expires_at)`

	SelectWorkspace       = `SELECT workspace_id, path, created_at, created_offset, updated_at, updated_offset FROM workspaces WHERE workspace_id = ?`
	SelectWorkspaceByPath = `SELECT workspace_id, path, created_at, created_offset, updated_at, updated_offset FROM workspaces WHERE path = ?`
	SelectAllWorkspaces   = `SELECT workspace_id, path, created_at, created_offset, updated_at, updated_offset FROM workspaces ORDER BY workspace_id`
	InsertWorkspace       = `INSERT INTO workspaces(path, created_at, created_offset, updated_at, updated_offset) VALUES(?, ?, ?, ?, ?)`
	UpdateWorkspacePath   = `UPDATE workspaces SET path = ?, updated_at = ?, updated_offset = ? WHERE workspace_id = ?`
	DeleteWorkspace       = `DELETE FROM workspaces WHERE workspace_id = ?`

	DeleteExpiredLocks = `DELETE FROM workspace_locks WHERE expires_at <= ?`
	SelectAnyLock      = `SELECT 1 FROM workspace_locks WHERE workspace_id = ? AND expires_at > ? LIMIT 1`
	SelectWriteLock    = `SELECT 1 FROM workspace_locks WHERE workspace_id = ? AND mode = 'write' AND expires_at > ? LIMIT 1`
	InsertLock         = `INSERT INTO workspace_locks(workspace_id, mode, session_id, acquired_at, expires_at) VALUES(?, ?, ?, ?, ?)`
	DeleteLock         = `DELETE FROM workspace_locks WHERE workspace_id = ? AND session_id = ?`
	RenewLock          = `UPDATE workspace_locks SET expires_at = ? WHERE workspace_id = ? AND session_id = ? AND expires_at > ?`
)

var MetaSchema = []string{
	SetMetaApplicationID,
	SetSchemaVersion,
	CreateMetaInfo,
	CreateWorkspaces,
	CreateWorkspaceLocks,
	CreateLocksSingleWriter,
	CreateLocksExpiry,
}
