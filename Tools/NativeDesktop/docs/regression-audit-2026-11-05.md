# Native regression audit — 2026-11-05

Follow-up: [history-backed attribution and controlled reproductions](regression-history-audit-2026-11-05.md)
now records the recovered feature boundaries, stronger native tests, and a full
47-package Go test pass after network restrictions were lifted.

## Scope and provenance

This pass addresses the white main-window border and collapsed Settings size,
plus related chrome transitions, geometry persistence, and message routing.
No computer-control tool or input automation was used. Native tests create
their own windows through the production setup and record regular desktop
screenshots. The tests never start the lamp connection lifecycle or actuate it.

When this regression pass began, the supplied directory had no `.git` metadata. No usable RayanLamp
Git history was found in the inspected workspace/backups; the repository URL
recorded in the changelog was not accessible as a repository. Consequently no
honest commit-hash range or `git bisect` result is available. The causal feature
boundaries below are source/runtime findings, not invented commit attribution.

A subsequent [source-history recovery](history/README.md) imported available
JSONL edits as explicitly incomplete checkpoints. Those recovery commits do
not retroactively provide original build snapshots or a valid build-bisect range.

The previous distribution was preserved before rebuilding:

- `RayanLamp.exe`: 41,645,056 bytes;
- SHA-256: `6A1C3D8CD3BAEC5D845D858DBF410C882937F642289AE51D4BFCAA0E7E398C69`;
- executable timestamp: July 25, 01:24; the later interrupted source edits
  were not included in that binary. A source ZIP was also retained under the
  workspace's `work/regressions-20261105` evidence directory.

## Confirmed causes and repairs

1. **GPU/ordinary-HWND transition exposed classic chrome.** The compatibility
   layered host masked USER32's non-client drawing, while the ordinary
   DirectComposition host retained `WS_THICKFRAME` and received an approximately
   8-pixel white GDI frame. Clearing WINDOWEDGE or the DWM border color alone
   did not remove it. Captionless NC painting is now suppressed, retaining
   native resize hit-testing and the unmodified native-caption path.
2. **Settings layout overwrote its desired size twice.** Localization/layout
   changed the hidden HWND to 540x177 logical units before Show; Walk's legacy
   Show could independently replace prepared dimensions with layout minima.
   Final placement now occurs last and Show preserves it explicitly, including
   ownerless/modeless use. The default is 780x640 logical units. Closing captures
   geometry before destruction; saved sizes are DPI-independent.
3. **Native-to-custom GPU chrome left stale white backing.** The ordinary
   child below the alpha visual retained GDI pixels. Clearing only that
   app-owned surface before presentation fixes it without disabling DirectX,
   snapshotting, changing the material owner, or allocating a new brush/frame.
4. **Adjacent defects.** The interrupted chrome-reassert message used the same
   ID as network-change automation; it now has a separate ID and a collision
   test. Geometry handles invalid DPI, negative monitor positions, and
   workspace/screen conversion for minimized/maximized restoration. Zero-valued
   extended styles clear Win32's thread-local last-error before ambiguous
   Get/SetWindowLong calls, avoiding false failure reports.

The relevant platform contracts are documented by Microsoft for
[WM_NCACTIVATE](https://learn.microsoft.com/en-us/windows/win32/winmsg/wm-ncactivate)
and [SetWindowLongPtr](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwindowlongptrw).

## Verification and repeatability

- Settings: owned English, ownerless English, and Persian first-open,
  resize/repeated Show, Cancel, and reopen; owner remains enabled.
- At this desktop's 144 DPI, first-open outer size is 1170x960 physical pixels
  (780x640 DIP); resized reopen preserves logical size within one physical pixel
  of fractional-DPI rounding.
- Chrome: automatic Blur, Acrylic, Solid; native Solid/Acrylic; custom Acrylic;
  legacy Blur; transparent/no-border; transparent-to-Blur; compact Acrylic and
  compact Blur. Each is checked across activation and hide/show, with captures
  before and after. The final native suite passes and retained 28 PNGs.
- Pixel gate rejects the captured known-bad white frame. GPU-capable variants
  must keep an active presenter with zero presentation failures; a silent
  fallback is a test failure. Hardware observed: NVIDIA GeForce GTX 1080,
  D3D feature level 12.1.
- Run `tools/test-native-windows.ps1` for the manifest-equipped native suite.
  See `visual-regression-contract.md` VIS-022/VIS-023 for lasting safeguards.

Limits: these captures prove this Windows 11/150%-DPI desktop, not every OS,
theme, monitor, or drag sequence. Mixed-DPI geometry has unit coverage, not
physical multi-monitor acceptance in this pass. Explorer taskbar APIs return
E_ACCESSDENIED in the restricted test process, so the fixture deliberately
omits tray initialization; tray/taskbar integration is not re-certified here.
The DWM border attribute getter is unsupported in this environment, so visual
acceptance uses actual screenshots rather than claiming attribute readback.

## Build and test receipt

- Output: `dist/RayanLamp.exe`, Windows/amd64 **develop**, CGO disabled;
  41,674,752 bytes.
- SHA-256: `E8CD6F23B2C529113CBC39C6B5E9A1D4928DF986F962188CF2B54DE2A637C35B`.
- Canonical PE verification passed: architecture, console subsystem, icons,
  manifest, VERSIONINFO, native debug payload, and embedded develop Web edition.
- Full `go vet -tags rayanlamp_develop ./...` passed.
- Complete tests for config, UI, dialogs, winfx, CLI, and embedded Web passed.
  The TUI suite passed with only network API-transport tests excluded.
- Web TypeScript check and all 193 tests across 23 files passed. Vite/PWA
  rebuilt the develop bundle from current source using installed dependencies.
- Full Go suite was attempted but is NOT green in this restricted environment:
  loopback TCP calls fail with Windows socket access-permission errors. The
  final packaging command therefore used `-SkipTests` after the recorded
  attempt and the passing targeted suites; it did not weaken network tests.
- ANSI/emoji tests previously inherited `TERM=dumb` / `NO_COLOR=1`. Tests that
  assert rich-terminal output now establish their own environment and pass
  under those inherited settings; production console policy is unchanged.

Evidence is under `work/regressions-20261105/final-native` in the enclosing
workspace. Its `screenshots` directory includes `auto-blur.png`,
`compact-acrylic.png`, `native-acrylic.png`, `custom-acrylic.png`,
`owned-first.png`, `owned-reopened.png`, and the Persian equivalents. Earlier
known-bad captures remain under `before-ncpaint` for comparison.
