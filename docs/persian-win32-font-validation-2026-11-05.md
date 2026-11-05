# Persian Win32 font validation — 2026-11-05

The native GUI was registering the canonical VF successfully but asking GDI
for its typographic family, `Yekan Bakh VF`. `GetTextFaceW` reproduced an
**Arial** substitution at both normal and bold weights on this Windows 11 host.
The corrected implementation selects the VF's physical named faces,
`Yekan Bakh VF Regular` and `Yekan Bakh VF Bold`, and verifies the selection.
The same single font asset remains shared with the browser UI.

Native registration is now shared for the lifetime of the process. Independent
settings/diagnostics and SDK dialogs register it before creating controls.
Live locale changes update explicit headings and recorder labels as well as
body text, including children of all six tab pages.

The screenshot review also exposed an RTL layout defect in vendored Walk:
after resize/locale layout, each selected page could have zero width. Mapping
the complete tab-client rectangle with `MapWindowPoints` replaces the invalid
one-point `ScreenToClient` calculation. The regression fixture reproduced zero
width before this fix and verified 1,152-pixel page widths afterward.

## Verification

- Native fixture on Windows 11 build 26100, 144 DPI: settings owned/ownerless,
  Persian first-open, live fa/en switches, all six Persian pages, retained
  resize and close/reopen dimensions passed.
- 74 real settings HWND font handles and 9 diagnostics handles were checked
  against the physical selected GDI face, including a fa/en/fa diagnostics
  round trip. Testing `walk.Font.Family()` alone would miss substitution.
- Main renderer and custom popup HFONT selection passed at 96, 120, 144 and
  192 DPI; this does not claim physical mixed-DPI monitor validation.
- 40 regular screen captures were retained locally. Settled Persian settings
  pages, diagnostics and dashboard were visually inspected. Font/geometry
  captures disable motion; transition smoothness was not re-certified here.
- The existing main chrome fixture passed its 12 backdrop/chrome/locale
  variants using GTX 1080 hardware DirectComposition. These tests operate
  only their own windows and inject no mouse/keyboard input.
- Canonical develop build passed `go vet` and all 47 Go test packages, then
  verified the EXE's icon, manifest, version, architecture and embedded Web UI.
  The unchanged committed frontend distribution was reused with
  `-SkipWebBuild`; no frontend rebuild was needed for these native changes.
- History/date verification and `git fsck` passed.

Native fixture evidence is under the enclosing workspace's
`work/persian-font-20261105/native-final/`; the canonical build log and previous
EXE backup are in `work/persian-font-20261105/`. These files are not shipped.

## Executable receipt

- Source: `74b9f0c52836c97cebd7fe607d1f70a6c1326aa9` (font/RTL fix plus graceful
  ellipsis for dashboard hints that exceed the available width).
- Artifact: `dist/RayanLamp.exe`, Windows amd64, develop, CGO disabled.
- Version readback: `RayanLamp 1.0.0 develop (74b9f0c52836, 2026-11-05T01:40:02Z)`.
- SHA-256: `B6198D0D5CE94B691242C8AB38F13B5B0D27929C64BAD3D094121AA9C7D92AF0`.

Windows-owned caption bars, file pickers, notifications and TaskDialogs retain
Windows system fonts. Older Windows font fallback is implemented but was not
tested on a separate legacy Windows host in this pass.
