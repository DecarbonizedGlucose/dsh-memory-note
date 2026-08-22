#!/usr/bin/env bash
# Uninstall dsh-memory-note:
#   1. remove the bundle from a dsh profile,
#   2. remove the Go core executable from GOBIN when it is present.
#
# Left untouched: the source checkout, adapter node_modules/dist, and the
# memory data under ~/.local/share/dsh-memory-note. Delete those manually if
# you no longer need them.
#
# Usage: scripts/uninstall.sh [profile]
#   profile — the dsh profile to remove from (default: web).

set -euo pipefail

PROFILE="${1:-web}"

command -v dsh >/dev/null 2>&1 || { echo "error: dsh is required" >&2; exit 1; }

echo "==> removing bundle from profile '$PROFILE'"
dsh plugin --profile "$PROFILE" remove dsh-memory-note

# The core lives in GOBIN; remove it when go is available and the file exists.
if command -v go >/dev/null 2>&1; then
  CORE="$(go env GOPATH)/bin/dsh-memory-note"
  if [ -e "$CORE" ]; then
    echo "==> removing core binary"
    rm -f "$CORE"
  fi
fi

echo
echo "done. If you wrote a binaryPath override into \$DSH_HOME/cordis.patch.yml,"
echo "remove that memory-note row yourself. Memory data under"
echo "~/.local/share/dsh-memory-note was left untouched."
