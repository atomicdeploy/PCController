[CmdletBinding()]
param(
    [switch]$SkipDesktopBranding
)

$ErrorActionPreference = 'Stop'
$repository = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path

function Invoke-CheckedNative {
    param(
        [Parameter(Mandatory)]
        [string]$Command,
        [Parameter(ValueFromRemainingArguments)]
        [string[]]$Arguments
    )

    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Command failed with exit code $LASTEXITCODE"
    }
}

Invoke-CheckedNative -Command 'git' -Arguments @('-C', $repository, 'config', '--local', 'core.autocrlf', 'false')
Invoke-CheckedNative -Command 'git' -Arguments @('-C', $repository, 'config', '--local', 'core.eol', 'lf')
Invoke-CheckedNative -Command 'git' -Arguments @('-C', $repository, 'config', '--local', 'commit.template', '.gitmessage')

if (-not $SkipDesktopBranding -and $env:OS -eq 'Windows_NT') {
    $desktopIni = Join-Path $repository 'Desktop.ini'
    if (-not (Test-Path -LiteralPath $desktopIni)) {
        throw "Desktop branding file is missing: $desktopIni"
    }
    Invoke-CheckedNative -Command 'attrib.exe' -Arguments @('+s', $repository)
    Invoke-CheckedNative -Command 'attrib.exe' -Arguments @('+h', '+s', $desktopIni)
}

Write-Host "Configured Git defaults for $repository"
if (-not $SkipDesktopBranding -and $env:OS -eq 'Windows_NT') {
    Write-Host 'Applied the tracked PCController folder icon through Desktop.ini.'
}
