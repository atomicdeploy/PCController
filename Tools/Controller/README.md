<div align="center"><a href="../../README.md"><img src="../../docs/assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Host Controller Tool

`controller` is the PC-side utility for ControllerBoardMini. It combines an
authenticated serial console, Charm terminal UI, embedded browser UI, native protocol client,
programmer launcher, JSON-RPC IPC service, script runner, reusable Go package,
optional C-compatible library, and firmware WebSocket relay in one codebase.

## Capabilities

- Automatic serial discovery and reconnect by COM ID, VID/PID, friendly or
  human name, USB serial number, or Windows PnP instance identity
- Event-driven Windows USB/serial change notification, stable last-device
  persistence, and an interactive chooser when multiple devices match
- A `tcp://host:port` transport for the protocol-compatible virtual board
- Mandatory `PCController` identity handshake before an automatically detected
  port is accepted
- Explicit `open`, `close`, and `reconnect`
- 115200 8N1 native COBS/CRC-8 protocol
- Live voltage, current, power, temperature, input, relay, menu, and PWM
  status in a Bubble Tea/Bubbles/Lip Gloss interface
- Responsive React control center embedded in `controller.exe`, with
  Vazirmatn Persian typography, RTL/LTR flow, dark/light/system themes,
  reduced-motion support, graphs, dialogs, audio cues, and keyboard navigation
- Continuous text or newline-delimited JSON monitoring for logging
- Host-side command shell with history, completion, quoting, and raw protocol
  access
- Repeatable batch scripts with variables, sleeps, repeats, and fail-fast or
  continue-on-error behavior
- Cross-platform NDJSON JSON-RPC 2.0, REST v1, authenticated standard
  WebSocket, and bounded Engine.IO-v4/Socket.IO-over-WebSocket service; the
  first TUI/shell process owns serial and later clients route through it
- Importable Go API and optional `c-shared` JSON ABI
- Persistent JSON host configuration, `fsnotify` hot reload, macros,
  event-driven automations, a typed local-device contract, and loopback data-hub integration
- A fixed, read-only Windows host-facts catalog for system, computer, firmware,
  storage, and serial diagnostics; callers cannot submit arbitrary queries
- DTR and RTS reset pulse
- Controller-owned Arduino CLI compile/update, MiniCore `urclock`, and guarded
  USBasp/AVRDUDE recovery workflows; direct Arduino upload is disabled
- Watched-file WebSocket firmware relay with SHA-256 validation,
  auto-reconnect, temporary-file cleanup, and programming callback

## Build and test

Go 1.25 or newer is required. The current workstation uses Go 1.26.5.

```console
build.cmd --host-only
```

Run that command from the repository root. The shared project-owned Node build
runs the locked web typecheck/tests/build before `go mod download`,
`go test ./...`, and `go vet ./...`, embeds and verifies
Windows icon/manifest/version resources, strips the Go binary, UPX-packs and
tests it, builds and smoke-tests the C ABI, and publishes:

```text
bin\controller.exe
bin\host-manifest.json
bin\pccontroller.dll
bin\pccontroller.h
bin\licenses\...
```

`bin\licenses` collects the license/notice files from every resolved Go
module for binary redistribution.

Resource regeneration and UPX are default packaging steps. Use root
`--skip-resources` or `--no-upx` to disable them explicitly. The ordinary
executable is built with `CGO_ENABLED=0` and does not require a C compiler.
During development, `controller version` reports `development`, a SHA-256
source hash, and the UTC build time injected by the build script. Windows
resources intentionally use numeric `0.0.0.0` and the string `development`;
there is no fabricated release version or tag.

`build.cmd` and `build.sh` share one Node/Controller plan. See the
[project-owned build guide](../Build/README.md) for deterministic identity,
bootstrap requirements, and dry-run/plan commands.

Windows development, deployment, discovery, and programming examples use
the project-owned Controller executable and platform adapters for device
notifications, discovery, diagnostics, and desktop actions. Optional OS
enrichment stays behind those adapters and an unavailable capability is
reported explicitly.

Run the current source without a separate package build:

```console
go run -buildvcs=false ./cmd/controller
go run -buildvcs=false ./cmd/controller ports
```

Manual equivalents:

```console
cd web
npm ci
npm run typecheck
npm test
npm run build
cd ..
go mod download
go test ./...
go vet ./...
go build -buildvcs=false -trimpath -o bin\controller.exe ./cmd/controller
```

The default root package also builds `pccontroller.dll` and its generated C
header. That target needs `CGO_ENABLED=1` and a native MinGW-w64 compiler
matching `go env GOARCH` on `PATH` (or an explicit compatible `CC`); the build
rejects MSYS-only targets. Use `--no-shared-library` only when intentionally
building without the ABI. See
[C Library API](docs/C-Library-API.md).

## Embedded web control center

Launch the full primary host lifecycle and open the same-origin app:

```console
.\bin\controller.exe web
.\bin\controller.exe web --no-open
.\bin\controller.exe web --no-tray
.\bin\controller.exe web export --output pccontroller-webui.zip
```

The production bundle is compiled into the Go executable. The listener serves
the SPA, REST, typed JSON-RPC, subscriptions, integration proxy, standard
WebSocket, and Socket.IO without a second web server. Static delivery includes
strong ETags and exact `HEAD`, suffix/open/closed `Range`, `If-Range`,
multipart-range, and `416` behavior. Local-device actions use the bounded host
manager; arbitrary data-hub resources stream through the authenticated
loopback-only bridge without forwarding the controller token.

`web` starts the complete primary runtime without instantiating Bubble Tea or
reading terminal input. It owns controller discovery/reconnect, automations,
global hotkeys, local integrations, and the shared command engine for its full
lifetime, so a TUI is neither launched nor required. The browser terminal sends
ordinary dispatcher commands while the persistent WebSocket independently
pushes status, board events, host events, and global page actions. Those event
lines are interleaved in the bounded terminal transcript and remain available
in the filterable Activity page.

On Windows, a primary-owning `web` process also provides a native tray menu by
default; `--no-tray` disables it. Its tooltip and status row use the
authenticated controller state, not merely an open HTTP/WebSocket listener.
Dashboard, Controls, Workbench, Updates, and Settings actions exist only while
the board is connected. Connect/Reconnect and Exit remain available while
offline, and a late disconnect is checked again before any page can open.
The native popup is created and themed before first use, cached between opens,
keyboard-accessible, and refreshed only when its authoritative state changes.
Session lock and system suspend synchronously release held keyboard ownership
and apply the configured `stop-motion`, `all-off`, or release-only policy under
a bounded deadline. Unlock, resume, and network changes reconcile telemetry or
device discovery without overriding a deliberately paused connection. These
policies are validated and editable from the Web Settings page.

Per-user URI and Start-menu integration is opt-in and can be removed
idempotently. Cleanup verifies ownership before deleting anything and preserves
foreign registrations or shortcuts:

```console
.\bin\controller.exe desktop ensure
.\bin\controller.exe desktop uninstall
```

The UI includes live electrical/thermal graphs, relay and PWM controls, a
peripheral workbench for displays, addressable LEDs, sound, RF, macros, I2C,
host actions and recovery diagnostics, a typed local-device surface, a
service-neutral data workspace, dialogs/toasts, command palette, procedural
audio, and guarded command confirmation. Its cinematic first-launch gate,
clip/filter/spring transitions, glass layers, semantic colors, responsive
mobile navigation, RTL geometry, and dark/light themes are code-native and do
not require remote assets.

The bundled manifest lets supported desktop and mobile browsers install the
same-origin UI in standalone presentation and provides shortcuts to Overview,
Workbench, Activity, and Settings. Its service worker always uses the network
and stores no offline response cache. Closing the Controller host therefore
becomes visible immediately instead of leaving an obsolete control panel. When
optional interaction cues are enabled, supported visible-page mobile clients
may also use restrained vibration for select, success, and warning feedback;
no workflow depends on haptics.

The exact embedded distribution also has a deterministic, secret-free ZIP
export contract for trusted static hosting. See
[Portable WebUI bundle](docs/Portable-WebUI.md) for archive guarantees,
controller-target validation, and the required origin policy. `web export`
fails if its destination already exists and prints file/byte counts plus the
archive SHA-256.

### Bounded Windows host facts

The shared `os facts` command exposes five read-only diagnostic profiles:
`system`, `computer`, `firmware`, `storage`, and `serial`. `os facts list`
returns their descriptions, selected class, columns, and maximum row counts.
The same fixed catalog is available through JSON-RPC and REST:

```console
bin\controller.exe exec os facts system
bin\controller.exe ipc call --method controller.os.facts.catalog
bin\controller.exe ipc call --method controller.os.facts --params "{\"profile\":\"serial\",\"timeout_ms\":2500}"
```

The Windows adapter uses a bounded native management provider internally. It
accepts only catalog profile names: no shell command, arbitrary query text,
write, method invocation, or caller-selected class/column is exposed. Results
have fixed row/byte/cell limits, a short private cache, and a deadline no longer
than five seconds. Other platforms report this optional capability as
unavailable.

## Terminal UI

Launch the authenticated auto-detecting TUI:

```console
.\bin\controller.exe
```

Narrow discovery when several USB serial adapters are present:

```console
.\bin\controller.exe tui --port COM18
.\bin\controller.exe tui --device "USB-SERIAL CH340"
.\bin\controller.exe tui --device 1A86:7523
.\bin\controller.exe tui --device serial:ABC123
.\bin\controller.exe tui --vid 1A86 --pid 7523 --name CH340
```

`--device` accepts a COM ID, human/friendly name, `VID:PID`,
`serial:VALUE`, or `instance:VALUE`. A unique match is selected
automatically; a TUI/shell/programming command displays a numbered chooser
when several indistinguishable adapters match. After a successful native
HELLO, the port plus stable identity fields are saved in the PC JSON config
as `connection.last_device`. This does not write the MCU EEPROM.

The same filters can be supplied through `PCCONTROLLER_DEVICE`,
`PCCONTROLLER_PORT`,
`PCCONTROLLER_VID`, `PCCONTROLLER_PID`, `PCCONTROLLER_NAME`, and
`PCCONTROLLER_BAUD`.

Keys:

```text
Ctrl+O    resume authenticated auto-connect
Ctrl+X    explicitly close and pause reconnect
Ctrl+R    pulse DTR and RTS
Ctrl+F    bring a diagnosed serial-port owner window to the foreground
Ctrl+W    ask a diagnosed serial-port owner window to close gracefully
Ctrl+T    press twice within five seconds to terminate a diagnosed owner
Ctrl+C    exit
Up/Down  shell history
Tab       command completion
PgUp/Dn  scroll the event log
```

Automatic selection never trusts VID/PID or a friendly USB name alone. Every
candidate must answer the native `HELLO` request with board kind `1` and the
name `PCController`.

After flashing and disconnecting USBasp, use this minimal COM18 smoke test:

```console
bin\controller.exe ports
bin\controller.exe exec --port COM18 hello
bin\controller.exe exec --port COM18 status
bin\controller.exe exec --port COM18 stream 1000
bin\controller.exe tui --port COM18
```

The first long-running TUI, shell, or `ipc serve` process becomes the primary
serial owner at `127.0.0.1:8787`. Later `exec`, batch, monitor, reset, shell,
and programming invocations use its JSON-RPC command/event stream instead of
opening COM18 a second time. A second interactive TUI intentionally falls back
to the IPC-backed secondary console. `hello` should report the
`PCController` identity, and
`status` should print the complete 48-byte telemetry record as named values,
including the captured reset cause and persistent boot count. The stream
command enables one periodic status
frame per second. Non-PCController programs that bypass this IPC ownership
must still not open the same COM port concurrently.

On Windows, a direct local-COM access-denied or sharing-violation error is
enriched through native handle and window inspection. Where process
permissions allow, the error and TUI show the owner's
process name, PID, executable path, and top-level window. Wide TUI layouts also
show **Owner**, **Ask Close**, and **Terminate** buttons. Foreground and
graceful `WM_CLOSE` actions are safe first choices. Termination is never
automatic: it requires the second action within five seconds, and the host
rejects its current PID or another process using the same controller
executable. Protected/elevated processes may yield only an unresolved-owner
diagnostic; use the primary IPC path whenever the existing owner is another
controller instance.

The TUI refreshes its live cards at the configured interval (250 ms by
default) and also redraws immediately for streamed status events. Telemetry
updates do not flood the command log.

## Shell and commands

The command prompt is embedded at the bottom of the TUI. A plain terminal
shell and one-shot form are also available:

```console
bin\controller.exe shell --port COM18
bin\controller.exe exec --port COM18 status
```

Connection and protocol:

```text
ports
open [PORT]
close
reconnect
hello
status
voltage
current
temp [list|scan]
stream PERIOD_MS                       # 0 disables; otherwise >= 100
settings
settings decimals VOLTAGE CURRENT      # each 0..2; EEPROM read-modify-write
settings color INDEX                   # persistent ready-color preset 0..7
settings motion-exit-hold SECONDS       # persistent 1..31-second menu exit hold
settings set FLAGS LIGHT ON OFF DISPLAY_OPEN DISPLAY_CLOSED STATUS OUTPUT_PERSISTENCE
  STREAM DEFAULT_PAGE SAVE_LAST STATUS_COLOR VOLTAGE_DECIMALS
  CURRENT_DECIMALS MOTION_EXIT_HOLD_SECONDS RELAY_RESTORE_MASK
event latest
event wait [KIND] [TIMEOUT]
query OPCODE RESPONSE_OPCODE [PAYLOAD_HEX]
write HEX_BYTES
```

`settings` reports decoded status color, decimal precision, motion exit hold,
the host-owned programming latch, and the raw extended byte. Focused update
forms first read the board's current record and replace only their own fields.
Decimal precision zero through two is supported; an all-zero decimal encoding
means the configured two-decimal default.

Menu, relays, PWM, feedback, and RF:

```text
menu prev|next|dec|inc
menu page N
relay N on|off|toggle
relay side left|right stop|up|down
relay off
relay test [MS]                        # default 250; 0 stops
pwm get
pwm off                                # emergency clear of all 16 channels
pwm set CHANNEL VALUE                  # channel 0..15, logical value 0..4095
rgb R G B [BRIGHTNESS]
rgb effect list
rgb effect play NAME                  # background; TUI/shell/IPC stay responsive
rgb effect wait NAME                  # foreground; useful with one-shot exec
rgb effect stop|status
strip pixel N R G B [BRIGHTNESS]
strip fill R G B [BRIGHTNESS]
strip clear
buzzer FREQUENCY_HZ DURATION_MS
melody list
melody play NAME [REPEATS]            # background; 0=until stopped, otherwise 1..20
melody wait NAME [REPEATS]            # wait until all notes are acknowledged/played
melody stop|status
silent status|on|off
display segments|lcd|both DURATION_MS [TEXT]
macro list|show NAME_OR_ID|create ID NAME [CATEGORY [COLOR]]|delete NAME_OR_ID
macro record start NAME [CATEGORY [COLOR]]|record status|record save|record discard
macro play NAME_OR_ID|status|cancel [keep]
automation list|run NAME
rf send CODE BITS PROTOCOL [PULSE_US]  # protocol 1..12
rf learn [indefinite|timer [DURATION]] # default indefinite + multi-code; timer aliases: single, one-shot
rf cancel
rf list
rf remove ID|all
rf map ID none
rf map ID key 1|2|3|4 [press|toggle|momentary]
rf map ID menu prev|next|dec|inc
rf map ID relay 5..8 toggle|momentary|press
rf map ID side left|right up|down|stop
rf map ID pwm 0..10 [press|toggle|momentary]
i2c scan
reset lines|app|bootloader
```

The Web workbench and TUI RF page (`W`) wrap these commands in a guided
A/B/C/D handset flow. Each step opens one bounded capture, stops learning after
the new record event, reads the exact stored identity back, and requires an
explicit mapping confirmation. Both surfaces also flag unmapped/duplicate
records and guard remap, remove, clear, and one-burst verification actions.

Raw RF receive packets also produce host `rf.gesture` events. A new burst emits
`down`; a short release emits `up` followed by `click` after the 400 ms
double-click decision window; a second short burst in that window emits
`double-click`. A continuous burst emits `hold` after 600 ms and then `repeat`.
The repeat gate accelerates from 150 ms to 100 ms after two seconds and 60 ms
after four seconds, subject to the remote's actual packet rate. Because common
433 MHz codes carry no physical up/down bit, release and double-click are
necessarily inferred from packet gaps (250 ms release gap); very unusual
remotes with slower repeat packets may need future timing configuration.

Direct relay writes use human labels `1..8`, converted to protocol indices
`0..7`. R1-R4 requests are routed through the same
disable/break/direction/enable sequencer. Use `relay side ...` for motion
because it states the intended side and direction explicitly. Before a host
start command for R1-R4—including each host macro step—the host queries fresh
status and applies `safety.motion_door_policy`: `always` permits both door
states and is the default, `closed` permits only a closed enclosure, `open`
permits only an open enclosure, and `never` denies all motion. An unknown door
state is rejected whenever the selected policy needs a known state. Stop/off
commands remain available under every policy. The firmware's
direction/enable break-before-make sequencer remains authoritative at the
relay layer.

Learned RF records are deliberately prevented from mapping directly to
R1-R4. Use `rf map ID side left|right up|down|stop`; that firmware path is
reed-gated and retains the direction/enable interlock. Direct learned mappings
remain available for user relays R5-R8. Existing direct R1-R4 mappings can
still be listed and should be removed or remapped.

PWM is direct and per-channel; there is no global operating mode or autonomous
demonstration state. `pwm get` returns controller availability, the selected
channel, and all sixteen logical values. `pwm set` accepts channel `0..15`
(including the documented user/enclosure/power/status aliases) and value
`0..4095`. `pwm off` clears every channel, including enclosure, power, and
status RGB, and is intended as an emergency all-output clear.

The current settings record separates output policy from live values.
`OUTPUT_PERSISTENCE` is a bit mask: bit 0 restores motion, bit 1 restores user
relays, bit 2 restores the eight EEPROM-backed user PWM values, and bit 3 keeps
the selected motion direction relay when a side stops. `RELAY_RESTORE_MASK`
holds the last R1..R8 state; only the domains enabled by the persistence mask
may consume it after a normal boot. Programming mode clears both fields in its
temporary safe settings image and restores the exact captured record only
after verified reconnect.

Named melodies and status effects come from the watched PC JSON configuration.
`melody` sends one acknowledged tone at a time and waits for its duration and
gap before sending the next, avoiding the MCU's ten-entry tone-queue limit.
`rgb effect` supports `flash` and smooth `breathe` definitions and is capped at
20 native requests per second. Starting a new item replaces the old item on
that output; disconnect/cancellation stops future frames. Stopping an LED
effect leaves its base color at full configured brightness. Stopping a melody
cannot silence a tone already sounding because the current firmware has no
buzzer-stop opcode; that one tone may finish (at most five seconds by config).
The board's EEPROM `silent` setting still wins, so use `silent off` before an
audible host notification.

The reusable hands-on attention sequence is `display both 0 WAIT`, `melody
play attention 0`, and `rgb effect play attention`. Acknowledgement stops both
streams with `melody stop` plus `rgb effect stop`; `display both 1200 ok` then
shows the completion handoff and yields back to the normal front panel.

These are host-streamed effects: the host must remain connected and running.
Use `play` from the persistent TUI, shell, or IPC server. A one-shot
`controller.exe exec` exits after a background start, so use `melody wait ...`
or a finite `rgb effect wait ...` there.

`reset app` and `reset bootloader` currently request the same firmware
watchdog reset; the target byte is a forward-compatible hint. Use
`reset lines` or a programming command with `urclock` when bootloader entry
must be guaranteed.

See [Protocol and Network API](docs/Protocol-and-Network-API.md) for exact framing and
payload schemas.
See the [Control-Surface Capability Matrix](docs/Control-Surface-Capability-Matrix.md)
for command reachability across the TUI, CLI, Go/C libraries, IPC, REST,
WebSocket, Socket.IO, bridge, and event surfaces.

An explicit virtual-board endpoint uses the same authenticated native
protocol:

```console
bin\controller.exe exec --port tcp://127.0.0.1:8765 hello
bin\controller.exe tui --port tcp://127.0.0.1:8765
```

TCP endpoints are not discovered from the serial-port list. They must be
supplied explicitly, still have to return the `PCController` identity, and do
not support DTR/RTS control-line reset. This transport is a development link,
not an encrypted or authenticated network tunnel.

## Monitoring, scripts, and IPC

Continuously monitor a controller as human-readable rows or JSON records:

```console
bin\controller.exe monitor --port COM18 --interval 500ms
bin\controller.exe monitor --port COM18 --json > telemetry.jsonl
bin\controller.exe monitor --port COM18 --count 10
```

Run a command file:

```console
bin\controller.exe batch --port COM18 --file commissioning.pc
type commissioning.pc | bin\controller.exe batch --port COM18 --file -
```

Script syntax is line-oriented. Blank lines, `#` comments, and `;` comments
are ignored. The host-only directives are:

```text
set NAME VALUE
unset NAME
sleep 250ms
repeat 4 status
pwm set ${CHANNEL} 1024
```

Start cross-platform JSON-RPC IPC on loopback:

```console
bin\controller.exe ipc serve --port COM18
bin\controller.exe ipc call --method controller.snapshot
bin\controller.exe ipc call --method controller.command.execute --params "{\"command\":\"rf list\"}"
bin\controller.exe ipc call --method controller.rf.map --params "{\"id\":3,\"action\":\"key\",\"target\":\"2\",\"behavior\":\"press\"}"
bin\controller.exe ipc call --method controller.rf.transmit --params "{\"code\":1193046,\"bits\":24,\"protocol\":1,\"pulse_us\":350,\"repeats\":1}"
bin\controller.exe ipc call --method controller.command.execute --params "{\"command\":\"melody play notify\"}"
bin\controller.exe ipc call --method controller.command.execute --params "{\"command\":\"rgb effect play attention\"}"
bin\controller.exe ipc call --method controller.bridge.list
bin\controller.exe ipc call --method controller.bridge.call --params "{\"peer\":\"lab\",\"request\":{\"jsonrpc\":\"2.0\",\"id\":7,\"method\":\"controller.snapshot\"}}"
```

Use `ipc serve --stdio` for a parent process that wants newline-delimited
JSON-RPC over pipes. The API includes connection/reset/programming ownership,
snapshots/status/history, menus, RF, messages, events, OS-policy operations,
and correlated host-bridge calls; see
[Protocol and Network API](docs/Protocol-and-Network-API.md) for the canonical
method and REST route table.

`controller.history.status` reads the same retained measurement series before
and after a host restart. The default host configuration samples once per
second for 24 hours. Compact samples live in `measurements.jsonl` in the host
data directory, separately from important-event `timeline.jsonl`; startup
prunes expired, duplicate, and corrupt-tail records. Owner-only permissions,
atomic compaction, and a 32 MiB hard ceiling keep that local telemetry store
bounded. Setting `ui.history_hours` to `0` clears and disables measurement
retention without disabling the important-event timeline.

The TCP listener rejects non-loopback addresses by default. Remote mode
requires `ipc.allow_remote`, a token of at least 24 characters, a non-wildcard
browser origin list, and explicit `ipc.remote_policy` capabilities. Its safe
default permits read/event subscriptions only. Token possession alone does not
grant board writes, reset, programming, shutdown, virtual keys, power actions,
host-automation execution, or bridge calls.

Browser WebSocket sessions never put that durable token in a URL. The Web UI
uses authenticated `POST /api/v1/session/ticket` to obtain a 15-second,
single-use, Origin/peer/transport-bound ticket for each connection and
reconnect, offers it as a WebSocket subprotocol, and keeps REST command fallback
available if the live stream cannot be established. Native clients retain
header authentication. Inbound webhook events also drop credential-bearing
query/header fields, cookies, referrers, tickets, signatures, and the raw request
URI before publishing metadata.

Configured `integrations.websocket_clients` can subscribe to another primary,
forward loop-safe typed events, and issue correlated `bridge call` requests.
Each host still has exactly one local serial owner and the target reapplies its
own remote policy and board safety guards.

Go programs can import the module-root `controller` package directly:

```go
client := controller.New(controller.Options{Port: "COM18"})
defer client.Shutdown()
if err := client.Connect(ctx); err != nil { /* handle */ }
status, err := client.Status(ctx)

_ = client.SetRelay(ctx, 5, true)          // human R1..R8 numbering
_ = client.SetPWMChannel(ctx, 0, 2048)    // logical 0..15, value 0..4095
_ = client.SetStatusRGB(ctx, 30, 120, 255, 180)
_ = client.PlayTone(ctx, 1047, 75)
_ = client.TransmitRF(ctx, 0x123456, 24, 1, 350, 2)

operation, err := client.StartMelody(ctx, controller.Melody{
    Name: "notify",
    Notes: []controller.MelodyNote{
        {FrequencyHz: 1047, DurationMS: 75, GapMS: 25},
        {FrequencyHz: 1568, DurationMS: 150},
    },
}, 1)
if err == nil { err = <-operation.Done }
```

The API also exposes snapshots, events, shell execution, learned RF
learn/list/remove/map operations, serial-port enumeration, individual
R1-R8/PWM operations, RF transmit, custom RGB, tones, melodies, and status LED
effects. Every direct operation uses the same validation and native opcodes as
the command engine.

## Persistent host configuration and automation

Every invocation loads a strict JSON host configuration. If it does not exist,
the tool creates defaults at the platform user-config location:

```text
Windows  %AppData%\PCController\config.json
Linux    $XDG_CONFIG_HOME/PCController/config.json (or ~/.config/...)
macOS    ~/Library/Application Support/PCController/config.json
```

Override it with `PCCONTROLLER_CONFIG` or the global `--config FILE` option;
`--config` may appear before or after the subcommand. Useful inspection
commands are:

```console
bin\controller.exe config path
bin\controller.exe config show
bin\controller.exe --config lab.json config validate
```

### Product identity

User-visible default identity is owned by
[`web/package.json`](web/package.json): `productName`, `productShortName`,
`productTagline`, and `description`. The normal build verifies that the
generated Go constants and Win32 resources still match that source. Set
`ui.app_title` in the watched host configuration for a persistent title, or
set `APP_TITLE` for a one-process override; TUI, CLI help/version, web
navigation and browser title, desktop notifications, host-defined menus, and
service display names all consume the effective value. The same variable may
replace the web bundle's build-time fallback.

Wire HELLO identity, URI/header names, C ABI symbols, module names, firmware
artifact names, and the default config directory remain stable technical
identifiers. They are intentionally not derived from a user-editable title,
because changing them would break device, IPC, or upgrade compatibility.

See [examples/config.example.json](examples/config.example.json) for the full
schema: connection filters/timing, UI limits, project and firmware paths,
programming tools, named scripts, host-streamed relay/PWM macros, melodies,
status LED effects, and event-action automations. Firmware settings are
deliberately excluded because the board owns them in EEPROM.

The checked-in example deliberately leaves `connection.port` empty and uses
the observed CH340 identity (`VID 1A86`, `PID 7523`, friendly name
`USB-SERIAL CH340`). COM numbers can move; `last_device` is maintained by the
host after a successful connection and may be overridden by JSON, environment,
or CLI selectors.

TUI, shell, monitor, and IPC-server sessions watch the configuration directory
with `fsnotify`, debounce atomic replacements, and retain the last-known-good
values when a replacement is invalid. A five-second safety poll covers file
systems that miss replacement events. Connection changes re-arm discovery;
macro, melody, status-effect, programming, script, and automation lookups use
the current config. An already-running melody/effect keeps its validated
snapshot; edits apply to the next `play`.

Automation matches can filter lifecycle/state/message content, key/gesture
and physical/RF/host source, learned RF ID, RF code, or RF protocol. Actions
can run a board command, repeat an RF transmission, launch a bounded host
command or named script, or emit a host event. Rules have cooldowns, at most
eight actions, a 30-second action deadline, bounded captured output, and
cannot recursively match automation-result events.

`connection.reset_on_reconnect` applies only to a real automatic reconnect
epoch. When enabled, the host pulses DTR once after opening the first new
physical serial transport, before HELLO authentication. HELLO retries and
later candidate opens cannot create a reset storm, changing only this setting
does not reconnect, acknowledged application resets do not consume/re-trigger
the policy, and TCP endpoints are never pulsed.

## Programming

Bootstrap the latest resolved, host-data-local firmware toolchain with
`controller toolchain bootstrap`; use `--locked` only for an intentional exact
rollback. Its dependency executable, core/compiler data, downloads,
and user-library directory are isolated below the PCController data root rather
than the user's standard Arduino15/sketchbook locations. Explicit paths remain
available; otherwise direct
AVRDUDE/Urclock operations discover the programmer supplied by the selected
MiniCore installation. Add `--dry-run` to inspect a workflow without executing
it. The complete profile, proxy, artifact, and recovery rules are in
[Toolchain Bootstrap and Safe Programming](../../docs/Toolchain-and-Safe-Programming.md).

From `Tools\Controller`:

```console
bin\controller.exe toolchain compile ..\.. --output-dir ..\..\.build\firmware
bin\controller.exe program flash ..\..\.build\firmware\PCController.ino.hex 1A86:7523
bin\controller.exe program recover ..\..\.build\firmware\PCController.ino.hex [PORT]
bin\controller.exe program flash ..\..\.build\firmware\PCController.ino.with_bootloader.hex --method usbasp --app-device "USB-SERIAL CH340"
bin\controller.exe boot backup .\backups --device "USB-SERIAL CH340"
```

Direct dependency upload is disabled. Controller snapshots board-owned
settings separately from PC configuration, prepares the displays, temporarily
mutes an audible board, waits for deferred EEPROM persistence, completes a
verified flash/EEPROM/metadata backup, writes and verifies, reconnects, restores
the exact settings, and verifies their semantic readback. A durable recovery
marker survives host/programmer interruption. USBasp remains an advanced path
selected explicitly with `--method usbasp`, and its `--app-device` selector is
never sent to ISP. `--programmer` is only an optional backend-ID override.

`program recover HEX [PORT]` is a primary-owned completion path for a durable
failed programming session whose image may already be written. It matches the
HEX hash and exact authenticated device, performs a fresh read-only Urboot
semantic verification, reconnects only to that saved device, and completes the
recorded restore/reinitialization without rewriting flash. Secondary instances
delegate it through IPC. Verification or identity failure retains safe outputs
and the recovery marker; an absent optional LCD is only a presentation warning.
Do not substitute a direct programmer invocation or another COM port.

The direct USBasp workflow writes only the selected flash image. It does not
invent a sibling `.eep` filename and does not use the unsafe
dependency-backend `upload --programmer ... --input-file ...with_bootloader.hex`
shortcut.

Inside the TUI/shell, the compact equivalent is:

```text
program flash ..\..\.build\firmware\PCController.ino.hex COM18
program recover ..\..\.build\firmware\PCController.ino.hex [PORT]
program flash ..\..\.build\firmware\PCController.ino.with_bootloader.hex --method usbasp
boot backup .\backups
```

Each backup creates a new `pccontroller-YYYYMMDD-HHMMSS` directory containing
`flash.hex`, `eeprom.hex`, raw `programmer.txt`, and an atomic
`manifest.json`. The manifest records status, timestamps, method, port, MCU,
programmer, file sizes and SHA-256 hashes, and the application identity when
the native application was reachable before entering the bootloader. Firmware
blobs are stored by SHA-256 and reused rather than duplicated. A partial read
remains marked `incomplete`; it is never reported as a complete backup.

### Offline current settings transfer

No unpublished-build migration/version chain is retained. File-only commands
understand the current semantic 31-value-byte plus CRC-8 settings record and
report any other width/layout as unsupported:

```console
bin\controller.exe eeprom inspect --input .\eeprom.hex
bin\controller.exe eeprom inspect --backup-manifest .\backup\manifest.json
bin\controller.exe eeprom export --backup-manifest .\backup\manifest.json --output .\settings.hex
bin\controller.exe eeprom import --backup-manifest .\backup\manifest.json --settings .\settings.hex --output .\eeprom-restore.hex
bin\controller.exe eeprom restore --backup-manifest .\backup\manifest.json --output .\eeprom-original.hex
```

Export, import, and restore require a complete validated backup manifest.
Import overlays only EEPROM addresses `0x0020..0x003E` and preserves every
other byte from the full 1,024-byte backup. Outputs are canonical, hashed,
created without overwrite, and never written to a device by these commands.
An actual EEPROM write remains a separate explicitly confirmed operation.

The application protocol and Urboot/AVRDUDE are mutually exclusive users of
the same UART. Programming closes the application session before invoking the
tool, then re-arms connection and requires a fresh authenticated application
HELLO after the programmer exits. Native board commands are true framed
opcodes. Urboot/Urclock is deliberately delegated to MiniCore's current
AVRDUDE `urclock` implementation; the host coordinates boot mode and exclusive
port ownership rather than maintaining a second, potentially drifting
bootloader-protocol implementation.

### Browser and bridge update workspace

The embedded browser has a dedicated **Firmware & updates** page. It is a
host-mediated remote programmer, not MCU-native OTA: a browser or secondary
instance sends authenticated artifact/update requests to the primary host,
and only that primary process may take the serial/programming path.

The page can:

- select a local firmware, EEPROM Intel HEX, readback, or host executable;
  calculate SHA-256 in the browser; then upload and stage it without writing;
- download and verify an HTTP(S) or peer-host artifact, with optional expected
  SHA-256, while the Go transport honors the process proxy environment;
- discover the newest successful GitHub Actions artifact, latest/tagged GitHub
  release, or a product-neutral HTTP manifest before downloading; retain
  release/run/source metadata, provider digests, build hash/time, packed
  two-second timestamp, and target platform;
- safely select firmware/EEPROM/host content from ZIP releases, rejecting path
  traversal, links/devices, ambiguous matches, and expansion-limit violations;
- inventory and download content-addressed firmware, flash readbacks, EEPROM
  backups, and host packages without duplicating identical firmware bytes;
- explicitly request a fresh flash+EEPROM capture, board-firmware update,
  dedicated captured-flash restore, current-layout EEPROM restore, or
  recoverable host self-update; and
- show transfer bytes, progress, state, build hash, packed date/time comparison,
  digest, verification, and the same update events received over WebSocket.

Selection, download, and staging are deliberately inert. A separate review
dialog is the authorization boundary for every board write or host replacement.
The ordinary path probes Urboot/Urclock first; an ISP option is shown only
after that probe reports no bootloader route. Connecting ISP hardware does not
itself authorize a write.

A `flash-backup` never enters the firmware-update RPC. Its review action calls
the dedicated `controller.restore.flash` contract (or
`POST /api/v1/restores/flash`). The primary process then runs the guarded
backup-before-write programmer transaction, verifies the restored bytes,
reconnects application `HELLO`, and restores the saved lifecycle state. UART
Urclock is the default; USBasp is accepted only as an explicitly selected ISP
fallback.

Full host releases can embed `default-firmware.hex` plus a generated, complete
1,024-byte Intel HEX EEPROM image. The EEPROM contains current development
defaults, a valid CRC, dense menu IDs 0..13, safe-zero PWM values, and an empty
20-record learned-RF store; an empty compiler `.eep` is never substituted.
The recovery offer is enabled when the validated pair exists and disabled when
the firmware artifact is absent, but first-board programming still needs the
operator's explicit grant. The host manifest records both embedded SHA-256
values.

“Build-only” and “watch-only” mean exactly that: they compile and validate but
do not unexpectedly open hardware. To deliver watched changes, start watch with
its explicit upload option, or let an authenticated primary-host update request
run the guarded backup → quiet outputs → program → verify → restore sequence.
This separation prevents CI or a background source watcher from flashing a
connected board without an operator-selected deployment policy.

The discovery card is the deployment source side of that policy. It can stage
an update and stream `artifact.discovery.*` progress to every subscribed local
or remote UI. The subsequent programming review uses the existing guarded
update RPC. In other words, a deployment watcher can download and then request
programming, while a command explicitly named build/watch-only remains unable
to open COM or ISP hardware. Provider/peer bearer tokens are transient; proxy
variables are inherited by the Go HTTP client and its dependencies.

An authenticated peer can consume `GET /api/v1/discovery/manifest`, whose
relative artifact links point at this host's immutable SHA-256 download routes.
The same schema can be returned through `controller.discovery.local_manifest`;
no local filesystem path or credential is published.

## WebSocket firmware relay

The Controller includes a project-native WebSocket path for transferring a
validated ASCII Intel HEX image to a trusted programming host:

```console
bin\controller.exe ws serve --file ..\..\.build\firmware\PCController.ino.hex --listen 127.0.0.1:3000
bin\controller.exe ws client --url ws://BUILD-PC:3000/firmware --method urclock --port COM18
```

Each versioned JSON message contains the base filename, modification time,
SHA-256, and base64 firmware bytes. The client limits message/file size,
rejects unsafe names or checksum mismatches, writes a temporary `.hex`, runs
the selected programmer, and removes the temporary file.

This is standard WebSocket, not Socket.IO. Use the Controller server and client
together. The default listener is loopback-only; a non-loopback
listener should be used only on a trusted network because this phase does not
add authentication or TLS.

## Dependencies and provenance

Direct Go dependencies:

| Package | Version | License | Purpose |
|---|---:|---|---|
| `github.com/charmbracelet/bubbletea` | 1.3.10 | MIT | TUI runtime |
| `github.com/charmbracelet/bubbles` | 1.0.0 | MIT | input, viewport, spinner |
| `github.com/charmbracelet/lipgloss` | 1.1.0 | MIT | terminal layout/style |
| `github.com/coder/websocket` | 1.8.15 | ISC | firmware relay |
| `github.com/fsnotify/fsnotify` | 1.10.1 | BSD-3-Clause | host-config file watching |
| `github.com/go-ole/go-ole` | 1.3.0 | MIT | optional Windows system-profile adapter |
| `go.bug.st/serial` | 1.8.0 | BSD-3-Clause | serial I/O and USB enumeration |
| `golang.org/x/net` | 0.57.0 | BSD-3-Clause | standards-based proxy environment resolution |

Complete transitive versions are locked in `go.sum`; redistributed terms are
collected in the repository [third-party notices](../../THIRD_PARTY_NOTICES.md).
