# Project-owned build and packaging

`build.mjs` is the single policy implementation behind the root `build.cmd`
and `build.sh` launchers. Both wrappers forward their argv unchanged to the
same Node process, so Windows and Bash produce the same command plan. Neither
launcher invokes PowerShell.

The default is hardware-free: it tests, vets, resource-stamps, packages, and
UPX-tests the native host, then asks the current Controller implementation to
compile the AVR firmware. It never selects or opens a serial/USB device.

## Common commands

```console
build.cmd --dry-run
build.cmd --plan-json
build.cmd --host-only
build.cmd --firmware-only
build.cmd --arduino-update
build.cmd --clean
```

On Bash, use the identical options with `./build.sh`.

Generated outputs have one canonical location per product:

- `Tools/Controller/bin/` contains `controller.exe` (or `controller`), the
  optional C ABI library/header, dependency notices, and `host-manifest.json`.
- `.build/firmware/` contains Controller-validated AVR images and
  `firmware-manifest.json`.

After a host package passes validation, the publisher removes known legacy
host output directories and loose `Tools/Controller/controller[.exe]`
artifacts so they cannot shadow or be mistaken for the canonical package.

The host package embeds and then verifies its version, UTC build time, and
SHA-256 source identity. Windows packaging also verifies the PE resource
section before UPX, runs `upx -t`, re-runs the packed executable identity
check, and smoke-tests `PCControllerInvoke`/`PCControllerFree` in the generated
C ABI library. The manifest records exact artifact sizes and SHA-256 hashes.
Only after every validation succeeds does the publisher atomically swap the
staged directory into the canonical `bin` location; a failed swap restores
the previous package.

For a repeatable identity, pass all three values explicitly:

```console
build.cmd --version 0.5.0 --build-time 2026-08-01T16:12:58Z --build-timestamp 35019D5D
```

`PCCONTROLLER_VERSION`, `PCCONTROLLER_HOST_BUILD_TIME`,
`PCCONTROLLER_BUILD_TIMESTAMP`, and `SOURCE_DATE_EPOCH` are also supported.
An explicit packed timestamp takes precedence for the firmware.

## Programming boundary

Programming exists only behind explicit switches and is delegated to the
canonical Controller command. Direct `arduino-cli upload` is disabled.

```console
build.cmd --upload --port COM18
build.cmd --usbasp-flash
build.cmd --burn-bootloader --usbasp-troubleshooting
```

Urclock programming requires a device selector. USBasp is a hidden recovery
path and requires `--usbasp-troubleshooting` (the `--usbasp-flash` alias adds
that authorization). Controller owns pre-flash backup, artifact validation,
write/verify, and application reauthentication. The advanced
`--allow-incomplete-backup` override is never implied.

Use `--dry-run` to inspect the full ordered plan without starting a
subprocess, changing a file, or opening a device. `--plan-json` is intended
for wrapper parity and automation tests.

## Bootstrap requirements

- Node.js 20.19 or newer must already be visible to the thin launcher.
- Go 1.25 or newer is required for the host and for `--firmware-only`, which
  runs the current Controller source directly.
- Firmware compile requires the Controller-supported Arduino CLI/MiniCore
  installation. The build layer does not rediscover or program through it.
- Windows host packaging requires `go-winres` unless
  `--skip-resources` is explicit, and UPX unless `--no-upx` is explicit.
- The C ABI package requires a native target-matching C compiler unless
  `--no-shared-library` is explicit. MSYS-only compiler targets are rejected.

On Windows, the Node tool merges current process PATH with the machine and
user PATH read through `reg.exe`. This picks up newly installed UPX and build
tools without a PowerShell refresh. It cannot bootstrap Node itself because
Node is needed to start the orchestrator.

The root and Controller-local PowerShell build scripts were removed. The
project-owned CMD/Bash workflow is the sole launcher surface and both wrappers
execute this same Node plan.

## Tests

```console
node --check Tools/Build/build.mjs
node --test Tools/Build/build.test.mjs
node --test Tools/Firmware/firmware.test.mjs
build.cmd --dry-run --no-color
bash.exe build.sh --dry-run --no-color
```

The integration suite freezes identity values and asserts that CMD and Bash
emit byte-equivalent JSON plans, compile/program through Controller, preserve
the USBasp authorization gate, and never introduce a PowerShell or direct
Arduino upload action.
