# RayanLamp source preservation and PCController integration

Audited 2026-11-05 against the referenced **Build RayanLamp Go app** chat
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
- Current working-copy result: **2,389 identical files, 36 adapted files, seven
  regenerated content-hashed Web bundles, one explicitly excluded font, zero
  missing source files**. Exact old/new generated bundle paths are audited; the
  original bytes remain in the immutable import. Git clean/EOL filters
  are applied to avoid treating Windows line endings as source changes.
- Original donor blobs remain recoverable from that import even when the working
  adaptation changes a file. The donor's individual commits were **not** merged
  as PCController ancestry: this is a squashed source import, not full-history
  integration. Recovered-history documents remain imported with their caveats.
- Build products, caches, local secrets and untracked scratch material are not
  source-tree omissions. The donor checkout was clean at the audit.
- The private GlassDWM C# prototype and lamp firmware outside RayanLamp were not
  part of this donor repository. Do not claim they were imported.

The only intentionally absent donor file in the working adaptation is
`assets/fonts/yekanbakh-vf.ttf`: David explicitly requested using PCController's
WebUI font instead. `assets/fonts/Vazirmatn-Regular.ttf` and its OFL notice replace
it. Preserve that decision; do not silently restore the restricted donor font.

## Repeatable audit

From the PCController repository root:

```powershell
node Tools/Build/audit-native-donor.mjs
node Tools/Build/audit-native-donor.mjs --donor 'C:/path/to/RayanLamp'
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
| Native settings, diagnostics and hotkey recorder dialogs | `internal/dialogs` | Partial: source retained in full; current PCController preferences/forms expose only a subset |
| English/Persian, RTL, localization and native font selection | `internal/i18n`, localization files, `internal/winfx/persianfont_windows.go` | Partial: donor engine preserved and font replaced; new controller pages still contain English literals |
| Macro hotkeys, conflict detection and Windows registration | `internal/ui`, `internal/dialogs/hotkey_recorder_windows.go`, `internal/winfx` | Retained; donor lamp hotkeys disabled in controller mode until board-action binding exists |
| Toast/balloon actions, imported WAV and shared sound pipeline | `internal/notify`, `internal/sfx`, `internal/eventeffects` | Retained; donor notification/sound configuration disabled in controller mode; do not report it as connected |
| Taskbar thumbnail controls, progress, Explorer recreation | `internal/ui/taskbar_windows.go`, `internal/winfx/taskbar_windows.go` | Retained; donor lamp taskbar initialization explicitly bypassed in controller mode |
| Suspend/resume, lock/unlock and network-change recovery | `internal/ui`, `internal/appcore`, `internal/winfx` | Partial: window lifecycle reused; PCController transport remains owned by its existing core, never donor lamp reconnection |
| Bounded automation queue, timeouts, output and environment contract | `internal/automation` | Retained; donor automation disabled in controller mode pending safe PCController event/action adaptation |
| Lamp HTTP/WebSocket, reconnect, fallback, address caching | `internal/lamp`, `internal/appcore` | Retained, intentionally dormant in PCController mode; hardware uses existing PCController RPC, not lamp endpoints |
| Unified dispatcher, CLI/VT100, named pipes, WM_COPYDATA, loopback API | `internal/commands`, `internal/cli`, `internal/ipc`, `internal/wmipc`, `internal/localapi` | Donor source retained; controller uses its existing owner RPC; donor WM_COPYDATA/local API are not board command entry points |
| Public Go client, native SDK DLL and COM Automation | `sdk`, `internal/dllapi`, `internal/comserver`, `cmd` | Retained; donor exports are not yet adapted PCController SDK/COM contracts |
| URI activation, Jump Lists, startup and self-install | `internal/shellintegration`, `internal/selfinstall` | Retained; donor registrations must not register RayanLamp identities for PCController |
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
Detachable front-panel, individual-channel and telemetry windows share the same
host observation/command owner; each has its own compositor, saved placement and
visibility. Magnetic snapping includes sibling windows. Native fixture checks
cover show/hide, observation propagation, no duplicate tray, and sibling snapping.

The original `Tools/Controller/internal/nativeshell/menu.go` remains the tray
action owner: status; Dashboard, Controls, Workbench, Updates and Settings when
connected; contextual connect/reconnect; and Exit. The GUI adds desktop-only
actions without replacing that host list. Preferences can select the native
Windows menu renderer instead of the styled menu.

`node Tools/Build/update-readme-previews.mjs` regenerates the README gallery from
actual GUI/TUI renderers with isolated sample data. These screenshots are not
live-board, deployment or full donor-parity acceptance evidence.

## Completion rule

Full native feature parity remains **open**. Do not replace that status with a
passing source audit, successful build, source-history marker, or screenshot.
Close each retained/partial row only after its PCController identity, settings,
current-owner RPC behavior, contextual UI and appropriate runtime verification
are implemented. Do not enable dormant lamp transports, automation or outputs
as a shortcut to making a feature appear active.
