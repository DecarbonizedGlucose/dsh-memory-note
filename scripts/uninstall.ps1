# Uninstall dsh-memory-note on Windows:
#   1. remove the bundle from a dsh profile,
#   2. remove the Go core executable from GOBIN when it is present,
#   3. ask whether to delete the whole memory-data directory, guarded by
#      hard safety checks (see below).
#
# For Unix-like systems, use scripts/uninstall.sh. This script assumes
# PowerShell and the Windows data directory (%LOCALAPPDATA%\dsh-memory-note).
# It performs no platform detection of its own.
#
# Left untouched: the source checkout, adapter node_modules/dist.
#
# Usage: .\scripts\uninstall.ps1 [[-Profile] <name>]
#   -Profile — the dsh profile to remove from (default: web).
#
# The memory-data directory is the DSH_MEMORY_NOTE_HOME environment variable
# when set, otherwise %LOCALAPPDATA%\dsh-memory-note (falling back to
# %USERPROFILE%\AppData\Local\dsh-memory-note). It is only offered for
# deletion when every hard check passes; otherwise it is never deleted.

[CmdletBinding()]
param(
    [string]$Profile = "web"
)

$ErrorActionPreference = "Stop"

if (-not (Get-Command dsh -ErrorAction SilentlyContinue)) {
    Write-Error "error: dsh is required"
    exit 1
}

# Removing an already-absent plugin must not abort the rest of the cleanup.
Write-Host "==> removing bundle from profile '$Profile'"
dsh plugin --profile $Profile remove dsh-memory-note
if ($LASTEXITCODE -ne 0) {
    Write-Host "    (bundle was not present; continuing)"
}

# The core lives in GOBIN (falling back to GOPATH\bin) — the same place
# `go install` writes it. Remove it when go is available and the file exists.
if (Get-Command go -ErrorAction SilentlyContinue) {
    $bindir = (go env GOBIN).Trim()
    if (-not $bindir) {
        $bindir = Join-Path (go env GOPATH).Trim() "bin"
    }
    $Core = Join-Path $bindir "dsh-memory-note.exe"
    if (Test-Path -LiteralPath $Core) {
        Write-Host "==> removing core binary"
        Remove-Item -LiteralPath $Core -Force
    }
}

# ---------------------------------------------------------------------------
# Memory-data directory deletion. The safety checks are the point here: a
# wrong or hostile DSH_MEMORY_NOTE_HOME (e.g. a drive root or the user's
# profile) must never turn this into a recursive delete of something else. We
# only ever delete a directory that (a) resolves to a real, dedicated-looking
# data directory, and (b) the user confirms twice by typing the full path.
# ---------------------------------------------------------------------------

function Resolve-DataDir {
    if ($env:DSH_MEMORY_NOTE_HOME) {
        return $env:DSH_MEMORY_NOTE_HOME
    }
    if ($env:LOCALAPPDATA) {
        return (Join-Path $env:LOCALAPPDATA "dsh-memory-note")
    }
    $profile = if ($env:USERPROFILE) {
        $env:USERPROFILE
    } else {
        (Join-Path $env:HOMEDRIVE $env:HOMEPATH)
    }
    return (Join-Path $profile "AppData\Local\dsh-memory-note")
}

$DataDir = Resolve-DataDir

# Hard check 1: the candidate must be non-empty and absolute.
if (-not $DataDir) {
    Write-Warning "skip: no data directory resolved"
    $DataDir = $null
}
elseif (-not [System.IO.Path]::IsPathRooted($DataDir)) {
    Write-Warning "skip: data directory is not an absolute path: $DataDir"
    $DataDir = $null
}

# Hard check 2: the candidate must exist and be a directory.
if ($DataDir -and -not (Test-Path -LiteralPath $DataDir -PathType Container)) {
    Write-Warning "skip: data directory does not exist (nothing to delete): $DataDir"
    $DataDir = $null
}

# Hard check 3: never a drive root or the user's profile directory.
function Test-DangerousPath {
    param([string]$Path)
    try {
        $canon = (Resolve-Path -LiteralPath $Path).Path.TrimEnd('\')
    }
    catch {
        return $true   # unresolvable: treat as dangerous, bail out
    }
    # Drive roots: "C:\" resolves to "C:\".
    if ($canon -match '^[A-Za-z]:\\?$') { return $true }
    # The user's profile directory itself.
    $profileCanon = $null
    if ($env:USERPROFILE) {
        try { $profileCanon = (Resolve-Path -LiteralPath $env:USERPROFILE).Path.TrimEnd('\') } catch {}
    }
    if ($profileCanon -and ($canon -eq $profileCanon)) { return $true }
    return $false
}

if ($DataDir -and (Test-DangerousPath $DataDir)) {
    Write-Warning "skip: refusing to delete a drive root/profile directory: $DataDir"
    Write-Warning "       remove its contents yourself if you are sure."
    $DataDir = $null
}

# Hard check 3b: reject a reparse point (junction / directory symlink). A
# junction points at another directory; -Recurse must not be trusted to stay
# inside it.
if ($DataDir) {
    $attrs = (Get-Item -LiteralPath $DataDir -Force).Attributes
    if ($attrs -band [System.IO.FileAttributes]::ReparsePoint) {
        Write-Warning "skip: refusing to delete a junction/symlink directory: $DataDir"
        Write-Warning "       remove it (and its target) yourself if you are sure."
        $DataDir = $null
    }
}

# Hard check 4: sentinel-file check. Only a directory that actually looks like
# this tool's data directory (contains meta.db at the top level) is eligible.
if ($DataDir -and -not (Test-Path -LiteralPath (Join-Path $DataDir "meta.db") -PathType Leaf)) {
    Write-Warning "skip: '$DataDir' does not contain meta.db — not a dsh-memory-note"
    Write-Warning "       data directory, or it holds something else. Refusing to delete."
    $DataDir = $null
}

# If everything passed, ask — twice — before deleting.
if ($DataDir) {
    Write-Host ""
    Write-Host "The memory-data directory is:"
    Write-Host "  $DataDir"
    $Confirm1 = Read-Host "Delete this entire directory? [y/N]"
    if ($Confirm1 -in @("y", "Y")) {
        $Confirm2 = Read-Host "Type the full path to confirm deletion"
        $typedCanon = $null
        try { $typedCanon = (Resolve-Path -LiteralPath $Confirm2).Path.TrimEnd('\') } catch {}
        $dataCanon = (Resolve-Path -LiteralPath $DataDir).Path.TrimEnd('\')
        # -ieq: case-insensitive, matching Windows path semantics.
        if ($typedCanon -and ($typedCanon -ieq $dataCanon)) {
            Write-Host "==> deleting $DataDir"
            Remove-Item -LiteralPath $DataDir -Recurse -Force
            Write-Host "deleted."
        }
        else {
            Write-Warning "aborted: path does not match the data directory exactly."
        }
    }
    else {
        Write-Host "skipped: memory-data directory left untouched."
    }
}

Write-Host ""
Write-Host "done. If you wrote a binaryPath override into `$DSH_HOME/cordis.patch.yml,"
Write-Host "remove that memory-note row yourself."