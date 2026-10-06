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
   observe a physical release directly; Up uses the existing 250 ms packet-gap
   timeout. Click waits for the double-click window.
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

- `controller.rf.catalog {"read_board":true}`: learned records, learning status,
  recent receive activity, assignments, discovered application identities/actions,
  keyboard policy, board controls, effects and semantic board mapping options.
  Omit read_board for cached host activity polling without serial table reads.
- `controller.rf.binding.put {"binding":{...},"previous_name":"...", "allow_keyboard":false}`:
  validated, transactional watched-config mutation. Bindings are the existing
  `automations` entries matching `rf.gesture` and `source:rf`; selectors include
  `rf_code`, `rf_bits`, `rf_protocol`, `gesture`.
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
