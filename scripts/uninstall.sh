#!/usr/bin/env bash
# Uninstall dsh-memory-note on Unix-like systems:
#   1. remove the bundle from a dsh profile,
#   2. remove the Go core executable from GOBIN when it is present,
#   3. ask whether to delete the whole memory-data directory, guarded by
#      hard safety checks (see below).
#
# For Windows, use scripts/uninstall.ps1. This script assumes a POSIX shell
# and the standard Unix data directory (~/.local/share/dsh-memory-note). It
# performs no platform detection of its own.
#
# Left untouched: the source checkout, adapter node_modules/dist.
#
# Usage: scripts/uninstall.sh [profile]
#   profile — the dsh profile to remove from (default: web).
#
# The memory-data directory is the DSH_MEMORY_NOTE_HOME environment variable
# when set, otherwise ~/.local/share/dsh-memory-note. It is only offered for
# deletion when every hard check passes; otherwise it is never deleted.

set -euo pipefail

PROFILE="${1:-web}"

command -v dsh >/dev/null 2>&1 || { echo "error: dsh is required" >&2; exit 1; }

# Removing an already-absent plugin must not abort the rest of the cleanup.
echo "==> removing bundle from profile '$PROFILE'"
if dsh plugin --profile "$PROFILE" remove dsh-memory-note 2>/dev/null; then
  :
else
  echo "    (bundle was not present; continuing)"
fi

# The core lives in GOBIN (falling back to GOPATH/bin) — the same place
# `go install` writes it. Remove it when go is available and the file exists.
if command -v go >/dev/null 2>&1; then
  BINDIR="$(go env GOBIN)"
  if [ -z "$BINDIR" ]; then
    BINDIR="$(go env GOPATH)/bin"
  fi
  CORE="$BINDIR/dsh-memory-note"
  if [ -e "$CORE" ]; then
    echo "==> removing core binary"
    rm -f "$CORE"
  fi
fi

# ---------------------------------------------------------------------------
# Memory-data directory deletion. The safety checks are the point here: a
# wrong or hostile DSH_MEMORY_NOTE_HOME (e.g. "~", "/", or the user's home)
# must never turn this into a recursive delete of something else. We only
# ever delete a directory that (a) resolves to a real, dedicated-looking data
# directory, and (b) the user confirms twice by typing the absolute path.
# ---------------------------------------------------------------------------

# Resolve the data directory like the core on Unix: env override, else default.
if [ -n "${DSH_MEMORY_NOTE_HOME:-}" ]; then
  DATA_DIR="$DSH_MEMORY_NOTE_HOME"
else
  DATA_DIR="${HOME:-}/.local/share/dsh-memory-note"
fi

# Hard check 1: the candidate must be non-empty and absolute.
case "$DATA_DIR" in
  "" )
    echo "skip: no data directory resolved" >&2
    DATA_DIR=""
    ;;
  /* )
    : # absolute -> pass
    ;;
  * )
    echo "skip: data directory is not an absolute path: $DATA_DIR" >&2
    DATA_DIR=""
    ;;
esac

# Hard check 2: the candidate must exist and be a directory.
if [ -n "$DATA_DIR" ] && [ ! -d "$DATA_DIR" ]; then
  echo "skip: data directory does not exist (nothing to delete): $DATA_DIR" >&2
  DATA_DIR=""
fi

# Hard check 3: never the filesystem root or the user's home.
dangerous_path() {
  local p="$1"
  # Canonicalize for comparison (resolves symlinks and trailing slashes).
  local canon
  canon="$(cd "$p" 2>/dev/null && pwd -P)" || return 1
  local homecanon=""
  if [ -n "${HOME:-}" ]; then
    homecanon="$(cd "$HOME" 2>/dev/null && pwd -P || echo "$HOME")"
  fi
  # Filesystem root.
  [ "$canon" = "/" ] && return 0
  # The user's home directory itself.
  [ -n "$homecanon" ] && [ "$canon" = "${homecanon%/}" ] && return 0
  return 1
}

if [ -n "$DATA_DIR" ] && dangerous_path "$DATA_DIR"; then
  echo "skip: refusing to delete a root/home directory: $DATA_DIR" >&2
  echo "       remove its contents yourself if you are sure." >&2
  DATA_DIR=""
fi

# Hard check 4: sentinel-file check. Only a directory that actually looks like
# this tool's data directory (contains meta.db at the top level) is eligible.
if [ -n "$DATA_DIR" ] && [ ! -f "$DATA_DIR/meta.db" ]; then
  echo "skip: '$DATA_DIR' does not contain meta.db — not a dsh-memory-note data" >&2
  echo "       directory, or it holds something else. Refusing to delete." >&2
  DATA_DIR=""
fi

# If everything passed, ask — twice — before deleting.
if [ -n "$DATA_DIR" ]; then
  echo
  echo "The memory-data directory is:"
  echo "  $DATA_DIR"
  printf "Delete this entire directory? [y/N] "
  read -r CONFIRM1
  if [ "${CONFIRM1:-}" = "y" ] || [ "${CONFIRM1:-}" = "Y" ]; then
    printf "Type the absolute path to confirm deletion: "
    read -r CONFIRM2
    # Compare canonically to defeat trailing-slash / case tricks.
    TYPED_CANON="$(cd "$CONFIRM2" 2>/dev/null && pwd -P)" || TYPED_CANON=""
    DATA_CANON="$(cd "$DATA_DIR" 2>/dev/null && pwd -P)"
    if [ -n "$TYPED_CANON" ] && [ "$TYPED_CANON" = "$DATA_CANON" ]; then
      echo "==> deleting $DATA_DIR"
      rm -rf -- "$DATA_DIR"
      echo "deleted."
    else
      echo "aborted: path does not match the data directory exactly." >&2
    fi
  else
    echo "skipped: memory-data directory left untouched."
  fi
fi

echo
echo "done. If you wrote a binaryPath override into \$DSH_HOME/cordis.patch.yml,"
echo "remove that memory-note row yourself."
