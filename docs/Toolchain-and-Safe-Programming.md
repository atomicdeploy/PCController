<div align="center"><a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Toolchain Bootstrap and Safe Programming

PCController exposes one generic firmware toolchain interface. MiniCore, the
firmware dependency CLI, AVR compiler/programmer tools, and upstream libraries
are implementation dependencies; their product names are not part of the
public command hierarchy.

## Latest-first and reproducible by design

The toolchain has two source-controlled layers:

- `Tools/Controller/toolchain-profile.json` is a **policy**. It selects the
  latest compatible stable releases at or above declared minimums.
- `Tools/Controller/toolchain-lock.json` is an **exact resolution**. It records
  versions, download URLs and SHA-256 values, the MiniCore archive hash,
  library archive hashes, the Urboot tag/commit/tree, and Go lock hashes.

Normal bootstrap resolves the current stable policy. `--locked` intentionally
uses the existing exact lock for offline recovery, rollback, or forensic
reproduction. It is not the default update mode.

Prerelease firmware-CLI builds and Urboot `main` are canaries only. They may be
reported by `--include-canary`, but they are never selected, installed, or
written to the stable lock automatically. A resolution that differs only in its
timestamp preserves the existing lock byte-for-byte.

Inspect or refresh the policy without opening a serial port:

```console
controller toolchain profile
controller toolchain lock
controller toolchain check --include-canary --require-current
controller toolchain update
```

For CI and dependency maintenance, the smaller resolver imports no serial, IPC,
TUI, or integration package:

```cmd
cd Tools\Controller
go run .\cmd\toolchain-resolver check --include-canary --require-current
```

These commands query registries and files only. They do not enumerate COM18,
reset the MCU, start the TUI, write EEPROM, or call a programmer.

## Clean-machine bootstrap

A prebuilt Controller host is the seed on a new machine. From the project root:

```console
Tools\Controller\bin\controller.exe toolchain bootstrap --dry-run
Tools\Controller\bin\controller.exe toolchain bootstrap
```

The first command is read-only. By default bootstrap resolves the current
stable policy, then:

1. selects the current operating-system/architecture firmware-CLI asset;
2. verifies its registry-provided SHA-256 before extraction;
3. creates profile-local data, cache, and user/sketchbook directories;
4. binds child processes to those directories through a generated config and
   explicit environment values;
5. refreshes core and library indexes;
6. installs the exact MiniCore and library versions from that resolution;
7. installs the AVR compiler, AVRDUDE, and Urclock metadata supplied by the
   resolved MiniCore package;
8. records the managed firmware-CLI path in PC-side host configuration.

The generated CLI configuration path is recorded independently from the
executable path. This keeps an explicit global `--cli` usable even though its
core/library installation lives under the machine-specific PCController data
root. Omitting `--cli` installs the verified portable executable there too;
`board initialize --portable-cli` deliberately forces that portable choice.

To reproduce the checked-in lock without registry resolution, use:

```console
Tools\Controller\bin\controller.exe toolchain bootstrap --locked --dry-run
Tools\Controller\bin\controller.exe toolchain bootstrap --locked
```

Use the managed executable and its generated configuration explicitly for a
reproducible project build:

```console
build.cmd --all --toolchain-cli C:\path\to\arduino-cli.exe --toolchain-config C:\path\to\firmware-cli.yaml
```

`PCCONTROLLER_TOOLCHAIN_CLI` and `PCCONTROLLER_TOOLCHAIN_CONFIG` provide the
same build-only defaults. Explicit command-line switches win. Proxy selection
continues to come only from the standard proxy environment, with the shared
local-network and loopback bypass policy; neither path persists a proxy URL.

Bootstrap does not replace an unrelated global executable or add a
developer-specific source directory to `PATH`. Override the managed location
with `--install-dir`, use an existing dependency CLI with `--cli`, or provide
reviewed alternate `--policy`/`--lock` files. `PCCONTROLLER_DATA_DIR` may move
the host data root but must be absolute.

The managed tree is isolated from the ordinary Arduino15 directory and normal
sketchbook. Reports distinguish `cli_installed` from
`cli_downloaded_this_run`, and include exact config, data, cache, and user
paths. Later Controller compile, core-info, bootloader, and programming
operations reuse this configuration. `toolchain sync` remains a separate
operation for auditing/updating an explicitly selected existing installation.

When a verified managed workspace already exists and the shared Arduino
installation is incomplete or duplicated, adopt it without another download:

```console
controller toolchain adopt --source-data C:\path\to\managed\data --source-user C:\path\to\managed\user --target-data "%LOCALAPPDATA%\Arduino15" --target-user "%USERPROFILE%\Documents\Arduino" --cli "C:\Program Files\Arduino CLI\arduino-cli.exe" --toolchain-config "%LOCALAPPDATA%\Arduino15\pccontroller-firmware-cli.yaml"
```

This project-owned operation atomically replaces complete package-vendor
trees (`arduino`, `builtin`, and `MiniCore`), copies verified libraries, and
saves the shared CLI/config paths. It specifically repairs installations that
claim MiniCore is installed but lack `cores/MCUdude_corefiles/Arduino.h` or a
board `variants/` directory. It is network-free and never creates another CLI
installation.

At the time of this documentation update, the exact lock resolves:

| Area | Stable lock |
| --- | --- |
| Firmware dependency CLI | 1.5.1, per-platform archives SHA-256 verified |
| Board core | `MiniCore:avr@3.1.2` |
| FQBN | Canonical `fqbn` in [`toolchain-profile.json`](../Tools/Controller/toolchain-profile.json); copied only into generated lock/runtime artifacts |
| Urboot | `u8.0.1`, commit `bd52751acaa5923163e938a6e35051c22317da68` |
| Go | 1.26.5 |
| PWM library | Adafruit PWM Servo Driver Library 3.0.3 |
| Power monitor library | Adafruit INA219 1.2.3 |
| RF library | rc-switch 2.6.4 |
| Seven-segment library | TM1637TinyDisplay 1.12.2 |
| Temperature libraries | DallasTemperature 4.0.6 and OneWire 2.3.8 |

The production firmware uses local drivers where that materially reduces AVR
flash. The upstream libraries remain installed for examples, comparison, and
future profiles.

The complete source build also uses Node.js, npm, UPX, go-winres, and GitHub
Actions. Their stable/LTS policy and exact hashes live in
`Tools/Dependencies/`; npm transitive graphs remain exact in their respective
`package-lock.json` files. See `Tools/Dependencies/README.md` for the unified
no-device updater and scheduled validation workflow.

## Proxy behavior

Bootstrap, resolution, synchronization, and dependency maintenance inherit the
complete parent environment. They recognize upper- and lower-case
`HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `FTP_PROXY`, and `NO_PROXY`, plus
`ARDUINO_NETWORK_PROXY` for the present firmware backend. Child processes
receive the same environment. Proxy values are neither printed nor persisted.

Controller never invents or hardcodes a proxy endpoint. The caller supplies
it through standard proxy environment variables. Every Go host process also
preserves the caller's `NO_PROXY` entries and appends loopback, `.local`, IPv4
private/CGNAT/link-local networks, and IPv6 loopback/ULA/link-local networks;
both upper- and lower-case forms are passed to children. Local IPC, API,
WebSocket, discovery, and bridge traffic therefore bypasses an Internet proxy
by default while external dependency downloads can still use it.

When a backend needs its own proxy key, Controller derives it process-locally
from `HTTPS_PROXY`, then `HTTP_PROXY`, `ALL_PROXY`, or `FTP_PROXY`. This avoids
putting credentials in `firmware-cli.yaml`.

The configured-network attempt runs first. With direct retry enabled, a failed
network operation is tried once more in a child environment with proxy
variables cleared. Neither the parent process nor machine environment changes.
Use `--direct-retry=false` for Controller commands or `--no-direct-retry` for
the unified updater where policy requires proxy-only access.

## Urboot-Custom reproducibility

Urboot-Custom is the generic patchable bootloader variant. Its active source is
hash-pinned Urboot `u8.0.1`; the customization is a unified diff with a
backend-neutral progress hook. The currently selected backend emits TM1637
page progress, but the bootloader name and core patch do not depend on that
peripheral.

Urboot `u8.0`, MiniCore 3.1.2, AVR GCC 7.3.0, and Binutils 2.26.20160125 are
retained only as a stock-hash fixture. The build must first reproduce both old
MiniCore images exactly, then verify the active `u8.0.1` source hashes, apply
the diff, derive the exported-page-writer jump from the ELF, and enforce the
custom size/address/metadata assertions. This separates reproduction-fixture
hashes from the active latest-stable custom source.

The last verified custom build used 510 of its 512 allocated bytes and imposes
a 32,256-byte application ceiling. The current stock-core profile instead
links a 32,344-byte sketch and derives its 12-byte identity footer from that
profile's 32,384-byte application limit (`0x7E74..0x7E7F`). It therefore does
not claim drop-in compatibility with the custom 512-byte bootloader. A future
Urboot-Custom feature profile must select its own 32,256-byte ceiling and
identity address, then remove or reconfigure enough application features to
fit. Rebuild firmware and rerun the selected bootloader assertion immediately
before any ISP installation.
See `Tools/Bootloader/Urboot-Custom/README.md` for hashes, retained features,
optional size tradeoffs, and the installation procedure.

## Automated dependency maintenance

The unified no-device commands are:

```console
update-dependencies.cmd --check --require-current
update-dependencies.cmd --apply --validate
```

or on POSIX systems:

```console
./update-dependencies.sh --check --require-current
./update-dependencies.sh --apply --validate
```

The scheduled GitHub workflow resolves stable dependencies from primary
registries, updates exact locks, and validates the candidate before it can open
a pull request. Validation covers firmware and its 32,256-byte ceiling,
Urboot-Custom source/diff/hash/512-byte checks, Go and host packaging, Win32
resources and UPX, both npm domains and the web production build, plus the
VirtualBoard build/tests. Prerelease and `main` observations remain canary-only.
All external Actions use immutable commit revisions with readable major-version
comments; their exact revisions and workflow consumers are included in the
host-tool lock. Normal host CI reads its exact Node.js and go-winres versions
from that lock instead of duplicating them in workflow source.

The candidate report includes npm security totals. Its deterministic PR plan
adds upstream release-note links, explicit license/security review, firmware and
bootloader headroom, compressed-host size, and reviewer checkboxes. This plan is
uploaded with the same evidence and used verbatim as the validated PR body.

A failed candidate uploads its report/evidence and creates or updates one
actionable blocked-update issue rather than proposing broken source. A later
passing run closes that issue. The workflow definition and local source checks
have been validated, but a real hosted scheduled/manual run has **not yet been
observed**. Hosted artifact publication, issue lifecycle, and dependency-PR
creation therefore remain explicit live-CI acceptance items.

## Guarded programming lifecycle

All normal firmware writes go through Controller. Direct dependency-backend
upload is intentionally unavailable. The ordinary UART bootloader path is:

```console
controller program flash .build\firmware\PCController.ino.hex COM18
```

Before releasing the application UART, Controller performs this sequence:

1. validate Intel HEX and calculate its SHA-256;
2. authenticate the running PCController application;
3. query board-owned MCU settings and store a separate semantic snapshot;
4. create a durable recovery marker before making a temporary board change;
5. cancel any macro and command every relay and PWM channel off;
6. persist a temporary reset-safe MCU image: Silent, illumination zero/off,
   output persistence and relay restore mask cleared, status light off, and
   programming-visible TM1637;
7. show `Prog` on TM1637 and `Programming...` / `Do not disconnect` on the
   host-owned front panel and physical LCD where supported;
8. wait beyond the firmware's deferred EEPROM-write window and verify the
   temporary settings by querying them back;
9. close the application UART;
10. read and verify full flash, EEPROM, and programmer-metadata backup;
11. write and verify the selected firmware image;
12. durably mark the host programming outcome, reconnect to a fresh
    application HELLO, restore exact original MCU settings, wait, query and
    compare;
13. release the programming panel, show `Programming done` / `Device ready`,
    and remove the recovery marker only after successful readback.

If the board was already silent, it remains silent. If it was audible, it is
made audible again only by restoring the captured MCU settings. Unsupported
display and LCD operations are capability warnings so a reduced board can still
use mandatory raw EEPROM backup. Failure to force safe outputs or persist safe
settings is fatal while retaining the recovery marker. A normally returned
failed flash still records its final host outcome, then attempts reconnection
and exact restoration.

An interrupted operation leaves a marker under the host `state` directory. On
every authenticated reboot before host completion is recorded, the primary
reasserts relay/PWM-off, the safe EEPROM image, and `Prog`; it deliberately does
not resume illumination or audio. Once the programmer has recorded its final
outcome, the next connection finishes exact restoration and clears the marker.

### Development EEPROM reinitialization

`--reinitialize-eeprom` is a deliberately destructive, host-tool-only escape
hatch for an unpublished development board whose `GET_SETTINGS` payload no
longer matches the current host schema. It does **not** add an old settings
decoder, migration table, or compatibility branch to the firmware. Use it only
when losing the board's previous semantic settings is acceptable:

```console
controller program flash .build\firmware\PCController.ino.hex COM18 ^
  --method urclock --reinitialize-eeprom
```

If another Controller instance owns the UART, the secondary uploads the exact
firmware artifact and this flag to the primary through the typed
`controller.update.firmware` request, then follows that operation to its final
result. The primary remains the only process that opens the port.

The exception still requires a complete verified flash, raw EEPROM, and
programmer-metadata backup before the firmware write. It cannot be combined
with `--allow-incomplete-backup`. Before releasing UART ownership, the host
persists the settings-query failure and any live state it could capture,
cancels macros, releases all relays, fades PWM when possible (otherwise forces
it off), shows the programming cues, and plays the power-down melody. It does
not rewrite the incompatible EEPROM before that raw backup.

Immediately after the untouched raw backup, Controller reconnects to a
current-schema application when available, persists and reads back the durable
Silent/Programming flags, restores `Prog`, and releases UART again before the
firmware write. This post-backup boundary is part of the flash gate: failure to
arm a supported latch prevents flashing. If the old settings schema is
genuinely unsupported, Controller cannot safely patch unknown EEPROM bytes and
reports that limitation; the newly written transaction image becomes the
first safe latch point.

After verified flashing, and before clearing the lifecycle marker, the host
programs and independently reads back a complete generated 1,024-byte
**transaction** EEPROM image. It contains the canonical Go-owned settings,
empty RF store, rich status-LED profiles, and an armed Silent/Programming latch
with visible `Prog`. Every verification/reconnect reset after this write
therefore remains quiet and output-safe. Only after the new authenticated
`HELLO` does the host query the current schema, commit and verify ordinary
canonical settings with Silent and Programming cleared and all relay/PWM
persistence disabled, then play the single configurable ready cue. It never
maps or restores old semantic values or live outputs. The original raw EEPROM
remains recoverable from the pre-flash backup, but restoring it would
deliberately reintroduce the incompatible development state.

### Alpha version and feature-profile policy

During alpha, a newer **version build** may replace the current wire, flash, or
EEPROM layout directly. Do not add dual readers, migrations, compatibility
aliases, or preservation logic merely to accept an earlier alpha version. Keep
the mandatory raw backup, reject the obsolete semantic layout, and use the
explicit reinitialization transaction when the old values are intentionally
discarded.

Compatibility and preservation are required only when distinct
**profile/feature builds are intentionally supported at the same time**. Each
such profile must declare its capabilities, application limit, identity
address, and persistence layout; host routing and tests must select the profile
rather than guessing from a version number.

Because the durable programming latch suppresses the MCU's ordinary boot tune
during intermediate resets, the host waits until settings/output verification
and front-panel restoration are complete, then streams the configurable rising
`programming-ready` melody. A normal restore skips that cue when the captured
MCU Silent setting was enabled; explicit development reinitialization ends with
Silent disabled and therefore plays it. This keeps audible completion aligned
with the host's ready state instead of sounding during the first reboot.

Regression acceptance must record the reset count and observe the complete
guarded transaction: once the post-backup latch is armed, no ordinary startup
melody may occur and TM1637 must retain `Prog` across every flash,
EEPROM-readback, and application-authentication reset. Repeated startup notes
are a lifecycle failure even when programming ultimately succeeds.

### Read-only recovery of an already-written image

Use the following only when a guarded write transaction ended as failed but the
selected image may already be present:

```console
controller program recover firmware.hex [PORT]
```

The command is owned by the authenticated primary runtime. A secondary CLI,
shell, TUI, or IPC client delegates it to that primary; no second process opens
the port. The optional `PORT` is an assertion that must match the primary's
already-authenticated device, including its stable physical identity. It is not
a fallback selector and cannot move a pending transaction to another board.

Recovery loads and validates the HEX, matches its SHA-256 plus device
fingerprint to one durable failed programming session, and reasserts that
session's safe relay/PWM/audio/display state. It then releases the application
UART and asks Urclock for a fresh, read-only flash verification. The semantic
verifier checks the programmed bytes and critical vector/reset behavior; this
command never writes or patches flash.

Only a verified result can complete the programmer outcome. The host then
reconnects to the exact saved device, authenticates application HELLO, performs
the recorded normal restore or development EEPROM reinitialization, verifies
settings and safe outputs, and clears the recovery marker. Any verification,
identity, reconnect, or restore failure keeps the marker and programming-safe
state. Direct dependency-tool, raw serial, or ISP invocation is deliberately
not accepted as a bypass.

An absent optional LCD is a warning-only capability result. The host records
that no physical LCD message was shown, while mandatory backup/readback,
TM1637/host presentation, exact reconnect, and recovery continue normally.

### Verified development recovery checkpoint

The 2026-08-02 recovery pass completed against the Instance-ID-pinned COM18
controller without using ISP. The board authenticated as `800A5B70`; its exact
deployed application artifact SHA-256 is
`bb928b7b680d6e393d842bd28412b6760340a2ff61ae40b21a926036358bb092`.
The primary-owned Urclock transaction backed up, wrote, and verified 32,244
programmed bytes, then reauthenticated the application before reporting
success.

The previously durable recovery marker is cleared. Current-schema EEPROM was
intentionally reinitialized and read back with Silent off, illumination Off,
all output-persistence bits off, relay restore mask zero, and motion break
1 ms. After the pinned reconnect, the safe live snapshot showed 12.282 V, PWM
available, no active relays, no framing/CRC protocol errors, and reset count 22.
The optional LCD was absent, so its presentation step produced a non-fatal
capability warning; raw backup, programming verification, application return,
and safe-state recovery did not depend on that optional peripheral.

## Advanced USBasp recovery

### Blank-board initialization

Use one ownership-aware transaction for a new board:

```console
controller board initialize --skip-toolchain
controller board provision --uart auto
controller board provision --firmware PCController.ino.hex --uart COM4
controller board provision --force-initialize --firmware PCController.ino.hex --uart COM4
```

The initializer validates
the USBasp target signature, backs up flash, EEPROM, signature, fuses, and lock
bits, then invokes the selected FQBN's stock core `burn-bootloader` recipe with
verification. It does not install PCController's custom Urboot experiment.
The first failed default-speed USBasp exchange is retried with a 32 microsecond
bit-clock period. Initialization completes its mandatory backup at the working
speed, obtains the fuse bytes from the selected FQBN's expanded core properties,
and uses slow SCK only to repair that fuse policy before probing again at normal
speed. A successful fast probe makes the core bootloader burn use `usbasp`;
`usbasp_slow` is retained only when normal speed still fails (or was explicitly
forced). This avoids carrying a recovery clock into the much larger bootloader
write while keeping a conservative fallback.

Initialization owns only the selected fuse policy and bootloader; it never
compiles or uploads application firmware. `board provision` first authenticates
an existing application and retains it when healthy. With `--firmware HEX`, it
uses the verified UART bootloader to write/readback exactly that image. If the
application cannot authenticate, Provision probes Urboot before falling back
to ISP, preserving a usable bootloader during application repair; use
`--force-initialize` only when ISP setup must precede the upload. INA219,
PCA9685, DS18B20, and LCD absence is non-fatal so a partially populated board
can still be initialized and verified.

`--name` accepts at most eight printable ASCII characters with no surrounding
whitespace. After the first authenticated application boot, Controller writes
the name through the native SETTINGS exchange, commits the combined
settings/name EEPROM record with CRC-8, and verifies readback. Use `controller
board name get`, `set NAME`, or `clear` afterward. A requested name requires
UART; bootloader-only initialization cannot store it.

The native application record is authoritative, not Urboot user metadata.
MiniCore disables Urclock metadata for ordinary uploads, and Urclock's title is
upload-time flash metadata that requires a bootloader reset and consumes scarce
top-of-flash space. The native record is mutable while the application runs,
survives normal firmware uploads because the selected core retains EEPROM, and
works through the same local or remote Controller owner. A future profile may
mirror the name into Urboot metadata for provisioning audit, but that mirror
must never become the source of truth.

The Programming page in the TUI runs the same transaction with `I`; the CLI
remains useful for logs and automation.

`board initialize`, `board provision`, and `board blank` are also Controller shell commands, so
the existing versionless API/RPC/TUI command path uses the exact same Go
lifecycle rather than a browser- or shell-specific AVRDUDE path. The output's
machine-readable report includes each named phase and measured milliseconds.

USBasp is an explicit troubleshooting fallback. Its ISP transport must never
receive a COM or friendly-name selector. Supply the application connection
separately so the same settings, display, and buzzer lifecycle runs around ISP:

```console
controller program flash firmware.with_bootloader.hex ^
  --method usbasp --app-device "USB-SERIAL CH340"
```

Selecting `--method usbasp` is sufficient; `--programmer` is needed only to
override the host's configured ISP backend for different hardware.

`--app-device` accepts the same COM, friendly-name, VID:PID, serial, and
instance selectors as the ordinary connection layer. The resolved application
port is kept separate and is never placed in the ISP command.

Standalone USBasp fails closed when the application-lifecycle selector is
absent or cannot authenticate. `--allow-incomplete-backup` is the explicit,
logged recovery override for an application UART that is genuinely
unavailable; it is never the default. When the primary TUI owns the board, the
request routes through that owner and reuses its authenticated runtime.

Installing Urboot-Custom itself requires ISP because the running bootloader
protects its region and the custom image begins one page lower. Application
updates can use UART/Urclock afterward. The first ISP write cannot render
TM1637 progress because the MCU is held under ISP control and the display pins
are also SCK/MOSI.

On Windows, `controller driver usbasp status` identifies the exact USBasp
instance. `driver usbasp install --package DIR` validates a pre-generated
VID/PID-specific INF and invokes PnPUtil for a noninteractive elevated install.
`driver usbasp ensure` does nothing for a healthy started driver and downloads
and launches Zadig only when a connected USBasp lacks one. `driver usbasp zadig
--latest [--download-only]` resolves the stable official libwdi GitHub release
dynamically, stores it below the canonical per-user host data root, and requires
Windows Authenticode trust before launch. Go's standard HTTP proxy environment
is authoritative; no proxy endpoint is hardcoded. Zadig remains a visible
fallback because it exposes no supported silent driver-install interface.

Return an initialized test board to shelf state with `controller board blank
--confirm BOARDNAME --uart auto`. The operation authenticates that exact name,
persists the `Prog` safety latch, and takes a new complete backup. If the part
starts on factory clocking, it temporarily applies the selected external-crystal
core fuse policy at slow SCK, proves normal-speed USBasp, then carries out the
long erase/readback at fast speed. It clears application/bootloader flash and
EESAVE-preserved EEPROM to `FF`, restores ATmega328P factory fuse/lock defaults
`62/D9/FF/FF`, then repeats full flash/EEPROM/fuse readback at a factory-safe
SCK. A successful report therefore proves a *factory blank*—not merely a
blanked deployed board. When UART identity is genuinely unavailable, the
deliberately conspicuous fallback is `--uart none --confirm ERASE-BOARD`.

## Persistence ownership and artifacts

MCU EEPROM and PC host configuration remain separate:

- the board owns its settings, eight-byte operator name, learned RF
  records/mappings, and reset journal;
  the current image has no generic EEPROM automation table;
- the host owns port preferences, UI/network configuration, histories,
  scripts, and tool paths;
- a programming settings snapshot is a backup artifact, not a host setting,
  and is never merged into the host configuration file.

The default Windows data root is `%LOCALAPPDATA%\PCController`:

```text
backups\operations\                  raw programming transactions/manifests
backups\firmware\sha256\             deduplicated firmware blobs
backups\board-settings\sha256\       semantic MCU-settings snapshots
state\programming-recovery-*.json    interrupted-operation recovery markers
tools\toolchain\                     managed firmware dependency CLI
tools\toolchain\firmware-cli.yaml   managed dependency configuration
tools\toolchain\data\                isolated cores and compiler/programmer tools
tools\toolchain\downloads\           isolated download cache
tools\toolchain\user\                isolated libraries/sketchbook
tools\zadig\TAG\                     cached Authenticode-verified Zadig release
```

No unpublished alpha-version migration chain is retained in normal firmware.
Offline settings tools operate on a complete, validated EEPROM backup and
preserve RF, reset-journal, and unknown bytes outside the current profile's
settings/name record:

```console
controller eeprom inspect --backup-manifest BACKUP\manifest.json
controller eeprom export --backup-manifest BACKUP\manifest.json --output SETTINGS.hex
controller eeprom import --backup-manifest BACKUP\manifest.json --settings SETTINGS.hex --output EEPROM-RESTORE.hex
controller eeprom restore --backup-manifest BACKUP\manifest.json --output EEPROM-ORIGINAL.hex
```

Outputs are hashed and never overwritten. These file operations do not write a
board.

## Verification boundary

The dependency resolver, exact stock/custom Urboot build, lock comparison, and
hardware-free source tests can be run without a device. They prove source,
hash, compiler, layout, packaging, and simulator properties; they do not prove
electrical behavior, physical display visibility, UART availability, or a
successful ISP installation.

No COM18, TUI, UART programming, EEPROM write, or ISP operation is implied by a
successful no-device dependency check. Hardware results must be reported
separately after the corresponding board test is actually performed.
