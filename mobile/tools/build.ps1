param([ValidateSet('Verify','ApkPreview')][string]$Mode='Verify', [string]$Flutter='C:/development/flutter/bin/flutter.bat', [string]$InitialController='')
$ErrorActionPreference='Stop'
$root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (!(Test-Path -LiteralPath $Flutter)) { throw 'Reuse the installed Flutter SDK; no SDK installation is performed.' }
if ((Get-PSDrive ([IO.Path]::GetPathRoot($root).Substring(0,1))).Free -lt 8GB) { throw 'Less than 8 GiB free; preserve caches and reclaim reviewed material first.' }
$build=Join-Path $root 'build'
$bytes=(Get-ChildItem -LiteralPath $build -File -Recurse -ErrorAction SilentlyContinue | Measure-Object Length -Sum).Sum
if ($bytes -gt 4GB) { throw 'Mobile build cache exceeds 4 GiB; review it, do not automatically clean it.' }
Push-Location $root
$previousNoProxy=$env:NO_PROXY
try {
  $env:NO_PROXY='localhost,127.0.0.1,::1'
  & $Flutter pub get --offline --enforce-lockfile
  if ($LASTEXITCODE -ne 0) { throw 'Locked offline dependency restore failed.' }
  & $Flutter analyze --no-pub
  if ($LASTEXITCODE -ne 0) { throw 'Analysis failed.' }
  & $Flutter test --no-pub
  if ($LASTEXITCODE -ne 0) { throw 'Tests failed.' }
  if ($Mode -eq 'ApkPreview') {
    $arguments=@('build','apk','--release','--no-pub','--target-platform','android-arm64')
    if ($InitialController) { $uri=[Uri]$InitialController; if ($uri.Scheme -notin @('http','https') -or $uri.UserInfo -or $uri.Query -or $uri.Fragment) { throw 'Initial endpoint must be a non-secret HTTP(S) origin.' }; $arguments += "--dart-define=PC_ORIGIN=$InitialController" }
    & $Flutter @arguments
    if ($LASTEXITCODE -ne 0) { throw 'APK preview build failed.' }
    Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $root 'build/app/outputs/flutter-apk/app-release.apk')
  }
} finally { $env:NO_PROXY=$previousNoProxy; Pop-Location }
