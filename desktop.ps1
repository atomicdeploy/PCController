# Launch from any working directory. No build, install or automatic board action.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command node -ErrorAction SilentlyContinue)) {
    Write-Error 'Node.js was not found in PATH.'
    exit 1
}
& node (Join-Path $PSScriptRoot 'Tools/Build/run-desktop.mjs') @args
exit $LASTEXITCODE
