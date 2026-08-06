<div align="center"><a href="../../../README.md"><img src="../../../docs/assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Hosted Front-Panel Menus

PCController can temporarily lend the physical four-key, TM1637, and 2x16 LCD
front panel to menus defined by the PC host. These definitions live only in the
host configuration; they are not copied into MCU EEPROM.

## Open and control a hosted menu

Keep the primary PCController TUI running, then use its prompt, a secondary CLI,
JSON-RPC, REST, or WebSocket command bridge:

```text
host-menu list
host-menu open host
host-menu open macro-library
host-menu status
host-menu key K2 press
host-menu key K4 press
host-menu key K3 hold
host-menu close
```

From another terminal, route the same commands to the serial-owning primary:

```console
bin\controller.exe exec host-menu open host
bin\controller.exe exec host-menu status
bin\controller.exe exec host-menu key K2 press
bin\controller.exe exec host-menu close
```

On firmware advertising HELLO capability bit 19, holding physical K4 while the
board is on its Door page requests the default hosted menu. While captured:

- K1/K2 select the previous/next item.
- K3/K4 decrease/increase numbers, booleans, and option values with rollover.
- K4 enters a submenu or selects an enabled action. On a read-only, disabled,
  or HOST-only text item it is a true no-op: no read callback, value, or revision
  changes. The host emits `menu.action.denied` and requests a short error cue;
  MCU EEPROM Silent mode remains authoritative.
- Hold K3 to go back or close the root hosted menu.
- A guarded action requires selection followed by a deliberate K4 hold.

Before capture, the host cancels RF learning and explicitly stops both motion
groups. If the host disappears, firmware releases its capture after its bounded
offline timeout and restores the board default page.

## Configuration

The same `host_menus` shape is supported in JSON, YAML, and TOML and hot-reloads
through the normal config watcher. Each menu has an immutable host node ID,
parent, four-character label, LCD title/content, brightness request, edit
visual, flags, and items. Item types are `readonly`, `text`, `number`, `bool`,
`select`, `submenu`, and `action`. A select item may use static `options` or an
`options_source`; the built-in `macro.library` source resolves the current
ID-sorted macro list in memory without duplicating definitions in the menu
configuration. Editing an active label/title/content updates
the TUI preview and physical/virtual display immediately; an inactive edit
still emits the normalized definition event but does not steal the display.

```json
{
  "host_menus": {
    "default_menu": "host",
    "request_gesture": "door-hold-k4",
    "display_duration_ms": 1500,
    "session_timeout_ms": 120000,
    "menus": [
      {
        "id": "host",
        "label": "HOST",
        "title": "HOST",
        "items": [
          {
            "id": "poll",
            "label": "POLL",
            "title": "Polling ms",
            "type": "number",
            "value": "200",
            "min": 100,
            "max": 5000,
            "step": 50,
            "read_action": "pc.ui.status_interval_ms",
            "write_action": "pc.ui.status_interval_ms"
          },
          {
            "id": "profile",
            "label": "PROF",
            "title": "Profile",
            "type": "select",
            "value": "quiet",
            "options": [
              {"label": "Quiet", "value": "quiet"},
              {"label": "Fast", "value": "fast"}
            ],
            "write_action": "command:profile ${value}"
          }
        ]
      }
    ]
  }
}
```

Built-in read/write actions cover host/device status, current date/time,
IP/API state, application title, polling period, LCD prompt mirroring, DTR
reset-on-reconnect, and primary monitor brightness. A `shell:` or `command:`
action routes through the existing
validated controller shell. The default System Actions page contains `BRIT`,
Lock, Suspend, Hibernate, Restart, and Shut down. Every OS write/action has
`guarded: true`; independent `os_actions.power` and `os_actions.brightness`
policies are disabled by default and remain authoritative after a physical
front-panel confirmation. Power actions also enforce the configured allow-list
and confirmation token. The brightness item reads 0..100 and changes in
policy-bounded steps through DDC/CI or native WMI for an integrated laptop
panel. Responses identify the selected backend and display. Changes remain
in steps of five; an adapter or display driver supported by neither backend
returns one combined, actionable unsupported result.

## Macro library on the physical front panel

The default HOST menu links to three config-backed pages without adding AVR
flash or EEPROM data:

- `macro-library` (`MACR`) selects from the file-watched macro library, shows
  the selected name/ID/step count, and starts guarded playback;
- `macro-recording` (`REC`) shows live recording state and starts, saves, or
  deliberately discards a recording. A physical start uses an automatically
  unique `panel-YYMMDD-HHMMSS` name that can be renamed later from the host;
- `macro-playback` (`RUN`) shows live MCU step/elapsed progress and provides
  ordinary safe cancel plus a guarded keep-output cancel.

The endpoints are `host.macro.selection`, `host.macro.selected`,
`host.macro.play`, `host.macro.recording`, `host.macro.record.start`,
`host.macro.record.save`, `host.macro.record.discard`,
`host.macro.playback`, `host.macro.cancel`, and
`host.macro.cancel.keep`. They adapt to the same command engine and
`MacroRunner` used by CLI, TUI, REST/JSON-RPC, WebSocket, and WebUI. Macro
definitions stay in host configuration; the board receives only the active
bounded queue. File-watcher changes replace the selector options immediately,
preserve a still-valid selection, and fall back to the lowest configured ID
when the selected macro is removed.

## Protocol contract

- HELLO bit 19 authorizes host capture.
- HELLO bit 23 authorizes the board-owned EEPROM menu visibility/order record.
  Stable built-in IDs are dense `0..13`; the exact schema-2 payload is 11 bytes:
  `[2, 14, visible-mask LE, seven packed rank bytes]`.
- `DISPLAY_TEXT` target 3 writes four segment characters plus two padded LCD
  lines and captures physical keys. Its metadata packs a four-bit host state
  and a 12-bit editable value.
- `DISPLAY_TEXT` target 4 releases capture.
- `FRONT_PANEL` schema 2 provides exact raw segments, LCD content, physical key
  mask, capture state, host state, and editable value for the TUI/API mirror.
- Physical key events remain ordinary asynchronous `EVENT` frames and are
  consumed by the same manager as TUI/IPC virtual keys.

Capability 24 defines a richer volatile hierarchy/content protocol and is
implemented by the Go host and VirtualBoard for forward testing. The physical
ATmega328P build does not advertise it: host-owned pages therefore use the
capability-19 capture path. On that capability fallback the host approximates
`blink`, `edit-dim`, and `alternate` with bounded 400 ms-or-slower segment
updates and cancels the scheduler on reload, release, or disconnect. Existing
capability-19 frames cannot carry a per-page brightness command, so the board's
global EEPROM TM1637 brightness remains authoritative. No claim is made that a
host-only brightness value reaches current physical firmware.

Firmware implements `MACRO_START`, `MACRO_STEP`, and `MACRO_CANCEL`
(`0x39..0x3B`) as a bounded RAM queue driven by the MCU clock. The host keeps
that queue filled ahead of execution and publishes name, ID, elapsed time,
duration, step, buffer health, timing error, and completion/cancellation through
the hosted-menu and event surfaces. Relay, motion, PWM, buzzer, display, RF,
and other accepted opcodes are scheduled through their ordinary safety paths.

## First-run setup synchronization

The setup screen does not finish on an animation timer. It waits for an
authenticated application HELLO and final STATUS. HELLO bit 20 makes STATUS bit
12 authoritative as buzzer busy. If the board melody is already idle, the host
plays the configured `ui.welcome_melody` through its output scheduler and waits
for completion. Capability-limited firmware never has raw bit 12 reinterpreted; it receives
a readiness grace before the host melody. Offline, missing-ready, and audio
paths have a bounded 30-second wait followed by an explicit warning the user
may acknowledge.
