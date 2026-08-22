# dsh-memory-note

A lightweight, local, cross-session memory core for DeepSeek Harness: a
one-shot Go executable plus a thin TypeScript adapter bundle.

```text
DeepSeek Harness -> adapter (bundle) -> Go core -> SQLite
```

## Storage layout

```
DSH_MEMORY_NOTE_HOME/               # default: ~/.local/share/dsh-memory-note
    meta.db                         # metadata
    memory/
        workspace-WID-memory.db     # one database per workspace
```

## Usage

One invocation runs exactly one subcommand:

```text
dsh-memory-note <subcommand> '<json-request>'
```

- Workspace: `workspace-resolve`, `workspace-register`, `workspace-rebind`,
  `workspace-clear`, `workspace-delete`
- Memory: `memory-search`, `memory-list`, `memory-get`, `memory-create`,
  `memory-update`, `memory-supersede`, `memory-invalidate`, `memory-delete`
- Internal: `version`, `help`

Memory data lives in `~/.local/share/dsh-memory-note` (override with
`DSH_MEMORY_NOTE_HOME`). The full JSON contract is in
[`doc/protocol-v1-proposal.md`](doc/protocol-v1-proposal.md).

## Install / uninstall

Requirements: `go`, `pnpm`, and `dsh` on PATH.

```sh
git clone <repo-url>
cd dsh-memory-note
scripts/install.sh [profile]    # default profile: web
scripts/uninstall.sh [profile]
```

`install.sh` builds the Go core into `GOBIN`, builds the adapter bundle, and
registers it into the profile via `dsh plugin ... add link:`. `uninstall.sh`
removes the bundle and the core binary; memory data is left untouched.

After installing, make sure `$(go env GOPATH)/bin` is on PATH and restart the
profile (`dsh --profile <name>`) so the tools load.
