$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repository = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$lockPath = Join-Path $repository 'Tools\Dependencies\resolved-tools-lock.json'
$lock = Get-Content -LiteralPath $lockPath -Raw | ConvertFrom-Json
$compiler = $lock.windows_c_compiler

if (-not $compiler.package_id -or -not $compiler.package_version -or
    -not $compiler.compiler_version -or -not $compiler.target) {
    throw 'The resolved host-tool lock has no complete Windows C compiler identity.'
}

function Get-CompilerCandidates {
    $candidates = [System.Collections.Generic.List[string]]::new()
    foreach ($name in @('x86_64-w64-mingw32-gcc.exe', 'gcc.exe')) {
        Get-Command $name -All -ErrorAction SilentlyContinue | ForEach-Object {
            $candidates.Add($_.Source)
        }
    }

    $runnerCompiler = 'C:\mingw64\bin\gcc.exe'
    if (Test-Path -LiteralPath $runnerCompiler -PathType Leaf) {
        $candidates.Add($runnerCompiler)
    }

    $packageRoot = Join-Path $env:LOCALAPPDATA 'Microsoft\WinGet\Packages'
    if (Test-Path -LiteralPath $packageRoot -PathType Container) {
        Get-ChildItem -LiteralPath $packageRoot -Directory |
            Where-Object { $_.Name.StartsWith("$($compiler.package_id)_", [StringComparison]::OrdinalIgnoreCase) } |
            ForEach-Object {
                $candidate = Join-Path $_.FullName 'mingw64\bin\gcc.exe'
                if (Test-Path -LiteralPath $candidate -PathType Leaf) {
                    $candidates.Add($candidate)
                }
            }
    }

    return $candidates | Select-Object -Unique
}

function Select-LockedCompiler {
    foreach ($candidate in Get-CompilerCandidates) {
        $version = (& $candidate -dumpfullversion).Trim()
        if ($LASTEXITCODE -ne 0) { continue }
        $target = (& $candidate -dumpmachine).Trim()
        if ($LASTEXITCODE -ne 0) { continue }
        Write-Host "Examined Windows C compiler $candidate ($version / $target)."
        if ($version -eq $compiler.compiler_version -and $target -eq $compiler.target) {
            return $candidate
        }
    }
    return $null
}

$selected = Select-LockedCompiler
if (-not $selected) {
    $winget = Get-Command winget -ErrorAction SilentlyContinue
    if (-not $winget) {
        throw "Locked Windows C compiler $($compiler.compiler_version) is absent and Windows Package Manager is unavailable."
    }

    Write-Host "Provisioning locked Windows C compiler package $($compiler.package_id) $($compiler.package_version)."
    $arguments = @(
        'install', '--id', $compiler.package_id, '--exact', '--source', 'winget',
        '--scope', 'user', '--architecture', 'x64', '--version', $compiler.package_version,
        '--silent', '--accept-package-agreements', '--accept-source-agreements', '--disable-interactivity'
    )
    $proxy = $env:HTTPS_PROXY
    if (-not $proxy) { $proxy = $env:HTTP_PROXY }
    if (-not $proxy) { $proxy = $env:ALL_PROXY }
    if ($proxy) { $arguments += @('--proxy', $proxy) }
    & $winget.Source @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Windows Package Manager failed to provision $($compiler.package_id) $($compiler.package_version)."
    }
    $selected = Select-LockedCompiler
}

if (-not $selected) {
    throw "Locked Windows C compiler $($compiler.compiler_version) / $($compiler.target) was not discoverable after provisioning."
}

$compilerBin = Split-Path -Parent $selected
Add-Content -LiteralPath $env:GITHUB_PATH -Value $compilerBin
Add-Content -LiteralPath $env:GITHUB_ENV -Value "CC=$selected"
Write-Host "Selected locked Windows C compiler: $selected"
& $selected --version
