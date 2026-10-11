# Whole-effect repeat and selective capture checkpoint

Owning backend issue: #633. The paired application's issue and source-readiness
checkpoint are linked from that issue and the owning pull request on GitHub.

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

The follow-up ran the complete `internal/control` and `internal/appconfig`
stable-path suites without a test-name filter; both passed. Additional tests
reject raw seat commands absent from the active profile, ensure an all-off-only
selector does not capture on edges, and prove reading recording capabilities
cannot mutate the active take, including nested actions and capture modes.
The repository structure/link/privacy check also passed locally.

The subsequent review follow-up passed focused stable-path control/TUI tests
covering capture, whole-effect repetition, macro timing and automation actions.
Two Web test files passed all ten tests, and TypeScript `--noEmit` passed. These
include EN/FA disabled-entry-point DOM checks, full repeated catalogue span,
invalid timing, selection-change refresh, immutable start responses and an
explicit supported-control allowlist. DOM tests are not installed-window visual
acceptance. No test or source operation moved physical outputs.

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

The PCController Web recorder in `web/src/macro-library.tsx` now disables its
three unwired start/import actions, with localized console guidance, instead
of silently starting an empty take. The TUI shortcut in `internal/tui/macros.go`
offers capture help without prefilling a selectionless start command. Save,
discard and playback remain available. These are truthful interim guards, not
a completed visual recording chooser. Before recording-ready deployment, wire
explicit advertised-key/opcode selection with None/All into both interfaces
through the existing command carrier. The Web `controller.macro.*` strings are
local busy/action labels, not dedicated recording RPC methods. Do not invent new
RPC methods or restore default-all as a shortcut.

Capture metadata also explicitly filters controls to implemented exact
seat/relay/PWM keys. The current profile's controls return already has only
these kinds; RGB/display/strip/beep are peripherals, not semantic capture keys,
and remain opcode-selected. Synthetic unsupported-descriptor tests prevent
future presentation catalog expansion from silently enabling unsupported keys.
Starting a take returns an isolated state copy, and live selection changes emit
a structured recorder event so other clients request authoritative state.
Web/TUI catalogue durations now use the full planned repeated span, including
autonomous duration tails, without claiming a different executor completion
contract.

Full catalogue duration is planned span, not proof that the existing execution
lifecycle waits for every autonomous peripheral tail: runner completion and
`DurationUS` currently refer to the last command dispatch. Repetition spacing
does include the complete authored span. End-of-span lifecycle accounting for
trailing beep/display/PWM duration remains a separate host/MCU executor item;
do not add an output-altering fake wait opcode or claim this acceptance complete.

After source review and CI, merge the exact tested source; deployment stays
separate until the persisted-definition blocker and coordinated front-end
readiness are satisfied. Retain the earlier seat-cue protocol-success receipt
and pending human physical confirmation as a separate acceptance gate.
