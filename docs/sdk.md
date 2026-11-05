# Native and Go SDK

RayanLamp has an optional external SDK for applications that prefer an
in-process client library over launching a command-line child process. It is a
controller for the authoritative primary, not another lamp runtime: every call
uses the same current-user named pipe and canonical command dispatcher as the
GUI, CLI, TUI, Web UI, URI handler, and COM Automation server.

The ordinary portable release remains exactly one `RayanLamp.exe`, built with
`CGO_ENABLED=0`. SDK artifacts are created only by an explicit SDK build and
are written under `dist-sdk`, never `dist`.

## Windows build

Build one architecture:

```powershell
.\tools\build-sdk.ps1 -Architecture amd64
.\tools\build-sdk.ps1 -Architecture 386
```

Build both:

```powershell
.\tools\build-sdk.ps1 -Architecture all -Force
```

The script validates the MinGW target tuple and rejects Git/MSYS `/usr/bin`
GCC, which would introduce the MSYS runtime. On this machine it can use Git's
standalone `mingw64` and `mingw32` toolchains. `-CCPath` selects an explicit
matching compiler for a single target without changing `go env` or the user's
global `PATH`.

Each architecture directory contains:

```text
dist-sdk/<architecture>/RayanLamp.dll
dist-sdk/<architecture>/RayanLamp.h
dist-sdk/<architecture>/libRayanLamp.dll.a
```

The GNU import library is optional; any Windows language can use
`LoadLibrary`/`GetProcAddress`. The DLL has Windows version information and
imports only allow-listed Windows system libraries. The build verifies the PE
machine, DLL characteristic, exact public export set, version resource, and
dependency allow-list, then loads and exercises the ABI through a matching
native host. The 32-bit smoke test runs on 64-bit Windows through WOW64.

## Cross-build from Linux

Install the matching MinGW-w64 GCC and binutils packages, then run:

```bash
make windows-sdk-cross ARCH=amd64 FORCE=1
make windows-sdk-cross ARCH=386 FORCE=1
bash ./tools/build-sdk-linux.sh --architecture all --force
```

The Linux path performs the same target-tuple validation, c-archive/native
link, exact PE/export/resource/dependency inspection, and native harness
compilation. It cannot execute the resulting Windows harness without Wine, so
runtime loading remains a Windows CI check. ARM64 SDK output intentionally
fails with clear guidance until a reproducible LLVM-MinGW toolchain is pinned;
this does not affect the existing CGO-free ARM64 executable cross-build.

## Attach-only behavior

`RayanLamp_ExecuteUtf8`, `RayanLamp_ExecuteW`, `RayanLamp_On`,
`RayanLamp_Off`, `RayanLamp_Toggle`, and `RayanLamp_GetState` never launch a
process and never connect directly to the lamp. If no primary owns the
current-user pipe they return `RAYANLAMP_STATUS_UNAVAILABLE`, normally with a
structured JSON error envelope.

Bootstrap is a separate, explicit API:

```c
int32_t status = RayanLamp_EnsurePrimaryW(
    executable_path,
    wcslen(executable_path),
    12000
);
```

It starts only the exact supplied `RayanLamp.exe` path with `/background` and
waits for named-pipe readiness. It does not search `PATH`, infer an executable,
or turn Excel, AutoHotkey, or another DLL host into an invisible embedded
primary. Portable mode performs no automatic SDK, COM, URI, or registry
registration.

## C ABI

Include `RayanLamp.h`. The public exports use `WINAPI`, fixed-width integer
statuses, and explicit string lengths:

- `RayanLamp_ABIVersion`
- `RayanLamp_IsRunning`
- `RayanLamp_EnsurePrimaryW`
- `RayanLamp_ExecuteUtf8`
- `RayanLamp_ExecuteW`
- `RayanLamp_On`
- `RayanLamp_Off`
- `RayanLamp_Toggle`
- `RayanLamp_GetState`
- `RayanLamp_Free`

ABI version `0x00010000` means ABI 1.0. It is deliberately separate from the
living, versionless JSON command contract. A zero status means that transport
completed; callers must still inspect `{"ok":false,"error":...}` because the
dispatcher may reject a valid request. Negative statuses cover invalid ABI
input, unavailable primary, timeout, unsupported platform, and internal
failure. `GetLastError` is set to a corresponding Win32 code after native ABI
calls.

Returned strings are NUL terminated, while buffer lengths exclude the
terminator. Free every non-null returned pointer with `RayanLamp_Free`,
including error responses. No Go pointer crosses the ABI. Inputs and responses
are bounded to 1 MiB, method names to 64 UTF-8 bytes, and timeouts to
100–600000 ms; zero selects 12 seconds.

The first API call pins the Go-backed DLL for process lifetime. A host may
balance its own `LoadLibrary` with `FreeLibrary`, but must not force-unmap the
module or assume that the Go runtime can be reloaded in the same process.

Example:

```c
RAYANLAMP_BUFFER response = {0};
int32_t status = RayanLamp_ExecuteUtf8(
    "lamp.toggle",
    sizeof("lamp.toggle") - 1,
    NULL,
    0,
    12000,
    &response
);
if (response.data != NULL) {
    /* Parse response.data as UTF-8 JSON, even when status is negative. */
    RayanLamp_Free(response.data);
}
```

AutoHotkey v2 can use the UTF-16 entry directly:

```autohotkey
dll := A_ScriptDir "\RayanLamp.dll"
method := "lamp.toggle"
out := Buffer(A_PtrSize * 2, 0)
status := DllCall(
    dll "\RayanLamp_ExecuteW",
    "WStr", method,
    "UPtr", StrLen(method),
    "Ptr", 0,
    "UPtr", 0,
    "UInt", 12000,
    "Ptr", out,
    "Int"
)
pointer := NumGet(out, 0, "Ptr")
length := NumGet(out, A_PtrSize, "UPtr")
json := pointer ? StrGet(pointer, length, "UTF-16") : ""
if pointer
    DllCall(dll "\RayanLamp_Free", "Ptr", pointer)
```

For VBA, the opt-in out-of-process COM object remains the friendlier and safer
Automation interface; it avoids architecture-matched DLL declarations and
manual buffer ownership.

## Public Go client

Go hosts can import `rayanlamp.local/app/sdk` from the source module:

```go
client, err := sdk.New(sdk.Options{Timeout: 5 * time.Second})
if err != nil {
    return err
}
response, err := client.Toggle(context.Background())
if err != nil {
    return err // transport failure
}
if !response.OK {
    return response.DecodeResult(nil) // dispatcher failure
}
```

`Client` is concurrent-safe, performs no I/O during construction, and exposes
`Call` for every present or future dispatcher method without requiring a new
DLL export.
