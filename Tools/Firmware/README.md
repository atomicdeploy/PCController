# PCController AVR firmware studio

`firmware.mjs` is the dependency-free AVR counterpart to ASA0002E's advanced
Node firmware tool. It keeps the polished VT-100/emoji output, stable watch
mode, coalescing, byte-identical skip, strict artifact validation, hashes,
atomic manifests, graceful interruption, and explicit exit codes.

It delegates compile to `Tools/Build/build.mjs` and every programmer action to
the canonical native Controller instead of duplicating MiniCore, Arduino CLI,
or AVRDUDE discovery. Paths are resolved from the script location and `PATH`;
no machine- or user-specific path is embedded and no PowerShell process is
used.

## Safe, offline operations

```console
firmware.cmd build
firmware.cmd check
firmware.cmd manifest
firmware.cmd upload --port COM18 --dry-run
firmware.cmd watch --once --dry-run
```

`build` is the safe default. `check` verifies every Intel HEX checksum, record
shape, address range, application/bootloader boundary, and SHA-256 digest.
`manifest` atomically writes `.build/firmware/firmware-manifest.json`.

## Build-on-edit

```console
# Build only; never opens hardware.
firmware.cmd watch

# Build, then upload each stable content change.
firmware.cmd watch --upload --port COM18
firmware.cmd watch --upload --method urclock --port COM18
```

The watcher hashes source contents every 250 ms and waits for a 500 ms stable
window. It skips timestamp-only touches, serializes actions, and queues one
new action when editing continues during a build. UART upload is never
implicit.

## Urboot operations

Every UART operation requires `--port` or `PCCONTROLLER_PORT`. There is no
fallback to an arbitrary serial device.

```console
firmware.cmd upload --port COM18
firmware.cmd upload --method urclock --port COM18
firmware.cmd upload --method usbasp --programmer usbasp --usbasp-troubleshooting
firmware.cmd probe --port COM18
firmware.cmd metadata --port COM18
firmware.cmd backup --port COM18 --output backups\flash.hex
firmware.cmd verify --port COM18 --hex .build\firmware\PCController.ino.hex
```

Every programming method first delegates only the hardware-free compile to the
shared project-owned Node build utility. The firmware studio then validates all Intel HEX records,
address boundaries, required images, and SHA-256 hashes before it starts any
programmer process:

The build plan also freezes one packed local build timestamp for the delegated
compile. The firmware receives it as `PCCONTROLLER_BUILD_TIMESTAMP` using the
schema-2 `date<<16|time` layout and two-second resolution; all compile entry
points retain `-Wl,--relax` so the MiniCore application has the same verified
flash layout.

- `urclock` hands the validated application image to the native Controller.
- `usbasp` additionally requires `--usbasp-troubleshooting`, the complete
  merged application + Urboot image, and the Controller's EESAVE preflight.
  It never writes the generated `.eep`.

Direct `arduino-cli upload` is intentionally disabled. Controller owns the
automatic pre-flash backup, write/verify, and post-program application
reauthentication. The root `build.cmd --arduino-update` command likewise
routes index/core/library maintenance through `controller arduino update`.

Thus a malformed, oversized, incomplete, or missing image cannot reach a
serial or ISP programmer through this tool.
Backup, verify, probe, and metadata delegate to the native Controller
programmer, which resolves AVRDUDE through `PATH` or Arduino CLI's MiniCore
tool installation.

Run `firmware.cmd --help` or `./firmware.sh --help` for all options.
