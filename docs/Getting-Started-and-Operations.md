<div align="center"><a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Getting started and operations

This is the canonical operating guide for ControllerBoardMini, the native
Controller host, its embedded WebUI, and the Virtual Board. It describes the
current source tree and keeps three kinds of state separate:

- firmware and board settings stored in MCU EEPROM;
- host settings stored in the platform PC configuration directory;
- build/deployment evidence stored in manifests and backup directories.

Contributors should open the [Repository and File Map](Repository-Map.md)
before editing. It shows which file owns each domain, what is generated, where
tests live, and which per-user files must never enter Git.

> [!CAUTION]
> Start with unloaded outputs. A successful build, simulator run, or browser
> session does not prove a relay, mechanism, supply, sensor, or programmer is
> wired safely.

## 1. Prerequisites

For a complete local validation environment, install:

- Go and Node.js versions accepted by the checked-in module/package metadata;
- Git and the platform C/C++ toolchain required by the Virtual Board;
- the tools resolved by PCController's managed firmware profile;
- optional `go-winres`, MinGW-w64, and UPX for full Windows packaging.

Do not install random Arduino libraries into the normal user sketchbook to
repair a managed build. PCController resolves the firmware CLI, MiniCore, and
library versions from its project-owned profile.

## 2. Build without touching hardware

From the repository root:

```console
build.cmd --all --clean --no-upx
```

```bash
./build.sh --all --clean --no-upx
```

Focused commands:

```console
build.cmd --host-only
build.cmd --firmware-only
build.cmd --dry-run
```

Build and test the Virtual Board separately:

```console
Push-Location Tools/VirtualBoard
cmake --preset release
cmake --build --preset release --parallel
ctest --preset release
Pop-Location
```

The checked-in Ninja presets are `debug`, `release`, and `relwithdebinfo`;
configure, build, and test use the same name and write to
`Tools/VirtualBoard/.build/<preset>/`.

The default plan never discovers, opens, resets, or programs hardware. The
root build writes firmware and host output to:

```text
.build/firmware/
Tools/Controller/bin/
```

The standalone Virtual Board helper uses `Tools/VirtualBoard/.build/`; the CI
matrix uses its own `.build/virtual-board/` staging directory.

If the build reports success, retain its manifest and hashes. Do not substitute
a loose executable or a manifest from an earlier source tree.

## 3. Choose a control surface

### Embedded WebUI

```console
Tools\Controller\bin\controller.exe web --port COM18
```

The host listens on <http://127.0.0.1:8787/> by default. It opens the browser
only after a controller has completed HELLO authentication. While the board is
offline, the URL remains available for host settings, port discovery,
diagnostics, and reconnect; device-only controls remain absent.

Use `--no-open` when another application will navigate to the WebUI:

```console
Tools\Controller\bin\controller.exe web --no-open --port COM18
```

On Windows, a primary-owning web process also provides a native tray menu by
default. Add `--no-tray` when that shell is not wanted. Its connection label is
derived from authenticated serial state, page links appear only while a board
is connected, and both automatic launch and tray actions refuse to open a page
while offline. Connect/Reconnect and Exit remain available in either state.

Windows desktop integration is explicit and reversible. `desktop ensure`
installs the current executable's per-user protocol, Start-menu entry, and
hash-bound `toast-logo.png` identity. Every WinRT toast uses that local PNG as
its `appLogoOverride`; Windows is never asked to silently render a text-only
product toast. `desktop test` sends a branded, actionable end-to-end
diagnostic. `desktop uninstall` removes only entries whose ownership still
matches that executable.

Notification delivery selects the strongest available Windows surface in a
fixed order: branded WinRT toast, legacy notification-area balloon with the
product icon, then a bounded TaskDialog. `desktop test` succeeds only when the
first, image-bearing WinRT backend is active; runtime status reports the
selected backend and whether it was branded.

```console
Tools\Controller\bin\controller.exe desktop ensure
Tools\Controller\bin\controller.exe desktop test
Tools\Controller\bin\controller.exe desktop uninstall
```

For a durable per-user installation, use the extracted Windows package rather
than copying `controller.exe` by itself. The release package carries a
hash-bound inventory for the exact executable, embedded resources, host
manifest, notices, and companion files:

```console
controller.exe install --expected-package-sha256 <inventory-root-sha256> --desktop
controller.exe installation status
controller.exe repair --expected-package-sha256 <inventory-root-sha256> --desktop
controller.exe uninstall
```

Installation and repair use content-addressed slots and a recovery journal, so
an update never overwrites the running image. Repair is idempotent when every
installed digest is healthy and rebuilds a damaged slot only from a verified
package. Enabling native desktop integration or changing its display name also
journals the prior and desired identities before mutation; interrupted cleanup
or activation is retried idempotently before status can report healthy. The
default uninstall preserves both roaming configuration and local host data,
including board backups. Data removal is deliberately separate:

```console
controller.exe uninstall --purge-data --preview-purge
controller.exe uninstall --purge-data --confirm-purge PURGE-PC-CONTROLLER-USER-DATA
```

The exact confirmation is required in addition to `--purge-data`; neither flag
is implied by uninstall. Preview reports the exact deduplicated set without
deleting it. A configured host file is removed only as that exact file, while a
canonical or `PCCONTROLLER_DATA_DIR` data root is recursive only when its durable
product/user ownership marker validates. Unmarked non-empty roots and any path
containing a symlink, junction, reparse point, device alias, or alternate data
stream are refused. `--desktop` uses the direct native URI/AUMID/shortcut adapter
with the exact installed executable. Lifecycle lock waits are interruptible and
bounded to five minutes. Unsupported platforms return an error rather than
falling back to a shell script.

The exact embedded WebUI can also be exported for audit or a trusted static
host. The command never overwrites an existing archive:

```console
Tools\Controller\bin\controller.exe web export --output pccontroller-webui.zip
```

Follow the [portable WebUI contract](../Tools/Controller/docs/Portable-WebUI.md)
when selecting a controller origin; tokens never belong in the exported
configuration file or URL.

`web` is a complete headless primary mode. It owns serial discovery/reconnect,
the command dispatcher, automations, global hotkeys, integrations, IPC, and the
browser event stream without constructing a terminal UI.

Where the browser offers installation, add the WebUI to the desktop or home
screen for a standalone, native-feeling window. The installed manifest includes
shortcuts to Overview, Workbench, Activity, and Settings. Installation does not
make the controller available without its host: the service worker is
network-only and keeps no offline cache, so a stopped host is reported at once.

### Terminal UI

```console
Tools\Controller\bin\controller.exe tui --port COM18
```

The TUI provides live status, command entry, menus, RF, macros, host actions,
settings, programming flows, and diagnostics. Keyboard help is available from
inside the interface.

On Control, use `Up`/`Down` to select a relay, motion action, or named PWM
channel. PWM is shown as 0–100%; `Left`/`Right`, `Home`/`End`, or the mouse
slider changes it. On Board and App Settings, `Enter` opens a modal draft.
Adjust fields with the arrow keys, then press `Enter` to save or `Esc` to
discard. Multi-value rows keep decimal precision, visibility, relay restore,
and status-light state fields together so a selection cannot accidentally
change an unrelated row. On Events, press `E` to expand or compact the aligned
history graphs.

### Shell and one-shot CLI

```console
Tools\Controller\bin\controller.exe shell --port COM18
Tools\Controller\bin\controller.exe exec --port COM18 status
Tools\Controller\bin\controller.exe monitor --port COM18 --json
```

Use the shell for repeated work and `exec` for one acknowledged operation. A
one-shot process is not the right owner for a long-running scheduled output.

## 4. Connect to the intended controller

Selectors may be supplied as a COM name, friendly name, VID/PID, USB serial,
or Windows device instance:

```console
controller.exe tui --port COM18
controller.exe tui --device "USB-SERIAL CH340"
controller.exe tui --device 1A86:7523
controller.exe tui --device serial:ABC123
controller.exe tui --device instance:<device-instance>
controller.exe tui --vid 1A86 --pid 7523 --name CH340
```

List current candidates before narrowing:

```console
controller.exe ports
```

Automatic selection is fail-closed:

1. enumerate candidates through the platform adapter;
2. apply all supplied filters;
3. open one candidate at a time;
4. require the native `PCController` HELLO identity;
5. remember the stable identity after success;
6. ask the operator when several indistinguishable candidates remain.

COM numbers can change. Prefer a stable selector for deployed systems.

## 5. Understand primary ownership

The first long-running WebUI, TUI, shell, or IPC server becomes the primary
serial owner. Later Controller processes use its loopback JSON-RPC/event stream
instead of opening the device again.

This prevents:

- competing reads and mismatched native sequence IDs;
- accidental DTR reset from a second serial open;
- a programmer starting while an application session still owns UART;
- different user interfaces applying different safety rules.

If another program owns the port, use the diagnostic shown by Controller.
Platform owner actions are explicit: bring forward and ask-close are safe first
choices; termination is guarded, confirmed, and never automatic.

## 6. First WebUI flow

On first launch:

1. Choose language and direction. Persian uses the bundled Vazirmatn font.
2. Choose light, dark, or system theme and reduced-motion preference.
3. Decide whether optional interaction audio is enabled.
4. Review the detected host and connection state.
5. Open the dashboard after the setup state is saved.

If optional interaction audio is enabled, a supported visible mobile page may
pair select, success, and warning cues with short vibration patterns. Haptics
are supplemental, never required, and are silent when the platform does not
provide them or the page is not visible.

The dashboard distinguishes:

- **controller connected** — an authenticated board and usable device controls;
- **host ready** — the WebSocket/host is available but no board is authenticated;
- **paused/reconnecting** — a requested or automatic connection transition;
- **offline** — no usable host event transport.

Never infer board connection from the word WebSocket or a green transport
indicator. The controller state is separate.

### Keyboard conventions

The command palette, navigation, help, theme, language, audio, reconnect, and
terminal actions are keyboard-accessible. Help renders each physical key in a
separate keycap; separators such as `+`, `/`, and “then” are plain text.

Focus remains visible, dialogs trap focus, Escape closes the current transient
surface, and reduced motion avoids long or disorienting transitions.

### Browser communication

WebSocket is the preferred full-duplex RPC/event transport. REST is a bounded
fallback. `BroadcastChannel` synchronizes allowed state across same-origin
tabs. Terminal and Activity receive board and host events from the same primary
runtime.

The terminal safely renders common `console.*` levels, `%s` substitution, and
validated `%c` style spans. It does not evaluate HTML or JavaScript from output.

## 7. Core operations

Confirm identity and status first:

```text
hello
status
settings
menu list
```

### Telemetry

Status reports available supply voltage, current, power, temperatures, door and
Bluetooth inputs, relay/PWM state, displays, RF, menu, reset cause, and boot
count. A missing sensor remains missing; the host does not invent a normal
reading. JSON status retains `uptime_ms` for calculation and adds a derived
human-readable `uptime` string for direct presentation.

Use the dashboard chart to select electrical, power, or thermal series and a
time window. The accessible summary and table expose recent values without
requiring visual chart interpretation.

### Relays and motion

R5–R8 are general-purpose user outputs. R1–R4 form two direction/enable sides
and should be controlled through side-motion commands rather than raw relay
writes.

Before a host motion start, Controller requests fresh status and applies the
configured door policy. Firmware still owns local reed gating and
break-before-make sequencing. Stop/off remains available under every policy.

Commission unloaded, verify direction, then add one controlled load at a time.

### PWM, enclosure light, and status output

- PWM channels 0–10 are user/commissioning channels.
- Channel 11 owns enclosure illumination.
- Channel 12 is the power indicator.
- Channels 13–15 are status red/green/blue.
- D6 owns the addressable strip.

PWM has no global operating state. Read all sixteen logical values with
`pwm get`, write one channel directly with `pwm set CHANNEL VALUE` (`0..15`,
`0..4095`), and use host macros or automations for scheduled demonstrations.
`pwm off` clears all sixteen channels and is an emergency all-output operation;
use it deliberately. Board output-persistence settings independently decide
whether motion, user relays, and the eight EEPROM-backed user PWM values may be
restored after a normal boot; programming mode always keeps them off.

Web and API clients reconcile every mutation against a fresh sixteen-channel
board readback. Generic sliders cover only user channels `0..10`; channels
`11..15` remain visible but use dedicated illumination, power-indicator, and
status-RGB controls. `controller.pwm.values`, `controller.pwm.set`, and
`controller.pwm.off` provide the typed RPC surface; matching REST operations
are `GET`, `PUT`, and `DELETE /api/pwm`.

### Buzzer and effects

`beep path board|host|both|none` selects the two independent renderers. A
deprecated compatibility alias remains accepted for existing automation.
`silent board ...` changes the EEPROM-backed board mute while `silent host ...`
changes PC playback; neither silently overwrites the other. A board-silent
planned tone is still pushed to the host and may play there when the host path
is enabled. Stopping a melody prevents future notes, but a tone already
accepted by a renderer may finish.

Current boards render status effects from one compact descriptor. Use any
decimal or hex RGB color with `breathe`, `flash`, `cycle`, or `transition`;
effect names do not imply colors. Reusable `rgb profile` condition descriptors
live in EEPROM and apply immediately after a verified write.

For the standard hands-on attention cue, use `display both --duration 5s WAIT`, `melody
play attention 0`, and `rgb effect play attention`. Repeat count zero is the
explicit until-stopped mode. On acknowledgement, run `melody stop`, `rgb
effect stop`, then `display both --duration 1200ms ok`; normal display/status ownership
resumes after the bounded `ok` message.

### Displays and menus

The TM1637 front panel is firmware-owned. Optional LCD and hosted menu overlays
are host-owned. The WebUI and TUI may preview or write supported display/menu
state, but board EEPROM remains the source of truth for local layout and
brightness settings.

Use `display segments|lcd|both` for arbitrary text. Segment messages longer
than four cells marquee automatically; `--scroll` forces the same effect for a
short message. `--repeat once|loop|interval`, `--speed`, `--duration`, and
`--interval` control the sequence. A marquee always renders a fully blank final
frame before stopping or restarting. The automatic door message defaults to
interval scheduling at about two presentations per minute, not an endless
loop.

See [Front Panel and Menus](Front-Panel-and-Menus.md) for all pages, gestures,
and save/discard behavior.

### RF

The Web workbench and TUI RF page provide a guided A/B/C/D handset workflow.
Select one labeled button, begin the bounded capture, press only that handset
button, and verify the read-back identity (slot, code, bit length, protocol, and
pulse timing) before choosing an action. A fresh identity starts on **Unmapped**
and no A/B/C/D label is inferred to mean physical K1/K2/K3/K4. If that exact
board record already has an explicit saved action, the review preserves it.
The next handset step opens only after the Unmapped choice or an explicit
mapping is acknowledged.

The same surface reviews the complete board inventory, marks unmapped and
duplicate identities as needing attention, and exposes explicit remap, remove,
clear, and one-burst verification actions. Removal and clear require a separate
confirmation. Transmit is always a single reviewed burst in the guided UI;
isolate actuators and observe the intended receiver before confirming it. If the
controller disconnects, the workflow pauses and hides device actions. Escape
cancels an in-progress capture. In the TUI press `W` on the RF page, then use
`A`, `B`, `C`, or `D` to choose a handset label and `Enter` to capture/confirm.

RF commands and typed RPC can also list, learn, remove, clear, transmit, and map
records. A new learned record is unassigned until reviewed. Direct learned
mappings to the motion relay group are rejected; use the side-action path so
reed and sequencing rules remain active.

Record the remote, code, bit length, protocol, mapping, physical result, and
stop behavior during commissioning.

### I²C

```text
i2c scan
```

Expected defaults are INA219 at `0x40`, the PWM expander at `0x41`, and an
optional LCD at `0x27` or `0x3F`. A raw transfer is an advanced reviewed action
with bounded length and cooperative bus ownership.

## 8. Settings and configuration

### Board settings

MCU settings include sound, door/relay audio, sensor assignment, motion policy,
direction break, enclosure mode/brightness, TM1637 brightness, status
brightness/color, display precision, PWM defaults, telemetry period, and menu
layout.

The board validates its EEPROM CRC and rewrites canonical defaults when the
record is invalid. Do not copy board settings into host JSON and call that a
backup.

### Host configuration

The host creates strict JSON configuration in the platform user-config
directory:

```text
Windows  %AppData%\PCController\config.json
Linux    $XDG_CONFIG_HOME/PCController/config.json or ~/.config/PCController/config.json
macOS    ~/Library/Application Support/PCController/config.json
```

Inspect it safely:

```console
controller.exe config path
controller.exe config show
controller.exe config validate
controller.exe --config lab.json config validate
```

Long-running hosts watch atomic replacements, retain last-known-good settings
after invalid edits, and apply safe live changes without silently inventing
defaults. Use [Host Configuration and Integrations](Host-Configuration-and-Integrations.md)
for the full schema and security boundaries.

Web settings normalize text and endpoint input while editing, show current
validation state, and block malformed or out-of-scope integration targets.
The same host configuration owns `ui.peripheral_presentation` for relay,
motion-side, and MOSFET/PWM names, descriptions, and ordering. The historical
`ui.peripheral_names` map remains compatible. The operation never touches MCU
EEPROM. Use `controller.exe peripherals`, `controller.peripherals.get`/`.set`,
or `GET`/`PUT /api/peripherals` when another host surface needs the same ordered
descriptors; connected subscribers receive updates immediately.

## 9. Monitor, scripts, and IPC

### Monitoring

```console
controller.exe monitor --port COM18 --interval 500ms
controller.exe monitor --port COM18 --json > telemetry.jsonl
controller.exe monitor --port COM18 --count 10
```

### Batch scripts

```console
controller.exe batch --port COM18 --file commissioning.pc
type commissioning.pc | controller.exe batch --port COM18 --file -
```

Scripts are line-oriented and support comments, variables, sleeps, and repeats.
Use fail-fast for programming and safety-sensitive commissioning.

### Local IPC

```console
controller.exe ipc serve --port COM18
controller.exe ipc call --method controller.snapshot
controller.exe ipc call --method controller.command.execute --params "{\"command\":\"status\"}"
```

The listener defaults to loopback. Remote mode requires a long token, explicit
origins, and capability policy. Read/event permission does not imply board
writes, reset, programming, power, virtual keys, automation, or bridge calls.

See [Protocol and Network API](../Tools/Controller/docs/Protocol-and-Network-API.md)
for routes, envelopes, subscriptions, range behavior, and authorization.

## 10. Build, back up, and program firmware

Safe build-only commands:

```console
firmware.cmd build
firmware.cmd check
firmware.cmd watch
```

Explicit deployment:

```console
firmware.cmd upload --method urclock --port COM18
firmware.cmd upload --method usbasp --port COM18
```

The primary Controller coordinates:

1. validate the exact artifact and target;
2. snapshot board identity and settings;
3. quiet outputs and preserve audible state;
4. create and validate flash/EEPROM/metadata backup;
5. close the native application session;
6. program and verify/read back;
7. require a fresh authenticated application HELLO;
8. restore settings and audio state;
9. verify semantic readback and clear the recovery marker.

If a write transaction failed after the image may already have reached flash,
do not bypass the primary or repeat the write blindly. With the authenticated
board connected, ask the primary to recover that exact transaction:

```console
controller.exe program recover firmware.hex [PORT]
```

`PORT` is optional and acts only as an exact-device assertion against the
currently authenticated controller; it cannot redirect recovery to another
device. A secondary Controller instance automatically delegates the request to
the primary serial owner. The primary matches the firmware SHA-256 and durable
failed-session marker, reasserts the safe programming state, releases UART, and
performs a fresh **read-only** Urboot semantic verification. It does not rewrite
flash. It then reconnects only to the same physical device identity, restores
or reinitializes settings according to the recorded transaction, verifies
readback, and clears the marker only after success.

A missing optional LCD is reported as a capability warning: TM1637/host status,
mandatory backup, verification, reconnect, and safe recovery continue. A
verification or exact-device mismatch remains fatal, leaves safe outputs
asserted, and retains the marker for diagnosis. Direct AVRDUDE, direct serial,
or ISP bypass is not an equivalent recovery path.

Selection, download, source watch, and staging are inert. Every write has a
separate review boundary. Backup failure blocks by default.

For remote delivery, open **Firmware & updates → Release & workflow
discovery**. Select GitHub release, successful workflow artifact, or HTTP
manifest; choose artifact kind/platform; then discover metadata. Review the
hash and build/packed timestamp comparison before choosing **Download and
stage only**. ZIP releases are inspected without unsafe extraction. Once the
verified item appears in the content-addressed inventory, select it and use the
separate board-programming, EEPROM-restore, or host-update review action.

This is also the intended automatic pipeline boundary: an update service may
perform discovery and staging unattended, but it must submit a distinct
authorized update request to the primary process before hardware is opened.
Set `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` (or their lowercase forms) in
the primary process environment when the remote host or GitHub requires it.

ISP is an explicitly selected recovery route. Connect it only after the
ordinary bootloader path has been reviewed and found unavailable. Read
[Toolchain and Safe Programming](Toolchain-and-Safe-Programming.md) before the
first physical write.

## 11. Use the Virtual Board

Start the packaged simulator, then connect explicitly:

```console
controller.exe tui --port tcp://127.0.0.1:8765
controller.exe exec --port tcp://127.0.0.1:8765 hello
```

The Virtual Board is useful for command, event, API, reconnect, menu, settings,
and UI development. It does not validate electrical timing, bus wiring, RF
range, acoustic output, thermal behavior, or loaded mechanisms.

## 12. Troubleshooting

### Web page loads but says host ready

The HTTP/WebSocket host is available, but no controller is authenticated.
Check `ports`, selectors, cable/driver state, and HELLO diagnostics. Do not
expect device controls or a Live label in this state.

### Favicon or static asset returns 404

Use the canonical packaged executable from `Tools/Controller/bin`, not an old
loose binary. The production bundle includes the real multi-resolution ICO,
SVG mark, font, scripts, and styles.

### Port is busy

First check whether another Controller primary already owns it; use that
primary's IPC. If another program owns the port, use the reported owner details
and safe bring-forward/ask-close actions. Never terminate a process simply
because its PID appears in an error.

On Windows, owner diagnosis first makes a two-second, target-scoped Restart
Manager query over the selected COM alias and its `QueryDosDevice` targets. A
cancelled lookup requests the native
[`RmCancelCurrentTask`](https://learn.microsoft.com/en-us/windows/win32/api/restartmanager/nf-restartmanager-rmcancelcurrenttask)
path and no background native-query worker survives the call. `RmGetList`
documents a cancelled result, but Windows does not guarantee that every native
call or driver returns at the context deadline. The primary does not run
WMI/CIM, shell out, or sweep the machine-wide NT handle table.

Restart Manager's documented
[`RmRegisterResources`](https://learn.microsoft.com/en-us/windows/win32/api/restartmanager/nf-restartmanager-rmregisterresources)
contract accepts full file paths and does not promise COM device-path
association. Some serial drivers reject `RmGetList` for a COM resource. On that
specific result, the primary starts the same canonical Controller executable in
a hidden, read-only helper mode. The child performs one bounded NT handle scan,
writes one size-limited strict JSON result, and starts no configuration, UI,
serial session, network listener, or shell. The parent owns its hard context,
kills it on timeout, waits for termination, and briefly caches the result so no
stalled native worker or temporary executable survives a retry.

Returned actions are guarded by PID, process start time, executable identity,
and current window ownership; termination also requires the exact confirmation
shown by the app. If both native paths fail, the app preserves the original
serial-open error, reports the attribution limitation, and offers no process
action. Maintainers can validate a specific already-owned port through the
canonical executable without opening or configuring that port:

```console
bin\controller.exe --internal-port-owner-diagnose COM18
```

Run that command from `Tools/Controller` while a labeled external application
already owns the selected port. A successful record has `found:true` plus the
expected PID, executable, process start time, and optional window metadata. A
`found:false` or `error` record is an honest driver/permission limitation and
must not enable bring-forward, close, or terminate controls.

### Board reconnects repeatedly

Confirm supply and cable, stable selector, baud, DTR policy, one serial owner,
and authenticated HELLO failures. Reset lines and application reset are
different operations.

### Sensor value is absent or implausible

Check shared ground, INA219 VIN+/shunt orientation, I²C addresses, OneWire
pull-up, sensor ROM assignment, and readings against a trusted instrument.

### Settings do not save

Read the field-level WebUI error or run `config validate`. Endpoint settings
must be normalized roots inside their allowed network scope. Board settings
also require a connected authenticated device and valid EEPROM response.

## Final acceptance

Before calling an artifact complete, run the current build gate, exercise the
packaged WebUI/TUI/CLI/API flows, preserve hashes/manifests, and complete every
applicable physical item in [Project Acceptance](Project-Checklist.md).

<p align="center"><a href="../README.md">← Return to the PCController main page</a></p>
