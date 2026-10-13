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
