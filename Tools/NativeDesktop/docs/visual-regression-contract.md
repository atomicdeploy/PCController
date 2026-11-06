# Visual regression contract

This document is normative for PCControllerDesktop's native Windows UI. Changes to
window chrome, DWM/Accent composition, `UpdateLayeredWindow`, GDI painting,
popup caching, regions, text, icons, hover motion, or resize behavior MUST
preserve these rules. A locally attractive screenshot is not sufficient when
the implementation violates a rule below.

The purpose of this file is to retain the engineering lessons behind earlier
regressions in the repository itself, rather than relying on chat history or a
developer remembering a previous fix.

## Terms

- **Live material** means DWM, AccentPolicy, or another Windows compositor
  effect sampling the current content behind an ordinary PCControllerDesktop-owned HWND.
- **Snapshot** means capturing desktop or other-window pixels and replaying
  them as fake blur/transparency. Production UI MUST NOT do this.
- Offscreen buffers containing only PCControllerDesktop-owned pixels are allowed.
  Retaining PCControllerDesktop's own premultiplied RGBA surface for a short opacity
  transition is also allowed.
- Test-only screenshots are allowed for visual verification. They MUST never
  become an input to production rendering.
- **Canonical geometry** means one shape calculation consumed by every native
  region, alpha mask, hit test, and border for a surface.

## Surface ownership

| Surface | Material owner | Foreground owner | Text/alpha method | Outer-corner owner |
| --- | --- | --- | --- | --- |
| Layered main window | Live ordinary DWM/Accent host where applicable | App-owned premultiplied RGBA via `UpdateLayeredWindow` | Grayscale glyph masks composited into premultiplied RGBA | The configured main-window corner policy and alpha region |
| Native/compatible main window | Ordinary HWND/DWM | Walk/GDI client | Native opaque GDI text | Native frame or the explicit compatibility region, never both as competing contours |
| Tray popup | Live ordinary DWM/Accent parent | Per-pixel layered child | Grayscale GDI masks converted to premultiplied alpha | Windows 11's standard DWM round contour aligned to one 8-DIP app mask; matching parent/child HRGNs remain the compatibility and input clip |
| Settings and diagnostics | Native non-client frame | Themed native controls plus a short-lived app-owned page-transition surface | Opaque native/GDI text | Native dialog frame |

## Required invariants

### VIS-001: material is live; desktop capture is forbidden

The main window and tray popup MUST NOT use `PrintWindow`, desktop
`BitBlt`, Windows Graphics Capture, desktop duplication, DWM thumbnails, or an
equivalent mechanism to imitate Acrylic or Blur.

Why: captured pixels become stale as windows move, only appear correct after a
drag ends, expose the wrong content during animation, and violate the meaning
of transparency. They also caused the tray popup to look like a frozen
rectangle instead of live material.

### VIS-002: every outer contour has one canonical geometry

The tray parent HRGN, layered-child HRGN, BGRA alpha silhouette, and border MUST
use the same 8-DIP pixel predicate at the same device DPI. Do not combine
`CreateRoundRectRgn` with a separate hand-written circle.

On Windows 11, the live DWM/Accent host MUST additionally request
`DWMWCP_ROUND`. Accent can composite its material outside the binary HWND
region when `DWMWCP_DONOTROUND` is selected, even while `GetWindowRgn` reports
the correct complex region. The DWM contour owns the antialiased material edge;
the matching canonical HRGNs remain the compatibility and input clip, and the
layered BGRA surface uses the same 8-DIP geometry. Backdrop setup happens before
the final HRGNs are committed, and every composition refresh or cached-window
reuse restores `DWMWCP_ROUND`.

Why: those rasterizers disagree about inclusive bounds and ellipse pixels. The
former implementation disagreed on 8–16 pixels depending on DPI, leaking raw
material as corner spikes/notches. A later implementation forced
`DWMWCP_DONOTROUND`; its HRGNs remained mathematically correct, but the Accent
visual bled through as a full square. Reapplying a region cannot clip that
independent compositor contour.

### VIS-003: layered text never uses ClearType color subpixels

Text placed in a per-pixel surface MUST use a grayscale coverage mask and
premultiplied source-over compositing. It MUST NOT infer glyph alpha from
ClearType RGB channels or paint ordinary GDI text directly into a transparent
layer.

Why: one alpha channel cannot represent three independent ClearType subpixels.
Violations produced black and white fringes around `LIGHT IS OFF` and other
labels even when SVG artwork remained clean.

### VIS-004: the first visible frame is already complete

Popup and main-window preparation MUST occur while hidden or fully transparent.
The backdrop, final dimensions, regions, foreground, and icon state must be
committed before reveal. A cached popup must prepare a fresh frame before every
activation.

Why: revealing a host before its foreground caused white flashes, black
rectangles, stale menu content, and brief opaque backgrounds.

### VIS-005: live material remains visible through interactive states

Hover, press, selection, loading stripes, and reveal lighting over Acrylic or
Blur MUST remain translucent and time-animated. They MUST NOT replace the live
surface with an opaque rectangle. Solid/flat mode may use opaque fills.

Custom-rendered buttons MUST also preserve the native activation lifecycle:
mouse-down arms and depresses, moving out raises without disarming, moving back
in depresses again, and mouse-up activates only over the originally armed
control. Keyboard activation depresses on the first key-down and fires exactly
once on its matching key-up. Capture loss, focus loss, cancellation, hiding, or
a live layout change releases the visual without invoking the action.

Why: an opaque hovered row hid the glass and made the menu look like two
unrelated renderers. Instant state changes made buttons and menus visibly pop.
Dispatching a custom control on mouse/key down reduced its pushed state to an
easy-to-miss timed pulse and allowed keyboard auto-repeat.

### VIS-006: composition remains active while moving and resizing

Acrylic/Blur MUST not be disabled during drag and restored only on drop.
Resize and drag handlers may reposition or resize app-owned surfaces, but MUST
not substitute a captured backdrop.

Why: the effect previously existed only after dragging stopped, which revealed
that the implementation was replaying or rebuilding the wrong visual.

### VIS-007: chrome and borders are singular

The selected title-bar mode is exactly one of none, custom, or native. Border
mode is independent. Do not leave a native non-client strip behind a custom
caption, draw two borders, or reserve an empty gap between them. Native mode
must retain the application icon. Transparent + no-border must disable DWM
non-client rendering and DWM corner rounding before its alpha region is
presented; otherwise DWM may retain a shadow for the full rectangular HWND.

Why: mixed ownership produced unwanted title bars, double outlines, and empty
space along the top edge. Leaving DWM's rounded non-client silhouette enabled
also produced a rectangular shadow around an otherwise alpha-shaped window.

### VIS-008: transparent pixels have real input semantics

In transparent + no-border mode, canonical alpha-zero pixels MUST be excluded
from the top-level window region so input reaches windows belonging to other
processes. `HTTRANSPARENT` alone is not sufficient. Acrylic/Blur drag planes
may deliberately retain a minimal nonzero alpha where the window must remain
draggable.

Why: a visually absent surface that still intercepts clicks is not transparent.

### VIS-009: dialogs remain readable native surfaces

Settings, Diagnostics, TaskDialog, tabs, and buttons MUST use native dark/light
frame theming and composition-safe client painting. Do not extend full-client
DWM glass behind ordinary GDI children. Active/hovered tabs need animated,
readable contrast; dark buttons must not expose rectangular parent backplates.

Why: full-client glass behind GDI produced black voids, washed text, unreadable
active tabs, Vista-looking controls, flicker, and square slabs around rounded
buttons.

### VIS-010: magnetic snap is a per-message assist, not a lock

Snap acquisition and release are evaluated throughout every drag. Moving
outside hysteresis releases the edge without requiring another drag session.
Holding Ctrl suspends and detaches snap; releasing Ctrl re-enables it in the
same session. Closing Settings must not replay an old snap position.

Why: one-shot and sticky implementations trapped the window or stopped
snapping until the mouse was released.

### VIS-011: vector art remains vector until final device size

Steady device artwork and icons with SVG sources are rendered at the exact target
device size. Raster resizing must use the shared alpha-correct high-quality
resizer. Windows ICO resources must contain real, individually rendered sizes.

Why: stretching one bitmap or letting default Windows scaling choose a large
frame produced blurry devices, menus, captions, and taskbar icons.

### VIS-012: state icons update as one system

Main-window, tray, taskbar, caption, and dialog icon state must be updated
together where applicable. Healthy state needs no `LIVE` label; offline state
uses the offline device artwork.

Why: updating only the tray left a stale taskbar icon and redundant status text.

### VIS-013: motion is elapsed-time based and never snapshot based

Window entrance/terminal exit is approximately 300 ms. APNG, hover, button,
tab, and popup transitions use elapsed time so delayed timer ticks do not
stretch the animation. Close-to-tray is a hide, not terminal exit. Disabling
the global Animations option settles surfaces immediately and safely.
Interactive transitions MUST NOT depend on the low-priority
`WM_TIMER` → invalidation → `WM_PAINT` chain. Frame requests are coalesced,
generation-safe, and commit an already-buffered surface in the initiating UI
dispatch; immutable glyph, icon, bitmap, DC, and font resources are not
recreated on every frame.

Why: blocking or frame-count motion stuttered under load, while snapshot-based
animation broke live material. Low-priority timer/paint messages also collapsed
several logical frames during mouse input, and per-frame GDI allocation added
avoidable jitter.

### VIS-014: configuration preview is immediate and reversible

Native Settings is modeless, retains its last tab, has an icon for every tab,
and applies appearance changes immediately. Cancel/close rolls back the
preview without moving the main window to a stale position. No dialog or
preview may be created once terminal shutdown begins.

Why: delayed preview made settings impossible to judge; stale baselines and
modal ownership caused position jumps and blocked the main UI.

### VIS-015: every application menu has one styled presenter

The notification-area menu, main-client context menu, custom-caption context
menu, native-caption context menu, and keyboard context-menu gesture MUST route
through the same item model and custom popup renderer. An ordinary custom
rendering failure gets one fresh-window retry and is then reported; it MUST NOT
silently downgrade to classic `CreatePopupMenu`/`TrackPopupMenu`. Intentional
native rendering is reserved for explicit policy and Windows High Contrast.
High-DPI or short work areas use a readable compact layout rather than a
different renderer.

Why: the main menu appeared as an unstyled classic Windows menu while the tray
menu remained Acrylic. The paths shared a builder, but renderer errors were
discarded, native caption right-click bypassed the builder, and an oversized
DPI-scaled layout could deterministically trigger the hidden downgrade.

### VIS-016: composition transitions finish in the initiating UI event

Changing transparent mode to Blur, Acrylic, Mica, Tabbed, or Auto MUST commit
the recalculated frame and app-owned layered surface, synchronously repaint the
required non-client/client damage without erasing the glass, and flush the DWM
queue before the settings callback returns. It MUST NOT rely on a later move,
resize, activation, or focus change to reveal the requested material.

For native chrome, immersive dark mode and the product caption icon MUST be
restored after the final backdrop/frame mutation. The live device-state icon may
occupy the large taskbar/Alt+Tab icon slot, but it MUST NOT become Walk's
retained caption icon or replace the product icon on a DPI change.

Why: a queued child invalidation left the transparent layered surface stale
until dragging damaged the HWND. Applying dark mode only before rebuilding the
DWM frame left the caption white until activation. Reusing the state icon as
the window's canonical icon also removed or replaced the native caption icon
after compact-mode and DPI transitions.

### VIS-017: dialog polish never replaces native control semantics

Settings-page navigation may briefly present persistent offscreen buffers that
contain only PCControllerDesktop-owned page and child-control pixels. The transition
surface covers exactly the tab content rectangle, keeps the tab header live,
and is removed only after the destination page has completed its final hidden
layout and redraw. It MUST NOT capture the desktop, a DWM backdrop, or pixels
from another process, and it MUST NOT use blanket `WS_EX_COMPOSITED`.

Custom dialog buttons, fields, and dropdowns may own their frame, glyph,
chevron, focus, hover, and pressed-state painting, but the underlying native
`BUTTON`, `EDIT`, and `COMBOBOX` HWNDs remain authoritative for activation,
caret and selection, IME, clipboard, password/cue behavior, type-ahead,
Alt+Down/F4, default/cancel routing, accessibility, and automation. Dialog
actions use deterministic mnemonic accelerators. Animations-off and High
Contrast settle immediately; a live system accessibility change must re-query
that policy and restore native chrome without requiring the dialog to reopen.
Every cached DC, bitmap, font, region, and subclass is released on destruction
or invalidated on DPI/size/theme changes.

Why: directly revealing a newly selected child-HWND tree exposed controls one
paint at a time. Replacing native input controls to hide that layout sacrificed
keyboard, IME, and accessibility behavior; repeating the same accelerator on
several buttons made Alt-key invocation depend on enumeration order.

Native control interaction must settle before app-owned chrome is presented.
A pointer move inside an already-hot dropdown must not synchronously repaint
the complete control again. Popup theming is cached and reapplied only when
the popup HWND or effective appearance changes. A themed combo's native
`WM_PRINTCLIENT` output and app-owned chrome are rendered into one offscreen
surface and transferred to the live paint DC exactly once; live background
erase is suppressed. Tab handoff freezes only the newly selected destination
page, uses transparent DC background mode for its app-owned capture, presents
one terminal overlay frame, and redraws the live destination once behind it.

Why: painting custom dropdown chrome before USER32 let the themed native hot
state overwrite rounded edges; repainting on every mouse move made the control
flash. Reapplying `SetWindowTheme` issued destructive erase/frame cycles.
Freezing every TabPage altered hidden-page `WS_VISIBLE` state, while opaque
capture mode stamped solid rectangles behind check boxes and radio buttons.

### VIS-018: complex dialogs remain responsive across monitors

The Windows move/resize loop owns Settings geometry. Per-pointer messages must
not synchronously lay out its entire child-HWND hierarchy. Layout work is
coalesced, tagged by monotonically increasing generation, and only the newest
result may reach HWNDs. Cancellation or completion of an older generation must
never cancel, overwrite, or apply after its replacement. The final size is
committed without suppressing native move, resize, DPI, accessibility, or
non-client behavior.

Before first reveal, an owned dialog is centered and constrained using the
owner monitor's work area and effective DPI, including negative virtual-screen
coordinates. Crossing monitors must retain the physical cursor relationship,
recompute DPI-scaled constraints once, and never apply old-monitor pixel
dimensions after `WM_DPICHANGED`. The outer minimum is capped to the active
monitor work area; when ideal content is larger, the Settings tab surface
scrolls instead of placing the caption or action buttons beyond the desktop.

Why: synchronous full-tree layout in every sizing dispatch made the caption
lag behind the pointer. Creating on an arbitrary monitor and later centering
with stale-DPI dimensions caused jumps, invalid minimum sizes, and broken
layouts on mixed-DPI and negative-coordinate monitor arrangements.

### VIS-019: executable, class, and live window icons form one identity

Every registered application window class MUST load both its large and small
icons from the executable's embedded multi-resolution product icon. The class
icon resource ID and release resource generator MUST agree, and the PE release
gate must reject a binary that omits that agreed ID. A generic system
`IDI_APPLICATION` icon is only a last-resort runtime fallback for third-party
hosts that do not embed an application icon.

Per-window `WM_SETICON` updates may still present live device state in the
taskbar, Alt+Tab, and owned windows. They MUST NOT be the only icon assignment:
Task Manager's collapsed/root process row, shell grouping, and any class-level
fallback must retain the PCController product identity at both system icon sizes.

Why: Walk requested legacy resource ID 7 while goversioninfo emitted icon
groups 2 and 32512. Later per-HWND state icons made child rows look correct,
but the registered class silently inherited Windows' generic application icon
and Task Manager exposed the mismatch on the root process row.

### VIS-020: cross-surface assets have one canonical owner

An identical runtime asset MUST NOT be embedded separately for native and Web
consumers. The ordinary 512-pixel product artwork, bundled PCM WAV catalog, and
YekanBakh variable font come from the canonical Go asset bundle; the embedded
Web host serves those bytes directly and the static exporter inserts each
exactly once. Purpose- or size-specific PWA artwork, such as a maskable icon or
Apple-touch size, may remain distinct when its pixels or semantics genuinely
differ.

`assets/PCControllerDesktop.ico` is the executable's PE-resource build input and MUST NOT
also enter Go's runtime filesystem. Source animation frames and unused raster/
ICO variants likewise stay unembedded unless a reviewed runtime consumer is
added.

Why: broad asset wildcards silently carried redundant icon packs and large
source previews into the single executable. Copying the same WAVs and ordinary
512-pixel logo into Vite output would repeat those bytes again without changing
what either surface renders or plays.

### VIS-021: localization mirrors presentation, never protocol data

`ui.locale` MUST update every open native, terminal, and Web presentation
without restart. Persian uses `fa-IR`, an RTL base direction, mirrored
direction-sensitive geometry, YekanBakh VF, and Persian digits for prose and
human dates. URLs, IP addresses, ports, durations stored in configuration,
hotkeys, paths, endpoint/command names, hashes, and raw diagnostic or terminal
streams MUST remain byte-stable, Latin-digit, and LTR-isolated.

At 320- and 390-pixel phone widths, 768-pixel tablet width, and desktop width,
neither language may create document-level horizontal overflow. Phone
navigation stays fixed and complete, tablet/desktop rails move to the logical
start edge, settings tabs may scroll only inside their own strip, and dialogs
remain viewport-bounded with their content scrolling internally.

Why: reversing the complete tree corrupts protocol strings and terminal data;
translating copy without mirroring geometry creates mixed-direction controls;
and English-only breakpoints often overflow once Persian labels and variable
font metrics are applied.

### VIS-022: captionless GPU hosts must suppress classic non-client paint

Keeping `WS_THICKFRAME` for native resizing also lets USER32 synthesize
`WS_EX_WINDOWEDGE`. Clearing that bit or setting `DWMWA_BORDER_COLOR` is not
enough: captionless windows MUST consume `WM_NCPAINT` and use the documented
`WM_NCACTIVATE` `lParam=-1` path to avoid classic frame repainting. Native
caption mode MUST retain its normal non-client processing. Reassert the DWM
outline policy after activation/show without recursively recalculating frames.

An ordinary GDI child beneath a DirectComposition visual MUST clear its
app-owned backing to black before presenting the alpha foreground. This is
the transparent clear value in the extended glass area, not a screenshot.
Otherwise a native-to-custom frame change can leave opaque white pixels
beneath the GPU surface. Never treat silent fallback to the layered renderer
as proof that a hardware-rendering regression was fixed.

Why: the older layered HWND masked classic frame paint; the new ordinary
DirectX host exposed an approximately 8-pixel white ring. Rectangle/style
assertions passed while the visible frame was still wrong. A screenshot gate
must detect the known-bad frame and reject it in fixed output.

Also assert message ownership directly: a later GPU repaint may hide an
incorrect default NC paint before a screenshot is taken. Test both captionless
handling and native-title-bar delegation; a final clean bitmap alone is not
sufficient. The history-backed negative control reproduced the white frame
when both NC suppression and the GDI backing clear were disabled.

### VIS-023: Settings owns its final placement, not the layout minimum

Prepare Settings' hidden rectangle AFTER localization, theming, and control
setup. Suppress Walk's legacy Show-time minimum-size/centering pass only for
dialogs that explicitly own their prepared bounds. This also applies to
ownerless opens and repeated Show calls. Do not globally change other dialogs.

Persist Settings bounds before HWND destruction, including Cancel. Store size
in 96-DPI logical units and position in virtual-screen pixels. Restore into
the nearest monitor's work area and normalize invalid DPI. Normal-state
captures use screen coordinates; minimized/maximized `WINDOWPLACEMENT`
coordinates need workspace-to-screen conversion. A negative X or Y alone is
valid on a multi-monitor desktop; only the pair (-1, -1) means first-open
centering.

Why: hidden localization/layout shrank the dialog to 540x177 logical units,
and Walk Show could shrink it again. Earlier pre-layout sizing alone did not
fix the bug. Capturing WINDOWPLACEMENT unconditionally also introduces a
taskbar-sized position drift with a top/left taskbar.

After resizing, exercise live locale/font/RTL changes and every Settings tab,
then assert the outer bounds did not change before testing Cancel and reopen.
First-show-only and source-string checks do not cover later layout overwrites.

## Regression catalog

| Historical symptom | Root cause that must not return | Governing invariant |
| --- | --- | --- |
| Tray corners have spikes, notches, a broken shadow, or square material bleed | App-owned masks used different geometry, or the live Windows 11 material host used `DWMWCP_DONOTROUND` instead of the aligned standard round contour | VIS-002 |
| Tray first-open white flash | Parent became visible before its layered first frame was committed | VIS-004 |
| Reopened cached tray shows stale pixels | Cached HWND activated without hidden re-preparation | VIS-004 |
| Tray hover becomes opaque or changes instantly | Opaque selection fill or no time-based hover state | VIS-005 |
| Acrylic appears only after drag stops | Captured/rebuilt backdrop instead of live composition | VIS-001, VIS-006 |
| Black rectangle or ring appears around device/text | Erased/opaque host pixels mixed with a per-pixel layer | VIS-003, VIS-004 |
| White/black fringe around labels | ClearType/GDI pixels treated as one alpha channel | VIS-003 |
| Double border or top gap | Native and custom chrome both reserved/drew an edge | VIS-007 |
| Thick white border only in the GPU path | Captionless `WS_THICKFRAME` received classic GDI non-client paint | VIS-022 |
| White opaque background after native-to-custom chrome | Stale GDI redirection pixels below the alpha DirectComposition visual | VIS-022 |
| Settings opens tiny or loses resized bounds | Localization/layout and Walk Show overwrite prepared dimensions; Settings geometry was not persisted | VIS-023 |
| Settings position drifts on each reopen | Workspace coordinates restored as screen coordinates | VIS-023 |
| Transparent/no-border still has a rectangular shadow | DWM corner/non-client silhouette remained enabled behind an alpha-shaped HWND | VIS-007 |
| Transparent areas still consume clicks | Visual alpha changed without a real cross-process window region | VIS-008 |
| Settings is black, unreadable, flickery, or has square button corners | Full-client glass or uncontrolled native erasing behind GDI controls | VIS-009 |
| Snap never releases or works only once per drag | Latch-only snap state instead of per-`WM_MOVING` hysteresis | VIS-010 |
| Device/menu/caption icon is blurry | Generic bitmap stretch or incomplete ICO pack | VIS-011 |
| Tray changes but taskbar icon does not | State propagated to only one icon owner | VIS-012 |
| Task Manager root/group row is generic while its child window is correct | Window class loaded a nonexistent resource ID and relied only on later per-HWND icons | VIS-019 |
| Executable grows after adding a Web/native asset already used elsewhere | The same WAV/logo/ICO entered multiple embedded filesystems instead of one canonical asset owner | VIS-020 |
| Persian copy is clipped, laid out LTR, or makes the mobile page scroll sideways | Locale changed strings without direction-aware geometry and multi-width responsive validation | VIS-021 |
| URLs, hotkeys, hashes, or terminal output are reversed or use Persian digits | RTL/digit shaping leaked from presentation prose into invariant technical data | VIS-021 |
| Window/menu appears or disappears abruptly | Missing elapsed-time lifecycle transition | VIS-013 |
| Menu reveal/hover/dismissal skips frames while the pointer moves | Fixed-step `WM_TIMER` → invalidation → `WM_PAINT` chain, per-frame DIB/glyph recreation, or a non-coalesced frame queue | VIS-004, VIS-005, VIS-013 |
| Settings tab transition hitches when changing pages | Tab navigation synchronously applies the complete runtime configuration, or its frame waits behind low-priority timer/paint messages | VIS-009, VIS-013, VIS-014 |
| Settings changes apply only after closing | Preview was treated as persistence-only state | VIS-014 |
| Main context menu is classic while tray menu is styled | Native-caption bypass or a swallowed custom-renderer error fell through to `TrackPopupMenu` | VIS-015 |
| A held button never looks depressed, activates too early, or repeats | Custom surface dispatched on mouse/key down instead of arming until matching release | VIS-005 |
| Transparent-to-Blur/Acrylic stays gray until move | Frame/material changed without a synchronous layered-surface and DWM commit | VIS-016 |
| Native caption is white or loses its icon | Caption theme/icon was finalized before later frame mutations, or a state icon replaced the retained product icon | VIS-007, VIS-012, VIS-016 |
| Settings controls visibly assemble after changing tabs | The destination child-HWND tree was revealed before layout and painting completed, instead of being committed beneath one app-owned transition surface | VIS-009, VIS-013, VIS-017 |
| Settings caption lags while dragging or resize breaks after changing monitors | The UI thread performed full hierarchy layout for every sizing message, or first-show bounds retained the wrong monitor's DPI | VIS-018 |
| Dropdown flashes when the pointer moves or has square/fringed corners | App chrome painted before native hover, unchanged pointer motion forced synchronous paints, popup theme was destructively reapplied, or fill/region geometry diverged | VIS-009, VIS-017 |
| Check boxes have solid slabs or a tab visibly paints several times | Capture used opaque background mode, button-class controls missed the effective parent brush, every page was unfrozen, or the terminal overlay was committed more than once | VIS-009, VIS-017 |
| A polished field loses IME, type-ahead, clipboard, accessibility, or Alt-key behavior | Visual customization replaced the native control rather than subclassing only its presentation | VIS-009, VIS-017 |

## Automated coverage

The following tests are release gates:

- `internal/traymenu/corner_region_windows_test.go`: parent/foreground region
  parity at 96, 120, 144, and 192 DPI; four-corner symmetry; material-before-
  region commit ordering; `DWMWCP_ROUND` restoration after composition refresh;
  and no production desktop-capture APIs.
- `internal/traymenu/render_windows_test.go`: transparent outer pixels,
  translucent material hover, premultiplied blending, and transition easing.
- `internal/traymenu/lifecycle_test.go` and `popup_windows_test.go`: hidden
  first-frame commit, cached re-preparation, owner/thread safety, and cache
  disposal.
- `internal/traymenu/fallback_policy_windows_test.go` and `layout_test.go`:
  custom retry without silent native downgrade, intentional accessibility
  reasons, and readable short-monitor density.
- `internal/ui/menu_routing_windows_test.go`: tray, client, caption, and
  keyboard context requests share the styled presenter.
- `internal/ui/button_activation_windows_test.go`: held pointer/keyboard press,
  leave and re-entry, release-inside, release-outside, cancellation, key-repeat
  suppression, and release-only dispatch wiring.
- `internal/ui/text_windows_test.go` and `paint_windows_test.go`: grayscale
  layered glyphs and dual-background alpha recovery.
- `internal/graphics/resize_test.go`: alpha-correct resize without colored
  edge bleeding.
- `internal/winfx/pixelregion*_test.go`: exact-alpha click-through, including
  an actual cross-GUI-thread integration test.
- `internal/ui/backdrop_transition_windows_test.go`: transparent-to-material
  transition selection and complete, ordered synchronous commit.
- `internal/ui/window_state_windows_test.go` and
  `internal/winfx/backdrop_test.go`: frameless shadow policy, split
  caption/taskbar icon ownership, final native-caption theme ordering, and
  synchronous non-erasing frame redraw flags.
- Dialog page/tab/button/field/theme tests: atomic page staging, readable
  selected states, continuous backgrounds, persistent-buffer lifetime, no
  rectangular backplate or double stroke, deterministic mnemonics, and native
  keyboard/input/accessibility message pass-through.
- Window fade tests: elapsed-time opacity, renderer backend separation, and
  temporary native-style restoration.
- Popup and dialog-motion tests: elapsed-time settlement after delayed ticks,
  coalesced generation-safe frame scheduling, cached foreground composition,
  and tab-only navigation that never invokes full runtime configuration
  preview.
- Dialog geometry tests: owner-monitor centering at target DPI, negative
  virtual-screen coordinates, oversized work-area recovery, monitor-bounded
  minimum tracking, strict filtering of only the native sizing-loop advisory
  messages, and generation-safe latest-only layout completion.
- Dialog combo/page tests: exact rounded-region bounds and symmetry across
  DPI, stable popup-theme caching, native-first pointer observation,
  one offscreen native/chrome commit, transparent capture background mode,
  target-only redraw blocking, and single terminal handoff.
- Asset, Web-handler, and static-export tests: PE-only product ICO exclusion,
  unused pack exclusion, exact canonical WAV/logo bytes, and one copy of each
  shared asset in the deterministic archive.

## Manual release matrix

The automated native lifecycle/screenshot subset runs with:

```powershell
./tools/test-native-windows.ps1 -OutputDirectory C:\path\to\test-evidence
```

The runner generates a temporary Common Controls v6/DPI resource for each test
package, removes it in `finally`, and leaves only development test executables,
logs, and PNGs in the requested evidence directory (never in the release
distribution). It owns all tested windows and injects no mouse or keyboard
input. `-ResourceTool` accepts an already-built pinned goversioninfo tool for
offline use. `-BorderBaseline` optionally verifies the pixel classifier against
a known-bad capture from the same fixture. Keep other windows from covering
the fixture during screen capture; test screenshots are not compositor input.

Composition output cannot be completely proven from an offline bitmap. Before
shipping a change in the affected code, check:

- Windows 10 and Windows 11;
- 100%, 125%, 150%, and 200% DPI;
- system light, system dark, and High Contrast;
- tray prewarm/reuse enabled and disabled;
- first popup, cached popup, hover transition, click-away, and keyboard close;
- tray icon, main client, and native/custom caption context-menu parity;
- solid, Acrylic, and Blur fallbacks;
- live transparent-to-Acrylic and transparent-to-Blur changes without moving,
  resizing, deactivating, or refocusing the window;
- main title bar none/custom/native and border auto/none;
- native Acrylic and Blur caption material, dark/light caption state, and
  product icon before and after a DPI transition;
- Settings live move and resize on mixed-DPI monitors to the left, right,
  above, and below the primary display, including a monitor with a smaller
  work area;
- every Settings dropdown under continuous pointer motion, repeated open/close,
  keyboard type-ahead, light/dark/High Contrast, and 100–200% DPI;
- every Settings tab under rapid pointer and keyboard navigation, checking
  that controls never assemble in stages or acquire opaque rectangular
  backplates;
- transparent + no-border click-through;
- window show, close-to-tray/show, and terminal exit transitions.

For a liveness check, move or animate content behind an open glass surface.
The material must update immediately. A screenshot may record the result, but
must not be used by the renderer.

## VIS-024: Verify the physical Persian font, including hidden tab pages

`walk.Font.Family()` returns the requested logical family even if Windows maps
it to Arial. Font registration success is also insufficient. Select the actual
HFONT into a DC and use `GetTextFaceW` to verify `Yekan Bakh VF Regular` and
`Yekan Bakh VF Bold` on variable-font-capable Windows. The bare typographic
family reproduced an Arial substitution on Windows 11.

Use the one canonical VF asset and a shared process-private registration.
Dialog entry points must work independently of the main Shell. Preserve
heading sizes and use the bold named face when changing locales; updating the
root font alone leaves explicitly styled child fonts unchanged. Walk's
TabWidget is not a Container: traversal must visit its Pages explicitly.

The vendored TabWidget must map its entire tab-client rectangle using
`MapWindowPoints(..., 2)`. Its old one-point `ScreenToClient` calculation
produced an origin near the opposite edge under RTL; subtracting that origin
twice collapsed selected-page widths to zero after a layout pass. Assert
positive usable page dimensions after resizing and switching languages/tabs.
Microsoft explicitly requires [MapWindowPoints for mirrored layouts](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-screentoclient).

Regression coverage reads actual native control font handles (including all
six Settings pages), verifies language round trips and retains the existing
geometry/reopen tests. Main-renderer and popup HFONTs are checked at 96, 120,
144 and 192 DPI. Native screenshots include the Persian dashboard, Settings
pages and diagnostics. Fixture HWNDs are topmost so unrelated windows do not
invalidate screen evidence; no input is injected into another application.

## VIS-025: native dialog chrome uses fractional coverage

Rounded input/button HWNDs must not use binary `SetWindowRgn` masks for their
visible contours. Settings chrome shares `dialograster_windows.go`: rounded
gradient fills, borders/focus rings, tab highlights, checkbox marks, chevrons
and recorder pills use premultiplied BGRA coverage at the final physical size.
Keep existing canonical control radii and native-resolution text; do not blur
or supersample entire controls to hide jagged geometry. The main/tray DWM corner
ownership is unchanged.

Rounded input corners blend into the aligned ancestor material, not the input's
own solid brush or a guessed flat dialog color. The native input remains in
charge of text, IME, selection, type-ahead and accessibility. The custom colored
focus ring replaces only the native dotted focus ornament; clearing custom
theming restores native keyboard cues and High Contrast behavior.

The shared chrome cache is bounded to 64 DIBs / 16 MiB and cleared with native
paint-buffer destruction/invalidation. Parent-material masks are refreshed from
the live ancestor brush, never from screen capture; unchanged masks/colored
rasters reuse resources between hover frames. Coverage tests include 96, 120,
144 and 192 DPI, symmetric fractional edges, premultiplied channels, stroke-only
transparent interiors, joined glyph strokes, real-HDC blending and cache bounds.

## Change gate

Settings form geometry must reserve visible scrollbar footprints even for
ordinary non-wrapping grids. Its content must never extend beneath a vertical
scrollbar without an actual horizontal scroll range. Center row labels against
the measured native dropdown HWND height, not a dialog-unit estimate that
USER32 later clamps. Give spare form width to editors rather than label columns;
compound snap controls own a separate row. Native viewport regression coverage
switches every tab, resizes to normal/minimum/wide bounds, and changes English
and Persian live without replacing native input or dialog ownership (VIS-017,
VIS-018, VIS-023).

Any edit to `internal/ui`, `internal/traymenu`, dialog paint code, or relevant
`internal/winfx` composition/region helpers must:

1. identify the affected `VIS-*` rules;
2. preserve one authoritative owner for alpha, corners, text, and chrome;
3. add or update a regression test for the failure mode;
4. run `go test ./...` and `go vet ./...`;
5. run the applicable manual matrix where Windows compositor output changed.

Never “fix” a live-material problem by capturing the desktop. Never introduce
a second app-owned corner equation to patch one DPI. The tray's Windows 11 DWM
material contour is deliberately paired with the canonical 8-DIP app mask;
do not replace it with `DWMWCP_DONOTROUND`. Fix ownership or canonical geometry
instead.
