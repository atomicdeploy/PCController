# Desktop development and recovery

The native GUI is part of the main `controller.exe`, linked from
`Tools/NativeDesktop` through `Tools/Controller/cmd/controller/native_*windows.go`.
Use this integrated entry point, not the retained donor standalone executable:
the PCController core remains the sole transport and hardware owner.

## Launch

From the repository, in any working directory:

```powershell
.\desktop.ps1                          # Installed GUI, hardware auto-connect paused
.\desktop.ps1 --preview                # No core, network or hardware transport
.\desktop.ps1 --preview --page outputs # Offline channel-card preview
.\desktop.ps1 --attach 127.0.0.1:8788  # Attach to an explicitly selected running core
.\desktop.ps1 --package C:\staged-host # A package made by the canonical build
```

`desktop.cmd` provides the same options. `--config FILE` selects a separate
configuration. Only `--auto-connect` opts into automatic hardware connection for
a newly started core. Attaching never pauses, reconnects or replaces an existing
core. The launcher does not build, install, update or stop another application.

Without Node.js, use the equivalent direct installed command:

```powershell
& "$env:LOCALAPPDATA\Programs\PCController\bin\controller.exe" gui --no-auto
```

The existing no-argument Start-menu/Desktop shortcuts still open the TUI; this
is not evidence that the native GUI has been removed. GUI and TUI are distinct
entry points of the same host. The GUI's console toggle opens the TUI client
against that host, not another serial owner.

## Build and verify

```powershell
.\build.cmd --host-only
.\desktop.ps1 --development

# Prepare a package without replacing the canonical development output:
.\build.cmd --host-only --stage-only
# Pass the reported package directory to --package.

node --test Tools/Build/run-desktop.test.mjs
$env:GOFLAGS='-mod=mod'
node Tools/Build/go-tests.mjs --package cmd/controller --run 'TestDesktop' --retest
$env:GOFLAGS='-mod=vendor'
node Tools/Build/go-tests.mjs --module Tools/NativeDesktop --retest
```

Never run `go test` directly on Windows. The stable runner preserves firewall
identity. Native-window fixtures additionally require
`PCCONTROLLER_DESKTOP_WINDOW_TESTS=1`; `screenshots.cmd` regenerates the offline
README gallery. Build/package success and fixture screenshots are not proof of
physical hardware, audio, multi-DPI, or human acceptance.

An IPC/WebSocket subscription acknowledgement is not a board handshake. The
native header and controls follow the authoritative board snapshot; a lost IPC
stream revokes controls immediately. A locally owned core shutting down also
closes its GUI, rather than leaving a live-looking orphan window.

## Resize performance

The resize renderer retains full-size, pixel-exact glass rather than switching
to a flat theme, stretching text, or disabling acrylic. The selected optimization
reuses bounded CPU staging storage, skips unused control-only lighting math on
panels, computes shared lighting once per scanline, and uses exact straight-edge
distance math. The DirectX presentation path resizes and uploads the completed
frame together, without presenting an additional transparent intermediate frame.
The standalone `Presenter.Resize` contract remains unchanged.

An asynchronous Walk sizing experiment did not give a repeatable improvement in
the same scene, so the established sizing/layout behavior was preserved. A paint
suppression experiment was also excluded rather than risk stale final geometry.

Measured on this Windows workstation (GTX 1080, DirectX/DirectComposition), using
24 test-owned native resizes around a 900x700 window:

| Metric | Original | Optimized |
| --- | --- | --- |
| Median complete resize redraw | 97.4 ms | approximately 61-66 ms |
| Go allocation per step | 19.54 MiB | approximately 2.67 MiB |
| Presentations for one completed resized frame | two | one |

These timings are not a 60 FPS guarantee or human acceptance. The renderer still
has a substantial CPU/GDI full-frame cost. No controller commands, event timing,
telemetry throttles or safety policies are changed by this work. Storage grows
with 25% spare capacity, releases a shrink beyond 2:1 and is disposed when hidden.

The original material SHA-256 is retained in `TestGlassResizeMaterialDigest`;
additional tests cover exact signed-distance results, dark/light hover pixels,
clipped controls, CPU store independence, DPI bitmap replacement, large shrink,
real DirectX presentation counts and final resize geometry in both backends.

```powershell
$env:GOFLAGS='-mod=vendor'
$env:PCCONTROLLER_DESKTOP_WINDOW_TESTS='1'
$env:PCCONTROLLER_DESKTOP_DIRECTX_SMOKE='1'
node Tools/Build/go-tests.mjs --module Tools/NativeDesktop --package internal/ui --package internal/windx --run 'TestGlass|TestRoundedRectDistance|TestControllerResize|TestResizeRGBA|TestForeground|TestPresenter|TestResizeFrameBuffer' --retest
```

`TestControllerResizeTiming` reports end-to-end timing and allocations when the
compiled test at the runner's stable product-owned path is invoked with
`-test.run=^TestControllerResizeTiming$ -test.v`. Never move that executable to a
new per-task path. Screenshots from the resize fixture can be requested with the
same capture-tool/output-directory environment used by the gallery.

## Windows transitions and Aero Snap

Custom chrome keeps `WS_CAPTION`, `WS_SYSMENU`, `WS_MINIMIZEBOX`,
`WS_MAXIMIZEBOX` and `WS_THICKFRAME` as window-manager metadata. The native caption
is still hidden by `WM_NCCALCSIZE`; removing these style capabilities instead had
disabled Windows' normal window-management behavior. Explicit Border: None
remains a genuinely frameless opt-out; native-titlebar mode is unchanged.

The actual painted caption reports `HTCAPTION`, `HTMINBUTTON`, `HTMAXBUTTON`
and `HTCLOSE`, with child HWNDs passing those hits to the top-level window.
In particular `HTMAXBUTTON` is the Windows 11 Snap Layout hover contract.
Minimize/maximize/restore use `WM_SYSCOMMAND`, preserving OS transitions and
accessibility preferences rather than synthesizing application animations.
Close retains the existing close-to-tray and authoring guards. Maximized client
bounds are constrained to the monitor work area; restored windows keep their
full custom client surface. Transparent hit pixels use the actual surface client
origin, including while maximized. Maximization clears rounded clipping regions
so the older-Windows HRGN fallback cannot clip the invisible sizing-frame inset;
restoring reinstates the configured rounding. Native caption dragging and magnetic widget snapping
coexist. No global Windows animation or Snap setting is changed.

`TestControllerNativeWindowCommandsAndSnapContract` exercises real test-owned
HWNDs in both macOS and Windows custom-chrome styles: native style flags, parent/
child hit routing, captured release/cancel, minimize/maximize/restore, restored
geometry and taskbar-safe maximized bounds. The border lifecycle fixture covers
native/custom, acrylic/blur/solid, legacy/DirectX and compact modes. Automated
state/geometry checks are not visual proof of the OS transition or human Snap
Layout acceptance; those still require checking the installed window with the
user's Windows animation/Snap preferences enabled.

Custom frame activation must not call the normal frame painter. Restoring the
window-manager caption flags exposed themed NCUAH caption/frame draw messages
that bypass WM_NCPAINT. These are consumed in custom/none mode, and ordinary
WM_NCACTIVATE returns TRUE without DefWindowProc. Minimized activation retains
the documented default-processing path with repaint suppressed. Native-titlebar
mode is unchanged. The posted border-policy reassertion remains defensive;
preventing a flash must not depend on that deferred work or a later repaint.

`TestControllerCustomFrameOwnsActivationPaint` guards message ownership and native
fallback. `TestControllerActivationDoesNotFlashClassicBorder` samples actual
screen-edge pixels immediately around activation, before forcing a repaint, in
DirectX and legacy modes and while posted cleanup is blocked. Existing native
minimize/maximize/restore and Snap hit-testing contracts must still pass.

## Console / TUI button

The GUI's Console button launches a full TUI client against its exact IPC
endpoint; it never starts a second serial owner. Its IPC token is passed only in
the child environment when configured, not on the command line. Tokenless local
IPC does not pass an empty secret reference (which would abort TUI startup).
A dedicated system `conhost.exe`
is launched with actual NULL startup handles so it creates an independent
visible console, not a redirected ConPTY session. This does not change the user's
default Terminal application or hide unrelated Terminal tabs. The explicit GUI
child binds CONIN$/CONOUT$ before startup so Bubble Tea gets interactive handles;
ordinary CLI redirection is untouched.

Console startup waits for the child's user-ACL-protected pipe (including the
initial FILE_NOT_FOUND interval), with a deadline and early-process-exit fence.
Only pipe discovery is retried; toggle commands are never replayed. Startup
bridge failures are reported instead of swallowed. Closing the TUI allows the
next explicit click to launch a fresh instance; hide/show reuses the same one.

`TestNativeConsoleOwnedWindowToggle` is an opt-in real, test-owned console
fixture. It verifies interactive streams, sole ownership, delayed startup and
show/hide/show without duplicate processes. Other regressions cover deadlines,
early exit, dedicated launch arguments, token placement, stale environment
overrides and preservation of redirected non-GUI CLI streams.

## Preserve and resume

- Host settings: `%APPDATA%\PCController\config.json` (or `controller config path`).
- Native layout/preferences: `native-desktop.json` beside the selected host
  configuration. Offline preview preferences use `native-preview.json` instead.
- Source and test fixtures: `Tools/NativeDesktop`, the native host adapters and
  their tests are tracked together in this repository.
- Full imported source provenance and the repeatable missing-file audit:
  [donor coverage](NATIVE-DONOR-COVERAGE.md).
- Remaining development/acceptance work, including RTL capture and specialized
  editors: [feature-parity ledger](NATIVE-FEATURE-PARITY.md). Overall parity is
  deliberately still open; do not replace that ledger with a blanket claim.

Do not reset dirty worktrees, remove preservation refs, overwrite configuration,
or replace a running authoring session to recover a launcher. Use the approved
installer/updater for installed packages; retain the source branch and package
identity independently of the installed binary. Archive known generated Web
bundle churn before restoring its tracked baseline after a local build.
