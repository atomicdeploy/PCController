[CmdletBinding()]
param(
    [string]$Version,
    [ValidateSet('amd64', '386', 'arm64')]
    [string]$Architecture = 'amd64',
    [ValidateSet('develop', 'production')]
    [string]$Edition,
    [ValidateRange(0, 65535)]
    [int]$BuildNumber = 0,
    [string]$Commit,
    [string]$BuildDate,
    [string]$UPXPath,
    [switch]$Compress,
    [switch]$SkipTests,
    [switch]$SkipVet,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'terminal.ps1')

$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$expectedOutput = [IO.Path]::GetFullPath(
    (Join-Path $projectRoot 'dist\PCControllerDesktop.exe')
)
$buildArguments = @{
    Architecture = $Architecture
    BuildNumber = $BuildNumber
    Compress = $Compress
    SkipTests = $SkipTests
    SkipVet = $SkipVet
    Force = $Force
}
if (-not [string]::IsNullOrWhiteSpace($Edition)) {
    $buildArguments.Edition = $Edition
}
foreach ($optional in @{
    Version = $Version
    Commit = $Commit
    BuildDate = $BuildDate
    UPXPath = $UPXPath
}.GetEnumerator()) {
    if (-not [string]::IsNullOrWhiteSpace([string]$optional.Value)) {
        $buildArguments[$optional.Key] = $optional.Value
    }
}

$reportedOutput = $null
& (Join-Path $PSScriptRoot 'build.ps1') @buildArguments |
    ForEach-Object {
        # Preserve native npm/Go diagnostics in the caller's success stream
        # while retaining the final artifact-path object for validation.
        $reportedOutput = $_
        Write-Output $_
    }
if (-not (Test-Path -LiteralPath $expectedOutput -PathType Leaf)) {
    throw "Build did not create the expected executable: $expectedOutput"
}
if (-not [string]::Equals(
    [IO.Path]::GetFullPath([string]$reportedOutput),
    $expectedOutput,
    [StringComparison]::OrdinalIgnoreCase
)) {
    throw "Build reported an unexpected output path: $reportedOutput"
}

$releaseEntries = @(
    Get-ChildItem -LiteralPath (Split-Path $expectedOutput) -Force
)
if ($releaseEntries.Count -ne 1 -or
    $releaseEntries[0].PSIsContainer -or
    -not [string]::Equals(
        $releaseEntries[0].FullName,
        $expectedOutput,
        [StringComparison]::OrdinalIgnoreCase
    )) {
    throw 'The release directory must contain exactly one file: PCControllerDesktop.exe.'
}

$hash = (Get-FileHash -LiteralPath $expectedOutput -Algorithm SHA256).Hash.ToLowerInvariant()
Write-PCControllerDesktopStatus -Level Artifact -Message $expectedOutput
Write-PCControllerDesktopStatus -Level Info `
    -Icon (Get-PCControllerDesktopEmojiGlyph -Name Hash) -Label 'SHA-256' -Message $hash
Write-PCControllerDesktopStatus -Level Success `
    -Message 'Verified the single-file Windows release directory contract.'
Write-Output $expectedOutput
