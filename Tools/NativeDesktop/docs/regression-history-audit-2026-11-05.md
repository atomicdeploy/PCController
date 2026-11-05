# History-backed regression verification — 2026-11-05

This follows the [original native audit](regression-audit-2026-11-05.md).
Both production fixes were already present at `97d02d3` when this pass began.
This pass attributes the changes using recovered evidence, independently
reproduces the bad behavior, and strengthens the tests. It does not pretend
that newly added tests are newly implemented production fixes.

## Offending source boundaries

These are recovered feature boundaries, **not certified good/bad builds**.
The recovery checkpoints have missing files and cannot support an honest
whole-application `git bisect`.

| Regression | Recovered source boundary | Evidence |
| --- | --- | --- |
| Thick white main-window frame | `f3ae340..7442532` (DirectX introduction) | `applyConfigUI` changes the layered policy to `perPixelForegroundForUI(cfg.UI) && !directXEnabled`. The ordinary GPU HWND exposes default non-client painting and stale child GDI backing previously masked by the layered host. |
| Settings opens at its layout minimum | `f9928d7..c525c6c` (localization introduction) | The successful July 24 patch adds `applySettingsLocalization(session, working)` after hidden placement. Settings' original file preimage was unavailable during recovery, so this attribution uses the recorded patch, not a misleading `git blame` on its later reconciliation import. |
| Chrome/network message collision | `d79e626..454be1f` (interrupted repair) | The new `wmReassertWindowChrome` initially used `0x8527`, already owned by network-change handling. `14fe433` changes it to `0x8528`. |

The localization record is in root session
`019f8cfc-271a-72e1-8747-e3eb883de5ec`, physical JSONL line 48306,
at `2026-07-24T19:17:02.850Z` (UTC). Its patch SHA-256 is
`423ef2ad4fc5f6c2783d42b9cbfc95e7edd9c20822e4848c9a783acc2fd2bc76`;
the tool result explicitly reports success. Raw conversation logs remain private.

The border/message fixes are visible in `454be1f..14fe433`. The complete
Settings and patched Walk files first enter the recovered Git tree in
`abfd5a3`; that is an import boundary, **not** their original introduction date.
The separate ownerless/repeated-Show defect was Walk's legacy size-from-layout
behavior and is fixed by explicit preservation of prepared bounds.

## Controlled reproduction, not a fabricated historical checkout

A separate `git archive` export of `97d02d3` was used. Only named mechanisms
were disabled in that disposable source copy. The real working source was
never rolled back, and no historical script or arbitrary logged command ran.

1. Disabling captionless NC-paint suppression alone did **not** fail the old
   final-screenshot test on this desktop: the later GPU/backing repaint can
   hide the damage. This exposed a test gap, not proof the contract is optional.
2. Disabling both that suppression and the pre-presentation GDI backing clear
   reproduced the thick white border. The screen-pixel assertion failed on
   `auto-blur.png` while the DirectX presenter remained active. This is a
   combined counterfactual, not a claim to have isolated each contribution.
3. Moving final Settings placement back before localization/theming, while
   retaining the Walk preservation fix, reproduced **540x177 DIP** in English
   and **570x162 DIP** in Persian. Owned and ownerless cases failed. This
   independently identifies the initialization-order defect.

The negative-control code is outside the repository under the enclosing
workspace's `work/history-regression-pass/control`; it must never be packaged.

## Safeguards added in this pass

- The native chrome fixture now checks actual message-handler results for
  `WM_NCPAINT` and `WM_NCACTIVATE`: captionless modes must handle them; native
  title bars must delegate. A later repaint cannot conceal that violation.
- The native Settings fixture now resizes, switches English/Persian live,
  visits all six tabs, and asserts exact unchanged outer bounds after each
  layout pass, before Cancel and reopen persistence checks.
- Existing coverage also checks the message-ID collision, zero-valued
  extended-style error handling, ownerless placement, fractional DPI,
  negative monitor coordinates, activation/hide/show, native/custom chrome,
  renderer transitions, and transparent/no-border behavior. No additional
  production failure was found in this tested scope.

These extend VIS-022 and VIS-023. Screenshots are observations only: no desktop
capture is fed to any renderer. No computer-control tool or input injection
was used, and no lamp-control command was sent.

## Results

- Full `go test -tags rayanlamp_develop -count=1 ./...`: **47 packages passed**.
  The previously blocked socket tests now run with network access available.
- `go vet -tags rayanlamp_develop ./...`: passed.
- Expanded manifest-equipped native suite: passed; **31 screen captures**.
  The final run also verifies its pixel classifier against the fresh bad-border
  screenshot from the negative control.
- Hardware: NVIDIA GeForce GTX 1080, DirectComposition/D3D feature level 12.1,
  Windows at 144 DPI. Fixed Settings opens at 1170x960 physical pixels
  (780x640 DIP) and preserves resized bounds across live layout and reopen.
- Evidence: `work/history-regression-pass/final-native`, `border-negative-full`,
  `settings-negative`, and `go-test.log` in the enclosing workspace.

This is not all-Windows/all-monitor acceptance. Physical mixed-DPI monitor
movement, Explorer tray behavior, interactive TUI, lamp hardware, and every
historical UI complaint are not re-certified by these fixtures. The DWM border
attribute getter still returns unsupported here; actual captures and geometry
are used instead of claiming successful attribute readback.

## Rebuilt executable

The canonical build of `e453f07` passed with
`tools/build.ps1 -Edition develop -SkipWebBuild -Force`: Go vet and the full
Go test suite were enabled, followed by resources, compilation, and PE checks.
Only frontend rebuilding was skipped; the unchanged checked-in develop Web
bundle was reused and its edition verified.

- Artifact: `dist/RayanLamp.exe`, Windows/amd64, CGO disabled.
- SHA-256: `31D4A0299C0A28C1ECBB246574EB6C7AD2AE6662B7E14CF2AAF0E44455467810`.
- The previous executable is preserved as
  `work/history-regression-pass/RayanLamp-before.exe` in the enclosing workspace.
- No running RayanLamp process was found or terminated; no installation,
  registry change, device command, or GitHub push was performed.
