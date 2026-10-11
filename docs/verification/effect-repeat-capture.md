# Whole-effect repeat and selective capture checkpoint

Owners: [PCController #633](https://github.com/atomicdeploy/PCController/issues/633)
and [Pealayer #141](https://github.com/ToghrolTP/pealayer/issues/141).

The current alpha sequence contract owns finite whole-effect repetition. Step
repeat fields are removed and rejected, with no alias or automatic migration.
The shared recorder defaults to no selected evidence; explicit all, advertised
control keys and numeric opcode arrays select evidence before persistence.
Targeted relay/motion/PWM steps preserve excluded channels during normal
playback. Safety cancellation/cleanup keeps its existing scope.

## Source verification

Stable Windows runner compiled the public library, CLI, host, appconfig and
control packages. Focused tests passed for whole-effect mixed-sequence order in
host/MCU compilation, repeat overlap/zero/overflow/capacity rejection, none/all,
partial seat/relay/PWM capture, MCU edge deltas, raw-opcode OR selection, numeric
JSON arrays/range validation, inherited append metadata, strict retired-field
rejection, media execution and existing capture/append/effect round trips.
Some public/host packages had no test matching this filter: their compilation
is evidence, not runtime acceptance. Full CI remains the merge gate.

Local verification used installed Go 1.27.1 with the module directive lowered
only during the stable runner pass, then restored to 1.27.2 before committing.
No production command, application restart, firmware upload or actuation was
performed in this source pass.

## Deployment blocker and next action

A read-only check found existing persisted definitions with **nontrivial
per-step repetition**. Do not deploy the rejecting contract over them, delete
their fields, or replace interleaved step repeats with a guessed whole-effect
repeat. Coordinate explicit reauthoring of those definitions preserving their
exact scheduled operations and durations, with human acceptance. Keep the live
configuration intact until that work is authorized and verified; this checkpoint
does not implement a product-owned migration or legacy reader.

Pealayer owns its native/Web editor and chooser. Backend capability/state JSON
is documented in [Effects System](../Effects-System.md). Other front-end chooser
wiring and installed end-to-end acceptance must be verified separately. Appending
adds evidence to the base authored sequence; whole-effect repeat settings remain
on that same definition. Board-retained filtering is relay-only, and excluded
edges can still consume the board ring capacity.

After source review and CI, merge the exact tested source; deployment stays
separate until the persisted-definition blocker and coordinated front-end
readiness are satisfied. Retain the earlier seat-cue protocol-success receipt
and pending human physical confirmation as a separate acceptance gate.
