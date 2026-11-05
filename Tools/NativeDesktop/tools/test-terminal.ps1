[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'terminal.ps1')

function Assert-True {
    param(
        [Parameter(Mandatory = $true)][bool]$Condition,
        [Parameter(Mandatory = $true)][string]$Message
    )
    if (-not $Condition) {
        throw $Message
    }
}

$escape = [char]27
$styled = Format-RayanLampTerminalMessage `
    -Level Success `
    -Message 'release ready' `
    -Ansi:$true `
    -Emoji:$true
Assert-True (($styled.ToCharArray() | Where-Object { $_ -eq $escape }).Count -eq 2) `
    'Styled status output contains an unexpected escape sequence.'
$stripped = $styled -replace "$escape\[[0-9;]*m", ''
$successGlyph = Get-RayanLampEmojiGlyph -Name Success
Assert-True ($styled.StartsWith("$escape[1;32m$successGlyph OK$escape[0m ")) `
    'Styled success output does not use the expected SGR sequence and reset.'
Assert-True ($stripped -eq "$successGlyph OK release ready") `
    'Styled output does not reduce to the expected readable text.'

$plain = Format-RayanLampTerminalMessage `
    -Level Artifact `
    -Message 'dist/RayanLamp.exe' `
    -Ansi:$false `
    -Emoji:$false
Assert-True (-not $plain.Contains([string]$escape)) `
    'Plain output leaked an ANSI escape character.'
Assert-True ($plain -eq 'ARTIFACT: dist/RayanLamp.exe') `
    'Plain output does not use the stable ASCII fallback.'

foreach ($level in @(
    'Start', 'Step', 'Success', 'Info', 'Artifact', 'Warning'
)) {
    $ansiSample = Format-RayanLampTerminalMessage `
        -Level $level `
        -Message 'sample' `
        -Ansi:$true `
        -Emoji:$true
    $escapeCount = @(
        $ansiSample.ToCharArray() | Where-Object { $_ -eq $escape }
    ).Count
    Assert-True ($escapeCount -eq 2) `
        "$level output must contain one SGR opener and one reset."
    Assert-True ($ansiSample -match "$escape\[[0-9;]+m.*$escape\[0m ") `
        "$level output has an incomplete or malformed SGR sequence."

    $asciiSample = Format-RayanLampTerminalMessage `
        -Level $level `
        -Message 'sample' `
        -Ansi:$false `
        -Emoji:$false
    Assert-True (-not $asciiSample.Contains([string]$escape)) `
        "$level plain output leaked an escape character."
    Assert-True ($asciiSample -notmatch '[^\x00-\x7f]') `
        "$level plain output is not ASCII-safe."
}

$oldNoColor = Get-RayanLampEnvironmentValue -Name 'NO_COLOR'
$hadNoColor = $null -ne (Get-Item Env:NO_COLOR -ErrorAction SilentlyContinue)
$oldForceColor = Get-RayanLampEnvironmentValue -Name 'FORCE_COLOR'
$hadForceColor = $null -ne (Get-Item Env:FORCE_COLOR -ErrorAction SilentlyContinue)
$oldEmoji = Get-RayanLampEnvironmentValue -Name 'RAYANLAMP_EMOJI'
$hadEmoji = $null -ne (
    Get-Item Env:RAYANLAMP_EMOJI -ErrorAction SilentlyContinue
)
$oldTerm = Get-RayanLampEnvironmentValue -Name 'TERM'
$hadTerm = $null -ne (Get-Item Env:TERM -ErrorAction SilentlyContinue)
try {
    $env:NO_COLOR = '1'
    $env:FORCE_COLOR = '1'
    Assert-True (-not (Test-RayanLampVT100)) `
        'NO_COLOR must override forced color.'
    $env:RAYANLAMP_EMOJI = 'never'
    Assert-True (-not (Test-RayanLampEmoji)) `
        'RAYANLAMP_EMOJI=never must disable emoji.'
    $env:RAYANLAMP_EMOJI = 'not-a-mode'
    $env:TERM = 'dumb'
    Assert-True (-not (Test-RayanLampEmoji)) `
        'An unknown emoji mode must fall back to automatic terminal policy.'
}
finally {
    if ($hadNoColor) {
        $env:NO_COLOR = $oldNoColor
    }
    else {
        Remove-Item Env:NO_COLOR -ErrorAction SilentlyContinue
    }
    if ($hadForceColor) {
        $env:FORCE_COLOR = $oldForceColor
    }
    else {
        Remove-Item Env:FORCE_COLOR -ErrorAction SilentlyContinue
    }
    if ($hadEmoji) {
        $env:RAYANLAMP_EMOJI = $oldEmoji
    }
    else {
        Remove-Item Env:RAYANLAMP_EMOJI -ErrorAction SilentlyContinue
    }
    if ($hadTerm) {
        $env:TERM = $oldTerm
    }
    else {
        Remove-Item Env:TERM -ErrorAction SilentlyContinue
    }
}

$parseFailed = $false
Get-ChildItem -LiteralPath $PSScriptRoot -Filter '*.ps1' -File |
    ForEach-Object {
        $tokens = $null
        $parseErrors = $null
        [void][Management.Automation.Language.Parser]::ParseFile(
            $_.FullName,
            [ref]$tokens,
            [ref]$parseErrors
        )
        if ($parseErrors.Count -ne 0) {
            $parseFailed = $true
        }
        foreach ($parseError in $parseErrors) {
            Write-Error (
                '{0}:{1}: {2}' -f
                $_.FullName,
                $parseError.Extent.StartLineNumber,
                $parseError.Message
            )
        }
        $source = [IO.File]::ReadAllText($_.FullName)
        Assert-True (-not $source.Contains([string]$escape)) `
            "$($_.Name) contains a literal ESC character."
        Assert-True ($source -notmatch '[^\x00-\x7f]') `
            "$($_.Name) must remain ASCII source for Windows PowerShell 5.1."
    }
Assert-True (-not $parseFailed) 'One or more PowerShell tools failed to parse.'

Write-RayanLampStatus -Level Success `
    -Message 'PowerShell parsing, VT100, reset, fallback, and NO_COLOR checks passed.'
