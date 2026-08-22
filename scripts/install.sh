#!/usr/bin/env bash
# Install dsh-memory-note from a source checkout:
#   1. build the Go core and install it into GOBIN,
#   2. build the TypeScript adapter bundle,
#   3. register the bundle into a dsh profile via a local link: install.
#
# The core is built from the checkout, so no version tag or remote is needed.
# The adapter is installed as a link: so edits to adapter/ (plus a rebuild)
# take effect on the next profile restart or HMR reload.
#
# Usage: scripts/install.sh [profile]
#   profile — the dsh profile to install into (default: web).

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROFILE="${1:-web}"

command -v go >/dev/null 2>&1 || { echo "error: go is required" >&2; exit 1; }
command -v pnpm >/dev/null 2>&1 || { echo "error: pnpm is required" >&2; exit 1; }
command -v dsh >/dev/null 2>&1 || { echo "error: dsh is required" >&2; exit 1; }

# Failure recovery: steps 1-3 are idempotent, so a plain re-run is safe. Only
# the dsh plugin step can leave a half-installed profile; in that case remove
# the bundle before retrying.
trap 'echo "error: install failed." >&2
      echo "re-run is safe for go/pnpm steps. If the plugin step failed partway:" >&2
      echo "  dsh plugin --profile $PROFILE remove dsh-memory-note" >&2
      echo "then re-run this script." >&2' ERR

# 1. Core: install the one-shot executable into GOBIN (~/go/bin by default).
echo "==> building core"
(cd "$ROOT" && go install ./cmd/dsh-memory-note)

# 2. Adapter: install dependencies (peers resolve from the checkout's own
#    node_modules) and build the dist/ bundle entry.
echo "==> building adapter"
pnpm --dir "$ROOT/adapter" install
pnpm --dir "$ROOT/adapter" build

# 3. Register the bundle. dsh forwards the args to pnpm inside the profile
#    directory; link: keeps the checkout live for development.
echo "==> installing into profile '$PROFILE'"
dsh plugin --profile "$PROFILE" add "link:$ROOT/adapter"

echo
echo "done. Make sure '$(go env GOPATH)/bin' is on PATH: the adapter defaults to"
echo "binaryPath=dsh-memory-note, which is resolved through PATH at call time."
echo "Memory data lives in ~/.local/share/dsh-memory-note unless DSH_MEMORY_NOTE_HOME is set."
echo "Restart the profile (e.g. 'dsh --profile $PROFILE') to load the tools."
