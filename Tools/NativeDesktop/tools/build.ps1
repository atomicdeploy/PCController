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
    [switch]$SkipWebBuild,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'terminal.ps1')

$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$distRoot = Join-Path $projectRoot 'dist'
$defaultEditionPath = Join-Path $projectRoot 'build-edition.txt'
$buildInfoPath = Join-Path $projectRoot 'internal\buildinfo\buildinfo.go'
$resourceTool = 'github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0'
$semanticVersionPattern = '^(?<major>0|[1-9]\d*)\.(?<minor>0|[1-9]\d*)\.(?<patch>0|[1-9]\d*)(?:-(?:[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+(?:[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$'

function Write-Utf8NoBom {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][string]$Value
    )
    $encoding = New-Object Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($LiteralPath, $Value, $encoding)
}

function Get-SourceVersion {
    $source = [IO.File]::ReadAllText($buildInfoPath)
    $match = [regex]::Match(
        $source,
        '(?m)^\s*Version\s*=\s*"(?<version>[^"]+)"'
    )
    if (-not $match.Success) {
        throw "Could not read the application version from $buildInfoPath."
    }
    return $match.Groups['version'].Value
}

function Assert-DirectChildPath {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][string]$ExpectedParent,
        [Parameter(Mandatory = $true)][string]$ExpectedLeafPrefix
    )

    $fullPath = [IO.Path]::GetFullPath($LiteralPath)
    $fullParent = [IO.Path]::GetFullPath($ExpectedParent).TrimEnd(
        [char[]]@('\', '/')
    )
    $actualParent = [IO.Path]::GetDirectoryName($fullPath)
    $leaf = [IO.Path]::GetFileName($fullPath)
    if (-not [string]::Equals(
        $actualParent,
        $fullParent,
        [StringComparison]::OrdinalIgnoreCase
    ) -or -not $leaf.StartsWith(
        $ExpectedLeafPrefix,
        [StringComparison]::Ordinal
    )) {
        throw "Unsafe generated path: $fullPath"
    }
}

function Assert-SafeDistPath {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)

    $fullPath = [IO.Path]::GetFullPath($LiteralPath)
    $expected = [IO.Path]::GetFullPath((Join-Path $distRoot 'PCControllerDesktop.exe'))
    if (-not [string]::Equals(
        $fullPath,
        $expected,
        [StringComparison]::OrdinalIgnoreCase
    )) {
        throw "Refusing to replace an unexpected output path: $fullPath"
    }
}

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

function Resolve-UPXExecutable {
    param(
        [AllowEmptyString()][string]$RequestedPath,
        [bool]$SearchPath
    )

    if (-not [string]::IsNullOrWhiteSpace($RequestedPath)) {
        $resolved = [IO.Path]::GetFullPath($RequestedPath)
        if (-not (Test-Path -LiteralPath $resolved -PathType Leaf)) {
            throw "The requested UPX executable does not exist: $resolved"
        }
        return $resolved
    }
    if (-not $SearchPath) {
        return $null
    }
    $command = Get-Command upx -ErrorAction SilentlyContinue
    if ($null -eq $command) {
        return $null
    }
    return [IO.Path]::GetFullPath($command.Source)
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go is required and must be available on PATH.'
}
if ([string]::IsNullOrWhiteSpace($Edition)) {
    if (-not (Test-Path -LiteralPath $defaultEditionPath -PathType Leaf)) {
        throw "Canonical build edition is missing: $defaultEditionPath"
    }
    $Edition = [IO.File]::ReadAllText($defaultEditionPath).Trim()
}
if ($Edition -notin @('develop', 'production')) {
    throw "Canonical build edition must be develop or production; got $Edition."
}

Write-PCControllerDesktopStatus -Level Start `
    -Message "Preparing the Windows/$Architecture PCControllerDesktop $Edition edition."

$buildTag = if ($Edition -eq 'production') {
    'pccontroller_desktop_production'
}
else {
    'pccontroller_desktop_develop'
}

$sourceVersion = Get-SourceVersion
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = $sourceVersion
}
if (-not [string]::Equals($Version, $sourceVersion, [StringComparison]::Ordinal)) {
    throw "Requested version $Version does not match buildinfo.Version $sourceVersion. Update the source version before producing a release."
}

$versionMatch = [regex]::Match($Version, $semanticVersionPattern)
if (-not $versionMatch.Success) {
    throw "Version must be a strict, filename-safe semantic version such as 1.2.3 or 1.2.3-beta.1+build.4."
}
$major = [int]$versionMatch.Groups['major'].Value
$minor = [int]$versionMatch.Groups['minor'].Value
$patch = [int]$versionMatch.Groups['patch'].Value
foreach ($component in @($major, $minor, $patch)) {
    if ($component -gt 65535) {
        throw 'Windows version components must be between 0 and 65535.'
    }
}
$fileVersion = "$major.$minor.$patch.$BuildNumber"

if ([string]::IsNullOrWhiteSpace($BuildDate)) {
    $BuildDate = [DateTime]::UtcNow.ToString(
        'yyyy-MM-ddTHH:mm:ssZ',
        [Globalization.CultureInfo]::InvariantCulture
    )
}
if ([string]::IsNullOrWhiteSpace($Commit)) {
    $Commit = 'dev'
    # Source archives and Codex output folders intentionally have no .git
    # metadata. Do not invoke git there: Windows PowerShell promotes its
    # expected stderr diagnostic to a terminating NativeCommandError.
    if ((Get-Command git -ErrorAction SilentlyContinue) -and
        (Test-Path -LiteralPath (Join-Path $projectRoot '.git'))) {
        $candidate = (& git -C $projectRoot rev-parse --short=12 HEAD 2>$null)
        if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($candidate)) {
            $Commit = $candidate.Trim()
        }
    }
}
if ($Commit -notmatch '^[A-Za-z0-9._+-]{1,64}$') {
    throw 'Commit may contain only letters, numbers, dot, underscore, plus, and hyphen.'
}
if ($BuildDate -notmatch '^[0-9T:Z+.-]{1,64}$') {
    throw 'BuildDate must be a compact ISO-8601 value without spaces.'
}

$outputPath = Join-Path $distRoot 'PCControllerDesktop.exe'
$temporaryOutput = Join-Path $distRoot ('.PCControllerDesktop.' + [Guid]::NewGuid().ToString('N') + '.exe')
$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ('PCControllerDesktop-build-' + [Guid]::NewGuid().ToString('N'))
$appResource = Join-Path $projectRoot "cmd\PCControllerDesktop\zz_pccontroller_desktop_windows_$Architecture.syso"
$buildLockPath = Join-Path $distRoot '.pccontroller-desktop-build.lock'
$buildLock = $null
$ownsBuildLock = $false
$createdResources = New-Object 'Collections.Generic.List[string]'

Assert-SafeDistPath -LiteralPath $outputPath
Assert-DirectChildPath -LiteralPath $temporaryOutput -ExpectedParent $distRoot `
    -ExpectedLeafPrefix '.PCControllerDesktop.'
Assert-DirectChildPath -LiteralPath $temporaryRoot `
    -ExpectedParent ([IO.Path]::GetTempPath()) `
    -ExpectedLeafPrefix 'PCControllerDesktop-build-'

$environment = @{}
foreach ($name in @('GOOS', 'GOARCH', 'CGO_ENABLED')) {
    $environment[$name] = @{
        Existed = Test-Path -LiteralPath "Env:$name"
        Value = [Environment]::GetEnvironmentVariable($name, 'Process')
    }
}

New-Item -ItemType Directory -Force -Path $distRoot | Out-Null

try {
    try {
        $buildLock = [IO.File]::Open(
            $buildLockPath,
            [IO.FileMode]::OpenOrCreate,
            [IO.FileAccess]::ReadWrite,
            [IO.FileShare]::None
        )
        $ownsBuildLock = $true
    }
    catch {
        throw "Another PCControllerDesktop release build is active: $($_.Exception.Message)"
    }

    New-Item -ItemType Directory -Path $temporaryRoot | Out-Null

    $env:GOOS = 'windows'
    $env:CGO_ENABLED = '0'
    $hostArchitecture = (& go env GOHOSTARCH).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($hostArchitecture)) {
        throw 'Could not determine the Go host architecture.'
    }

    Push-Location $projectRoot
    try {
        $webDistRoot = Join-Path $projectRoot 'internal\webui\dist'
        if ($SkipWebBuild) {
            foreach ($requiredWebFile in @(
                'index.html',
                'build-edition.json',
                'manifest.webmanifest',
                'sw.js',
                'theme-init.js'
            )) {
                $webFile = Join-Path $webDistRoot $requiredWebFile
                if (-not (Test-Path -LiteralPath $webFile -PathType Leaf)) {
                    throw "-SkipWebBuild requires a checked-in frontend distribution; missing $webFile."
                }
            }
        }
        else {
            Write-PCControllerDesktopStatus -Level Step `
                -Icon (Get-PCControllerDesktopEmojiGlyph -Name Web) -Label 'WEB' `
                -Message 'Building the embedded Web UI.'
            & (Join-Path $PSScriptRoot 'build-web.ps1') `
                -Edition $Edition `
                -SkipTests:$SkipTests
            if ($LASTEXITCODE -ne 0) {
                throw 'Bundled web dashboard build failed.'
            }
        }
        $webEditionMarkerPath = Join-Path $webDistRoot 'build-edition.json'
        $webEditionMarker = Get-Content -LiteralPath $webEditionMarkerPath -Raw |
            ConvertFrom-Json
        if ($webEditionMarker.edition -ne $Edition -or
            [bool]$webEditionMarker.developer_tools -ne
                ($Edition -eq 'develop')) {
            throw "Embedded Web UI does not match the requested $Edition edition."
        }
        $webSourceMaps = @(
            Get-ChildItem -LiteralPath $webDistRoot -Recurse -Filter '*.map'
        )
        if ($Edition -eq 'production' -and $webSourceMaps.Count -ne 0) {
            throw 'Production Web UI must not contain source maps.'
        }
        if ($Edition -eq 'develop' -and $webSourceMaps.Count -eq 0) {
            throw 'Development Web UI must retain source maps.'
        }
        $webAssetRoot = Join-Path $webDistRoot 'assets'
        foreach ($lazyPattern in @('xterm-*.js', 'addon-webgl-*.js')) {
            if (@(
                Get-ChildItem -LiteralPath $webAssetRoot -Filter $lazyPattern
            ).Count -ne 1) {
                throw "Embedded Web UI must retain one lazy $lazyPattern chunk."
            }
        }
        if ($Edition -eq 'production') {
            $productionScripts = @(
                Get-ChildItem -LiteralPath $webDistRoot -Recurse -Filter '*.js'
            )
            foreach ($forbiddenMarker in @(
                'sourceMappingURL',
                '@preact/debug',
                '__VITEST__',
                'test-fixtures'
            )) {
                if (Select-String -LiteralPath $productionScripts.FullName `
                        -SimpleMatch -Quiet $forbiddenMarker) {
                    throw "Production Web UI contains developer marker $forbiddenMarker."
                }
            }
        }

        $env:GOARCH = $hostArchitecture
        if ((& go env CGO_ENABLED).Trim() -ne '0') {
            throw 'CGO_ENABLED=0 was not accepted by the Go toolchain.'
        }
        if ($Edition -eq 'production') {
            Write-PCControllerDesktopStatus -Level Step `
                -Icon (Get-PCControllerDesktopEmojiGlyph -Name Typecheck) -Label 'GRAPH' `
                -Message 'Verifying that developer servers and SDK bridges are absent.'
            $dependencyGraph = @(
                & go list -tags $buildTag -deps ./cmd/PCControllerDesktop
            )
            if ($LASTEXITCODE -ne 0) {
                throw 'Could not inspect the production dependency graph.'
            }
            $forbiddenDependencies = @(
                'expvar',
                'net/http/pprof',
                'runtime/pprof',
                'runtime/trace',
                'testing',
                'pccontroller.local/desktop/internal/dllapi',
                'pccontroller.local/desktop/sdk'
            )
            $unexpectedDependencies = @(
                $dependencyGraph |
                    Where-Object { $_ -in $forbiddenDependencies }
            )
            if ($unexpectedDependencies.Count -ne 0) {
                throw "Production dependency graph contains developer-only packages: $($unexpectedDependencies -join ', ')"
            }
        }
        if (-not $SkipVet) {
            Write-PCControllerDesktopStatus -Level Step `
                -Icon (Get-PCControllerDesktopEmojiGlyph -Name Typecheck) -Label 'VET' `
                -Message 'Running Go static analysis with CGO disabled.'
            & go vet -tags $buildTag ./...
            if ($LASTEXITCODE -ne 0) {
                throw 'go vet failed.'
            }
        }
        if (-not $SkipTests) {
            Write-PCControllerDesktopStatus -Level Step `
                -Icon (Get-PCControllerDesktopEmojiGlyph -Name Test) -Label 'TEST' `
                -Message 'Running the complete Go test suite.'
            & go test -count=1 -tags $buildTag ./...
            if ($LASTEXITCODE -ne 0) {
                throw 'go test failed.'
            }
        }

        $manifestPath = Join-Path $temporaryRoot 'PCControllerDesktop.exe.manifest'
        $manifest = [IO.File]::ReadAllText((Join-Path $projectRoot 'resources\PCControllerDesktop.exe.manifest'))
        $manifestPlaceholder = 'version="1.0.0.0"'
        $manifestMatchCount = [regex]::Matches(
            $manifest,
            [regex]::Escape($manifestPlaceholder)
        ).Count
        if ($manifestMatchCount -ne 1) {
            throw "Expected exactly one application version placeholder in the manifest; found $manifestMatchCount."
        }
        $manifestVersion = "version=`"$fileVersion`""
        $manifest = $manifest.Replace($manifestPlaceholder, $manifestVersion)
        if (-not $manifest.Contains($manifestVersion)) {
            throw "Failed to stamp application manifest version $fileVersion."
        }
        Write-Utf8NoBom -LiteralPath $manifestPath -Value $manifest

        # goversioninfo is a host tool. Running `go run` with GOARCH set to an
        # ARM64 target would produce an ARM64 helper that an amd64 release host
        # cannot execute. Keep the helper native and select the COFF machine
        # explicitly for the generated resource object.
        $env:GOARCH = $hostArchitecture
        $resourceIs64 = $Architecture -in @('amd64', 'arm64')
        $resourceIsArm = $Architecture -eq 'arm64'
        $resource64Flag = $resourceIs64.ToString().ToLowerInvariant()
        $resourceArmFlag = $resourceIsArm.ToString().ToLowerInvariant()

        $resourceSpecs = @(
            @{
                Source = Join-Path $projectRoot 'resources\versioninfo.json'
                Output = Join-Path $temporaryRoot 'versioninfo.json'
                Syso = $appResource
            }
        )
        Write-PCControllerDesktopStatus -Level Step `
            -Icon (Get-PCControllerDesktopEmojiGlyph -Name Resource) -Label 'RESOURCE' `
            -Message "Generating $Architecture icon, manifest, and VERSIONINFO resources."
        foreach ($spec in $resourceSpecs) {
            $info = Get-Content -LiteralPath $spec.Source -Raw | ConvertFrom-Json
            foreach ($fixed in @($info.FixedFileInfo.FileVersion, $info.FixedFileInfo.ProductVersion)) {
                $fixed.Major = $major
                $fixed.Minor = $minor
                $fixed.Patch = $patch
                $fixed.Build = $BuildNumber
            }
            $info.StringFileInfo.FileVersion = $fileVersion
            $info.StringFileInfo.ProductVersion = $Version
            Write-Utf8NoBom -LiteralPath $spec.Output -Value ($info | ConvertTo-Json -Depth 12)

            if (Test-Path -LiteralPath $spec.Syso) {
                throw "A stale generated resource already exists: $($spec.Syso)"
            }
            & go run $resourceTool `
                "-64=$resource64Flag" `
                "-arm=$resourceArmFlag" `
                "-icon=$(Join-Path $projectRoot 'assets\PCControllerDesktop.ico')" `
                "-manifest=$manifestPath" `
                "-o=$($spec.Syso)" `
                $spec.Output
            $resourceGenerated = Test-Path -LiteralPath $spec.Syso
            if ($resourceGenerated) {
                $createdResources.Add([string]$spec.Syso)
            }
            if ($LASTEXITCODE -ne 0 -or -not $resourceGenerated) {
                throw "Failed to generate Windows resources for $($spec.Source)."
            }
        }

        $env:GOARCH = $Architecture
        $linkerFlags = "-X=pccontroller.local/desktop/internal/buildinfo.Commit=$Commit -X=pccontroller.local/desktop/internal/buildinfo.Date=$BuildDate"
        if ($Edition -eq 'production') {
            $linkerFlags = "-s -w $linkerFlags"
        }
        Write-PCControllerDesktopStatus -Level Step `
            -Icon (Get-PCControllerDesktopEmojiGlyph -Name Compile) -Label 'COMPILE' `
            -Message "Cross-compiling the unified Windows/$Architecture $Edition executable."
        & go build -trimpath -buildvcs=false -tags $buildTag `
            -ldflags $linkerFlags `
            -o $temporaryOutput ./cmd/PCControllerDesktop
        if ($LASTEXITCODE -ne 0) {
            throw 'Unified PCControllerDesktop build failed.'
        }

        $env:GOARCH = $hostArchitecture
        Write-PCControllerDesktopStatus -Level Step `
            -Icon (Get-PCControllerDesktopEmojiGlyph -Name Typecheck) -Label 'PE' `
            -Message 'Verifying architecture, console subsystem, icon, manifest, version, and embedded Web UI.'
        & go run ./tools/verify-pe `
            --file $temporaryOutput `
            --arch $Architecture `
            --file-version $fileVersion `
            --edition $Edition
        if ($LASTEXITCODE -ne 0) {
            throw 'Windows executable structural verification failed.'
        }

        $compressionRequested = $Compress -or
            -not [string]::IsNullOrWhiteSpace($UPXPath)
        if ($compressionRequested) {
            if ($Edition -eq 'develop') {
                Write-Warning 'UPX makes native debugger workflows less convenient; production edition is recommended for compression.'
            }
            if ($Architecture -eq 'arm64') {
                Write-Warning 'UPX compression is skipped for ARM64; the release remains uncompressed.'
            }
            else {
                $upxExecutable = Resolve-UPXExecutable `
                    -RequestedPath $UPXPath `
                    -SearchPath $true
                if ($null -eq $upxExecutable) {
                    Write-Warning 'UPX was requested but is unavailable; the release remains uncompressed.'
                }
                else {
                    Write-PCControllerDesktopStatus -Level Step `
                        -Icon (Get-PCControllerDesktopEmojiGlyph -Name Compress) -Label 'UPX' `
                        -Message 'Compressing and integrity-checking the executable.'
                    & $upxExecutable --best --lzma --no-progress -- $temporaryOutput
                    if ($LASTEXITCODE -ne 0) {
                        throw 'UPX compression failed.'
                    }
                    & $upxExecutable -t --no-progress -- $temporaryOutput
                    if ($LASTEXITCODE -ne 0) {
                        throw 'UPX integrity verification failed.'
                    }
                    & go run ./tools/verify-pe `
                        --file $temporaryOutput `
                        --arch $Architecture `
                        --file-version $fileVersion `
                        --packed
                    if ($LASTEXITCODE -ne 0) {
                        throw 'Packed Windows executable structural verification failed.'
                    }
                    Write-PCControllerDesktopStatus -Level Success `
                        -Message "Compressed PCControllerDesktop.exe with $upxExecutable"
                }
            }
        }
    }
    finally {
        Pop-Location
    }

    if (Test-Path -LiteralPath $outputPath) {
        if (-not $Force) {
            throw "Release executable already exists: $outputPath. Pass -Force to replace it."
        }
        Assert-SafeDistPath -LiteralPath $outputPath
        Remove-Item -LiteralPath $outputPath -Force
    }
    Move-Item -LiteralPath $temporaryOutput -Destination $outputPath
    $outputHash = (Get-FileHash -LiteralPath $outputPath -Algorithm SHA256).Hash.ToLowerInvariant()
    Write-PCControllerDesktopStatus -Level Artifact -Message $outputPath
    Write-PCControllerDesktopStatus -Level Info `
        -Icon (Get-PCControllerDesktopEmojiGlyph -Name Hash) -Label 'SHA-256' `
        -Message $outputHash
    Write-PCControllerDesktopStatus -Level Success `
        -Message "Built the $Edition edition with CGO_ENABLED=0; one console-subsystem executable contains GUI, CLI, TUI, and Web modes."
    Write-Output $outputPath
}
finally {
    foreach ($resource in $createdResources) {
        if (Test-Path -LiteralPath $resource) {
            Remove-Item -LiteralPath $resource -Force
        }
    }
    if (Test-Path -LiteralPath $temporaryRoot) {
        Remove-Item -LiteralPath $temporaryRoot -Recurse -Force
    }
    if (Test-Path -LiteralPath $temporaryOutput) {
        Remove-Item -LiteralPath $temporaryOutput -Force
    }
    foreach ($name in @('GOOS', 'GOARCH', 'CGO_ENABLED')) {
        Restore-EnvironmentValue -Name $name `
            -Value $environment[$name].Value `
            -Existed $environment[$name].Existed
    }
    if ($ownsBuildLock -and $null -ne $buildLock) {
        $buildLock.Dispose()
        if (Test-Path -LiteralPath $buildLockPath) {
            Remove-Item -LiteralPath $buildLockPath -Force
        }
    }
}
