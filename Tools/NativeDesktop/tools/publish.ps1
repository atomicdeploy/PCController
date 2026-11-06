[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot 'terminal.ps1')
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$destination = 'AtomicDeploy/PCController'
$remoteURL = "https://github.com/$destination.git"

function Invoke-Checked {
    param([string]$Program, [string[]]$Arguments)
    $result = & $Program @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Program failed with exit code $LASTEXITCODE. No force/recovery operation was attempted."
    }
    return $result
}

Push-Location $projectRoot
try {
    foreach ($program in @('git', 'gh', 'node')) {
        if (-not (Get-Command $program -ErrorAction SilentlyContinue)) {
            throw "$program is required on PATH."
        }
    }
    $dirty = Invoke-Checked git @('status', '--porcelain')
    if ($dirty) { throw 'Commit/review local changes before publication.' }
    $branch = Invoke-Checked git @('branch', '--show-current')
    if ($branch -ne 'main') { throw 'Publication must run from main.' }
    Invoke-Checked node @('tools/verify-history.mjs')
    $login = Invoke-Checked gh @('api', 'user', '--jq', '.login')
    if ($login -ine 'AtomicDeploy') { throw "Expected AtomicDeploy authentication, got $login." }
    # A failed lookup alone cannot distinguish absence from a network/auth error.
    # Listing the owner's repositories successfully makes that decision explicit.
    $repos = (Invoke-Checked gh @('repo', 'list', 'AtomicDeploy', '--limit', '1000', '--json', 'nameWithOwner')) -join "`n"
    $exists = @($repos | ConvertFrom-Json | Where-Object { $_.nameWithOwner -ieq $destination }).Count -gt 0
    if (-not $exists) {
        Invoke-Checked gh @('repo', 'create', $destination, '--public', '--description', 'Native Windows device controller with live glass UI, TUI and synchronized web dashboard')
    }
    $visibility = Invoke-Checked gh @('repo', 'view', $destination, '--json', 'visibility', '--jq', '.visibility')
    if ($visibility -ne 'PUBLIC') { throw 'Destination is not public; refusing to change its visibility implicitly.' }
    $remotes = @(Invoke-Checked git @('remote'))
    if ($remotes -contains 'origin') {
        $existing = Invoke-Checked git @('remote', 'get-url', 'origin')
        if ($existing -notin @($remoteURL, "git@github.com:$destination.git")) {
            throw "Unexpected origin: $existing"
        }
    } else {
        Invoke-Checked git @('remote', 'add', 'origin', $remoteURL)
    }
    Write-PCControllerDesktopStatus -Level Start -Message "Publishing main to public $destination."
    Invoke-Checked git @('push', '--set-upstream', 'origin', 'main')
    $local = Invoke-Checked git @('rev-parse', 'HEAD')
    $remote = Invoke-Checked git @('ls-remote', 'origin', 'refs/heads/main')
    if (($remote -split '\s+')[0] -ne $local) { throw 'Remote main SHA does not match local HEAD.' }
    Write-PCControllerDesktopStatus -Level Success -Message "Verified remote main at $local. Check GitHub Actions for CI results."
} finally {
    Pop-Location
}
