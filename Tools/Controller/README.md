# PCController Tool

`controller` is the PC-side utility for ControllerBoardMini. It combines an
authenticated serial console, Charm terminal UI, native protocol client,
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
- Continuous text or newline-delimited JSON monitoring for logging
- Host-side command shell with history, completion, quoting, and raw protocol
  access
- Repeatable batch scripts with variables, sleeps, repeats, and fail-fast or
  continue-on-error behavior
- Cross-platform loopback/stdio JSON-RPC 2.0 IPC; the first TUI/shell process
  owns serial and later CLI processes route through that primary owner
- Importable Go API and optional `c-shared` JSON ABI
- Persistent JSON host configuration, `fsnotify` hot reload, macros, and
  event-driven automations
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
runs `go mod download`, `go test ./...`, and `go vet ./...`, embeds and verifies
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

The historical local PowerShell build scripts were removed to prevent policy
drift; `build.cmd` and `build.sh` share one Node/Controller plan. See the
[project-owned build guide](../Build/README.md) for deterministic identity,
bootstrap requirements, and dry-run/plan commands.

Windows development, deployment, discovery, and programming examples use
`cmd.exe`, the project-owned Controller executable, and native Win32 device
notifications/SetupAPI. They do not invoke PowerShell, WMI, or CIM.

Run the current source without a separate package build:

```console
go run -buildvcs=false ./cmd/controller
go run -buildvcs=false ./cmd/controller ports
```

Manual equivalents:

```console
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
`status` should print the 48-byte telemetry record as named values, including
the captured reset cause and persistent boot count. Its first 43 bytes remain
the legacy telemetry prefix. The stream command enables one periodic status
frame per second. Non-PCController programs that bypass this IPC ownership
must still not open the same COM port concurrently.

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
settings set FLAGS LIGHT ON OFF DISPLAY STATUS PWMBOOT STREAM
  [DEFAULT_PAGE SAVE_LAST [STATUS_COLOR VOLTAGE_DECIMALS CURRENT_DECIMALS]]
event latest
event wait [KIND] [TIMEOUT]
query OPCODE RESPONSE_OPCODE [PAYLOAD_HEX]
write HEX_BYTES
```

`settings` reports the decoded `status_color`, `voltage_decimals`, and
`current_decimals` fields as well as the raw `extended` byte. All update forms
first read the board's current record and replace only the requested fields, so
an older compact `settings set` command cannot erase newer EEPROM options.
Decimal precision zero through two is supported; legacy records whose decimal
bits are all zero intentionally decode as the two-decimal default.

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
pwm mode off|manual|auto
pwm set CHANNEL VALUE
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
melody play NAME [REPEATS]            # background; 1..20 repeats
melody wait NAME [REPEATS]            # wait until all notes are acknowledged/played
melody stop|status
silent status|on|off
display segments|lcd|both DURATION_MS [TEXT]
macro list|play NAME_OR_ID|status|cancel
automation list|run NAME
rf send CODE BITS PROTOCOL [PULSE_US]  # protocol 1..12
rf learn [SECONDS]                     # 1..120
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
disable/break/direction/settle sequencer. Use `relay side ...` for motion
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
remain available for user relays R5-R8. Existing legacy R1-R4 mappings can
still be listed and should be removed or remapped.

Use `pwm mode off` to stop the user-channel commissioning owner. `pwm off`
clears all 16 channels, including enclosure, power, and status RGB, and is
intended as an emergency all-output clear.

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
bin\controller.exe ipc call --method controller.execute --params "{\"command\":\"rf list\"}"
bin\controller.exe ipc call --method controller.execute --params "{\"command\":\"melody play notify\"}"
bin\controller.exe ipc call --method controller.execute --params "{\"command\":\"rgb effect play attention\"}"
```

Use `ipc serve --stdio` for a parent process that wants newline-delimited
JSON-RPC over pipes. Supported methods are `controller.ping`,
`controller.connect`, `controller.close`, `controller.snapshot`,
`controller.status`, `controller.execute`, `controller.rf.list`,
`controller.event.latest`, `controller.event.next`, and
`controller.reset.lines`.
The TCP listener rejects non-loopback addresses.

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

The tool locates `arduino-cli` through `PATH`. For direct AVRDUDE/Urclock
operations it first accepts explicit executable/configuration paths, then
checks `avrdude` on `PATH`, then asks Arduino CLI for
`config get directories.data` and selects the newest MiniCore
`tools/avrdude/*/bin` installation. This also supports portable Arduino CLI
layouts instead of assuming `%LOCALAPPDATA%\Arduino15`. Add `--dry-run` to
resolve and print a workflow without executing it.

From `Tools\Controller`:

```console
bin\controller.exe program --method compile --sketch ..\.. --output-dir ..\..\.build\firmware
bin\controller.exe program --method urclock --operation write-flash --device 1A86:7523 --hex ..\..\.build\firmware\PCController.ino.hex
bin\controller.exe program --method usbasp --operation write-flash --programmer usbasp --usbasp-troubleshooting --hex ..\..\.build\firmware\PCController.ino.with_bootloader.hex
bin\controller.exe boot backup .\backups --device "USB-SERIAL CH340"
```

Direct Arduino upload is disabled. For either permitted flash method,
Controller first attempts a complete timestamped flash/EEPROM backup, then
writes, verifies, releases the programmer, and authenticates the application
again. USBasp remains an explicitly authorized troubleshooting path.

The direct USBasp workflow writes only the selected flash image. It does not
invent a sibling `.eep` filename and does not use the unsafe
`arduino-cli upload --programmer ... --input-file ...with_bootloader.hex`
shortcut.

Inside the TUI/shell, the compact equivalent is:

```text
program urclock ..\..\.build\firmware\PCController.ino.hex COM18
program flash ..\..\.build\firmware\PCController.ino.with_bootloader.hex --usbasp-troubleshooting
boot backup .\backups
```

Each backup creates a new `pccontroller-YYYYMMDD-HHMMSS` directory containing
`flash.hex`, `eeprom.hex`, raw `programmer.txt`, and an atomic
`manifest.json`. The manifest records status, timestamps, method, port, MCU,
programmer, file sizes and SHA-256 hashes, and the application build
hash/date/time when the native application was reachable before entering the
bootloader. A partial read remains marked `incomplete`; it is never reported as
a complete backup.

### Offline development settings migration

The host can convert the validated legacy unversioned 19-byte settings value
plus CRC-8 into the current development-v2 29-byte value plus CRC-8 without
opening a COM port:

```console
bin\controller.exe eeprom migrate --input .\backups\eeprom.hex --output .\settings-development-v2.hex
```

The converter preserves all 19 legacy value bytes, initializes the visible
menu mask to `0x7FFF`, initializes packed menu order to page IDs 0 through 14,
and recalculates the record CRC-8. Its canonical sparse Intel HEX contains
exactly EEPROM addresses `0x0020` through `0x003D` (32 through 61); RF records
starting at 64 and the reset journal starting at 320 are absent and therefore
not part of the migration artifact. A fully valid current record is rejected;
otherwise the CRC-valid legacy prefix is authoritative and ignored stale tail
bytes are not copied. Existing output files are never replaced.

This is deliberately host-side tooling, not AVR compatibility code, so it
consumes no firmware flash. The command only creates a proposed file. Applying
it remains a separate, explicitly confirmed `program --operation write-eeprom`
operation followed by readback verification.

The application protocol and Urboot/AVRDUDE are mutually exclusive users of
the same UART. Programming closes the application session before invoking the
tool, then re-arms connection and requires a fresh authenticated application
HELLO after the programmer exits. Native board commands are true framed
opcodes. Urboot/Urclock is deliberately delegated to MiniCore's current
AVRDUDE `urclock` implementation; the host coordinates boot mode and exclusive
port ownership rather than maintaining a second, potentially drifting
bootloader-protocol implementation.

## WebSocket firmware relay

The local PuzzleBoard `Server.js` and `Client.js` tools formed a Socket.IO file watcher
that transferred an ASCII HEX file and invoked urclock on the receiver. This
tool implements the same workflow as original Go code:

```console
bin\controller.exe ws serve --file ..\..\.build\firmware\PCController.ino.hex --listen 127.0.0.1:3000
bin\controller.exe ws client --url ws://BUILD-PC:3000/firmware --method urclock --port COM18
```

Each versioned JSON message contains the base filename, modification time,
SHA-256, and base64 firmware bytes. The client limits message/file size,
rejects unsafe names or checksum mismatches, writes a temporary `.hex`, runs
the selected programmer, and removes the temporary file.

This is standard WebSocket, not Socket.IO, and is intentionally not
wire-compatible with the old JavaScript event framing. Use the Go server and
Go client together. The default listener is loopback-only; a non-loopback
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
| `go.bug.st/serial` | 1.8.0 | BSD-3-Clause | serial I/O and USB enumeration |

Complete transitive versions are locked in `go.sum`. Detailed reference,
licensing, and non-copying decisions are recorded in
[Upstream Source Audit](docs/Upstream-Source-Audit.md).
