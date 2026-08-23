# SQL Standard

This document specifies the table structure, SQL statements, transaction boundaries, and invariants of the dsh-memory-note storage layer. It is the storage-layer realization of `overall-design.md` and `protocol-v1-proposal.md`; where the three conflict, the protocol document takes precedence.

Scope: `meta.db` under HOME, and each `workspace-{WID}-memory.db` under `memory/` (referred to below as a memory DB).

## 1. General rules

- Every SQLite connection must set: `PRAGMA foreign_keys = ON`, `PRAGMA busy_timeout = 1000` (milliseconds), `PRAGMA journal_mode = WAL`.
- All SQL values use bound parameters; string-concatenated SQL is forbidden.
- All TEXT comparison and sorting use BINARY collation (the SQLite default); the protocol's "`memory_id` ascending" ordering is comparison by UTF-8 byte order.
- Database files are created with mode `0600` and directories with mode `0700`.
- Database identifiers:
  - `meta.db`: `PRAGMA application_id = 0x44534D4D` ("DSMM"), `PRAGMA user_version = 2`;
  - memory DB: `PRAGMA application_id = 0x44534D57` ("DSMW"), `PRAGMA user_version = 2`.
- Time columns: always stored as two INTEGERs — epoch seconds (the UTC instant) and offset minutes (the UTC offset at that instant, range -840..840). Comparison, range filtering, and ordering inside SQL use only epoch seconds; the external output format is defined by protocol §3.
- JSON columns (`source_json`, `metadata_json`): always stored in compact serialization (no extra whitespace), so that the protocol's length limits match the stored byte count.
- Transaction model: memory write commands use a single write transaction inside the memory DB; `meta.db` carries only short transactions (mapping validation, lock acquire/release, workspace-table changes). The WID lock is released and meta coordination ends only after the memory DB write transaction commits; if the lock release or teardown fails, the command returns `internal_error`, but the memory changes have already taken effect; callers follow the protocol's unknown-result discipline (read state first, then decide the next step).

## 2. meta.db

### 2.1 Schema

```sql
CREATE TABLE meta_info (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  schema_version INTEGER NOT NULL CHECK (schema_version = 2),
  cursor_key TEXT NOT NULL
);

CREATE TABLE workspaces (
  workspace_id INTEGER PRIMARY KEY AUTOINCREMENT
    CHECK (workspace_id BETWEEN 1 AND 9007199254740991),
  path TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  created_offset INTEGER NOT NULL CHECK (created_offset BETWEEN -840 AND 840),
  updated_at INTEGER NOT NULL,
  updated_offset INTEGER NOT NULL CHECK (updated_offset BETWEEN -840 AND 840)
);

CREATE TABLE workspace_locks (
  workspace_id INTEGER NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('read', 'write')),
  session_id TEXT NOT NULL,
  acquired_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (workspace_id, session_id)
);

CREATE UNIQUE INDEX workspace_locks_single_writer
  ON workspace_locks(workspace_id) WHERE mode = 'write';

CREATE INDEX workspace_locks_expiry ON workspace_locks(expires_at);
```

Notes:

- `workspace_id` is assigned by SQLite AUTOINCREMENT; after a delete, `sqlite_sequence` does not roll back, so a WID is never reused.
- `path` is the canonical path computed by the application layer (absolute, cleaned, symlink-resolved); the UNIQUE constraint ensures one path belongs to exactly one WID.
- `workspace_locks` implements the per-WID read/write locks from overall design §3. Writer mutual exclusion is backed by the partial unique index; read locks may coexist in any number. Each Session holds at most one row per WID (guaranteed by the primary key).
- `session_id` comes from the Session's random session ID and is independent of the workspace path.

### 2.2 Lock protocol

Time is measured in Unix seconds throughout. The lease TTL is 30 seconds: `expires_at = acquired_at + 30`.

Acquiring a lock (done atomically within one short `meta.db` transaction; the transaction uses `BEGIN IMMEDIATE`, so concurrent writers queue within SQLite's busy_timeout instead of hitting snapshot conflicts, and a timeout is reported as `workspace_busy`):

1. Reap expired locks: `DELETE FROM workspace_locks WHERE expires_at <= ?` (crash fallback);
2. Conflict check:
   - target is a write lock: any unexpired lock row for the WID conflicts (`SELECT 1 FROM workspace_locks WHERE workspace_id = ? AND expires_at > ? LIMIT 1`);
   - target is a read lock: an unexpired write-lock row for the WID conflicts (`SELECT 1 FROM workspace_locks WHERE workspace_id = ? AND mode = 'write' AND expires_at > ? LIMIT 1`);
3. If there is no conflict, INSERT its own lock row and commit. A concurrent write-lock INSERT that violates the partial unique index is also treated as a conflict.

On conflict, roll back the short transaction, wait, and retry, within a total budget of 5 seconds (including the busy_timeout wait); exceeding the budget returns `workspace_busy`.

Release: on normal completion, run `DELETE FROM workspace_locks WHERE workspace_id = ? AND session_id = ?`. A Session must release all of its lock rows before it ends.

Renew: when an operation is nearing expiry and is still the owner, run `UPDATE workspace_locks SET expires_at = ? WHERE workspace_id = ? AND session_id = ? AND expires_at > ?`; a zero affected-row count means ownership was lost, and the current operation must abort and must not begin new writes.

Locks coordinate process-level mutual exclusion; data atomicity is provided by the memory DB's SQLite transactions, and the two are not interchangeable.

### 2.3 Workspace statements

- resolve: `SELECT workspace_id, path, created_at, created_offset, updated_at, updated_offset FROM workspaces WHERE path = ?`; no row returns `workspace: null`.
- register: the application layer first validates the canonical path; then, inside a `BEGIN IMMEDIATE` write transaction, query by path — if the row exists, commit and return the existing row (`created: false`); if not, INSERT and commit, then create the memory DB file per §3.1. Concurrent registration of the same path is arbitrated by the `path` UNIQUE constraint (the side whose INSERT hits the unique conflict returns the existing row instead); WIDs are assigned by AUTOINCREMENT, so two processes never receive the same WID and a DB filename collision cannot happen.
- rebind: inside a `BEGIN IMMEDIATE` write transaction, read the target row (no row → `workspace_not_found`) → if the new path equals the current path, commit and return the original row (a no-op that leaves `updated_at` untouched) → query by the new path (exists → `workspace_path_used`) → `UPDATE workspaces SET path = ?, updated_at = ?, updated_offset = ? WHERE workspace_id = ?` → commit.
- delete: inside a `BEGIN IMMEDIATE` write transaction, `DELETE FROM workspaces WHERE workspace_id = ?` (zero affected rows → `workspace_not_found`, roll back) → commit → delete the files per §3.5.

## 3. Memory DB

### 3.1 Creation

The filename depends only on the decimal WID: `memory/workspace-{WID}-memory.db`. It is created with `O_CREATE|O_EXCL` (mode 0600); an already-existing file is a should-not-happen condition (WIDs are not reused) and returns `workspace_broken`. After creating the tables and inserting the header row, fsync the file.

### 3.2 Schema

```sql
CREATE TABLE memory_info (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  workspace_id INTEGER NOT NULL
    CHECK (workspace_id BETWEEN 1 AND 9007199254740991),
  schema_version INTEGER NOT NULL CHECK (schema_version = 2)
);

CREATE TABLE memories (
  workspace_id INTEGER NOT NULL
    CHECK (workspace_id BETWEEN 1 AND 9007199254740991),
  memory_id TEXT NOT NULL,
  content TEXT NOT NULL,
  type TEXT,
  scope TEXT,
  source_json TEXT NOT NULL,
  metadata_json TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('active', 'superseded', 'invalid')),
  version INTEGER NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991),
  supersedes TEXT,
  superseded_by TEXT,
  created_at INTEGER NOT NULL,
  created_offset INTEGER NOT NULL CHECK (created_offset BETWEEN -840 AND 840),
  updated_at INTEGER NOT NULL,
  updated_offset INTEGER NOT NULL CHECK (updated_offset BETWEEN -840 AND 840),
  PRIMARY KEY (workspace_id, memory_id),
  FOREIGN KEY (workspace_id, supersedes) REFERENCES memories(workspace_id, memory_id),
  FOREIGN KEY (workspace_id, superseded_by) REFERENCES memories(workspace_id, memory_id)
);

CREATE TABLE memory_history (
  workspace_id INTEGER NOT NULL,
  memory_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  content TEXT NOT NULL,
  type TEXT,
  scope TEXT,
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
);

CREATE INDEX memories_search ON memories(state, type, scope, updated_at, memory_id);
CREATE INDEX memories_list ON memories(workspace_id, updated_at, memory_id);
```

Notes:

- `memory_id` is unique only within a WID (composite primary key) and may repeat across workspaces; it is generated by the application layer with an opaque format.
- The composite foreign keys keep a supersession chain from crossing workspaces; the default RESTRICT action requires clearing the other end's pointer before a delete (see the §3.3 delete ordering).
- `memory_history` stores the complete old row from before each successful modification, and `version` is the old row's version at archive time; it has no foreign keys because an archived snapshot may reference rows that no longer exist. History exists only for internal audit and the "delete all versions" semantics, and is not exposed through the protocol.

### 3.3 State-transition statements

All write commands complete within a single memory DB write transaction, checking in this order: existence → expected version → state (protocol §8). When a statement's `WHERE ... AND version = ?` affects a row count other than 1, return `version_conflict` and roll back.

- create: `INSERT INTO memories(...) VALUES(...)`; `state = 'active'`, `version = 1`, `supersedes` is the new record's predecessor ID or NULL, `superseded_by = NULL`.
- update:
  1. SELECT the old row (no row → `memory_not_found`);
  2. old version mismatch → `version_conflict`; state not active → `invalid_memory_state`;
  3. INSERT the old row into `memory_history` (`archived_at = now`);
  4. `UPDATE memories SET content = ?, type = ?, scope = ?, source_json = ?, metadata_json = ?, version = version + 1, updated_at = ?, updated_offset = ? WHERE workspace_id = ? AND memory_id = ? AND version = ?`.
- supersede:
  1. SELECT the old row and check existence, version, and state (as in update);
  2. INSERT the old row into `memory_history`;
  3. INSERT the new row (`state = 'active'`, `version = 1`, `supersedes = old id`);
  4. `UPDATE memories SET state = 'superseded', superseded_by = ?, version = version + 1, updated_at = new record's created_at, updated_offset = new record's created_offset WHERE workspace_id = ? AND memory_id = ? AND version = old version`.
- invalidate:
  1. SELECT the old row and check (as in update);
  2. INSERT the old row into `memory_history`;
  3. `UPDATE memories SET state = 'invalid', version = version + 1, updated_at = ?, updated_offset = ? WHERE workspace_id = ? AND memory_id = ? AND version = ?`.
- delete:
  1. SELECT the old row; no row → `memory_not_found`; version mismatch → `version_conflict`. State is not checked;
  2. `DELETE FROM memory_history WHERE workspace_id = ? AND memory_id = ?` (deletes all history versions);
  3. if the old row's `supersedes` or `superseded_by` is non-NULL, clear the other end's pointer and increment the other end's version by 1:
     `UPDATE memories SET supersedes = CASE WHEN supersedes = ? THEN NULL ELSE supersedes END, superseded_by = CASE WHEN superseded_by = ? THEN NULL ELSE superseded_by END, version = version + 1, updated_at = ?, updated_offset = ? WHERE workspace_id = ? AND (supersedes = ? OR superseded_by = ?)`;
     incrementing the other end's version ensures a writer holding the old version number receives `version_conflict`;
  4. `DELETE FROM memories WHERE workspace_id = ? AND memory_id = ? AND version = ?` (affected row count other than 1 → `version_conflict`).
  The ordering clears the pointers before deleting the row, so the composite foreign keys (RESTRICT) do not block the delete.
- clear: within one transaction, `DELETE FROM memories WHERE workspace_id = ?` (the row count is `deleted_count`), then `DELETE FROM memory_history WHERE workspace_id = ?`.

### 3.4 Read statements

- get: `SELECT ... FROM memories WHERE workspace_id = ? AND memory_id = ?`; any state.
- search: the retrieval statement is `SELECT ... FROM memories WHERE workspace_id = ? AND state = 'active' ORDER BY updated_at DESC, memory_id ASC`; time ranges, `type`/`scope` exact filters, keyword substring matching, and score are all computed by the application layer over these returned rows using the deterministic algorithm from protocol §7.1 (filters are not pushed down into SQL and do not change the result set's semantics; rows with `type IS NULL` do not match any non-empty `types` filter).
- list: keyset pagination. First page: `SELECT ... FROM memories WHERE workspace_id = ? ORDER BY updated_at DESC, memory_id ASC LIMIT ?`; subsequent pages: `SELECT ... FROM memories WHERE workspace_id = ? AND (updated_at < ? OR (updated_at = ? AND memory_id > ?)) ORDER BY updated_at DESC, memory_id ASC LIMIT ?`. Fetch `limit + 1` rows each time to determine whether another page follows; the cursor encodes the previous page's last-row `(updated_at, memory_id)` and is an opaque string. The cursor payload is authenticated with HMAC-SHA256 keyed by `meta_info.cursor_key`; a cursor that cannot be decoded or whose HMAC does not verify returns `invalid_request`, so a caller cannot forge a cursor to change the pagination starting point.

### 3.5 File deletion

After the meta transaction commits, `workspace-delete` removes `workspace-{WID}-memory.db` and its `-wal` and `-shm` files: first best-effort delete the `-wal`/`-shm`, then delete the main file; the main file must be deleted successfully, otherwise return `internal_error`. The mapping is already gone and cannot be rolled back, and leftover files are never accessed again because WIDs are not reused.

## 4. Open validation and corruption handling

- The `meta.db` and memory DB paths must be regular files and not symlinks, otherwise return `home_broken` / `workspace_broken` respectively.
- `PRAGMA application_id` and `PRAGMA user_version` must match the §1 identifiers: a wrong application_id → `home_broken` / `workspace_broken`; an unsupported user_version → `schema_mismatch`.
- The header row must exist: `meta_info`'s `schema_version = 2` and its `cursor_key` must be a valid 32-byte hex value; in the memory DB's `memory_info`, `workspace_id` must match the WID being opened and `schema_version = 2`.
- Every entry in the `memory/` directory must be a regular file, not a symlink, with a name matching the `workspace-{decimal WID}-memory.db`, `-wal`, or `-shm` pattern; any other entry → `workspace_broken`. Files whose names match the pattern but whose WID is unregistered are leftovers from `workspace-delete`; they are not treated as anomalies and are never opened.
- Running `PRAGMA integrity_check` on every open is not required; when corruption is detected, report it as `workspace_broken` / `home_broken` and never silently rebuild or clear data. `workspace-delete` is the only channel for cleaning up a half-corrupted workspace.

## 5. Command-transaction matrix

| Command | WID lock | meta transaction | memory DB transaction | Commit order |
|---|---|---|---|---|
| `workspace-resolve` | none | short read | none | — |
| `workspace-register` | none (coordinated by the meta write transaction + O_EXCL DB creation) | write | DB creation (no transaction) | create DB after meta commit |
| `workspace-rebind` | write | write | none | — |
| `workspace-clear` | write | read (validate mapping) | write | release lock after memory commit |
| `workspace-delete` | write | write (delete mapping) | none (delete files) | delete files after meta commit |
| `memory-search` | read | read (validate mapping) | read | — |
| `memory-list` | read | read (validate mapping) | read | — |
| `memory-get` | read | read (validate mapping) | read | — |
| `memory-create` / `update` / `supersede` / `invalidate` / `delete` | write | read (validate mapping) | write | release lock after memory commit |

The lock is held for the command's entire execution; read commands hold the read lock, write commands hold the write lock, and a workspace-level write command's write lock excludes all memory access for that WID.
