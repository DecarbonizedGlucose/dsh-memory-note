# dsh-memory-note

**English** | [中文](README_zh.md)

A lightweight, local, cross-session memory core for DeepSeek Harness: a
one-shot Go executable plus a thin TypeScript adapter bundle.

```text
DeepSeek Harness -> adapter (bundle) -> Go core -> SQLite
```

## Quick start

Requirements: `go`, `pnpm`, and `dsh` on PATH.

**Unix-like (Linux / macOS / BSD):**

```sh
git clone https://github.com/DecarbonizedGlucose/dsh-memory-note
cd dsh-memory-note
scripts/install.sh [profile]    # default profile: web
```

**Windows (PowerShell):**

```powershell
git clone https://github.com/DecarbonizedGlucose/dsh-memory-note
cd dsh-memory-note
.\scripts\install.ps1 [-Profile web]
```

The install script builds the Go core into `GOBIN`, builds the adapter bundle,
and registers it into the profile via `dsh plugin ... add link:`.

After installing, make sure your `GOBIN` (`$(go env GOPATH)/bin`) is on PATH
and restart the profile (`dsh --profile <name>`) so the tools load.

Uninstall:

```sh
scripts/uninstall.sh [profile]         # Unix-like
```

```powershell
.\scripts\uninstall.ps1 [-Profile web] # Windows
```

The uninstall script removes the bundle and the core binary, then asks whether
to delete the whole memory-data directory (after hard safety checks — it never
deletes a directory that is not clearly this tool's data directory).

## Usage

One invocation runs exactly one subcommand:

```text
dsh-memory-note <subcommand> '<json-request>'
```

- Workspace: `workspace-resolve`, `workspace-register`, `workspace-rebind`,
  `workspace-clear`, `workspace-delete`
- Memory: `memory-search`, `memory-list`, `memory-get`, `memory-history`,
  `memory-diff`, `memory-create`, `memory-update`, `memory-supersede`,
  `memory-invalidate`, `memory-delete`
- Internal: `version`, `help`

The full JSON contract is in
[`doc/en/protocol-v2-proposal.md`](doc/en/protocol-v2-proposal.md).

## Storage layout

```
DSH_MEMORY_NOTE_HOME/               # default: see platform below
    meta.db                         # metadata
    memory/
        workspace-WID-memory.db     # one database per workspace
```

- Unix-like (Linux / macOS / BSD): `~/.local/share/dsh-memory-note`
- Windows: `%LOCALAPPDATA%\dsh-memory-note` (fallback
  `%USERPROFILE%\AppData\Local\dsh-memory-note`)

Override with `DSH_MEMORY_NOTE_HOME`.
