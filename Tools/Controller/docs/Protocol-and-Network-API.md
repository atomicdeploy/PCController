<div align="center"><a href="../../../README.md"><img src="../../../docs/assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Protocol and Network API

The application UART is 115200 baud, 8 data bits, no parity, one stop bit.

Machine consumers can use the generated [OpenAPI 3.1](../api/openapi.json),
[AsyncAPI 3.0](../api/asyncapi.json), and
[JSON-RPC schema/catalog](../api/jsonrpc.schema.json). The standalone
[offline API reference](../api/reference.html) renders the same contracts
without a documentation server. Repository validation regenerates the
contracts logically and rejects drift from the actual RPC dispatcher or REST
route families.

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
request's sequence. Firmware-originated periodic `STATUS`, asynchronous
`EVENT`, `SEGMENT_CHANGED`, and `BUZZER_CHANGED` frames, plus the unsolicited
boot `HELLO`, always use sequence `0`;
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
u8  identitySchema = 3
u8  boardKind                 (1 = PCController)
u32 capabilities
u32 buildHash                 little-endian source hash
u32 buildTimestamp            little-endian packed date<<16|time
```

The 14-byte compact identity is mandatory. The board kind supplies the stable
technical identity; firmware identity is hash/time based rather than a
semantic version.
The timestamp packs `(year-2000)<<9 | month<<5 | day` in the upper word and
`hour<<11 | minute<<5 | second>>1` in the lower word. It has two-second
resolution; the host retains the raw `u32` and renders `YYMMDDHHMMSS`.

The current AVR image deliberately does not duplicate identity in a magic PROGMEM
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
| SET_SETTINGS | `05` | exact 15-byte settings shape 3 record below |
| TEMPERATURE_LIST | `06` | none |
| BUZZER | `10` | `u16 frequency_hz, u16 duration_ms` |
| PWM_SET | `11` | `u8 channel, u16 logical_value` |
| PWM_ALL_OFF | `12` | none |
| — | `13` | reserved; senders must not use this value |
| STATUS_RGB | `14` | `u8 r, u8 g, u8 b, u8 brightness` |
| PWM_GET | `15` | none; returns availability and all sixteen values |
| ADDRESSABLE_LED | `16` | `u8 pixel, u8 r, u8 g, u8 b, u8 brightness`; pixel `0..10`, or `FF` to fill |
| RF_TX | `20` | `u32 code, u8 bits (1..32), u8 protocol (1..12), u16 pulse_us` |
| RF_LEARN_START | `21` | `u8 timeout_seconds` (1..120) |
| RF_LEARN_CANCEL | `22` | none |
| RF_LEARN_CLEAR | `23` | none |
| RF_LEARN_LIST | `24` | `u8 cursor` |
| RF_LEARN_REMOVE | `25` | `u8 id` |
| MENU_ACTION | `30` | `u8 action` (Previous/Next/Decrease/Increase = 0..3) |
| RELAY_SET | `31` | `u8 index, u8 active` |
| RELAY_SIDE | `32` | `u8 side, u8 motion` |
| RELAY_ALL_OFF | `33` | none |
| RELAY_TEST | `34` | `u16 step_ms` (`0` stops, otherwise `250..65535`) |
| RESET | `35` | `u8 target` (0 application, 1 bootloader hint) |
| I2C_TRANSFER | `36` | bounded generic I2C transaction record |
| MENU_SET_PAGE | `37` | `u8 page` |
| DISPLAY_TEXT | `38` | legacy targets `0..4` below, or scheduled-segment target `5` |
| MACRO_START | `39` | `u8 id, char label[4]` (space padded printable ASCII) |
| MACRO_CANCEL | `3A` | none |
| MACRO_STEP | `3B` | `u8 kind, u8 target, u16 value` |
| FRONT_PANEL_GET | `3C` | none |
| REMOTE_KEY_GESTURE | `3D` | `u8 action, u8 keyEvent` |
| MENU_LIST | `3E` | `u8 cursor` |
| RF_LEARN_REPLACE | `3F` | one complete 12-byte learned-RF record |
| MENU_LAYOUT_GET | `40` | none |
| MENU_LAYOUT_SET | `41` | exact current layout record |
| PROGRAM_STATE | `45` | semantic prefix `u8 state` (`0` Idle, `1` Running); future tail tolerated |

Value `26` is reserved: RF mapping changes replace one complete learned record
through `RF_LEARN_REPLACE`. `SET_STREAM` remains the immediate stream-cadence
operation, while `MENU_ACTION` remains the compact stateless navigation path;
`REMOTE_KEY_GESTURE` is the distinct stateful down/hold/up input path. A
`Down` performs its primary action in the receive pass, `HoldRepeat` repeats
it, and `Up` releases momentary output; `Click` and `HoldStart` are lifecycle
classification only. Clients must not wait out a click/double-click timer
before sending `Down`.

### Mirrored display and buzzer output

#### Push-first state rule

Active physical output state is push-first. A UI, bridge, or integration must
not create a repeating read timer merely because a snapshot opcode exists.
Firmware emits a changed-only sequence-zero opcode, the Go runtime updates its
authoritative cache immediately, and that one event fans out over IPC,
WebSocket, Socket.IO, peer bridges, webhooks, TUI, and WebUI. Snapshot reads are
reserved for initial connection, an explicit user refresh, recovery after a
detected event gap, or a clearly labelled bounded legacy fallback. A fallback
must stop as soon as push capability is present and must never shadow the
normal low-latency path.

The board pushes changed physical outputs instead of requiring the host to poll
the active front panel. `SEGMENT_CHANGED` (`9C`) carries four raw TM1637 segment
bytes followed by `u8 brightness`. `BUZZER_CHANGED` (`9D`) carries
`u16 frequency_hz, u16 duration_ms, u8 muted`. Both use sequence zero and are
emitted only when the corresponding physical output changes. The host may
still request `FRONT_PANEL_GET` during connection, manual refresh, or recovery.

The host's melody scheduler sends acknowledged `BUZZER` frames. Each accepted
firmware note is mirrored through `BUZZER_CHANGED`, then published immediately
through IPC, WebSocket, Socket.IO, bridge peers, optional native WinRing0
motherboard-speaker playback, and optional Web Audio. Current firmware receives
one compact `STATUS_EFFECT` descriptor and renders the animation locally; the
rate-limited `STATUS_RGB` stream remains only as a bounded older-firmware
compatibility path.

At the command/API layer a melody repeat count of zero means repeat until an
explicit stop, while 1..20 remains the bounded mode. This is the reusable
continuous `WAIT`/attention-ringtone path; it does not change the `BUZZER`
wire payload.

These effects are intentionally PC-side configuration, not firmware EEPROM
settings. They stop producing future frames if the host is canceled or
disconnected. A buzzer note already accepted by the MCU continues until its
duration expires because there is no dedicated buzzer-stop opcode.

`RELAY_SIDE` sides are 0 left and 1 right; motion is 0 stop, 1 up, 2 down.
The firmware owns safe disable-before-direction sequencing. Direct
`RELAY_SET` is intended for R5..R8; unsafe R1..R4 combinations may be
rejected.

The current firmware implements both `RESET` targets as the same watchdog
reset. Target `1` does not by itself guarantee that Urboot remains active;
use a DTR/RTS reset pulse or the `urclock` programming workflow for reliable
bootloader entry.

Legacy `DISPLAY_TEXT` targets are segments `0`, LCD `1`, both `2`, captured
front panel `3`, and release `4`. Scheduled segment target `5` uses:

```text
u8  target = 5
u16 speed_ms              0 selects 260 ms; scrolling clamps to at least 80 ms
u8  text_length           0..40
u8  options               bits 0..1 repeat: 0 once, 1 loop, 2 interval
                          bit 7 force marquee; all other input bits reserved
u16 hold_ms               static/once visible duration
u8  interval_seconds      1..255 when repeat=interval
char text[text_length]    printable ASCII
```

Text longer than four cells scrolls automatically; bit 7 explicitly scrolls a
shorter message. A marquee walks into a completely empty four-cell frame after
its last character. Only after that blank frame does it stop, restart, or
yield to the local page for its interval. Empty text clears the scheduled host
overlay. The Go API defaults to one bounded presentation; the configured
automatic presenter uses interval mode at roughly two presentations per minute
instead of looping continuously.

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
| PWM_VALUES | `92` | `u8 available, u8 selected, 16*u16 values` |
| I2C_RESULT | `93` | `u8 count, count*u8 address` |
| RF_ENTRIES | `94` | paginated learned entries below |
| TEMPERATURES | `95` | named temperature records below |
| MENU_LIST | `97` | paginated firmware-owned menu entries below |
| SEGMENT_CHANGED | `9C` | `u8 raw_segments[4], u8 brightness`; unsolicited, changed-only |
| BUZZER_CHANGED | `9D` | `u16 frequency_hz, u16 duration_ms, u8 muted`; unsolicited, changed-only |
| EVENT | `A0` | `u8 eventType, event-specific data...` |

`MENU_LIST` schema `1` starts with `u8 schema, u8 total, u8 nextCursor,
u8 count`; each entry is `u8 id, u8 mode, char label[4]`. `nextCursor=FF`
ends the list. The host treats this live list as authoritative and enriches it
with its human descriptions; a board without the optional directory capability
uses the current host manifest without introducing a second wire schema.

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
learned relay actions are limited to user outputs R5-R8. Existing direct records remain
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
| `9` | RF learning lifecycle | `u8 type=9, u8 state, u8 learnedCount, u8 mode, u8 totalSeconds, u8 remainingSeconds` |
| `10` | relay outputs | `u8 type=10, u8 activeMask` |
| `11` | alert transition | `u8 type=11, u8 kind, u8 active` |

RF-learning lifecycle states are ended `0`, cancelled `1`, storage full `2`,
started `3`, and timer progress `4`; modes are indefinite `0` and timer `1`.
Alert kinds are firmware fault `1` and HOT temperature `2`. Relay and alert
events update host state immediately without waiting for the next telemetry
frame; measurements remain in `STATUS`.

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

Current firmware and host exchange one exact 15-byte shape. Unsupported
records, unknown shapes, and trailing bytes are rejected.

```text
offset  type  field
0       u8    schema = 3
1       u8    flags                 bit0 silent; bit1 ProgrammingMode;
                                    bit2 swap tLED/tBT; bits3-4 motion-door
                                    policy; bit5 door audio off; bit6 relay
                                    audio off; bit7 reserved and cleared on apply
2       u8    lightMode
3       u8    onBrightness
4       u8    offBrightness
5       u8    displayBrightness     0..7
6       u8    statusBrightness
7       u8    outputPersistence     bit0 restore motion; bit1 restore user
                                    relays; bit2 restore stored user PWM;
                                    bit3 retain direction relay on stop
8       u16   streamPeriod_ms       0, or at least 100
10      u8    defaultPage           0..13
11      u8    extendedFlags         bit0 save last page
12      u8    displayOptions        bits0-2 closed TM1637 brightness 0..7;
                                    bits3-7 motion exit hold seconds 1..31;
                                    encoded zero selects the 2-second default
13      u8    relayRestoreMask      last R1..R8 state; restore policy gates use
14      u8    motionBreakMs         exact disable-to-direction delay, 1..255 ms
```

PWM control is exclusively direct: `PWM_SET` writes one logical channel value
from `0..4095`, `PWM_GET` reports availability plus all sixteen values, and
`PWM_ALL_OFF` clears every channel. Scheduled behavior belongs in host macros
or automations rather than firmware state. Channels 0..7 also update the
EEPROM-backed user values used when output-persistence bit 2 is enabled.
`relayRestoreMask` is continuously updated from the applied relay state, but
motion and user-relay subsets are restored only when their respective policy
bits are enabled. The programming latch always prevents output restoration.

## Status payload

Current `STATUS` requires the complete 48-byte record:

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
33      u8    pwmAvailable
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

Readers require at least 48 bytes, always decode reset telemetry, and tolerate
future appended fields after the canonical record.

The UART record carries only the millisecond counter. Every JSON status shape
(snapshot, history, RPC, REST, WebSocket, and scripting output) retains
`uptime_ms` as that stable machine value and also includes a derived `uptime`
string such as `1h13m12.21s`. Consumers should calculate with `uptime_ms` and
may present `uptime` directly; the derived string is never written to the MCU
or added to the compact UART payload.

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
MCU EEPROM or the PC configuration. JSON-RPC uses protocol `2.0`; schema
negotiation reports JSON-RPC `2.0` only because that standards-defined marker
is required by the wire format. Canonical REST URLs live directly under
`/api/`; product-version prefixes such as `/api/v1/` are unsupported and
rejected. JSON-RPC and WebSocket peers remain capability- and semantics-driven so different feature sets can
still interoperate.

### Immediate-alpha exposure

Issue #148 is the active contract: application authentication and
authorization are disabled across raw IPC, HTTP/REST, standard WebSocket,
Socket.IO, browser UI configuration, and peer bridges. Product entry points
report `auth_required: false`; inbound bearer references are neither resolved
nor injected, and configured principals, Origin rules, session tickets, and
`remote_policy` bits do not grant or deny an operation. The capability
classifier remains in the protocol so a complete future permission design can
reuse semantic operation names, but it is dormant in the alpha runtime.

Loopback remains the default listener. Selecting a non-loopback listener still
requires the explicit `ipc.allow_remote` configuration choice and a valid
non-wildcard Origin list so exposure cannot happen accidentally through a bind
typo. These are configuration/exposure checks, not caller authentication. An
optional *outbound* peer secret reference may be resolved only to contact and
upgrade an older host that still enforces the superseded bearer flow. New alpha
peers work without it.

The retained server-proof and session-ticket endpoints are dormant
compatibility code, not active evidence of a security boundary. Do not expose
the alpha listener to an untrusted network. Motion policies, door checks, relay
sequencing, numeric bounds, OS confirmations, and exclusive programming
ownership remain functional safety checks and are not application auth/authZ.

## JSON-RPC 2.0

The raw transport contains one UTF-8 JSON object per line. HTTP and WebSocket
use the same request and response model:

```json
{"jsonrpc":"2.0","id":1,"method":"controller.status","params":{}}
```

```json
{"jsonrpc":"2.0","id":1,"result":{"uptime_ms":1234,"uptime":"1.234s"}}
```

An omitted `id` is a notification and receives no direct response. Errors use
the standard `error.code`/`error.message` object. The primary serializes
mutating RPC calls through the same controller client while snapshot and event
subscriptions use cached/thread-safe paths.

Standard codes are parse error `-32700`, invalid request `-32600`, method not
found `-32601`, and invalid params `-32602`. Runtime/device failures use
`-32000`. The dormant future-auth implementation reserves authentication
required `-32001` and remote capability denied `-32003`; alpha product entry
points do not emit those outcomes. The request ID is preserved on every parsed
request error.

### RPC methods

| Method | Parameters | Result/behavior |
|---|---|---|
| `controller.ping` | `{}` | service health and JSON-RPC wire-format identity |
| `controller.connect`, `controller.open`, `controller.port.open` | optional `port` | resume discovery or open the requested transport, then authenticate `HELLO` |
| `controller.close`, `controller.port.close` | `{}` | close UART and pause automatic reconnect |
| `controller.reset`, `controller.reset.lines`, `controller.port.reset` | optional `pulse_ms` | one explicit DTR-only pulse, then fresh application authentication |
| `controller.snapshot` | `{}` | cached connection, identity, status, and settings |
| `controller.command.catalog` | `{}` | machine-readable registered command names, aliases, usage, summary, and task group |
| `controller.status` | `{}` | fresh board status |
| `controller.peripherals.get` | `{}` | host-owned custom names plus the canonical 34-entry peripheral descriptor registry; requires `read` |
| `controller.peripherals.set` | `peripheral_names` object | atomically replace custom host names and return the normalized names plus registry; requires `host_configuration` |
| `controller.pwm.values` | `{}` | authoritative board availability, selected channel, and all sixteen logical values; requires `read` |
| `controller.pwm.set` | `channel` (`0..15`), `value` (`0..4095`) | write one channel, read back, and return the complete authoritative sixteen-channel snapshot; requires `board_commands` |
| `controller.pwm.off` | `{}` | clear every PWM channel, read back, and return the complete authoritative snapshot; requires `board_commands` |
| `controller.temperatures` | optional `rescan` | named temperatures and ROM identities |
| `controller.menu.list`, `controller.menu.current` | `{}` | live board catalog when advertised, otherwise the canonical capability-limited manifest |
| `controller.menu.jump`, `controller.menu.page` | `page` ID or name | select a board menu page |
| `controller.command.execute` | `command` | run any ordinary controller command; `quit`/`exit` requests primary shutdown |
| `controller.program_state.get` | `{}` | current host-owned Idle/Running owners, reason, and revision |
| `controller.program_state.set` | `mode`, optional `owner`, `reason` | set/clear one host-owned Running claim and mirror it to capable firmware |
| `controller.rf.list` | `{}` | all learned records |
| `controller.rf.learn.start` | optional `mode`, optional `timeout_ms` | start indefinite multi-code learning (default) or bounded `timer` multi-code learning; `single` and `one-shot` are timer aliases |
| `controller.rf.learn.status` | `{}` | current learn-session state |
| `controller.rf.learn.cancel` | `{}` | stop learning and emit an explicit end event |
| `controller.rf.map` | `id`, semantic `action`, optional `target` and `behavior` | validate and replace one learned mapping, then return the full board readback |
| `controller.rf.remove` | learned-record `id` (`0..19`) | remove one record, then return the full board readback |
| `controller.rf.clear` | exact `confirm: "CLEAR RF"` | clear all learned records and return an empty inventory |
| `controller.rf.transmit` | `code`, `bits`, `protocol`, optional `pulse_us`, `repeats` | transmit a validated waveform; omitted repeats means one burst |
| `controller.event.latest` | `{}` | latest monotonically increasing host event ID |
| `controller.event.next` | `after_id`, optional `kind`, `opcode`, `stream`, `timeout_ms` | long-poll the next event or exact unsolicited opcode without status polling; `stream` is `activity`, `state`, `telemetry`, or `debug` |
| `controller.opcode.send`, `.exchange`, `.request` | `opcode`, optional `payload` (base64) or `payload_hex`, optional `expect_opcode` (ACK by default) | exchange an opaque 1..255 UART opcode without requiring the host to understand its schema |
| `controller.app.instances` | `{}` | list live UI/automation instances and bounded non-secret state |
| `controller.app.bridge` | `{}` | query the original coordinator bridge instance, including bounded process self-information |
| `controller.app.instance.get` | `id` | query one exact live instance |
| `controller.app.instance.report` | `id`, `surface`, `page`, optional `state`, `lease_seconds`, `values`, `self` | create or refresh an ephemeral instance report; TUI followers use bounded navigation mode/group/epoch/revision values |
| `controller.app.instance.remove` | `id` | remove one instance report immediately |
| `controller.app.navigate` | `page`, optional `target` | navigate `*`, a surface such as `webui`/`tui`, or one exact instance ID |
| `controller.history.status` | optional ISO-8601 `since` | retained measurement samples, including samples restored from the bounded host data store after restart |
| `controller.history.timeline` | optional `since`, `limit` | durable important-event timeline |
| `controller.os.facts.catalog`, `controller.host.facts.catalog` | `{}` | fixed read-only Windows profile descriptors, columns, and row limits |
| `controller.os.facts`, `controller.host.facts` | optional `profile` and `timeout_ms` (100–5000) | bounded read-only result for `system`, `computer`, `firmware`, `storage`, or `serial`; empty profile selects `system` |
| `controller.lcd.presentation.status` | `{}` | prompt/priority presentation state |
| `controller.lcd.presentation.configure` | `enabled`, `debounce_ms`, `priority_hold_ms` | configure host LCD presentation |
| `controller.lcd.prompt` | `line1`, `line2` | queue a debounced prompt mirror |
| `controller.lcd.priority` | `kind`, `line1`, `line2`, optional `hold_ms` | display a priority overlay, then restore the prompt |
| `controller.message.send` | typed message envelope below | route/log a message and optionally display it on the board LCD |
| `controller.bridge.list` | `{}` | configured peers and live connection state, without URLs or credentials |
| `controller.bridge.call` | `peer`, nested JSON-RPC `request` | correlated call through that peer; recursive bridge calls are rejected |
| `controller.network.peers.get` | `{}` | persistent peer topology including optional secret references but never resolved or plaintext credentials |
| `controller.network.peers.set` | `peers` array | atomically replace and hot-apply peer topology; unknown fields and plaintext `auth_token` are rejected, and `host_configuration` classifies remote policy when permissions return |
| `controller.artifact.manifest`, `controller.artifact.list` | optional artifact `kind` | update capability/default/current discovery and content-addressed catalog |
| `controller.artifact.fetch`, `controller.artifact.capture` | typed fetch or explicitly authorized capture request | queue transfer/readback through the primary |
| `controller.update.firmware`, `controller.update.eeprom`, `controller.update.host` | artifact SHA-256 plus `authorized: true` | queue the guarded update for that domain |
| `controller.restore.flash` | captured-flash SHA-256, `authorized: true`, optional `method`, `port` | restore a `flash-backup` through its own guarded operation; never reinterpret it as firmware |
| `controller.update.status` | optional operation `id` | asynchronous transfer/programming status and progress |
| `controller.device.status` | `{}` | local-device HTTP/event-stream health and last confirmed power state |
| `controller.device.action` | `action`, optional `text` or `count` | bounded Local Device living power, display, alert, or passive-refresh action |
| `controller.device.inspect` | `resource` | sanitized `capabilities` or `snapshot` document only |
| `controller.integrations.local.get` | `{}` | credential-free local-device and data-hub enable/URL settings |
| `controller.integrations.local.set` | `local_device`, `data_hub` | validate and persist LAN-only device and loopback-only data roots |
| `controller.ports` | `{}` | current serial devices with stable identity fields |
| `controller.quit`, `controller.exit` | `{}` | close the primary and emit lifecycle shutdown |

Full TUI instances follow the ephemeral `default` navigation group unless that
process starts with navigation synchronization disabled. A follower reports
`navigation_sync=follow`, `navigation_group`, a random process-session
`navigation_epoch`, and a monotonically increasing `navigation_revision` in
its instance values. The primary keeps one in-memory canonical page, epoch, and
revision per live group. The first live follower seeds an empty group; later
followers receive an exact-instance catch-up, and a fresh local page report is
fanned out only to the other live followers. Receiver acknowledgements of the
canonical page are no-ops. When the last lease leaves or expires the group is
discarded, so active pages are never persisted as host configuration.
After an event-session reconnect a follower adds
`navigation_catch_up=true`; the coordinator then re-sends the canonical page
instead of treating the client's potentially stale page as new intent.

Coordinator navigation actions carry `navigation_sync=group`, group, epoch,
revision, and source-instance metadata. Clients reject duplicate, older, or
foreign-epoch deliveries until an authoritative primary reconnect resets their
acceptance cursor. Generic `controller.app.action` and `/api/app/action`
callers cannot supply these coordinator-owned fields. Authenticated remote
control instead uses `controller.app.navigate` or `/api/app/navigate`, which is
individually authorized and audited under `host_configuration`. An explicit
navigation can still target an opted-out instance, but does not enroll it in a
group or make it follow later broadcasts. Prompt input, cursor/editor/modal
state, terminal visibility, and serial ownership always remain local.

RF learning has two mutually exclusive modes. An omitted mode or
`{"mode":"indefinite"}` keeps accepting codes until cancellation. A bounded
session uses `{"mode":"timer","timeout_ms":30000}` and continues accepting
multiple codes during that window. The accepted `single` and `one-shot` mode
aliases normalize to `timer`; status and snapshots always return the canonical
mode together with `configured_ms`, live `remaining_ms`, captured count, and an
explicit end reason.

`controller.rf.map` uses operator-readable values rather than exposing compact
firmware enums. Actions are `none`, `key`, `menu`, `relay`, `side`, or `pwm`.
Key targets are `1..4`; relay targets are the user relays `5..8`; PWM targets are
`0..10`; menu targets are `prev`, `next`, `dec`, or `inc`; and side targets are
`left`/`A` or `right`/`B`. Key, relay, and PWM behaviors are `press`, `toggle`,
or `momentary`; side behavior is `up`, `down`, or `stop`. `none` accepts neither
target nor behavior. These typed mutations require `board_commands`, while list
and learn status remain `read` operations.

The guided Web/TUI workflow starts a fresh 30-second timer for exactly one
labeled A/B/C/D step. It reacts only to a new `rf.learn.mapping-required` event,
cancels the capture window, reads the stored record back, shows its exact
identity, and waits for explicit confirmation before mapping. Disconnect,
timeout, cancellation, and full-storage events leave the step retryable; they do
not synthesize a capture or optimistic mapping.

The opaque opcode exchange is deliberately versionless. It retains the native
48-byte payload bound, sequence correlation, ACK/error validation, board-command
authorization, and one caller-selected response opcode, but does not interpret
experimental payload bytes. This lets a client query a newer firmware feature
through an older generic-capable bridge without adding API-version branches.

Application-instance coordination uses the same event fan-out. A WebUI, TUI,
CLI, automation, or bridge peer reports an ID, surface, current page, optional
presence state, bounded process/browser `self` information, and at most 32
non-secret presentation values. Reports may use
a bounded lease (45 seconds by default, 300 maximum); this liveness refresh is
not board-state polling. Credential-like value keys and control-bearing values
are rejected. Native process self-information can include PID, parent PID,
image path, working directory, start time, and explicitly allowlisted runtime
variables. Raw environment, PATH, arguments, and credential-like variable keys
are never published. The original host registers a non-expiring `bridge`
surface and exposes it directly through `controller.app.bridge`; every other
instance can therefore find the actual coordinator instead of guessing which
UI owns it. Navigation targets `*`, a surface (`webui` or `tui`), or an exact
instance ID. The resulting `app.page` event retains source and target metadata
through IPC, REST, WebSocket, Socket.IO, and bridge paths so each recipient can
apply only matching commands and avoid echo loops.

The same targeted action envelope carries `app.title`, `app.progress`, and
`app.osc`. A TUI continuously derives its normal terminal title from the live
product name and selected page, restores the previous title when Bubble Tea
exits, and reports the effective title plus OSC capabilities in its instance
values. `app.progress` accepts `clear`, `normal`, `error`, `indeterminate`, or
`warning` plus an optional/required `0..100` value and emits Windows Terminal's
OSC `9;4` progress state. `app.osc` accepts only a bounded 1..512-byte payload
beginning with a numeric selector; embedded ESC, BEL, ST, C0, and C1 controls
are rejected so callers cannot smuggle multiple terminal sequences in one
action. `app title auto` resumes page-derived titles.

Artifact lifecycle events are also presentation state. A fresh `update.*`
event immediately selects the Updates page in matching WebUI/TUI instances,
updates the visible operation ID/state/detail/progress bar, and updates the TUI
title plus OSC `9;4` tab/taskbar progress. Completion clears terminal progress;
failure leaves an error state visible. The UI reads operation status on initial
entry or explicit refresh, while ongoing changes remain push-driven.

Firmware may emit the compact `APP_NAVIGATION` event with target `all`,
`webui`, or `tui` and an ASCII page ID. It enters the same host event path and
is processed immediately; it does not wait for a UI poll. Host-to-board
navigation/query can use the generic opcode exchange until a specific semantic
opcode is useful enough to standardize, preserving the living protocol model.

Programming, macros, relays, PWM, RF transmit, settings, display, RGB, buzzer,
and I2C operations remain available through `controller.command.execute`, so API
clients do not need a second less-safe command implementation. The response is
correlated to its JSON-RPC ID and the inner native command remains correlated
to its UART sequence.

## REST and inbound web service

All JSON endpoints share the IPC listener:

| Method and path | Purpose |
|---|---|
| `GET /healthz` | unauthenticated liveness and service identity; no board data |
| `GET /api/ui-config` | unauthenticated non-secret browser bootstrap contract |
| `POST /api/rpc` | one JSON-RPC request |
| `GET /api/snapshot` | cached controller snapshot |
| `GET /api/peripherals` | custom names and the canonical 34-entry descriptor registry; `read` capability |
| `PUT /api/peripherals` | replace custom names from `peripheral_names`; `host_configuration` capability |
| `GET /api/pwm` | authoritative availability, selected channel, and all sixteen values; `read` capability |
| `PUT /api/pwm` | write `channel` (`0..15`) and `value` (`0..4095`), then return all sixteen values; `board_commands` capability |
| `DELETE /api/pwm` | clear all sixteen channels and return their authoritative readback; `board_commands` capability |
| `GET /api/commands` | machine-readable shared command catalog |
| `GET /api/program-state` | current host-owned Idle/Running state |
| `PUT` or `POST /api/program-state` | update `owner`, `mode`, and optional `reason` |
| `GET /api/menu/catalog` | live firmware menu IDs, labels, kinds, flags, and parent relationships |
| `GET /api/menu/layout` | live visible mask and ordered menu IDs |
| `PUT` or `POST /api/menu/layout` | persist a validated `visible_mask` and `order` on the board |
| `GET /api/host-menus` | current watched host-presented menu directory |
| `PUT` or `POST /api/host-menus` | validate and persist the complete host-presented menu directory |
| `GET /api/os/status` | bounded host status and OS-action policy state |
| `GET /api/os/facts?profile=...` | bounded read-only Windows facts; `profile=list` or `catalog` returns the fixed catalog |
| `POST /api/os/key` | validated virtual-key request under the `virtual_keys` capability |
| `POST /api/os/power` | confirmed lock, suspend, hibernate, restart, shutdown, or monitor-brightness request under `power_actions` |
| `POST /api/command` | `{"command":"..."}` through the ordinary command engine |
| `POST /api/messages` | typed message envelope |
| `POST /api/display` | arbitrary segment/LCD text with `speed_ms`, `duration_ms`, `repeat`, `interval_ms`, and optional forced `scroll` |
| `POST /api/opcode` | opaque opcode exchange using the same payload/expected-response fields as JSON-RPC |
| `GET /api/app/bridge` | original coordinator bridge instance and bounded process self-information |
| `GET /api/app/instances` | list instances, or query exact `?id=...` |
| `POST /api/app/instances` | create/refresh an instance report |
| `DELETE /api/app/instances?id=...` | remove one instance report |
| `POST /api/app/navigate` | navigate a page with optional target instance/surface |
| `POST /api/app/action` | route a validated page/title/progress/OSC/command/lifecycle action with optional target instance/surface |
| `GET /api/bridges` | configured peer names/protocols and live state |
| `POST /api/bridges/call` | `peer` plus a nested JSON-RPC `request` |
| `GET /api/artifacts/manifest` | artifact/default/current discovery and latest operation |
| `GET /api/artifacts` | content-addressed catalog, optionally filtered by `kind` |
| `POST /api/artifacts/upload`, `/fetch`, or `/capture` | import, remote-download, or explicitly read device memory |
| `GET` or `HEAD /api/artifacts/{kind}/{sha256}` | checksum/ETag/range-capable artifact download |
| `POST /api/updates/firmware`, `/eeprom`, or `/host` | explicitly authorized update job |
| `POST /api/restores/flash` | explicitly authorized captured-flash restore job |
| `GET /api/updates/status/{id}` | update progress and result |
| `POST /api/webhooks/inbound` | typed incoming message when inbound webhooks are enabled |
| `/api/integrations/datahub/*` | authenticated service-neutral streaming bridge to the configured loopback data service |
| `/api/integrations/device/*` | always fails closed; device operations require typed RPC |
| `POST /ipc` | JSON-RPC compatibility on the configured WebSocket path |

All data/API routes except `/healthz` and the non-secret `/api/ui-config`
bootstrap apply host authentication. Bodies are limited to
1 MiB. Unsupported methods are rejected, and the inbound webhook path is `404`
when disabled. An inbound webhook is data, not an implicit shell command. To
make it actionable, enable a narrow text mapping whose resulting command still
passes the normal safety path.

Before publishing an inbound webhook, the host removes credential-shaped query
and metadata names, caller-reserved provenance, cookies, referrers, signatures,
and all headers outside a bounded trace/content allowlist. Host-owned method and
path provenance is then added, and an empty-body fallback records only the
routed path—not the raw `RequestURI` or query string.

### Peripheral names and authoritative PWM state

`controller.peripherals.get` and `GET /api/peripherals` return this shape:

```json
{
  "peripheral_names": {"relay.5": "Workbench lamp"},
  "peripherals": [
    {
      "key": "relay.5",
      "kind": "relay",
      "role": "user-output",
      "index": 5,
      "default_name": "User Relay 5",
      "control": "relay"
    }
  ]
}
```

The complete registry always contains 34 descriptors: eight relays, two motion
sides, sixteen PWM channels, two displays, and six sensors. Custom values are
presentation names in `ui.peripheral_names`, not device settings. Set methods
trim keys and names, reject invalid input atomically, and treat a blank name as
a request to remove that override so the descriptor's `default_name` becomes
visible again. No peripheral-name operation reads or writes MCU EEPROM.

All PWM read and mutation methods return the native `PWM_VALUES` JSON shape:

```json
{
  "available": true,
  "selected_channel": 3,
  "values": [0, 0, 0, 2048, 0, 0, 0, 0, 0, 0, 0, 0, 256, 0, 0, 0]
}
```

The response is an authoritative board readback, not an optimistic echo of the
requested value. Channels `0..10` are user/commissioning outputs and may be
presented as generic PWM controls. Channels `11..15` are role-specific:
enclosure illumination, power indication, and status red/green/blue. They stay
in every sixteen-value snapshot, but user interfaces must represent them with
their dedicated controls and semantics rather than another generic slider.
`controller.pwm.off` and `DELETE /api/pwm` deliberately clear all sixteen
channels, including the role-specific outputs.

### Bounded Windows host facts

Host facts are diagnostics, not a general management-query endpoint. The
authenticated read surface exposes only these compiled profiles:

| Profile | Fixed class | Maximum rows | Columns |
|---|---|---:|---|
| `system` | `Win32_OperatingSystem` | 1 | caption, version, build, architecture, last boot, total/free visible memory |
| `computer` | `Win32_ComputerSystem` | 1 | manufacturer, model, system type, physical memory, logical processors |
| `firmware` | `Win32_BIOS` | 2 | manufacturer, firmware version/release date, SMBIOS level |
| `storage` | `Win32_LogicalDisk` | 16 | fixed-volume ID, type, filesystem, size, free space |
| `serial` | `Win32_SerialPort` | 32 | device ID, name, description, status, configuration error code |

JSON-RPC rejects unknown fields and accepts only `profile` plus an optional
`timeout_ms` from 100 through 5000. REST accepts one `profile` query parameter
in addition to the normal authentication token; any caller-supplied query,
class, column, or repeated parameter receives `400`. The provider caps each
cell at 512 bytes, the whole result at 64 KiB, the combined row count at 32,
and reuses a five-second private cache across CLI, IPC, REST, and WebSocket RPC.
Calls are serialized behind one bounded worker so a stuck native call cannot
create unbounded OS threads. The surface is read-only and exposes no arbitrary
WQL, write, process-control, or method-invocation path. Non-Windows hosts return
an explicit unavailable error.

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

The embedded static set includes the Web App Manifest, mobile presentation
metadata, icons, and a deliberately network-only service worker. Supported
browsers may install the UI and expose its Overview, Workbench, Activity, and
Settings shortcuts, but the worker retains no response cache and provides no
offline control surface. If the host listener stops, the browser observes the
network failure immediately. The manifest and worker receive the same exact
MIME, validator, range, and reserved-path treatment as the other embedded
assets.

`controller web` starts the same primary host services used by the TUI but does
not construct a terminal model or read terminal input. It owns discovery,
serial, automations, global hotkeys, integrations, and the command dispatcher
for its whole lifetime; `--no-open` serves the application without launching a
browser. The UI sends terminal commands through same-origin typed RPC and keeps
a persistent subscription for status, board/host events, and global application
actions. A TUI is therefore not required in web mode. When the TUI is the
primary instead, the same action broker retains bounded TUI delivery and also
mirrors each valid page, title, progress, and OSC action into cursor-based
browser event history. Browsers apply matching page actions and fresh update
lifecycle navigation; terminal-only actions remain available to matching TUI
instances without being interpreted by the browser.

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

Browser clients obtain the one-use subprotocol ticket described under
Authentication; native clients may authenticate the upgrade directly with a
Bearer or compatibility header. Neither client type places credentials in the
WebSocket URL. The successful `subscribed` response includes the authenticated
principal used for subsequent capability decisions.

```json
{"jsonrpc":"2.0","id":1,"method":"controller.subscribe","params":{"topics":["events","state","opcodes","status"],"opcodes":[156,157,225],"interval_ms":200,"after_id":0}}
```

```json
{"jsonrpc":"2.0","id":2,"method":"controller.unsubscribe","params":{}}
```

Topics are `events`, `state`, `debug`, `opcodes`, and `status` (`opcode` and
`telemetry` are accepted as singular/status aliases). An omitted opcode filter receives every
valid nonzero opcode; a supplied `opcodes` array selects exact values 1..255.
Status interval is 50..60000 ms and defaults to 200 ms. Event
delivery starts after `after_id`; zero starts at the current tail and does not
replay the whole timeline. Push messages are JSON-RPC notifications:

The `events` topic is the human-facing activity stream: actionable entries such
as connection, door, RF, macro, automation, and fault changes. It has its own
retention ring, so animation frames and measurements cannot evict useful
one-shot activity. `state` carries changed-only display, status-light, buzzer,
and other continuous output frames for immediate UI mirrors without writing
them to activity consoles. `debug` is an explicit opt-in for raw `rx`, `tx`, and
opcode trace events. Subscribe to `opcodes` for opaque unsolicited frames and
`status` for the independently paced live measurement stream. Durable timeline
storage and ordinary TUI, WebUI, and secondary-console logs consume `activity`
only; diagnostic monitors may deliberately request the noisier streams.

```json
{"jsonrpc":"2.0","method":"controller.event","params":{}}
{"jsonrpc":"2.0","method":"controller.state","params":{}}
{"jsonrpc":"2.0","method":"controller.debug","params":{}}
{"jsonrpc":"2.0","method":"controller.opcode","params":{"opcode":225,"payload":"qrs="}}
{"jsonrpc":"2.0","method":"controller.status","params":{}}
{"jsonrpc":"2.0","method":"controller.error","params":{}}
```

A status subscription participates in demand accounting. UART stays open after
unsubscribe, but periodic status polling stops when no UI, script, automation,
IPC, WebSocket, or bridge consumer requires it. Asynchronous board events keep
flowing because they do not require polling. `controller.close` deliberately
pauses automatic reconnect; a later `controller.open`/`controller.connect`
clears that pause only after a transport has authenticated successfully.

## Socket.IO compatibility

Socket.IO uses a distinct configured path, default `/socket.io/`. The bounded
adapter is genuine Engine.IO v4 / Socket.IO framing over WebSocket:

```text
ws://HOST:PORT/socket.io/?EIO=4&transport=websocket
```

It sends an Engine.IO open packet, accepts Socket.IO connect/disconnect, and
implements Engine.IO ping/pong. Socket.IO event packets use the usual
`42["name",payload]` form. Supported incoming events are:

In the immediate alpha, Engine.IO opens without an application credential;
Origin enforcement still precedes the open packet. Header and one-use ticket
code is retained only for future design and older-host upgrade compatibility.

| Event | Payload | Response/push events |
|---|---|---|
| `subscribe` | WebSocket subscription object | `subscribed`, then `controller.event`, `controller.opcode`, `controller.status`, or `controller.error` |
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
headers/body template, timeout, retry policy, and optional signing secret.
Methods are GET, POST, PUT, PATCH, and DELETE. A suffix `*` in the event-kind
filter matches a prefix; empty or `*` matches all events. `webhook.*` events are
not sent back through webhooks, preventing a direct delivery loop.

GET and DELETE add `id`, `kind`, and `text` query parameters. Body-bearing
methods send the event JSON by default. A configured template can use
`{{id}}`, `{{kind}}`, `{{text}}`, `{{source}}`, `{{time}}`, `{{event}}`, and
`{{metadata}}`. In a JSON body, a placeholder inside a quoted string is JSON
escaped; outside a string, `event` and `metadata` are inserted as JSON values.
The completed body—not merely the template—is limited to 256 KiB and must be
valid JSON when its content type is JSON.

Delivery is durable. Before a worker sends an event, the queue is atomically
persisted under the host data directory as `state/outbound-webhooks.json`.
The state contains delivery/event metadata, but never configured URLs,
headers, rendered bodies, or signing secrets. It is bounded to 1,024 pending,
512 dead-letter, and 2,048 recent deduplication records; completed
deduplication records expire after 24 hours. Eight workers prevent one slow
target from blocking unrelated targets. Shutdown drains pending work until the
bounded host shutdown deadline; an interrupted attempt remains recoverable on
the next launch.

Each logical target/event pair receives a deterministic `Idempotency-Key` and
a stable delivery/correlation ID. Every attempt has a fresh attempt ID and
attempt number. Receivers should deduplicate on `Idempotency-Key`, because a
network failure can make the sender unable to prove whether the receiver
committed an earlier attempt. The emitted headers are:

- `Idempotency-Key`
- `X-PCController-Delivery-ID`
- `X-PCController-Correlation-ID`
- `X-PCController-Attempt-ID`
- `X-PCController-Attempt`

Only 2xx is success. Transport failures and HTTP 408, 425, 429, and 5xx retry
with exponential backoff and 20% jitter. Defaults are six attempts, 500 ms
initial delay, and 30 seconds maximum delay; per-target settings can override
them. A valid delta-seconds or HTTP-date `Retry-After` is honored when it is
longer than the computed delay, capped at 24 hours. Other HTTP failures and
exhausted retryable failures move to the durable dead-letter list. Response
draining is capped at 64 KiB. Redirects are never followed: a 3xx response is a
failure, so credentials, configured headers, signatures, and delivery headers
cannot escape to an unvalidated redirect destination. Configure the canonical
final URL explicitly.

When `signing_secret` is configured, the sender also sets
`X-PCController-Timestamp`, `X-PCController-Nonce`, and
`X-PCController-Signature`. The signature is `v1=` followed by the lowercase
hex HMAC-SHA256 of this exact byte sequence:

```text
timestamp + "\n" + nonce + "\n" + uppercase_method + "\n" +
request_uri + "\n" + delivery_id + "\n" + body
```

The receiver should verify the signature in constant time, reject stale
timestamps, and reject reused nonces. Prefer `signing_secret_ref` over the
plaintext field; `os:NAME` uses the current Windows user's Credential Manager
and `env:NAME` resolves only from the process environment. `secret_headers`
maps an outbound header name to the same reference types. Resolved values are
excluded from config inspection, queue state, administrative responses,
snapshots, exports, and error text.

Queue administration is structured and never returns endpoint credentials:
delivery listings are limited to 1..100 records per call, and each diagnostic
`last_error` is capped at 2 KiB.

| Operation | JSON-RPC method | REST endpoint |
|---|---|---|
| Status | `controller.webhooks.status` | `GET /api/webhooks/outbound/status` |
| Pending deliveries | `controller.webhooks.pending` with `{"limit":25}` | `GET /api/webhooks/outbound/pending?limit=25` |
| Dead letters | `controller.webhooks.dead` with `{"limit":25}` | `GET /api/webhooks/outbound/dead?limit=25` |
| Replay one | `controller.webhooks.replay` with `{"delivery_id":"ID"}` | `POST /api/webhooks/outbound/replay` with the same JSON body |
| Clear one | `controller.webhooks.clear` with `{"delivery_id":"ID"}` | `POST /api/webhooks/outbound/clear` with the same JSON body |

Bulk replay/clear requires the explicit body
`{"all":true,"confirm_all":true}`. Status/list operations require the remote
read capability; replay/clear require the remote integrations capability. The
same JSON-RPC methods work through authenticated IPC, HTTP RPC, WebSocket,
Socket.IO RPC, and a permitted host bridge. CLI/TUI operators can use
`webhook status`, `webhook pending [LIMIT]`, `webhook dead [LIMIT]`,
`webhook replay DELIVERY_ID`, and `webhook clear dead DELIVERY_ID`; bulk CLI
operations require the literal `CONFIRM`. Queue/retry/dead-letter events are
also emitted to the host timeline.

## Outbound WebSocket bridge

An enabled `integrations.websocket_clients` entry makes the primary host a
standard WebSocket or bounded Socket.IO client. During alpha it connects
without credentials unless an optional compatibility bearer is configured for
an older peer. It subscribes to validated `events`/`state`/`status` topics, reconnects with bounded
backoff, and can forward local events as correlated `controller.message.send`
calls. Transport/control/error events and remote-origin messages are not
re-forwarded, preventing a direct two-host echo loop. Incoming remote
events and state remain structured; status is re-emitted locally as a
source-tagged message.

`controller.bridge.call`, `POST /api/bridges/call`, and
`bridge call PEER METHOD [PARAMS_JSON]` use the existing persistent connection
and an internal wire ID, then restore the caller's nested JSON-RPC ID in the
response. The target host applies its own token, remote capability policy, and
ordinary safety path. Recursive bridge calls are rejected, so this API is not
an unrestricted network pivot. Incoming command requests on an outbound peer
connection additionally require that peer's `allow_commands` flag.

`controller.opcode.exchange` may be used as the nested bridge method. Because
the bridge forwards the opaque opcode and bytes rather than a feature-specific
schema, experimental firmware queries continue to work after the generic path
has landed even when that bridge has not been rebuilt for the experiment.

The bridge does not open another host's COM port. Each instance retains one
local serial owner. Programming through a remote primary requires the target's
`programming` and `connection_control` permissions and follows the same
application-UART close, guarded toolchain/Urclock run, and fresh `HELLO`
recovery as local programming.

Subscribed peer state remains structured. In particular, an unsolicited
`buzzer.note` retains its frequency/duration metadata so an independently
enabled host renderer can play it immediately. The receiver stamps
`bridge.ingress` and never forwards an ingressed event again; this gives
server-to-edge mirroring exactly once without polling or bridge cycles. Both
JSON-RPC and Socket.IO peers must include `state` in their configured topics.

## Artifact distribution and remote updates

Firmware distribution and hardware programming are deliberately separate
operations. “Build-only watch” means only the source-to-artifact half; it must
not quietly turn every save or CI build into a physical write. A deployment
watcher may discover and download an update, then send a separate authorized
update request to the already-running primary host. That primary is the sole
component allowed to open hardware. It applies the staged image after an
explicit `authorized: true` request and performs the existing settings-preservation and
complete flash/EEPROM/metadata backup, releases UART ownership, invokes
Urclock/AVRDUDE, verifies the write, reconnects to a valid application `HELLO`,
then restores the board settings/buzzer lifecycle. A missing or unresponsive
bootloader produces a diagnostic that recommends the explicit `usbasp` method;
it does not silently switch to ISP.

Captured-flash restore is deliberately not a firmware-update alias. The RPC
method is `controller.restore.flash`, the REST route is
`POST /api/restores/flash`, and the selected artifact must have kind
`flash-backup`. The primary owner still uses the same proven guarded programmer
transaction: Urclock is the default, `usbasp` must be requested explicitly,
flash/EEPROM/metadata are backed up before the write, AVRDUDE verifies the
write, and the host requires application `HELLO` reconnect plus lifecycle
restoration before reporting completion.

The host data directory contains a SHA-256 content store. Equal bytes map to
one immutable blob even if they arrive from a browser upload, another host, an
HTTP repository, or a device readback. Metadata records preserve kind, source,
size, build hash, packed build timestamp, and platform. Firmware, EEPROM, and
flash-readback artifacts must parse as Intel HEX before publication. Downloads
respect `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`, limit redirects, reject an
HTTPS-to-HTTP downgrade, remove bearer credentials on cross-authority
redirects (including after a composed client redirect callback), and verify
declared size plus SHA-256 before the final name appears. An injected ordinary
HTTP client is only a settings template: its proxy and dial hooks cannot relax
the public-source invariant. A local test or explicitly trusted peer must select the
separately named trusted constructor explicitly.
Remote artifact and update sources are public-network only: the initial URL,
every redirect, and the effective response URL reject loopback, private,
link-local, multicast, local-DNS, and metadata destinations. Direct connections
resolve again immediately before dialing and use the validated numeric address,
closing DNS-rebinding time-of-check/time-of-use gaps while preserving the
original TLS server name. Proxy trust is bound to the proxy function's actual
choice for that request; a configured proxy bypassed by `NO_PROXY` cannot exempt
a matching direct destination. The local-name suffix list used by public-source
rejection is also the one used to build the default `NO_PROXY` bypass. An
explicitly configured forward proxy remains an operator trust boundary because
that proxy performs its own destination lookup; commission proxy behavior before
enabling remote artifact intake.

Artifact metadata and blobs are opened relative to one confined store root.
Size and SHA-256 are verified on the already-open regular-file handle, that
handle is rewound, and the same handle is served to the caller. The store does
not verify a path and reopen it later, so a rename or symlink replacement cannot
substitute a different file between integrity verification and download.

The store root is an exclusive trusted-writer boundary owned by the controller
process account: it requests owner-only directory modes and portable read-only
blob modes, while the canonical user-data directory supplies the platform ACL.
Portable modes are advisory on platforms such as Windows. Every later open
hashes the content again and rejects an in-place change. A privileged or
same-account actor that deliberately changes permissions and overwrites an inode
while that already-open handle is actively being streamed is outside this
process-local threat model. Do not grant another process write access to the
store root; use the import API, which stages and rehashes content, instead.

An embedded default firmware/EEPROM pair makes first-board recovery available;
it never authorizes a write. The manifest reports `defaults_enabled` only when
both validated artifacts exist. A running-board comparison uses its build hash
and packed two-second timestamp, not a semantic version. The operator must
still choose the artifact and programming method and explicitly confirm the
operation. Host executable updates accept only a structurally valid PE, ELF,
or Mach-O executable. The parsed header supplies OS/architecture; conflicting
caller metadata is rejected. A copied instance of the current host coordinates
replacement after the primary exits. Its durable journal preserves exact argv
(including `--config`) and working directory, keeps a verified old image,
starts the candidate, and requires a token-bound health acknowledgement within
30 seconds. Early exit or timeout kills the candidate, atomically restores the
old image, and restarts it with the same argv. A later process reconciles a
journal orphaned by coordinator or machine failure from executable hashes.

Device capture, firmware/EEPROM programming, captured-flash restore, and host
self-update share one exclusive transaction lane. Downloads may continue in
parallel, but two operations cannot contend for UART, ISP, reset lines, or the
running executable. These requests accept optional `idempotency_key`; HTTP may
instead use `Idempotency-Key`. The same key plus request returns the original
operation ID, including after restart; different parameters are rejected.
Completed operation journals survive restart. A queued/running hardware job
interrupted by restart becomes `failed` with `host_restarted` and is not
silently replayed.

Artifact and update JSON-RPC methods are:

| Method | Parameters | Result/behavior |
|---|---|---|
| `controller.artifact.manifest` | `{}` | feature/default/current artifacts, board identity, policy, and latest update status |
| `controller.artifact.list` | optional `kind` | SHA-256-sorted artifact descriptors |
| `controller.artifact.fetch` | `url`, `kind`, optional `name`, `sha256`, `bytes`, build identity, `idempotency_key` | queue a verified proxy-aware HTTP download |
| `controller.artifact.upload.begin`, `.chunk`, `.finish`, `.abort` | bounded transfer descriptor, ordered binary chunks, or `transfer_id` | authenticated bridge artifact transport; incomplete transfers expire and never enter the immutable store |
| `controller.artifact.capture` | `components`, `authorized`, optional `method`, `port`, `idempotency_key` | explicitly read and verify current flash/EEPROM through the primary |
| `controller.update.firmware` | `artifact_sha256`, `authorized`, optional `method`, `port`, `allow_incomplete_backup`, `reinitialize_eeprom`, `idempotency_key` | guarded backup-then-flash; explicit reinitialization retains raw EEPROM, programs/readbacks the complete Go-owned factory image, and discards incompatible semantic settings |
| `controller.restore.flash` | `artifact_sha256`, `authorized`, optional `method`, `port` | guarded restore of a `flash-backup`; Urclock by default, explicit USBasp fallback |
| `controller.update.eeprom` | same | full pre-write capture, then confirmed EEPROM restore |
| `controller.update.host` | `artifact_sha256`, `authorized` | stage a verified deferred self-update |
| `controller.peer.update.host` | `peer`, host `artifact_sha256`, `authorized`, optional `idempotency_key` | transfer through the existing authenticated bridge, revalidate on the peer, then ask that peer coordinator to replace itself gracefully |
| `controller.update.status` | optional operation `id` | latest or selected asynchronous status |

Peer host replacement is an application protocol, not an SSH deployment
recipe. The source streams a verified executable through its already-connected
bridge in bounded chunks; the target rehashes and reparses it before its own
coordinator performs the ordinary journaled self-update and rollback health
check. Either host can be source or target. `allow_commands`, the target's
`programming` policy, and explicit `authorized: true` are all required.

Provider and manifest discovery use a companion, product-neutral contract:

| Method | Parameters | Result/behavior |
|---|---|---|
| `controller.discovery.github.workflow` | `repository`, `kind`, optional `branch`, `workflow`, `platform`, `api_base_url`, build identity, `packed_timestamp`, `bearer_token` | newest successful matching run and its non-expired artifacts; metadata only |
| `controller.discovery.github.release` | `repository`, `kind`, optional `tag`, `include_prerelease`, `platform`, `api_base_url`, `packed_timestamp`, `bearer_token` | latest stable, requested tag, or opted-in prerelease assets; reads `SHA256SUMS` when provided |
| `controller.discovery.manifest` | `url`, optional `bearer_token` | fetch and validate a `controller-update-manifest/v1` document |
| `controller.discovery.local_manifest` | `{}` | publish this primary host's deduplicated inventory in the same portable manifest format |
| `controller.discovery.check` | current artifact identity, `kind`, optional `platform`, candidate list | `same`, `newer`, `older`, `different`, or `unavailable`, using digest before packed/build time |
| `controller.discovery.stage` | candidate, optional transient `bearer_token`, `idempotency_key` | queue proxy-aware download, digest/size verification, safe ZIP member selection, and content-store import; never programs |
| `controller.discovery.status` | optional operation `id` | latest or selected discovery/staging progress |

`bearer_token` is request-scoped; the Web UI does not persist it and clears it
after a successful stage. `platform` uses Go-style `os/arch` values such as
`windows/amd64`. ZIP extraction rejects traversal, links/devices, excessive
entry counts, and excessive expanded size. An archive containing more than one
equally suitable member must provide `archive_path` instead of relying on an
arbitrary first match.

A minimal independently hosted manifest is:

```json
{
  "format": "controller-update-manifest/v1",
  "generated_at": "2026-08-02T00:00:00Z",
  "artifacts": [
    {
      "kind": "firmware",
      "name": "board.hex",
      "url": "images/board.hex",
      "bytes": 48192,
      "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "build_hash": "source-or-build-hash",
      "build_timestamp": "2026-08-02T00:00:00Z",
      "packed_timestamp": 889298268
    }
  ]
}
```

Artifact URLs may be absolute or relative to the manifest. Unknown additive
fields are ignored within the recognized format, while required known fields,
URLs, sizes, kinds, and digests are still validated.

The equivalent REST routes are:

| Method and path | Purpose |
|---|---|
| `GET /api/artifacts/manifest` | feature/default/current manifest and board hash/time comparison |
| `GET /api/artifacts?kind=...` | list all or one kind |
| `POST /api/artifacts/upload?kind=...&name=...` | raw or multipart browser upload; optional hash/size query or checksum header |
| `POST /api/artifacts/fetch` | queue verified remote HTTP download |
| `POST /api/artifacts/capture` | explicit fresh flash/EEPROM capture |
| `GET` or `HEAD /api/artifacts/{kind}/{sha256}` | ranged immutable artifact download |
| `GET` or `HEAD /api/artifacts/current/flash` | last explicitly captured and verified current flash |
| `GET` or `HEAD /api/artifacts/current/eeprom` | last explicitly captured and verified current EEPROM |
| `POST /api/updates/firmware` | explicit firmware programming job |
| `POST /api/restores/flash` | explicit captured-flash restore job; never routed through firmware update |
| `POST /api/updates/eeprom` | explicit EEPROM restore job |
| `POST /api/updates/host` | explicit deferred host replacement job |
| `GET /api/updates/status/{id}` | update progress/result |
| `POST /api/discovery/github/workflow` | discover successful workflow artifacts without downloading |
| `POST /api/discovery/github/release` | discover release assets and checksum metadata |
| `GET /api/discovery/manifest` | serve this primary host's portable manifest for peer discovery/sync |
| `POST /api/discovery/manifest` | fetch and validate a remote update manifest |
| `POST /api/discovery/check` | compare candidate and installed/staged identities |
| `POST /api/discovery/stage` | queue verified download/extraction/import; no programming |
| `GET /api/discovery/status/{id}` | discovery/staging bytes, percentage, result, or safe failure |

Progress is emitted on the existing event/WebSocket/Socket.IO paths as
`update.queued`, `update.downloading`, `update.downloaded`,
`update.programming`, `update.verifying`, `update.completed`, or
`update.failed`, with operation ID, percent, kind, hash, and safe error code in
metadata. Status also carries typed `programming_method`,
`bootloader_outcome`, and `isp_fallback_suggested`. A timed-out Urclock job
therefore reports `timed_out` and explicitly suggests ISP recovery without a UI
parsing AVRDUDE prose; USBasp/host jobs report `not_attempted`. A secondary
local process or authenticated bridge peer calls these
same RPC methods; it never opens the port itself. Remote artifact bytes require
a configured bearer token even when read access is enabled, and applying or
capturing memory additionally requires the remote `programming` capability.

Discovery/staging publishes `artifact.discovery.queued`,
`artifact.discovery.downloading`, `artifact.discovery.completed`, and
`artifact.discovery.failed` through the same event, WebSocket, Socket.IO, and
bridge paths. Metadata includes operation ID, artifact kind, candidate ID,
completed bytes, total bytes, and percentage. External downloads use Go's
standard proxy-environment behavior (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`,
including conventional lowercase forms), bounded redirects, cross-authority
credential stripping, and HTTPS-downgrade rejection.

The board lifecycle uses a separate current-only durable marker. Before UART is
released, the host snapshots exact MCU EEPROM settings, cancels any macro,
sends relay/PWM all-off, and persists a temporary safe image: Silent on,
illumination zero/off, output persistence and relay restore mask cleared,
status light off, and closed-door
TM1637 brightness equal to its open value so `Prog` stays visible. Every
authenticated application reboot with an unfinished marker reasserts safe
outputs plus `Prog` / `Programming...`; ordinary illumination and audio cannot
resume just because the MCU reset. The programmer must durably record its final
host outcome before reconnect may restore and verify the exact original EEPROM
settings (including audio) and clear the marker.

The current protocol and firmware use settings flag bit 1 as the durable
`ProgrammingMode` safety latch. Boot and normal service keep motion, relays,
PWM, illumination, and macro execution inactive while the latch is set, and
the TM1637 remains on `Prog`. After a final programmer result, the host first
restores and reads back every original EEPROM field while retaining the latch;
only a second verified settings write clears it and permits normal outputs.
The host recovery journal remains the authoritative multi-step operation
record, while this single reused bit provides the reset-safe MCU hook without
growing EEPROM.

For an unpublished development board whose settings payload cannot be decoded,
the authorized firmware-update request may set `reinitialize_eeprom: true`.
This option is mutually exclusive with `allow_incomplete_backup`: the primary
must first retain a complete verified raw EEPROM image. The marker records the
query error and partial live-state result, outputs are forced safe, and after
flashing only the new firmware's current settings schema is accepted. Silent
is cleared, illumination/persistence/relay restore are disabled, outputs and
readback are verified, and the marker is then removed. No legacy decoder or
migration behavior is added to the board firmware.

SETTINGS byte 12 is `displayOptions`: bits 0..2 hold closed-door TM1637
brightness and bits 3..7 hold the motion-menu exit duration in seconds. An
encoded hold value of zero means the configured two-second default; values
1..31 are exact. Byte 13 carries the relay restore mask. The settings prefix is
exactly 15 bytes, with byte 14 holding the persisted 1..255 ms motion
break-before-direction interval; flags bit 7 has no timing role. A SET_SETTINGS
command may append a name-length byte and up to eight printable ASCII name
bytes; the exact 15-byte command deliberately preserves the current name. A
SETTINGS response appends settings-persisted at byte 15, name-persisted at byte
16, name length at byte 17, and the name at byte 18. The current EEPROM record
contains the 31 settings bytes, one name length, eight name bytes, and CRC-8.

The native CRC-backed EEPROM value is the authoritative operator name. Urclock
filename/title metadata is upload-time bootloader metadata and is not used as
runtime board identity. During alpha, successive version builds do not gain
layout migrations or compatibility aliases; those are reserved for distinct
profile/feature builds that are intentionally supported concurrently.

## Network device directory and discovery

Advertisement is enabled by default for mDNS/DNS-SD, SSDP/UPnP,
WS-Discovery, bounded UDP broadcast, and NetBIOS. SSDP advertises
`urn:pccontroller-org:service:bridge:1`, answers its type and `ssdp:all`, and
publishes the UPnP device description. `GET /upnp/public.json` is the canonical
secret-free document containing hostname, host build, board firmware and port
identity, service/connection health, current public telemetry, and API/Web/
WebSocket/Socket.IO/operation/event/opcode endpoints. SOAP `GetPublicInfo`
locates that document and `GetStatus` returns the principal health/telemetry
fields directly.

`controller.discovery.scan` and `controller network discover|list` query any
combination of the transports and return one merged `Instance` per host.
`protocols` identifies every successful path and `sources` retains each raw
transport observation. `controller.discovery.connect` opens raw IPC and reads
the remote snapshot; `controller network connect --target NAME` also probes
HTTP, WebSocket, and Socket.IO. Optional `--token-ref REF` exists only for an
older auth-on host during upgrade. The TUI HOST
Settings page uses `D` to scan and Enter/C on a discovered row to open its Web
endpoint. The Web workbench renders the same merged directory and Connect
navigates to the selected host.

Advertisement can be disabled or narrowed per protocol through host config,
`controller.discovery.config.get|set`, the Web workbench, the TUI network
editor, or `controller network advertise`. Scan start, each merged device,
completion/failure, connection, and configuration-change events fan out through
the ordinary WebSocket and Socket.IO event subscriptions. Public-document HTTP
enrichment bypasses Internet proxies and is pinned to the discovery responder's
exact host and port. Discovery can still be blocked by Windows Firewall,
VLANs, VPNs, or Wi-Fi isolation. Finding an instance does not enable its
listener or change board/OS safety policy.

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
`ipc`, `rest`, `webhook`, `websocket`, and `socket_io`. Targets are `client`, `server`, `bridge`,
`board`, `lcd`, `host`, and `all`. `type` contains 1..32 lowercase letters,
digits, dot, dash, or underscore. Text/action lengths are bounded. A board/LCD
target is converted to two printable 16-byte rows and sent through
`DISPLAY_TEXT`; every accepted message is also a source-tagged host event.

Network ingress does not trust a payload's claimed source. Raw IPC is tagged
`ipc`, REST is `rest`, standard WebSocket is `websocket`, Socket.IO is
`socket_io`, peer traffic is `bridge`, and inbound HTTP hooks are `webhook`; a
different claimed value is retained only as bounded `metadata.claimed_source`.
Authenticated messages also carry bounded `metadata.principal` and
`metadata.authentication`. This prevents a remote message from impersonating a
physical `board` event in text mappings.

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
last known-good configuration if a reload is invalid. This file is host-owned;
it never replaces the board's CRC-checked EEPROM record.

Authentication references, the named remote principal, origin lists, remote
policy, webhook enablement, outbound hooks, and bridge clients are read from
the current watched host configuration. Supported reference fields are
`ipc.auth_token_ref`, bridge `auth_token_ref`, webhook
`signing_secret_ref`, and webhook `secret_headers`. A replacement config is
published only after its references resolve. Listener address and URL-path
topology are established when the primary starts and require a host restart to
move the bound socket/path; policy or token changes take effect without
touching MCU EEPROM.

### Verification boundary

Automated tests cover the multiplexed raw/HTTP/WebSocket listener, Bearer and
header authentication, one-use browser tickets, replay/expiry/origin/transport
binding, missing-Origin and conflicting-credential rejection, pre-auth frame
suppression, remote capability denial, route-derived message
provenance, REST, Engine.IO v4/Socket.IO framing, correlated host-to-host calls,
forwarded event messages, every outbound webhook method, discovery packet
parsing, and secret-free SSDP advertisements. In addition to library-level
tests, package-independent raw RFC 6455 clients and raw peer servers verify
HTTP upgrades, client masking, JSON-RPC correlation/errors, Engine.IO
open/connect/ping/pong, both WebSocket roles, typed messages, and bridge event
forwarding. A raw native-protocol virtual board verifies that subscription
demand starts/stops only STATUS polling while the authenticated transport and
asynchronous events remain live, and that explicit Close/Open pauses/resumes it.

All automated listeners bind dynamic loopback ports and all executable tests
run from stable project-owned paths; they never open a physical serial port.
Actual mDNS/SSDP visibility across Windows Firewall/VLAN boundaries, TLS
reverse-proxy commissioning, physical USB unplug/replug notification timing,
and remote flash against physical hardware remain deployment tests rather than
claims made by this unit suite.

The Windows host uses native Credential Manager generic credentials scoped to
the current user and local machine for `os:` references; no subprocess is
involved. Other platforms currently fail explicitly for `os:` while portable
`env:` references remain available. Plaintext values are never silently
migrated or deleted. `config show` and `config secrets status` omit them, and
the CLI accepts new durable secret values only from a named environment
variable or standard input, never a command-line argument.

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
