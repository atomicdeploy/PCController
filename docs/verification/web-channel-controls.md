# Web channel search, PWM drafts and motion geometry

The command palette previously searched page titles and static commands only.
It now reads the profile-resolved peripheral catalog on open and relevant catalog
events, searches current names/keys/groups, and reveals/focuses the matching
control. Search never actuates an output. Hidden controls are not offered.

Browser UI configuration and PWM readback previously used global base names,
ignoring the active board profile's presentation. They now share that resolved
catalog. The mixer displays the operator's current name together with its physical
MOSFET/user-PWM identity and stable key, so a renamed output is still identifiable.

The mixer combined full PWM reads/write ACKs with narrow selected-channel telemetry
that could predate a completed write. That older sample could replace the displayed
draft. Also, a failed older mutation rolled back the draft even when a newer desired
value was queued. Full serialized PWM reads/ACKs now own mixer confirmation;
pointer/keyboard/number editing protects drafts between ACKs, final release flushes
the latest intent, and older failures cannot roll back a newer draft. Exact raw
values are submitted once rather than converted back to percent and through the
curve again. No mutation retry, fabricated board confirmation, new polling loop
or timing/safety relaxation is introduced.

At narrow widths the motion identity spanned a four-column grid, but the following
three buttons landed in `1fr auto auto`. Hold Up inherited all spare width. A
separate equal-three-column action group now keeps Up/Stop/Down the same size,
with a one-column parent on narrow screens. Interrupted hold gestures still stop.

Added pure search, overlapping-failure/gesture and profile-name regressions.
Tests/CI are skipped under the user's standing instruction. TypeScript passed;
host compilation, delivery, rendered geometry and physical gesture acceptance
are recorded separately below rather than inferred from source.

## Cafe delivery, 13 October 2026

The root checkpoint `fe02a1e1` is published on the shared owner branch; only the
13 relevant Web/Go/test/doc paths were committed, preserving ongoing mobile edits.
An isolated index reconciled the fix onto main for PR
[#657](https://github.com/atomicdeploy/PCController/pull/657), without switching
the shared checkout, creating a worktree or importing the pending VirtualBoard
feature. One UI-catalog conflict was resolved by retaining main and applying only
the profile-catalog fix. The PR remains policy-blocked with normal auto-merge
enabled. No administrator override, security dismissal or CI/test run was used.

One staged host build completed in 54.2 seconds, with TypeScript/Web bundle,
host/C ABI compilation and package integrity checks. The package host source
digest is `91f289f52bf716f08c3e26310991e3cec8875aedc80934749170084075e6784a`;
controller SHA-256 is
`505162cd4f8d4f12b42e17cbfd3b913ae8b4bfad90d1f15e41697ed1a1724a2e`.
The package retains the already deployed development lineage; it is not claimed
as a build of the isolated main-based PR tree. Generated distribution output is
preserved separately; the mobile owner did not change the Web/Go files.

Cafe's primary-owned updater completed `op-2901a89fe99e6f52` with
`terminal_verified=true`. A fresh read-only check verified the installed digest,
semantically unchanged controller configuration and profile-resolved `Window`
in both browser UI settings and complete PWM readback. Channel 0 was observed at
raw 463/logical 36.981 percent with the existing gamma 2.4; no output mutation was
issued. Pealayer remained paused, connected and without sync error, at matching
desired/prepared/clock ACK 85. Private rollback/configuration backup remains under
the host's program-root `verification/web-channel-controls`.

A temporary browser-inspection tunnel invocation was denied by tool policy and
was not retried through an alternate path. No live rendered geometry or physical
gesture pass is claimed. A supervised Window-only slider probe was requested;
until answered, real PWM/seat/relay outputs remain unchanged. Source compilation
and live catalog verification are distinct from human/physical UI acceptance.
