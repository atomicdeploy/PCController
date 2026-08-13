[CmdletBinding()]
param(
    [switch]$SkipDesktopBranding
)

$ErrorActionPreference = 'Stop'
$repository = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path

git -C $repository config --local core.autocrlf false
git -C $repository config --local core.eol lf
git -C $repository config --local commit.template .gitmessage

if (-not $SkipDesktopBranding -and $env:OS -eq 'Windows_NT') {
    $desktopIni = Join-Path $repository 'Desktop.ini'
    if (-not (Test-Path -LiteralPath $desktopIni)) {
        throw "Desktop branding file is missing: $desktopIni"
    }
    & attrib.exe +s $repository
    & attrib.exe +h $desktopIni
}

Write-Host "Configured Git defaults for $repository"
if (-not $SkipDesktopBranding -and $env:OS -eq 'Windows_NT') {
    Write-Host 'Applied the tracked PCController folder icon through Desktop.ini.'
}
