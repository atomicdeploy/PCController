# Run from any working directory; all paths are resolved by the generator.
$ErrorActionPreference = 'Stop'
if (-not (Get-Command node -ErrorAction SilentlyContinue)) {
    Write-Error 'Node.js was not found in PATH.'
    exit 1
}
& node (Join-Path $PSScriptRoot 'Tools/Build/update-readme-previews.mjs') @args
exit $LASTEXITCODE
