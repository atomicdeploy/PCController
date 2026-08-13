<div align="center"><a href="../../README.md"><img src="../../docs/assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Project-owned build and packaging

`Tools/CommandPlan/controller-command.mjs` is the shared board, artifact, and
Controller-command policy behind both `build.mjs` and the firmware studio.
The root `build.cmd`/`build.sh` and `firmware.cmd`/`firmware.sh` launchers only
bootstrap Node and forward argv unchanged, so Windows and Bash produce the
same plans, failures, canonical artifact routes, and explicit programming
method selection. None of these launchers invokes PowerShell.

The canonical `Tools/Controller/toolchain-profile.json` owns the FQBN, MCU,
clock, programmer baud, bootloader description, and flash/EEPROM geometry.
Its generator embeds the same policy and compile-time capacities in the Go
Controller. Build dry-runs and real execution both call the same command
builder; neither reconstructs USBasp or Urclock argv independently.

Interactive output uses Chalk-managed VT-100 color, clear emoji-labelled
stages, elapsed timing, and `cli-table3` Unicode tables with centered headers
and width-aware alignment. Compact utilization gauges and visible hashes come
from validated host and firmware manifests, so the summary is an auditable
account of the exact artifacts that were built. No table border, column
padding, or ANSI sequence is assembled by an ad-hoc renderer.
Define `NO_COLOR` (even with an empty value) or pass `--no-color` for plain
logs and automation. Define `FORCE_COLOR` to retain VT-100 styling in a
non-interactive CI stream; `NO_COLOR` always wins when both are present.
The visible product label comes from
`Tools/Controller/web/package.json` (`productName`) rather than a build-script
literal. `APP_TITLE` may override it for one build invocation.

The default is hardware-free: the current Controller source first compiles and
validates AVR firmware, the packager stages that exact application image plus
the same validated full default EEPROM image, then it installs the exact web dependency
lock with `npm ci`, type-checks/tests/builds the embedded web application, and
tests, vets, resource-stamps, packages, and UPX-tests the native host. It never
selects or opens a serial/USB device.

Before the WebUI input identity is frozen, the build regenerates the canonical
256 px product mark and its seven-size ICO from the same project-owned source.
The native Windows resource and embedded browser therefore cannot silently
drift to different icons. The packaged host also applies the named `APP`
resource explicitly to attached classic conhost windows, so a parent build
shell or Windows icon cache cannot leave the running app with the generic
console icon.

## Common commands

```console
build.cmd --dry-run
build.cmd --plan-json
build.cmd --host-only
build.cmd --firmware-only
build.cmd --toolchain-sync
build.cmd --firmware-only --toolchain-cli C:\path\to\arduino-cli.exe --toolchain-config C:\path\to\firmware-cli.yaml
build.cmd --clean
```

On Bash, use the identical options with `./build.sh`.

On Windows, `build.cmd --host-only` delegates Go tests to
`Tools/Build/go-tests.mjs`. Do not run `go test` directly: temporary Go test
paths can appear as a new application to Windows Firewall on every run. The
project runner uses deterministic `pccontroller-tests-*.exe` names beneath
`%LOCALAPPDATA%\PCController\test-programs\go` across every worktree, caches a
source/toolchain/environment identity pass, serializes concurrent worktrees
with a machine-wide lock, and keeps live LAN acceptance
on the stable packaged `controller.exe` identity. Windows Go tests never opt
into wildcard LAN broadcast unless `PCCONTROLLER_TEST_LAN=1` is explicitly set;
normal builds leave that acceptance test to the packaged host.

Generated outputs have one canonical location per product:

- `Tools/Controller/bin/` contains `controller.exe` (or `controller`), the
  optional C ABI library/header, dependency notices, and `host-manifest.json`.
- Windows output also contains `installation-package.json`, whose canonical
  root SHA-256 binds every installable file and the validated host/resource
  identity used by install, repair, and uninstall.
- `.build/firmware/` contains Controller-validated AVR images and
  `firmware-manifest.json`.

When a validated application/default-EEPROM pair exists, the host executable
embeds both exact Intel HEX files from the firmware manifest. The EEPROM gate
requires all 1,024 addressable bytes, and `host-manifest.json` records separate
enabled flags, SHA-256 values, container sizes, and EEPROM data bytes. A
host-only build reuses an existing validated firmware manifest when present;
otherwise the embedded recovery feature is disabled. It never substitutes the
compiler's empty `.eep` file and never grants an automatic device write.

Reusable CI cleans generated host output before downloading the same-run
firmware artifact into `.build/firmware`. It then builds without a second
cleanup and independently asserts that both embedded-default flags are true.

`--firmware-only --clean` intentionally removes only `.build/firmware`; it
does not touch a running canonical host in `Tools/Controller/bin`. A bare
`--clean` remains the explicit full generated-output cleanup.

After a host package passes validation, the publisher removes known stale
host output directories and loose `Tools/Controller/controller[.exe]`
artifacts so they cannot shadow or be mistaken for the canonical package.

The host package embeds and then verifies its version, UTC build time, and
SHA-256 source identity. That identity covers Go sources, the web package and
lockfile, web source/public assets, and the exact Vite `dist` bytes embedded by
Go. The host manifest records the lock-based install, web type-check/test
status, exact input/`dist` SHA-256 values, and copied license files for bundled
runtime npm packages (build-only dependencies are excluded). Windows packaging
also verifies
the PE resource section before UPX, runs `upx -t`, re-runs the packed executable
identity check, and smoke-tests `PCControllerInvoke`/`PCControllerFree` in the
generated C ABI library. The manifest records exact artifact sizes and SHA-256
hashes.
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
build.cmd --upload --method usbasp --port COM18
build.cmd --install-bootloader --method usbasp
```

Urclock programming requires a device selector. USBasp is an advanced recovery
path selected explicitly through `--method usbasp`; `--programmer` is only an
optional backend-ID override for different ISP hardware. Controller owns
pre-flash backup, artifact validation, write/verify, and application
reauthentication. On standalone USBasp writes, `--port` supplies the separate
application lifecycle selector and is never sent to ISP. The advanced
`--allow-incomplete-backup` override is never implied.

Use `--dry-run` to inspect the full ordered plan without starting a
subprocess, changing a file, or opening a device. `--plan-json` is intended
for wrapper parity and automation tests. The firmware studio exposes the same
machine-readable boundary, for example:

```console
firmware.cmd upload --method usbasp --plan-json
```

Plan JSON includes the canonical target/FQBN, Controller path, application,
complete-flash, default-EEPROM, and manifest paths. Emitting a plan never
requires a device to be opened or a Controller subprocess to be started.

## Bootstrap requirements

- Node.js 22.12 or newer must already be visible to the thin launcher. The
  launcher bootstraps Chalk and `cli-table3` from the reviewed
  `Tools/Build/package-lock.json` when needed; host builds independently use
  the embedded web application's exact lock with `npm ci`.
- Go 1.26 or newer is required for the host and for `--firmware-only`, which
  runs the current Controller source directly.
- Firmware compile requires the Controller-supported dependency CLI/MiniCore
  installation. Run `controller toolchain bootstrap` on a clean machine to
  resolve the latest compatible profile into an exact, hash-verified local
  lock without replacing unrelated global tools.
- A build can select that managed CLI and its adjacent profile explicitly with
  `--toolchain-cli` and `--toolchain-config`. The equivalent build-only
  environment variables are `PCCONTROLLER_TOOLCHAIN_CLI` and
  `PCCONTROLLER_TOOLCHAIN_CONFIG`; command-line values take precedence. These
  values are forwarded through the shared Controller command plan, so compile
  never falls back to a different global CLI after a portable bootstrap.
- Windows host packaging requires `go-winres` unless
  `--skip-resources` is explicit, and UPX unless `--no-upx` is explicit.
- The C ABI package requires a native target-matching C compiler unless
  `--no-shared-library` is explicit. On Windows the resolver inspects both the
  target triple and predefined macros, rejects Cygwin/MSYS impostors, and picks
  the newest compatible MinGW-w64 compiler it can discover. If none exists, it
  installs the exact latest-resolved user-scoped toolchain from
  `Tools/Dependencies/resolved-tools-lock.json` through Windows Package
  Manager; the scheduled dependency resolver advances that lock to the latest
  stable release. `--no-compiler-bootstrap` changes installation to a clear failure.
  `HTTP_PROXY`, `HTTPS_PROXY`, and `ALL_PROXY` remain in the child environment,
  and the selected proxy is also supplied through the package manager's native
  proxy option without logging its value.

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
node --check Tools/Build/go-tests.mjs
node --check Tools/CommandPlan/controller-command.mjs
node --test Tools/Build/build.test.mjs
node --test Tools/Firmware/firmware.test.mjs
npm --prefix Tools/Controller/web ci --no-audit --no-fund
npm --prefix Tools/Controller/web run typecheck
npm --prefix Tools/Controller/web run build
build.cmd --dry-run --no-color
bash.exe build.sh --dry-run --no-color
```

The integration suite freezes identity values and asserts that both CMD/Bash
launcher pairs emit byte-equivalent JSON plans, compile/program through
Controller, preserve the explicit USBasp method-selection boundary, and never
introduce a PowerShell or direct dependency-upload action.

On Windows, Go tests are compiled to stable names beneath
`%LOCALAPPDATA%\PCController\test-programs\go`; other platforms use
`.build/tests/go/`. A content/toolchain/environment identity reuses an existing
passing result; `--retest` runs the same binaries again without inventing new
temporary executable names. The cache identity includes embedded WebUI and
default-recovery assets, and the shared lock prevents concurrent worktrees from
overwriting one another's binary/cache pair.

For a machine-level Windows backstop, this workstation sets Go's `GOTMPDIR` to
`%LOCALAPPDATA%\PCController\go-noexec-temp` and grants the interactive user an
object-inherit-only deny-execute ACL there. That makes a mistakenly invoked
temporary `*.test.exe` non-executable while leaving the supported stable output
directory executable. To intentionally remove the workstation guard, remove
that explicit deny ACL with `icacls` and run `go env -u GOTMPDIR`; do not do so
for normal project work.
