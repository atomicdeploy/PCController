# C Library API

The normal reusable API is the Go package at the module root:

```go
import controller "pccontroller.local/controller"
```

For other languages, `cmd/controllerlib` exposes a deliberately small,
language-neutral JSON ABI:

```c
char *PCControllerInvoke(char *request_json);
void PCControllerFree(char *response_json);
```

Build it from the project root with the canonical host packager (which also
tests the exported ABI):

```console
build.cmd --host-only
```

Go emits both `bin/pccontroller.dll` and `bin/pccontroller.h` on Windows
(`.so` on Linux and `.dylib` on macOS). This requires `CGO_ENABLED=1` and a C
compiler compatible with the target Go toolchain. On Windows the build script
searches `gcc`, `clang`, and `cc` on `PATH`, checks the compiler target against
`go env GOARCH`, and requires a native MinGW-w64/Windows-GNU target. Set `CC`
explicitly when automatic selection is not appropriate; an MSYS-target
compiler cannot link this native Windows shared library.

Every call returns allocated UTF-8 JSON. Copy or parse it, then call
`PCControllerFree` exactly once. The response envelope is:

```json
{"ok":true,"handle":1,"result":{}}
```

or:

```json
{"ok":false,"error":"description"}
```

Requests and representative operations:

```json
{"operation":"create","options":{"vid":"1A86","pid":"7523","name":"USB-SERIAL CH340"}}
{"operation":"connect","handle":1,"timeout_ms":15000}
{"operation":"commands","handle":1}
{"operation":"execute","handle":1,"command":"status"}
{"operation":"status","handle":1}
{"operation":"temperatures","handle":1,"rescan":true}
{"operation":"event_next","handle":1,"after_id":42,"kind":"rf","timeout_ms":30000}
{"operation":"snapshot","handle":1}
{"operation":"rf_list","handle":1}
{"operation":"close","handle":1}
{"operation":"destroy","handle":1}
{"operation":"ports"}
```

| Operation | Behavior |
|---|---|
| `create` | Create a client from the full Go `Options` JSON shape and return a handle. |
| `ports` | Enumerate serial ports; no handle is required. |
| `connect` | Auto-detect, or explicitly open top-level `port` when provided; returns the snapshot. |
| `commands` | Return the shared command catalog with names, aliases, usage, summary, and task group. |
| `execute` | Run any registered shell command and return `{"output":"..."}`. This includes status/settings, display/strip, macros, streamed melodies/status effects, relay/PWM/RF, reset, raw query, and programming commands. |
| `status` | Request and decode current telemetry, including reset fields when supplied. |
| `temperatures` | Return named DS18B20 role/ROM/value records; `rescan:true` asks the board to rescan first. |
| `event_next` | Wait for the first retained/new event with ID greater than `after_id`; optional `kind` filters it. |
| `snapshot` | Return the latest connection, identity, status, and settings state without a UART request. |
| `rf_list` | Fetch every page of learned RF entries. |
| `close` | Close and pause automatic reconnect while keeping the handle valid. |
| `destroy` | Shut down the client and remove the handle. |

`timeout_ms` defaults to 15000 for handle operations. For `event_next`, use a
larger timeout when an idle wait is expected. Event IDs are monotonic within
the client; pass the returned event's ID as the next `after_id` to avoid
receiving it again. `kind` uses the host event names such as `key`, `door`,
`rf`, `macro`, `connect`, or `disconnect`.

Handles are local to the process that loaded the library. Operations on one
handle are serialized, so callers from multiple native threads cannot
interleave native UART requests.

A C-ABI handle is itself a serial owner; it does not silently attach to the
loopback IPC primary. If the TUI/shell/IPC service already owns the board, a
native application should call the documented JSON-RPC endpoint instead of
creating a competing DLL handle. Conversely, an application can make its DLL
handle the sole owner and expose its own IPC boundary. CLI `exec`, batch,
monitor, reset, shell, and programming processes automatically route through
the standard primary process.

Background `melody play` and `rgb effect play` operations remain active while
their handle and the hosting process remain alive. They can be canceled through
another `execute` request (`melody stop` or `rgb effect stop`). Definitions are
PC-side JSON configuration only; they are not mixed with the MCU's
EEPROM-owned settings.

The complete per-domain reachability contract is in the
[Control-Surface Capability Matrix](Control-Surface-Capability-Matrix.md).

`examples/c_abi_smoke.c` is a minimal Windows consumer that resolves both
exports, invokes the port-list operation, and releases the returned string.
Build it with any compatible MinGW-w64 compiler. Treat the Go shared runtime
as process-lifetime state: keep the DLL loaded and let normal process shutdown
unload it instead of calling `FreeLibrary` while Go runtime threads are active.
