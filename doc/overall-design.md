# Overall Design

This document defines the overall design of `dsh-memory-note`. It fixes the runtime structure, component responsibilities, storage boundaries, concurrency model, and memory lifecycle. The exact JSON request, response, and error contracts are defined separately by `protocol-v1-proposal.md`.

The project is local and lightweight. Its core responsibilities are safe one-shot execution, workspace identity independent of paths, cross-process coordination, explicit memory state transitions, optimistic version checks, and a strict user-approval boundary.

## 1. Runtime architecture

```text
DeepSeek Harness / future coding agents
                  |
        TypeScript adapter
                  |
       one-shot process + JSON
                  |
          Go executable
                  |
             SQLite files
```

The Go executable uses one-shot mode. Each process invocation executes exactly one subcommand:

```text
dsh-memory-note <subcommand> '<json request>'
```

Each public operation therefore has three corresponding interfaces:

1. a subcommand name;
2. an RPC function handled by the Go executable;
3. an independent JSON request/response contract.

The executable writes exactly one protocol response to stdout for every business command. Diagnostics are written to stderr. `version` prints a single-line version string to stdout and exits 0; `help` prints usage to stderr and exits 0; neither emits a JSON response, and neither is exposed as a Harness tool. The exact command and exit-code contract is defined by the protocol document.

### 1.1 Session

A `Session` is created for every one-shot invocation and is the highest-level runtime component manager. It is not a persisted Harness conversation and does not represent long-term memory data.

A Session owns the resources needed by that invocation:

- a cryptographically random session ID;
- the resolved `DSH_MEMORY_NOTE_HOME` path;
- a private temporary directory;
- the RPC dispatcher used for the selected subcommand;
- the SQLite connections and transactions opened by the invocation;
- the workspace read or write locks acquired under its session ID.

The Session lifecycle is fixed:

1. parse the process arguments sufficiently to select the subcommand;
2. create the session ID and private temporary directory;
3. resolve and validate `DSH_MEMORY_NOTE_HOME`;
4. initialize `DSH_MEMORY_NOTE_HOME` atomically if it does not exist;
5. strictly decode and validate the JSON request;
6. acquire the required workspace lock;
7. execute the RPC function and commit its SQLite transaction;
8. release locks, close databases, and remove the temporary directory;
9. emit one response and exit.

Cleanup is attempted on every normal return path. Lock expiration is only a fallback for crashes, forced termination, or cleanup failure.

### 1.2 `main()` and RPC dispatch

`main()` is a thin process entry point. It only creates the Session, selects one subcommand, delegates to its RPC function, serializes the result, and chooses the exit code.

`main()` must not contain SQL, memory state transitions, workspace migration logic, or duplicated request validation. Each subcommand has one clearly named handler and one explicit request/response type. Shared helpers are introduced only for behavior actually shared by several commands.

No daemon, background service, network listener, or resident in-memory state is required.

## 2. Storage layout

All persistent databases use SQLite and the `.db` extension. The complete SQL schema, statement semantics, and storage invariants are defined separately by `sql-standard.md`. The storage layout is:

```text
DSH_MEMORY_NOTE_HOME/
├── meta.db
└── memory/
    ├── workspace-{WID}-memory.db
    └── ...
```

The default `DSH_MEMORY_NOTE_HOME` is `~/.local/dsh-memory-note/`. It may be overridden by the `DSH_MEMORY_NOTE_HOME` environment variable. The configured value must resolve to an absolute path.

The workspace itself stores no plugin database, WID marker, lock file, or other plugin-owned state. Moving or deleting a workspace directory therefore does not implicitly move or delete its memory database.

In the remainder of this document, **HOME** means `DSH_MEMORY_NOTE_HOME`, not the operating-system `$HOME` directory. This alias is not used in the code.

### 2.1 `meta.db`

`meta.db` stores metadata shared across workspaces:

- `workspace_id`, also called WID, as a non-negative integer assigned by SQLite;
- the workspace's current normalized absolute path;
- workspace creation and update times and schema metadata;
- the per-WID cross-process read/write lock records.

`meta.db` does not store memory content. It is the authority for resolving a path to a WID and for coordinating operations that may race with workspace rebinding, clearing, or deletion.

The workspace name is not an identity. Directories with the same name but different absolute paths may have different WIDs. A WID is not a prefixed string or random token. The memory database filename depends only on the decimal WID, for example `workspace-12-memory.db`.

WIDs are stable and are not reused after workspace deletion.

### 2.2 Workspace memory database

Each workspace has one dedicated `workspace-{WID}-memory.db`. It stores only that workspace's memories, states, versions, metadata, sources, replacement relationships, and archived historical versions.

No query or transaction may combine memory rows from different workspace databases. The WID from `meta.db`, not a request-supplied file path, determines which database file is opened.

SQLite transactions remain responsible for atomic changes inside a memory database. The cross-process lock in `meta.db` coordinates access to the database file and workspace lifecycle; it does not replace SQLite transactions.

### 2.3 HOME initialization and integrity

Every Session checks HOME before executing business logic.

- If HOME does not exist, the Session initializes a complete `meta.db` and `memory/` layout and then continues. Because this is the first time the workspace is involved (a database with no related data is also treated as first-time), no user consent is required.
- If HOME exists and contains a regular-file `meta.db` and a directory `memory/`, the Session continues after schema validation.
- If HOME exists but either item is missing, has the wrong type, or is otherwise untrusted, the Session returns `home_broken`. It must not silently rebuild, truncate, or replace existing data.

Initialization is built completely in a sibling temporary directory and published by an atomic rename. Concurrent initializers may race to publish, but after one succeeds, the others must discard their temporary layouts and reopen the published HOME.

The implementation must reject unsafe path types such as symbolic links where they could redirect database or temporary-file operations outside HOME. Temporary names must be derived from the random session ID, created with private permissions, and never contain raw memory content.

## 3. Cross-process read/write locks

One-shot mode allows several Go processes to operate concurrently. `meta.db` provides read/write locks isolated by WID.

The required lock mode is determined by the operation:

| Operation | Lock |
|---|---|
| memory search or get | read |
| memory create, update, supersede, invalidate, or delete | write |
| workspace rebind, clear, or delete | write |
| workspace registration | write coordination while creating the mapping and database |

The lock rules are:

- several read locks for the same WID may coexist;
- a write lock is mutually exclusive with every read or write lock for that WID;
- operations for different WIDs do not block one another;
- rebinding, clearing, deleting, or migrating a workspace excludes all memory access for that WID;
- lock acquisition and conflict checks are performed atomically in a short `meta.db` transaction;
- each lock records its mode, owning session ID, acquisition time, and expiration time;
- the owning Session releases its locks on normal completion;
- expired locks may be reclaimed so that a crashed process cannot block the workspace permanently;
- lock acquisition has a bounded wait time and returns `workspace_busy` on timeout.

If an operation can run long enough to approach its expiration time, its Session renews the lease while it remains the owner. Before starting a modifying transaction, it must verify that it still owns the write lock. If ownership has been lost, the operation aborts without starting a new write.

This lease design coordinates normally executing processes and recovers from ordinary crashes. It does not claim to make a process paused beyond its complete lease period safe to resume; that stronger failure model would require an additional fencing mechanism and is outside the current design.

## 4. SQLite and versioning rules

Memory database transactions provide atomicity for each command:

- `memory-create`, `memory-update`, `memory-invalidate`, and `memory-delete` each complete in one transaction;
- `memory-supersede` changes the old record, creates the new record, and establishes both relationship fields in one transaction;
- `workspace-clear` removes all memory rows in one transaction;
- `memory-search` and `memory-list` run in read-only transactions and never mutate data;
- a successful response is emitted only after the transaction commits.

All SQL values use bound parameters. Foreign-key enforcement is enabled for every connection. SQLite busy handling is bounded; it must not cause an invocation to wait forever.

Each memory has a monotonically increasing `version`. Mutating an existing memory requires `expected_version`; comparison and mutation occur in the same transaction. A mismatch returns `version_conflict` without changing data.

This is optimistic concurrency control with MVCC-like version semantics at the application level. It prevents stale LLM proposals or concurrent Sessions from silently overwriting a newer fact. It is not a promise that every historical version remains queryable.

Every successful `memory-update`, `memory-supersede`, or `memory-invalidate` archives the replaced record as an internal historical version. `memory-delete` removes the current record together with all of its archived versions, and `workspace-clear` removes all records and all history in one transaction. Historical versions are internal audit data and are never exposed through the JSON protocol.

## 5. TypeScript layer responsibilities

The TypeScript adapter is currently specific to DeepSeek Harness. It is deliberately thin.

It is responsible for:

- registering Harness tools;
- mapping each tool call to one subcommand and JSON request;
- passing the current workspace path or WID required by that request;
- invoking the Go executable without a shell;
- parsing the single JSON response;
- using Harness's user-approval mechanism before invoking any write operation.

It does not implement storage, locks, transactions, memory state transitions, or a second copy of request semantics.

The LLM may directly invoke read operations without user approval. It may detect that a memory contradicts current facts and propose `update`, `supersede`, or `invalidate`, but it cannot approve its own proposal. Approval is a Harness interaction, not a JSON boolean that the LLM can set.

The Go executable mechanically enforces protocol, state, version, transaction, and integrity rules. The LLM decides semantic intent; the user authorizes effects.

## 6. Memory model and subcommands

A memory is not merely a content row. It includes an identity, workspace ownership, content and metadata, source information, state, version, timestamps, and replacement relationships. The complete field-level contract is defined by the JSON protocol.

The normal state transitions are:

| Subcommand | Meaning | Result |
|---|---|---|
| `memory-create` | record a new fact | new active memory with a new ID |
| `memory-update` | revise the same fact | same ID, version incremented |
| `memory-supersede` | replace an old fact with a new fact | old record retained as superseded; new active ID created; relationship established |
| `memory-invalidate` | declare a fact invalid without a replacement | record retained as invalid |
| `memory-delete` | explicitly remove a record | row physically deleted from the current database |

The eight public memory subcommands are:

### `memory-search`

Retrieves candidate memories by query and filter. Filtering dimensions include keywords, type, scope, and time. It is a candidate retrieval operation, not a semantic decision that a memory is relevant or true.

### `memory-get`

Reads one memory precisely by `memory_id`, including its full content, current version, source, state, and replacement relationship. This is the fine-grained read used before proposing a version-sensitive write.

### `memory-list`

Lists the memories of a workspace in any state, ordered by `updated_at` and then `memory_id`, with cursor pagination. It returns compact rows without content, sources, or metadata; `memory-get` is the fine-grained read for a full record.

### `memory-create`

Creates a completely new memory and generates a new `memory_id`.

### `memory-update`

Modifies the content or metadata of the same fact. The `memory_id` remains unchanged and the version increments. It must not be used when the old and new facts should both remain visible as a replacement chain.

### `memory-supersede`

Replaces an old fact with a new one. The new fact receives a new `memory_id`; the old record is retained as `superseded`; and a `supersedes` / `superseded_by` relationship is established atomically.

### `memory-invalidate`

Marks a memory as invalid when no new fact replaces it. The record remains available for explicit retrieval and audit of why it is no longer active.

### `memory-delete`

Physically deletes a memory row. It is reserved for an explicit user deletion request, accidental persistence, or sensitive information removal. Normal staleness uses `supersede` or `invalidate`, not `delete`. Physical deletion from SQLite does not guarantee secure erasure from storage media, filesystem snapshots, or backups.

## 7. Workspace identity and subcommands

### `workspace-register`

Assigns a WID to an absolute workspace path, records the mapping in `meta.db`, and atomically prepares the corresponding memory database. Registering the same normalized path again returns the existing WID instead of creating a second workspace.

### `workspace-resolve`

Looks up the WID and metadata currently associated with a normalized absolute path. It does not create a workspace or memory database.

### `workspace-rebind`

After a directory is moved or renamed, binds the existing WID to a new absolute path. It modifies only `meta.db`; the memory database is neither moved nor renamed because its filename depends only on WID.

Rebinding is the normal migration operation for a workspace whose storage identity remains the same. The new path must not already belong to another WID.

### `workspace-clear`

Retains workspace metadata, WID, path binding, and database files, but removes all memories from the workspace database.

### `workspace-delete`

Deletes the workspace mapping and physically deletes its memory database and SQLite sidecar files. This operation does not delete the user's workspace directory. A deleted WID is not reassigned. A workspace with corrupted data (metadata only, with no memory database) can be cleaned up completely through this command.

## 8. Failure boundaries

The response envelope and stable error codes are defined by the JSON protocol. The following design rules apply regardless of the command:

- stdout contains only the protocol response; diagnostics use stderr;
- a command never reports success before its transaction or required filesystem operation has completed;
- malformed or unknown JSON fields are rejected instead of ignored;
- a broken HOME or missing workspace database is reported, not silently replaced with an empty database; `workspace-delete` is the sole exception, as the designated cleanup path for a workspace whose memory database is missing or corrupt;
- a lock timeout is reported as `workspace_busy`;
- a stale memory write is reported as `version_conflict`;
- an invalid state transition is rejected without modifying the record;
- temporary files are removed on normal completion and contain no raw request or memory content unless an operation explicitly requires staged bytes.

If the process terminates before emitting a valid response, the adapter reports an unknown result. It must inspect current state through read operations before proposing another write; it must not blindly retry by changing `expected_version`.

## 9. Design principles

1. One process invocation executes one explicit operation and owns one short-lived Session.
2. Workspace identity is the WID; paths are mutable metadata.
3. Memory databases are isolated by WID and remain outside user workspaces.
4. Concurrent reads are allowed; writes and workspace lifecycle operations are exclusive per WID.
5. SQLite transactions protect database atomicity; `meta.db` locks coordinate processes.
6. Version checks prevent stale writes without requiring full historical-version storage.
7. The LLM proposes semantic changes, the user approves writes, and Go enforces mechanical safety.
8. Invalid or ambiguous state is reported explicitly rather than silently repaired.
9. The JSON protocol is the external contract, `sql-standard.md` pins the storage semantics, and the design document explains why the components and rules exist.

## 10. Versioning and compatibility

There is exactly one user-facing version: the **application version** (currently `1.0.0`), shared by the Go core and the TypeScript adapter bundle. Its single source is `internal/version`; the `version` subcommand prints it and the adapter mirrors it in `package.json`. Release builds may override it with `-ldflags "-X .../internal/version.Version=X.Y.Z"`.

- **The protocol version tracks the major component.** Releases `1.x.x` implement protocol `v1`; a protocol breaking change (protocol §11) becomes protocol `v2` and bumps the application version to `2.0.0`. The core and the adapter must share the same major version and protocol.
- **The SQL schema version is an internal migration counter and never follows releases.** It increments only when table structures change; opening a database checks it and fails closed with `schema_mismatch` — data is never silently migrated or rebuilt. A `1.0.x` bugfix release therefore never invalidates existing databases.

Version numbers are labels; compatibility comes from the mechanisms that are actually exercised: the shared Go/TS request fixtures pin the core/adapter contract, the `schema_version` gate pins databases, and every mismatch fails closed instead of being repaired silently.
