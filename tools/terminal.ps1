# Shared terminal presentation for RayanLamp's PowerShell tooling.
#
# RayanLamp-authored human status is written to the information stream so the
# script's final success-stream object remains a machine-readable artifact
# path. (Native child tools can still forward their own ordinary stdout.)
# ANSI SGR is emitted only for an attached VT-capable terminal. NO_COLOR and
# TERM=dumb always win; redirected output is deliberately plain.

Set-StrictMode -Version Latest

function Get-RayanLampEnvironmentValue {
    param([Parameter(Mandatory = $true)][string]$Name)

    $item = Get-Item -LiteralPath "Env:$Name" -ErrorAction SilentlyContinue
    if ($null -eq $item) {
        return $null
    }
    return [string]$item.Value
}

function Test-RayanLampEnvironmentEnabled {
    param([Parameter(Mandatory = $true)][string]$Name)

    $value = Get-RayanLampEnvironmentValue -Name $Name
    if ([string]::IsNullOrWhiteSpace($value)) {
        return $false
    }
    return $value -notmatch '^(?:0|false|no|off)$'
}

function Test-RayanLampVT100 {
    param(
        [ValidateSet('Output', 'Error')]
        [string]$Stream = 'Output'
    )

    $noColor = Get-RayanLampEnvironmentValue -Name 'NO_COLOR'
    if (-not [string]::IsNullOrEmpty($noColor)) {
        return $false
    }
    if ([string]::Equals(
        (Get-RayanLampEnvironmentValue -Name 'TERM'),
        'dumb',
        [StringComparison]::OrdinalIgnoreCase
    )) {
        return $false
    }
    if ([string]::Equals(
        (Get-RayanLampEnvironmentValue -Name 'CLICOLOR'),
        '0',
        [StringComparison]::Ordinal
    ) -or [string]::Equals(
        (Get-RayanLampEnvironmentValue -Name 'FORCE_COLOR'),
        '0',
        [StringComparison]::Ordinal
    )) {
        return $false
    }

    if ((Test-RayanLampEnvironmentEnabled -Name 'FORCE_COLOR') -or
        (Test-RayanLampEnvironmentEnabled -Name 'CLICOLOR_FORCE') -or
        (Test-RayanLampEnvironmentEnabled -Name 'RAYANLAMP_FORCE_COLOR')) {
        return $true
    }

    try {
        $redirected = if ($Stream -eq 'Error') {
            [Console]::IsErrorRedirected
        }
        else {
            [Console]::IsOutputRedirected
        }
        if ($redirected) {
            return $false
        }
    }
    catch {
        return $false
    }

    $property = $Host.UI.PSObject.Properties['SupportsVirtualTerminal']
    return $null -ne $property -and [bool]$Host.UI.SupportsVirtualTerminal
}

function Test-RayanLampEmoji {
    param(
        [ValidateSet('Output', 'Error')]
        [string]$Stream = 'Output'
    )

    if (Test-RayanLampEnvironmentEnabled -Name 'NO_EMOJI') {
        return $false
    }
    $preference = Get-RayanLampEnvironmentValue -Name 'RAYANLAMP_EMOJI'
    if (-not [string]::IsNullOrWhiteSpace($preference)) {
        switch ($preference.Trim().ToLowerInvariant()) {
            { $_ -in @('1', 'always', 'force', 'on', 'true', 'yes') } {
                return $true
            }
            { $_ -in @('0', 'never', 'off', 'false', 'no') } {
                return $false
            }
        }
    }
    if ([string]::Equals(
        (Get-RayanLampEnvironmentValue -Name 'TERM'),
        'dumb',
        [StringComparison]::OrdinalIgnoreCase
    )) {
        return $false
    }
    try {
        $interactive = if ($Stream -eq 'Error') {
            -not [Console]::IsErrorRedirected
        }
        else {
            -not [Console]::IsOutputRedirected
        }
        if (-not $interactive) {
            return $false
        }
        if (-not [string]::IsNullOrWhiteSpace(
            (Get-RayanLampEnvironmentValue -Name 'WT_SESSION')
        ) -or -not [string]::IsNullOrWhiteSpace(
            (Get-RayanLampEnvironmentValue -Name 'ConEmuANSI')
        ) -or -not [string]::IsNullOrWhiteSpace(
            (Get-RayanLampEnvironmentValue -Name 'ANSICON')
        )) {
            return $true
        }
        return [Console]::OutputEncoding.CodePage -eq 65001
    }
    catch {
        return $false
    }
}

function Get-RayanLampEmojiGlyph {
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet(
            'Start', 'Step', 'Success', 'Info', 'Artifact', 'Warning',
            'Dependencies', 'Typecheck', 'Test', 'Web', 'Resource', 'Compile',
            'Compress', 'Hash', 'Size', 'Assets', 'Lamp', 'Cleanup', 'Preserve'
        )]
        [string]$Name
    )

    $codePoints = @{
        Start = @(0x1F680)
        Step = @(0x1F527)
        Success = @(0x2705)
        Info = @(0x2139, 0xFE0F)
        Artifact = @(0x1F4E6)
        Warning = @(0x26A0, 0xFE0F)
        Dependencies = @(0x1F4E5)
        Typecheck = @(0x1F50E)
        Test = @(0x1F9EA)
        Web = @(0x1F310)
        Resource = @(0x1FA9F)
        Compile = @(0x1F528)
        Compress = @(0x1F5DC, 0xFE0F)
        Hash = @(0x1F510)
        Size = @(0x1F4CF)
        Assets = @(0x1F3A8)
        Lamp = @(0x1F4A1)
        Cleanup = @(0x1F9F9)
        Preserve = @(0x1F6E1, 0xFE0F)
    }[$Name]

    $builder = [Text.StringBuilder]::new()
    foreach ($codePoint in $codePoints) {
        [void]$builder.Append([char]::ConvertFromUtf32($codePoint))
    }
    return $builder.ToString()
}

function Format-RayanLampTerminalMessage {
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet('Start', 'Step', 'Success', 'Info', 'Artifact', 'Warning')]
        [string]$Level,
        [Parameter(Mandatory = $true)][string]$Message,
        [Parameter(Mandatory = $true)][bool]$Ansi,
        [Parameter(Mandatory = $true)][bool]$Emoji,
        [string]$Icon,
        [string]$Label
    )

    $presentation = @{
        Start = @{
            Icon = Get-RayanLampEmojiGlyph -Name Start
            Label = 'START'
            Sgr = '1;36'
        }
        Step = @{
            Icon = Get-RayanLampEmojiGlyph -Name Step
            Label = 'STEP'
            Sgr = '1;36'
        }
        Success = @{
            Icon = Get-RayanLampEmojiGlyph -Name Success
            Label = 'OK'
            Sgr = '1;32'
        }
        Info = @{
            Icon = Get-RayanLampEmojiGlyph -Name Info
            Label = 'INFO'
            Sgr = '1;34'
        }
        Artifact = @{
            Icon = Get-RayanLampEmojiGlyph -Name Artifact
            Label = 'ARTIFACT'
            Sgr = '1;35'
        }
        Warning = @{
            Icon = Get-RayanLampEmojiGlyph -Name Warning
            Label = 'WARNING'
            Sgr = '1;33'
        }
    }[$Level]

    $resolvedIcon = if ([string]::IsNullOrWhiteSpace($Icon)) {
        $presentation.Icon
    }
    else {
        $Icon
    }
    $resolvedLabel = if ([string]::IsNullOrWhiteSpace($Label)) {
        $presentation.Label
    }
    else {
        $Label.Trim().ToUpperInvariant()
    }
    $prefix = if ($Emoji) {
        "$resolvedIcon $resolvedLabel"
    }
    else {
        $resolvedLabel
    }

    if (-not $Ansi) {
        return "${prefix}: $Message"
    }

    $escape = [char]27
    return "$escape[$($presentation.Sgr)m$prefix$escape[0m $Message"
}

function Write-RayanLampStatus {
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet('Start', 'Step', 'Success', 'Info', 'Artifact', 'Warning')]
        [string]$Level,
        [Parameter(Mandatory = $true)][string]$Message,
        [string]$Icon,
        [string]$Label
    )

    $formatted = Format-RayanLampTerminalMessage `
        -Level $Level `
        -Message $Message `
        -Ansi:(Test-RayanLampVT100) `
        -Emoji:(Test-RayanLampEmoji) `
        -Icon $Icon `
        -Label $Label
    Write-Information $formatted -InformationAction Continue
}
