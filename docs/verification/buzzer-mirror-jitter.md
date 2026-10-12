# Bounded buzzer mirror playout

Owner: Linux/board mirroring issue #106.

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
