[CmdletBinding()]
param(
    [string]$SourcePath,
    [switch]$StartWithWindows,
    [switch]$RegisterShellIntegration,
    [switch]$RegisterCOM,
    [switch]$Launch,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'terminal.ps1')

$localAppData = [Environment]::GetFolderPath('LocalApplicationData')
if ([string]::IsNullOrWhiteSpace($localAppData)) {
    throw 'The current user LocalAppData directory is unavailable.'
}
$programsRoot = Join-Path $localAppData 'Programs'
$installPath = [IO.Path]::GetFullPath((Join-Path $programsRoot 'RayanLamp'))
$installedExecutable = Join-Path $installPath 'RayanLamp.exe'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$expectedRunValue = '"{0}"' -f $installedExecutable

function Resolve-SourceExecutable {
    param([string]$Requested)

    if (-not [string]::IsNullOrWhiteSpace($Requested)) {
        $resolved = Get-Item -LiteralPath $Requested -ErrorAction Stop
        if ($resolved.PSIsContainer) {
            $executables = @(
                Get-ChildItem -LiteralPath $resolved.FullName -Filter '*.exe' -File
            )
            if ($executables.Count -ne 1 -or
                -not [string]::Equals(
                    $executables[0].Name,
                    'RayanLamp.exe',
                    [StringComparison]::OrdinalIgnoreCase
                )) {
                throw 'A source directory must contain exactly one executable: RayanLamp.exe.'
            }
            return [IO.Path]::GetFullPath($executables[0].FullName)
        }
        return [IO.Path]::GetFullPath($resolved.FullName)
    }

    $projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
    $candidates = @(
        @(
            (Join-Path $PSScriptRoot 'RayanLamp.exe'),
            (Join-Path $projectRoot 'dist\RayanLamp.exe')
        ) | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf }
    )
    if ($candidates.Count -ne 1) {
        throw 'Specify -SourcePath. Automatic discovery requires exactly one RayanLamp.exe.'
    }
    return [IO.Path]::GetFullPath($candidates[0])
}

function Assert-RayanLampExecutable {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)

    if (-not (Test-Path -LiteralPath $LiteralPath -PathType Leaf) -or
        -not [string]::Equals(
            [IO.Path]::GetFileName($LiteralPath),
            'RayanLamp.exe',
            [StringComparison]::OrdinalIgnoreCase
        )) {
        throw "The source is not the unified RayanLamp.exe: $LiteralPath"
    }
}

function Assert-ExactInstallPath {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)

    $expected = [IO.Path]::GetFullPath(
        (Join-Path $localAppData 'Programs\RayanLamp')
    )
    $actual = [IO.Path]::GetFullPath($LiteralPath)
    if (-not [string]::Equals(
        $actual,
        $expected,
        [StringComparison]::OrdinalIgnoreCase
    )) {
        throw "Refusing to replace an unexpected installation path: $actual"
    }
}

function Test-ShellRegistrationOwnedBy {
    param([Parameter(Mandatory = $true)][string]$Executable)

    $commandKey = 'Registry::HKEY_CURRENT_USER\Software\Classes\rayan-lamp\shell\open\command'
    if (-not (Test-Path -LiteralPath $commandKey)) {
        return $false
    }
    $registeredCommand = (Get-Item -LiteralPath $commandKey -ErrorAction Stop).GetValue('')
    if ($null -eq $registeredCommand) {
        return $false
    }
    $expectedCommand = '"{0}" /uri "%1"' -f $Executable
    return [string]::Equals(
        ([string]$registeredCommand).Trim(),
        $expectedCommand,
        [StringComparison]::OrdinalIgnoreCase
    )
}

function Test-COMRegistrationOwnedBy {
    param([Parameter(Mandatory = $true)][string]$Executable)

    $clsid = '{D9603C3B-7D90-53B2-AC01-7656B027603F}'
    $subkey = "Software\Classes\CLSID\$clsid\LocalServer32"
    $expectedCommand = '"{0}" /com-server' -f $Executable
    foreach ($view in @(
        [Microsoft.Win32.RegistryView]::Registry64,
        [Microsoft.Win32.RegistryView]::Registry32
    )) {
        $baseKey = $null
        $serverKey = $null
        try {
            $baseKey = [Microsoft.Win32.RegistryKey]::OpenBaseKey(
                [Microsoft.Win32.RegistryHive]::CurrentUser,
                $view
            )
            $serverKey = $baseKey.OpenSubKey($subkey, $false)
            if ($null -ne $serverKey) {
                $command = [string]$serverKey.GetValue('', $null)
                if ([string]::Equals(
                    $command.Trim(),
                    $expectedCommand,
                    [StringComparison]::OrdinalIgnoreCase
                )) {
                    return $true
                }
            }
        }
        catch [System.PlatformNotSupportedException] {
            continue
        }
        finally {
            if ($null -ne $serverKey) {
                $serverKey.Dispose()
            }
            if ($null -ne $baseKey) {
                $baseKey.Dispose()
            }
        }
    }
    return $false
}

function Get-StartWithWindowsRegistration {
    if (-not (Test-Path -LiteralPath $runKey)) {
        return [PSCustomObject]@{
            Exists = $false
            Command = $null
        }
    }
    try {
        $command = (
            Get-ItemProperty `
                -LiteralPath $runKey `
                -Name 'RayanLamp' `
                -ErrorAction Stop
        ).RayanLamp
        return [PSCustomObject]@{
            Exists = $true
            Command = [string]$command
        }
    }
    catch [System.Management.Automation.ItemNotFoundException] {
        return [PSCustomObject]@{
            Exists = $false
            Command = $null
        }
    }
    catch [System.Management.Automation.PSArgumentException] {
        return [PSCustomObject]@{
            Exists = $false
            Command = $null
        }
    }
}

function Test-StartWithWindowsRegistrationOwned {
    param([Parameter(Mandatory = $true)]$Registration)

    return $Registration.Exists -and [string]::Equals(
        ([string]$Registration.Command).Trim(),
        $expectedRunValue,
        [StringComparison]::OrdinalIgnoreCase
    )
}

function Wait-ControllerShutdown {
    param(
        [Parameter(Mandatory = $true)][string]$Controller,
        [string[]]$PrefixArguments = @(),
        [int]$TimeoutMilliseconds = 8000
    )

    $deadline = [DateTime]::UtcNow.AddMilliseconds($TimeoutMilliseconds)
    do {
        & $Controller @PrefixArguments --status --quiet 2>$null | Out-Null
        if ($LASTEXITCODE -ne 0) {
            return
        }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'The running RayanLamp primary did not release IPC before the upgrade timeout.'
}

function Move-DirectoryWithRetry {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination,
        [int]$TimeoutMilliseconds = 8000
    )

    $deadline = [DateTime]::UtcNow.AddMilliseconds($TimeoutMilliseconds)
    while ($true) {
        try {
            Move-Item -LiteralPath $Source -Destination $Destination -ErrorAction Stop
            return
        }
        catch {
            if ([DateTime]::UtcNow -ge $deadline) {
                throw
            }
            Start-Sleep -Milliseconds 100
        }
    }
}

$sourceExecutable = Resolve-SourceExecutable -Requested $SourcePath
Assert-RayanLampExecutable -LiteralPath $sourceExecutable
Assert-ExactInstallPath -LiteralPath $installPath
if ([string]::Equals(
    $sourceExecutable,
    [IO.Path]::GetFullPath((Join-Path $installPath 'RayanLamp.exe')),
    [StringComparison]::OrdinalIgnoreCase
)) {
    throw 'The source executable is already installed.'
}

New-Item -ItemType Directory -Force -Path $programsRoot | Out-Null
$stagingPath = Join-Path $programsRoot (
    'RayanLamp.install.' + [Guid]::NewGuid().ToString('N')
)
$backupPath = Join-Path $programsRoot (
    'RayanLamp.backup.' + [Guid]::NewGuid().ToString('N')
)
$movedExisting = $false
$installedNew = $false
$shellWasRegistered = $false
$shellRegisteredThisRun = $false
$comWasRegistered = $false
$comRegisteredThisRun = $false
$runRegisteredThisRun = $false

if ($RegisterShellIntegration) {
    $shellWasRegistered = Test-ShellRegistrationOwnedBy `
        -Executable $installedExecutable
}
if ($RegisterCOM) {
    $comWasRegistered = Test-COMRegistrationOwnedBy `
        -Executable $installedExecutable
}
if ($StartWithWindows) {
    $runRegistration = Get-StartWithWindowsRegistration
    if ($runRegistration.Exists -and
        -not (Test-StartWithWindowsRegistrationOwned -Registration $runRegistration)) {
        throw (
            'The current-user RayanLamp startup value belongs to another command ' +
            "and was preserved: $($runRegistration.Command)"
        )
    }
}

try {
    New-Item -ItemType Directory -Path $stagingPath | Out-Null
    $stagedExecutable = Join-Path $stagingPath 'RayanLamp.exe'
    Copy-Item -LiteralPath $sourceExecutable -Destination $stagedExecutable
    Assert-RayanLampExecutable -LiteralPath $stagedExecutable
    $sourceHash = (Get-FileHash -LiteralPath $sourceExecutable -Algorithm SHA256).Hash
    $stagedHash = (Get-FileHash -LiteralPath $stagedExecutable -Algorithm SHA256).Hash
    if (-not [string]::Equals(
        $sourceHash,
        $stagedHash,
        [StringComparison]::OrdinalIgnoreCase
    )) {
        throw 'The staged executable does not match the source SHA-256.'
    }

    if (Test-Path -LiteralPath $installPath) {
        if (-not $Force) {
            throw 'RayanLamp is already installed. Pass -Force for an in-place upgrade.'
        }
        $oldController = Join-Path $installPath 'RayanLamp.exe'
        $controllerPrefix = @('cli')
        $legacyController = Join-Path $installPath 'rayanlampctl.exe'
        if (Test-Path -LiteralPath $legacyController -PathType Leaf) {
            $oldController = $legacyController
            $controllerPrefix = @()
        }
        if (Test-Path -LiteralPath $oldController -PathType Leaf) {
            & $oldController @controllerPrefix --quit --quiet 2>$null
            Wait-ControllerShutdown `
                -Controller $oldController `
                -PrefixArguments $controllerPrefix
        }
        Move-DirectoryWithRetry -Source $installPath -Destination $backupPath
        $movedExisting = $true
    }

    Move-Item -LiteralPath $stagingPath -Destination $installPath
    $installedNew = $true

    if ($StartWithWindows) {
        # Recheck immediately before writing so a registration transferred
        # during staging is never deliberately overwritten.
        $runRegistration = Get-StartWithWindowsRegistration
        if ($runRegistration.Exists) {
            if (-not (
                Test-StartWithWindowsRegistrationOwned `
                    -Registration $runRegistration
            )) {
                throw (
                    'The current-user RayanLamp startup value changed ownership ' +
                    "during installation and was preserved: $($runRegistration.Command)"
                )
            }
        }
        else {
            New-Item -Path $runKey -Force | Out-Null
            Set-ItemProperty -Path $runKey -Name 'RayanLamp' -Type String `
                -Value $expectedRunValue
            $runRegisteredThisRun = $true
        }
    }
    if ($RegisterShellIntegration) {
        & $installedExecutable /register-shell /quiet
        $shellExitCode = $LASTEXITCODE
        if ($shellExitCode -eq 0) {
            $shellRegisteredThisRun = -not $shellWasRegistered
        }
        elseif ($shellExitCode -eq 3) {
            $shellRegisteredThisRun = -not $shellWasRegistered
            Write-Warning (
                'The URI protocol was registered, but Windows did not accept every ' +
                'Jump List operation. The installed URI integration remains usable.'
            )
        }
        else {
            throw "Per-user shell integration failed with exit code $shellExitCode."
        }
    }
    if ($RegisterCOM) {
        & $installedExecutable /register-com /quiet
        if ($LASTEXITCODE -ne 0) {
            throw 'Per-user COM registration failed.'
        }
        $comRegisteredThisRun = -not $comWasRegistered
    }
    if ($Launch) {
        Start-Process -FilePath $installedExecutable
    }

    if ($movedExisting -and (Test-Path -LiteralPath $backupPath)) {
        Remove-Item -LiteralPath $backupPath -Recurse -Force
    }
    Write-RayanLampStatus -Level Success `
        -Icon (Get-RayanLampEmojiGlyph -Name Lamp) -Label 'INSTALLED' `
        -Message "RayanLamp is installed for the current user at $installPath"
}
catch {
    $installFailure = $_
    if ($runRegisteredThisRun) {
        try {
            $runRegistration = Get-StartWithWindowsRegistration
            if (Test-StartWithWindowsRegistrationOwned -Registration $runRegistration) {
                Remove-ItemProperty `
                    -LiteralPath $runKey `
                    -Name 'RayanLamp' `
                    -ErrorAction Stop
            }
            elseif ($runRegistration.Exists) {
                Write-Warning (
                    'Installer rollback preserved a RayanLamp startup value that ' +
                    "changed ownership: $($runRegistration.Command)"
                )
            }
        }
        catch {
            Write-Warning (
                'Installer rollback could not remove the newly registered ' +
                "start-with-Windows value: $($_.Exception.Message)"
            )
        }
    }
    if ($comRegisteredThisRun -and
        (Test-Path -LiteralPath $installedExecutable -PathType Leaf)) {
        & $installedExecutable /unregister-com /quiet
        if ($LASTEXITCODE -ne 0) {
            Write-Warning (
                'Installer rollback could not remove the newly registered COM ' +
                'Automation server. Run RayanLamp.exe /unregister-com to retry.'
            )
        }
    }
    if ($shellRegisteredThisRun -and
        (Test-Path -LiteralPath $installedExecutable -PathType Leaf)) {
        & $installedExecutable /unregister-shell /quiet
        if ($LASTEXITCODE -ne 0) {
            Write-Warning (
                'Installer rollback could not remove every newly registered shell ' +
                'integration. Run RayanLamp.exe /unregister-shell to retry.'
            )
        }
    }
    if ($installedNew -and (Test-Path -LiteralPath $installPath)) {
        Assert-ExactInstallPath -LiteralPath $installPath
        Remove-Item -LiteralPath $installPath -Recurse -Force
    }
    if ($movedExisting -and (Test-Path -LiteralPath $backupPath)) {
        Move-Item -LiteralPath $backupPath -Destination $installPath
    }
    throw $installFailure
}
finally {
    if (Test-Path -LiteralPath $stagingPath) {
        Remove-Item -LiteralPath $stagingPath -Recurse -Force
    }
}
