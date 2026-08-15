package sql

const (
	CreateWorkspaces = `CREATE TABLE IF NOT EXISTS workspaces (
		workspace_id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL UNIQUE,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`

	CreateWorkspaceLocks = `CREATE TABLE IF NOT EXISTS workspace_locks (
		workspace_id INTEGER NOT NULL,
		owner TEXT NOT NULL,
		mode TEXT NOT NULL CHECK(mode IN ('read', 'write')),
		expires_at INTEGER NOT NULL,
		PRIMARY KEY(workspace_id, owner),
		FOREIGN KEY(workspace_id) REFERENCES workspaces(workspace_id) ON DELETE CASCADE
	)`

	CreateWorkspaceLocksExpiry = `CREATE INDEX IF NOT EXISTS workspace_locks_expiry
		ON workspace_locks(expires_at)`

	InsertWorkspace = `INSERT INTO workspaces(path, created_at, updated_at)
		VALUES(?, ?, ?)`

	SelectWorkspace = `SELECT workspace_id, path, created_at, updated_at
		FROM workspaces WHERE workspace_id = ?`

	SelectWorkspaceByPath = `SELECT workspace_id, path, created_at, updated_at
		FROM workspaces WHERE path = ?`

	UpdateWorkspacePath = `UPDATE workspaces SET path = ?, updated_at = ?
		WHERE workspace_id = ?`

	DeleteWorkspace = `DELETE FROM workspaces WHERE workspace_id = ?`

	DeleteExpiredLocks = `DELETE FROM workspace_locks WHERE expires_at <= ?`

	DeleteWorkspaceLock = `DELETE FROM workspace_locks
		WHERE workspace_id = ? AND owner = ?`
)

var MetaSchema = []string{
	CreateWorkspaces,
	CreateWorkspaceLocks,
	CreateWorkspaceLocksExpiry,
}

func InsertWorkspaceLock(write bool) string {
	blockedBy := `mode = 'write'`
	if write {
		blockedBy = `1 = 1`
	}
	return `INSERT INTO workspace_locks(workspace_id, owner, mode, expires_at)
		SELECT ?, ?, ?, ?
		WHERE EXISTS(SELECT 1 FROM workspaces WHERE workspace_id = ?)
		AND NOT EXISTS(
			SELECT 1 FROM workspace_locks
			WHERE workspace_id = ? AND ` + blockedBy +
		`)`
}
