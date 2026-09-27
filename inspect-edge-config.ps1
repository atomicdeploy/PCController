$ErrorActionPreference = 'Stop'
$path = Join-Path $env:APPDATA 'PCController\config.json'
$config = Get-Content -Raw -LiteralPath $path | ConvertFrom-Json
[pscustomobject]@{
    path = $path
    exists = Test-Path -LiteralPath $path
    auth_token_present = -not [string]::IsNullOrWhiteSpace([string]$config.ipc.auth_token)
    auth_token_length = ([string]$config.ipc.auth_token).Length
    auth_token_ref = [string]$config.ipc.auth_token_ref
    allow_remote = [bool]$config.ipc.allow_remote
    listen = [string]$config.ipc.listen
    environment_config = [string]$env:PCCONTROLLER_CONFIG
    appdata = [string]$env:APPDATA
} | ConvertTo-Json -Compress
