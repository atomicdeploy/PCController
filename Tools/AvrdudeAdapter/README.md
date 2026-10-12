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
