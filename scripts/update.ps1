# Rebuild an existing linked installation after the source checkout changes.
# This script replaces the Go core and rebuilds the adapter bundle. It does not
# register a plugin and never reads or writes DSH_MEMORY_NOTE_HOME.
#
# Run scripts/install.ps1 first. Restart the dsh profile after this script exits.

[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$TempCore = $null

foreach ($cmd in @("go", "pnpm")) {
    if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) {
        Write-Error "error: $cmd is required"
        exit 1
    }
}

$bindir = (go env GOBIN).Trim()
if (-not $bindir) {
    $goPaths = (go env GOPATH).Trim().Split([System.IO.Path]::PathSeparator)
    $bindir = Join-Path $goPaths[0] "bin"
}
if (-not [System.IO.Path]::IsPathRooted($bindir)) {
    Write-Error "error: Go binary directory is not absolute: $bindir"
    exit 1
}

$Core = Join-Path $bindir "dsh-memory-note.exe"
if (-not (Test-Path -LiteralPath $Core -PathType Leaf)) {
    Write-Error "error: no installed core at '$Core'; run scripts/install.ps1 first"
    exit 1
}

try {
    # Build beside the installed binary. Move-Item publishes the complete file
    # only after both builds succeed, leaving the old core intact on failure.
    $TempCore = Join-Path $bindir (".dsh-memory-note.update.{0}.exe" -f [guid]::NewGuid())
    Write-Host "==> building core"
    Push-Location $Root
    try {
        go build -o $TempCore ./cmd/dsh-memory-note
        if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    }
    finally {
        Pop-Location
    }

    Write-Host "==> building adapter"
    pnpm --dir "$Root\adapter" install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) { throw "pnpm install failed" }
    pnpm --dir "$Root\adapter" build
    if ($LASTEXITCODE -ne 0) { throw "pnpm build failed" }

    Write-Host "==> replacing core"
    Move-Item -LiteralPath $TempCore -Destination $Core -Force
    $TempCore = $null
}
catch {
    Write-Error "error: update failed; fix the error and rerun this script. $($_.Exception.Message)"
    exit 1
}
finally {
    if ($TempCore -and (Test-Path -LiteralPath $TempCore)) {
        Remove-Item -LiteralPath $TempCore -Force
    }
}

Write-Host ""
Write-Host "updated. Restart the dsh profile to load the rebuilt adapter."
Write-Host "Memory data was not changed."
