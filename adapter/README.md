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

The bundle registers 13 tools:

- Workspace: `workspace_resolve`, `workspace_register`, `workspace_rebind`,
  `workspace_clear`, `workspace_delete`
- Memory: `memory_search`, `memory_list`, `memory_get`, `memory_create`,
  `memory_update`, `memory_supersede`, `memory_invalidate`, `memory_delete`

`version` and `help` belong to the Go executable and are not Harness tools.
The request and response contract is defined in
[`../doc/en/protocol-v1-proposal.md`](../doc/en/protocol-v1-proposal.md).

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
- Tool callers may omit `workspace_id`. The adapter resolves the agent
  session's current working directory with `workspace-resolve` and supplies
  the resulting ID. An unregistered directory produces an error that points to
  `workspace_register`.
- `workspace_resolve` uses the session working directory when `path` is
  omitted. Registration and rebind still require an explicit path.
- A timeout, signal, missing executable, or malformed core response is an
  unknown result. The adapter never retries a write automatically.

User-facing cards avoid raw protocol fields. Model output keeps `memory_id` and
`version` unchanged when they are needed for a later write; other displayable
values, including states and dates, are rendered in natural language.

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
`pnpm test:integration` builds the Go core and exercises a complete lifecycle.

Harness packages are peer dependencies in production. They are also listed as
development dependencies so this directory can type-check and test on its own.
