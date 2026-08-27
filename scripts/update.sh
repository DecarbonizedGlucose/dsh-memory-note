#!/usr/bin/env bash
# Rebuild an existing linked installation after the source checkout changes.
# This script replaces the Go core and rebuilds the adapter bundle. It does not
# register a plugin and never reads or writes DSH_MEMORY_NOTE_HOME.
#
# Run scripts/install.sh first. Restart the dsh profile after this script exits.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEMP_CORE=""

command -v go >/dev/null 2>&1 || { echo "error: go is required" >&2; exit 1; }
command -v pnpm >/dev/null 2>&1 || { echo "error: pnpm is required" >&2; exit 1; }

on_exit() {
  local status=$?
  if [ -n "$TEMP_CORE" ]; then
    rm -f -- "$TEMP_CORE"
  fi
  if [ "$status" -ne 0 ]; then
    echo "error: update failed; fix the error and rerun this script." >&2
  fi
  exit "$status"
}
trap on_exit EXIT

BINDIR="$(go env GOBIN)"
if [ -z "$BINDIR" ]; then
  GOPATH_VALUE="$(go env GOPATH)"
  BINDIR="${GOPATH_VALUE%%:*}/bin"
fi
case "$BINDIR" in
  /*) ;;
  *) echo "error: Go binary directory is not absolute: $BINDIR" >&2; exit 1 ;;
esac

CORE="$BINDIR/dsh-memory-note"
if [ ! -f "$CORE" ]; then
  echo "error: no installed core at '$CORE'; run scripts/install.sh first" >&2
  exit 1
fi

# Build the replacement beside the installed binary. Publishing it with mv is
# atomic on the same filesystem, so a failed build cannot damage the old core.
TEMP_CORE="$(mktemp "$BINDIR/.dsh-memory-note.update.XXXXXX")"
echo "==> building core"
(cd "$ROOT" && go build -o "$TEMP_CORE" ./cmd/dsh-memory-note)
chmod 0755 "$TEMP_CORE"

echo "==> building adapter"
pnpm --dir "$ROOT/adapter" install --frozen-lockfile
pnpm --dir "$ROOT/adapter" build

echo "==> replacing core"
mv -f -- "$TEMP_CORE" "$CORE"
TEMP_CORE=""

echo
echo "updated. Restart the dsh profile to load the rebuilt adapter."
echo "Memory data was not changed."
