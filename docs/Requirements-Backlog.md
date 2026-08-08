# Requirements Backlog

This is the canonical public map from normalized project requirements to GitHub issues. Closely related requests are grouped into one verifiable requirement; raw conversation text, machine-local paths, and private audit data are intentionally excluded.

- Repository: [atomicdeploy/PCController](https://github.com/atomicdeploy/PCController)
- Normalized requirements: **69**
- Open: **57**
- Closed with current evidence: **12**
- State policy: hardware, live-system, regression, partial-integration, and finalization work stays open until its own acceptance evidence exists.

## [#1 — Firmware architecture, flash budget, EEPROM, and reset safety](https://github.com/atomicdeploy/PCController/issues/1)

3 open / 2 closed / 5 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `fw-core-architecture` | [#15](https://github.com/atomicdeploy/PCController/issues/15) | ✅ closed | Reduce firmware entry point to modular, documented domain composition |
| `mcu-eeprom-settings` | [#16](https://github.com/atomicdeploy/PCController/issues/16) | ✅ closed | Persist independent MCU settings with CRC, deferred writes, and page defaults |
| `reset-safety-journal` | [#17](https://github.com/atomicdeploy/PCController/issues/17) | 🟡 open | Complete graceful reset safety and reliable reset-cause journal telemetry |
| `firmware-identity-layout-time` | [#18](https://github.com/atomicdeploy/PCController/issues/18) | 🟡 open | Finalize compact build identity, time model, flash budget, and migration architecture |
| `mcu-event-automation` | [#87](https://github.com/atomicdeploy/PCController/issues/87) | 🟡 open | Persist compact board-owned event automations for offline-safe actions |

## [#2 — Board peripherals, sensors, displays, lighting, and audio](https://github.com/atomicdeploy/PCController/issues/2)

4 open / 1 closed / 5 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `board-pin-map-inputs` | [#19](https://github.com/atomicdeploy/PCController/issues/19) | 🟡 open | Validate the complete board pin, shift-register, BT Audio, and reed mapping |
| `measurement-sensors-i2c` | [#20](https://github.com/atomicdeploy/PCController/issues/20) | 🟡 open | Deliver stable INA219 and dual-DS18B20 measurements with conflict-free I2C discovery |
| `pwm-lighting-rgb-strip` | [#21](https://github.com/atomicdeploy/PCController/issues/21) | 🟡 open | Complete PWM ownership, enclosure fade, status RGB, power light, and addressable strip behavior |
| `displays-audio` | [#22](https://github.com/atomicdeploy/PCController/issues/22) | 🟡 open | Finish smooth TM1637, optional LCD, buzzer, melody, and configurable cue behavior |
| `cooperative-host-i2c-profile` | [#23](https://github.com/atomicdeploy/PCController/issues/23) | ✅ closed | Measure and implement the cooperative host-driven I2C/LCD profile |

## [#3 — Relay and motion-control safety](https://github.com/atomicdeploy/PCController/issues/3)

3 open / 0 closed / 3 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `relay-motion-interlocks` | [#24](https://github.com/atomicdeploy/PCController/issues/24) | 🟡 open | Verify relay mapping, break-before-make, side isolation, and safe stop behavior |
| `motion-door-policy` | [#25](https://github.com/atomicdeploy/PCController/issues/25) | 🟡 open | Apply a persisted four-mode motion-door safety policy to every command source |
| `relay-user-controls-break-setting` | [#26](https://github.com/atomicdeploy/PCController/issues/26) | 🟡 open | Expose R5-R8 behaviors and configurable break timing across all control surfaces |

## [#4 — Front panel, menus, keys, and synchronized UX](https://github.com/atomicdeploy/PCController/issues/4)

5 open / 0 closed / 5 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `frontpanel-key-gestures` | [#27](https://github.com/atomicdeploy/PCController/issues/27) | 🟡 open | Complete physical and remote key gestures with responsive hold acceleration |
| `board-menu-hierarchy-settings` | [#28](https://github.com/atomicdeploy/PCController/issues/28) | 🟡 open | Finish nested board menus, editors, save/discard, and persistent default-page behavior |
| `first-run-board-synchronization` | [#29](https://github.com/atomicdeploy/PCController/issues/29) | 🟡 open | Synchronize first-run setup, board initialization, and welcome melody |
| `frontpanel-snapshot-remote-menus` | [#30](https://github.com/atomicdeploy/PCController/issues/30) | 🟡 open | Mirror the live front panel and support remote keys plus PC-defined board menus |
| `lcd-console-status-events` | [#31](https://github.com/atomicdeploy/PCController/issues/31) | 🟡 open | Mirror console context to LCD and make Door the event-aware default page |

## [#5 — 433 MHz RF learning, mappings, and actions](https://github.com/atomicdeploy/PCController/issues/5)

4 open / 0 closed / 4 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `rf-transport-learning-core` | [#32](https://github.com/atomicdeploy/PCController/issues/32) | 🟡 open | Validate 433 MHz receive/transmit and end-to-end learned-record CRUD |
| `rf-learning-sessions-capacity` | [#33](https://github.com/atomicdeploy/PCController/issues/33) | 🟡 open | Add explicit RF learn sessions, unmapped defaults, and capacity for at least 20 records |
| `rf-latency-gestures-guided` | [#34](https://github.com/atomicdeploy/PCController/issues/34) | 🟡 open | Reduce RF action latency and verify click/hold/repeat behavior with guided capture |
| `rf-metadata-format-reorder` | [#35](https://github.com/atomicdeploy/PCController/issues/35) | 🟡 open | Provide consistent RF formatting, metadata, action search, and transactional reorder |

## [#6 — Native UART protocol, telemetry, and event model](https://github.com/atomicdeploy/PCController/issues/6)

2 open / 2 closed / 4 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `protocol-native-uart` | [#14](https://github.com/atomicdeploy/PCController/issues/14) | ✅ closed | Replace Firmata with the native COBS/opcode UART protocol |
| `protocol-command-event-coverage` | [#36](https://github.com/atomicdeploy/PCController/issues/36) | 🟡 open | Complete native command coverage and immediate typed event delivery |
| `protocol-frontpanel-menu-uptime` | [#37](https://github.com/atomicdeploy/PCController/issues/37) | 🟡 open | Extend protocol schemas for live menus, front-panel snapshots, host state, and uptime |
| `protocol-simulator-transport` | [#38](https://github.com/atomicdeploy/PCController/issues/38) | ✅ closed | Maintain deterministic native-protocol simulator and fragmented-transport tests |

## [#7 — PC host TUI, configuration, automation, and OS integration](https://github.com/atomicdeploy/PCController/issues/7)

9 open / 0 closed / 9 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `host-foundation-config-library` | [#39](https://github.com/atomicdeploy/PCController/issues/39) | 🟡 open | Provide the Go host, Charm TUI foundation, separate hot-reloaded config, and reusable APIs |
| `tui-pages-controls` | [#40](https://github.com/atomicdeploy/PCController/issues/40) | 🟡 open | Build polished multipage TUI controls for board, settings, RF, programming, and automation |
| `monitoring-format-history` | [#41](https://github.com/atomicdeploy/PCController/issues/41) | 🟡 open | Improve monitoring presentation, adaptive units, subscriptions, graphs, and timeline |
| `console-command-ux` | [#42](https://github.com/atomicdeploy/PCController/issues/42) | 🟡 open | Finish console history, nested completion, command organization, and clean output |
| `host-automation-hotkeys-os` | [#43](https://github.com/atomicdeploy/PCController/issues/43) | 🟡 open | Complete macros, melodies, automations, hotkeys, notifications, and guarded OS actions |
| `host-macro-recording-playback-sync` | [#44](https://github.com/atomicdeploy/PCController/issues/44) | 🟡 open | Stream recorded macros into an MCU-timed queue with synchronized progress and safety |
| `host-keyboard-bindings-output-state` | [#45](https://github.com/atomicdeploy/PCController/issues/45) | 🟡 open | Add configurable keyboard motion/output bindings with authoritative live-state reconciliation |
| `embedded-webui-native-experience` | [#101](https://github.com/atomicdeploy/PCController/issues/101) | 🟡 open | Deliver the embedded responsive WebUI as a complete native-feeling controller client |
| `privileged-service-tray-controller` | [#116](https://github.com/atomicdeploy/PCController/issues/116) | 🟡 open | Run a privileged background service with a separate interactive tray controller |

## [#8 — IPC, APIs, networking, discovery, and remote bridges](https://github.com/atomicdeploy/PCController/issues/8)

5 open / 0 closed / 5 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `ipc-websocket-api-suite` | [#46](https://github.com/atomicdeploy/PCController/issues/46) | 🟡 open | Provide unversioned living IPC, REST, JSON-RPC, WebSocket, and bridge APIs |
| `network-bridge-discovery` | [#47](https://github.com/atomicdeploy/PCController/issues/47) | 🟡 open | Bridge controller hosts over the network with mDNS/SSDP discovery |
| `http-webhooks-socketio-messages` | [#48](https://github.com/atomicdeploy/PCController/issues/48) | 🟡 open | Add bidirectional HTTP, webhooks, WebSocket client/server, Socket.IO, and actionable messages |
| `remote-control-security` | [#49](https://github.com/atomicdeploy/PCController/issues/49) | 🟡 open | Define security and policy gates for every remote and disruptive control path |
| `network-artifact-import-export-sync` | [#117](https://github.com/atomicdeploy/PCController/issues/117) | 🟡 open | Serve, fetch, import, export, and synchronize controller artifacts between hosts |

## [#9 — USB lifecycle, device selection, and single-owner IPC](https://github.com/atomicdeploy/PCController/issues/9)

3 open / 2 closed / 5 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `stable-device-selection` | [#50](https://github.com/atomicdeploy/PCController/issues/50) | ✅ closed | Select the controller by stable identity, friendly name, COM name, or VID/PID |
| `usb-reconnect-notifications` | [#51](https://github.com/atomicdeploy/PCController/issues/51) | 🟡 open | Validate event-driven USB reconnect and opt-in DTR reset semantics |
| `primary-serial-owner-ipc` | [#52](https://github.com/atomicdeploy/PCController/issues/52) | 🟡 open | Enforce one serial owner and route secondary processes through IPC |
| `controller-discovery-authority` | [#53](https://github.com/atomicdeploy/PCController/issues/53) | ✅ closed | Make controller-owned discovery authoritative and explain platform inventory drift |
| `serial-lifecycle-contract` | [#54](https://github.com/atomicdeploy/PCController/issues/54) | 🟡 open | Keep the serial protocol connected independently of telemetry subscriptions |

## [#10 — Urboot/Urclock programming, backup, patch, and restore](https://github.com/atomicdeploy/PCController/issues/10)

5 open / 1 closed / 6 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `uart-urclock-programming` | [#55](https://github.com/atomicdeploy/PCController/issues/55) | ✅ closed | Use UART Urboot/Urclock as the normal programming path and verify application return |
| `preflash-backup-dedup-restore` | [#56](https://github.com/atomicdeploy/PCController/issues/56) | 🟡 open | Require atomic flash/EEPROM backup, hash deduplication, and verified restore before writes |
| `canonical-host-programming-entrypoint` | [#57](https://github.com/atomicdeploy/PCController/issues/57) | 🟡 open | Route every build, upload, verify, backup, and recovery through the host tool |
| `hex-patch-settings-export` | [#58](https://github.com/atomicdeploy/PCController/issues/58) | 🟡 open | Finish guarded Intel HEX patching and separate live settings export from EEPROM parsing |
| `graceful-host-snapshot` | [#59](https://github.com/atomicdeploy/PCController/issues/59) | 🟡 open | Write an atomic diagnostic board snapshot on graceful host exit |
| `urboot-custom-progress-backend` | [#88](https://github.com/atomicdeploy/PCController/issues/88) | 🟡 open | Maintain a reproducible Urboot-Custom progress-hook patch and safe ISP install plan |

## [#11 — Build, dependencies, simulation, packaging, and developer tooling](https://github.com/atomicdeploy/PCController/issues/11)

4 open / 3 closed / 7 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `arduino-go-dependencies` | [#60](https://github.com/atomicdeploy/PCController/issues/60) | ✅ closed | Provision managed firmware and host toolchains plus globally discoverable UPX |
| `project-import-structure` | [#61](https://github.com/atomicdeploy/PCController/issues/61) | ✅ closed | Preserve reusable project layers, merge LocalLib variants, and consolidate source/tool directories |
| `native-virtual-board` | [#62](https://github.com/atomicdeploy/PCController/issues/62) | ✅ closed | Provide a desktop virtual board for fast native protocol and behavior tests |
| `tooling-entrypoint-consolidation` | [#63](https://github.com/atomicdeploy/PCController/issues/63) | 🟡 open | Unify build and programmer policy behind one command-plan implementation |
| `canonical-host-artifact-packaging` | [#64](https://github.com/atomicdeploy/PCController/issues/64) | 🟡 open | Produce one current source-identified controller artifact with verified packaging |
| `latest-toolchain-update-automation` | [#89](https://github.com/atomicdeploy/PCController/issues/89) | 🟡 open | Automate latest-compatible dependency updates with resolved-lock reproducibility |
| `authorized-reusable-component-porting` | [#118](https://github.com/atomicdeploy/PCController/issues/118) | 🟡 open | Directly port all applicable generalized components from authorized sibling applications |

## [#12 — Documentation, licensing, GitHub, and final code quality](https://github.com/atomicdeploy/PCController/issues/12)

3 open / 1 closed / 4 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `github-license-notices` | [#65](https://github.com/atomicdeploy/PCController/issues/65) | ✅ closed | Publish the complete repository with dual licensing and preserved third-party notices |
| `canonical-documentation-guide` | [#66](https://github.com/atomicdeploy/PCController/issues/66) | 🟡 open | Organize starter-friendly documentation with complete operational and architecture coverage |
| `final-code-documentation-gate` | [#67](https://github.com/atomicdeploy/PCController/issues/67) | 🟡 open | Run the final concise code-comment and missing-requirement audit after layouts freeze |
| `requirements-backlog-publication` | [#68](https://github.com/atomicdeploy/PCController/issues/68) | 🟡 open | Maintain a deduplicated public requirements map and true GitHub sub-issue hierarchy |

## [#13 — Live hardware acceptance and release readiness](https://github.com/atomicdeploy/PCController/issues/13)

7 open / 0 closed / 7 total

| ID | Issue | State | Requirement |
|---|---:|:---:|---|
| `hardware-frontpanel-audio` | [#69](https://github.com/atomicdeploy/PCController/issues/69) | 🟡 open | Validate final-image buttons, menus, reset stability, and audio cues on hardware |
| `hardware-door-bt-temperature` | [#70](https://github.com/atomicdeploy/PCController/issues/70) | 🟡 open | Validate enclosure, BT Audio, and temperature-role transitions on hardware |
| `hardware-pwm-displays-lighting` | [#71](https://github.com/atomicdeploy/PCController/issues/71) | 🟡 open | Visually validate TM1637, PWM, enclosure fade, power/RGB, and D6 strip |
| `hardware-relay-motion` | [#72](https://github.com/atomicdeploy/PCController/issues/72) | 🟡 open | Load-test relay identification, motion directions, interlocks, and door policy safely |
| `hardware-rf-handset` | [#73](https://github.com/atomicdeploy/PCController/issues/73) | 🟡 open | Complete real-handset RF capture, mapping, gesture, removal, and transmit validation |
| `hardware-lcd-usb-macro` | [#74](https://github.com/atomicdeploy/PCController/issues/74) | 🟡 open | Validate optional LCD, USB lifecycle, and a harmless cancellable macro end to end |
| `release-handoff` | [#75](https://github.com/atomicdeploy/PCController/issues/75) | 🟡 open | Complete final release evidence, launch, operating handoff, and acceptance closure |

## Synchronization

Run the idempotent helper from the repository root:

```sh
node Tools/Audit/sync-github-requirements.mjs --apply
```

Without `--apply`, the helper performs a read-only plan and still validates catalog labels and epic identities.
