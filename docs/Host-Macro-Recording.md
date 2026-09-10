# Host macro recording and playback

| Capability | Owner | Availability |
|---|---|---|
| Named library, categories, recorded command deltas | 🖥️ Host | CLI, TUI, Web, IPC, API, WebSocket/Socket.IO command path |
| Host scheduler and acknowledged output commands | 🖥️ Host | Default for new recordings; 100 ms alpha tolerance |
| Firmware-timed queue | 🔌 Board | Explicit MCU-mode macros; firmware capability required |
| Physical/RF-origin capture, exact MCU timing and offline recovery | 🔬 Acceptance pending | Issues #44 and #74 in the [canonical requirements backlog](Requirements-Backlog.md) |

New host recordings capture accepted **relay, motion, PWM/MOSFET, beep,
display/message, RF transmit and addressable-strip** commands. Automatic status
RGB animation and stream/settings housekeeping are intentionally excluded.
Rejected commands are not recorded. This mode records host command evidence;
it does not claim to capture physical-key or incoming RF actions.

Times are monotonic host offsets from the first acknowledged action. They are
not MCU execution timestamps. Playback schedules each step against one epoch,
then waits for the normal command acknowledgement. The live result reports
maximum error and tolerance violations rather than claiming perfect timing.

## Quick record / save / play

Use the packaged controller. `exec` addresses the already-running host, keeping
one serial owner. Do not operate relays, motors or PWM loads until they are safe.

| Command | Purpose |
|---|---|
| `controller.exe exec macro record start cinema-demo examples green` | Start a named host take |
| `controller.exe exec display segments --duration 500ms TEST` | Send a display action (or use the normal UI controls) |
| `controller.exe exec macro record status` | Inspect captured count and any error |
| `controller.exe exec macro record save` | Persist the take; `record stop` is equivalent |
| `controller.exe exec macro record discard` | Explicitly discard instead |
| `controller.exe exec macro list` | List saved IDs, names, modes and categories |
| `controller.exe exec macro show cinema-demo` | Inspect ordered steps before playback |
| `controller.exe exec macro rename cinema-demo cinema-ready` | Rename without changing ID or steps |
| `controller.exe exec macro category cinema-ready cinema` | Set the category |
| `controller.exe exec macro play cinema-ready` | Play the saved definition using its recorded mode |
| `controller.exe exec macro monitor` | Combined playback and recorder snapshot |
| `controller.exe exec macro cancel` | Cancel and switch relays/PWM off; report cleanup failures |
| `controller.exe exec macro cancel keep` | Explicit opt-in to preserve current outputs |

Pass command words as separate CLI arguments; quote only an individual name or
text argument containing spaces, not the entire command after `exec`.

The Web macro panel lists saved macros, records with a chosen name/category,
plays a selected definition after its physical-output confirmation, and updates
recording counts/playback progress from macro events. The TUI Automations page
uses the same library and state, including when it is a secondary remote IPC
client. Its console accepts the identical commands. `controller.snapshot` and
`/api/snapshot` expose `macros.library`, `macros.recording` and `macros.playback`;
command transports use the existing `controller.command.execute` route rather
than a second macro engine. All clients refresh host snapshots on macro events;
no board polling or manual page refresh is required for macro progress.

Empty saves and storage failures retain the take for retry or explicit discard.
Recording cannot start during playback. Cancellation is checked before every
dispatch, including simultaneous steps. Raw relay/motion opcodes still use the
host motion-permission check. Existing output interlocks remain authoritative.
Playback and its cancellation cleanup stay bound to the session captured at
start. If USB disconnects or another board/session replaces it, playback fails
instead of redirecting output commands to the replacement.

Use `macro record start-mcu NAME` only for the explicit firmware-clock workflow.
Existing definitions without `mode` retain their MCU behavior. Neither this
guide nor a successful host playback closes the separate precise timing,
retained circular-buffer, loaded-motion and physical-input acceptance gates.
