# RF application actions

PCController owns RF reception, learned EEPROM records, transmission and the
persisted assignments. Pealayer consumes `controller.rf.catalog` and uses the
same assignment APIs from its desktop and Web managers. No separate RF rules
are stored in Pealayer.

## Use

1. In Pealayer, open **Workspace → RF controls**, or **Hardware Monitor → RF controls**.
   The Web Hardware page has **RF controls → Manage**.
2. Open **Remotes → Learn buttons**, press the handset buttons, then stop learning
   and refresh. Reception and inferred gestures appear in **Receive activity**.
3. Choose **Assign**, name the assignment and choose a gesture. **Down** executes
   immediately once per press; **Up** executes after reception stops. Hold,
   repeat, click and double-click are also available. A radio receiver cannot
   observe a physical release directly; Up uses a 250 ms packet-gap inference.
   Timed firmware requires MCU-clock silence evidence, so delayed host dispatch
   does not create false releases; untimed sources use the legacy host clock.
   Click waits for the double-click window.
4. Add one or more ordered actions (maximum eight): advertised board control,
   effect, application command, RF transmission, keyboard key, external program,
   script or event. Choose **Application → pealayer (all) → Toggle** for play/pause.
   Values are required only by commands that take an argument, e.g. a seek time.
5. Save. An existing board action is shown separately; explicitly **Unassign
   board action** to replace it, or retain both deliberately. Editing an
   assignment updates that rule; it does not append another copy. Two enabled
   assignments cannot silently claim the same code/bits/protocol/gesture.

Host assignments need PCController running. Learned board mappings remain
board-owned and can work without Pealayer. Host assignments are suppressed during
RF learning; this does not rewrite or disable existing firmware mappings.

Keyboard and external-program actions execute on the **PCController host**, which
may differ from the computer displaying Pealayer. Keyboard actions preserve the
native OS executor and allowlist. The explicit Allow checkbox enables and
allowlists only the chosen keys. Keys are native virtual-key names such as SPACE,
F5 or PLAYPAUSE, or exact modifier chords such as `CTRL+SHIFT+S`. A chord is
allowlisted as one combination; it does not authorize its individual keys or
other combinations. Native input presses modifiers first and releases in reverse
order, including cancellation/error cleanup. Standalone modifiers remain denied.
Program/script arguments are separate argv entries, not an implicit shell string.
Program/script tasks use the existing bounded 30-second automation contract.
Enable **Launch independently** to start an application without waiting for it
to close or terminating it at the task timeout. This action reports a successful
process start, not that the external program completed its own work.

## Contracts

### MOSFET percentage assignments

Choose a channel from the advertised control list; do not infer channel numbers
from a 0–100 brightness range. MOSFET controls advertise `pwm.N.on`, `.off`,
`.toggle`, `.increase` and `.decrease`. On defaults to 100% logical intensity;
the optional shared [recall policy](PWM-ON-RECALL.md) restores confirmed nonzero
duty instead. Off requests
0%; relative actions use the channel's advertised `step_percent` (default 1%).
When that step is smaller than the hardware's representable change, a relative
action advances by one actual 12-bit duty quantum in the requested direction,
except at 0/4095 endpoints. This prevents repeated Increase/Decrease getting
stuck on unchanged readback, including the first code above zero. Actual
readback remains authoritative: the configured logical step is not a promise
of finer-than-hardware resolution, and absolute-percent conversion is unchanged.
The existing `controller.control.invoke {"action_id":"pwm.0.increase"}` carries
the same action chosen in RF assignments; no extra per-binding delta parameter
is required. Existing motion mappings and firmware RF mappings are not rewritten.

Configure the step through the existing channel configuration RPC, for example
`controller.pwm.channel.configure` with
`{"channel":0,"output_type":"lighting","icon":"lightbulb","curve":"gamma","gamma":2.2,"step_percent":2.5}`.
Omission in stored channel data uses the documented 1% default, not a migration
or persisted rewrite. In a channel configuration update, omitting the step
preserves an existing custom step; explicitly supply 1 to restore the default.
An explicitly supplied step must be finite, greater than zero and at most 100.
Use the appropriate type/curve for the actual output; motors do not receive a
lighting curve implicitly.

The shared CLI/TUI commands are `pwm on CHANNEL`, `pwm off CHANNEL`,
`pwm toggle CHANNEL`, `pwm increase CHANNEL` and `pwm decrease CHANNEL`; `pwm off` without a channel
still zeros all channels. `pwm configure CHANNEL TYPE CURVE [GAMMA [ICON [STEP_PERCENT [ON_BEHAVIOR]]]]`
configures the same channel data. Thus `pwm configure 0 lighting gamma 2.2 lightbulb 2.5`
sets a 2.5% logical step for a confirmed lighting channel. Raw duty remains
0–4095 and is not a percent step.

Actions use a fresh board read, a single curve conversion, an acknowledged
write and authoritative all-channel readback. The transport generation is fixed
throughout; reconnect, unavailable hardware, invalid readback or timeout cannot
fall through to another board or invent success. A failed readback after an
acknowledged write means the current output outcome is unknown. Readback events
update subscribed clients, and configured peripheral locks remain authoritative.
Host percentage changes are serialized with ordinary host PWM writes. Zeroing
requests cancel/supersede pending percentage changes without waiting on that
gate. Autonomous firmware/macro output changes are not a host-side atomic
compare-and-swap: an owned channel may independently change again after the ACK.
System ownership and physical acceptance remain separate from assigning an ID.

Source regression coverage includes logical gamma/linear steps, exact endpoints,
clamping, channel-step validation and advertised IDs, named/numeric command lock
resolution, read/write/readback failures, cancellation/zeroing preemption, and a
framed in-memory serial fixture proving read/set/ACK/readback plus rejection of
a replaced connection during the first read. These tests do not learn a real
code, modify EEPROM/configuration or establish physical output behavior.
Transport phases use the configured serial request timeout; the complete queued
action is bounded by three such phases, not an invented progress percentage.

- `controller.rf.catalog {"read_board":true}`: learned records, learning status,
  recent receive activity, assignments, discovered application identities/actions,
  keyboard policy, board controls, effects and semantic board mapping options.
  Omit read_board for cached host activity polling without serial table reads.
  Each sampled learned entry includes `board_assignment` with machine `action`,
  `target`, `behavior` and a current-name `label`; raw MCU fields remain available
  for diagnostics, not UI enum inference. PWM mapping `behavior_labels` explain
  that both Press/Toggle toggle 0/100%, while Momentary expires after 350 ms
  silence. These are autonomous firmware mappings, unlike host control actions.
  Optional `last_seen` is RFC3339 genuine board reception time matched by the
  complete code/bits/protocol waveform. A bounded 256-waveform session cache
  survives catalog/activity-ring refresh, not process restart; unknown is
  omitted, never inferred from refresh, learning completion or transmission.
  The existing immediate `rf.receive` event (`source:board`, `time`, `rf_code`,
  `rf_bits`, `rf_protocol`) updates visible dates directly; coalesce rendering,
  never request a serial learned table for each packet. Catalog reads may expose
  structured `board_error_detail` with the same RPC error code/message/data.
  An acknowledged PWM write with failed confirmation uses code -32010 and data
  `kind:confirmation_unavailable`, `resource:pwm`, `write_acknowledged:true`,
  `retryable:false`; retain its detail and do not blindly repeat the mutation.
- `controller.rf.binding.put {"binding":{...},"previous_name":"...", "allow_keyboard":false}`:
  validated, transactional watched-config mutation. Bindings are the existing
  `automations` entries matching `rf.gesture` and `source:rf`; selectors include
  `rf_code`, `rf_bits`, `rf_protocol`, `gesture`.
  Missing, malformed or unadvertised control actions are rejected before any
  persistence or keyboard-consent change. RPC code -32602 carries the human
  message "Choose an available control action before saving this assignment"
  and `data` with `kind:validation_error`, `resource:rf_binding`, the exact
  `binding.actions[N].action_id` field and reason `required`, `invalid_format`
  or `not_advertised`. Show the human message and retain field detail for
  technical disclosure; do not infer validation state from a raw error string.
- `controller.rf.binding.remove {"name":"..."}` removes only that RF assignment.
- Existing `controller.rf.learn.start/status/cancel`, `.map`, `.remove`, `.clear`
  and `.transmit` remain authoritative. Map/remove/clear use board readback.
- `app` actions contain `app_kind`, `app_target`, optional `app_value`. They use
  the live advertised-action coordinator, frozen target identities, expiry,
  delivery nonce and actual consumer acknowledgement. Missing/rejected/expired
  targets produce a failed automation result, never a claimed completion.
- `control` actions contain an advertised `action_id`; they reuse semantic
  invocation and board-profile locks. `effect` uses a stable effect reference in
  `macro` and the unified `effect play` engine. Existing legacy macro rules remain
  intact but are not offered as a second Effects concept in the new manager.

The catalog's learned records and host bindings are intentionally separate
storage views of the same RF button. Neither API silently modifies the other.
Wire acknowledgements do not prove over-the-air delivery to a physical receiver;
real handset/receiver/transmitter validation remains a physical acceptance gate.

## Integration acceptance checkpoint

The 6 October 2026 local installation uses host revision `3486fc1a` and Pealayer
revision `60dd5bd`. Both were deployed using their own update APIs, including
graceful shutdown and hash verification, not copied over running executables.

VirtualBoard RF receive/learning/readback/TX, Pealayer playback acknowledgements,
Web assignment rename/reassignment and native Windows F23/program actions were
exercised. Exact key/chord consent and release cleanup passed the focused Go
runner. RF activity now has an independent newest-20 buffer, unaffected by busy
media telemetry and excluding bridge echoes. Persisted application/semantic
automation delivery is initialized before clients are served, not by the first
RPC call.

After host update `op-ff82c9543489a12e` completed, Pealayer remained running with
PID 17236. Receive/gesture event 379 triggered automation completion event 406
and changed real playback. Selected-coordinator identity admission and push
heartbeats repaired stale command/subscription connections during this restart.
No outcome-uncertain mutation is blindly replayed.

Temporary bindings/learned test entries were removed, original keyboard policy
and board connection were restored, and existing user effects were preserved.
Physical RF acceptance remains open in #73. Cafe's update API did not respond;
native screenshot capture was unavailable. See the Pealayer guide for captured
Web dark/light/receive evidence and the exact executable hashes:

[Pealayer RF controls and verification](https://github.com/ToghrolTP/pealayer/blob/fix/hardware-monitor-layout/docs/RF-CONTROLS.md).
