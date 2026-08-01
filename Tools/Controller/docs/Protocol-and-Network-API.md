# Protocol and Network API

The application UART is 115200 baud, 8 data bits, no parity, one stop bit.

## Framing

Frames are COBS encoded and terminated by `0x00`. The decoded frame is:

```text
offset  type          meaning
0       u8            magic = 0xA5
1       u8            advisory envelope revision (currently 1)
2       u8            opcode
3       u8            sequence
4       u8            payload length, 0..48
5       u8[length]    payload
5+len   u8            CRC-8/ATM
```

CRC uses polynomial `0x07`, initial value `0x00`, over every decoded byte
before the CRC. Multi-byte values are little-endian.

The MCU accepts a frame by canonical magic, bounded length, and CRC rather
than requiring its advisory revision byte to equal the local build's value.
Known write operations validate a required semantic payload prefix and ignore
trailing extension fields. Structurally distinct record shapes retain their
shape byte; unknown opcodes receive `Unsupported`. This provides loose
capability-based interoperability without embedding a table of past firmware
versions or development migrations.

The stream decoder bounds incomplete frames, isolates a malformed frame at
the next zero delimiter, and continues decoding later frames.

## Sequence ownership

Host requests use sequence values `1..255`. A direct response (`ACK`, `ERROR`,
`HELLO`, `STATUS`, `SETTINGS`, `PWM_VALUES`, `I2C_RESULT`, or `RF_ENTRIES`) echoes its
request's sequence. Firmware-originated periodic `STATUS` and asynchronous
`EVENT` frames, plus the unsolicited boot `HELLO`, always use sequence `0`;
the host never allocates that value.

The host correlates both sequence and expected response opcode. For `ACK` and
`ERROR`, it also verifies the request opcode carried in the payload. This
prevents an asynchronous or stale frame from satisfying an unrelated
in-flight request even if a malformed firmware build reuses the same nonzero
sequence.

## Identity

Automatic port detection must send `HELLO` and accept only a valid `HELLO`
response with board kind `1` and name `PCController`.

```text
HELLO response:
u8  firmwareMajor
u8  firmwareMinor
u8  firmwarePatch
u8  boardKind                 (1 = PCController)
u32 capabilities
u8  nameLength
u8  name[nameLength]

optional build-identity extension, current schema:
u8  identitySchema = 2
u32 buildHash                 little-endian source hash
u32 buildTimestamp            little-endian packed date<<16|time
```

The current firmware keeps the legacy identity prefix and reports version
`0.0.0`; its appended identity is the authoritative build discriminator.
The timestamp packs `(year-2000)<<9 | month<<5 | day` in the upper word and
`hour<<11 | minute<<5 | second>>1` in the lower word. It has two-second
resolution; the host retains the raw `u32` and renders `YYMMDDHHMMSS`.

Schema `1` remains readable for existing devices and carries `buildHash`,
compiler `__DATE__` (11 bytes), and compiler `__TIME__` (8 bytes). Still older
firmware can omit the extension. VID/PID and name filters only reduce the
candidate set; they do not replace this handshake.

Schema `2` is canonical over HELLO and in the host build/backup manifest; the
current AVR image deliberately does not duplicate it in a magic PROGMEM
record. The named-region HEX patcher must therefore not search for compiler
immediates or claim that identity is safely patchable. Changing firmware
identity requires a rebuild until a linker-declared metadata region can be
added without consuming the ATmega328P safety margin.

## Application/bootloader ownership

This framed opcode protocol exists only while the PCController application is
running. Urboot/Urclock is a separate protocol on the same UART. The host never
mixes the two byte streams: its primary process closes the native session,
delegates Urclock commands to MiniCore's AVRDUDE `urclock` programmer, and
requires a new valid application `HELLO` after AVRDUDE exits.

`boot backup DIR` uses that handoff to read flash and EEPROM and request raw
Urboot/Urclock metadata. It writes SHA-256 and application build identity into
a host-side JSON manifest; backup metadata is not a native opcode and does not
alter firmware EEPROM.

## Opcodes

| Request | Value | Payload |
|---|---:|---|
| HELLO | `01` | none |
| GET_STATUS | `02` | none |
| SET_STREAM | `03` | `u16 period_ms` (`0` disables, otherwise `100..65535`) |
| GET_SETTINGS | `04` | none |
| SET_SETTINGS | `05` | settings schema 1 or 2 record below |
| TEMPERATURE_LIST | `06` | none |
| BUZZER | `10` | `u16 frequency_hz, u16 duration_ms` |
| PWM_SET | `11` | `u8 channel, u16 logical_value` |
| PWM_ALL_OFF | `12` | none |
| PWM_MODE | `13` | `u8 mode` (0 Off, 1 Manual, 2 Auto) |
| STATUS_RGB | `14` | `u8 r, u8 g, u8 b, u8 brightness` |
| PWM_GET | `15` | empty for all values; optional channel reserved |
| ADDRESSABLE_LED | `16` | `u8 pixel, u8 r, u8 g, u8 b, u8 brightness`; pixel `0..10`, or `FF` to fill |
| RF_TX | `20` | `u32 code, u8 bits (1..32), u8 protocol (1..12), u16 pulse_us` |
| RF_LEARN_START | `21` | `u8 timeout_seconds` (1..120) |
| RF_LEARN_CANCEL | `22` | none |
| RF_LEARN_CLEAR | `23` | none |
| RF_LEARN_LIST | `24` | `u8 cursor` |
| RF_LEARN_REMOVE | `25` | `u8 id` |
| RF_MAP | `26` | `u8 id, u8 actionKind, u8 actionValue, u8 behavior` |
| MENU_ACTION | `30` | `u8 action` (Previous/Next/Decrease/Increase = 0..3) |
| RELAY_SET | `31` | `u8 index, u8 active` |
| RELAY_SIDE | `32` | `u8 side, u8 motion` |
| RELAY_ALL_OFF | `33` | none |
| RELAY_TEST | `34` | `u16 step_ms` (`0` stops, otherwise `250..65535`) |
| RESET | `35` | `u8 target` (0 application, 1 bootloader hint) |
| I2C_SCAN | `36` | none |
| MENU_SET_PAGE | `37` | `u8 page` |
| DISPLAY_TEXT | `38` | `u8 target, u16 duration_ms, u8 length, printable ASCII[length]` |
| MACRO_START | `39` | `u8 id, char label[4]` (space padded printable ASCII) |
| MACRO_CANCEL | `3A` | none |
| MACRO_STEP | `3B` | `u8 kind, u8 target, u16 value` |
| MENU_LIST | `3E` | `u8 cursor` |
| PROGRAM_STATE | `45` | semantic prefix `u8 state` (`0` Idle, `1` Running); future tail tolerated |

### Host-streamed notifications

No additional wire opcode is required for configurable PC notifications.
The host's melody scheduler sends one acknowledged `BUZZER` frame, waits for
that note's duration plus its configured gap, then sends the next. The status
animation scheduler sends rate-limited `STATUS_RGB` frames for flash or
breathe effects (no more than 20 requests/second). Consequently these effects
use the normal sequence/ACK/error/disconnect behavior and cannot overflow the
MCU's small buzzer queue.

These effects are intentionally PC-side configuration, not firmware EEPROM
settings. They stop producing future frames if the host is canceled or
disconnected. A buzzer note already accepted by the MCU continues until its
duration expires because protocol version 2 has no dedicated buzzer-stop
opcode.

`RELAY_SIDE` sides are 0 left and 1 right; motion is 0 stop, 1 up, 2 down.
The firmware owns safe disable-before-direction sequencing. Direct
`RELAY_SET` is intended for R5..R8; unsafe R1..R4 combinations may be
rejected.

The current firmware implements both `RESET` targets as the same watchdog
reset. Target `1` does not by itself guarantee that Urboot remains active;
use a DTR/RTS reset pulse or the `urclock` programming workflow for reliable
bootloader entry.

`DISPLAY_TEXT` targets are segments `0`, LCD `1`, and both `2`. Text is at
most 40 bytes; an empty string clears the host override. A duration of zero
keeps the text until it is cleared or replaced.

Macro step kinds are relay `0` (`target 0..7`, value `0..1`), PWM `1`
(`target 0..10`, value `0..4095`), all relays off `2`, all user PWM off `3`,
and finish `4`. Kinds `2..4` require zero target and value. The host streams
the timed steps; the board tracks ownership so cancellation can safely turn
off only outputs claimed by that macro.

| Response/event | Value | Payload |
|---|---:|---|
| ACK | `80` | `u8 requestOpcode, u8 result` (`result=0`) |
| HELLO | `81` | identity above |
| ERROR | `82` | `u8 requestOpcode, u8 errorCode, detail...` |
| STATUS | `90` | fixed status below |
| SETTINGS | `91` | fixed settings below |
| PWM_VALUES | `92` | `u8 mode, u8 selected, 16*u16 values` |
| I2C_RESULT | `93` | `u8 count, count*u8 address` |
| RF_ENTRIES | `94` | paginated learned entries below |
| TEMPERATURES | `95` | named temperature records below |
| MENU_LIST | `97` | paginated firmware-owned menu entries below |
| EVENT | `A0` | `u8 eventType, event-specific data...` |

`MENU_LIST` schema `1` starts with `u8 schema, u8 total, u8 nextCursor,
u8 count`; each entry is `u8 id, u8 mode, char label[4]`. `nextCursor=FF`
ends the list. The host treats this live list as authoritative and enriches it
with its human descriptions; legacy firmware falls back to the host manifest.

### Named temperature records

`TEMPERATURES` uses this schema:

```text
u8 schema = 1
u8 count
count * {
  u8  role                   0=tLED, 1=tBT
  u8  rom[8]                 DS18B20 ROM, wire order
  i16 temperature_centiC
}
```

The maximum native payload currently permits at most four 11-byte entries;
the firmware reports the two assigned roles.

### Learned RF records and mappings

`RF_LEARN_LIST` begins with cursor `0`. Each `RF_ENTRIES` response is:

```text
u8 schema = 1
u8 total
u8 nextCursor              0xFF when complete
u8 count                   currently 0..3
count * {
  u8  id
  u32 code
  u8  bits
  u8  protocol
  u16 pulse_us
  u8  actionKind
  u8  actionValue
  u8  behavior
}
```

Action kinds are none `0`, key `1`, menu `2`, relay `3`, side `4`, and PWM
user channel `5`. Values use zero-based key/menu/relay/side/channel indices.
Behaviors are press/default `0`, toggle `1`, momentary `2`, up `3`, down `4`,
and stop `5`. A remote button is one learned record; its RF code and timing
identity remain stable while its mapping can be changed independently.

The wire format can represent relay indices `0..7`, but the current host API
intentionally refuses new direct learned mappings to indices `0..3` (R1-R4).
Those motion relays must use the firmware's reed-gated `side` action; direct
learned relay actions are limited to user outputs R5-R8. Legacy records remain
visible through `RF_LEARN_LIST` so they can be removed or remapped.

## Event payloads

All asynchronous `EVENT` frames have sequence `0`. The current schemas are:

| Type | Name | Exact payload |
|---:|---|---|
| `1` | key | `u8 type=1, u8 key, u8 gesture, u8 source, u8 sourceId` |
| `2` | door | `u8 type=2, u8 open` |
| `3` | Bluetooth | `u8 type=3, u8 state` |
| `4` | PWM channel | `u8 type=4, u8 channel` |
| `5` | RF learned | `u8 type=5, u8 learnedId` |
| `6` | macro | `u8 type=6, u8 state, u8 macroId` |
| `7` | boot/reset | `u8 type=7, u8 resetCause, u32 resetCount` |
| `8` | RF received | `u8 type=8, u32 code, u8 bits, u8 protocol, u16 pulse_us, u8 learnedId` |
| `9` | RF learning lifecycle | `u8 type=9, u8 state, u8 learnedCount` |
| `10` | relay outputs | `u8 type=10, u8 activeMask` |

RF-learning lifecycle states are ended `0`, cancelled `1`, storage full `2`,
and started `3`. Relay events update host state immediately without waiting for
the next telemetry frame.

The key payload is exactly five bytes in current firmware. Keys are zero-based
`0..3`. Gestures are click `0`, double-click `1`, hold-start `2`,
hold-repeat `3`, hold-release `4`, down `5`, and up `6`. Sources are physical
`0`, RF `1`, and host `2`; `sourceId` is `FF` when there is no learned RF
source.

The RF-received payload is exactly ten bytes. `learnedId` is `FF` for an
unlearned signal. Macro states are started `1`, step `2`, cancelled `3`, and
completed `4`.

The boot/reset payload is exactly six bytes:

```text
[07, resetCause, resetCount byte0, byte1, byte2, byte3]
```

`resetCount` is the MCU-owned, EEPROM-persisted boot count. `resetCause` is
the AVR `MCUSR` bit mask captured before ordinary C++ initialization. The same
values are appended to every `STATUS`, allowing a host that missed the startup
event to recover the latest reset information.

## Settings payload

The 10-byte form is the common semantic core. The 12-byte form appends default
page and menu-presentation flags. Current firmware applies fields by presence,
not by a table keyed to the first byte, and safely retains its current values
for an absent optional tail. The host emits the complete 12-byte form.

```text
10-byte common core:
u8  schema = 1
u8  flags                   bit0 silent; bit1 reserved; bit2 swap tLED/tBT;
                            bits3-4 motion-door policy; bit5 door audio off;
                            bit6 relay audio off; bit7 100 ms motion break
u8  lightMode
u8  onBrightness
u8  offBrightness
u8  displayBrightness       0..7
u8  statusBrightness
u8  pwmBootMode
u16 streamPeriod_ms          0, or at least 100

12-byte complete record:
u8  schema = 2
u8  flags
u8  lightMode
u8  onBrightness
u8  offBrightness
u8  displayBrightness        0..7
u8  statusBrightness
u8  pwmBootMode              0..2
u16 streamPeriod_ms          0, or at least 100
u8  defaultPage              0..13
u8  extendedFlags            bit0 save last page
```

## Status payload

Current `STATUS` is 48 bytes. Bytes `0..42` are the unchanged legacy 43-byte
record, so older readers can still decode their known prefix. The reset fields
are appended at byte 43:

```text
offset  type  field
0       u32   uptime_ms
4       i32   supply_mV
8       i32   bus_mV
12      i32   current_mA
16      i32   power_mW
20      i16   temperature1_centiC
22      i16   temperature2_centiC
24      u16   flags
26      u8    rawInputs
27      u8    activeKeys
28      u8    activeRelays
29      u8    menuPage
30      u8    programMode
31      u8    doorOpen
32      u8    bluetoothState
33      u8    pwmMode
34      u8    pwmChannel
35      u16   pwmValue
37      u8    lcdAddress
38      u8    pwmErrors
39      u16   framingErrors
41      u16   crcErrors
43      u8    resetCause
44      u32   resetCount
```

All offsets are byte offsets and the final `u32` occupies the half-open byte
range `[44:48]` (bytes `44..47`).

Readers should require at least the 43-byte legacy prefix, decode reset
telemetry when at least 48 bytes are present, and tolerate future appended
fields.

Status flag bit 13 reports the host-owned Running state, bit 14 reports that
the firmware considers the host offline, and bit 15 reports its hot-temperature
condition. The Go decoder exposes these as `program_running`, `host_offline`,
and `hot` in addition to preserving the raw flags. HELLO capability bit 24
advertises `PROGRAM_STATE`; the host sends the current state immediately after
each authenticated connection/reconnection and after every state change. It
also reasserts that semantic byte every two seconds without requesting status,
so firmware's five-second host-presence watchdog stays truthful even when no
telemetry view is subscribed. It does not probe older firmware that omits the
capability.

## Host service architecture

The UART protocol above is the MCU wire format. Network clients normally talk
to the Go host instead of reproducing serial ownership, discovery, retries, and
Urclock handoff. The first long-running host is the **primary** process and is
the only process that opens the board. Secondary TUI, CLI, RPC, REST,
WebSocket, and Socket.IO clients send requests to that primary.

PC operating-system actions use this ordinary authenticated command surface;
they are not native AVR opcodes. `os brightness get|set VALUE` reads/writes the
primary display through Win32 DDC/CI, while `os sleep`, `os hibernate`,
`os restart`, and related commands use the guarded power executor. JSON-RPC,
REST, WebSocket, Socket.IO, IPC, TUI, and hosted front-panel callers therefore
share one current file-watched policy and one audit-event path. Brightness and
power writes are disabled by default, and a DDC/CI-unsupported display returns
a capability error rather than falling back to an untracked shell command.

The default host endpoint is `127.0.0.1:8787`. A single TCP listener
multiplexes newline-delimited JSON-RPC and HTTP by inspecting the first request
bytes. HTTP then serves REST, standard WebSocket, and Socket.IO paths. Closing
the serial port does not stop this service; closing the service does not erase
MCU EEPROM or the PC configuration. JSON-RPC uses protocol `2.0`; the durable
application contract is API version `1`, returned by `controller.ping` and
`GET /healthz` and represented by the `/api/v1/` route prefix.

### Authentication and exposure

Loopback-only use is the safe default. When `ipc.allow_remote` is false, a
non-loopback bind is rejected. Remote access requires all of the following:

- `ipc.allow_remote: true`;
- an `ipc.auth_token` containing at least 24 characters;
- an explicit, non-wildcard `ipc.allowed_origins` list for browser WebSocket
  clients;
- the relevant `ipc.remote_policy` capability set to `true`.

Raw JSON-RPC carries the token in the top-level `auth` member. HTTP accepts
`Authorization: Bearer TOKEN` or `X-PCController-Token: TOKEN`. WebSocket
handshake query `access_token=TOKEN` is available for browser APIs that cannot
set a header, but headers are preferred because URLs may be logged. Token
comparison is constant-time. Discovery advertisements never contain the
token.

Authentication proves possession of the host token; authorization is separate.
The default remote policy permits only `read` and `events`. Messages, board
commands, host-configuration writes, connection control, reset, programming,
shutdown, virtual keys, power/display actions, host-automation execution,
integration access, and bridge calls are independent
opt-ins. Programming additionally requires connection control. A remote
`controller.execute` request is classified before execution, so using the
generic command surface cannot bypass the reset, programming, OS-action, or
bridge-call gates. Authorized mutating attempts and denied attempts publish
`security.remote.authorized` or `security.remote.denied` timeline events
without recording the token or request payload.

Motion policies, door checks, relay sequencing, bounds, OS confirmation rules,
and exclusive programming ownership still apply after authorization. Use TLS
termination, a VPN, or an SSH tunnel on untrusted networks; the built-in
listener does not itself terminate TLS.

```json
"ipc": {
  "allow_remote": true,
  "auth_token": "replace-with-at-least-24-characters",
  "allowed_origins": ["controller.example:*"],
  "remote_policy": {
    "read": true,
    "events": true,
    "messages": false,
    "board_commands": false,
    "host_configuration": false,
    "connection_control": false,
    "reset": false,
    "programming": false,
    "shutdown": false,
    "virtual_keys": false,
    "power_actions": false,
    "host_automations": false,
    "bridge_calls": false,
    "integrations": false
  }
}
```

## JSON-RPC 2.0

The raw transport contains one UTF-8 JSON object per line. HTTP and WebSocket
use the same request and response model:

```json
{"jsonrpc":"2.0","id":1,"method":"controller.status","params":{},"auth":"..."}
```

```json
{"jsonrpc":"2.0","id":1,"result":{"uptime_ms":1234}}
```

An omitted `id` is a notification and receives no direct response. Errors use
the standard `error.code`/`error.message` object. The primary serializes
mutating RPC calls through the same controller client while snapshot and event
subscriptions use cached/thread-safe paths.

Standard codes are parse error `-32700`, invalid request `-32600`, method not
found `-32601`, and invalid params `-32602`. Host extensions use authentication
required `-32001`, remote capability denied `-32003`, and runtime/device error
`-32000`. The request ID is preserved on every parsed request error.

### RPC methods

| Method | Parameters | Result/behavior |
|---|---|---|
| `controller.ping` | `{}` | service health, JSON-RPC version, and API version |
| `controller.connect`, `controller.open`, `controller.port.open` | optional `port` | resume discovery or open the requested transport, then authenticate `HELLO` |
| `controller.close`, `controller.port.close` | `{}` | close UART and pause automatic reconnect |
| `controller.reset`, `controller.reset.lines`, `controller.port.reset` | optional `pulse_ms` | one explicit DTR-only pulse, then fresh application authentication |
| `controller.snapshot` | `{}` | cached connection, identity, status, and settings |
| `controller.command.catalog` | `{}` | machine-readable registered command names, aliases, usage, summary, and task group |
| `controller.status` | `{}` | fresh board status |
| `controller.temperatures` | optional `rescan` | named temperatures and ROM identities |
| `controller.menu.list`, `controller.menu.current` | `{}` | live board catalog when supported, otherwise the versioned host fallback |
| `controller.menu.jump`, `controller.menu.page` | `page` ID or name | select a board menu page |
| `controller.command.execute`, `controller.execute` | `command` | run any ordinary controller command; `quit`/`exit` requests primary shutdown |
| `controller.program_state.get` | `{}` | current host-owned Idle/Running owners, reason, and revision |
| `controller.program_state.set` | `mode`, optional `owner`, `reason` | set/clear one host-owned Running claim and mirror it to capable firmware |
| `controller.rf.list` | `{}` | all learned records |
| `controller.rf.learn.start` | `timeout_ms`, `indefinite`, `multiple` | start finite/indefinite, single/multi learning |
| `controller.rf.learn.status` | `{}` | current learn-session state |
| `controller.rf.learn.cancel` | `{}` | stop learning and emit an explicit end event |
| `controller.event.latest` | `{}` | latest monotonically increasing host event ID |
| `controller.event.next` | `after_id`, optional `kind`, `timeout_ms` | long-poll the next event without status polling |
| `controller.history.status` | optional ISO-8601 `since` | retained measurement samples |
| `controller.history.timeline` | optional `since`, `limit` | durable important-event timeline |
| `controller.lcd.presentation.status` | `{}` | prompt/priority presentation state |
| `controller.lcd.presentation.configure` | `enabled`, `debounce_ms`, `priority_hold_ms` | configure host LCD presentation |
| `controller.lcd.prompt` | `line1`, `line2` | queue a debounced prompt mirror |
| `controller.lcd.priority` | `kind`, `line1`, `line2`, optional `hold_ms` | display a priority overlay, then restore the prompt |
| `controller.message.send` | typed message envelope below | route/log a message and optionally display it on the board LCD |
| `controller.bridge.list` | `{}` | configured peers and live connection state, without URLs or credentials |
| `controller.bridge.call` | `peer`, nested JSON-RPC `request` | correlated call through that peer; recursive bridge calls are rejected |
| `controller.device.status` | `{}` | local-device HTTP/event-stream health and last confirmed power state |
| `controller.device.action` | `action`, optional `text` or `count` | bounded Local Device v1 power, display, alert, or passive-refresh action |
| `controller.device.inspect` | `resource` | sanitized `capabilities` or `snapshot` document only |
| `controller.integrations.local.get` | `{}` | credential-free local-device and data-hub enable/URL settings |
| `controller.integrations.local.set` | `local_device`, `data_hub` | validate and persist LAN-only device and loopback-only data roots |
| `controller.ports` | `{}` | current serial devices with stable identity fields |
| `controller.quit`, `controller.exit` | `{}` | close the primary and emit lifecycle shutdown |

Programming, macros, relays, PWM, RF transmit, settings, display, RGB, buzzer,
and I2C operations remain available through `controller.execute`, so API
clients do not need a second less-safe command implementation. The response is
correlated to its JSON-RPC ID and the inner native command remains correlated
to its UART sequence.

## REST and inbound web service

All JSON endpoints share the IPC listener:

| Method and path | Purpose |
|---|---|
| `GET /healthz` | unauthenticated liveness and service identity; no board data |
| `GET /api/v1/ui-config` | unauthenticated non-secret browser bootstrap contract |
| `POST /api/v1/rpc` | one JSON-RPC request |
| `GET /api/v1/snapshot` | cached controller snapshot |
| `GET /api/v1/commands` | machine-readable shared command catalog |
| `GET /api/v1/program-state` | current host-owned Idle/Running state |
| `PUT` or `POST /api/v1/program-state` | update `owner`, `mode`, and optional `reason` |
| `POST /api/v1/command` | `{"command":"..."}` through the ordinary command engine |
| `POST /api/v1/messages` | typed message envelope |
| `GET /api/v1/bridges` | configured peer names/protocols and live state |
| `POST /api/v1/bridges/call` | `peer` plus a nested JSON-RPC `request` |
| `POST /api/v1/webhooks/inbound` | typed incoming message when inbound webhooks are enabled |
| `/api/v1/integrations/datahub/*` | authenticated service-neutral streaming bridge to the configured loopback data service |
| `/api/v1/integrations/device/*` | always fails closed; device operations require typed RPC |
| `POST /ipc` | JSON-RPC compatibility on the configured WebSocket path |

All data/API routes except `/healthz` and the non-secret `/api/v1/ui-config`
bootstrap apply host authentication. Bodies are limited to
1 MiB. Unsupported methods receive `405`; the inbound webhook path is `404`
when disabled. An inbound webhook is data, not an implicit shell command. To
make it actionable, enable a narrow text mapping whose resulting command still
passes the normal safety path.

### Embedded browser application and exact byte serving

The production React application is compiled into the Go executable and served
from the same listener at `/`; no adjacent asset directory is required at
runtime. Hashed assets use immutable caching while the application shell uses
revalidation. Static responses support `GET`, `HEAD`, strong ETags,
`If-None-Match`, `If-Range`, closed/open/suffix byte ranges, multipart ranges,
exact `Content-Length`/`Content-Range`, and `416` responses. API, WebSocket,
Socket.IO, and health paths remain reserved and cannot fall through to the SPA
shell. Canonical path validation rejects traversal and encoded separator/dot
variants before opening an embedded file.

`controller web` starts the same primary host services used by the TUI but does
not construct a terminal model or read terminal input. It owns discovery,
serial, automations, global hotkeys, integrations, and the command dispatcher
for its whole lifetime; `--no-open` serves the application without launching a
browser. The UI sends terminal commands through same-origin typed RPC and keeps
a persistent subscription for status, board/host events, and global
`app.page` actions. A TUI is therefore not required in web mode. When the TUI
is the primary instead, the same action broker retains bounded TUI delivery and
also mirrors each valid page action into cursor-based browser event history.

The local-integration proxy resolves only the configured short names; request
data can never supply an upstream URL. The data hub is restricted to loopback,
while the typed device manager accepts only loopback/private/link-local addresses or explicitly local names. The bridge
streams HTTP bodies and WebSocket upgrades without buffering full exports,
preserves upstream range/ETag semantics, disables environment proxies, and
removes PCController authorization, cookies, forwarding headers, and
`access_token` before forwarding. Upstream `Set-Cookie` is also removed so a
companion service cannot set cookies on the controller origin.

## Standard WebSocket

Connect to the configured `ipc.websocket_path` (default `/ipc`). After the
authenticated HTTP upgrade, send ordinary JSON-RPC request objects. Two
connection-local control methods manage push data:

```json
{"jsonrpc":"2.0","id":1,"method":"controller.subscribe","params":{"topics":["events","status"],"interval_ms":200,"after_id":0}}
```

```json
{"jsonrpc":"2.0","id":2,"method":"controller.unsubscribe","params":{}}
```

Topics are `events` and `status` (`telemetry` is accepted as an alias for
`status`). Status interval is 50..60000 ms and defaults to 200 ms. Event
delivery starts after `after_id`; zero starts at the current tail and does not
replay the whole timeline. Push messages are JSON-RPC notifications:

```json
{"jsonrpc":"2.0","method":"controller.event","params":{}}
{"jsonrpc":"2.0","method":"controller.status","params":{}}
{"jsonrpc":"2.0","method":"controller.error","params":{}}
```

A status subscription participates in demand accounting. UART stays open after
unsubscribe, but periodic status polling stops when no UI, script, automation,
IPC, WebSocket, or bridge consumer requires it. Asynchronous board events keep
flowing because they do not require polling.

## Socket.IO compatibility

Socket.IO uses a distinct configured path, default `/socket.io/`. The bounded
adapter is genuine Engine.IO v4 / Socket.IO framing over WebSocket:

```text
ws://HOST:PORT/socket.io/?EIO=4&transport=websocket
```

It sends an Engine.IO open packet, accepts Socket.IO connect/disconnect, and
implements Engine.IO ping/pong. Socket.IO event packets use the usual
`42["name",payload]` form. Supported incoming events are:

| Event | Payload | Response/push events |
|---|---|---|
| `subscribe` | WebSocket subscription object | `subscribed`, then `controller.event`, `controller.status`, or `controller.error` |
| `unsubscribe` | `{}` | `unsubscribed` |
| `message` | typed message envelope | `message.accepted` or `error` |
| `command` | `{"command":"..."}` | `command.response` |
| `rpc` | JSON-RPC request | `rpc.response` |

This adapter deliberately does **not** implement Engine.IO long-polling,
namespaces, rooms, binary attachments, acknowledgement callbacks, or a general
Socket.IO cluster. Clients must force WebSocket transport. The standard `/ipc`
endpoint remains the preferred full JSON-RPC API.

## Outbound webhooks

Each enabled host webhook has a name, event-kind filter, URL, method, optional
headers/body template, and timeout. Methods are GET, POST, PUT, PATCH, and
DELETE. A suffix `*` in the event-kind filter matches a prefix; empty or `*`
matches all events.

GET and DELETE add `id`, `kind`, and `text` query parameters. Body-bearing
methods send the event JSON by default or replace bounded `{{id}}`, `{{kind}}`,
`{{text}}`, and `{{source}}` tokens in the configured template. Only 2xx is
success. Response draining is bounded, concurrent deliveries are capped, and
`webhook.*` events are not sent back through webhooks, preventing a direct
loop. Delivery success and errors are emitted to the host timeline.

## Outbound WebSocket bridge

An enabled `integrations.websocket_clients` entry makes the primary host a
standard WebSocket or bounded Socket.IO client. It authenticates with a Bearer
token, subscribes to validated `events`/`status` topics, reconnects with bounded
backoff, and can forward local events as correlated `controller.message.send`
calls. Transport/control/error events and remote-origin messages are not
re-forwarded, preventing a direct two-host echo loop. Incoming remote
events/status are re-emitted locally as source-tagged messages.

`controller.bridge.call`, `POST /api/v1/bridges/call`, and
`bridge call PEER METHOD [PARAMS_JSON]` use the existing persistent connection
and an internal wire ID, then restore the caller's nested JSON-RPC ID in the
response. The target host applies its own token, remote capability policy, and
ordinary safety path. Recursive bridge calls are rejected, so this API is not
an unrestricted network pivot. Incoming command requests on an outbound peer
connection additionally require that peer's `allow_commands` flag.

The bridge does not open another host's COM port. Each instance retains one
local serial owner. Programming through a remote primary requires the target's
`programming` and `connection_control` permissions and follows the same
application-UART close, guarded toolchain/Urclock run, and fresh `HELLO`
recovery as local programming.

## mDNS and SSDP discovery

When enabled, mDNS advertises `_pccontroller._tcp.local.` with non-secret TXT
metadata for the WebSocket path, Socket.IO path, and authentication requirement.
SSDP advertises and discovers
`urn:pccontroller-org:service:bridge:1`, responds to its own type and
`ssdp:all` searches, sends alive/byebye notifications, and publishes a
`/healthz` location. Instance discovery returns protocol, name, host, port,
addresses/TXT or SSDP location/USN, and observation time.

Discovery is optional because multicast can be blocked by Windows Firewall,
VLANs, VPNs, or Wi-Fi isolation. Finding an instance does not authenticate the
client and never grants command authority.

## Typed text-message envelope

HTTP, JSON-RPC, WebSocket, Socket.IO, peer bridges, and the LCD presenter use
one schema:

```json
{
  "source": "client",
  "target": "lcd",
  "type": "operator.notice",
  "text": "Service required",
  "line1": "SERVICE",
  "line2": "REQUIRED",
  "action": "open-events"
}
```

Allowed sources are `client`, `server`, `bridge`, `board`, `lcd`, `host`,
`ipc`, `webhook`, and `websocket`. Targets are `client`, `server`, `bridge`,
`board`, `lcd`, `host`, and `all`. `type` contains 1..32 lowercase letters,
digits, dot, dash, or underscore. Text/action lengths are bounded. A board/LCD
target is converted to two printable 16-byte rows and sent through
`DISPLAY_TEXT`; every accepted message is also a source-tagged host event.

Network ingress does not trust a payload's claimed source. REST/raw IPC is
tagged `ipc`, WebSocket/Socket.IO is tagged `websocket`, peer traffic is tagged
`bridge`, and inbound HTTP hooks are tagged `webhook`; a different claimed
value is retained only as bounded `metadata.claimed_source`. This prevents a
remote message from impersonating a physical `board` event in text mappings.

`action` is descriptive metadata. It is never executed automatically. A
deliberately enabled host `text_mappings` rule can match source, target, type,
and text content and then submit a fixed configured command. This separation
prevents received text from becoming shell input and retains authentication,
logging, motion policy, and board safety.

## Host configuration and USB lifecycle

JSON, YAML, and TOML use one semantic schema selected by file extension.
Unknown future keys are ignored so an older host can continue with the fields
it understands; known-field type errors, multiple YAML documents, invalid
ranges, and unsafe remote combinations are rejected. Long-running processes watch atomic replacements and keep the
last known-good configuration if a reload is invalid. This file is PC-owned;
it never replaces the board's CRC-checked EEPROM record.

Authentication tokens, origin lists, remote policy, webhook enablement,
outbound hooks, and bridge clients are read from the current watched host
configuration. Listener address and URL-path topology are established when the
primary starts and require a host restart to move the bound socket/path; policy
or token changes take effect without touching MCU EEPROM.

### Verification boundary

Automated tests cover the multiplexed raw/HTTP/WebSocket listener, Bearer and
header authentication, remote capability denial, route-derived message
provenance, REST, Engine.IO v4/Socket.IO framing, correlated host-to-host calls,
forwarded event messages, discovery packet parsing, and secret-free SSDP
advertisements. They use in-process loopback servers and never open a serial
port. Actual mDNS/SSDP visibility across Windows Firewall/VLAN boundaries,
TLS reverse-proxy commissioning, and remote flash against physical hardware
remain deployment tests rather than claims made by this unit suite.

The Windows serial mode starts both DTR and RTS inactive. Merely opening the
application therefore does not request reset. `connection.reset_on_reconnect`
defaults false and, when enabled, permits exactly one DTR-only pulse for a
genuine physical reconnect epoch. Native Windows Plug-and-Play registry change
notifications emit disconnect/reconnecting/connected lifecycle events; a
safety retry is used only if platform notification cannot be established.

See [Host Configuration and Integrations](../../../docs/Host-Configuration-and-Integrations.md)
for configuration examples, TUI surfaces, and commissioning guidance.
See [Control-Surface Capability Matrix](Control-Surface-Capability-Matrix.md)
for the per-domain CLI, library, IPC, REST, WebSocket, event, and authorization
reachability contract.
