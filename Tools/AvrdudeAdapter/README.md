# PCController libavrdude adapter

This target links a small process-isolated adapter directly to the pinned
`libavrdude` source. The adapter emits JSON Lines for library messages,
progress callbacks, errors, and completion. The Go host consumes those events
through its existing guarded coordinator and `update.*` progress stream.

The process boundary is deliberate: libavrdude has mutable process-global
configuration/callback state and does not promise a stable ABI. The adapter
and static library are compiled together from the checksum-locked source, so
the controller never loads an unrelated system DLL.

The same build also packages the matching upstream CLI for the two Urclock
extension queries that do not have a libavrdude operation API. Auto mode uses
that CLI only for metadata/version queries; programmer probe, erase, read,
write, verify, and their progress all remain on the library adapter.

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
`tools/avrdude/`: `pccontroller-avrdude.exe`, `avrdude.exe`, `avrdude.conf`, and
`COPYING.txt` (executables have no suffix on Unix). The host discovers this
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
