# Overall Design

## 1. Runtime architecture

```text
DeepSeek Harness / future coding agents
                  |
        TypeScript adapter
                  |
       one-shot process + JSON
                  |
             Go binary
```

Go binary uses one-shot mode. Only one subcommand is executed per process call：

```text
dsh-memory-note <subcommand> '<json request>'
```

A `Session` holds all metadata for a single call and is the highest-level component manager.

Each public function corresponds to:
1. A subcommand;
2. An RPC function;
3. A set of independent request/response protocols.


The resulting JSON is written to stdout, and diagnostics are written to stderr. Internal Go functionalities don't need to be exposed to TypeScript, such as `version`.

## 2. Storage layout

All persistent databases use SQLite, and the file extension is uniformly `.db`. The default root directory is fixed as follows:

```text
DSH_MEMORY_NOTE_HOME
├── meta.db
└── memory/
    ├── workspace-{WID}-memory.db
    └── ...
```

The default `DSH_MEMORY_NOTE_HOME` is `~/.local/dsh-memory-note/` which can be overwritten by overriden with the `DSH_MEMORY_NOTE_HOME` environment variable.

Next, we will refer to `DSH_MEMORY_NOTE_HOME` as `HOME`.

### 2.1 meta.db

`meta.db` only stores metadata across workspaces：

- `workspace_id` aka `WID`, a non-negative integer, assigned by SQLite;
- The current absolute path of the workspace;
- A small amount of metadata, such as creation and update times;
- Cross-process read-write locks for workspace memory DB.

The workspace name is not an identity. Directories with the same name but different absolute paths can have different WIDs. A WID is not a prefixed string or a random combination of characters. The memory DB filename depends only on the decimal WID, so the memory DB does not need to be moved or renamed after the directory is moved, for example, `workspace-12-memory.db`.

### 2.2 workspace memory DB

Each workspace has its own dedicated `workspace-{WID}-memory.db`. This database only stores the memories, state, and relationships of that workspace, and does not store data from other workspaces.

### 2.3 Cross-process read-write locks

One-shot mode allows multiple Go processes to access the same workspace simultaneously. `meta.db` must provide distributed read-write locks isolated by WID:

- Acquire a read lock for memory reads;

- Acquire a write lock for memory creation/modification/clearing/entire database deletion/rebinding;

- Multiple read locks can coexist;

- Write locks are mutually exclusive with any other lock;

- During workspace migration, other processes must not read from or write to the corresponding memory DB;

- Locks must have an owner and an expiration time to prevent permanent blocking after abnormal process exit.

Locks only coordinate processes within this project and do not replace SQLite's own transactions.

### 2.4 Launching session and HOME integrity

Each one-shot call first creates a short-lived `Session`. The `Session` owns the RPC server to be used and generates a random session ID for creating cross-process read-write locks and a temporary directory. Then, the integrity of the HOME directory is checked. If the check fails, it should return immediately.

- HOME does not exist: Initialize a complete `meta.db` and `memory/`, then proceed with the business logic call.

- HOME exists and contains both the regular file `meta.db` and the directory `memory/`: Proceed with the business logic call.

- HOME exists but is missing any one of these items, or is of an incorrect type: Return `home_broken`, indicating that the data is untrusted and requiring the user to manually clean it up; the program should not automatically rebuild it.

Initialization is first completed in a sibling temporary directory, then the entire directory is renamed to HOME to prevent concurrent processes from seeing a partially initialized directory.

## 3. TS Layer Responsibilities

- Currently only compatible with DeepSeek Harness.

- LLM can directly call read functions without user approval.

- Any write operation must go through Harness's user approval mechanism.

- LLM can detect if a memory entry contradicts the current facts and suggest update, supersede, or invalidate.

- LLM cannot approve its own write operations.

- TS only handles tool registration, user approval, and process invocation; it does not implement storage services.

## 4. Memory Subcommands

The core memory subcommands currently provide the following seven:

### `memory-search`

Retrieves candidate memories by query and filter. Target filtering dimensions include keywords, type, scope, and time.

### `memory-get`

Reads a memory precisely by `memory_id`, returning the complete content, current version, source, status, and replacement relationship.

### `memory-create`

Creates a completely new memory, generating a new `memory_id`.

### `memory-update`

Modifies the content or metadata of the same fact. The `memory_id` remains unchanged, but the version number increments.

### `memory-supersede`

Replaces an old fact with a new one. The new fact acquires a new `memory_id`; the old record is retained and marked as `superseded`, establishing a `supersedes` / `superseded_by` relationship between the two.

### `memory-invalidate`

Declares a memory as invalid, but no new facts replace it. The record is retained, and its status is changed to `invalid`.

### `memory-delete`

Physically deletes a memory. Only used for explicit user requests for deletion, sensitive information erasure, or accidental overwriting; normal expiration should not rely on `delete`.

## 5. Workspace subcommands

### `workspace-register`

Assigns a WID to an absolute workspace path, records the mapping in `meta.db`, and prepares the corresponding memory database.

### `workspace-rebind`

After a directory is moved or renamed, binds the existing WID to the new absolute path. Only modifies `meta.db`, does not move the memory database. Users should ensure that the new directory exists afterward.

### `workspace-clear`

Retains the workspace metadata and memory database files, but clears all memories within them.

### `workspace-delete`

Deletes the workspace metadata and physically deletes the memory database.

