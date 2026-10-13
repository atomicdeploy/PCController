# Bounded buzzer mirror playout

Owner: Linux/board mirroring issue #106.

## Follow-up: short-note and display fidelity

The 50 ms receiver cushion alone did not cover arrival bursts beyond that
cushion: the old device-clock deadline deducted lateness from a fresh note,
and could discard a short note entirely. A fresh observation now reanchors
its source with the same 50 ms cushion if its mapped start has already passed,
including a fresh first note delayed by scheduler handoff. The original duration
is retained; following notes resume
device-clock cadence from that anchor. This is bounded playout recovery, not
sample-exact acoustic synchronization or permission to replay old audio.

Dispatch preserves Runtime's local monotonic receipt timestamp (also assigned
when remote events are ingressed), rather than restamping after integration
work. Observations queued longer than the existing 250 ms freshness bound
are not rebuffered. Explicit Stop still cancels/flushed its source immediately;
generation replacement still discards prior buffered notes. Mirror enablement,
board sound policy, native kernel finite-tone limits and cleanup are unchanged.

The WebSocket state limiter previously shared one timestamp between RGB, PWM
and segment frames. An RGB frame could consume the budget for the next digit
or blink frame. Continuous domains now have independent budgets, while changed
segment edges bypass decimation. Pealayer requests immediate changed state
and uses its hardware revision for peer delivery rather than a 100 ms sample.
No extra board polling, synthetic frames or interpolation is introduced.

Go host compilation and focused package vet passed. Pure regression cases were
added for fresh 10 ms recovery, scheduler handoff, stale queued notes and cross-domain frame
budgets; tests/CI were not run in this iteration at the user's request.
Pealayer passed offline cargo check with existing warnings. Deployment and
physical short-note/segment fidelity must be recorded separately below.

## Cause and behavior

The receiver anchored the first device-clock note at local receipt time, then
preserved subsequent board-clock cadence. Any extra transport delay on a later
note was deducted from its remaining audible duration. For example, 25 ms of
extra arrival delay shortened an 80 ms note even on the native speaker backend.
The native backend was observed selected on `asus@server`; an external helper's
process startup is not established as the cause of the user's report.

Timed notes now have a bounded 50 ms playout cushion. Subsequent device-clock
spacing and note durations remain unchanged for jitter within that cushion.
Untimed legacy observations are not delayed. The existing 250 ms timestamp-skew
and five-minute gap guards remain; expired notes are still skipped rather than
replayed or stretched. This adds 50 ms initial receiver latency, not a claim of
sample-accurate acoustic synchronization.

A second interruption path was found in source: Linux capability probing uses
`KDMKTONE(0)`, which stops sound. Reconciliation now reuses an unchanged, already
ready native backend instead of probing it when unrelated integration or
presentation settings change. First setup, changed native configuration and
failed-backend recovery still perform real resolution; no unavailable backend
is fabricated as ready.

An explicit zero-frequency/zero-duration Stop bypasses playout, cancels its own
active note and flushes that source's pending/replacement buffer immediately.
Generation changes discard old-source buffered notes before reanchoring. Other
sources and enabled mirroring remain untouched. Kernel-owned finite tone limits
and the existing cancellation cleanup remain in place.

## Verification and delivery boundaries

Stable-path Windows full `internal/hostbridge` package passed, including device
clock wrap/restart, delayed-arrival duration preservation, source isolation,
serialized replacement, buffered Stop and reconnect-generation regressions.
`go vet ./...` passed from `Tools/Controller`.

No physical tone was played for this pass. A 12-second read-only source event
sample contained no changed segment/note events, so it cannot measure active
transport jitter. Installed runtime replacement, human listening, longer jitter
bursts and physical two-board acceptance remain explicit gates. No Cafe firmware
write, Linux privilege/service configuration change or Pealayer GUI-launch
bypass occurred.
