[CmdletBinding()]
param(
    [ValidateSet('amd64', '386', 'all')]
    [string]$Architecture = 'amd64',
    [ValidateRange(0, 65535)]
    [int]$BuildNumber = 0,
    [string]$CCPath,
    [switch]$SkipTests,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$sdkRoot = [IO.Path]::GetFullPath((Join-Path $projectRoot 'dist-sdk'))
$buildInfoPath = Join-Path $projectRoot 'internal\buildinfo\buildinfo.go'
$resourceTemplate = Join-Path $projectRoot 'resources\RayanLamp.dll.rc.in'
$nativeRoot = Join-Path $projectRoot 'sdk\native'
$headerPath = Join-Path $nativeRoot 'RayanLamp.h'
$wrapperPath = Join-Path $nativeRoot 'RayanLamp.c'

function Get-SourceVersion {
    $source = [IO.File]::ReadAllText($buildInfoPath)
    $match = [regex]::Match(
        $source,
        '(?m)^\s*Version\s*=\s*"(?<version>[0-9]+\.[0-9]+\.[0-9]+)"'
    )
    if (-not $match.Success) {
        throw "Could not read the SDK version from $buildInfoPath."
    }
    return $match.Groups['version'].Value
}

function Resolve-SDKCompiler {
    param(
        [Parameter(Mandatory = $true)][string]$TargetArchitecture,
        [string]$RequestedPath
    )

    $expectedMachine = if ($TargetArchitecture -eq 'amd64') {
        'x86_64-w64-mingw32'
    }
    else {
        'i686-w64-mingw32'
    }
    $compilerName = $expectedMachine + '-gcc.exe'
    $candidates = New-Object 'Collections.Generic.List[string]'
    if (-not [string]::IsNullOrWhiteSpace($RequestedPath)) {
        $candidates.Add($RequestedPath)
    }
    else {
        $gitRoot = Join-Path $env:ProgramFiles 'Git'
        $gitCandidate = if ($TargetArchitecture -eq 'amd64') {
            Join-Path $gitRoot 'mingw64\bin\x86_64-w64-mingw32-gcc.exe'
        }
        else {
            Join-Path $gitRoot 'mingw32\bin\i686-w64-mingw32-gcc.exe'
        }
        $candidates.Add($gitCandidate)
        $command = Get-Command $compilerName -CommandType Application `
            -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($null -ne $command) {
            $candidates.Add($command.Source)
        }
    }

    foreach ($candidate in $candidates) {
        if (-not (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            continue
        }
        $resolved = [IO.Path]::GetFullPath($candidate)
        if ($resolved -match '(?i)[\\/]usr[\\/]bin[\\/]') {
            # Git/MSYS's /usr/bin cross compiler links against the MSYS C
            # runtime. It is not the standalone MinGW-w64 toolchain.
            continue
        }
        $machine = (& $resolved -dumpmachine 2>$null | Select-Object -First 1)
        if (-not $?) {
            continue
        }
        if (-not [string]::Equals(
            ([string]$machine).Trim(),
            $expectedMachine,
            [StringComparison]::OrdinalIgnoreCase
        )) {
            continue
        }
        return $resolved
    }
    throw "No validated $expectedMachine MinGW compiler was found. " +
        "Use -CCPath with the matching compiler; MSYS-target GCC is rejected."
}

function Assert-ChildPath {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][string]$ExpectedParent
    )
    $path = [IO.Path]::GetFullPath($LiteralPath)
    $parent = [IO.Path]::GetFullPath($ExpectedParent).TrimEnd('\', '/')
    if (-not [string]::Equals(
        [IO.Path]::GetDirectoryName($path),
        $parent,
        [StringComparison]::OrdinalIgnoreCase
    )) {
        throw "Unsafe SDK output path: $path"
    }
}

function Write-Utf8NoBom {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][string]$Value
    )
    [IO.File]::WriteAllText(
        $LiteralPath,
        $Value,
        [Text.UTF8Encoding]::new($false)
    )
}

function Invoke-SDKBuild {
    param(
        [Parameter(Mandatory = $true)][string]$TargetArchitecture,
        [string]$RequestedCC,
        [Parameter(Mandatory = $true)][string]$FileVersion,
        [Parameter(Mandatory = $true)][string]$ProductVersion
    )

    $cc = Resolve-SDKCompiler `
        -TargetArchitecture $TargetArchitecture `
        -RequestedPath $RequestedCC
    $toolchainBin = Split-Path -Parent $cc
    $compilerPrefix = if ($TargetArchitecture -eq 'amd64') {
        'x86_64-w64-mingw32'
    }
    else {
        'i686-w64-mingw32'
    }
    $windres = Join-Path $toolchainBin ($compilerPrefix + '-windres.exe')
    if (-not (Test-Path -LiteralPath $windres -PathType Leaf)) {
        $windres = Join-Path $toolchainBin 'windres.exe'
    }
    if (-not (Test-Path -LiteralPath $windres -PathType Leaf)) {
        throw "The validated MinGW toolchain has no windres.exe: $toolchainBin"
    }

    $outputDirectory = Join-Path $sdkRoot $TargetArchitecture
    Assert-ChildPath -LiteralPath $outputDirectory -ExpectedParent $sdkRoot
    if (Test-Path -LiteralPath $outputDirectory) {
        if (-not $Force) {
            throw "SDK output already exists: $outputDirectory. Pass -Force to replace it."
        }
    }

    $temporaryRoot = Join-Path (
        [IO.Path]::GetTempPath()
    ) ('RayanLamp-sdk-' + [Guid]::NewGuid().ToString('N'))
    $publishDirectory = Join-Path $temporaryRoot 'publish'
    $archivePath = Join-Path $temporaryRoot 'RayanLampGo.a'
    $generatedHeader = Join-Path $temporaryRoot 'RayanLampGo.h'
    $resourceSource = Join-Path $temporaryRoot 'RayanLamp.dll.rc'
    $resourceObject = Join-Path $temporaryRoot 'RayanLamp.dll.o'
    $temporaryDLL = Join-Path $publishDirectory 'RayanLamp.dll'
    $temporaryImportLibrary = Join-Path $publishDirectory 'libRayanLamp.dll.a'
    $smokeExecutable = Join-Path $temporaryRoot 'RayanLamp-dll-smoke.exe'
    $definitionPath = Join-Path (
        $nativeRoot
    ) ("RayanLamp_$TargetArchitecture.def")

    $savedEnvironment = @{}
    foreach ($name in @(
        'GOOS',
        'GOARCH',
        'CGO_ENABLED',
        'CC',
        'CXX',
        'PATH'
    )) {
        $savedEnvironment[$name] = @{
            Exists = Test-Path -LiteralPath "Env:$name"
            Value = [Environment]::GetEnvironmentVariable($name, 'Process')
        }
    }

    try {
        New-Item -ItemType Directory -Path $publishDirectory | Out-Null
        $env:PATH = $toolchainBin + [IO.Path]::PathSeparator + $env:PATH
        $env:GOOS = 'windows'
        $env:GOARCH = $TargetArchitecture
        $env:CGO_ENABLED = '1'
        # Go tokenizes CC itself. Keeping the executable name space-free while
        # prepending its validated directory avoids the C:\Program Files split.
        $env:CC = [IO.Path]::GetFileName($cc)
        $cxxCandidate = Join-Path $toolchainBin ($compilerPrefix + '-g++.exe')
        if (-not (Test-Path -LiteralPath $cxxCandidate -PathType Leaf)) {
            $cxxCandidate = Join-Path $toolchainBin 'g++.exe'
        }
        $env:CXX = [IO.Path]::GetFileName($cxxCandidate)

        $resource = [IO.File]::ReadAllText($resourceTemplate).
            Replace(
                '@FILE_VERSION_COMMAS@',
                $FileVersion.Replace('.', ',')
            ).
            Replace('@FILE_VERSION@', $FileVersion).
            Replace('@PRODUCT_VERSION@', $ProductVersion)
        Write-Utf8NoBom -LiteralPath $resourceSource -Value $resource

        Push-Location $projectRoot
        try {
            Write-Host "Building attach-only RayanLamp SDK for Windows/$TargetArchitecture"
            & go build `
                -trimpath `
                -buildvcs=false `
                -buildmode=c-archive `
                -tags 'rayanlamp_sdk,rayanlamp_production' `
                -ldflags '-s -w' `
                -o $archivePath `
                ./cmd/RayanLampDLL
            if ($LASTEXITCODE -ne 0) {
                throw "Go c-archive build failed for $TargetArchitecture."
            }
            if (-not (Test-Path -LiteralPath $generatedHeader -PathType Leaf)) {
                throw "Go did not create the expected c-archive header: $generatedHeader"
            }
            & $windres `
                --input ($resourceSource.Replace('\', '/')) `
                --output ($resourceObject.Replace('\', '/')) `
                --output-format=coff
            if ($LASTEXITCODE -ne 0) {
                throw "DLL version resource build failed for $TargetArchitecture."
            }

            $linkArguments = @(
                '-std=c11',
                '-Os',
                '-Wall',
                '-Wextra',
                '-Werror',
                '-shared',
                '-static-libgcc',
                '-Wl,--gc-sections',
                '-Wl,--exclude-all-symbols',
                "-Wl,--out-implib,$temporaryImportLibrary",
                '-I', $nativeRoot,
                '-I', $temporaryRoot,
                '-o', $temporaryDLL,
                $wrapperPath,
                $resourceObject,
                $archivePath,
                $definitionPath,
                '-lwinmm',
                '-lws2_32',
                '-lntdll'
            )
            & $cc @linkArguments
            if ($LASTEXITCODE -ne 0) {
                throw "Native SDK DLL link failed for $TargetArchitecture."
            }
            Copy-Item -LiteralPath $headerPath `
                -Destination (Join-Path $publishDirectory 'RayanLamp.h')

            $hostArchitecture = (& go env GOHOSTARCH).Trim()
            $env:GOARCH = $hostArchitecture
            $env:CGO_ENABLED = '0'
            & go run ./tools/verify-dll `
                --file $temporaryDLL `
                --arch $TargetArchitecture `
                --file-version $FileVersion
            if ($LASTEXITCODE -ne 0) {
                throw "SDK DLL structural verification failed for $TargetArchitecture."
            }

            $smokeArguments = @(
                '-std=c11',
                '-Os',
                '-Wall',
                '-Wextra',
                '-Werror',
                '-Wno-cast-function-type',
                '-municode',
                '-I', $nativeRoot,
                '-o', $smokeExecutable,
                (Join-Path $projectRoot 'tools\dll-smoke\main.c')
            )
            & $cc @smokeArguments
            if ($LASTEXITCODE -ne 0) {
                throw "SDK native smoke harness build failed for $TargetArchitecture."
            }
            & $smokeExecutable $temporaryDLL
            if ($LASTEXITCODE -ne 0) {
                throw "SDK native smoke test failed for $TargetArchitecture."
            }
        }
        finally {
            Pop-Location
        }

        if (Test-Path -LiteralPath $outputDirectory) {
            # outputDirectory was resolved and checked as one direct child of
            # dist-sdk before this exact replacement.
            Remove-Item -LiteralPath $outputDirectory -Recurse -Force
        }
        New-Item -ItemType Directory -Force -Path $sdkRoot | Out-Null
        Move-Item -LiteralPath $publishDirectory -Destination $outputDirectory
        $hash = (
            Get-FileHash `
                -LiteralPath (Join-Path $outputDirectory 'RayanLamp.dll') `
                -Algorithm SHA256
        ).Hash.ToLowerInvariant()
        Write-Host "SDK artifact: $outputDirectory"
        Write-Host "SHA-256: $hash"
        return $outputDirectory
    }
    finally {
        foreach ($name in $savedEnvironment.Keys) {
            $saved = $savedEnvironment[$name]
            if ($saved.Exists) {
                Set-Item -LiteralPath "Env:$name" -Value $saved.Value
            }
            else {
                Remove-Item -LiteralPath "Env:$name" `
                    -ErrorAction SilentlyContinue
            }
        }
        if (Test-Path -LiteralPath $temporaryRoot) {
            Remove-Item -LiteralPath $temporaryRoot -Recurse -Force
        }
    }
}

if (-not (Test-Path -LiteralPath $resourceTemplate -PathType Leaf) -or
    -not (Test-Path -LiteralPath $headerPath -PathType Leaf) -or
    -not (Test-Path -LiteralPath $wrapperPath -PathType Leaf)) {
    throw 'The native SDK source set is incomplete.'
}
if ($Architecture -eq 'all' -and
    -not [string]::IsNullOrWhiteSpace($CCPath)) {
    throw '-CCPath may select only one architecture; invoke the script once per target.'
}

$productVersion = Get-SourceVersion
$versionParts = $productVersion.Split('.')
$fileVersion = "$($versionParts[0]).$($versionParts[1]).$($versionParts[2]).$BuildNumber"

if (-not $SkipTests) {
    $savedGOOS = $env:GOOS
    $savedGOARCH = $env:GOARCH
    $savedCGO = $env:CGO_ENABLED
    try {
        $env:GOOS = 'windows'
        $env:GOARCH = (& go env GOHOSTARCH).Trim()
        $env:CGO_ENABLED = '0'
        Push-Location $projectRoot
        try {
            & go test -count=1 ./sdk ./internal/dllapi ./tools/verify-dll
            if ($LASTEXITCODE -ne 0) {
                throw 'SDK-focused Go tests failed.'
            }
        }
        finally {
            Pop-Location
        }
    }
    finally {
        $env:GOOS = $savedGOOS
        $env:GOARCH = $savedGOARCH
        $env:CGO_ENABLED = $savedCGO
    }
}

New-Item -ItemType Directory -Force -Path $sdkRoot | Out-Null
$targets = if ($Architecture -eq 'all') {
    @('amd64', '386')
}
else {
    @($Architecture)
}
foreach ($target in $targets) {
    Invoke-SDKBuild `
        -TargetArchitecture $target `
        -RequestedCC $CCPath `
        -FileVersion $fileVersion `
        -ProductVersion $productVersion
}
