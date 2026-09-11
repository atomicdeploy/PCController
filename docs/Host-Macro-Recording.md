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

Both recorders exclude commands explicitly marked as automatic background
presentation, including the status-policy RGB animation, safety-status frames,
and restoration of the policy base after an overlay. MCU mode still captures
deliberately requested RGB and other queueable commands; it does not discard an
opcode merely because the status engine also uses it. MCU offsets come from
timestamped command acknowledgements, with a default tolerance of 2500 µs.
This provenance filter does not disable the status engine or its safety cues.

Times are monotonic host offsets from the first acknowledged action. They are
not MCU execution timestamps. Playback preserves an explicit leading delay,
then anchors its relative timeline once at the first successful acknowledgement.
Later steps retain their recorded offsets from that boundary; a slow first ACK
must not compress the recorded gaps. The clock is not reset after later ACKs.

`startup_delay_us` reports the first command's completion lateness against its
original deadline, including host scheduling, transport/ACK and observation
latency. It remains included in maximum error, violation count and the final
faithful result. Later steps still use the configured tolerance (100 ms for new
host recordings); startup anchoring does not conceal later overruns. CLI/TUI/Web
show startup delay separately so a completed macro is not mistaken for faithful
timing. Exact physical actuation timing still requires MCU execution evidence:
a delayed ACK can arrive after an output has already changed, and host-side
acknowledgement timing is not a hard real-time motor-control guarantee.

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
the UI needs no polling or manual page refresh for macro progress. The MCU
streaming runner also retains a 100 ms status-query fallback alongside its
execution/status-event handling; it is not an entirely poll-free board path.

Empty saves and storage failures retain the take for retry or explicit discard.
Recording cannot start during playback. Host cancellation is checked before
every dispatch, including simultaneous steps. Raw relay/motion opcodes still use the
host motion-permission check. Existing output interlocks remain authoritative.
Host playback and its cancellation cleanup stay bound to the session captured at
start. If USB disconnects or another board/session replaces it, playback fails
instead of redirecting output commands to the replacement.

Use `macro record start-mcu NAME` only for the explicit firmware-clock workflow.
Existing definitions without `mode` retain their MCU behavior. Neither this
guide nor a successful host playback closes the separate precise timing,
retained circular-buffer, loaded-motion and physical-input acceptance gates.

## Verified bounded MCU playback

The existing firmware-timed queue was exercised on the connected board with
three display-only commands at 0, 500000 and 1000000 µs. All 42 encoded bytes
fit in the 127-byte queue before playback starts. The firmware reported all
three commands executed in each of three runs, with no underruns, dispatch
errors or timing violations at the unchanged 2500 µs tolerance.

| Run | Maximum MCU ACK lateness | Result |
|---|---:|---|
| 1 | 1060 µs | 3/3, faithful |
| 2 | 884 µs | 3/3, faithful |
| 3 | 868 µs | 3/3, faithful |

These are MCU dispatcher-acknowledgement timestamps, not measured display/GPIO
edges. No firmware update was needed. This verifies small preloaded playback;
it does not prove streamed circular refill, physical/RF recording, cross-host
clock synchronization, loaded motion timing, or MCU reset/session-replacement
safety. Those remain separate acceptance work under the macro backlog.

To reproduce the isolated playback independently of recording, first choose an
unused ID/name and create a draft through the packaged Go controller. The
following PowerShell example uses ID 4 **only after verifying it is unused**:

```powershell
& '.\Tools\Controller\bin\controller.exe' exec macro list
& '.\Tools\Controller\bin\controller.exe' exec macro create 4 mcu-display-check verification blue
& '.\Tools\Controller\bin\controller.exe' exec config get macros
```

Check the returned array: an array index is not a macro ID. Continue with
`macros[4]` below only if index 4 is the newly created ID 4/name; otherwise use
its actual index. Never overwrite an existing user macro or the whole library.

```powershell
$macroDefinition='{"id":4,"name":"mcu-display-check","mode":"mcu","category":"verification","color":"blue","timing_tolerance_us":2500,"steps":[{"at_us":0,"kind":"display","destination":"segments","duration_ms":400,"text":"M001"},{"at_us":500000,"kind":"display","destination":"segments","duration_ms":400,"text":"M002"},{"at_us":1000000,"kind":"display","destination":"segments","duration_ms":400,"text":"DONE"}]}'
& '.\Tools\Controller\bin\controller.exe' exec config set 'macros[4]' $macroDefinition
& '.\Tools\Controller\bin\controller.exe' exec macro show mcu-display-check
& '.\Tools\Controller\bin\controller.exe' exec macro play mcu-display-check
& '.\Tools\Controller\bin\controller.exe' exec macro monitor
```

Before playback, `show` must confirm MCU mode, three display/`0x38` steps,
42 bytes and 2500 µs tolerance. Keep the host connected; firmware cancels an
active queue when the host goes offline. The normal cancel/error policy turns
relays and user PWM outputs off; `macro cancel keep` is an explicit cancel
option preserving their current values. Perform this diagnostic only when
that failure-safe behavior is acceptable. The same validated configuration
and macro commands are available through the existing command API/IPC paths.
