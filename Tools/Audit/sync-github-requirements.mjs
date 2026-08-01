#!/usr/bin/env node

// Publishes the normalized, public-safe requirements catalog as GitHub issues.
// The local user-turn audit is intentionally never read or uploaded by this tool.

import { spawnSync } from 'node:child_process';
import { promises as fs } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const REPO = 'atomicdeploy/PCController';
const APPLY = process.argv.includes('--apply');
const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');
const OUTPUT = resolve(ROOT, 'docs', 'Requirements-Backlog.md');

const EXPECTED_LABEL_COLORS = {
  '🔥 priority: critical': 'B60205',
  '⚡ priority: high': 'D93F0B',
  '🖥️ host': '1D76DB',
  '🧩 firmware': '0052CC',
  '🎛️ front-panel': '7057FF',
  '📡 rf-433': '006B75',
  '🔌 protocol-api': '0E8A16',
  '💾 storage': 'FBCA04',
  '🚀 programming': 'C2E0C6',
  '🛡️ safety': 'B60205',
  '🧪 testing': 'BFDADC',
  '🏗️ tooling-build': '6F42C1',
  '📚 documentation': '0075CA',
  '🔍 needs-hardware': 'D4C5F9',
  '⏳ finalization': 'EDEDED',
  '🌐 networking': '0B7285',
  '🚧 in progress': 'F9D0C4',
  '✨ ux': 'C5DEF5',
  '🐛 regression': 'D73A4A',
  '📦 dependencies': '0366D6',
  '🔒 security': 'D4A72C',
  '✅ verified': '2DA44E',
  '💡 enhancement': 'A2EEEF',
};

const EPICS = {
  1: '[Epic] Firmware architecture, flash budget, EEPROM, and reset safety',
  2: '[Epic] Board peripherals, sensors, displays, lighting, and audio',
  3: '[Epic] Relay and motion-control safety',
  4: '[Epic] Front panel, menus, keys, and synchronized UX',
  5: '[Epic] 433 MHz RF learning, mappings, and actions',
  6: '[Epic] Native UART protocol, telemetry, and event model',
  7: '[Epic] PC host TUI, configuration, automation, and OS integration',
  8: '[Epic] IPC, APIs, networking, discovery, and remote bridges',
  9: '[Epic] USB lifecycle, device selection, and single-owner IPC',
  10: '[Epic] Urboot/Urclock programming, backup, patch, and restore',
  11: '[Epic] Build, dependencies, simulation, packaging, and developer tooling',
  12: '[Epic] Documentation, licensing, GitHub, and final code quality',
  13: '[Epic] Live hardware acceptance and release readiness',
};

function requirement(id, parent, title, state, labels, section, criteria, evidence) {
  return { id, parent, title, state, labels, section, criteria, evidence };
}

const R = [
  requirement('fw-core-architecture', 1, 'Reduce firmware entry point to modular, documented domain composition', 'closed',
    ['🧩 firmware', '✅ verified'], 'Project import, LocalLib merge, and structure', [
      'Keep the main sketch limited to composition, setup, and a short service-oriented loop.',
      'Move domain state and behavior behind focused classes/modules without changing safety or wire behavior.',
      'Document ownership, timing, units, hardware assumptions, and non-obvious invariants without syntax-noise comments.',
      'Rebuild and regression-test within the measured AVR flash/SRAM budget after behavior and layouts freeze.',
    ], 'Verified by merged PR #80: the sketch is 49 high-level lines composed from eight documented domain fragments; fixed-identity HEX and EEP are byte-identical, application flash is 32,226 bytes, static SRAM is 1,444 bytes, modeled stack margin is 284 bytes, and canonical compile, Go test/vet, and CTest 2/2 passed.'),
  requirement('mcu-eeprom-settings', 1, 'Persist independent MCU settings with CRC, deferred writes, and page defaults', 'closed',
    ['🧩 firmware', '💾 storage', '✅ verified'], 'Firmware lifecycle, persistence, and reset', [
      'During development, use the current unversioned packed MCU settings plus CRC-8 with EEPROM.update and deferred writes; defer whole-record versioning/migration until the layout is finalized.',
      'Keep MCU EEPROM independent from host-side configuration.',
      'Persist sound, display, lighting, precision, PWM, telemetry, default-page, and save-last-page settings.',
      'Use sound-on as the factory default while honoring retained EEPROM, and verify saved precision across reset.',
    ], 'Current source implements the record and separation; live settings decoding and voltage/current precision persistence across reset were verified.'),
  requirement('reset-safety-journal', 1, 'Complete graceful reset safety and reliable reset-cause journal telemetry', 'open',
    ['🧩 firmware', '🛡️ safety', '💾 storage', '🧪 testing', '🔥 priority: critical'], 'Firmware lifecycle, persistence, and reset', [
      'Turn off side/general relays, PWM test, and all PWM channels before watchdog reset, with an explicit RGB cue.',
      'Emit reset cause and persistent count at boot and in status telemetry.',
      'Use a CRC-checked, marker-last wear-levelled journal with bounded migration and rollover behavior.',
      'Verify correct watchdog cause through the installed bootloader and perform a load-safe live reset test.',
    ], 'Safe reset sequencing and the journal exist and the journal advanced during a live reset loop, but the installed path reported cause 0 and rollover/migration plus loaded reset remain unverified.'),
  requirement('firmware-identity-layout-time', 1, 'Finalize compact build identity, time model, flash budget, and migration architecture', 'open',
    ['🧩 firmware', '💾 storage', '⏳ finalization', '🚧 in progress'], 'Development EEPROM, repository, licensing, and documentation', [
      'Encode a backward-decodable 32-bit half-second build timestamp and render YYMMDDHHMMSS with source/application hashes.',
      'Use one wrap-safe loop-time snapshot for ordinary services while leaving interrupts independent.',
      'Version/hash settings, RF, and automation layouts and perform old-image migration off-device with backup/readback verification.',
      'Allow documented development EEPROM reinitialization until layouts freeze and measure every flash/SRAM tradeoff.',
      'Inventory every firmware-resident and PC-hosted menu, then offer each ownership migration as an explicit user choice with measured flash/SRAM delta, lost offline behavior, protocol cost, and feature gain before changing it.',
      'Tie all size claims to a named build identity and distinguish measured linker-map deltas from estimates.',
    ], 'Schema-2 timestamp vectors and host decoding exist and a current 2-byte-free checkpoint is documented, but final firmware HELLO verification, global time consolidation, migration workflow, and user-approved menu ownership decisions remain open.'),

  requirement('board-pin-map-inputs', 2, 'Validate the complete board pin, shift-register, BT Audio, and reed mapping', 'open',
    ['🧩 firmware', '🧪 testing', '🔍 needs-hardware'], 'Board pins, shift registers, and input model', [
      'Use active-low shift bits 0-3 for Keys 1-4, bits 4-5 as reserved senses, bit 6 for BT Audio LED state, and bit 7 for the enclosure reed.',
      'Keep RF RX/TX on D2/D3, WS281x on D6, buzzer on D9, DS18B20 on D10, TM1637 data/clock on D11/D13, and shift outputs safely inactive at boot.',
      'Expose raw senses and verify every polarity and transition on the physical board.',
    ], 'The source pin map is complete and current buttons 1/2 have live evidence; reserved senses, BT/reed polarity, and all-key transitions still need a comprehensive hardware pass.'),
  requirement('measurement-sensors-i2c', 2, 'Deliver stable INA219 and dual-DS18B20 measurements with conflict-free I2C discovery', 'open',
    ['🧩 firmware', '🧪 testing', '🔍 needs-hardware'], 'INA219, DS18B20, and I2C', [
      'Keep INA219 at 0x40 and PWM at 0x41, calculate supply as bus plus shunt, and expose a live I2C scan.',
      'Use bounded nonblocking DS18B20 discovery with the documented pull-up, ROM identities, tLED/tBT roles, and no missing-pull-up lockup.',
      'Use responsive open-door sampling with hardware averaging and temperature smoothing that does not delay HOT warnings.',
      'Verify smooth visual readings and prove tLED/tBT roles with a controlled illumination test.',
    ], 'Live 12 V measurements, low-noise sampling, both ROMs, and the 0x40/0x41 scan are verified; role heating and visual smoothness remain hardware gaps.'),
  requirement('pwm-lighting-rgb-strip', 2, 'Complete PWM ownership, enclosure fade, status RGB, power light, and addressable strip behavior', 'open',
    ['🧩 firmware', '🎛️ front-panel', '🐛 regression', '🔍 needs-hardware'], 'PWM, enclosure light, power, RGB, and addressable LEDs', [
      'Map channels 0-10 to user outputs, 11 to enclosure light, 12 to power, and 13-15 to status RGB with safe polarity, caching, and 1 kHz output.',
      'Provide manual identification, auto demo, persistent user values, and Off/Auto/On enclosure brightness settings.',
      'Fade both door-transition directions without jitter or a jump and use coherent configurable RGB transition colors.',
      'Use one deterministic priority arbiter: HOT red/orange breathing with buzzer; Running plus open door hard red warning; PC offline red; RF violet breathing; Running plus closed door orange/yellow; Idle plus BT Audio connected blue; BT Audio powered off green/red blink; otherwise Idle green.',
      'Keep Running/Idle PC-owned rather than inferred from the reed; smoothly restore the underlying state after transient RF/door/BT cues.',
      'Ease/damp transitions into and out of informational states without unrelated intermediate colors or abrupt RF palette jumps, while retaining immediate hard flashing for warning and critical states.',
      'Support the D6 11-pixel strip and document emergency all-channel clear versus ordinary mode-off semantics.',
      'Validate real outputs, fades, power/RGB colors, and pixel order under safe load.',
    ], 'Core controllers are implemented and the latest live status showed enclosure PWM off, but the prioritized PC-owned Running/Idle color arbiter, eased informational transitions, regression criteria, and full visual/load validation are open.'),
  requirement('displays-audio', 2, 'Finish smooth TM1637, optional LCD, buzzer, melody, and configurable cue behavior', 'open',
    ['🧩 firmware', '🎛️ front-panel', '✨ ux', '🔍 needs-hardware'], 'TM1637 and optional I2C LCD', [
      'Cache TM1637 frames/brightness, preserve responsive UART service, and provide configurable precision and blink states.',
      'Drive an optional 16x2 LCD at 0x27 or 0x3F concurrently and accept bounded host text.',
      'Play the boot melody and exactly one clean key beep while Silent remains persistent and authoritative.',
      'Support board/host melodies plus configurable door and relay cues with save/discard/reset feedback.',
      'Validate the final image by sight and listening, including a connected LCD when available.',
    ], 'Timer1 fixed the buzzer and earlier melody/menu operation was user-confirmed; final-image key/cue listening, LCD hardware, and smooth-display visual checks remain open.'),
  requirement('cooperative-host-i2c-profile', 2, 'Measure and implement the cooperative host-driven I2C/LCD profile', 'open',
    ['🧩 firmware', '🖥️ host', '🔌 protocol-api', '🚧 in progress'], 'Development EEPROM, repository, licensing, and documentation', [
      'Expose bounded probe/read/write/write-read operations while allowing intentional access to known and future I2C devices.',
      'Use an expiring host lease so local drivers pause and refresh safely after release.',
      'Keep a tiny offline LCD fallback and rich PC-owned text/layout when connected.',
      'Measure exact flash/SRAM savings, cache/reset risks, and offline losses before selecting a production profile.',
    ], 'I2C scan and named device drivers exist, but the cooperative raw-access lease, measured profile, and offline fallback decision are not complete.'),

  requirement('relay-motion-interlocks', 3, 'Verify relay mapping, break-before-make, side isolation, and safe stop behavior', 'open',
    ['🧩 firmware', '🛡️ safety', '🔥 priority: critical', '🔍 needs-hardware'], 'Relays and motion safety', [
      'Use R1/R3 as Side A/B direction and R2/R4 as their output/enable, with disable-break-direction-settle-enable sequencing.',
      'Stop a side on release, stop all on reset, and never energize opposing motion.',
      'Expose individual relay, side motion, all-off, and identification operations.',
      'Validate both directions, timing, isolation, and stop behavior under a safely prepared real load.',
    ], 'The corrected source mapping and sequencing exist, but load-safe physical direction/interlock validation is still required.'),
  requirement('motion-door-policy', 3, 'Apply a persisted four-mode motion-door safety policy to every command source', 'open',
    ['🧩 firmware', '🖥️ host', '🛡️ safety', '🔥 priority: critical'], 'Motion, enclosure, RGB, audio, and board automation', [
      'Persist closed, open, always, and never policies with factory default always.',
      'Apply the same decision to local keys, learned RF, host commands, macros, and automations.',
      'Keep stop/off available even when starts are denied, and enforce the final gate atomically in firmware.',
      'Expose the policy through board menus, host UI, CLI, APIs, and backup decoding.',
    ], 'Local/RF enforcement and a host fail-closed preflight are partial; the host query/start is not atomic and the full policy/UI matrix is unfinished.'),
  requirement('relay-user-controls-break-setting', 3, 'Expose R5-R8 behaviors and configurable break timing across all control surfaces', 'open',
    ['🧩 firmware', '🖥️ host', '🛡️ safety', '🚧 in progress'], 'Relays and motion safety', [
      'Support R5-R8 toggle and momentary push behavior locally and remotely.',
      'Persist a board-owned break-before-direction interval with a safe 1 ms minimum/default for the current loads.',
      'Keep direction-settle and cross-side interlocks independent of the configurable break.',
      'Expose and decode the value in menus, TUI, CLI, APIs, backups, and offline EEPROM tools.',
    ], 'R5-R8 behavior and host commands exist, but break timing is still compiled-in and the final cross-surface setting plus hardware test are open.'),

  requirement('frontpanel-key-gestures', 4, 'Complete physical and remote key gestures with responsive hold acceleration', 'open',
    ['🧩 firmware', '🎛️ front-panel', '🧪 testing', '🔍 needs-hardware'], 'Buttons, gestures, menu, and audio', [
      'Emit Down, Up, Click, DoubleClick, HoldStart, HoldRepeat, and HoldRelease from the same source-tagged state machine.',
      'Act on initial press, begin hold at 600 ms, repeat at 150 ms, and accelerate to 60 ms after 1.8 seconds.',
      'Roll editable values over at bounds and show key identification 1-4.',
      'Verify all four physical keys, double-click, accelerated hold, editors, reset stability, and clean beeps on the final image.',
    ], 'Buttons 1/2 Down-Up-Click passed on a rollback image; final-image buttons 1-4, double-click, hold, editors, and audio remain open.'),
  requirement('board-menu-hierarchy-settings', 4, 'Finish nested board menus, editors, save/discard, and persistent default-page behavior', 'open',
    ['🧩 firmware', '🎛️ front-panel', '✨ ux', '🚧 in progress'], 'Buttons, gestures, menu, and audio', [
      'Organize related root pages under a starter-friendly category hierarchy while preserving direct host navigation.',
      'Provide blinking editors and explicit SAVE/diSC flows with distinct audio cues.',
      'Expose board sound, display/status brightness, Ready color, precision, illumination, PWM, relay, motion, RF, and user-output settings.',
      'Use a configurable default page after boot/no-change door close and optionally save the last page across power loss.',
    ], 'A six-field nested settings sequence, blinking, page commands, and default/save-last persistence exist; the broader category hierarchy and full physical-key validation remain incomplete.'),
  requirement('first-run-board-synchronization', 4, 'Synchronize first-run setup, board initialization, and welcome melody', 'open',
    ['🖥️ host', '🎛️ front-panel', '✨ ux', '⚡ priority: high'], 'TUI structure and interaction', [
      'Show a polished first-run setup/preview animation and persist completion in PC configuration.',
      'Authenticate HELLO/ready before presenting the board as initialized.',
      'Start or observe the welcome melody while the setup page is visible and keep progress synchronized with the physical board.',
      'Leave the page only after initialization and melody completion, or show a bounded, actionable offline/error result.',
      'Opening the app must not reset the board unless the user explicitly enabled DTR reset.',
    ], 'The host authenticates the board and DTR reset defaults off, but the first-run page and melody/initialization synchronization are not implemented or screenshot-verified.'),
  requirement('frontpanel-snapshot-remote-menus', 4, 'Mirror the live front panel and support remote keys plus PC-defined board menus', 'open',
    ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '🔌 protocol-api'], 'Configuration, menus, melodies, and programming surfaces', [
      'Snapshot exact TM1637 bytes/mask/brightness/blink, LCD cells/address/backlight, active keys, current page, submenu, and mode.',
      'Render the snapshot in TUI/API clients and update it after physical-board changes.',
      'Inject four remote keys with down/up/hold/gesture semantics through the same board state machine and source-tagged events.',
      'Serve host-defined nested typed menus from PC JSON/YAML/TOML with confirmation, callbacks, capture timeout, and host-loss fallback.',
      'Poll snapshots only while a subscriber needs them without closing the serial link.',
    ], 'Basic menu/page and display commands exist, but exact snapshots, remote-key injection, subscriber-aware polling, and PC-owned menu sessions remain open.'),
  requirement('lcd-console-status-events', 4, 'Mirror console context to LCD and make Status the event-aware default page', 'open',
    ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '✨ ux'], 'Motion, enclosure, RGB, audio, and board automation', [
      'Optionally mirror the active console prompt, completion, and result context to the 2x16 LCD without routine telemetry flicker.',
      'Prioritize HOT, error, door, motion, relay, and RF messages, then restore the prompt.',
      'Show PC OFFLINE / CONNECT USB when the host heartbeat expires, with slow scrolling only if needed.',
      'Make Status the default board page and briefly show incoming action name plus flashing On/Off.',
      'Add the legacy seven-segment animation only after measured flash savings make it safe.',
    ], 'Host LCD text and default-page machinery exist, but arbitration, offline fallback, prompt mirroring, event/status presentation, and optional animation are unfinished.'),

  requirement('rf-transport-learning-core', 5, 'Validate 433 MHz receive/transmit and end-to-end learned-record CRUD', 'open',
    ['🧩 firmware', '🖥️ host', '📡 rf-433', '🔍 needs-hardware'], '433 MHz RF receive, transmit, and learning', [
      'Receive on D2/INT0 and transmit on D3/INT1 while safely suspending/restoring RX during TX.',
      'Use learn terminology and CRC-checked records containing RF identity, timing, action, value, and behavior.',
      'Provide learn, list, map/remap, remove, clear, and transmit through board and host control surfaces.',
      'Verify a harmless handset end to end and confirm INT1 transmission with a receiver.',
    ], 'The full source/host path exists and Remote A produced live raw and learned events; complete CRUD and physical TX confirmation remain open.'),
  requirement('rf-learning-sessions-capacity', 5, 'Add explicit RF learn sessions, unmapped defaults, and capacity for at least 20 records', 'open',
    ['🧩 firmware', '🖥️ host', '📡 rf-433', '💾 storage', '💡 enhancement'], 'RF learning, mapping, latency, and capacity', [
      'Leave every newly learned record Unmapped until the user chooses an action.',
      'Support finite, indefinite, and multi-learn sessions with clear start/end/cancel/full notifications.',
      'Store at least 20 records if EEPROM endurance/layout permits, retaining CRC and individual management.',
      'Offer the action catalog locally when feasible and resolve compact IDs to host labels when connected.',
    ], 'Current firmware stores eight records and implicitly assigns a default action; the requested session model, capacity, and unmapped flow are not complete.'),
  requirement('rf-latency-gestures-guided', 5, 'Reduce RF action latency and verify click/hold/repeat behavior with guided capture', 'open',
    ['🧩 firmware', '🖥️ host', '📡 rf-433', '🐛 regression', '🔍 needs-hardware'], 'RF learning, mapping, latency, and capacity', [
      'Make a short single burst invoke its mapping reliably and reduce receive-to-action delay.',
      'Derive source-tagged down/click/double-click/hold/repeat/up semantics with safe release-gap inference.',
      'Guide A/B/C/D capture one labeled button at a time, show exact identity, confirm intent, and persist mapping.',
      'Validate repeat gaps and hold behavior against the real handset and remove/review stale records.',
    ], 'Host gesture synthesis and Remote A hold/repeat/up evidence exist, but the reported missed-short-burst regression and guided B/C/D validation remain open.'),
  requirement('rf-metadata-format-reorder', 5, 'Provide consistent RF formatting, metadata, action search, and transactional reorder', 'open',
    ['🖥️ host', '📡 rf-433', '✨ ux', '💾 storage', '💡 enhancement'], 'RF learning, mapping, latency, and capacity', [
      'Apply one configurable hexadecimal/decimal format across TUI, console, CLI, APIs, exports, logs, and dialogs without changing the raw u32.',
      'Store names, notes, categories, and colors by stable code/bits/protocol identity rather than record ID.',
      'Offer a searchable action picker and user-named categories with color choices in this order: red, blue, violet/purple, green, white.',
      'Reorder/renumber transactionally, read back, keep the list sorted by ID, and update metadata without confusing ID and RF identity.',
    ], 'Basic numeric mapping commands exist; consistent formatting, metadata UX, searchable actions, and transactional reorder are not implemented.'),

  requirement('protocol-native-uart', 6, 'Replace Firmata with the native COBS/opcode UART protocol', 'closed',
    ['🧩 firmware', '🖥️ host', '🔌 protocol-api', '✅ verified'], 'Native UART protocol and asynchronous events', [
      'Keep UART as the always-on 115200 8N1 application link.',
      'Frame magic/version/opcode/sequence/length/payload/CRC with bounded payloads and COBS delimiter handling.',
      'Support correlated request/response plus unsolicited HELLO, streams, and events.',
      'Remove Firmata from firmware and host dependencies.',
    ], 'Firmware, Go host, and protocol tests implement the native transport; Firmata is removed. This is the pre-existing closed issue 14.'),
  requirement('protocol-command-event-coverage', 6, 'Complete native command coverage and immediate typed event delivery', 'open',
    ['🧩 firmware', '🖥️ host', '🔌 protocol-api', '🚧 in progress'], 'Native UART protocol and asynchronous events', [
      'Cover identity, settings, sensors, sound, PWM/RGB/strip, RF, menus, relays, reset, I2C, displays, and macros.',
      'Publish door, BT Audio, key, RF, output, programming, automation, fault, reset, and shutdown events immediately with source tags.',
      'Keep framing/CRC counters and recoverable errors visible without printing raw HELLO bytes outside debug mode.',
      'Deliver the same typed state through TUI, scripting, Go/C APIs, IPC, and network consumers.',
    ], 'Broad command coverage and many asynchronous events pass, but dedicated firmware fault, relay-change, and temperature-alarm events remain missing.'),
  requirement('protocol-frontpanel-menu-uptime', 6, 'Extend protocol schemas for live menus, front-panel snapshots, host state, and uptime', 'open',
    ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '🔌 protocol-api', '🐛 regression'], 'Configuration, menus, melodies, and programming surfaces', [
      'Query the live menu catalog with IDs, labels, descriptions, current page, and submode.',
      'Transport exact TM1637/LCD/key/front-panel snapshot state and remote input gestures.',
      'Expose host-connected/disconnected state, date/time, optional labels, and host-owned menu/session messages.',
      'Expose a PC-owned Idle/Running program state with source/reason text; consumers, APIs, macros, and the host UI may acquire/release named ownership claims and transient reference-counted leases without coupling state to the enclosure door.',
      'Do not let completion of one macro or automation clear an explicit consumer-owned Running claim; publish every effective transition through status, events, history, scripting, IPC, and network APIs.',
      'Keep raw device uptime and render readable uptime in every monitoring/API/history/scripting surface.',
    ], 'Current status/menu commands expose basic IDs and telemetry, but the host fallback still confuses legacy Voltage=0/Status=14 with schema-2 Status=0/RF=14 instead of consuming the advertised live catalog; richer schemas, full snapshot, host session state, and cross-surface uptime remain incomplete.'),
  requirement('protocol-simulator-transport', 6, 'Maintain deterministic native-protocol simulator and fragmented-transport tests', 'open',
    ['🔌 protocol-api', '🧪 testing', '🐛 regression', '🚧 in progress'], 'Native virtual board', [
      'Model the current bounded COBS/CRC/opcode shapes over a desktop transport.',
      'Cover fragmented/delayed frames, HELLO, status, settings, displays, outputs, reset telemetry, and events.',
      'Keep virtual EEPROM and reset state separate from host configuration.',
      'Run repeatable unit and raw protocol smoke tests.',
    ], 'A stale packaged host exposed a coverage gap: VirtualBoard still emitted legacy HELLO schema 1 while production firmware emits compact schema 3. PR #81 aligns the simulator, parser/authentication fixtures, and formatter; local Go/vet/CTest/TCP evidence passes, but the issue remains open until the PR is current, green, and merged.'),

  requirement('host-foundation-config-library', 7, 'Provide the Go host, Charm TUI foundation, separate hot-reloaded config, and reusable APIs', 'closed',
    ['🖥️ host', '🔌 protocol-api', '💾 storage', '✅ verified'], 'Host application, TUI, configuration, shell, IPC, and library', [
      'Implement the host in Go with Bubble Tea, Bubbles, and Lip Gloss.',
      'Keep watched PC JSON configuration separate from MCU EEPROM.',
      'Provide shell, one-shot CLI, scripting, JSON-RPC IPC, Go API, and C-compatible JSON ABI.',
      'Expose board commands and events consistently and prove the shared library from an external C caller.',
    ], 'All host packages and vet pass; hot reload, command surfaces, DLL/header exports, and an external C-caller ports smoke test are verified.'),
  requirement('tui-pages-controls', 7, 'Build polished multipage TUI controls for board, settings, RF, programming, and automation', 'open',
    ['🖥️ host', '✨ ux', '⚡ priority: high', '💡 enhancement'], 'TUI structure and interaction', [
      'Provide navigable dashboard, measurements, outputs, app/board settings, menus, RF, programming, automation, history, events, and console pages.',
      'Support arrow and mouse navigation with clear focus and responsive layouts.',
      'Add visible port, reset, relay/motion, PWM slider, RGB, sound/melody, menu, RF, and programming controls.',
      'Distinguish live versus persisted board values and expose all watched host settings.',
      'Exercise the actual Windows TUI and inspect representative screenshots before completion.',
    ], 'The current TUI has monitoring and command entry, but the requested pages, controls, mouse behavior, settings editors, and screenshot QA are unfinished.'),
  requirement('monitoring-format-history', 7, 'Improve monitoring presentation, adaptive units, subscriptions, graphs, and timeline', 'open',
    ['🖥️ host', '✨ ux', '💾 storage', '💡 enhancement'], 'TUI structure and interaction', [
      'Style grouped key/value monitoring and expand LED Temperature and BT Audio Temperature names/states.',
      'Use adaptive SI units, independent field visibility and precision, and suppress age text while samples are under 500 ms old.',
      'Configure sampling rates and stop status polling only when no TUI/script/automation/TCP/IPC/WebSocket subscriber needs it.',
      'Retain configurable history for 24 hours by default, graph measurements, and show important events in a timeline.',
      'Reflect authoritative relay/PWM/motion state from every source rather than optimistic local UI state.',
    ], 'Basic live telemetry is present; styling, adaptive units, age debounce, subscriber accounting, durable graphs/timeline, and complete state reconciliation are open.'),
  requirement('console-command-ux', 7, 'Finish console history, nested completion, command organization, and clean output', 'open',
    ['🖥️ host', '✨ ux', '🐛 regression'], 'TUI structure and interaction', [
      'Recall the previous command with Right Arrow on an empty prompt.',
      'Complete nested subcommands with Tab or Right Arrow without printing literal completion diagnostics.',
      'Organize commands by task and use semantic color instead of one all-green style.',
      'Provide clear, quit, and exit and hide raw HELLO bytes outside debug mode.',
      'Provide menu list and grouped discoverable help for the native and Urboot/Urclock surfaces.',
    ], 'The shell and command engine exist, but nested completion and the requested console interaction/styling regressions remain unresolved.'),
  requirement('host-automation-hotkeys-os', 7, 'Complete macros, melodies, automations, hotkeys, notifications, and guarded OS actions', 'open',
    ['🖥️ host', '✨ ux', '🛡️ safety', '💡 enhancement'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Run/cancel named relay/PWM/display macros and create/preview/play/stop board or streamed melodies/effects.',
      'Add default-disabled, audited virtual-key actions and configurable global hotkeys for board/app commands.',
      'Support optional door/BT/RF/device-event scripting with no unsafe action enabled by default.',
      'When the enclosure opens while PC-owned program state is Running, publish one immediate warning transition plus a cleared transition, use configurable host beep and actionable Stop toast cues, and expose both to scripts/APIs/history.',
      'Keep that warning PC-owned and in PC configuration rather than AVR EEPROM, and never let the reed itself mutate Running/Idle state.',
      'Expose guarded IP/system/power actions with explicit policy and confirmation.',
      'Expose a PC-owned System Actions menu with Suspend, Hibernate, Restart, and policy-gated primary-monitor DDC/CI brightness; apply watched configuration immediately and publish accepted/denied events.',
      'Show actionable desktop notifications whose buttons return through the authenticated safety path.',
    ], 'Guarded OS actions and DDC/CI brightness are source-complete with injectable-backend tests; a harmless live brightness/menu check remains. Macros, effects, melodies, and event automation exist, but hotkeys, actionable toasts, full UI, and live macro validation remain open.'),
  requirement('host-macro-recording-playback-sync', 7, 'Stream recorded macros into an MCU-timed queue with synchronized progress and safety', 'open',
    ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '🔌 protocol-api', '🛡️ safety', '🔥 priority: critical'], 'Configuration, menus, melodies, and programming surfaces', [
      'Record relay, motion, PWM/MOSFET, buzzer/melody, seven-segment/LCD message, RF transmit, menu/front-panel, and extensible command steps using precise monotonic relative timestamps.',
      'Add MCU monotonic timestamps/deltas to board-originated activation events where supported so USB/network arrival jitter is never the authoritative recording clock.',
      'Keep durable names, IDs, categories, user colors, and the library PC-side; stream ordinary native-protocol opcode/payload records into a bounded AVR SRAM circular buffer whose MCU scheduler executes locally while the host refills ahead.',
      'Reuse the existing firmware command dispatch, payload validation, and inherent motion/output interlocks instead of adding a flash-heavy second macro allow-list or duplicate policy layer; keep rich permissions/security in the host or bridge and only minimal self-recursion/queue-integrity protection on the MCU.',
      'Report MCU start time and per-step execution timestamps so the host computes signed timing delta against start plus due offset; also report accepted/executed indexes, queue fill/free, underruns, late count, maximum absolute error, completion/cancel/error, and a final faithful flag.',
      'Expose list, record, stop/save/discard, playback, progress, and cancel through hosted custom-menu capability 19 and full TUI, CLI, IPC, REST, RPC, WebSocket, and bridge surfaces.',
      'Mirror macro name/ID, elapsed/duration/current step on seven-segment, LCD, and TUI and emit synchronized start/progress/finish/cancel/error/faithful/underrun events.',
      'Let automations start by name/ID, cancel, or replace under an explicit concurrency policy and trigger from macro lifecycle/health events.',
      'Allow physical, host, automation, and API cancellation while applying identical relay, motion, output, programming, and queue-health gates to every source.',
      'Keep only the active queue in AVR RAM, report exact flash/SRAM costs and tradeoffs, and block release until live timing, refill, underrun, cancellation, and safety behavior are verified.',
    ], 'Current source contains the bounded AVR queue/scheduler and MCU-clock execution acknowledgements plus a host macro foundation, but durable record/CRUD UX, hosted menu, automation invocation, final health reporting, and live refill/underrun/cancel/timing verification remain open.'),
  requirement('host-keyboard-bindings-output-state', 7, 'Add configurable keyboard motion/output bindings with authoritative live-state reconciliation', 'open',
    ['🖥️ host', '✨ ux', '🛡️ safety', '⚡ priority: high'], 'TUI structure and interaction', [
      'Provide factory mappings A/S for Side B Up/Down and K/L for Side A Up/Down.',
      'Use real keydown/keyup semantics so holding a motion key sustains safe motion and release stops it.',
      'Make digits 1-9 configurable action bindings that may target relays or PWM outputs rather than fixed relay numbers.',
      'Let every binding select momentary or toggle/latch behavior and use Ctrl for its configured alternate behavior.',
      'Render authoritative relay, PWM, and motion state after actions from keyboard, RF, physical controls, automation, IPC, or a remote bridge.',
    ], 'This is a newly normalized requirement; no current implementation or interaction test demonstrates the requested bindings or cross-source state reconciliation.'),

  requirement('ipc-websocket-api-suite', 8, 'Provide versioned JSON-RPC, REST, and authenticated WebSocket command/event APIs', 'open',
    ['🔌 protocol-api', '🌐 networking', '🔒 security', '⚡ priority: high'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Keep durable versioned JSON-RPC and REST request/response contracts with correlated errors/results.',
      'Run an authenticated or safely local WebSocket server alongside IPC for commands and typed subscriptions.',
      'Cover status, USB, RF, door, BT, keys, outputs, programming, reset, automation, and shutdown.',
      'Allow open, close, reconnect, reset, quit, programming, and every ordinary controller command.',
    ], 'Loopback JSON-RPC and a Go WebSocket relay exist, but the unified versioned REST/WebSocket service and full lifecycle/event matrix are not complete.'),
  requirement('network-bridge-discovery', 8, 'Bridge controller hosts over the network with mDNS/SSDP discovery', 'open',
    ['🖥️ host', '🔌 protocol-api', '🌐 networking', '🔒 security'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Bridge one host through another for programming, monitoring, configuration, commands, queries, and events.',
      'Preserve one serial owner and surface remote lifecycle/errors like local IPC events.',
      'Advertise/discover non-secret service metadata through mDNS and SSDP where supported.',
      'Require explicit authenticated authority after discovery; discovery alone never grants control.',
    ], 'Single-owner local IPC is proven, but network bridging and mDNS/SSDP discovery are not implemented end to end.'),
  requirement('http-webhooks-socketio-messages', 8, 'Add bidirectional HTTP, webhooks, WebSocket client/server, Socket.IO, and actionable messages', 'open',
    ['🔌 protocol-api', '🌐 networking', '🔒 security', '💡 enhancement'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Provide inbound HTTP and configurable outbound GET/POST/PUT/PATCH/DELETE webhooks.',
      'Support WebSocket client and server roles and genuine Socket.IO compatibility as a separate protocol.',
      'Carry a typed source-tagged text envelope among local clients, servers, bridges, the board, and LCD.',
      'Authenticate, authorize, audit, and safety-check every actionable message.',
    ], 'A limited WebSocket relay exists; the broader HTTP/webhook, client role, Socket.IO protocol, and actionable message envelope remain open.'),
  requirement('remote-control-security', 8, 'Define security and policy gates for every remote and disruptive control path', 'open',
    ['🔌 protocol-api', '🌐 networking', '🔒 security', '🛡️ safety', '🔥 priority: critical'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Authenticate remote commands, subscriptions, toast actions, messages, bridges, and network APIs.',
      'Authorize operations by capability and route board commands through the same motion/programming safety guards.',
      'Keep disruptive OS actions and key injection disabled by default with explicit policy and confirmation.',
      'Protect secrets and publish only non-secret discovery metadata with a durable audit trail.',
    ], 'Local loopback ownership reduces current exposure, but the requested remote services and their unified authorization/audit model are not complete.'),

  requirement('stable-device-selection', 9, 'Select the controller by stable identity, friendly name, COM name, or VID/PID', 'closed',
    ['🖥️ host', '🌐 networking', '✅ verified'], 'Host application, TUI, configuration, shell, IPC, and library', [
      'Accept explicit COM, friendly product/name, VID/PID, USB serial, and instance selectors.',
      'Authenticate every candidate with PCController HELLO before accepting it.',
      'Persist the last successful host-side device and prompt when multiple matches are ambiguous.',
      'Provide CH340 defaults that remain overrideable by configuration and flags.',
    ], 'The host implements all selectors, HELLO validation, ambiguity handling, and persistence; live COM18 CH340 VID/PID/name/instance values were saved successfully.'),
  requirement('usb-reconnect-notifications', 9, 'Validate event-driven USB reconnect and opt-in DTR reset semantics', 'open',
    ['🖥️ host', '🧪 testing', '🔍 needs-hardware'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Use Windows arrival/removal notifications rather than periodic full-port enumeration.',
      'Emit precise disconnected, reconnecting, reconnected, and error events to every consumer.',
      'Keep DTR reset independent and default-disabled; pulse once only after a real reappearance when explicitly enabled.',
      'Verify unplug/replug, authenticated reconnect, TUI/IPC updates, and both DTR modes on hardware.',
    ], 'The notification/reconnect implementation and default-disabled option exist; physical removal/reappearance and opt-in reset have not been verified.'),
  requirement('primary-serial-owner-ipc', 9, 'Enforce one serial owner and route secondary processes through IPC', 'closed',
    ['🖥️ host', '🛡️ safety', '🔌 protocol-api', '✅ verified'], 'Host application, TUI, configuration, shell, IPC, and library', [
      'Let one long-running host own the serial port.',
      'Route secondary CLI, TUI, monitor, reset, shell, and programmer commands through correlated IPC.',
      'Fan board/USB events out without opening a second COM handle.',
      'Prove secondary commands against the authenticated primary owner.',
    ], 'The running primary authenticated the current board and separate commands shared the IPC connection for HELLO, status, settings, sound, and melody without a second serial owner.'),
  requirement('controller-discovery-authority', 9, 'Make controller-owned discovery authoritative and explain Win32 versus WMI/CIM drift', 'open',
    ['🖥️ host', '🌐 networking', '🐛 regression', '⚡ priority: high'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Use the shipped controller executable as the authority for development and deployment discovery.',
      'Reproduce why a direct WMI/CIM query saw only COM1 while controller discovery found COM18 and COM19 through its Windows device path.',
      'Document the actual discovery APIs, filters, identity data, and environmental cause of the difference.',
      'Add regression coverage and never block programming solely on WMI/CIM results.',
    ], 'Controller-owned discovery currently finds the devices, but the external discovery discrepancy has not yet been isolated or regression-tested.'),
  requirement('serial-lifecycle-contract', 9, 'Keep the serial protocol connected independently of telemetry subscriptions', 'open',
    ['🖥️ host', '🔌 protocol-api', '🛡️ safety', '🚧 in progress'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Keep application UART enabled, open, and auto-reconnecting by default even with no measurement subscribers.',
      'Allow subscription accounting to stop polling without suppressing asynchronous events or closing serial.',
      'Make explicit Close pause reconnect until Open resumes it.',
      'Expose open, close, reconnect, reset, quit/exit, and all commands through IPC/WebSocket with correlated results.',
    ], 'The current primary owner and basic port commands exist, but the subscriber-independent lifecycle contract and full remote lifecycle command coverage are not verified.'),

  requirement('uart-urclock-programming', 10, 'Use UART Urboot/Urclock as the normal programming path and verify application return', 'closed',
    ['🚀 programming', '🛡️ safety', '✅ verified'], 'Bootloader, programming, build scripts, and packaging', [
      'Configure current MiniCore Urboot/Urclock for the ATmega328P board and preserve EEPROM as selected.',
      'Release serial ownership, run maintained Arduino CLI/AVRDUDE urclock operations, and reauthenticate application HELLO.',
      'Support probe, metadata, read, write, verify, and start without pretending the native application protocol is the bootloader protocol.',
      'Keep USBasp as an explicit troubleshooting fallback only.',
    ], 'Urboot/fuses were ISP-verified and current firmware was UART-uploaded, flash-verified, and reauthenticated; host commands delegate to the maintained backend.'),
  requirement('preflash-backup-dedup-restore', 10, 'Require atomic flash/EEPROM backup, hash deduplication, and verified restore before writes', 'open',
    ['🚀 programming', '💾 storage', '🛡️ safety', '🔥 priority: critical'], 'Development EEPROM, repository, licensing, and documentation', [
      'Before any flash write, read flash and EEPROM through Urclock into the host data directory.',
      'Store flash blobs by SHA-256, reference hashes in names/manifests, and never duplicate identical firmware.',
      'Block a write after failed backup unless an explicit logged override is provided.',
      'Mark partial reads incomplete, retain raw logs, and verify restore/readback.',
      'Use hidden explicitly authorized USBasp only when UART cannot work.',
    ], 'A tested backup workflow and atomic manifests exist, but it has not run on the current board and automatic pre-write gating, deduplication, restore, and fallback policy are incomplete.'),
  requirement('canonical-host-programming-entrypoint', 10, 'Route every build, upload, verify, backup, and recovery through the host tool', 'open',
    ['🚀 programming', '🏗️ tooling-build', '🛡️ safety', '⚡ priority: high'], 'Development EEPROM, repository, licensing, and documentation', [
      'Make the canonical controller executable the normal entry point for compile/upload/verify/backup/recovery.',
      'Keep platform wrappers thin and route Node/root tooling through the guarded host command plan.',
      'Reject stale binaries and mismatched command contracts using embedded source identity.',
      'Provide hardware-free plan tests and a live UART programming verification.',
    ], 'The host can program successfully, but root/Node wrappers still contain separate policy and stale generated executables can shadow the current source build.'),
  requirement('hex-patch-settings-export', 10, 'Finish guarded Intel HEX patching and separate live settings export from EEPROM parsing', 'open',
    ['🚀 programming', '💾 storage', '🛡️ safety', '🚧 in progress'], 'Development EEPROM, repository, licensing, and documentation', [
      'Inspect named application, bootloader, EEPROM, and metadata regions with checksum/address/bounds validation.',
      'Patch only declared regions, retain the original, show before/after SHA-256, and require verify/readback.',
      'Keep live native-protocol settings export separate from offline EEPROM-image parsing.',
      'Preserve unknown bytes and identify the supported layout/hash without mixing MCU state into host config.',
    ], 'The generic guarded patch engine exists, but firmware identity is not a declared region and the live/offline settings workflows are not complete.'),
  requirement('graceful-host-snapshot', 10, 'Write an atomic diagnostic board snapshot on graceful host exit', 'open',
    ['🖥️ host', '💾 storage', '🚀 programming', '💡 enhancement'], 'Development EEPROM, repository, licensing, and documentation', [
      'Atomically store board identity, last status/settings/menu, connection/reset metadata, active programming operation, and artifact hashes.',
      'Keep this diagnostic host data separate from EEPROM mirrors and configuration.',
      'Never present the snapshot as proof that an interrupted write completed.',
      'Use the snapshot as a safe input to future migration and recovery diagnostics.',
    ], 'Backup manifests exist, but the requested graceful-exit diagnostic snapshot and its recovery integration are not implemented.'),

  requirement('arduino-go-dependencies', 11, 'Maintain current Arduino cores, libraries, Go modules, and globally discoverable UPX', 'closed',
    ['🏗️ tooling-build', '📦 dependencies', '✅ verified'], 'Arduino toolchain and dependencies', [
      'Audit and update installed Arduino cores and requested well-supported libraries through the configured network path.',
      'Declare all Go host dependencies and package checksums.',
      'Use fixed-size local AVR drivers where needed to fit the target without misrepresenting linked libraries.',
      'Install UPX globally on PATH without hard-coding an extraction directory.',
    ], 'The checklist records current core/library versions, declared Go modules, fixed local AVR drivers, and UPX 5.2.0 available globally without a source-path dependency.'),
  requirement('project-import-structure', 11, 'Preserve reusable project layers, merge LocalLib variants, and consolidate source/tool directories', 'closed',
    ['🏗️ tooling-build', '🧩 firmware', '🖥️ host', '✅ verified'], 'Project import, LocalLib merge, and structure', [
      'Start from the reusable Puzzles project layer without its business rules.',
      'Compare and selectively merge Puzzles, Timer, and motor/HMI LocalLib variants and document the choices.',
      'Keep root LocalLib/Project aggregation exactly once and use canonical Tools/Controller, Tools/Firmware, and Tools/VirtualBoard locations.',
      'Remove stale duplicate host/tool directories and align root documentation/scripts to canonical paths.',
    ], 'The merge history is documented, project/state layers were restored, directories were consolidated, and current build references use the canonical tool locations.'),
  requirement('native-virtual-board', 11, 'Provide a desktop virtual board for fast native protocol and behavior tests', 'closed',
    ['🏗️ tooling-build', '🧪 testing', '🔌 protocol-api', '✅ verified'], 'Native virtual board', [
      'Build a C++17/CMake virtual board with desktop GCC-compatible tooling.',
      'Model settings, independent virtual EEPROM, sensors, inputs, RF, outputs, displays, macros, strip, and resets.',
      'Speak the native protocol over TCP and support interactive injection.',
      'Pass native unit, raw protocol, and host fragmented-transport tests.',
    ], 'The simulator builds and its tests/smokes pass, including full status shape and reset journal behavior; cycle-accurate shared AVR translation was not required for this completed behavioral scope.'),
  requirement('tooling-entrypoint-consolidation', 11, 'Unify build and programmer policy behind one command-plan implementation', 'open',
    ['🏗️ tooling-build', '🚀 programming', '🐛 regression', '⚡ priority: high'], 'Tooling entry-point consolidation audit', [
      'Move board profile and build/programming policy out of divergent PowerShell, Bash, Node, and Go implementations.',
      'Keep CMD/Bash/platform launchers thin and generate/test equivalent plans, help, failures, artifacts, and USBasp authorization.',
      'Use CMake presets for the virtual board rather than duplicated platform pipelines.',
      'Use the project controller tool for development/deployment discovery and programming.',
    ], 'The audit found duplicated FQBN/policy and command-contract drift; firmware wrappers share Node, but root and host routes are not yet unified.'),
  requirement('canonical-host-artifact-packaging', 11, 'Produce one current source-identified controller artifact with verified packaging', 'open',
    ['🏗️ tooling-build', '🖥️ host', '🐛 regression', '⚡ priority: high'], 'Tooling entry-point consolidation audit', [
      'Choose one generated controller executable location and make every launcher resolve exactly it.',
      'Remove or reject stale shadow copies and embed a verifiable source hash in release and development builds.',
      'Stamp accurate Windows resources, collect notices, compress with UPX, and verify hashes/version metadata.',
      'Rebuild DLL/header and repeat an external caller smoke test for the final source.',
    ], 'Five generated copies with mixed versions/source identity were found, and current resource changes postdate the listed artifact hashes; final canonical packaging is open.'),

  requirement('github-license-notices', 12, 'Publish the complete repository with dual licensing and preserved third-party notices', 'closed',
    ['📚 documentation', '🏗️ tooling-build', '✅ verified'], 'Development EEPROM, repository, licensing, and documentation', [
      'Publish the audited project through the authenticated GitHub workflow with safe ignore rules.',
      'License original project code as MIT OR BSD-2-Clause.',
      'Preserve dependency licenses/notices and never relicense incorporated third-party code.',
      'Keep generated binaries, local caches, hardware identities, and private audit data out of public history.',
    ], 'Verified on public main: the source baseline is published, LICENSE declares MIT OR BSD-2-Clause with both license texts, THIRD_PARTY_NOTICES.md preserves dependency licenses, and a tracked-file audit found no generated executable, DLL, firmware image, log, cache, or private hardware-identity artifacts.'),
  requirement('canonical-documentation-guide', 12, 'Organize starter-friendly documentation with complete operational and architecture coverage', 'open',
    ['📚 documentation', '⏳ finalization', '🚧 in progress'], 'Development EEPROM, repository, licensing, and documentation', [
      'Use canonical semantic Markdown names and a clear reading order with repaired relative links.',
      'Cover architecture, hardware, firmware, host/TUI, protocol/API/RPC/WebSocket, configuration, build/upload/Urclock, simulation, troubleshooting, safety, and licensing.',
      'Keep operating instructions separate from evidence/status while maintaining the checklist as acceptance truth.',
      'Explain exact feature/size tradeoffs and distinguish source proof, automated tests, and live hardware proof.',
      'Include a board-menu versus host-menu ownership catalog for every page, with build-identified measured or clearly labelled estimated flash/SRAM deltas, lost offline behavior, protocol cost, feature gains, and user-selectable recommendations.',
      'Catalog initialization for INA219, PWM/PCA9685, DS18B20/OneWire, TM1637, shift registers, 433 MHz RF, LCD/I2C, Timer1 buzzer, relays, WS2811/WS2812/status RGB, UART, and Urboot/Urclock.',
      'For each peripheral record address/pins, rate/resolution/averaging/timing/polarity/calibration or pull-up parameters as applicable, whether each value is compiled, EEPROM-owned, or host-owned, why it was selected, safe alternatives, and verification evidence.',
    ], 'Several focused guides and the canonical checklist exist, but final naming/link coverage and all requested API/network/final-state documentation remain unfinished.'),
  requirement('final-code-documentation-gate', 12, 'Run the final concise code-comment and missing-requirement audit after layouts freeze', 'open',
    ['📚 documentation', '⏳ finalization', '🧪 testing'], 'Development EEPROM, repository, licensing, and documentation', [
      'Comment public/domain functions, state, configuration, hardware assumptions, and non-obvious safety/timing/unit constraints.',
      'Avoid comments that merely repeat syntax.',
      'Audit every normalized request against implementation and current evidence after protocol/EEPROM/flash layouts freeze.',
      'Run a final missing, regression, contradiction, and documentation review without promoting planned work to complete.',
    ], 'The checklist contains the gate, but active layouts and behavior are not frozen and the final code/documentation review has not run.'),
  requirement('requirements-backlog-publication', 12, 'Maintain a deduplicated public requirements map and true GitHub sub-issue hierarchy', 'open',
    ['📚 documentation', '🧪 testing', '🚧 in progress'], 'Development EEPROM, repository, licensing, and documentation', [
      'Normalize all distinct checklist and audited user requirements without publishing raw conversation text or private paths.',
      'Give every normalized item a stable requirement marker, clear acceptance criteria, evidence/gaps, labels, and evidence-based state.',
      'Attach each requirement as a true GitHub sub-issue of exactly one epic and summarize open/closed counts on the epics.',
      'Keep a canonical repository map and an idempotent sync/validation helper.',
      'Maintain one repository-linked PCController Development project containing all 13 epics and 62 requirements exactly once, with truthful workflow, Area, Priority, Verification metadata and practical backlog/area/hardware/completed views.',
    ], 'The public source baseline, stable-marker issue graph, GraphQL sub-issue links, labels, states, counts, Requirements Backlog, and idempotent validator are complete. A 16-page wiki commit is prepared outside the workspace and the repository wiki feature is enabled, but GitHub requires an initial page to be created in an owner-authorized web session before its .wiki.git remote exists. Project-board creation also remains blocked because the active gh credential lacks read:project (and therefore writable project access); authentication scopes were intentionally not changed.'),

  requirement('hardware-frontpanel-audio', 13, 'Validate final-image buttons, menus, reset stability, and audio cues on hardware', 'open',
    ['🧪 testing', '🔍 needs-hardware', '🎛️ front-panel', '🔥 priority: critical'], 'Final hardware validation and handoff', [
      'Test Buttons 1-4 Down/Up/Click, double-click, hold acceleration, and editors while monitoring reset count.',
      'Validate the nested settings fields, save/discard, default-page behavior, and no menu-navigation reset.',
      'Listen for boot melody, one clean beep per key, and save/discard/door/relay cues with Silent off.',
      'Confirm the first-run TUI remains synchronized through board ready and welcome-melody completion.',
    ], 'Earlier melody and buttons 1/2 evidence exists, but the final-image full key/menu/audio and synchronized first-run pass is not complete.'),
  requirement('hardware-door-bt-temperature', 13, 'Validate enclosure, BT Audio, and temperature-role transitions on hardware', 'open',
    ['🧪 testing', '🔍 needs-hardware', '🧩 firmware'], 'Final hardware validation and handoff', [
      'Toggle the reed and verify immediate open/close events, default-page return, light fade, RGB cue, and motion emergency behavior.',
      'Toggle BT Audio and verify Off/On/Blink classification plus named TUI/IPC events.',
      'Run controlled illumination on/off logging and prove tLED warms while tBT remains comparatively cool.',
      'Record the final firmware identity and reset/error counters during the pass.',
    ], 'Ambient snapshots, ROM IDs, and basic event paths exist; controlled transitions and role proof remain unperformed.'),
  requirement('hardware-pwm-displays-lighting', 13, 'Visually validate TM1637, PWM, enclosure fade, power/RGB, and D6 strip', 'open',
    ['🧪 testing', '🔍 needs-hardware', '🎛️ front-panel'], 'Final hardware validation and handoff', [
      'Confirm smooth responsive TM1637 measurements and editor blink behavior.',
      'Identify and exercise PWM user channels and auto demo without disturbing system-owned channels.',
      'Observe both enclosure fade directions, power indication, coherent RGB animations, and strip pixel order.',
      'Confirm emergency clear and ordinary mode-off recovery behavior.',
    ], 'Source and read-only live telemetry are available, but the requested visual/output validation under safe conditions is pending.'),
  requirement('hardware-relay-motion', 13, 'Load-test relay identification, motion directions, interlocks, and door policy safely', 'open',
    ['🧪 testing', '🔍 needs-hardware', '🛡️ safety', '🔥 priority: critical'], 'Final hardware validation and handoff', [
      'Identify R1-R8 wiring and verify R5-R8 toggle/momentary behavior.',
      'Test Side A/B directions, break/settle timing, release-to-stop, cross-side isolation, and safe reset.',
      'Exercise closed/open/always/never policy for local, RF, host, macro, and automation sources.',
      'Verify door transition stops motion according to policy without an unsafe transient.',
    ], 'Implementation-level guards exist, but no complete safely prepared load test covers this matrix.'),
  requirement('hardware-rf-handset', 13, 'Complete real-handset RF capture, mapping, gesture, removal, and transmit validation', 'open',
    ['🧪 testing', '🔍 needs-hardware', '📡 rf-433'], 'Final hardware validation and handoff', [
      'Review/remove stale Remote A data and capture A/B/C/D one at a time.',
      'Map each stable RF identity to an explicitly confirmed action.',
      'Verify short burst, click, hold, repeat, inferred release, list/remap/remove, and latency.',
      'Transmit on INT1 and confirm reception on another receiver.',
    ], 'Remote A has useful live evidence, but the complete handset set, CRUD, latency regression, stale record cleanup, and physical TX test remain open.'),
  requirement('hardware-lcd-usb-macro', 13, 'Validate optional LCD, USB lifecycle, and a harmless cancellable macro end to end', 'open',
    ['🧪 testing', '🔍 needs-hardware', '🖥️ host'], 'Final hardware validation and handoff', [
      'With a backpack connected, verify 0x27/0x3F detection, both LCD rows, host text, and concurrent TM1637.',
      'Unplug/replug USB and verify lifecycle events, authenticated reconnect, both DTR modes, TUI, and IPC updates.',
      'Run and cancel a harmless named macro that labels TM1637, writes LCD text, changes one PWM channel, and toggles one general relay.',
      'Capture logs/screenshots and restore all outputs to a safe state.',
    ], 'Each path exists in source or tests, but no current physical end-to-end pass covers the connected LCD, USB reappearance, and macro cancellation together.'),
  requirement('release-handoff', 13, 'Complete final release evidence, launch, operating handoff, and acceptance closure', 'open',
    ['🧪 testing', '🔍 needs-hardware', '📚 documentation', '🔥 priority: critical'], 'Final hardware validation and handoff', [
      'Rebuild firmware/host from current source, record source/artifact hashes, upload/verify through the canonical host path, and authenticate HELLO.',
      'Run automated, simulator, and screenshot-driven interaction checks across every TUI page, keyboard/mouse control, settings editor, console completion, CLI, public library API, IPC/RPC, REST/WebSocket bridge, reconnect, programming, backup, and restore surface.',
      'Exercise every load-safe board path: identity/settings/menu queries, front-panel preview and remote keys, measurements, displays/audio, door/BT events, illumination/PWM/RGB, RF, macro timing/cancel, and safe output reset; record relay/motion/LCD/load checks as passed, failed, or explicitly human-blocked rather than assuming them.',
      'Launch the final canonical host against the board and verify secondary IPC operation.',
      'Provide complete board/host operating, safety, programming, backup, recovery, and troubleshooting instructions.',
      'Publish a final per-area verification matrix with exact commands, artifact hashes, firmware identity, screenshots/logs, observed results, remaining blockers, and restored safe output state.',
      'Show WAIT and play the unique continuous attention ringtone only when genuine physical user input is required, then stop the cue promptly after the response.',
      'On final successful launch and handoff, leave the board in a safe-output state with the seven-segment display showing ok.',
      'Close parent epics only after every linked child has current completion evidence.',
    ], 'A prior host was launched and a current firmware image was verified, but source/tooling has continued to change and the outstanding physical/UX/network/finalization checks prevent release closure.'),
];

function gh(args, input) {
  const result = spawnSync('gh', args, {
    cwd: ROOT,
    encoding: 'utf8',
    input: input === undefined ? undefined : `${JSON.stringify(input)}\n`,
    maxBuffer: 16 * 1024 * 1024,
  });
  if (result.status !== 0) {
    throw new Error(`gh ${args.join(' ')} failed (${result.status}): ${result.stderr || result.stdout}`);
  }
  return result.stdout.trim();
}

function api(method, path, input) {
  const args = ['api'];
  if (method !== 'GET') args.push('--method', method);
  args.push(path);
  if (input !== undefined) args.push('--input', '-');
  const output = gh(args, input);
  return output ? JSON.parse(output) : null;
}

async function allIssues() {
  const issues = [];
  for (let page = 1; ; page += 1) {
    const batch = api('GET', `repos/${REPO}/issues?state=all&per_page=100&page=${page}`);
    issues.push(...batch.filter((item) => !item.pull_request));
    if (batch.length < 100) break;
  }
  return issues;
}

function marker(id) {
  return `<!-- requirement-id: ${id} -->`;
}

function bodyFor(item) {
  return [
    marker(item.id),
    '',
    `Parent epic: #${item.parent}`,
    '',
    '## Requirement',
    '',
    item.title,
    '',
    '## Acceptance criteria',
    '',
    ...item.criteria.map((criterion) => `- ${criterion}`),
    '',
    '## Current evidence and gaps',
    '',
    item.evidence,
    '',
    '## Traceability',
    '',
    `- Canonical checklist section: **${item.section}**`,
    '- Normalized from the project checklist and private local request audit; no raw conversation text is published.',
    '',
  ].join('\n');
}

function sameLabels(issue, expected) {
  const actual = issue.labels.map((label) => typeof label === 'string' ? label : label.name).sort();
  return JSON.stringify(actual) === JSON.stringify([...expected].sort());
}

function gqlAddSubIssue(parentNodeId, childNodeId) {
  const query = 'mutation($issueId:ID!,$subIssueId:ID!){addSubIssue(input:{issueId:$issueId,subIssueId:$subIssueId,replaceParent:true}){issue{number} subIssue{number}}}';
  return JSON.parse(gh(['api', 'graphql', '-f', `query=${query}`, '-f', `issueId=${parentNodeId}`, '-f', `subIssueId=${childNodeId}`]));
}

async function currentSubIssues(parent) {
  return api('GET', `repos/${REPO}/issues/${parent}/sub_issues?per_page=100`);
}

async function validateRemote(published) {
  const fresh = await allIssues();
  const errors = [];
  for (const item of published) {
    const matches = fresh.filter((issue) => issue.body?.includes(marker(item.id)));
    if (matches.length !== 1) {
      errors.push(`${item.id}: expected one stable marker, found ${matches.length}`);
      continue;
    }
    const issue = matches[0];
    if (issue.number !== item.number) errors.push(`${item.id}: issue number drifted`);
    if (issue.title !== item.title) errors.push(`${item.id}: title drifted`);
    if (issue.state.toLowerCase() !== item.state) errors.push(`${item.id}: state drifted`);
    if (!sameLabels(issue, item.labels)) errors.push(`${item.id}: labels drifted`);
  }

  const seen = new Map();
  for (const parent of Object.keys(EPICS).map(Number)) {
    const actual = await currentSubIssues(parent);
    const expected = published.filter((item) => item.parent === parent).map((item) => item.number).sort((a, b) => a - b);
    const actualNumbers = actual.map((item) => item.number).sort((a, b) => a - b);
    if (JSON.stringify(actualNumbers) !== JSON.stringify(expected)) {
      errors.push(`epic #${parent}: sub-issues differ; actual=${actualNumbers.join(',')} expected=${expected.join(',')}`);
    }
    for (const number of actualNumbers) seen.set(number, (seen.get(number) ?? 0) + 1);
  }
  for (const item of published) {
    if (seen.get(item.number) !== 1) errors.push(`${item.id}: linked to ${seen.get(item.number) ?? 0} epics`);
  }
  if (errors.length) throw new Error(`remote validation failed:\n- ${errors.join('\n- ')}`);

  const closed = published.filter((item) => item.state === 'closed').length;
  process.stdout.write(`validated remote: ${fresh.length} total issues; ${published.length} requirements (${published.length - closed} open, ${closed} closed); ${Object.keys(EPICS).length} open epics; every requirement linked exactly once\n`);
}

function issueSummary(item, issue) {
  return {
    ...item,
    number: issue.number,
    url: issue.html_url,
  };
}

function epicBody(number, children) {
  const closed = children.filter((child) => child.state === 'closed').length;
  const open = children.length - closed;
  return [
    '<!-- requirements-epic-sync -->',
    '',
    'Tracks the deduplicated, public-safe requirements in this project area. Every item below is a true GitHub sub-issue with acceptance criteria and current evidence/gaps.',
    '',
    `**Status:** ${open} open / ${closed} closed / ${children.length} total`,
    '',
    '## Sub-issues',
    '',
    ...children.map((child) => `- [${child.state === 'closed' ? 'x' : ' '}] #${child.number} — ${child.title} (\`${child.id}\`)`),
    '',
    '## Closure rule',
    '',
    'This epic closes only when every linked sub-issue is closed with current source, test, live-system, or explicit hardware evidence appropriate to its acceptance criteria.',
    '',
  ].join('\n');
}

function markdown(items) {
  const closed = items.filter((item) => item.state === 'closed').length;
  const lines = [
    '# Requirements Backlog',
    '',
    'This is the canonical public map from normalized project requirements to GitHub issues. Closely related requests are grouped into one verifiable requirement; raw conversation text, machine-local paths, and private audit data are intentionally excluded.',
    '',
    `- Repository: [${REPO}](https://github.com/${REPO})`,
    `- Normalized requirements: **${items.length}**`,
    `- Open: **${items.length - closed}**`,
    `- Closed with current evidence: **${closed}**`,
    '- State policy: hardware, live-system, regression, partial-integration, and finalization work stays open until its own acceptance evidence exists.',
    '',
  ];
  for (const [parentText, title] of Object.entries(EPICS)) {
    const parent = Number(parentText);
    const children = items.filter((item) => item.parent === parent).sort((a, b) => a.number - b.number);
    const epicOpen = children.filter((item) => item.state === 'open').length;
    const displayTitle = title.replace(/^\[Epic\]\s*/, '');
    lines.push(`## [#${parent} — ${displayTitle}](https://github.com/${REPO}/issues/${parent})`, '', `${epicOpen} open / ${children.length - epicOpen} closed / ${children.length} total`, '');
    lines.push('| ID | Issue | State | Requirement |', '|---|---:|:---:|---|');
    for (const child of children) {
      lines.push(`| \`${child.id}\` | [#${child.number}](${child.url}) | ${child.state === 'closed' ? '✅ closed' : '🟡 open'} | ${child.title.replaceAll('|', '\\|')} |`);
    }
    lines.push('');
  }
  lines.push('## Synchronization', '', 'Run the idempotent helper from the repository root:', '', '```sh', 'node Tools/Audit/sync-github-requirements.mjs --apply', '```', '', 'Without `--apply`, the helper performs a read-only plan and still validates catalog labels and epic identities.', '');
  return lines.join('\n');
}

async function main() {
  if (new Set(R.map((item) => item.id)).size !== R.length) throw new Error('duplicate requirement id');
  const labels = JSON.parse(gh(['label', 'list', '--repo', REPO, '--limit', '200', '--json', 'name,color']));
  const labelNames = new Set(labels.map((label) => label.name));
  const missingLabels = [...new Set(R.flatMap((item) => item.labels))].filter((label) => !labelNames.has(label));
  if (missingLabels.length) throw new Error(`missing repository labels: ${missingLabels.join(', ')}`);
  const labelColors = new Map(labels.map((label) => [label.name, label.color.toUpperCase()]));
  const colorDrift = Object.entries(EXPECTED_LABEL_COLORS)
    .filter(([name, color]) => labelNames.has(name) && labelColors.get(name) !== color)
    .map(([name, color]) => `${name}=${labelColors.get(name)} (expected ${color})`);
  if (colorDrift.length) throw new Error(`repository label color drift: ${colorDrift.join(', ')}`);

  let issues = await allIssues();
  const parents = new Map(issues.filter((issue) => EPICS[issue.number]).map((issue) => [issue.number, issue]));
  for (const [numberText, title] of Object.entries(EPICS)) {
    const number = Number(numberText);
    const issue = parents.get(number);
    if (!issue || issue.title !== title) throw new Error(`epic #${number} missing or title drifted`);
  }

  const published = [];
  for (const item of R) {
    const expectedBody = bodyFor(item);
    let issue = issues.find((candidate) => candidate.body?.includes(marker(item.id)));
    if (!issue) issue = issues.find((candidate) => candidate.title === item.title);
    if (!issue) {
      if (!APPLY) {
        process.stdout.write(`CREATE ${item.id}: ${item.title}\n`);
        published.push({ ...item, number: 0, url: '' });
        continue;
      }
      issue = api('POST', `repos/${REPO}/issues`, { title: item.title, body: expectedBody, labels: item.labels });
      issues.push(issue);
      process.stdout.write(`created #${issue.number} ${item.id}\n`);
    }

    const needsUpdate = issue.title !== item.title || issue.body !== expectedBody || !sameLabels(issue, item.labels) || issue.state.toLowerCase() !== item.state;
    if (APPLY && needsUpdate) {
      issue = api('PATCH', `repos/${REPO}/issues/${issue.number}`, {
        title: item.title,
        body: expectedBody,
        labels: item.labels,
        state: item.state,
        ...(item.state === 'closed' ? { state_reason: 'completed' } : {}),
      });
      const index = issues.findIndex((candidate) => candidate.number === issue.number);
      issues[index] = issue;
      process.stdout.write(`updated #${issue.number} ${item.id}\n`);
    }
    published.push(issueSummary(item, issue));
  }

  if (!APPLY) {
    process.stdout.write(`dry run: ${R.length} normalized requirements; rerun with --apply to mutate GitHub and write ${OUTPUT}\n`);
    return;
  }

  for (const parent of Object.keys(EPICS).map(Number)) {
    const linked = await currentSubIssues(parent);
    const linkedNumbers = new Set(linked.map((issue) => issue.number));
    const parentIssue = parents.get(parent);
    for (const child of published.filter((item) => item.parent === parent)) {
      if (linkedNumbers.has(child.number)) continue;
      const childIssue = issues.find((issue) => issue.number === child.number);
      gqlAddSubIssue(parentIssue.node_id, childIssue.node_id);
      process.stdout.write(`linked #${child.number} under #${parent}\n`);
    }
  }

  for (const parent of Object.keys(EPICS).map(Number)) {
    const children = published.filter((item) => item.parent === parent).sort((a, b) => a.number - b.number);
    const desiredState = children.length > 0 && children.every((child) => child.state === 'closed') ? 'closed' : 'open';
    const parentIssue = parents.get(parent);
    const body = epicBody(parent, children);
    if (parentIssue.body !== body || parentIssue.state.toLowerCase() !== desiredState) {
      api('PATCH', `repos/${REPO}/issues/${parent}`, {
        body,
        state: desiredState,
        ...(desiredState === 'closed' ? { state_reason: 'completed' } : {}),
      });
      process.stdout.write(`updated epic #${parent}\n`);
    }
  }

  await fs.writeFile(OUTPUT, markdown(published), 'utf8');
  process.stdout.write(`wrote ${OUTPUT}\n`);
  await validateRemote(published);
}

main().catch((error) => {
  process.stderr.write(`${error.stack ?? error.message}\n`);
  process.exitCode = 1;
});
