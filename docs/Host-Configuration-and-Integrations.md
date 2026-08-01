# Host Configuration and Integrations

The Go host is the serial owner, user interface, automation engine, programmer
launcher, and optional network bridge. Start it from the repository root after
building:

```console
Tools\Controller\bin\controller.exe tui
Tools\Controller\bin\controller.exe web
```

The normal TUI keeps the UART open and reconnecting, but does **not** assert
DTR or reset the board merely because the application started. Use the visible
Open, Close, and Reset controls or the matching commands when you intend those
actions.

## Keep PC and MCU settings separate

There are two independent persistence domains:

| Owner | Stored data | Storage |
|---|---|---|
| AVR firmware | sound, display/light/PWM options, menu default, motion-door policy, RF records, reset telemetry, and supported board automations | ATmega328P EEPROM |
| PC host | USB selection, TUI appearance, histories, network endpoints, hotkeys, notifications, webhooks, PC macros/scripts/automations, and programmer paths | JSON, YAML, or TOML file |

Editing the host file never silently copies its values into EEPROM. Board
Settings explicitly reads or writes the MCU record; App Settings edits the PC
file. Erasing development EEPROM does not remove the remembered Windows USB
identity or any PC automation.

## Configuration path, formats, and live reload

The default Windows file is:

```text
%AppData%\PCController\config.json
```

Select another file with `PCCONTROLLER_CONFIG` or `--config FILE`. The filename
extension selects `.json`, `.yaml`/`.yml`, or `.toml` decoding; unknown future
fields are ignored for semantic forward compatibility, while known fields
retain strict type/range/relationship validation. Unknown fields are not
available to the older process and may be omitted if that older binary
explicitly rewrites the whole file. A newer binary recognizes and re-saves its
current fields normally.
These commands show which file is active without opening the board:

```console
Tools\Controller\bin\controller.exe config path
Tools\Controller\bin\controller.exe config show
Tools\Controller\bin\controller.exe --config controller.yaml config validate
```

Long-running TUI, shell, monitor, and IPC sessions watch the file with
`fsnotify`. Atomic replacements are debounced and a safety check covers file
systems that miss a replacement event. A bad edit is reported and the last
valid configuration remains active.

The checked-in [configuration example](../Tools/Controller/examples/config.example.json)
contains the full schema. Important safe defaults are:

- `connection.reset_on_reconnect` is `false`;
- discovery and remote listening are opt-in;
- the serial application connection remains alive even when there is no
  telemetry subscriber;
- `safety.motion_door_policy` is `always`, matching the requested factory
  policy;
- Console-to-LCD mirroring is opt-in and debounced.

## USB selection, ownership, and reconnect

Selection accepts a COM ID, friendly/human name, `VID:PID`, USB serial number,
or Windows PnP instance ID. The shipped defaults identify the observed CH340
as VID `1A86`, PID `7523`, and friendly name `USB-SERIAL CH340`; flags and the
host file can override all three. After a successful native `HELLO`, the host
stores the stable identity and prefers it on the next launch. A unique match is
selected automatically; ambiguous matches are shown for selection.

The first long-running host becomes the primary process and is the only process
that opens the serial port. Later CLI or UI instances use its IPC service. An
explicit Close pauses reconnect until Open is requested. On Windows, registry
change notifications from the Plug-and-Play serial map drive arrival/removal;
the fallback retry is used only when native notification cannot be established.
Connection lifecycle events are available to the TUI, scripts, IPC, WebSocket,
and host automations.

The serial driver opens with DTR and RTS inactive. If
`reset_on_reconnect=true`, only a genuine physical reappearance may issue one
DTR pulse; `HELLO` retries cannot create a reset loop. An explicit reset also
uses DTR only and releases it afterward. TCP simulation links do not implement
modem-control reset.

## TUI pages and the mirrored front panel

The TUI is organized by task instead of hiding everything in one prompt:

- Dashboard for grouped, adaptive-unit measurements and connection state;
- Outputs for R1-R8, motion, PWM/MOSFET sliders, RGB, and audio controls;
- Menus/Front Panel for the active page, exact four-digit segment state,
  2x16 LCD, submenu/mode, and four remote keys;
- Board and App Settings for the two persistence domains;
- RF, Programming, Automation, Events/Graphs, and Console pages.

Mouse and keyboard actions share the same command path. Remote key injection
must include down/up/gesture semantics rather than merely changing a local
preview. The firmware snapshot capability determines whether raw segment bytes
and physical key state are authoritative; a host-only fallback is clearly
identified as such.

Console mirroring is controlled by `ui.mirror_prompt_to_lcd`. Input,
completion, and result context is rate-limited. Important door, HOT, motion,
relay, RF, and error overlays temporarily take priority, then the prompt is
restored. When a physical LCD is present but the host heartbeat expires, the
firmware fallback is:

```text
PC OFFLINE
CONNECT USB
```

`CONNECT USB TO PC` is 17 characters and may be scrolled slowly instead of
being written as a single 16-cell row.

## Embedded web control center

`controller.exe web` is a complete primary operating mode. It does not launch
the Charm TUI, allocate a Bubble Tea program, or depend on terminal input. The
headless process still owns USB discovery/reconnect, serial correlation,
automations, host menus, global hotkeys, notifications, integration managers,
and the same guarded command dispatcher. `--no-open` keeps that process and
HTTP service running without opening a browser.

The embedded responsive app includes the live dashboard and graphs, direct
relay/motion/PWM controls, an advanced peripheral workbench, Activity history,
local-device and loopback-data workspaces, and PC/board settings. Commands from
the browser terminal enter the primary dispatcher; status plus asynchronous
board, host, and global page-action events return over the persistent
WebSocket. Incoming event lines also appear in the bounded terminal transcript,
so command and event traffic remain visible together without losing the full
filterable timeline.

System-wide hotkeys remain registered in web-only mode. A validated
`app.page` action is delivered to the TUI queue when one exists and independently
published to browser subscribers, so a stalled or absent terminal consumer
cannot starve the web clients. High-risk radio transmission, macro/automation,
reset, raw/programming, remote-call, and host-power command families receive an
additional browser confirmation; backend capability policy and programming
guards remain authoritative.

### Searchable macro workspace

Page `8 Automate` is a first-class macro workspace rather than a shortcut to
the Console. Its ID-sorted, file-watched library shows each macro's name,
category, user color, step count, duration, and whether it is playing or being
recorded. Press `/` and type any ID (decimal or hexadecimal), name, category,
color, display label, LCD message, or step kind/text/destination to filter;
`Ctrl+U` clears and `Enter` finishes the search. Arrow keys select a row and
`Enter` or `P` starts it. Library rows and every bordered action are also
mouse-selectable.

The action keys are:

- `N` creates a named empty draft; `R` starts recording board operations;
- `S` saves the current recording and `D` discards it;
- `P` plays the selected macro; `C` cancels and safely turns affected outputs
  off, while `K` explicitly cancels and keeps their current states;
- `I` shows the selected definition, `X` requires a second `X` before deleting
  PC-side metadata, and `A` opens the automation rules list.

Playback reads the same `MacroRunner` instance used by shell, IPC, and API
commands. The page therefore reports live macro identity, elapsed/duration,
step progress, MCU circular-buffer fill out of 127 bytes, accepted bytes,
last/maximum device timing delta, configured tolerance, violations, underruns,
dispatch errors, lifecycle, and final faithfulness. Recording offsets come from
MCU acknowledgement timestamps, not variable USB/network arrival time. Macro
definitions remain PC configuration; only the active timing queue occupies AVR
RAM. The deterministic preview contains a safe representative library for UI
inspection and never opens serial.

## Global hotkeys

Windows builds use `RegisterHotKey`; registration is process-global and does
not require the TUI to have focus. A binding has a unique name, an enabled
flag, a chord, and an ordinary controller command. Commands can therefore
inject a board key, stop outputs, select a menu, run a macro, or control the
primary application through the same validation path used by Console and IPC.

Supported key names include letters, digits, F1-F24, arrows, Home/End,
PageUp/PageDown, Enter, Escape, Space, media, and volume keys. Bare keys are
restricted to F1-F24; other chords require Ctrl, Alt, Shift, or Win. Repeats
are suppressed by the operating system. A conflict is reported in App Settings
and does not prevent the controller from starting. Non-Windows builds report
the feature as unsupported.

The embedded web control center receives the same validated `app.page` action
stream, so `Ctrl+Alt+P` opens Events in whichever UI is active instead of being
consumed only by the terminal model. Page-local shortcuts remain disabled in
editable fields and during IME composition; press `?` in the web app for the
current map. The ordinary low-level keyboard-control bindings remain opt-in
and retain their focus-loss/fail-safe release behavior.

## Local device and data hub

Both companion applications are disabled by default and are host-side only:

```json
"integrations": {
  "local_device": {
    "enabled": false,
    "base_url": "http://192.168.1.50"
  },
  "data_hub": {
    "enabled": false,
    "base_url": "http://127.0.0.1:8080"
  }
}
```

The local device accepts only HTTP(S) roots on loopback, private, or link-local
networks and explicitly local hostnames. Its manager implements PCController's
own fixed Local Device v1 capability, snapshot, action, and event-stream
contract. Requests are bounded, redirects and environment proxies are disabled,
and only sanitized capability/snapshot inspection is exposed to browser clients.

The data hub must be a loopback root. It is deliberately service-neutral: the
browser supplies only a relative resource path while the host owns the upstream
origin. HTTP bodies and WebSocket upgrades stream without whole-payload
buffering, byte-range and validator semantics are preserved, and PCController
authorization, cookies, and forwarding headers are removed before forwarding.
The bundled Persian UI uses the unmodified Vazirmatn font under OFL-1.1.

## Guarded system actions and monitor brightness

The PC-owned `SYS` front-panel submenu exposes Monitor Brightness (`BRIT`),
Lock, Suspend, Hibernate, Restart, and Shut down. No firmware bytes or MCU
EEPROM are used for these items. Power requests require the two-step/hold menu
guard, `os_actions.power.enabled`, an allow-listed action, and the configured
confirmation token. The power policy is disabled by default; its default
allow-list is only Lock, Suspend/Sleep, and Hibernate, so enabling the policy
does not silently permit Restart or Shut down.

Brightness uses the Win32 DDC/CI physical-monitor APIs on the primary display.
Reads are harmless and always permitted; writes require
`os_actions.brightness.enabled` and the configured `min_percent..max_percent`
range, which defaults to 0..100. Laptop panels, docks, adapters, and monitor
drivers that do not expose DDC/CI return an unsupported error without blocking
the controller. The backend is injectable and tested without changing the
developer's real display.

```text
os policy
os brightness get
os brightness set 55
os brightness-policy range 20 80
os brightness-policy enable
os power-policy enable CONFIRM
os power-policy allow restart
```

All accepted/denied brightness and power attempts publish host events. Because
the manager reads the current Store value on every action, JSON/YAML/TOML file
watcher changes apply immediately to CLI, TUI/front-panel, IPC, and WebSocket
callers. The default remains deny-safe.

## Desktop notifications

Important events can produce Windows `ToastGeneric` notifications; routine
telemetry never does. A toast can open a relevant page or offer a safety action
such as stopping outputs. Action URIs use the `pccontroller://` scheme and
must return to the already-running primary process before executing a command.
The primary then applies the same authentication, logging, and board safety
guards as every other command source. The TUI reports whether the notifier and
action handler are actually available; accepting toast XML alone is not proof
that a button activation is installed.

## Local API and primary IPC

The default service is `127.0.0.1:8787`. One listener multiplexes:

- newline-delimited JSON-RPC 2.0 for local processes and pipes;
- REST/JSON endpoints;
- standard WebSocket JSON-RPC and subscriptions at `/ipc`;
- a bounded Engine.IO v4 / Socket.IO WebSocket adapter at `/socket.io/`;
- an optional inbound webhook.

Use loopback without a token only for the same PC. A non-loopback listener is
accepted only with deliberate remote enablement, an authentication token of at
least 24 characters, and an origin allow-list. Query-string tokens exist for
browser WebSocket clients that cannot set a header, but the Bearer or
`X-PCController-Token` header is preferred because URLs may be logged.

Authentication and authorization are separate. The default
`ipc.remote_policy` permits authenticated read/event subscriptions only;
messages, board commands, host configuration, port control, reset,
programming, shutdown, virtual keys, power/display actions, bridge calls, and
configured host-automation execution are independently opt-in. Remote
programming also requires connection-control permission. Every denied or authorized mutating attempt is source-tagged in
the host timeline, and generic `controller.execute` commands are classified so
they cannot bypass a narrower gate.

The exact methods, routes, frames, Socket.IO subset, and examples are in
[Protocol and Network API](../Tools/Controller/docs/Protocol-and-Network-API.md).

## Discovery and remote bridges

Optional mDNS/DNS-SD announces `_pccontroller._tcp.local.`. Optional SSDP uses
`urn:pccontroller-org:service:bridge:1`, answers `M-SEARCH`, sends alive/byebye
notifications, and points to `/healthz`. Advertisements expose the instance
name, endpoint paths, and the fact that authentication is required—never the
token. Multicast discovery only finds a service; it grants no control rights.
Firewall, VLAN, multicast, and corporate-network policy can still prevent
discovery even when the service is healthy.

Configured WebSocket clients let one host subscribe to another host and make
correlated calls through `controller.bridge.call`, `/api/v1/bridges/call`, or
the `bridge call` shell command. They retry with bounded backoff and preserve
the rule that exactly one local primary owns the attached serial port. The
target host independently checks its remote policy and ordinary safety guards;
recursive bridge calls are rejected. Remote programming still closes that
primary's UART, runs the guarded toolchain/Urclock workflow exclusively, and
requires a fresh application `HELLO` afterward.

## HTTP, webhooks, WebSocket, and Socket.IO

The REST service provides GET for health/snapshot reads and POST for JSON-RPC,
commands, typed messages, and inbound webhook delivery. Configured outbound
webhooks support GET, POST, PUT, PATCH, and DELETE. GET/DELETE receive encoded
event fields in the query; body methods receive event JSON or a bounded
template. Timeouts, response-size bounds, concurrency limits, loop prevention,
and non-2xx event reporting keep a slow endpoint from blocking serial control.

The standard WebSocket endpoint is bidirectional: clients submit JSON-RPC and
subscribe to `events` and/or `status`; the server pushes correlated responses,
events, status samples, and errors. A configured host can also act as an
outbound WebSocket client/bridge.

Socket.IO is not an alias for ordinary WebSocket. The implemented compatibility
surface uses Engine.IO 4 with `transport=websocket`, connection and ping/pong
packets, and Socket.IO events for `subscribe`, `unsubscribe`, `message`,
`command`, and `rpc`. Long-polling, namespaces, rooms, binary attachments, and
the broader Socket.IO ecosystem API are outside this bounded adapter.

## Typed, actionable text messages

The common envelope is:

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

Allowed sources identify client, server, bridge, board, LCD, host, IPC,
webhook, or WebSocket origin. Targets are client, server, bridge, board, LCD,
host, or all. The type is a short machine-readable label; text and optional LCD
rows remain human-readable. Sending to board/LCD uses the bounded native
display command and emits the same source-tagged host event.

Network routes assign the authoritative source (`ipc`, `websocket`, `bridge`,
or `webhook`) instead of trusting a caller-supplied `board`/`host` identity. A
different claimed source is retained as bounded metadata for diagnostics.

`action` is descriptive and is never executed merely because it appeared in a
message. An enabled host text mapping may turn a matching source/target/type/
text pattern into an ordinary controller command. That command is logged and
runs through authentication and motion/output safety; no untrusted message is
implicitly treated as shell text.

## RF presentation and record identity

The board's learned-record ID is a compact EEPROM slot, not a permanent remote
identity. PC metadata must therefore use the tuple `(code,bits,protocol,pulse)`
as its key. Names such as “Side A Up”, categories, colors, notes, and search
tags belong to PC configuration; the MCU retains only the compact learned
record and action mapping it needs offline.

The required RF editor will use one host display-format option for hexadecimal
or decimal values everywhere and a searchable action picker. Reordering must
be an explicit read/propose/write/read-back transaction that preserves tuple-
keyed metadata. At this documentation checkpoint these presentation, search,
and reorder tools remain acceptance work; the current learn/list/map/remove
commands are the fallback. See the RF section of the
[Project Checklist](Project-Checklist.md) before relying on record-ID order.

## Programming data, backups, and current settings transfer

The existing host can coordinate Arduino CLI, AVRDUDE/Urclock, USBasp recovery,
and explicit flash/EEPROM backups. The final durable workflow has stricter
acceptance requirements that are not yet all complete:

- every ordinary compile/flash operation is initiated by the host UI/API;
- before a write, Urclock captures flash and EEPROM into the host data
  directory; a hidden advanced USBasp fallback is used only when bootloader
  access cannot work;
- a failed backup blocks programming unless the operator gives an explicit,
  logged override;
- live settings export is distinct from parsing an offline EEPROM image;
- named Intel HEX regions can be inspected and boundedly patched with checksum,
  address, before/after SHA-256, retained-original, and read-back verification;
- graceful host exit writes an atomic diagnostic snapshot containing board
  identity, last telemetry/settings/menu, reset/connection state, operation
  state, and artifact hashes.

None of these PC files is the MCU's active configuration. The board continues
to own EEPROM, and a settings change reaches it only through a specific native
command or a deliberate programmer write.

No unpublished-build migration/version chain is retained in firmware or host
code. The file-only tools accept the current unversioned semantic settings
record, require a complete validated raw-backup manifest, and reject every
other width/layout explicitly:

```console
Tools\Controller\bin\controller.exe eeprom inspect --backup-manifest BACKUP\manifest.json
Tools\Controller\bin\controller.exe eeprom export --backup-manifest BACKUP\manifest.json --output SETTINGS.hex
Tools\Controller\bin\controller.exe eeprom import --backup-manifest BACKUP\manifest.json --settings SETTINGS.hex --output EEPROM-RESTORE.hex
Tools\Controller\bin\controller.exe eeprom restore --backup-manifest BACKUP\manifest.json --output EEPROM-ORIGINAL.hex
```

The current settings artifact is exactly 29 value bytes plus CRC-8 at EEPROM
addresses `0x0020..0x003D`. Export emits only that sparse region. Import
overlays it on the validated 1,024-byte backup so RF records, reset journal,
and every unknown byte remain unchanged; restore reproduces the original full
EEPROM image. Outputs are hashed and never overwritten. These commands do not
open serial or write a device; an actual EEPROM write and readback remain
separate, explicit programming operations.

## Measurement demand and history

Serial remains connected so asynchronous door, BT Audio, key, RF, reset, and
output events are received immediately. Periodic status requests are demand
driven: a visible monitoring/graph view, script, automation, IPC subscriber,
WebSocket subscriber, or remote bridge holds a subscription. When the last
subscriber leaves, polling stops without closing UART.

Display visibility and precision are PC settings. Values use adaptive SI units,
and age text is hidden for samples newer than 500 ms to avoid a distracting
`0/100/200 ms` cycle. Status history defaults to 24 hours; important events are
also written to a bounded timeline file for review after the TUI restarts.

## Commissioning boundaries

Automated tests can prove parsing, routing, safety checks, reconnect state,
mock TUI rendering, and network framing. They cannot prove that Windows toast
registration survived installation, multicast crosses the current network,
COM18 reconnects without an adapter-specific reset, or a real LCD/RF/relay load
behaves correctly. Keep those items as hardware/platform validation in the
[Project Checklist](Project-Checklist.md) until observed on the target system.
