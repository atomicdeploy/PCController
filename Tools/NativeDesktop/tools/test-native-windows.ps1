[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$OutputDirectory,
    [string]$ResourceTool,
    [string]$BorderBaseline
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$project = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$output = [IO.Path]::GetFullPath($OutputDirectory)
$savedEnvironment = @{}
$names = @('PCCONTROLLER_DESKTOP_WINDOW_TESTS', 'PCCONTROLLER_DESKTOP_CAPTURE_TOOL', 'PCCONTROLLER_DESKTOP_SCREENSHOTS', 'PCCONTROLLER_DESKTOP_BORDER_BASELINE')
foreach ($name in $names) { $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
New-Item -ItemType Directory -Path $output -Force | Out-Null
Push-Location $project
try {
    $arch = (& go env GOARCH).Trim()
    if ($LASTEXITCODE -ne 0 -or $arch -ne 'amd64') { throw 'Native screenshot suite currently requires Windows amd64.' }
    $capture = Join-Path $output 'window-capture.exe'
    & go build -o $capture ./tools/window-capture
    if ($LASTEXITCODE -ne 0) { throw 'Screenshot helper build failed.' }
    $env:PCCONTROLLER_DESKTOP_WINDOW_TESTS = '1'
    $env:PCCONTROLLER_DESKTOP_CAPTURE_TOOL = $capture
    $env:PCCONTROLLER_DESKTOP_SCREENSHOTS = Join-Path $output 'screenshots'
    $env:PCCONTROLLER_DESKTOP_BORDER_BASELINE = $BorderBaseline

    foreach ($suite in @(
        @{ Package = 'dialogs'; Test = '(TestSettingsWindowGeometryLifecycle|TestDiagnosticsPersianFontLifecycle)' },
        @{ Package = 'ui'; Test = 'TestShellChromeGeometryLifecycle' }
    )) {
        # Test executables need the same Common Controls v6 / DPI manifest as
        # the app. Only one package carries this resource at a time; retaining
        # it would duplicate resources when the real EXE is linked.
        $resource = Join-Path $project "internal\$($suite.Package)\zz_native_test_windows_amd64.syso"
        if (Test-Path -LiteralPath $resource) { throw "Refusing to overwrite existing test resource: $resource" }
        try {
            $resourceArgs = @('-64=true', "-icon=$(Join-Path $project 'assets\PCControllerDesktop.ico')",
                "-manifest=$(Join-Path $project 'resources\PCControllerDesktop.exe.manifest')",
                "-o=$resource", (Join-Path $project 'resources\versioninfo.json'))
            if ($ResourceTool) { & $ResourceTool @resourceArgs }
            else { & go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0 @resourceArgs }
            if ($LASTEXITCODE -ne 0) { throw 'Native test manifest generation failed.' }
            $testExe = Join-Path $output "$($suite.Package).test.exe"
            & go test -c -tags pccontroller_desktop_develop -o $testExe "./internal/$($suite.Package)"
            if ($LASTEXITCODE -ne 0) { throw "Native test compilation failed: $($suite.Package)" }
            & $testExe "-test.run=^$($suite.Test)$" '-test.v' '-test.count=1' '-test.timeout=2m' 2>&1 |
                Tee-Object -FilePath (Join-Path $output "$($suite.Package).log")
            if ($LASTEXITCODE -ne 0) { throw "Native test failed: $($suite.Package)" }
        }
        finally {
            if (Test-Path -LiteralPath $resource) { Remove-Item -LiteralPath $resource -Force }
        }
    }
}
finally {
    Pop-Location
    foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process') }
}
