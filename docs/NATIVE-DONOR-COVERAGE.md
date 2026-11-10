# Native desktop source preservation and PCController integration

Audited 2026-11-06 against the referenced **Build RayanLamp Go app** chat
(`019f8cfc-271a-72e1-8747-e3eb883de5ec`) and its actual clean checkout.

## Complete source import: verified

The donor is not a selection of similar-looking functions. The **entire tracked
tree** of `AtomicDeploy/RayanLamp` at
`b5847a0c86f9931a86048d418ff8ff5045f52b0a` was imported under
`Tools/NativeDesktop`. All **2,433 files** are represented in the imported tree,
including dependencies, assets, tests, tools, documentation and recovery records.

- Donor tree: `c515cb331313153b8866d241c3897aa622b25e3e`.
- Squashed subtree commit: `08ef6eb74ad38ba86a9a356d5d5ed3f8fda95c8a`.
- Parent integration commit: `3aed9ef6`.
- The subtree tree object equals the donor tree object exactly. This verifies
  every pathname, file mode and blob, not merely file counts or ancestry.
- Current working-copy result: **2,048 identical files, 377 adapted files, seven
  regenerated content-hashed Web bundles, one explicitly excluded font, zero
  missing source files**. Exact old/new generated bundle paths are audited; the
  original bytes remain in the immutable import. Git clean/EOL filters
  are applied to avoid treating Windows line endings as source changes.
- Desktop identity adaptation renames 65 imported paths without dropping their
  source. `Tools/Audit/native-desktop-path-migration.json` records each old/new
  pair, and the audit hashes the current target. Current Go imports use
  `pccontroller.local/desktop`; models, resources, SDK names, test switches and
  current UI copy identify PCController. Historical project names remain only
  as provenance/attribution, not as the current product identity.
- Existing desktop settings retain a read-only migration for the former
  connection key; new saves emit `device`. Compatibility wire vocabulary is
  isolated in `Tools/NativeDesktop/compatibility/device-protocol.json`. Lighting
  icon IDs and user channel labels are intentionally unchanged.
- Original donor blobs remain recoverable from that import even when the working
  adaptation changes a file. The donor's individual commits were **not** merged
  as PCController ancestry: this is a squashed source import, not full-history
  integration. Recovered-history documents remain imported with their caveats.
- Build products, caches, local secrets and untracked scratch material are not
  source-tree omissions. The donor checkout was clean at the audit.
- The private GlassDWM C# prototype and donor firmware outside that repository were not
  part of this donor repository. Do not claim they were imported.

The only intentionally absent donor file in the working adaptation is
`assets/fonts/yekanbakh-vf.ttf`: David explicitly requested using PCController's
WebUI font instead. `assets/fonts/Vazirmatn-Regular.ttf` and its OFL notice replace
it. Preserve that decision; do not silently restore the restricted donor font.

## Repeatable audit

From the PCController repository root:

```powershell
node Tools/Build/audit-native-donor.mjs
node Tools/Build/audit-native-donor.mjs --donor 'C:/path/to/original-donor'
node Tools/Build/audit-native-donor.mjs --json
```

The read-only audit verifies the immutable import, checks every working-copy
donor path, compares Git-normalized blob hashes, and lists adaptations separately.
`--json` emits the complete per-file inventory with original/current blob IDs.
`--donor` additionally rejects a newer/different or dirty donor checkout so an
old snapshot cannot be reported as the latest source. Missing source fails the
audit; the font exclusion is explicit. It does not build, run tests, contact
devices, stage changes, or certify the behavior of modified code.

## Feature inventory: source present does not mean runtime parity

Paths below are relative to `Tools/NativeDesktop`. **Connected** means a source
path is connected to the PCController adapter, not that physical acceptance or
cross-DPI/theme verification is complete. **Partial** and **Retained** remain
unfinished integration work; neither counts as full PCController feature parity.

| Donor capability | Actual imported source | PCController status |
| --- | --- | --- |
| DirectX/DirectComposition, GDI fallback, device-loss and hidden-window resource handling | `internal/windx`, `internal/graphics`, `internal/ui` | Connected through the real donor shell; full lifecycle/soak acceptance still separate |
| DWM/acrylic, Fluent controls, reveal/elevation, high contrast and reduced motion | `internal/winfx`, `internal/design`, `internal/ui/paint_windows.go` | Connected; acrylic regression repaired; not every native form uses the same material |
| APNG, vector rendering, pixel-correct icons and bounded caches | `internal/animation`, `internal/vector`, `assets` | Source retained; shell uses donor drawing infrastructure; lamp artwork is not substituted for board controls |
| Tray, styled popup, close-to-tray, always-on-top, snap and custom/native chrome | `internal/traymenu`, `internal/ui`, `internal/winfx` | Connected; GUI consumes the original host tray model with custom/native renderer preference, plus macOS-style chrome |
| Native settings, diagnostics and hotkey recorder dialogs | `internal/dialogs` | Settings and recorder connected: five adapted tabs, live preview/cancel, custom WAV, notifications, automation and startup. Full donor diagnostics remains partial |
| English/Persian, RTL, localization and native font selection | `internal/i18n`, localization files, `internal/winfx/persianfont_windows.go` | Partial: donor engine preserved and font replaced; new controller pages still contain English literals |
| Macro hotkeys, conflict detection and Windows registration | `internal/ui`, `internal/dialogs/hotkey_recorder_windows.go`, `internal/winfx` | Recorder/registration connected to compact/dashboard, hardware channels, Console/TUI and Show. Explicit saved preferences survive restart; no arbitrary output is selected for a former lamp shortcut |
| Toast/balloon actions, imported WAV and shared sound pipeline | `internal/notify`, `internal/sfx`, `internal/eventeffects` | Running/Idle and targeted host messages connected through PCController's branded notifier; explicit toast/balloon selection, bounded delivery queue and imported WAV retained. OS sound suppressed when desktop owns the cue; actual audio/notification acceptance remains separate |
| Taskbar thumbnail controls, progress, Explorer recreation | `internal/ui/taskbar_windows.go`, `internal/winfx/taskbar_windows.go` | Connected to four front-panel keys with navigation/connection/busy gates, status/error/suspend progress and existing Explorer recreation. Actual Explorer thumbnail-click acceptance remains separate |
| Suspend/resume, lock/unlock and network-change recovery | `internal/ui`, `internal/appcore`, `internal/winfx` | Partial: window lifecycle reused; PCController transport remains owned by its existing core, never donor lamp reconnection |
| Bounded automation queue, timeouts, output and environment contract | `internal/automation` | Connected to observed Running/Idle, targeted messages and Windows events; opt-in local scripts, timeout/output bounds and PCCONTROLLER_* environment. Standalone PCCONTROLLER_DESKTOP_* contract retained |
| Compatibility HTTP/WebSocket, reconnect, fallback, address caching | `internal/device`, `internal/appcore`, `compatibility` | Retained, intentionally dormant in PCController mode; hardware uses existing PCController RPC, not compatibility endpoints |
| Unified dispatcher, CLI/VT100, named pipes, WM_COPYDATA, loopback API | `internal/commands`, `internal/cli`, `internal/ipc`, `internal/wmipc`, `internal/localapi` | Donor source retained; controller uses its existing owner RPC; donor WM_COPYDATA/local API are not board command entry points |
| Public Go client, native SDK DLL and COM Automation | `sdk`, `internal/dllapi`, `internal/comserver`, `cmd` | Retained; donor exports are not yet adapted PCController SDK/COM contracts |
| URI activation, Jump Lists, startup and self-install | `internal/shellintegration`, `internal/selfinstall` | Startup connected using exact-owned PCController Run entry and `gui` argument, only on an explicit saved preference change. URI/Jump List/self-install use the existing host paths; donor-specific registrations remain dormant |
| Full-screen TUI, attach/standalone/headless and console behavior | `internal/tui`, `internal/terminal`, `internal/headless`, `internal/headlessweb` | Donor code preserved; PCController uses its existing TUI and Console toggle, not the lamp TUI |
| Responsive Preact SPA/PWA, terminal, multitab sync and offline shell | `webui`, `internal/webui` | Preserved in full; PCController's own WebUI remains authoritative, not replaced with a lamp dashboard |
| Direct/relay/auto Web transport and static exporter | `webui`, `tools/internal/webexport` | Retained; lamp direct transport is not a PCController board transport |
| Allow-listed diagnostics, redaction and device terminal | `internal/diagnostics`, `internal/deviceconfig`, `internal/deviceterminal` | Retained; controller has dedicated connection/events pages, not complete donor diagnostics/terminal parity |
| Develop/production editions, resource generation, cross-build and packaging | `tools`, `build-edition.txt`, `Makefile`, `.github` | All donor build sources retained; nested workflows do not automatically become PCController CI jobs |
| Original graphics/sound generators, licenses, vendor and regression tests | `assets`, `tools`, `vendor`, `docs`, test files | Preserved, except the explicit font replacement; imported tests are not evidence that the adapted app passed them |

The PCController-specific seam is `desktop` / `presentation`,
`internal/ui/controller_*_windows.go`, and
`../Controller/cmd/controller/native_*_windows.go`. It currently adds front-panel
keys and faded seven-segment mirroring, observed hardware channels, PWM sliders,
metadata editing/reordering, board connection, RF/program/buzzer/strip/display
forms, firmware/backups and events. Those additions do not erase the retained
donor implementation and do not close every integration gap listed above.

### Desktop restoration and documentation previews

Compact mode again preserves the saved view and renders only the front panel.
The card has a direct compact/expand action, and the last dashboard dimensions
survive a round trip, Settings previews and restart. Startup no longer clears
saved hotkey, notification, sound, automation or integration preferences.
Detachable front-panel, individual-channel and telemetry windows share the same
host observation/command owner; each has its own compositor, saved placement and
visibility. Magnetic snapping includes sibling windows. Native fixture checks
cover show/hide, observation propagation, no duplicate tray, and sibling snapping.

The original `Tools/Controller/internal/nativeshell/menu.go` remains the tray
action owner: status; Dashboard, Controls, Workbench, Updates and Settings when
connected; contextual connect/reconnect; and Exit. The GUI adds desktop-only
actions without replacing that host list. Preferences can select the native
Windows menu renderer instead of the styled menu.

The original menu model is byte-for-byte unchanged from `a304c3ec` (blob
`c9919e90c8c0d2c8a385317c2f2070b4ba6452ce`). Its original options are:

- Status: connected (including port), connecting/reconnecting, paused or offline.
- Dashboard (the default), Controls, Workbench, Updates and Settings, only while
  the board is connected.
- Connect now when offline/paused; Reconnect now while connecting/reconnecting;
  Reconnect controller while connected.
- Exit, which exits the host. The added Close desktop action closes only the GUI.

The GUI prepends that complete authoritative list to its desktop-only actions;
both acrylic and native HMENU renderers consume the resulting shared list.
Native Windows menus is a persistent option under Window appearance, also
available in Preferences > Desktop. High contrast selects native automatically.
The normal GUI-owned host starts with `--no-tray`, and detached widgets create no
additional tray. Standalone Web/TUI hosts retain their existing native tray.

The 2026-11-06 follow-up audit found and repaired actual bridge regressions:
host separators had acquired command fields and failed menu validation, Dashboard's
default marker was discarded, left-click was intercepted by an inert lamp-toggle
path, and the GUI flattened four-state tray icons/tooltips to connected/offline.
The adapter now retains valid separators, default/keyboard semantics, all four
packaged product status icons and the canonical tooltip. Left/right clicks and
keyboard invocation open the shared menu. The native renderer retains icons as
DPI-scaled 32-bit bitmaps in the icon column, independently of checkmarks.

Focused checks compare every original action, label, enabled/default/separator
state and dispatch identity, validate the merged model under both renderer
preferences, inspect native HMENU flags/artwork at 96/144/192 DPI, and verify
click routing without hardware actuation. They are not human tray-click or
older-machine acceptance.

`node Tools/Build/update-readme-previews.mjs` regenerates the README gallery from
actual GUI/TUI renderers with isolated sample data. These screenshots are not
live-board, deployment or full donor-parity acceptance evidence.

### Why the runtime features disappeared

Integration commit `32a81c5f` unconditionally cleared hotkeys, disabled
notifications/sound, automation, local API and Windows startup on every launch,
and bypassed taskbar initialization and the full donor Settings window. Those
guards prevented the lamp actions from reaching PCController, but replacements
were not wired. Keeping the source tree did not preserve the user experience.
The restored paths above now use PCController actions and identity; the dormant
lamp core is still never started.

The earlier isolated Settings screenshots also used donor-default blue tokens
and a DPI-unaware test executable. Settings fixtures now activate the same
PerMonitorV2/Common Controls context as the packaged application, verify actual
HFONT pixel heights and use the same `design.ControllerTokens` as production.
The gallery captures native pixels without resizing and includes the restored
Settings dialog. Neutral surfaces, purple accents and checkbox glyphs all use
the shared palette; high contrast retains the native checkbox renderer.

## Completion rule

The dedicated [native feature ledger](NATIVE-FEATURE-PARITY.md) tracks the current
Web/TUI integration work and its remaining gates. [Native activity RPC notes](NATIVE-ACTIVITY-RPC.md)
describe the shared preferences, intent fences and safe motion leases.

Full native feature parity remains **open**. Do not replace that status with a
passing source audit, successful build, source-history marker, or screenshot.
Close each retained/partial row only after its PCController identity, settings,
current-owner RPC behavior, contextual UI and appropriate runtime verification
are implemented. Do not enable dormant lamp transports, automation or outputs
as a shortcut to making a feature appear active.
