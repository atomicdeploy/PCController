<div align="center"><a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Project acceptance

This is the current acceptance contract for PCController. It is intentionally
short, source-oriented, and evidence-driven. Completed behavior still has to
pass against the exact source and packaged artifact being delivered; an older
binary, checksum, or test run cannot satisfy a newer tree.

> [!IMPORTANT]
> PCController has no stable release. Software validation does not substitute
> for safe-loaded physical operation, and hardware-dependent items remain
> explicitly unclaimed until observed on the target device.

## Status key

| Mark | Meaning |
|---|---|
| ✅ | Implemented and covered by source-level automated validation |
| 🟣 | Required exact-artifact acceptance gate; record results for every candidate |
| 🚧 | Required source or design work remains; do not infer completion from a build |
| ⚠️ | Requires a physical controller, peripheral, load, or target OS observation |
| ⛔ | Must fail closed or remain unavailable |

## Product and repository

- ✅ One canonical product identity drives the native host, WebUI, resources,
  packaging, notifications, and documentation.
- ✅ Stable wire names, configuration paths, C ABI symbols, and artifact names
  remain technical compatibility identifiers rather than editable display text.
- ✅ Root generated outputs are limited to `.build/firmware` and
  `Tools/Controller/bin`; the standalone Virtual Board helper owns
  `Tools/VirtualBoard/.build`.
- ✅ Documentation describes only this product and the current architecture.
  Superseded audits and duplicate artifact aliases are not maintained product
  documentation.
- ✅ Repository content, link, generated-bundle, immutable-Action, private-path,
  external-project, and stale-origin gates scan tracked plus ordinary
  non-ignored untracked text while excluding ignored caches and generated/binary
  paths.
- ✅ The current pre-freeze semantic audit covered all 123 private user turns,
  found no additional distinct public requirement, and corrected missing detail
  in existing normalized issues without publishing raw/private wording. The
  separate final frozen-layout code/documentation audit remains open.
- 🟣 Record source identity, WebUI distribution identity, executable identity,
  and package-manifest identity for the final candidate.

## Build and package gate

- ✅ Root CMD and Bash launchers share the project-owned Node build policy.
- ✅ Public build/deployment entry points are PowerShell-free; Node and the
  canonical host own behavior while CMD/Bash remain thin launchers.
- ✅ A default build is hardware-free and cannot discover, open, reset, or
  program a serial/USB target.
- ✅ Firmware builds use the resolved dependency profile, real MiniCore compile,
  strict Intel HEX validation, and explicit flash/SRAM budgets.
- ✅ Latest-stable dependency resolution covers the firmware toolchain, Go,
  Node/npm, UPX, go-winres, and immutable GitHub Actions. Exact locks include
  sources and integrity hashes; deterministic PR plans carry release-note,
  license, security, validation, and size evidence.
- ✅ Host builds install the locked Web dependencies, type-check/test/build the
  embedded application, run Go tests and vet, stamp native identity, package
  notices, and exercise the C ABI smoke path. The current stable native Windows
  external-C caller loaded the fresh DLL, invoked the ports JSON ABI, received a
  successful result, and exited without opening COM.
- ✅ Windows C-ABI builds reject Git/MSYS/Cygwin `gcc` false positives and
  discover or provision a latest compatible, proxy-aware native MinGW-w64
  compiler; the current resolver selects WinLibs GCC 16.1.0.
- ✅ The generated dependency lock records the native compiler package,
  version, target, immutable manifest/archive provenance, and checksum; the
  canonical host manifest records the selected compiler and binary integrity.
- ✅ Focused dependency tests pass 22/22, build tests pass 40/40, and the
  hardware-free host validator reports Windows GCC 16.1.0-14.0.0-r3.
- ✅ The last complete host gate before the optional-LCD warning-only patch
  passed 117 Web tests, all 29 Go package tests, `go vet`, UPX packaging, and
  the native external-C ABI smoke path.
- ✅ Reusable host builds clean first, then consume the exact same-run firmware
  artifact and require both the validated application and complete 1 KiB safe
  EEPROM defaults to be independently enabled in `host-manifest.json`.
- ✅ Virtual Board builds use the native CMake test path on supported targets.
- ✅ Canonical firmware source identity `800A5B70` links 32,232 of the stock
  32,384-byte application range, emits 32,244 application HEX data bytes, and
  uses 1,432 bytes static SRAM with a modeled 1,761-byte peak and 287 bytes
  free.
- 🟣 Run the complete build from the exact final tree and preserve its manifest,
  package list, test counts, hashes, and any warnings. The final aggregate rerun
  after the optional-LCD warning-only patch is still pending and must supersede
  the successful pre-patch gate above.
- ⚠️ Observe one real scheduled/manual dependency workflow through artifact
  publication and PR/blocked-issue lifecycle before closing
  [#89](https://github.com/atomicdeploy/PCController/issues/89).
- ⛔ A partial compile, source-only hash, or stale executable must not be called
  the current packaged product.

## Embedded WebUI

### Product experience

- ✅ Responsive desktop and native-feeling mobile layouts share one component
  system and semantic violet/coral/amber status palette.
- ✅ A Web App Manifest and mobile metadata enable standalone installation in
  supporting browsers, with route shortcuts for the four primary work areas.
- ✅ The installability worker is deliberately network-only and retains no
  offline cache; host shutdown must become visible immediately.
- ✅ Light/dark themes, Persian/English localization, RTL/LTR layout, and the
  bundled Persian font are first-class states.
- ✅ The first-run loading sequence, dialogs, drawers, banners, toasts, focus,
  and route transitions preserve layout and honor reduced motion.
- ✅ Every card action has meaningful iconography, aligned controls, visible
  focus, accessible labels, and non-jumping feedback.
- ✅ Device-only controls are absent while no board is authenticated. Host-only
  settings, discovery, diagnostics, and reconnect remain reachable.
- ✅ Connection labels distinguish an authenticated controller from an open Web
  transport; an offline dashboard must never claim to be live.
- ✅ The application does not open a browser while the board is disconnected.
- ✅ Brand and documentation banners link back to the main page.

### Controls and data

- ✅ Dashboard charts render real time-series telemetry with selectable domains,
  windows, axes, legends, accessible summaries, and empty/offline states.
- ✅ Workbench controls cover relays, PWM, enclosure light, RGB/status output,
  addressable strip, buzzer, TM1637/LCD, RF, I²C, macros, menus, host actions,
  firmware artifacts, and guarded updates where the capability is available.
- ✅ Settings normalize and validate input during editing, expose dynamic
  state/error text, keep actions aligned with their inputs, and block invalid
  persistence or integration targets.
- ✅ Keyboard shortcuts use one `<kbd>` element per physical key, place
  separators outside the element, and preserve focus and locale direction.
- ✅ Audio cues are optional, volume-controlled, state-aware, and never required
  to understand or complete an operation.
- ✅ Supported visible-page clients may add restrained vibration to select,
  success, and warning cues; haptics remain optional and non-semantic.
- ✅ Browser terminal output supports safe structured `console.*` rendering,
  `%s` substitution, `%c` style spans, and common log levels without evaluating
  or injecting HTML.

### Browser communication

- ✅ Standard WebSocket is the preferred full-duplex RPC/event channel; REST is
  a bounded fallback and JSON-RPC IDs remain correlated.
- ✅ `BroadcastChannel` synchronizes allowed appearance, navigation, and terminal
  messages across tabs without trusting arbitrary payloads.
- ✅ The server provides GET/HEAD, MIME types, cache validators, favicon, closed,
  open, suffix, and multipart byte ranges, `If-Range`, and correct 416 handling.
- 🟣 Exercise the packaged WebUI at desktop and narrow mobile widths in both
  locales and themes; check keyboard-only flow, reduced motion, two-tab sync,
  command/event duplex, install/standalone launch, live network-only worker
  behavior, dialogs, graphs, settings validation, no horizontal overflow, and
  no console/network errors.

## Native host and desktop integration

- ✅ One primary process owns the authenticated serial session. Secondary
  TUI/CLI/Web/API clients route through local IPC.
- ✅ `controller web` is a true headless primary mode and does not create or read
  from the TUI.
- ✅ TUI, shell, monitor, batch, WebUI, IPC, libraries, scripts, and automations
  share the command dispatcher and event model.
- ✅ The default HOST-presented `MACR` hierarchy uses the file-watched macro
  library for ID-sorted selection, nested recording and playback status,
  guarded play/discard/keep-output actions, safe cancel, and identical
  physical/TUI/Web preview through the shared menu manager and MacroRunner.
- ✅ Native desktop adapters provide global hotkeys, notifications, foreground
  attention, keyboard actions, serial-owner diagnostics, and guarded owner
  actions where supported by the target OS.
- ✅ A primary-owning Windows web process provides an optional native tray menu
  with authoritative connected/offline state, offline-safe page gating,
  Connect/Reconnect, and Exit; its themed/icon-bearing menu is initialized once,
  cached, and reconciled only on state changes; `--no-tray` disables it.
- ✅ Per-user desktop/URI registration is opt-in and has an ownership-checked,
  idempotent uninstall path that preserves foreign keys and shortcuts.
- ✅ The Windows native shell receives and deduplicates session lock/unlock,
  suspend/resume, and network-state changes as typed runtime/WebSocket events.
- ✅ Settings windows/dialogs retain stable geometry and avoid flicker, broken
  non-client borders, and duplicate processes.
- ✅ Platform integration is isolated behind tested adapters; unsupported
  capabilities report unavailable rather than silently succeeding.
- ✅ Measurement history survives host restarts in a separate compact data file,
  prunes expired/duplicate/corrupt-tail records on startup, and remains bounded
  by both configured retention and a 32 MiB storage ceiling; important-event
  timeline persistence stays independent.
- ⚠️ Validate global hotkeys, notifications, tray/context actions, window
  activation, theme changes, owner diagnosis, and guarded owner close/terminate
  on each packaged target OS.

### Native TUI acceptance

- 🟣 Use **Reboot** everywhere in the user interface. It is neutral while idle
  and changes to a braille-spinner `Rebooting` state in transit; genuinely
  dangerous actions are marked explicitly instead of implying ordinary actions
  may be unsafe.
- 🟣 Default to one Open/Close toggle while allowing an optional split-button
  style. Use actual `ON`/`OFF` state text and a semantically colored Execute
  action.
- 🟣 Rename Outputs to **Control**. Center a rounded, correctly padded Charm
  table with compact/expanded styles, nested groups, complete border/action
  colors, mouse support, and exclusive arrow-key navigation. Configured motion,
  digit, Ctrl-alternate, and application hotkeys remain active there.
- 🟣 Hide the integrated terminal on navigation-heavy pages and toggle it with
  tilde. Fix menu click offsets, mouse-wheel navigation, and stale/off-by-one
  setting activation; every physical or remote navigation gesture uses the same
  nonduplicated board beep path.
- 🟣 Render Board as grouped two-line tables. Refresh on entry, split Last Reset
  and Reset Count into aligned columns, hide zero protocol errors, color nonzero
  errors red, call the module `BT Audio`, and retain future Bluetooth Serial
  support only as a hidden source TODO.
- 🟣 Present voltage, current, power, and temperatures with adaptive units,
  semantic colors, stable aligned mini-graphs, and expandable history. Do not
  show noisy sub-500 ms age changes or static blinking/internal hints.
- 🟣 Expose each named PWM output directly as 0–100%; selecting its row opens a
  mixer-style slider. Remove firmware PWM-mode UI, keep a host demo macro, and
  reconcile channel-zero writes and authoritative state from every event source.
- 🟣 Make peripherals host-renamable through F2, watched configuration, IPC,
  and bridge APIs. File edits hot-apply without repeated reload-rejected events;
  the current `remote_policy` schema is accepted without changing MCU EEPROM.
- 🟣 Use modal editors with slider and typed values/units for 0–100% fields,
  multi-column illumination levels, grouped decimal precision, default page,
  seven-segment brightness, and individually configurable status colors with
  live previews. Hide build-time sensor-role assignment from daily settings.
- 🟣 Show one dim LCD `not detected at 0x27 or 0x3F` row linked to LCD settings;
  do not repeat that message in the console. UI settings allow detection to be
  disabled or the address/configuration changed.
- 🟣 RF shows Learn only while idle and Cancel only while active. The default is
  indefinite multi-code learning; timer is the bounded mode and `single` plus
  `one-shot` remain accepted synonyms. Display remaining time and a definite
  ended/cancelled/full notification. Use **View In**, not Radix, and omit static
  implementation hints.
- 🟣 `config set ui.app_title` persists and hot-applies. Interactive prompts use
  the corresponding modal rather than an interfering inline terminal.
- 🟣 The four-digit preview receives changed frames immediately, preserves
  decimal-point bits, and never waits for the slower general telemetry poll.
- 🟣 The Build page explains dependency/profile resolution, source and artifact
  identity, compile/package progress, content-addressed backup, reviewed upload,
  verify, reconnect, restore, release staging, and delegated-operation progress.
- 🟣 Desktop notifications use the product icon, concise semantic color and
  terminal-safe symbols, actionable buttons, and the same authenticated command
  path as the TUI. Ordinary tests never bind a randomized test executable to a
  public interface or repeatedly trigger Windows Firewall prompts.

## Firmware and protocol

- ✅ Native UART uses COBS framing, CRC-8/ATM, sequence IDs, bounded payloads,
  explicit opcodes, unsolicited sequence-zero events, and authenticated HELLO.
- ✅ Status includes power, current, temperatures, inputs, output state, display,
  RF, reset, and persistent boot information required by the host.
- ✅ Board settings are CRC-checked EEPROM state and remain separate from host
  JSON configuration.
- ✅ Cooperative I²C provides bounded 16-byte probe/read/write/write-read
  transactions without a device allow-list, an expiring 0–10 s host lease that
  pauses local bus users, and a compact preloaded LCD offline page; the disabled
  standalone renderer's 1328-byte flash and 49-byte SRAM cost is documented.
- ✅ The TM1637 menu has stable Door page zero, persistent visibility/order,
  cached rendering, key gestures, editors, and save/discard feedback.
- 🟣 Host-driven TM1637 scrolling is enabled by default for selected pages. On
  Door, an authenticated host scrolls `door is open` or `door is closed`, then
  yields immediately to warnings, edits, navigation, programming, and other
  higher-priority overlays; offline firmware retains `OPEN`/`CLSD`. Watched
  configuration controls enabled pages, speed, gap, and text, and every rendered
  four-digit frame remains visible in the remote front-panel preview.
- ✅ Relay motion applies disable/break/direction/enable sequencing and local
  reed interlocks. Host motion starts apply a fresh configured door policy.
- 🟣 Starting Down or Up applies only the configured break/settle interval
  (1 ms for the current load profile) before direction and output reach their
  requested state. A persisted stop policy chooses full relay release or
  output-only release that retains direction; emergency/programming/reboot paths
  always force every relay off.
- 🟣 The enclosure reed drives Auto illumination through both smooth fade
  directions between saved values. RGB priority and eased transitions distinguish
  host offline, HOT, Running/open warning, Running/closed, RF, BT Audio
  connected/blinking/off, and Idle without getting stuck in dim red or losing
  door/BT reactions.
- ✅ RF, PWM, RGB, strip, buzzer, displays, menus, macros, I²C, reset, and
  programming lifecycle operations have native command/event contracts.
- ✅ PWM is direct per channel through set/get/all-off; opcode `0x13` remains
  reserved and scheduled behavior belongs in host macros or automations.
- ✅ Output persistence independently gates motion, user-relay, and stored
  user-PWM restoration; direction retention on stop and the last relay mask are
  explicit board settings, and programming mode overrides every restore path.
- 🟣 Re-run protocol, settings, menu-layout, timing, memory, and host/Virtual
  Board compatibility tests against the final firmware image.
- ⚠️ Retain `SET_STREAM` and `MENU_ACTION` as fully implemented inbound paths.
  A later consolidation/removal review is tracked but requires explicit user
  approval before either command or any current caller may be changed.
- 🟣 The host can show `WAIT`, run the unique repeating attention ringtone, and
  stop both immediately after acknowledgement; final successful handoff shows
  `ok`. These cues honor Silent/Do Not Disturb and are never substituted for a
  recorded human observation.

### Known source and design gaps

- 🚧 [#87](https://github.com/atomicdeploy/PCController/issues/87): host
  automations are complete, but the MCU still has no generic EEPROM-backed rule
  table, CRUD opcodes, or deterministic offline event executor for door, BT
  Audio, host-loss, RF transmit, macro requests, and other bounded actions.
- 🚧 [#22](https://github.com/atomicdeploy/PCController/issues/22): MCU EEPROM
  stores Silent and door/relay cue enable flags, but each door-open, door-close,
  relay-on, and relay-off cue still uses fixed flash-resident notes and timing.
- 🚧 [#30](https://github.com/atomicdeploy/PCController/issues/30): the current
  physical HOST-menu path is push/capture. The AVR does not yet retain a menu
  directory, request nodes by ID, track generations, or render `----` plus
  bounded retry/failure states. Implement that profile or record an explicit,
  measured decision to retain the smaller capture fallback.
- 🚧 [#21](https://github.com/atomicdeploy/PCController/issues/21) and
  [#40](https://github.com/atomicdeploy/PCController/issues/40): MCU EEPROM owns
  one Ready color and global RGB brightness. Individually editable per-state
  colors/effects are host-configured, file-watched, and sent as live overrides;
  they are not independently persistent MCU settings. Keep that ownership
  explicit in the UI and final menu/flash tradeoff decision.

## Programming and update safety

- ✅ Artifact selection, URL download, staging, and inventory are inert.
- ✅ Every board write or host replacement has a separate review/authorization
  boundary and explicit target.
- ✅ The primary process coordinates authenticated snapshot, quiet outputs,
  settings/audio preservation, flash+EEPROM backup, write, verify/readback,
  reconnect, restoration, and durable interruption recovery.
- 🟣 Before latching programming mode, capture the live relay/PWM/settings and
  host-owned visual state; cancel macros; release every relay; smoothly ramp
  enclosure and user MOSFET outputs to zero; apply the programming RGB cue;
  write `Prog`; and play the PC-streamed power-down melody to completion.
- 🟣 Keep the EEPROM programming latch, `Prog`, Silent, zero outputs, and safe
  relay state across every intermediate reboot. Clear it only after verified
  flash/readback and authenticated application HELLO, then restore the captured
  settings and live outputs through canonical safe motion/output controllers.
  When the restored MCU setting is audible, stream the rising
  `programming-ready` cue only after the host/front panel are ready; intermediate
  resets remain quiet.
- ⚠️ Macro playback position is intentionally canceled rather than reconstructed;
  interruption recovery must report that one transient as non-restorable while
  retaining everything required for an explicit safe retry or completion.
- ✅ Bootloader programming is the normal route; ISP is an explicitly selected
  recovery method and does not inherit a serial selector as its programmer ID.
- ✅ Content-addressed storage deduplicates identical firmware and preserves
  raw logs, manifests, hashes, completeness, and source identity.
- ✅ The explicit development-only `--reinitialize-eeprom` path records an
  incompatible settings-query error, preserves the untouched raw EEPROM in the
  mandatory backup, routes through a primary bridge, never restores old
  semantics/live outputs, and clears its marker only after current-schema
  settings are audible, output-safe, and verified. It cannot be combined with
  the incomplete-backup override and adds no firmware compatibility baggage.
- ✅ `program recover HEX [PORT]` is a primary-owned completion path for a
  matching failed transaction. It reasserts safe state, performs fresh
  read-only Urboot semantic verification without rewriting flash, reconnects
  only to the saved physical device identity, and clears the marker only after
  authenticated restore/reinitialization. Secondary clients delegate through
  IPC; direct programmer/serial bypass is not equivalent.
- ✅ Live recovery completed on the Instance-ID-pinned COM18 controller. The
  board authenticated as `800A5B70`; deployed application artifact SHA-256 is
  `bb928b7b680d6e393d842bd28412b6760340a2ff61ae40b21a926036358bb092`.
  The primary-owned Urclock transaction backed up, wrote, and verified 32,244
  programmed bytes, reauthenticated the application, and cleared the recovery
  marker only after reconnect.
- ✅ Development EEPROM readback confirms Silent off, illumination Off, output
  persistence off, relay restore mask zero, and 1 ms motion break. The safe
  live sample reported 12.282 V, PWM available, zero active relays, zero
  framing/CRC protocol errors, and reset count 22.
- ⚠️ No optional LCD was detected during that pass. This is a warning-only
  capability absence; connected-LCD presentation remains a separate physical
  acceptance item and does not invalidate the recovered firmware or safe state.
- ✅ GitHub successful-workflow, GitHub release/checksum, and generic HTTP
  manifest discovery preserve digest, source, platform, build hash/time, and
  packed timestamp without opening hardware.
- ✅ Proxy-aware streaming staging reports typed progress, validates declared
  bytes/digests, safely selects ZIP members, and remains a separate operation
  from programming.
- ⛔ Backup failure blocks a write unless an advanced, explicit, logged override
  is separately granted.
- ⛔ CI, a plain build, plain source watch, artifact selection, or connected ISP
  hardware must never authorize programming.
- ⚠️ Validate the complete backup → prepare → program → verify → reconnect →
  restore transaction on a labeled board, including an intentionally interrupted
  recovery exercise, before a stable release.
- ⚠️ Commission real GitHub/private-peer credentials and proxy traversal on an
  isolated network; automated coverage uses local HTTP/proxy fixtures only.

## Network and integration safety

- ✅ IPC listeners default to loopback.
- ✅ Remote mode requires a long token, explicit non-wildcard origins, and a
  capability policy; read/event access does not imply board writes or OS actions.
- ✅ WebSocket events are typed, bounded, demand-counted, and share the primary
  runtime state.
- ✅ Independent raw RFC 6455 clients and servers verify authenticated standard
  WebSocket and Engine.IO-v4/Socket.IO interoperability in both directions,
  including masking, correlation, subscriptions, ping/pong, and typed messages.
- ✅ A raw virtual-board test verifies that subscription demand controls only
  STATUS polling: unsubscribe leaves UART ownership and asynchronous events
  live, Close pauses reconnect, and a successful Open resumes it.
- ✅ Outbound GET, POST, PUT, PATCH, and DELETE webhooks are delivered through
  real loopback HTTP requests with the documented query/body behavior.
- ✅ Bridge requests are correlated, loop-safe, and revalidated by the target's
  own policy and device safety guards.
- ✅ Local integration targets are normalized and constrained to the intended
  loopback/private scope; credentials, unsafe origins, and malformed roots are
  rejected.
- ⛔ Remote token possession alone must not grant reset, programming, shutdown,
  virtual keys, power actions, host automation, or bridge calls.
- ⚠️ Commission remote access, discovery, webhook receivers, proxy behavior,
  and cross-host bridges only in an isolated test network before deployment.

## Physical acceptance still required

The following work cannot be inferred from a source build or simulator:

### Human-assisted `WAIT` / ringtone queue

These items remain deliberately open. Before requesting help, the host must
back up the MCU settings, force safe outputs, prepare the exact capture/test,
confirm COM ownership, then show `WAIT` and play the unique repeating attention
ringtone. The cue stops immediately when the user responds; it is never used
while Do Not Disturb/Silent is active.

| State | Hands-on action still required | Tracking |
|:---:|---|---:|
| ⏳ | Press and identify every physical key; exercise click, double-click, delayed single-click, hold/repeat, editor roll-over, nested-menu enter/back, motion exit chords, and verify reset count does not change. | [#69](https://github.com/atomicdeploy/PCController/issues/69) |
| ⏳ | Open/close the enclosure and operate BT Audio while observing reed polarity, display/enclosure-light brightness fades, RGB easing/priority, audio cues, immediate events, and the tLED/tBT thermal roles. | [#70](https://github.com/atomicdeploy/PCController/issues/70), [#71](https://github.com/atomicdeploy/PCController/issues/71) |
| ⏳ | Identify every PWM/MOSFET and relay channel on safe loads, then verify both motion sides, 1 ms break-before-direction, hold-to-run/release-to-stop, interlocks, policy modes, and emergency stop. | [#71](https://github.com/atomicdeploy/PCController/issues/71), [#72](https://github.com/atomicdeploy/PCController/issues/72) |
| ⏳ | Resume guided RF learning for handset buttons B, C, and D after reviewing A; confirm explicit mappings, click/hold/repeat/release latency, CRUD/reorder, and an INT1 transmit observed by another receiver. | [#73](https://github.com/atomicdeploy/PCController/issues/73) |
| ⏳ | Connect/confirm the optional LCD, perform a real USB disconnect/reconnect cycle, and run/cancel the prepared harmless synchronized macro while checking board/host timing and state. | [#74](https://github.com/atomicdeploy/PCController/issues/74) |
| ⏳ | Connect USBasp only after an exact-size Urboot-Custom image, read-only ISP backup plan, fuse/lock review, and rollback image are ready; the first ISP operation is backup-only. | [#88](https://github.com/atomicdeploy/PCController/issues/88) |

No item in this queue may be marked ✅ from a build, simulator, or source review
alone; its linked issue remains open until the physical observation is recorded.

- 🟣 RF learning exposes exactly two mutually exclusive choices: the default
  indefinite multi-code mode and a bounded `timer` mode. Every surface shows
  timer duration/remaining time and a definite ended/cancelled/full event;
  `single` and `one-shot` remain accepted/documented synonyms for `timer`.

- ⚠️ Authenticate the intended CH340/serial device after reconnect, USB renumber,
  sleep/resume, application reset, and bootloader programming.
- ⚠️ Compare INA219 supply/current/power and both DS18B20 channels with trusted
  instruments across the intended range.
- ⚠️ Inspect TM1637 and optional LCD readability, cached/flicker-resistant
  updates, brightness transitions, RTL-independent wire behavior, and reset text.
- ⚠️ Exercise keys, hold/repeat/double-click, door reed polarity, Bluetooth LED
  sense, RF learn/transmit/range, and learned-action safety.
- ⚠️ Verify all sixteen PWM channels, enclosure automation, RGB/status output,
  addressable strip, buzzer levels, and user relays on safe test loads.
- ⚠️ Verify both motion sides with unloaded mechanisms first, then controlled
  loads, including break-before-make, opposite-direction requests, release stop,
  door interlock, disconnect, and emergency off.
- ⚠️ Confirm board settings persist exactly through power loss, normal upload,
  full recovery, backup/restore, and invalid-CRC default recovery.
- ⚠️ Perform acoustic, visual, thermal, electrical, and enclosure acceptance with
  the actual peripherals and supply.

## Stable-release exit gate

A stable release may be claimed only when:

1. the exact tag passes the complete automated build and repository gate;
2. package hashes, manifests, provenance, notices, and native identity agree;
3. packaged WebUI and native desktop acceptance pass on supported targets;
4. a labeled production-equivalent board passes every applicable physical and
   recoverable-programming item above;
5. remaining limitations are explicit in release notes;
6. signing status and security posture are stated accurately.

Until then, release artifacts remain engineering prereleases and the unchecked
hardware items above remain deliberately unclaimed.

<p align="center"><a href="../README.md">← Return to the PCController main page</a></p>
