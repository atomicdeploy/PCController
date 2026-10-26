# Strip workspace integration

The Flutter effect dialog now supports typed strip programs, the three original
presets, Fade/Pulse envelopes and an offline pixel preview. Unapplied technical
JSON disables typed editing; invalid programs prevent saving. Strip preview uses
the current `effect play ID COUNT FPS` command, matching native Pealayer.

Manual diagnostics adapt PakarController's deliberate full-frame probe contract
to a linear 1–100-pixel strip. Opening, selecting and moving the cursor never
publishes. Each lit probe consumes one arm bound to the selected peer; the arm
expires after 30 seconds. Connection changes revoke it. Off and each subsequent
probe are separate explicit requests; no failed/uncertain mutation is replayed.
The complete frame zeros every unselected pixel within its selected count.

`strip frame` now configures the exact frame length before staging/show, under
the same strip lock. A failed configuration cannot stage or display the frame.
Continuous animations retain their configure-once path and existing pacing.
This host change must be deployed with the diagnostics clients.

PCController's current AVR contract does not expose RGB frame readback. These
controls therefore report command acknowledgement, not PakarController-style
board RGB observations or optical verification. Unknown physical output remains
unknown until the operator checks it; source tests do not establish that result.

Validation: targeted Go manual-frame and streaming regressions; Flutter preset,
envelope, bounds, full-frame clearing and unapplied-draft fixtures. Runtime and
Android package acceptance remain open until an exact candidate is deployed and
tested. Native Pealayer owns the corresponding typed designer and probe UI in
its strip-workspace work. Playback-clock correction is coordinated separately.

The live 15-second seat test submitted 150 Up requests through Pealayer at 10 Hz.
No framing/CRC/reset delta was observed; final telemetry reported both directions
Stop and all relays off. Identical Up requests coalesce while already moving, so
this does not imply 150 physical relay cycles. Human motion acceptance is pending.
The three existing strip definitions retain their stable IDs and are grouped
with Fade/Pulse in Strip demos with three-pixel defaults for the current bench.
Private settings backups and live logs are not included in this repository.

## Prepared playback and readback contention

A replay on host ci.1647 exposed a PWM_GET (0x15) response wait occupying the
serial slot for 88 ms. The strip configure cue reached dispatch about 2 ms late,
then exhausted its unchanged 50 ms deadline behind that read. Framing and CRC
counters remained unchanged: this is a separate scheduling conflict.

A subsequent ci.1648 replay exposed internal display refresh (0x38) blocking
resume after the PWM-only guard. The current solution reserves the whole serial
channel during an explicit paused `prepare_play` handshake. Pealayer must wait
for matching `play_ready` feedback before unpausing. Queued/background work is
rejected before writing; in-flight work drains within the original arming budget.
The PWM status RPC retains the observation's time, generation and revision.
Pause, cancellation, stale intent, terminal faults and disconnect release the
reservation. Deferred health probes cannot trigger DTR recovery. Optional board
presentation runs through the timeline owner only in sufficiently large gaps;
dense playback defers it honestly. See [the complete contract](Prepared-Media-Timeline.md).

The new host and Pealayer publisher must be deployed together. Final synchronized
replay, dense/sparse playback and human physical acceptance remain open until
verified on that exact pair. Neither the PWM-only intermediate update nor the
standalone strip soak establishes synchronized playback acceptance.

Deployment evidence must include live compiled version/source identity from
the current listener. A prior updater reported a new disk digest while an old
TUI subsequently took primary ownership. That old process was gracefully closed
with operator approval and the ci.1647 listener verified separately. A disk
artifact manifest alone does not prove which executable image owns the board.
