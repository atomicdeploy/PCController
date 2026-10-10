# Prepared media timelines

Companion: [Pealayer playback contract](https://github.com/ToghrolTP/pealayer/blob/fix/hardware-monitor-layout/docs/prepared-hardware-timeline.md).

Registered consumers can call `controller.media.timeline.prepare` while their
media clock is paused. The volatile plan accepts `client_id`, positive `revision`,
`cues` (`id`, `reference`, `time_ms`, `duration_ms`), `actions` (`id`, `time_ms`,
existing `MacroStep`), and `max_lateness_ms` (default 50, allowed 5–250).
MacroStep relay/PWM/motion targets use the existing zero-based native contract.
Do not send human-facing Rn values as native indices.

Preparation uses the existing effect catalog, sequence compiler/expander and
strip renderer. It resolves profile-bound effects, validates motion permission,
freezes exact bytes in bounded RAM, and acknowledges their SHA-256 hash and
authenticated board generation. Revision reuse with different content fails.
Limits: 4,096 cues, 65,535 compiled commands, 8 MiB prepared-command budget,
32-bit media milliseconds, bounded strip pixels and frame rate. Capacity errors
never drop a tail of the effect.

An effect explicitly configured for MCU execution is rejected by this host
timeline until absolute media-epoch scheduling is advertised by firmware. It is
never silently downgraded. Choose host/automatic execution explicitly if the
host-scheduled timing contract is acceptable.

`controller.media.playback.update` now also accepts `epoch` and `plan_revision`.
Playing against an unprepared/faulted revision or unarmed epoch is rejected.
A paused update must first arm the cursor. Clock extrapolation retains a Go
monotonic anchor; public UTC timestamps are not used as scheduling clocks.

`controller.media.timeline.get` and playback snapshot `timeline` return:
revision/hash/generation/state, armed epoch/clock sequence, command count and ACK
count, explicitly rebased steps, last command/deadline, last/max ACK lateness,
last/max dispatch lateness, last/max board-request round-trip, device ACK
timestamp where advertised, and sticky failure reason. Total ACK lateness is
the end-to-end host-clock result. Dispatch lateness measures lateness against
the admitted/projected clock; it alone does not distinguish scheduler delay
from an incoming position correction.
ACK round-trip identifies transport/board response time. The same ledger
publishes `media.timeline` state events. ACK count advances only on success.

Timing diagnosis uses measured values, without relaxing the execution guards:

| Snapshot field | Meaning |
| --- | --- |
| `clock_timing.feedback_interval_ms` / `max_feedback_interval_ms` | Monotonic gap between accepted playing updates |
| `clock_timing.position_correction_ms` / `max_forward_correction_ms` | New position minus the previous position projected at its previous rate |
| Timeline `last_worker_gap_ms` / `max_worker_gap_ms` | Gap between executor iterations, including any intervening command work; not CPU attribution by itself |
| Timeline `last_clock_read_ms` / `max_clock_read_ms` | Elapsed read of the admitted clock, including lock wait and scheduling |
| Timeline `last_board_read_ms` / `max_board_read_ms` | Elapsed connection-generation read, including lock wait and scheduling |

`clock_timing` appears in both playback and timeline snapshots. Clock maxima
reset across pause/resume, owner, revision or epoch changes; they do not carry
an old test's playing gap into a new test. Worker maxima belong to the prepared
plan. The timeline retains the clock evidence sampled at the failure even when
a later paused playback update resets its own clock counters. Dispatch failures
include these measured phases in their error; this adds no per-tick events.
None of these host measurements prove a physical actuator edge.

Board snapshots copy connection facts under their lock, then expand effect
catalogs outside it. Library/configuration providers and presentation sorting
must not hold that lock: a waiting connection writer would also block subsequent
executor readers. The regression test blocks a catalog provider and verifies
connection writer/reader access remains available without dropping the catalog.

Clock expiration (250 ms while playing), session replacement, command failure,
or lateness over the admitted limit faults execution rather than silently skipping
commands. Dispatch consumes the same admitted budget: a command dispatched 12 ms
late under a 50 ms contract receives at most 38 ms for request plus ACK, never a
fresh second 50 ms window. Cleanup is attempted against the original board generation even if the
failed command might have applied. Cleanup failures/transport loss cannot prove
physical outputs are off. E-STOP cancels the media executor as well as existing
standalone effects. Conflicting live actuator commands are rejected during play.

Pause releases outputs. Resume restores latched relay/PWM/motion/strip values but
does not replay earlier RF, beep, menu or display one-shots. Seeking requires a
paused new epoch; skipped historical steps are classified as intentional rebase.
Effect definitions are frozen for a prepared run; editing them requires pausing
and preparing a new revision.

## Remaining acceptance gates

This is host-scheduled, precompiled execution, **not** firmware-epoch scheduling.
ACK lateness measures the received/projected host media clock, not physical
video presentation or output edges. Network uncertainty is not eliminated.
Absolute MCU queue start/seek/rate/lease support, physical timing instrumentation,
and loaded low-end-machine acceptance are still required for hard timing claims.
No firmware was flashed or physical timing certification performed for this pass.
