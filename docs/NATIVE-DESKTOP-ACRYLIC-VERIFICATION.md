# Native desktop: acrylic regression checkpoint

## Cause and repair

The controller integration replaced the donor's combined composition stack with
a standalone Accent acrylic call, immediately followed by `SYSTEMBACKDROP_NONE`.
With the DirectComposition alpha foreground this removed the behind-window
material and left a dark/black surface. Acrylic now follows the donor's working
MainWindow backdrop + transparent Accent blur + DWM blur + full glass frame.
High Contrast and disabled Windows transparency still select the solid fallback.
Unchanged-material refresh does not clear/rebuild the stack on activation.

Controller startup also forced Acrylic and custom chrome on every launch. Those
are now first-run defaults, not overrides of saved user choices.

## Native UI work in this checkpoint

- Device-pixel-bounded glyph sizing, cached by point size; no fixed-point glyph
  overflowing a fixed-pixel card slot at high DPI.
- Clickable, wider connection badge with a dedicated native Connection form.
- Native Firmware/Backups and bounded live Events pages replace those two Web
  fallbacks. Firmware uses immutable artifact identities and explicit confirmation
  through the approved primary-owner RPC service; no direct serial programmer.
- Shared forms use the UI font, logical sections, bounded numeric editors,
  checkboxes for booleans, wrapped compact actions, scrolling, and content-aware
  initial sizing. Refresh preserves row identity and never discards a dirty draft
  without confirmation. Event/firmware refresh is event-triggered and coalesced.
- MOSFET target remains explicitly pending until the operation finishes; observed
  on/off indicators are not optimistically changed to claim hardware success.
- Main-window settings retain raised/flat controls, macOS/Windows chrome, theme,
  animations, material, and pinning. Existing rename/icon/reorder paths remain.

## Verification and limits

Windows host build and `go vet ./cmd/controller` passed. CI and test suites were
not run, per the requested pause. The preview transport is disabled: no output,
learning, flash, or production deployment was used for visual verification.

The read-only capture helper's `screen` mode uses desktop BitBlt. It does not move,
focus, click, or resize windows, and does not use blocking PrintWindow. Prefix a
title filter with `=` for exact matching; output reports whether the target HWND
is foreground. Occlusion in captures is real, not repaired or synthesized.

Observed: main window blur in active and non-foreground states, raised controls,
channel sliders/semantic state, preferences, and a real acrylic popup with icons.
Popup runtime reported `material=acrylic native=false`. Native forms were captured
and corrected after finding narrow numeric controls and clipped actions.

Offline capture entry points: `gui --preview --preview-page` accepts `outputs`,
`preferences`, `menu`, `rf`, `programs`, `buzzer`, `strip`, `display`, `settings`,
`connection`, `firmware`, or `events`. It is rejected without `--preview` and never
executes a board action. Do not interpret preview sample values as telemetry.

This is not full native feature-parity acceptance. Advanced effect/strip authoring,
all RF live-reception/observed-code workflows, network/integration configuration,
and complete host preferences still need dedicated native UI. Pointer gesture,
cross-DPI/accessibility, connected RPC, physical board, and long-run acceptance
remain separate from these captures and the successful build.
