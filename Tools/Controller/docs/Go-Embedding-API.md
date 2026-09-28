<div align="center"><a href="../../../README.md"><img src="../../../docs/assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Embeddable host lifecycle

`pccontroller.local/controller/host` is the supported owner for running a
complete PCController host inside another Go process. It constructs the same
`controller.Client` and canonical JSON-RPC dispatcher used by the standalone
executable. It does not create another command implementation or let the
embedding application become an independent UART owner.

This package embeds the **PC host**, not a second firmware implementation. The
one production firmware compiled for AVR and supported OS-native targets is
tracked by [#103](https://github.com/atomicdeploy/PCController/issues/103);
its standalone, native-local, stream, and loadable adapters are tracked by
[#227](https://github.com/atomicdeploy/PCController/issues/227). An OS-native
firmware instance remains a board peer reached through the same negotiated
board contract as AVR, never through a VirtualBoard-only semantic shortcut.

PCController is alpha and uses one living, unversioned contract. Additive
optional fields and stable feature/capability identifiers provide evolution.
Unknown critical safety, identity, authorization, routing, replay, deadline,
or integrity semantics reject the affected operation directly.

## Lifecycle contract

- `host.New` validates immutable construction options without opening a board
  or listener.
- `Host.Start` is single-use and returns after configured listeners are ready.
  Board discovery continues in the background.
- One in-process RPC client is always available from `Host.RPC`.
- Protected native-local IPC is enabled by default: a current-user named pipe
  on Windows or an owner-only Unix-domain socket on Unix platforms.
- `host.HTTPOptions` independently enables the HTTP/WebSocket and raw JSON-RPC
  control plane. `127.0.0.1:8787` is the default address when HTTP is enabled.
- Native-local IPC and HTTP/WebSocket may run at the same time. They share the
  same service value, method dispatcher, board owner, event sequence, and
  capability state.
- `Host.Controller` exposes the canonical typed Go client for direct status,
  macro, effect, telemetry, and command calls. The embedder must not shut that
  client down directly.
- `Host.Events` preserves the controller's ordered event stream. Callbacks are
  not run under the Host lifecycle lock.
- `Host.Errors` reports bounded background failures without writing to global
  process output. Supplying a `Logger` opts into lifecycle diagnostics.
- Cancellation of the parent context initiates shutdown. `Host.Stop` is
  idempotent, closes listeners, drains endpoint workers, releases integrations
  and serial ownership, and closes `Host.Done`.
- A stopped Host is not restarted. Construct a new Host to begin another
  lifecycle.

The package never calls `os.Exit`, installs signal handlers, changes the
process working directory, or owns application shutdown policy.

## Go consumer

```go
embedded, err := host.New(host.Options{
    DataRoot: appDataDirectory,
    Branding: host.Branding{
        AppID:   "com.example.player",
        AppName: "Example Player",
    },
    HTTP: &host.HTTPOptions{Address: "127.0.0.1:8787"},
})
if err != nil {
    return err
}
if err := embedded.Start(appContext); err != nil {
    return err
}
defer func() {
    stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    _ = embedded.Stop(stopContext)
}()

var ping struct {
    OK bool `json:"ok"`
}
if err := embedded.Call(appContext, "controller.ping", map[string]any{}, &ping); err != nil {
    return err
}
```

[`examples/embedded-host`](../examples/embedded-host) is a buildable consumer
that adds signal handling and endpoint reporting. Its repository-specific
environment bootstrap is entry-point wiring; all PCController lifecycle and
control calls use the public `host` package.

`DataRoot` and `ConfigPath` are mutually exclusive. `DataRoot` places the
living `config.json`, measurement history, and timeline under an
application-owned directory. `Branding` applies presentation and native
endpoint identity for the process lifetime without rewriting persistent
configuration. `BuildInfo` is diagnostic only and never selects behavior.

Set `ControllerOptions` when the embedding application already has a complete
typed configuration. Otherwise the Host maps the watched PCController
configuration into the canonical typed client and hot-applies valid changes.

## Pealayer and other non-Go consumers

The packaged `pccontroller.dll`, `pccontroller.so`, or
`pccontroller.dylib` retains the two-function JSON ABI documented in
[C-Library-API.md](C-Library-API.md). The same ABI now owns `host.Host` handles;
there is no separate foreign-function dispatcher.

Pealayer integration sequence:

1. Bundle the platform library and generated `pccontroller.h` from the same
   verified PCController package. Keep the library loaded until process exit.
2. Call `PCControllerInvoke` with `host_create`. Supply Pealayer's private data
   root, stable application ID, display branding, and optional HTTP address.
3. Call `host_start`. Treat an ownership/listener error as evidence that an
   external coordinator may already be active; do not open UART as an implicit
   fallback.
4. For an embedded Host, call `host_call` for the same living RPC methods used
   by native-local and network clients. Use `controller.capabilities` and live
   snapshots before presenting hardware, effects, relays, or telemetry.
5. `host_endpoints` returns only listeners that actually started. Pealayer may
   display these for diagnostics or publish them to other authorized clients.
6. On application shutdown call `host_stop`, wait for success, then call
   `host_destroy`. `host_destroy` also stops a still-running Host.
7. If embedding is unavailable or ownership belongs to another process,
   connect to the advertised native-local endpoint first and use the ordinary
   `:8787` control plane only as the configured fallback. Transport selection
   never changes RPC method semantics.

Example create request:

```json
{
  "operation": "host_create",
  "host_options": {
    "data_root": "APPLICATION_DATA/PCController",
    "app_id": "pealayer",
    "app_name": "Pealayer",
    "disable_auto_connect": false,
    "disable_native": false,
    "enable_integrations": true,
    "http_address": "127.0.0.1:8787"
  }
}
```

Start and call requests:

```json
{"operation":"host_start","handle":1}
{"operation":"host_call","handle":1,"method":"controller.ping","params":{}}
{"operation":"host_endpoints","handle":1}
{"operation":"host_destroy","handle":1,"timeout_ms":10000}
```

Every returned UTF-8 response must be released exactly once with
`PCControllerFree`. Calls for one Host handle are serialized by the C adapter;
the canonical controller event IDs define ordering across calls and transports.

## Verification

On Windows, run Go tests through the stable project-owned runner:

```console
node Tools/Build/go-tests.mjs --package host
build.cmd --host-only
```

On Linux:

```console
node Tools/Build/go-tests.mjs --package host
go test -race ./host
./build.sh --host-only
```

The host package tests cover start/stop idempotence, duplicate ownership,
parent cancellation, in-process calls, and simultaneous native-local plus TCP
RPC. The canonical C-shared smoke creates a Host, starts it, performs an
in-process ping, destroys it, and verifies the generated ABI artifacts.
