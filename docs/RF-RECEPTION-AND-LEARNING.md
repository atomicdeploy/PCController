# RF reception and learning

RF identity is the complete code/bits/protocol tuple. Valid decoded packets are
forwarded even without an EEPROM assignment or an active learning session. Raw
reception is not authorization to run an output or persist an assignment.

`controller.rf.catalog` includes a bounded, volatile `receptions` collection
(maximum 256 tuples), host identity/generation and saved `preferences`.
Each receipt carries `received_count`, `last_seen`, pulse length and generation.
Raw `rf.receive` events carry the cumulative count in `metadata.received_count`;
coalescing presentation notifications does not lose packet counts. Counts mean
decoded receptions, including repeats, not physical button presses. Dialogs
snapshot a baseline at opening and reset it on closing/reopening or owner change,
not on a catalog refresh. Indicators stay active through consecutive packets
and expire after the shared 250 ms silence inference; disconnect clears activity.

Unlearned rows are dimmed and can be explicitly assigned on the host. The
`controller.rf.observed.add` operation can save a genuinely received waveform
on a capable board without learning mode. It assigns no action, preserves the
complete existing table, pins the transaction to the original generation and
verifies readback. Unobserved/old-owner tuples, full storage and unsupported
transactional replacement are rejected. Existing replacement operations share
one transaction lock with this operation.

Learning preferences are host-owned and shared by native, Web and TUI clients:
`timed: false`, `timeout_seconds: 60`, `continuous: false`. The saved timeout
is used only when timed learning is enabled. `controller.rf.preferences.set`
saves the complete settings object without starting/stopping learning or
changing RF assignments. Normal Learn sends `controller.rf.learn.start` with
no ad-hoc mode/duration; explicit diagnostic CLI/RPC options remain available.

Default one-code learning stops after a board `rf.learn.capture` confirmation,
not an unlearned reception. The host issues a bounded, original-generation
cancel, serialized with later learning operations. A failed cancel remains an
actionable failure, not a successful stop. A second waveform arriving before
the board processes host cancellation can still be captured; this is host-owned
stop-after-confirmation, not an autonomous board policy. Continuous mode leaves
learning active until explicit Stop, capacity exhaustion or the optional timer.

Shared firmware retains RF repeats during learning without rewriting EEPROM or
emitting duplicate learned events. This source change needs a firmware update
for continuous-learning reception visibility on physical boards. No extra RF
table, separate VirtualBoard implementation or timer polling was added.

The hard-coded A/B/C/D guided Web/TUI entry points are hidden. Their source is
retained for future pattern-profile authoring; it is not the default RF workflow.
