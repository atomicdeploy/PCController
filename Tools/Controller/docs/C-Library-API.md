<div align="center"><a href="../../../README.md"><img src="../../../docs/assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# C Library API

The normal reusable API is the Go package at the module root:

```go
import controller "pccontroller.local/controller"
```

For other languages, `cmd/controller-cabi` exposes a deliberately small,
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

For a developer-only CMake verification build that produces only the library,
generated header, and external C consumer smoke tests, configure the Go module
itself:

```console
cmake -S Tools/Controller -B .build/controller-cabi -G Ninja ^
  -DPCCONTROLLER_CABI_CC=C:\path\to\x86_64-w64-mingw32-gcc.exe
cmake --build .build/controller-cabi --target pccontroller_cabi_installed_smoke
ctest --test-dir .build/controller-cabi --output-on-failure
cmake --install .build/controller-cabi --prefix .build/controller-cabi/install
```

The CMake target delegates directly to `go build -buildmode=c-shared`; it does
not replace the canonical package/provisioning tool. On Windows it verifies
that the same compiler selected for CMake and CGO matches `go env GOARCH`,
advertises native MinGW-w64 macros, and is neither MSYS nor Cygwin. Point
`PCCONTROLLER_CABI_CC` at a native UCRT/MinGW-w64 compiler on a fresh build
directory. The regular host packager remains the preferred path when automatic
compiler discovery or provisioning is wanted. CMake output is deliberately
**not a deployable host package**: it omits the canonical manifest, embedded
defaults, product resources, package verification, and deployment lifecycle.

On Linux, the same developer-only CMake/CTest smoke uses the native system C
compiler by default:

```console
cmake -S Tools/Controller -B .build/controller-cabi -G Ninja -DBUILD_TESTING=ON
cmake --build .build/controller-cabi --target pccontroller_cabi_installed_smoke
ctest --test-dir .build/controller-cabi --output-on-failure
```

Go emits both `bin/pccontroller.dll` and `bin/pccontroller.h` on Windows
(`.so` on Linux and `.dylib` on macOS). This requires `CGO_ENABLED=1` and a C
compiler compatible with the target Go toolchain. On Windows the build script
checks candidates from `PATH` and the user-scoped package store against
`go env GOARCH`, their target triple, and their predefined macros. It rejects
Cygwin/MSYS compilers even when they are the first `gcc.exe` on `PATH`, selects
the newest native MinGW-w64/Windows-GNU candidate, and provisions the exact
latest-resolved package from the host-tool lock when none is installed. Set `CC` explicitly to a valid
native compiler when automatic selection is not appropriate, or pass
`--no-compiler-bootstrap` to prohibit installation. Standard HTTP proxy
environment variables are preserved for the package manager.

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

### AutoHotkey and VBA

The same stable C exports are directly usable by desktop automation tools; no
shell process, temporary file, or command-line quoting is required. An
AutoHotkey v2 call is:

```ahk
dll := A_ScriptDir "\pccontroller.dll"
json := '{"operation":"ports"}'
request := Buffer(StrPut(json, "UTF-8"))
StrPut(json, request, "UTF-8")
resultPtr := DllCall(dll "\PCControllerInvoke", "Ptr", request.Ptr, "CDecl Ptr")
try response := StrGet(resultPtr, "UTF-8")
finally DllCall(dll "\PCControllerFree", "Ptr", resultPtr, "CDecl")
```

Office VBA can declare the same pointer ABI with `PtrSafe`/`LongPtr`, pass a
null-terminated UTF-8 byte buffer to `PCControllerInvoke`, copy the returned
UTF-8 JSON, and release it with `PCControllerFree`. Match the Office and DLL
architectures. This explicit process-lifetime ABI is the supported automation
surface; the portable package does not silently register a machine-wide COM
class or require administrator access.
