# dsh-memory-note adapter

A DeepSeek Harness plugin bundle that exposes the `dsh-memory-note` Go core as
Harness tools.

```text
DeepSeek Harness -> adapter bundle -> Go core -> SQLite
```

The adapter is intentionally thin. It registers tools, starts the core without
a shell, supplies the current workspace when possible, requests approval for
writes, and passes the core response back to the model.

## Tools

The bundle registers 15 tools:

- Workspace: `workspace_resolve`, `workspace_register`, `workspace_rebind`,
  `workspace_clear`, `workspace_delete`
- Memory: `memory_search`, `memory_list`, `memory_get`, `memory_history`,
  `memory_diff`, `memory_create`, `memory_update`, `memory_supersede`,
  `memory_invalidate`, `memory_delete`

`version` and `help` belong to the Go executable and are not Harness tools.
The request and response contract is defined in
[`../doc/en/protocol-v2-proposal.md`](../doc/en/protocol-v2-proposal.md).

## Configuration

Add the bundle to a Cordis profile and configure it as follows:

```yaml
- id: memory-note
  name: dsh-memory-note
  config:
    binaryPath: dsh-memory-note # default; an absolute path also works
    timeoutMs: 30000            # default timeout for one core invocation
    home: /absolute/path        # optional DSH_MEMORY_NOTE_HOME override
```

`binaryPath` is resolved through `PATH` unless it is absolute. When `home` is
set, the adapter exports it as `DSH_MEMORY_NOTE_HOME` for the child process.

## Runtime behavior

- Read tools run directly. Every write tool asks for Harness approval before
  the core process starts; only an `allowed-once` decision proceeds.
- Before the first tool call, the adapter checks that the core and adapter use
  the same protocol major version. A mismatch reports which core was found and
  asks the user to reinstall it instead of forwarding an incompatible request.
- Tool callers may omit `workspace_id`. The adapter resolves the agent
  session's current working directory with `workspace-resolve` and supplies
  the resulting ID. An unregistered directory produces an error that points to
  `workspace_register`.
- `workspace_resolve` uses the session working directory when `path` is
  omitted. Registration and rebind still require an explicit path.
- A timeout, signal, missing executable, or malformed core response is an
  unknown result. The adapter never retries a write automatically.

User-facing cards and approval descriptions avoid internal handles. Model
output keeps `memory_id` and `version` unchanged when they are needed for a
later write; other displayable values, including states and dates, are rendered
in natural language. Memory tool descriptions tell the model not to repeat IDs,
versions, citations, or protocol metadata in user-facing replies unless the
user asks for them. This is guidance, not a confidentiality boundary: the
adapter does not control the model's final text. `memory_history` gives the
model version numbers as handles for precise rollback reads, while user cards
show only counts and content.

## Bounded context rendering

Model-facing renders are kept inside an explicit budget so a large search or a
64 KiB memory never crowds out the live prompt:

- at most 8 memories per search render, one memory's content truncated to
  2000 UTF-8 bytes, and a whole search render to 16000 bytes (with an ellipsis
  on a code-point boundary);
- a `search` render is prefixed with a trust notice marking it as untrusted
  history that never overrides the current request, instructions, or repository
  rules;
- each hit carries its `citation` (memory_id + version) as the handle for a
  later version-safe write (§4).

Git branch scoping: `memory_search` auto-detects the agent's current branch
(`DSH_MEMORY_NOTE_GIT_BRANCH`, then `.git/HEAD`); a non-git or detached-HEAD
workspace simply omits the branch and sees everything.

## Install

From a local checkout:

```sh
dsh plugin --profile demo add link:./adapter
```

From a Git repository:

```sh
dsh plugin --profile demo add github:you/repo#<commit-sha>
```

`prepare` builds `dist/` from `src/` with `tsc`, so the bundle can be installed
without a monorepo checkout.

## Development

```sh
pnpm install
pnpm check
pnpm test:integration
```

`pnpm check` runs type checking and unit tests with a scripted fake core.
`pnpm test:integration` builds the real Go core in a temporary directory, loads
the adapter with simulated Harness tool and approval services, and exercises a
complete lifecycle against an isolated temporary HOME. It does not start DSH or
a browser profile.

Harness packages are peer dependencies in production. They are also listed as
development dependencies so this directory can type-check and test on its own.
