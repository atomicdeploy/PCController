# PCController libavrdude adapter

Normal host packages statically link the pinned `libavrdude` and this C bridge
into PCController through CGO. The default package needs neither
`pccontroller-avrdude` nor `avrdude`, and does not load a libavrdude DLL.
Progress and messages feed the existing guarded coordinator and `update.*`
stream directly. The optional C ABI host library uses the same backend.

`--avrdude-mode helper` retains the process-isolated JSON Lines adapter as an
alternative. A library crash or fatal upstream exit can terminate a direct
CGO host; helper mode contains it in the child process. Both use the same C
implementation and checksum-locked upstream source (no stable ABI is assumed).

Urclock `showall` and `showversion` are library extended parameters. Their
`LIBAVRDUDE_EXIT_OK` result is a successful query, not a failed initialization.
Fuse evidence, EESAVE preflight and slow-clock recovery also use the bridge;
none require the compatibility CLI. Arduino core bootloader installation is
still delegated to the selected Arduino CLI and its core-supplied tools.

## Build and runtime selection

```console
node Tools/Build/build.mjs --host-only --avrdude-mode native
node Tools/Build/build.mjs --host-only --avrdude-mode helper
node Tools/Build/build.mjs --host-only --avrdude-mode cli
```

`native` is the default. CLI-only builds also accept `--no-avrdude-library`.
Native Go builds require `CGO_ENABLED=1`, `-tags libavrdude`, and CMake's resolved
static archive/dependency flags. The packager handles these automatically and
records developer build variables in `.build/avrdude-adapter-host/cgo-env.json`.
Do not substitute a system DLL or an archive built from unrelated headers.

Runtime `--avrdude-backend auto` prefers native, then helper, then CLI in builds
without native support. An explicit helper or CLI path takes precedence in auto
mode. `native` requires CGO; `helper` requires the isolated executable; `library`
accepts native or helper; `cli` explicitly selects an external AVRDUDE.

All native calls are serialized process-wide, including callbacks and cleanup.
Cancellation waits for an active transport call to return, then stops at the
next transfer/stage boundary. It never kills a C thread, longjmps across library
frames, or returns programming ownership while C still owns the board. Device
I/O timeouts can therefore delay cancellation. Select helper mode when a hard
process termination boundary is required. Cancellation is not successful
verification and can leave partially written flash; existing backup/readback
and explicit recovery requirements remain in force.

Build without opening a programmer:

```sh
cmake -S Tools/AvrdudeAdapter -B .build/avrdude-adapter -G Ninja
cmake --build .build/avrdude-adapter --target pccontroller-avrdude
```

Normal builds never program a board. Runtime programming remains behind the
Controller's explicit backup, authorization, readback, and bridge ownership
checks.

## Distribution layout

The host archive keeps `controller.exe` at its root. Programming tools live in
`tools/avrdude/`: `avrdude.conf` and `COPYING.txt`. Helper-mode packages additionally
include `pccontroller-avrdude.exe` and the optional compatibility `avrdude.exe`
(executables have no suffix on Unix). The host discovers this
directory automatically; explicit configured tool paths retain precedence.
The adapter is an internal JSON Lines helper, not a second controller app.

Windows builds statically link the MinGW C++/GCC support libraries used by USB
dependencies. Earlier builds linked `libstdc++-6.dll` dynamically but did not
package it; build-machine PATH entries concealed the missing runtime. Packaging
now smoke-checks both staged executables with only Windows System32 on PATH.
Do not download arbitrary DLLs to repair an older package.

Default notification PNGs are embedded in the controller executable. Windows
toast file URIs use a content-addressed per-user cache extracted from those
bytes. Optional local overrides belong in `assets/toast/toast-logo.png` or
`assets/toast/toast-event-<name>.png` beside the controller, where names are
`info`, `warning`, `error`, `door`, `hot`, `temperature-cleared`, `motion`,
`relay`, and `radio`. A custom `--toast-icon` is packaged at that override path;
ordinary packages need no loose PNG files. Invalid overrides retain the normal
notification validation and fallback behavior.
