# Windows notification and backlog checkpoint

This short checkpoint links the corrective work recorded on 2026-08-12. It is
intentionally a map, not a replacement for the canonical generated
[requirements backlog](Requirements-Backlog.md).

| Area | Tracker record | Current checkpoint |
|---|---|---|
| Windows toast/balloon app icon | [#43](https://github.com/atomicdeploy/PCController/issues/43) | `web` now establishes the per-user AppUserModelID and Start-menu identity before the host runtime starts; Windows can associate unpackaged WinRT toasts with the executable's `APP` icon. Hardware observation remains required. |
| RGB status operation/configuration | [#21](https://github.com/atomicdeploy/PCController/issues/21), [#107](https://github.com/atomicdeploy/PCController/issues/107), [#71](https://github.com/atomicdeploy/PCController/issues/71) | Current RGB-off regression, compact shared engine, EEPROM/build defaults, and live visual proof are tracked together. |
| BT Audio active-HIGH identification | [#19](https://github.com/atomicdeploy/PCController/issues/19), [#70](https://github.com/atomicdeploy/PCController/issues/70) | Electrical type/polarity is a commissioning and hardware-acceptance item; it must not be inferred from active-LOW inputs. |
| Web UI overhaul/disconnected behavior | [#101](https://github.com/atomicdeploy/PCController/issues/101), [#124](https://github.com/atomicdeploy/PCController/issues/124) | Truthful disconnected UI, sidebar repair, client-OS hints, and cross-surface parity are open. |
| Generated definitions and profile deployment | [#125](https://github.com/atomicdeploy/PCController/issues/125), [#66](https://github.com/atomicdeploy/PCController/issues/66) | Menus, opcodes, profiles, firmware tables, and operator docs need one generated source plus a standards-based project/docs layout. |
| Authentication | [#148](https://github.com/atomicdeploy/PCController/issues/148), [#49](https://github.com/atomicdeploy/PCController/issues/49) | The unintended current gate must be removed. The full local/remote session design is explicitly deferred until requested. |
| Host CPU/timeline growth | [#149](https://github.com/atomicdeploy/PCController/issues/149) | Preserve the live evidence, then profile the apparent unbounded CPU and `timeline.jsonl` growth before applying retention/backpressure. |

## Verification

- Focused host test: `go test ./cmd/controller -run
  'TestWebDesktopIntegrationOptionsUseCurrentPresentation|TestWindowsNotifier'
  -count=1` passed.
- Windows host smoke build: `go build -trimpath -o
  Tools/Controller/bin/controller-toast-check.exe ./cmd/controller` passed.

The change deliberately does not touch a connected board or enable sound.
