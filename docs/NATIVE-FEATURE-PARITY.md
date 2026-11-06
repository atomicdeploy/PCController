# Native desktop feature parity

This is an implementation and acceptance ledger, not a claim that every native
workflow is complete. **Overall parity is OPEN.** Source preservation is audited
separately in [the donor inventory](NATIVE-DONOR-COVERAGE.md).

## Connected native workspaces

Open **Board control > Activities** or **All activities** in the shared tray.
Each activity uses the selected PCController owner's existing authenticated
command/RPC engine. Opening an editor does not learn RF, flash firmware, run an
effect, or change an output.

| Web/TUI area | Native implementation | Remaining gate |
| --- | --- | --- |
| Front panel, keys, seven segments | Existing acrylic panel, segment fades, compact mode and detachable widgets | Physical mirroring/audio and multi-DPI soak |
| Outputs | Profile-resolved cards, Off/On order, PWM sliders, inline name/icon editing, reorder, locks/visibility, semantic active state | Physical click/slider acceptance; richer icon catalog |
| Board operating profile | Profile name/mode, raw relay exposure, semantic routing list/add/edit/remove and physical-target choices | Real attached-board profile transitions |
| Board EEPROM settings | Native scalar controls for every exposed settings field; generation/settings digest precheck | Board write/readback acceptance |
| Menus | Live catalog, page navigation, visibility, order and host display labels | Physical menu acceptance |
| RF | Complete known/unlearned waveform identities, dim observations, modal counters, 250 ms presence, explicit add/map/remove, persistent learning preferences, host binding editor and transmitter | Structured binding/hold-profile editor, richer metadata; RF hardware acceptance |
| Motion | Equal-width held Up/Stop/Down controls; press/release/focus/capture lifecycle, renewable 700 ms leases, owner tokens and bounded stop watchdog | Motor/door-interlock physical acceptance |
| Programs/effects | Catalog selection, metadata, folder choices/create, sequence/strip draft creation, complete advanced definition editor, execution policy, recording, stop behavior and application mode | Native graphical steps/timeline, file import/export and folder rename |
| Buzzer | Contextual tone/melody playback and stop, repeats and loop | Actual audio on supported hosts/boards |
| LED strip/status lighting | Pixel count/order/FPS/color/stream controls, RGB preview/release, board profiles and full reactive status-policy editor | Every procedural-effect preset editor and physical color/timing acceptance |
| Telemetry | Six valid measurements, shared visibility/precision, antialiased live charts, history queries and event table | Interactive historical-chart navigation/export |
| Firmware/toolchain | System-first provisioning plan/provision/sync, build without upload, USBasp status/install | Real tool installation/build and board acceptance |
| Updates/backups | Local file picker, chunked verified staging and no-overwrite export, URL staging, kind-scoped artifact inventory, explicit backup/apply, release/workflow discovery and staging | Richer progress presentation and dedicated cross-product gateway workflows (shared gateway implementation is now present) |
| Network/integrations | Discovery, bridge/instance inspection, persistent peers, UDP/discovery/local-device/DataHub/lifecycle/buzzer preferences | Rich peer navigation; live transport acceptance |
| Host OS | Facts, virtual keys, brightness/power with existing policy gates, OS policy, hotkeys, manual host automations | Structured general event-rule/notification editors |
| Data/technical tools | Owner-relative DataHub GET/POST, bounded table/JSON/export, I²C workbench, diagnostics and command catalog/terminal | Large streaming exports and specialized opcode/message editors |
| Preferences | Full host UI settings round-trip with CAS; PWM calibration; identity; retained five-tab desktop settings | New activity localization/RTL, explicit direction, compact-number/audio preference presentation and ownership unification |

The command terminal and advanced JSON editors are useful expert tools, **not
substitutes for missing dedicated graphical workflows**. Donor SDK/COM adaptation
and other retained-only features remain open in the separate donor ledger.

## Shared editor behavior

- Tables select by stable identity; restored selection hydrates the editors.
- Invalid numeric literals survive until corrected. Actions validate their own
  section; whole-form saves validate every applicable field.
- Pending requests disable edits. Errors retain drafts. Unchanged Save is inert.
- Profile/routing saves preserve drafts in the other tab and retain discard warnings.
- Refresh/Close stay outside scrolling editors; tabs retain their active section.
- Unknown enum values remain visible and round-trip without selecting a default.
- Native table body/header/selection use the shared palette; high contrast uses
  system colors. Opaque themed GDI dialogs intentionally remain separate from the
  live acrylic main/widget/tray renderers, avoiding the full-client black/text-loss
  regression.
- Channel intent captures host, connection generation, board identity, profile
  revision and PWM conversion revision from the rendered catalog. Admission and
  serial-write guards reject superseded owners. Mutations are not replayed.

## Verification and reproducible previews

Focused host suites cover native adapters, UI/status-policy CAS, peers/hotkeys,
artifact staging, RF caching, motion leases and composed serial-write guards.
All 47 native module packages have passed the stable-path runner. Native-window
fixtures separately exercise RF form layout and release-before-start-ACK motion
input using mocked callbacks, without starting a core or transport.

```powershell
$env:GOFLAGS='-mod=mod'
node Tools/Build/go-tests.mjs --package cmd/controller --package internal/ipcjson --package internal/control --package internal/link --retest
$env:GOFLAGS='-mod=vendor'
node Tools/Build/go-tests.mjs --module Tools/NativeDesktop --retest
$env:PCCONTROLLER_DESKTOP_WINDOW_TESTS='1'
node Tools/Build/go-tests.mjs --module Tools/NativeDesktop --package internal/ui --run 'TestControllerActivityFormNativeLayout|TestControllerMotionReleasesPendingOwnedPress' --retest
node Tools/Build/update-readme-previews.mjs
```

Screenshots are offline fixtures from real renderers, not evidence of a connected
board. Build/package, installed identity, CI/merge, physical and human acceptance
must be reported independently. No physical tests are authorized by merely
opening these pages or regenerating the gallery.
