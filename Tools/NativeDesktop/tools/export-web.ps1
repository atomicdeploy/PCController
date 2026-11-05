[CmdletBinding()]
param(
    [switch]$SkipTests
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'terminal.ps1')

$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$webRoot = [IO.Path]::GetFullPath((Join-Path $projectRoot 'webui'))
$distRoot = [IO.Path]::GetFullPath((Join-Path $projectRoot 'dist'))
$outputPath = [IO.Path]::GetFullPath(
    (Join-Path $distRoot 'PCControllerDesktop-Web-Static.zip')
)
$temporaryRoot = [IO.Path]::GetFullPath(
    (Join-Path ([IO.Path]::GetTempPath()) (
        'PCControllerDesktop-web-export-' + [Guid]::NewGuid().ToString('N')
    ))
)
$sourceRoot = [IO.Path]::GetFullPath(
    (Join-Path $temporaryRoot 'dist')
)

foreach ($commandName in @('go', 'node', 'npm')) {
    if (-not (Get-Command $commandName -ErrorAction SilentlyContinue)) {
        throw "$commandName is required to build the static web dashboard."
    }
}

$temporaryParent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd(
    [char[]]@('\', '/')
)
if (-not [string]::Equals(
    [IO.Path]::GetDirectoryName($temporaryRoot),
    $temporaryParent,
    [StringComparison]::OrdinalIgnoreCase
) -or -not [IO.Path]::GetFileName($temporaryRoot).StartsWith(
    'PCControllerDesktop-web-export-',
    [StringComparison]::Ordinal
)) {
    throw "Unsafe temporary web export path: $temporaryRoot"
}

New-Item -ItemType Directory -Force -Path $distRoot | Out-Null
New-Item -ItemType Directory -Path $temporaryRoot | Out-Null

function Restore-EnvironmentValue {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [AllowNull()][string]$Value,
        [bool]$Existed
    )
    if ($Existed) {
        Set-Item -LiteralPath "Env:$Name" -Value $Value
    }
    else {
        Remove-Item -LiteralPath "Env:$Name" -ErrorAction SilentlyContinue
    }
}

$goHostOS = (& go env GOHOSTOS).Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($goHostOS)) {
    throw 'Could not determine the Go host operating system.'
}
$goHostArchitecture = (& go env GOHOSTARCH).Trim()
if ($LASTEXITCODE -ne 0 -or
    [string]::IsNullOrWhiteSpace($goHostArchitecture)) {
    throw 'Could not determine the Go host architecture.'
}

$environment = @{}
foreach ($name in @(
    'GOOS',
    'GOARCH',
    'CGO_ENABLED',
    'PCCONTROLLER_DESKTOP_WEB_OUT_DIR',
    'VITE_PCCONTROLLER_DESKTOP_STATIC_EXPORT',
    'VITE_PCCONTROLLER_DESKTOP_BUILD_EDITION'
)) {
    $environment[$name] = @{
        Existed = Test-Path -LiteralPath "Env:$name"
        Value = [Environment]::GetEnvironmentVariable($name, 'Process')
    }
}

try {
    $env:PCCONTROLLER_DESKTOP_WEB_OUT_DIR = $sourceRoot

    Push-Location $webRoot
    try {
        Write-PCControllerDesktopStatus -Level Step `
            -Icon (Get-PCControllerDesktopEmojiGlyph -Name Dependencies) -Label 'DEPS' `
            -Message 'Installing the locked Web UI dependency graph.'
        & npm ci --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) {
            throw 'npm ci failed.'
        }
        Write-PCControllerDesktopStatus -Level Step `
            -Icon (Get-PCControllerDesktopEmojiGlyph -Name Typecheck) -Label 'TYPECHECK' `
            -Message 'Checking the static Web UI TypeScript contract.'
        & npm run typecheck
        if ($LASTEXITCODE -ne 0) {
            throw 'Frontend typecheck failed.'
        }
        if (-not $SkipTests) {
            Write-PCControllerDesktopStatus -Level Step `
                -Icon (Get-PCControllerDesktopEmojiGlyph -Name Test) -Label 'TEST' `
                -Message 'Running Web UI unit tests.'
            & npm test
            if ($LASTEXITCODE -ne 0) {
                throw 'Frontend unit tests failed.'
            }
        }
        $env:VITE_PCCONTROLLER_DESKTOP_STATIC_EXPORT = 'true'
        $env:VITE_PCCONTROLLER_DESKTOP_BUILD_EDITION = 'production'
        Write-PCControllerDesktopStatus -Level Step `
            -Icon (Get-PCControllerDesktopEmojiGlyph -Name Web) -Label 'WEB' `
            -Message 'Building the direct-first static SPA/PWA.'
        & npm run build
        if ($LASTEXITCODE -ne 0) {
            throw 'Static frontend production build failed.'
        }
    }
    finally {
        Pop-Location
    }

    foreach ($requiredFile in @(
        'index.html',
        'build-edition.json',
        'manifest.webmanifest',
        'sw.js',
        'theme-init.js'
    )) {
        $requiredPath = [IO.Path]::GetFullPath(
            (Join-Path $sourceRoot $requiredFile)
        )
        if (-not $requiredPath.StartsWith(
            $sourceRoot.TrimEnd('\') + '\',
            [StringComparison]::OrdinalIgnoreCase
        ) -or -not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
            throw "Static frontend build output is incomplete: $requiredPath"
        }
    }
    $editionMarker = Get-Content `
        -LiteralPath (Join-Path $sourceRoot 'build-edition.json') `
        -Raw |
        ConvertFrom-Json
    if ($editionMarker.edition -ne 'production' -or
        [bool]$editionMarker.developer_tools) {
        throw 'Static Web export must be a source-map-free production edition.'
    }

    # The exporter is a host build tool. Do not inherit a release target such
    # as windows/arm64 and then try to execute that binary on an amd64 host.
    $env:GOOS = $goHostOS
    $env:GOARCH = $goHostArchitecture
    $env:CGO_ENABLED = '0'

    Push-Location $projectRoot
    try {
        Write-PCControllerDesktopStatus -Level Step `
            -Icon (Get-PCControllerDesktopEmojiGlyph -Name Artifact) -Label 'ZIP' `
            -Message 'Writing the deterministic static frontend archive.'
        & go run ./tools/export-web --source $sourceRoot --output $outputPath
        if ($LASTEXITCODE -ne 0) {
            throw 'Static frontend export failed.'
        }
    }
    finally {
        Pop-Location
    }
}
finally {
    foreach ($name in @(
        'GOOS',
        'GOARCH',
        'CGO_ENABLED',
        'PCCONTROLLER_DESKTOP_WEB_OUT_DIR',
        'VITE_PCCONTROLLER_DESKTOP_STATIC_EXPORT',
        'VITE_PCCONTROLLER_DESKTOP_BUILD_EDITION'
    )) {
        Restore-EnvironmentValue -Name $name `
            -Value $environment[$name].Value `
            -Existed $environment[$name].Existed
    }
    if (Test-Path -LiteralPath $temporaryRoot -PathType Container) {
        $resolvedTemporary = [IO.Path]::GetFullPath($temporaryRoot)
        if (-not [string]::Equals(
            [IO.Path]::GetDirectoryName($resolvedTemporary),
            $temporaryParent,
            [StringComparison]::OrdinalIgnoreCase
        ) -or -not [IO.Path]::GetFileName($resolvedTemporary).StartsWith(
            'PCControllerDesktop-web-export-',
            [StringComparison]::Ordinal
        )) {
            throw "Refusing to remove unsafe temporary path: $resolvedTemporary"
        }
        Remove-Item -LiteralPath $resolvedTemporary -Recurse -Force
    }
}

if (-not (Test-Path -LiteralPath $outputPath -PathType Leaf)) {
    throw "Static frontend export did not create its expected output: $outputPath"
}

$hash = (
    Get-FileHash -LiteralPath $outputPath -Algorithm SHA256
).Hash.ToLowerInvariant()
$size = (Get-Item -LiteralPath $outputPath).Length
Write-PCControllerDesktopStatus -Level Artifact -Message $outputPath
Write-PCControllerDesktopStatus -Level Info `
    -Icon (Get-PCControllerDesktopEmojiGlyph -Name Size) -Label 'SIZE' `
    -Message "$size bytes"
Write-PCControllerDesktopStatus -Level Info `
    -Icon (Get-PCControllerDesktopEmojiGlyph -Name Hash) -Label 'SHA-256' -Message $hash
Write-PCControllerDesktopStatus -Level Success `
    -Message 'Validated the reproducible static Web UI archive.'
Write-Output $outputPath
