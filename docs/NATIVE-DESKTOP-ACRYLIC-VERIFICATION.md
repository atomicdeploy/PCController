# Native desktop: acrylic regression checkpoint

## Glass hover frame budget

The former compositor rebuilt the full software glass canvas, static cards and
every button's material/shadow on each hover tick. A 640x720 fixture with eight
buttons took 41.56 ms per frame before foreground recovery and presentation.
The visual clock also discarded elapsed time above 50 ms, stretching a 140 ms
transition whenever painting delayed the message queue.

The compositor now retains the current static background and one current
body/shadow raster per control. Only changed material keys rerasterize. Keys own
palette values and include geometry, hover, press and pending phase; pointer
coordinates do not invalidate idle siblings. Layout/mask changes invalidate
affected shadow caches. Hiding the window or leaving glass mode releases the
cache. Live text, segments and other foreground content remain outside it.
The animation clock consumes actual elapsed time, and the existing idle timer
shutdown and reduced-motion behavior remain intact.

Validation uses the stable Windows runner:

```powershell
node Tools/Build/go-tests.mjs --module Tools/NativeDesktop --package internal/ui
```

`TestGlassHoverFrameTiming` reports compositor timing without a brittle
machine-speed assertion. Cache tests cover light/dark/high-contrast palettes,
live foreground changes, resizing, pointer/press state, masked adjacent shadows,
clipped controls and hidden-resource release. A reference comparison retains the
original layered material with only bounded 8-bit alpha rounding differences.

For an interactive desktop, set `RAYANLAMP_WINDOW_TESTS=1` and run
`TestControllerGlassHoverPresentation` through the same stable runner. It owns
an offline native window, loads the product's Common Controls manifest, and
measures full paint plus presentation submission in both DirectX and compatibility
modes. It never starts a board core or injects mouse/keyboard input. These timings
are not monitor scanout FPS or remote-machine/hardware acceptance.

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
