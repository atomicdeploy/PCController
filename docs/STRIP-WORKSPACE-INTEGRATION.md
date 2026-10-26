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
