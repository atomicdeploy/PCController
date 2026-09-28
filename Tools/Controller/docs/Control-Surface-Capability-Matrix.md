<div align="center"><a href="../../../README.md"><img src="../../../docs/assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Control-Surface Capability Matrix

This is the durable reachability contract for the PC host. It answers two
questions for every device domain: which single implementation performs the
work, and through which public surfaces can a caller reach it?

## Shared dispatch contract

All registered controller commands execute in the same `shell.Engine`. The
following adapters do not reimplement relay, motion, PWM, RF, macro, settings,
programming, or safety behavior:

| Consumer surface | Discovery | Execution |
|---|---|---|
| Interactive shell and TUI prompt | `help` and completion | enter the command directly |
| Direct CLI or a secondary CLI process | `help` | command arguments are forwarded to the primary serial owner |
| Go library | `Client.CommandCatalog()` | `Client.Execute(ctx, command)` |
| C-compatible shared library | `{"operation":"commands"}` | `{"operation":"execute","command":"..."}` |
| NDJSON/HTTP/WebSocket JSON-RPC | `controller.command.catalog` | `controller.command.execute` |
| REST | `GET /api/commands` | `POST /api/command` |
| Socket.IO WebSocket subset | `rpc` event with `controller.command.catalog` | `command` event or `rpc` event |
| Host-to-host bridge | nested `controller.command.catalog` | nested `controller.command.execute` |

Standard WebSocket accepts every JSON-RPC method after authentication. Its
`controller.subscribe` independently streams human `events`, continuous
changed-only `state`, explicit `debug`, raw `opcodes`, and/or paced `status`.
Ordinary UI activity feeds subscribe to `events`; live previews additionally
consume `state`, so frame/measurement traffic cannot spam or evict one-shot
activity. Subscription is observation, not a second command implementation.

Native HTTP/WebSocket clients retain Bearer and `X-PCController-Token`
authentication. A native client following an unauthenticated discovery record
first verifies the responder-bound `/api/auth/server-proof` HMAC before sending
that bearer. Browser WebSocket and Socket.IO clients exchange that durable
header credential at `POST /api/session/ticket`, then use the returned
short-lived one-use `Sec-WebSocket-Protocol` ticket on a credential-free URL.
Every subsequent capability decision retains the authenticated principal and
transport in the security audit context.

## Device and host domains

“Generic” below means the shared command path above. A typed API is an
additional convenience that still ends in the same runtime/native protocol.

| Domain | Shared shell/CLI command | Go library convenience | Typed JSON-RPC / REST | Live result or event path | Default remote capability |
|---|---|---|---|---|---|
| MCU EEPROM settings | `settings`, `silent`, `stream` | Generic; decoded `Settings` is present in `Snapshot` | Generic command | refreshed settings/status; no PC config is written | read-only query: `read`; mutation: `board_commands` |
| Menu and physical front panel | `menu`, `display` | `SetMenuPage`, `SetMenuPageByName`, `MenuCatalog`, `MenuLayout`, `SetMenuLayout` | `controller.menu.*`; `/api/menu/catalog`; `/api/menu/layout` | changed-only segment events update the shared cache and every channel immediately; snapshot reads are initial/manual/recovery only | query: `read`; navigation/layout/display: `board_commands` |
| PC-hosted menu configuration | TUI configuration page; generic menu/display interaction | `ReplaceHostMenuDirectory`, `PushHostMenuContent`, `HostMenuState` when firmware advertises support | `controller.host_menu.*`; `/api/host-menus` | host config reload plus front-panel events | query: `read`; config write: `host_configuration`; board overlay: `board_commands` |
| Relays R1–R8 and side motion | `relay` | `SetRelay`, `ToggleRelay`, `SetMotionSide`, `AllRelaysOff` | Generic command | relay event and `active_relays` status mask | `board_commands` |
| PWM/MOSFET and illumination | `pwm`; Settings has a dedicated illumination editor | `SetPWMChannel`, `AllPWMOff`, `PWMValues`, `Illumination`, `SetIllumination` | `controller.illumination.get/set`; generic PWM command remains available | one host-owned observer reads channel 11, publishes changed-only `illumination.changed` state, and exposes persisted mode/endpoints, door-selected target, and applied PWM to every client | query: `read`; mutation: `board_commands` |
| Status RGB and WS addressable LEDs | `rgb`, `strip` | `SetStatusRGB`, `SetStatusRGBBase`, status-effect scheduler; `strip` remains generic | Generic command | output scheduler state plus board status/events | effect query: `read`; mutation: `board_commands` |
| Buzzer, silence, and melodies | `buzzer`, `silent`, `melody` | `PlayTone`, `StartMelody`, configured melody scheduler, stop/status methods | Generic command | output scheduler state and buzzer-busy telemetry when advertised | query: `read`; play/stop: `board_commands`; create/delete: `host_configuration` |
| 433 MHz receive, guided A/B/C/D learn, map, and transmit | `rf`; TUI `W` guide | `TransmitRF`, learn start/status/cancel, list/remove/clear/map methods | typed `controller.rf.*` list/learn/map/remove/clear/transmit methods; generic command remains available | immediate RF receive/gesture/learn lifecycle events; mapping-required drives identity readback | list/status: `read`; mutation/transmit: `board_commands` |
| Macros and automation | `macro`, `automation` | `SetMacros`; shared basic-host or precise-MCU recorder/player | Generic command | mode-tagged macro step/start/cancel/complete events and scheduler state | macro query: `read`; playback/cancel: `board_commands`; create/delete/record: `host_configuration`; host automation run: `host_automations` |
| INA219, DS18B20, status telemetry | `status`, `temp` | `Status`, `Temperatures`, `SubscribeStatus`, history/timeline methods | `controller.status`, `controller.temperatures`, history RPCs; snapshot REST | WebSocket/Socket.IO status subscription and asynchronous events | `read` / `events` |
| Cooperative I2C and LCD | `i2c`, `display` | `I2CTransfer`, `ScanI2C`, `RescanLCD`, LCD prompt/priority methods | `controller.lcd.*`; generic I2C command | LCD presentation state and device events | LCD config: `host_configuration`; bus/display operations: `board_commands` |
| Reset and serial lifecycle | `ports`, `open`, `close`, `reconnect`, `reset` | `ListPorts`, `Connect`, `Open`, `Close`, `PulseResetFor` | port/reset RPCs and snapshot | connection lifecycle events | `connection_control` or `reset` |
| Build, bootloader, backup, restore, flash | `toolchain`, `boot`, `program` | toolchain bootstrap/sync conveniences; complete operations remain Generic | artifact/update RPCs plus dedicated `controller.restore.flash`; `/api/restores/flash`; Generic command | programming lifecycle, backup manifest, verified write, and fresh authenticated `HELLO` | `programming` **and** `connection_control` |
| Idle/Running application state | `program-state` (`run-state`) | `ProgramState`, `SetProgramState`, `AcquireProgramState` | `controller.program_state.get/set`; `GET/PUT /api/program-state` | `program.state` and `program.state.sync`; telemetry `program_running` | query: `read`; mutation: `board_commands` |
| Host/OS status, virtual keys, power, monitor brightness | `os` | `HostSystemStatus`, `PressVirtualKey`, `RequestPowerAction` | `controller.os.*`; `/api/os/status`; `/api/os/key`; `/api/os/power` | audited OS action/timeline events | `read`, `virtual_keys`, or `power_actions` |
| Read-only Windows host facts | `os facts [system\|computer\|firmware\|storage\|serial\|list]` | shared fixed-catalog provider through Generic command | `controller.os.facts*` and `controller.host.facts*`; `GET /api/os/facts` | bounded snapshot with profile/class/columns/rows/truncation/source/time | `read`; no arbitrary query or write surface |
| Typed text and LCD messages | message API rather than free-form shell text | `SendTextMessage` | `controller.message.send`; `/api/messages`; webhooks/Socket.IO | source-tagged message/event and optional LCD presentation | `messages` |

PWM channel names are stable logical aliases: `user1..user11` map to channels
0..10, `enclosure`/`illumination` to 11, `power` to 12, and
`status-r`, `status-g`, `status-b` to 13..15. Numeric channels 0..15 remain
accepted.

## Important boundaries

- MCU settings remain MCU-owned and CRC-checked in EEPROM. Host JSON/YAML/TOML
  configuration remains host-owned; neither silently replaces the other.
- Capability bit 24 now means `PROGRAM_STATE`. It is not a host-menu-overlay
  bit. The current firmware does not advertise the anticipatory volatile
  host-menu directory/content protocol, so those typed calls return a clear
  unsupported error while built-in menus and PC-side menu configuration remain
  usable.
- On every authenticated connection/reconnection, and after each host state
  change, the host reasserts one-byte `PROGRAM_STATE` when capability bit 24 is
  present. A two-second state heartbeat keeps the firmware's five-second host
  watchdog accurate without polling telemetry. Older firmware is not probed.
- Remote policy remains deny-by-default for every mutation. Read-only generic
  commands are classified separately from their mutating subcommands; enabling
  `read` does not enable relay, motion, PWM, RF, reset, programming, or OS
  actions. Raw `query` and `write` diagnostics require the stronger
  `programming` plus `connection_control` gates because their payload can
  bypass ordinary opcode-specific intent classification. Running a configured
  host automation has its own `host_automations` gate because an automation may
  launch an allow-listed PC-side script; ordinary board-command permission is
  insufficient.
- WebSocket and Socket.IO event subscriptions expose the same status/event
  records regardless of whether a change originated at the physical keys, RF,
  automation, IPC, REST, another host bridge, or the local UI.
- The C-compatible library owns its own serial connection. If a primary host is
  already running, external consumers should use IPC/JSON-RPC instead of
  opening a competing library handle.

## Automated acceptance

The Go tests enforce that every row has at least one registered shared command,
that command metadata is discoverable and detached from handlers, that generic
library dispatch reaches every domain, that RPC and REST expose discovery and
execution, and that remote read/mutation classification retains safe defaults.
Program-state tests use an in-memory serial transport to verify exact `0x45`
frames on authenticated attach and state changes without touching a COM port.

Physical output behavior, bootloader transfer, and live network commissioning
remain hardware/deployment acceptance tests; catalog reachability is not
misreported as physical validation.
