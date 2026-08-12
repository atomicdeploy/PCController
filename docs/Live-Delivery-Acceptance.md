<div align="center"><a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Live delivery acceptance

This checklist converts the current alpha product requirements into observable
release gates. A source change, green unit test, or successful package build is
not enough on its own: the exact packaged host and exact firmware candidate
must satisfy the relevant live gates below.

## Non-negotiable delivery rules

| Area | Acceptance condition | Tracker |
|---|---|---|
| One delivery line | All accepted changes are integrated on the canonical delivery pull request before it is merged. Superseded WIP branches are not merged wholesale. | #101, #174 |
| Current assets | The executable reports the exact source and embedded-Web identities that were just built. A connected browser reloads once when those identities change; the service worker never serves a stale application bundle. | #101, #163 |
| Push-first state | Every connected WebSocket/Socket.IO/TUI/IPC client receives output, connection, settings, front-panel, and macro state changes without manual refresh. Snapshot reads are limited to initial connection, explicit user refresh, detected-gap recovery, or legacy-firmware fallback. | #154, #164, #165 |
| One command path | WebUI, TUI, CLI, REST, JSON-RPC, IPC, WebSocket, Socket.IO, bridges, scripts, and native integrations invoke the same normalized command and event semantics. | #124, #162, #164–#166 |
| Truthful capabilities | A UI control is shown only when its host and board capability is available. Disconnected clients retain host controls but do not display stale board values. | #101, #125, #159 |
| Evidence | Every live candidate records source, firmware, executable, embedded-Web, package, backup, and test identities. | #155, #157 |

## Web shell and navigation

- Search displays only the detected client platform shortcut and keeps the
  modifier, separator, and `K` visually compact.
- Live/online states use meaningful icons and distinguish host transport from
  authenticated board connectivity.
- The actual shared application mark is the normal favicon/app icon; a letter
  mark is fallback-only. Runtime status may decorate that icon without
  replacing it.
- The compact sidebar has correctly sized icon buttons, no horizontal scroll,
  and a correctly positioned expand control. Its connection status supports
  click, context-menu key, right-click, focus-loss dismissal, Escape, and
  viewport-safe placement.
- Application preferences are a tabbed dialog separate from board EEPROM
  settings. Theme, language, audio, keyboard help, notifications, and other
  quick-header controls can be shown or hidden; Settings itself is always
  reachable and language is a dropdown.
- Peripheral workbench destinations form a closed, nested menu. Arbitrary
  route/page text is rejected in favor of canonical dropdown choices.

## Dashboard and cards

- Refresh is absent while the event socket is open and the authoritative value
  is less than one second old. It reappears after socket loss or staleness,
  including inside card menus.
- The hero/connection card exposes open, close, reconnect, port discovery, and
  device selection. Device selection is shown only when it is meaningful and
  includes the enumerated USB name.
- Card actions exist; user-layout cards can be reordered, collapsed, hidden,
  restored, and persisted without mutating another client's personal layout.
- Metric labels precede and align with metric details.
- Temperature uses named tabs while keeping the selected chart mounted and
  visible. Supply-voltage and thermal charts use semantic domains: nominal
  12 V fills the useful voltage range and about 52 °C reads visually hot rather
  than near the floor.
- Charts reject obvious noise, smooth display-only transitions without changing
  source measurements, preserve a stable animation direction, and avoid
  restarting an animation on every current sample.
- Relay and MOSFET names, descriptions, order, and motion-side labels use the
  shared peripheral catalog across every control surface. Inline editing is
  normalized and persisted by the host rather than becoming a Web-only alias.

## Outputs and event feedback

- Clicking any descendant of a relay switch toggles it immediately through the
  canonical command path. Optimistic state is reconciled or rolled back by the
  authoritative event; it does not add a second command or perceptible delay.
- Relay toasts are emitted for physical-front-panel and RF sources. The
  initiating Web/TUI/CLI control, macro, and automation paths do not receive a
  redundant state-change toast.
- Parsed HELLO, STATUS, `action.applied`/device-action frames, RGB render frames,
  telemetry, and app-instance heartbeats update state or debug streams and do
  not enter the ordinary activity/toast feed.
- Seven-segment, buzzer, status RGB, settings, relay/MOSFET, and connection
  changes are event-driven. Two open browser tabs must converge without either
  tab calling Refresh.
- The PWM mixer reserves scrollbar gutter; its scrollbar and controls never
  overlap.
- Hover audio fires at hover entry, remains optional, and never substitutes for
  visual feedback.

## Typed collections and firmware/device surfaces

- Typed tables have no unexplained inline-start gap, show semantic header
  icons, a visible resize indicator, a correctly anchored header menu, and
  focus-loss/Escape dismissal for menus and dropdowns.
- Column selection supports drag reordering, uses the column's header icon, and
  does not expose the deferred width slider. Filters include event type and
  user-adjustable stream/severity rules.
- Device and host-identity cards use live values or explicit pending states,
  expose applicable read/actions, and do not render contradictory
  `Unavailable`/`Offline` text while connected.
- Firmware/update copy is dynamic and explains the current state. A refresh
  spinner has a concrete in-flight operation; static filler text is omitted.
- Public REST/JSON-RPC names are versionless. `/api/v1` and similar versioned
  aliases are rejected rather than advertised.
- A board-settings write applies the runtime effect immediately, is read back,
  and separately reports durable EEPROM persistence; “accepted” is not treated
  as “persisted”.

## Macro recording and playback

| Gate | Required evidence |
|---|---|
| Sources | Host commands, raw opcode exchange, physical keys, and learned RF actions enter one generation/board-bound recorder. |
| Actions | Relay on/off, motion side/direction/stop, MOSFET/PWM, beep start/stop, display/message, and supported LED operations reuse their ordinary validated opcodes. |
| Timing | MCU acceptance timestamps produce wrap-safe ordered deltas. Playback reports its epoch, step progress, timing error, underruns, cancellation, and terminal state live. |
| Retention | The bounded board ring cannot be overwritten by a competing host start; capture/fetch uses an identity, and local replay/cancel preserves retained data until explicit clear/ack. |
| Library | Macros can be listed, named, renamed, categorized, colored, shown, deleted, played, cancelled, and monitored through the shared host library. |
| Surfaces | WebUI, TUI, CLI, REST, JSON-RPC, IPC, WebSocket/Socket.IO, bridges, and scripts expose the same library and live state rather than separate implementations. |
| Long recording | With a host connected, capture drains/continues beyond one MCU ring. Offline bounded capture reports truthful truncation/capacity. |
| Live acceptance | A harmless mixed macro is recorded with deliberate timing gaps, saved, played, monitored from a second client, and finishes with zero dispatch errors/underruns and timing within the declared bound. Loaded motion and audible buzzer tests require the operator's explicit physical observation. |

## Messaging and actions

- One bounded message envelope carries ordered targets, severity, correlation,
  sync/async delivery, expiry/deduplication identity, and optional described
  actions.
- A caller can target native toast, Web toast, TUI notice, or an explicit
  combination. All target adapters report delivery outcome; unsupported or
  disconnected targets are not silently reported as delivered.
- Actions return through the same correlation identity and authorization/
  capability path. Messages can be initiated from CLI, TUI, REST, JSON-RPC,
  IPC, WebSocket, Socket.IO, bridge, script, or browser console.
- The shared command/event fabric is also used for board operations such as
  buzzer, display read/write, relay/MOSFET, and macro state; “toast” is one
  presentation adapter, not a second control architecture.

## Exact live-candidate run

1. Fetch the canonical remote head into clean edge and handoff worktrees.
2. Run repository, generated-contract, Web type/test/build, full Go/vet,
   native firmware tests, AVR budget/stack, Windows resources, UPX, runtime
   identity, and C-ABI gates.
3. Create a complete flash/EEPROM/settings backup before any board mutation.
4. Program the exact verified firmware and migrate/apply EEPROM in the guarded
   lifecycle; verify flash, EEPROM, HELLO, capabilities, and settings readback.
5. Install/launch the exact packaged host and verify local plus configured LAN
   aliases return the same current bundle identity.
6. Exercise two simultaneous clients, reconnect after USB removal/reinsertion
   (including a changed COM number), pushed presentation state, output controls,
   settings durability, macro capture/playback/monitoring, and event filtering.
7. Preserve hashes and tracker evidence, then merge the canonical delivery PR.

Physical loaded motion, relay/MOSFET wiring, sound, sensor correlation, and
visual LED behavior still require the operator to be present and confirm the
corresponding load is safe to actuate. Those gates must remain visibly open
until that observation occurs.
