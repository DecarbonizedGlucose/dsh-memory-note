# Install dsh-memory-note from a source checkout on Windows:
#   1. build the Go core and install it into GOBIN,
#   2. build the TypeScript adapter bundle,
#   3. register the bundle into a dsh profile via a local link: install.
#
# For Unix-like systems, use scripts/install.sh. This script assumes PowerShell
# and the Windows data directory (%LOCALAPPDATA%\dsh-memory-note).
#
# The core is built from the checkout, so no version tag or remote is needed.
# The adapter is installed as a link: so edits to adapter/ (plus a rebuild)
# take effect on the next profile restart or HMR reload.
#
# Usage: .\scripts\install.ps1 [[-Profile] <name>]
#   -Profile — the dsh profile to install into (default: web).

[CmdletBinding()]
param(
    [string]$Profile = "web"
)

$ErrorActionPreference = "Stop"

$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

foreach ($cmd in @("go", "pnpm", "dsh")) {
    if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) {
        Write-Error "error: $cmd is required"
        exit 1
    }
}

# 1. Core: install the one-shot executable into GOBIN.
Write-Host "==> building core"
Push-Location $Root
try {
    go install ./cmd/dsh-memory-note
    if ($LASTEXITCODE -ne 0) { throw "go install failed" }
}
finally {
    Pop-Location
}

# 2. Adapter: install dependencies and build the dist/ bundle entry.
Write-Host "==> building adapter"
pnpm --dir "$Root\adapter" install --frozen-lockfile
if ($LASTEXITCODE -ne 0) { throw "pnpm install failed" }
pnpm --dir "$Root\adapter" build
if ($LASTEXITCODE -ne 0) { throw "pnpm build failed" }

# 3. Register the bundle. link: keeps the checkout live for development.
Write-Host "==> installing into profile '$Profile'"
dsh plugin --profile $Profile add "link:$Root\adapter"
if ($LASTEXITCODE -ne 0) { throw "dsh plugin add failed" }

Write-Host ""
$bindir = (go env GOBIN).Trim()
if (-not $bindir) {
    $bindir = Join-Path (go env GOPATH).Trim() "bin"
}
Write-Host "done. Make sure '$bindir' is on PATH: the adapter defaults to"
Write-Host "binaryPath=dsh-memory-note, which is resolved through PATH at call time."
Write-Host "Memory data lives in $env:LOCALAPPDATA\dsh-memory-note unless DSH_MEMORY_NOTE_HOME is set."
Write-Host "Restart the profile (e.g. 'dsh --profile $Profile') to load the tools."