[CmdletBinding()]
param(
    [switch]$RemoveUserData
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'terminal.ps1')

$localAppData = [Environment]::GetFolderPath('LocalApplicationData')
$appData = [Environment]::GetFolderPath('ApplicationData')
if ([string]::IsNullOrWhiteSpace($localAppData)) {
    throw 'The current user LocalAppData directory is unavailable.'
}

$installPath = [IO.Path]::GetFullPath((Join-Path $localAppData 'Programs\RayanLamp'))
$installedExecutable = Join-Path $installPath 'RayanLamp.exe'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'

function Assert-ExactPath {
    param(
        [Parameter(Mandatory = $true)][string]$Actual,
        [Parameter(Mandatory = $true)][string]$Expected,
        [Parameter(Mandatory = $true)][string]$Purpose
    )
    $actualFull = [IO.Path]::GetFullPath($Actual)
    $expectedFull = [IO.Path]::GetFullPath($Expected)
    if (-not [string]::Equals($actualFull, $expectedFull, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to remove an unexpected $Purpose path: $actualFull"
    }
}

function Test-ActivationCommandOwnedBy {
    param(
        [AllowNull()][string]$Command,
        [Parameter(Mandatory = $true)][string]$Executable
    )

    if ($null -eq $Command) {
        return $false
    }
    $candidate = $Command.Trim()
    if ($candidate.Length -ge 2 -and
        $candidate[0] -eq '"' -and
        $candidate[$candidate.Length - 1] -eq '"') {
        $candidate = $candidate.Substring(1, $candidate.Length - 2)
    }
    if ([string]::IsNullOrWhiteSpace($candidate) -or $candidate.Contains('"')) {
        return $false
    }
    return [string]::Equals(
        $candidate,
        $Executable,
        [StringComparison]::OrdinalIgnoreCase
    )
}

function Remove-OwnedShellRegistrationWithoutExecutable {
    param([Parameter(Mandatory = $true)][string]$Executable)

    $protocolRoot = 'Registry::HKEY_CURRENT_USER\Software\Classes\rayan-lamp'
    $commandKey = Join-Path $protocolRoot 'shell\open\command'
    $expectedCommand = '"{0}" /uri "%1"' -f $Executable
    if (Test-Path -LiteralPath $commandKey) {
        $currentCommand = (Get-Item -LiteralPath $commandKey -ErrorAction Stop).GetValue('')
        if ([string]::Equals(
            ([string]$currentCommand).Trim(),
            $expectedCommand,
            [StringComparison]::OrdinalIgnoreCase
        )) {
            Remove-Item -LiteralPath $protocolRoot -Recurse -Force
        }
    }

    $legacyActivator = '{66F0C709-6E91-45BE-9013-5A96219E6D3D}'
    $activatorRoot = "Registry::HKEY_CURRENT_USER\Software\Classes\CLSID\$legacyActivator"
    $activatorServer = Join-Path $activatorRoot 'LocalServer32'
    $appIDRoot = 'Registry::HKEY_CURRENT_USER\Software\Classes\AppUserModelId\YektaYar.RayanLamp'
    $activationCommand = $null
    if (Test-Path -LiteralPath $activatorServer) {
        $activationCommand = (Get-Item -LiteralPath $activatorServer -ErrorAction Stop).GetValue('')
    }
    $activationOwned = Test-ActivationCommandOwnedBy `
        -Command $activationCommand `
        -Executable $Executable
    if ($activationOwned -and (Test-Path -LiteralPath $activatorRoot)) {
        Remove-Item -LiteralPath $activatorRoot -Recurse -Force
    }

    $customActivator = $null
    if (Test-Path -LiteralPath $appIDRoot) {
        $customActivator = (Get-Item -LiteralPath $appIDRoot -ErrorAction Stop).GetValue(
            'CustomActivator'
        )
    }
    if ($null -ne $customActivator -and
        [string]::Equals(
            ([string]$customActivator).Trim(),
            $legacyActivator,
            [StringComparison]::OrdinalIgnoreCase
        ) -and
        ($null -eq $activationCommand -or $activationOwned) -and
        (Test-Path -LiteralPath $appIDRoot)) {
        Remove-Item -LiteralPath $appIDRoot -Recurse -Force
    }

    Write-Warning (
        'RayanLamp.exe was missing, so its owned registry entries were removed ' +
        'directly. Windows may retain its Jump List until the shell refreshes it.'
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
    throw 'The running RayanLamp primary did not release IPC before the uninstall timeout.'
}

function Remove-DirectoryWithRetry {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [int]$TimeoutMilliseconds = 8000
    )

    $deadline = [DateTime]::UtcNow.AddMilliseconds($TimeoutMilliseconds)
    while ($true) {
        try {
            Remove-Item -LiteralPath $LiteralPath -Recurse -Force -ErrorAction Stop
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

Assert-ExactPath -Actual $installPath `
    -Expected (Join-Path $localAppData 'Programs\RayanLamp') `
    -Purpose 'installation'

$controller = $installedExecutable
$controllerPrefix = @('cli')
$legacyController = Join-Path $installPath 'rayanlampctl.exe'
if (Test-Path -LiteralPath $legacyController -PathType Leaf) {
    $controller = $legacyController
    $controllerPrefix = @()
}
if (Test-Path -LiteralPath $controller -PathType Leaf) {
    & $controller @controllerPrefix --quit --quiet 2>$null
    Wait-ControllerShutdown `
        -Controller $controller `
        -PrefixArguments $controllerPrefix
}

$shellCommandKey = 'Registry::HKEY_CURRENT_USER\Software\Classes\rayan-lamp\shell\open\command'
$shellCommandExists = Test-Path -LiteralPath $shellCommandKey
$registeredShellCommand = $null
if ($shellCommandExists) {
    $registeredShellCommand = (
        Get-Item -LiteralPath $shellCommandKey -ErrorAction Stop
    ).GetValue('')
}
$expectedShellCommand = '"{0}" /uri "%1"' -f $installedExecutable
if ($shellCommandExists -and
    [string]::Equals(
        ([string]$registeredShellCommand).Trim(),
        $expectedShellCommand,
        [StringComparison]::OrdinalIgnoreCase
    )) {
    if (Test-Path -LiteralPath $installedExecutable -PathType Leaf) {
        & $installedExecutable /unregister-shell /quiet
        $shellExitCode = $LASTEXITCODE
        if ($shellExitCode -eq 3) {
            Write-Warning (
                'The owned URI registration was removed, but Windows did not ' +
                'complete every Jump List or legacy metadata cleanup operation.'
            )
        }
        elseif ($shellExitCode -ne 0) {
            Write-Warning (
                "Owned shell integration cleanup failed with exit code " +
                "$shellExitCode. Uninstallation will continue."
            )
        }
    }
    else {
        Remove-OwnedShellRegistrationWithoutExecutable `
            -Executable $installedExecutable
    }
}
elseif ($shellCommandExists) {
    Write-Warning (
        'The rayan-lamp URI protocol points elsewhere or has no owner command ' +
        'and was preserved.'
    )
}

if (Test-Path -LiteralPath $runKey) {
    $runValue = $null
    try {
        $runValue = (Get-ItemProperty -LiteralPath $runKey -Name 'RayanLamp' -ErrorAction Stop).RayanLamp
    }
    catch [System.Management.Automation.ItemNotFoundException] {
        $runValue = $null
    }
    catch [System.Management.Automation.PSArgumentException] {
        $runValue = $null
    }
    $expectedRunValue = '"{0}"' -f $installedExecutable
    if ($null -ne $runValue) {
        if ([string]::Equals(
            [string]$runValue,
            $expectedRunValue,
            [StringComparison]::OrdinalIgnoreCase
        )) {
            Remove-ItemProperty -LiteralPath $runKey -Name 'RayanLamp'
        }
        else {
            Write-Warning 'The RayanLamp startup value points elsewhere and was preserved.'
        }
    }
}

if (Test-Path -LiteralPath $installedExecutable -PathType Leaf) {
    & $installedExecutable /unregister-com /quiet
    if ($LASTEXITCODE -ne 0) {
        Write-Warning (
            'COM Automation registration was not removed. A 32-bit or 64-bit ' +
            'registry view may be owned by another executable and was preserved; ' +
            "RayanLamp exited with code $LASTEXITCODE."
        )
    }
}
elseif (Test-Path -LiteralPath $installPath) {
    Write-Warning (
        'RayanLamp.exe is missing, so ownership-safe 32-bit and 64-bit COM ' +
        'registration cleanup could not run.'
    )
}

if (Test-Path -LiteralPath $installPath) {
    $currentPath = [IO.Path]::GetFullPath((Get-Location).Path)
    $installPrefix = $installPath.TrimEnd('\') + '\'
    if ($currentPath.StartsWith($installPrefix, [StringComparison]::OrdinalIgnoreCase) -or
        [string]::Equals($currentPath, $installPath, [StringComparison]::OrdinalIgnoreCase)) {
        Set-Location ([IO.Path]::GetTempPath())
    }
    Remove-DirectoryWithRetry -LiteralPath $installPath
}

if ($RemoveUserData) {
    if ([string]::IsNullOrWhiteSpace($appData)) {
        throw 'The current user AppData directory is unavailable.'
    }
    $dataPath = [IO.Path]::GetFullPath((Join-Path $appData 'RayanLamp'))
    Assert-ExactPath -Actual $dataPath `
        -Expected (Join-Path $appData 'RayanLamp') `
        -Purpose 'user-data'
    if (Test-Path -LiteralPath $dataPath) {
        Remove-Item -LiteralPath $dataPath -Recurse -Force
    }
}

Write-RayanLampStatus -Level Success `
    -Icon (Get-RayanLampEmojiGlyph -Name Cleanup) -Label 'REMOVED' `
    -Message 'RayanLamp was uninstalled for the current user.'
if (-not $RemoveUserData) {
    Write-RayanLampStatus -Level Info `
        -Icon (Get-RayanLampEmojiGlyph -Name Preserve) -Label 'PRESERVED' `
        -Message "Configuration and logs remain under $appData\RayanLamp."
}
