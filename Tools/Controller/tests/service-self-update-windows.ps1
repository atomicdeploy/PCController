# Real SCM acceptance on a disposable elevated CI runner only. Never adopt,
# stop or repair an existing PCController service on a developer/user machine.
param([Parameter(Mandatory=$true)][string]$Executable)
$ErrorActionPreference = 'Stop'
if (-not $IsWindows -or $env:GITHUB_ACTIONS -ne 'true') { throw 'Requires a disposable Windows GitHub Actions runner.' }
if (Get-Service -Name PCController -ErrorAction SilentlyContinue) { throw 'Existing PCController service must remain untouched.' }
$root = Join-Path $env:RUNNER_TEMP ('pccontroller-service-update-' + [Guid]::NewGuid().ToString('N'))
$bin = Join-Path $root 'bin'
$candidateDir = Join-Path $root 'candidate'
New-Item -ItemType Directory -Path $bin,$candidateDir | Out-Null
$installed = Join-Path $bin 'controller.exe'
$candidate = Join-Path $candidateDir 'controller.exe'
Copy-Item -LiteralPath (Resolve-Path -LiteralPath $Executable).Path -Destination $installed
Copy-Item -LiteralPath $installed -Destination $candidate
# A different valid PE with identical behavior, to exercise actual replacement.
$stream = [IO.File]::Open($candidate, [IO.FileMode]::Append)
try { $bytes = [Text.Encoding]::UTF8.GetBytes('SCM mutual-update acceptance'); $stream.Write($bytes,0,$bytes.Length) } finally { $stream.Dispose() }
$digest = (Get-FileHash -LiteralPath $candidate -Algorithm SHA256).Hash.ToLowerInvariant()
$size = (Get-Item -LiteralPath $candidate).Length
$config = Join-Path $root 'config.json'
'{}' | Set-Content -LiteralPath $config -Encoding utf8
$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,0)
$listener.Start(); $port = $listener.LocalEndpoint.Port; $listener.Stop()
$origin = "http://127.0.0.1:$port"
$owned = $false
$nextId = 0
function Rpc([string]$Method, $Params) {
    $script:nextId++
    $body = @{jsonrpc='2.0';id=$script:nextId;method=$Method;params=$Params} | ConvertTo-Json -Depth 20 -Compress
    $response = Invoke-RestMethod "$origin/api/rpc" -Method Post -ContentType 'application/json' -Body $body -TimeoutSec 20
    if ($response.id -ne $script:nextId -or $response.error) { throw "RPC $Method failed: $($response | ConvertTo-Json -Depth 10 -Compress)" }
    return $response.result
}
try {
    & $installed "--config=$config" service install "--binary=$installed" "--data-dir=$(Join-Path $root 'data')" "--listen=127.0.0.1:$port" --no-auto
    if ($LASTEXITCODE -ne 0) { throw "Service installation failed: $LASTEXITCODE" }
    $owned = $true
    $before = Get-CimInstance Win32_Service -Filter "Name='PCController'"
    $manifest = Rpc 'controller.artifact.manifest' @{}
    if ($manifest.board.connected) { throw 'Isolated service unexpectedly connected to hardware.' }
    $upload = Rpc 'controller.artifact.upload.begin' @{kind='host-executable';name='controller.exe';sha256=$digest;bytes=$size;platform='windows/amd64'}
    $input = [IO.File]::OpenRead($candidate)
    try {
        $offset = [long]0
        $buffer = [byte[]]::new(384*1024)
        while (($count = $input.Read($buffer,0,$buffer.Length)) -gt 0) {
            $ack = Rpc 'controller.artifact.upload.chunk' @{transfer_id=$upload.transfer_id;offset=$offset;data=[Convert]::ToBase64String($buffer,0,$count)}
            $offset += $count
            if ($ack.transfer_id -ne $upload.transfer_id -or $ack.next_offset -ne $offset -or $ack.bytes_total -ne $size) { throw 'Upload ACK mismatch.' }
        }
    } finally { $input.Dispose() }
    $finished = Rpc 'controller.artifact.upload.finish' @{transfer_id=$upload.transfer_id}
    if ($finished.artifact.sha256 -ne $digest) { throw 'Verified upload digest mismatch.' }
    $operation = (Rpc 'controller.update.host' @{artifact_sha256=$digest;authorized=$true;idempotency_key=('scm-test-'+[Guid]::NewGuid().ToString('N'))}).operation
    $deadline = [DateTime]::UtcNow.AddSeconds(120)
    $verified = $false
    while ([DateTime]::UtcNow -lt $deadline) {
        try {
            $status = Rpc 'controller.update.status' @{id=$operation.id}
            $manifest = Rpc 'controller.artifact.manifest' @{}
            if ($status.state -eq 'failed') { throw "Update failed: $($status | ConvertTo-Json -Compress)" }
            if ($status.id -eq $operation.id -and $status.state -eq 'completed' -and $status.artifact_sha256 -eq $digest -and $manifest.current.host.sha256 -eq $digest) { $verified=$true;break }
        } catch { Write-Host "Waiting for SCM replacement: $($_.Exception.Message)" }
        Start-Sleep -Milliseconds 500
    }
    if (-not $verified) { throw 'SCM replacement did not reach verified terminal completion.' }
    $after = Get-CimInstance Win32_Service -Filter "Name='PCController'"
    if ($after.ProcessId -eq $before.ProcessId -or $after.State -ne 'Running' -or $after.PathName -ne $before.PathName -or $after.StartName -ne $before.StartName -or $after.StartMode -ne $before.StartMode) { throw 'SCM identity/configuration mismatch after update.' }
    if ($manifest.board.connected -or (Get-FileHash -LiteralPath $installed -Algorithm SHA256).Hash.ToLowerInvariant() -ne $digest) { throw 'Replacement/hardware isolation mismatch.' }
    Write-Host "Verified real SCM self-update: healthy process replacement, exact digest, durable completed operation and unchanged service configuration."
} finally {
    if ($owned) {
        $current = Get-CimInstance Win32_Service -Filter "Name='PCController'"
        if ($current.PathName -notlike ('*'+$installed+'*')) { throw 'Service owner changed; refusing cleanup.' }
        & $installed "--config=$config" service stop
        & $installed "--config=$config" service remove
        if ($LASTEXITCODE -ne 0) { throw 'Could not remove the isolated CI service.' }
    }
}
