[CmdletBinding()]
param(
    [ValidateSet('develop', 'production')]
    [string]$Edition,
    [switch]$SkipTests
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'terminal.ps1')

$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$defaultEditionPath = Join-Path $projectRoot 'build-edition.txt'
$webRoot = [IO.Path]::GetFullPath((Join-Path $projectRoot 'webui'))
$distRoot = [IO.Path]::GetFullPath(
    (Join-Path $projectRoot 'internal\webui\dist')
)
$packagePath = Join-Path $webRoot 'package.json'
$lockPath = Join-Path $webRoot 'package-lock.json'
$editionEnvironmentName = 'VITE_RAYANLAMP_BUILD_EDITION'
$editionEnvironmentExisted = Test-Path -LiteralPath "Env:$editionEnvironmentName"
$previousEditionEnvironment = [Environment]::GetEnvironmentVariable(
    $editionEnvironmentName,
    'Process'
)
if ([string]::IsNullOrWhiteSpace($Edition)) {
    if (-not (Test-Path -LiteralPath $defaultEditionPath -PathType Leaf)) {
        throw "Canonical build edition is missing: $defaultEditionPath"
    }
    $Edition = [IO.File]::ReadAllText($defaultEditionPath).Trim()
}
if ($Edition -notin @('develop', 'production')) {
    throw "Canonical build edition must be develop or production; got $Edition."
}

foreach ($commandName in @('node', 'npm')) {
    if (-not (Get-Command $commandName -ErrorAction SilentlyContinue)) {
        throw "$commandName is required to build the bundled web dashboard."
    }
}
foreach ($requiredPath in @($packagePath, $lockPath)) {
    if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
        throw "Required frontend build input is missing: $requiredPath"
    }
}

Push-Location $webRoot
try {
    Set-Item -LiteralPath "Env:$editionEnvironmentName" -Value $Edition
    Write-RayanLampStatus -Level Step `
        -Icon (Get-RayanLampEmojiGlyph -Name Dependencies) -Label 'DEPS' `
        -Message 'Installing the locked Web UI dependency graph.'
    & npm ci --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) {
        throw 'npm ci failed.'
    }
    Write-RayanLampStatus -Level Step `
        -Icon (Get-RayanLampEmojiGlyph -Name Typecheck) -Label 'TYPECHECK' `
        -Message 'Checking the Web UI TypeScript contract.'
    & npm run typecheck
    if ($LASTEXITCODE -ne 0) {
        throw 'Frontend typecheck failed.'
    }
    if (-not $SkipTests) {
        Write-RayanLampStatus -Level Step `
            -Icon (Get-RayanLampEmojiGlyph -Name Test) -Label 'TEST' `
            -Message 'Running Web UI unit tests.'
        & npm test
        if ($LASTEXITCODE -ne 0) {
            throw 'Frontend unit tests failed.'
        }
    }
    Write-RayanLampStatus -Level Step `
        -Icon (Get-RayanLampEmojiGlyph -Name Web) -Label 'WEB' `
        -Message "Building the $Edition SPA/PWA bundle."
    & npm run build
    if ($LASTEXITCODE -ne 0) {
        throw 'Frontend production build failed.'
    }
}
finally {
    if ($editionEnvironmentExisted) {
        Set-Item -LiteralPath "Env:$editionEnvironmentName" `
            -Value $previousEditionEnvironment
    }
    else {
        Remove-Item -LiteralPath "Env:$editionEnvironmentName" `
            -ErrorAction SilentlyContinue
    }
    Pop-Location
}

foreach ($relative in @(
    'index.html',
    'build-edition.json',
    'manifest.webmanifest',
    'sw.js',
    'theme-init.js'
)) {
    $requiredOutput = [IO.Path]::GetFullPath((Join-Path $distRoot $relative))
    if (-not $requiredOutput.StartsWith(
        $distRoot.TrimEnd('\') + '\',
        [StringComparison]::OrdinalIgnoreCase
    ) -or -not (Test-Path -LiteralPath $requiredOutput -PathType Leaf)) {
        throw "Frontend build output is incomplete: $requiredOutput"
    }
}
$editionMarkerPath = Join-Path $distRoot 'build-edition.json'
$editionMarker = Get-Content -LiteralPath $editionMarkerPath -Raw |
    ConvertFrom-Json
if ($editionMarker.edition -ne $Edition -or
    [bool]$editionMarker.developer_tools -ne ($Edition -eq 'develop')) {
    throw "Frontend edition marker does not describe the requested $Edition build."
}
$sourceMaps = @(Get-ChildItem -LiteralPath $distRoot -Recurse -Filter '*.map')
if ($Edition -eq 'production' -and $sourceMaps.Count -ne 0) {
    throw 'Production frontend must not contain source maps.'
}
if ($Edition -eq 'develop' -and $sourceMaps.Count -eq 0) {
    throw 'Development frontend must retain source maps.'
}
$assetRoot = Join-Path $distRoot 'assets'
foreach ($lazyPattern in @('xterm-*.js', 'addon-webgl-*.js')) {
    if (@(Get-ChildItem -LiteralPath $assetRoot -Filter $lazyPattern).Count -ne 1) {
        throw "Frontend must retain one lazy $lazyPattern chunk."
    }
}
if ($Edition -eq 'production') {
    $productionScripts = @(
        Get-ChildItem -LiteralPath $distRoot -Recurse -Filter '*.js'
    )
    foreach ($forbiddenMarker in @(
        'sourceMappingURL',
        '@preact/debug',
        '__VITEST__',
        'test-fixtures'
    )) {
        if (Select-String -LiteralPath $productionScripts.FullName `
                -SimpleMatch -Quiet $forbiddenMarker) {
            throw "Production frontend contains developer marker $forbiddenMarker."
        }
    }
}

Write-RayanLampStatus -Level Success `
    -Message "Built and validated the $Edition SPA/PWA at $distRoot"
Write-Output $distRoot
