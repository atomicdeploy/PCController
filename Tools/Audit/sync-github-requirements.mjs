#!/usr/bin/env node

// Publishes the normalized, public-safe requirements catalog as GitHub issues.
// The local user-turn audit is intentionally never read or uploaded by this tool.

import { spawnSync } from 'node:child_process';
import { promises as fs } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { repositoryWebUrl, resolveRepository } from '../../.github/scripts/repository-context.mjs';

const APPLY = process.argv.includes('--apply');
const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');
const REPO = resolveRepository(process.env, { cwd: ROOT });
const REPO_URL = repositoryWebUrl(REPO, process.env);
const OUTPUT = resolve(ROOT, 'docs', 'Requirements-Backlog.md');

const EXPECTED_LABEL_COLORS = {
  '🧭 epic': '5319E7',
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

// Domain labels are the Kanban grouping contract. Priority, workflow, evidence,
// and regression labels are useful metadata, but they never substitute for an
// owning product or engineering domain.
const DOMAIN_LABELS = new Set([
  '🖥️ host',
  '🧩 firmware',
  '🎛️ front-panel',
  '📡 rf-433',
  '🔌 protocol-api',
  '💾 storage',
  '🚀 programming',
  '🛡️ safety',
  '🧪 testing',
  '🏗️ tooling-build',
  '📚 documentation',
  '🔍 needs-hardware',
  '🌐 networking',
  '✨ ux',
  '📦 dependencies',
  '🔒 security',
]);

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

const EPIC_LABELS = {
  1: ['🧭 epic', '🔥 priority: critical', '🧩 firmware', '🛡️ safety'],
  2: ['🧭 epic', '⚡ priority: high', '🧩 firmware', '🎛️ front-panel'],
  3: ['🧭 epic', '🔥 priority: critical', '🧩 firmware', '🛡️ safety'],
  4: ['🧭 epic', '⚡ priority: high', '🎛️ front-panel', '✨ ux'],
  5: ['🧭 epic', '🧩 firmware', '🖥️ host', '📡 rf-433'],
  6: ['🧭 epic', '🧩 firmware', '🖥️ host', '🔌 protocol-api'],
  7: ['🧭 epic', '⚡ priority: high', '🖥️ host', '✨ ux'],
  8: ['🧭 epic', '🔌 protocol-api', '🌐 networking', '🔒 security'],
  9: ['🧭 epic', '🖥️ host', '🛡️ safety', '🌐 networking'],
  10: ['🧭 epic', '🔥 priority: critical', '🛡️ safety', '💾 storage', '🚀 programming'],
  11: ['🧭 epic', '🧪 testing', '🏗️ tooling-build', '📦 dependencies'],
  12: ['🧭 epic', '📚 documentation', '⏳ finalization'],
  13: ['🧭 epic', '🔥 priority: critical', '🧪 testing', '🔍 needs-hardware'],
};

// Supplemental issues are intentionally not body-owned by the normalized
// requirement catalog. Their required domain labels are additive so human and
// automation-managed workflow labels remain intact.
const EXTRA_ISSUE_REQUIRED_LABELS = {
  102: ['🔌 protocol-api', '📚 documentation', '✨ ux'],
  103: ['🧩 firmware', '🔌 protocol-api', '🧪 testing', '🏗️ tooling-build'],
  104: ['🖥️ host', '✨ ux'],
  105: ['🖥️ host', '🧪 testing', '🔍 needs-hardware'],
  106: ['🖥️ host', '🔌 protocol-api', '🧪 testing', '🏗️ tooling-build'],
  107: ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '🔌 protocol-api', '💾 storage'],
  108: ['🖥️ host', '🔌 protocol-api', '🧪 testing', '🌐 networking', '✨ ux'],
  109: ['🖥️ host', '🔌 protocol-api', '🧪 testing', '🌐 networking', '🔒 security'],
  110: ['🖥️ host', '🚀 programming', '🧪 testing', '🏗️ tooling-build'],
  112: ['🖥️ host', '🏗️ tooling-build', '🔒 security', '📦 dependencies'],
  115: ['📦 dependencies', '🏗️ tooling-build'],
  131: ['🖥️ host', '💾 storage', '🧪 testing', '🔒 security'],
  134: ['📦 dependencies', '🏗️ tooling-build'],
  135: ['🖥️ host', '🧪 testing', '🏗️ tooling-build', '🔍 needs-hardware'],
  137: ['🖥️ host', '🛡️ safety', '🧪 testing', '🏗️ tooling-build', '🔒 security', '📦 dependencies'],
  139: ['🖥️ host', '🛡️ safety', '🧪 testing', '🏗️ tooling-build', '🔒 security', '📦 dependencies'],
  145: ['📦 dependencies'],
  148: ['🖥️ host', '🌐 networking', '🔒 security'],
  149: ['🖥️ host', '💾 storage', '🧪 testing'],
  154: ['🖥️ host', '🧪 testing'],
  155: ['🖥️ host', '🧪 testing', '🏗️ tooling-build'],
  156: ['🖥️ host', '🌐 networking', '🔒 security'],
  157: ['🧩 firmware', '🚀 programming', '🏗️ tooling-build'],
  159: ['🖥️ host', '🔌 protocol-api', '✨ ux'],
  160: ['🖥️ host', '🧪 testing', '✨ ux'],
  161: ['🖥️ host', '📚 documentation', '✨ ux'],
  162: ['🖥️ host', '🎛️ front-panel', '🔌 protocol-api'],
  163: ['🖥️ host', '🏗️ tooling-build', '✨ ux'],
  164: ['🖥️ host', '🔌 protocol-api', '✨ ux'],
  165: ['🖥️ host', '🔌 protocol-api', '✨ ux'],
  166: ['🧩 firmware', '🖥️ host', '🔌 protocol-api'],
  171: ['🖥️ host', '🛡️ safety', '🧪 testing'],
  172: ['🖥️ host', '✨ ux'],
  177: ['🖥️ host', '🔌 protocol-api', '🛡️ safety', '🧪 testing', '🌐 networking', '✨ ux'],
  182: ['🧩 firmware', '🚀 programming', '🧪 testing', '🔍 needs-hardware'],
  183: ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '🔌 protocol-api', '💾 storage', '🧪 testing', '🏗️ tooling-build', '🔍 needs-hardware', '✨ ux'],
  184: ['🧩 firmware', '🖥️ host', '🔌 protocol-api', '🧪 testing', '🌐 networking', '🔒 security'],
  185: ['🧩 firmware', '🖥️ host', '🔌 protocol-api', '💾 storage', '🚀 programming', '🧪 testing', '🏗️ tooling-build', '🔍 needs-hardware'],
  186: ['🖥️ host', '🔌 protocol-api', '🧪 testing', '📚 documentation', '🌐 networking', '🔒 security'],
  189: ['🖥️ host', '🧪 testing', '✨ ux'],
  190: ['🧩 firmware', '🖥️ host', '🔌 protocol-api', '🧪 testing', '🏗️ tooling-build'],
  191: ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '🔌 protocol-api', '🧪 testing', '🔍 needs-hardware', '✨ ux'],
  192: ['🖥️ host', '🔌 protocol-api', '🧪 testing', '📚 documentation', '✨ ux'],
  193: ['🖥️ host', '🧪 testing', '📚 documentation', '🏗️ tooling-build', '✨ ux', '📦 dependencies', '🔒 security'],
  194: ['🖥️ host', '🧪 testing', '📚 documentation', '🏗️ tooling-build', '✨ ux'],
};

// Supplemental issues retain their independently authored bodies, but a small
// number are still first-class children of a normalized epic.
const EXTRA_ISSUE_EPIC_PARENTS = {
  102: 6,
  103: 11,
  104: 7,
  105: 7,
  106: 7,
  107: 2,
  108: 8,
  109: 8,
  110: 11,
  112: 11,
  115: 11,
  135: 7,
  137: 11,
  139: 11,
  145: 11,
  148: 8,
  149: 7,
  154: 9,
  155: 10,
  156: 8,
  157: 10,
  164: 8,
  171: 8,
  177: 8,
  182: 13,
  183: 13,
  184: 6,
  185: 1,
  186: 8,
  190: 6,
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
      'Provide independent remember-last-state policies for motion (factory disabled) and non-motion relays/MOSFET values, with deferred wear-aware persistence and safe reset ordering.',
      'Use sound-on as the factory default while honoring retained EEPROM, and verify saved precision across reset.',
    ], 'Current source implements the record and separation; live settings decoding and voltage/current precision persistence across reset were verified.'),
  requirement('mcu-event-automation', 1, 'Persist compact board-owned event automations for offline-safe actions', 'open',
    ['🧩 firmware', '💾 storage', '🛡️ safety', '💡 enhancement'], 'Motion, enclosure, RGB, audio, and board automation', [
      'Store compact MCU-owned automation records in EEPROM separately from watched PC automation/configuration, without an unpublished-build migration chain.',
      'Match door, BT Audio, host-connected/disconnected, relay, learned-RF, and other bounded board events.',
      'Invoke existing safe relay/motion-stop, PWM, RGB/audio, RF-transmit, and host-macro-request paths without duplicating a flash-heavy policy engine.',
      'Expose transactional list/add/edit/remove/clear and readback through the board menu where feasible, host TUI/CLI/APIs, and EEPROM backup/offline inspection.',
      'Define deterministic ordering, recursion/rate bounds, reset behavior, and a safe host-loss action while keeping Silent and motion interlocks authoritative.',
    ], 'Host-owned automations are implemented, but the firmware has no EEPROM automation table, board CRUD opcodes, or offline event-to-action executor; this remains genuinely missing.'),
  requirement('reset-safety-journal', 1, 'Complete graceful reset safety and reliable reset-cause journal telemetry', 'open',
    ['🧩 firmware', '🛡️ safety', '💾 storage', '🧪 testing', '🔥 priority: critical'], 'Firmware lifecycle, persistence, and reset', [
      'Turn off side/general relays, PWM test, and all PWM channels before watchdog reset, with an explicit RGB cue.',
      'Emit reset cause and persistent count at boot and in status telemetry.',
      'Use a CRC-checked, marker-last wear-levelled journal with bounded migration and rollover behavior.',
      'Verify correct watchdog cause through the installed bootloader and perform a load-safe live reset test.',
      'Use Reboot for every user-facing action and transient Rebooting braille/spinner feedback; do not label ordinary operations Safe Reset, and mark genuinely dangerous operations explicitly.',
    ], 'Safe reset sequencing exists. Native production tests now cover empty/corrupt/torn records, invalidate-first marker-last ordering, all 64 physical slots, journal wrap, and the full 32-bit count rollover. The installed path previously reported cause 0, so bootloader-specific cause capture and a load-safe live reset remain open.'),
  requirement('firmware-identity-layout-time', 1, 'Finalize compact build identity, time model, flash budget, and migration architecture', 'open',
    ['🧩 firmware', '💾 storage', '⏳ finalization', '🚧 in progress'], 'Development EEPROM, repository, licensing, and documentation', [
      'Encode a backward-decodable 32-bit half-second build timestamp and render YYMMDDHHMMSS with source/application hashes.',
		'Query HELLO board identity, firmware hash, and decoded build date/time on every connect and reconnect; expose the result on the appropriate TUI, WebUI, tray, API, and diagnostic pages without retaining stale identity after disconnect.',
		'Provide a board-owned firmware-identity page: show the full hash on the LCD and alternate the first and last four hash characters on TM1637 with deterministic, nonblocking timing.',
      'Keep development artifacts explicitly unversioned (`0.0.0-development` where a package field is mandatory); identify firmware and candidate binaries by source/content hash plus packed build date/time until a release is deliberately versioned.',
      'Use one wrap-safe loop-time snapshot for ordinary services while leaving interrupts independent.',
      'Version/hash settings, RF, and automation layouts and perform old-image migration off-device with backup/readback verification.',
      'Allow documented development EEPROM reinitialization until layouts freeze and measure every flash/SRAM tradeoff.',
      'Inventory every firmware-resident and PC-hosted menu, then offer each ownership migration as an explicit user choice with measured flash/SRAM delta, lost offline behavior, protocol cost, and feature gain before changing it.',
      'Tie all size claims to a named build identity and distinguish measured linker-map deltas from estimates.',
    ], 'Schema-2 timestamp vectors, HELLO decoding, connection snapshots, and host identity rendering exist; the explicit host-only development EEPROM reinitialization path is hardware-free source-tested without adding firmware compatibility baggage, and a current build-identified flash checkpoint is documented. The dedicated board-owned hash page, complete reconnect-surface audit, final firmware HELLO verification, global time consolidation, an intentional live reinitialization, off-device migration workflow, and user-approved menu ownership decisions remain open.'),

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
      'Expose every user channel directly as a named 0..100% value; remove the PWM-mode concept and replace the old auto demo with a host macro/example that uses ordinary channel commands.',
      'Persist optional non-motion relay/MOSFET last values across reset, fix channel 0 writes/readback, and retain manual channel identification plus Off/Auto/On enclosure brightness settings.',
      'Fade both door-transition directions without jitter or a jump and use coherent configurable RGB transition colors.',
      'Keep PWM/enclosure/power/strip ownership here, but defer status-effect descriptor, palette/profile, EEPROM, preview/fallback, and host-versus-board ownership decisions to #107, #183, and #185 so this issue cannot contradict their canonical contract.',
      'Use one deterministic priority arbiter: HOT red/orange breathing with buzzer; Running plus open door hard red warning; host offline red; RF violet activity; Running plus closed door orange/yellow; BT Audio connected blue; BT Audio waiting/not connected calm blue breathing; BT Audio powered off green/red breathing; otherwise Idle green.',
      'Keep Running/Idle host-owned rather than inferred from the reed; smoothly restore the underlying state after transient RF/door/BT cues.',
      'Ease/damp transitions into and out of informational states without unrelated intermediate colors or abrupt RF palette jumps, while retaining immediate hard flashing for warning and critical states.',
      'Fix the dim-red/static regression so door and BT Audio transitions reach the correct priority state and enclosure illumination follows the reed again.',
      'Support the D6 11-pixel strip and document emergency all-channel clear versus ordinary mode-off semantics.',
      'Validate real outputs, fades, power/RGB colors, and pixel order under safe load.',
    ], 'Direct per-channel PWM, output persistence, enclosure easing, the priority arbiter, and damped informational transitions are source/test complete. Status-effect/profile persistence and ownership are intentionally governed by #107/#183/#185 rather than frozen here. The exact current firmware still needs visual/load validation for channel 0, reed-driven illumination, BT/RF/door priority, power/status outputs, and D6 pixel order; the reported dim-red and illumination regressions therefore remain open as live acceptance.'),
  requirement('displays-audio', 2, 'Finish smooth TM1637, optional LCD, buzzer, melody, and configurable cue behavior', 'open',
    ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '🔌 protocol-api', '✨ ux', '🔍 needs-hardware', '🚧 in progress', '🐛 regression'], 'TM1637 and optional I2C LCD', [
      'Cache TM1637 frames/brightness, preserve responsive UART service, and provide configurable precision and blink states.',
      'Drive an optional 16x2 LCD at 0x27 or 0x3F concurrently and accept bounded host text.',
      'Play the boot melody and exactly one clean key beep while Silent remains persistent and authoritative.',
      'Support board/host melodies plus configurable door and relay cues with save/discard/reset feedback.',
      'Name the sound editor bEEP, persist independent door-open and door-closed TM1637 brightness targets, default closed to true off, and fade between targets without blocking UART.',
      'Use one DRY navigation-feedback path so physical and TUI/remote menu gestures produce the same configurable beep, except read-only denial cues.',
      'Decode TM1637 decimal-point bits without shifting/corrupting voltage text and push changed front-panel frames to the host immediately rather than waiting for a slow status poll.',
      'Implement #191: arbitrary TM1637/LCD/both messages across every control surface, overflow-only automatic scrolling unless explicitly forced, speed/hold/wait controls, once/loop/interval policies, a bounded couple-times-per-minute ambient default, and one fully blank terminal frame before stop or repeat.',
      'Mirror generated buzzer note frequency, duration, source, and timing immediately to host clients for WebAudio and optional native playback without routing high-rate frames through ordinary logs.',
      'Use the canonical buzzer route board, host, both, or none with independent board and host silence; reserve audio terminology for future PCM and keep WinRing0 or any other native backend optional.',
      'Validate the final image by sight and listening, including a connected LCD when available.',
    ], 'Timer1 fixed the buzzer; cached display/decimal handling, door brightness fields/fading, shared navigation feedback, host melodies, and source-level frame notification now exist. Door/relay cue enable flags are persisted, but individual cue notes/frequencies/durations are still fixed rather than configurable; final-image key/cue listening, LCD hardware, and smooth-display visual checks remain open.'),
  requirement('cooperative-host-i2c-profile', 2, 'Measure and implement the cooperative host-driven I2C/LCD profile', 'closed',
    ['🧩 firmware', '🖥️ host', '🔌 protocol-api', '✅ verified'], 'Development EEPROM, repository, licensing, and documentation', [
      'Expose bounded probe/read/write/write-read operations while allowing intentional access to known and future I2C devices.',
      'Use an expiring host lease so local drivers pause and refresh safely after release.',
      'Keep a tiny offline LCD fallback and rich host-driven text/layout when connected.',
      'Measure exact flash/SRAM savings, cache/reset risks, and offline losses before selecting a production profile.',
    ], 'The bounded no-allow-list transfer opcode, 0-10 s expiring cooperative lease, local-driver pause/recovery, host LCD renderer, compact preloaded offline fallback, and measured 1328-flash/49-SRAM standalone-renderer tradeoff are implemented, documented, and source-tested.'),

  requirement('relay-motion-interlocks', 3, 'Verify relay mapping, break-before-make, side isolation, and safe stop behavior', 'open',
    ['🧩 firmware', '🛡️ safety', '🔥 priority: critical', '🔍 needs-hardware'], 'Relays and motion safety', [
      'Use R1/R3 as Side A/B direction and R2/R4 as their output/enable, with disable-break-direction-settle-enable sequencing.',
      'Stop a side on release, stop all on reset, and never energize opposing motion.',
      'Expose individual relay, side motion, all-off, and identification operations.',
      'Eliminate avoidable Down/start latency: direction and enable must follow only the configured safe break/settle interval, which defaults to 1 ms for the current load.',
      'Persist a stop policy selecting full direction-plus-output release or output-only release that retains the last direction relay, while every emergency/reset path still forces all off.',
      'Validate both directions, timing, isolation, and stop behavior under a safely prepared real load.',
    ], 'The corrected source mapping and sequencing exist, but load-safe physical direction/interlock validation is still required.'),
  requirement('motion-door-policy', 3, 'Apply a persisted four-mode motion-door safety policy to every command source', 'open',
    ['🧩 firmware', '🖥️ host', '🛡️ safety', '🔥 priority: critical'], 'Motion, enclosure, RGB, audio, and board automation', [
      'Persist closed, open, always, and never policies with factory default always.',
      'Apply the same decision to local keys, learned RF, host commands, macros, and automations.',
      'Keep stop/off available even when starts are denied, and enforce the final gate atomically in firmware.',
      'Expose the policy through board menus, host UI, CLI, APIs, and backup decoding.',
    ], 'The persisted four-mode predicate now gates every firmware motion-start path atomically: physical menu, RF side/direct-relay mappings, UART side/direct-test commands, and buffered macros. The complete eight-case policy/door matrix, unconditional stops, unaffected R5-R8, retained-direction stop, and denied persistence restore are source-tested. Board Settings now includes the compact SAFE editor with immediate fail-safe preview and atomic Save/Discard; host settings/TUI/CLI/API/backup decoding also exist. Physical loaded-motion acceptance remains open.'),
  requirement('relay-user-controls-break-setting', 3, 'Expose R5-R8 behaviors and configurable break timing across all control surfaces', 'open',
    ['🧩 firmware', '🖥️ host', '🛡️ safety', '🚧 in progress'], 'Relays and motion safety', [
      'Support R5-R8 toggle and momentary push behavior locally and remotely.',
      'Persist an exact board-owned 1..255 ms break-before-direction interval with a safe 1 ms minimum/default for the current loads.',
      'Keep direction-settle and cross-side interlocks independent of the configurable break.',
      'Expose and decode the value in menus, TUI, CLI, APIs, backups, and offline EEPROM tools.',
    ], 'R5-R8 behavior and the exact 1..255 ms EEPROM/settings/protocol/CLI/API/offline paths are implemented while preserving independent settle/interlock timing. The load-safe physical timing test remains open.'),

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
      'Expose page 0 as door, never STAT/Status, because it renders only OPEN/CLSD; retain stable numeric ID 0 for protocol navigation.',
      'Do not add a separate BT-status board page: retain tBT as the sensor page, use RGB for local BT Audio state, and keep full BT Audio events/details on host surfaces.',
      'Persist a configurable 1..31 second motion-menu exit chord without growing the EEPROM/settings payload.',
      'Use a configurable default page after boot/no-change door close and optionally save the last page across power loss.',
      'Persistently show/hide pages and reorder stable page IDs; browse visible pages and nested category children in configured ID/rank order.',
    ], 'The cap23 source now implements persistent visibility/order plus four category parents, leaf Back/Enter navigation, six-field settings, blinking, save/discard, default/save-last behavior, and no redundant BT-status page. Builds/tests cover the layout, but the final-image all-key hierarchy/editor pass remains open.'),
  requirement('first-run-board-synchronization', 4, 'Synchronize first-run setup, board initialization, and welcome melody', 'open',
    ['🖥️ host', '🎛️ front-panel', '✨ ux', '⚡ priority: high'], 'TUI structure and interaction', [
      'Show a polished first-run setup/preview animation and persist completion in PC configuration.',
      'Authenticate HELLO/ready before presenting the board as initialized.',
      'Start or observe the welcome melody while the setup page is visible and keep progress synchronized with the physical board.',
      'Leave the page only after initialization and melody completion, or show a bounded, actionable offline/error result.',
      'Opening the app must not reset the board unless the user explicitly enabled DTR reset.',
    ], 'The persisted first-run animation, authenticated HELLO/READY gate, buzzer-busy or bounded capability fallback grace, host welcome melody, timeout/error path, mouse/keyboard acknowledgement, and DTR-off default are source/test complete. The rebuilt packaged TUI still needs an actual Windows Terminal screenshot/listening pass against the board.'),
  requirement('frontpanel-snapshot-remote-menus', 4, 'Mirror the live front panel and support remote keys plus PC-defined board menus', 'open',
    ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '🔌 protocol-api'], 'Configuration, menus, melodies, and programming surfaces', [
      'Snapshot exact TM1637 bytes/mask/brightness/blink, LCD cells/address/backlight, active keys, current page, submenu, and mode.',
      'Render the snapshot in TUI/API clients and update it after physical-board changes.',
      'Inject four remote keys with down/up/hold/gesture semantics through the same board state machine and source-tagged events.',
      'Serve host-defined nested typed menus from PC JSON/YAML/TOML with confirmation, callbacks, capture timeout, and host-loss fallback.',
      'Honor live per-node brightness and edit-visual metadata so headers, values, read-only items, and unsaved edits are distinguishable; denied read-only input uses the shared denial cue without mutation.',
      'Receive changed-only pushed front-panel opcodes while supported; request snapshots only for initial sync, explicit refresh, sequence-gap recovery, or a visible bounded legacy fallback, without closing the serial link.',
    ], 'Exact schema-2 front-panel snapshots, remote-key gesture injection, TUI preview/press-and-hold controls, watched host-menu definitions, live brightness/edit metadata, read-only denial, live definition updates, and cap19 push/capture fallback are source/test complete. Physical mirroring and the flash-heavier board-pull/retry endpoint profile remain live/design acceptance gaps.'),
  requirement('lcd-console-status-events', 4, 'Mirror console context to LCD and make Door the event-aware default page', 'open',
    ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '✨ ux'], 'Motion, enclosure, RGB, audio, and board automation', [
      'Optionally mirror the active console prompt, completion, and result context to the 2x16 LCD without routine telemetry flicker.',
      'Prioritize HOT, error, door, motion, relay, and RF messages, then restore the prompt.',
      'Show PC OFFLINE / CONNECT USB when the host heartbeat expires, with slow scrolling only if needed.',
      'Make door the default board page, render OPEN/CLSD without any STAT label, and briefly overlay incoming action name plus flashing On/Off.',
      'Enable host-driven TM1637 text scrolling by default on selected pages: while authenticated, the Door page scrolls door is open or door is closed; it falls back immediately to local OPEN/CLSD when the host disappears.',
      'Keep scroll speed, inter-message gap, enabled pages, and host text watched/configurable; yield immediately to warnings, editors, menu navigation, programming, and higher-priority overlays while mirroring every rendered four-digit frame to host clients.',
      'Add the optional seven-segment animation only after measured flash savings make it safe.',
    ], 'Debounced prompt mirroring, priority event overlays, host-driven LCD ownership, compact PC-offline fallback, Door page zero, configurable Door text scrolling, immediate preview refresh, and higher-priority firmware arbitration are source/test complete. The optional seven-segment animation and final physical LCD/TM1637 presentation pass remain open.'),

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
      'Expose exactly two mutually exclusive modes: default indefinite multi-code learning, and timer learning with the configured duration and live remaining time visible on board, TUI, CLI, RPC/API, WebUI, and events.',
      'Call the bounded alternative timer mode canonically while retaining single and one-shot as accepted, documented synonyms for the same mode; always publish clear start/end/cancel/full notifications.',
      'Store at least 20 records if EEPROM endurance/layout permits, retaining CRC and individual management.',
      'Offer the action catalog locally when feasible and resolve compact IDs to host labels when connected.',
    ], 'Current source uses 20 twelve-byte CRC-checked records and learns new identities as Unmapped. The exact two-byte session request is `[mode, seconds]`: indefinite multi-code Learn is `[0, 0]`, bounded timer mode is `[1, 1..120]`, and single/one-shot remain accepted aliases. Board lifecycle events authoritatively report start, progress, end, cancel, full, remaining time, capture count, and mapping-required state; focused Go, Web, and Virtual Board tests pass. Final aggregate validation, fresh-image EEPROM/readback, and a real multi-button handset session remain open.'),
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
    ], 'Watched PC metadata keyed by stable RF identity, uniform hexadecimal/decimal presentation, the fixed color palette, searchable TUI action picker, staged ID-sorted reorder, firmware replace opcode, and readback-oriented host flow are source/test complete. Final-board reorder/rollback and handset UX remain unverified.'),

  requirement('protocol-native-uart', 6, 'Replace Firmata with the native COBS/opcode UART protocol', 'closed',
    ['🧩 firmware', '🖥️ host', '🔌 protocol-api', '✅ verified'], 'Native UART protocol and asynchronous events', [
      'Keep UART as the always-on 115200 8N1 application link.',
      'Frame magic/version/opcode/sequence/length/payload/CRC with bounded payloads and COBS delimiter handling.',
      'Support correlated request/response plus unsolicited HELLO, streams, and events.',
      'Remove Firmata from firmware and host dependencies.',
    ], 'Firmware, Go host, and protocol tests implement the native transport; Firmata is removed. This is the pre-existing closed issue 14.'),
  requirement('protocol-command-event-coverage', 6, 'Complete native command coverage and immediate typed event delivery', 'open',
    ['🧩 firmware', '🖥️ host', '🔌 protocol-api', '🧪 testing', '🌐 networking', '🚧 in progress'], 'Native UART protocol and asynchronous events', [
      'Cover identity, settings, sensors, sound, PWM/RGB/strip, RF, menus, relays, reset, I2C, displays, and macros.',
      'Publish door, BT Audio, key, RF, output, programming, automation, fault, reset, and shutdown events immediately with source tags.',
      'Keep framing/CRC counters and recoverable errors visible without printing raw HELLO bytes outside debug mode.',
      'Deliver the same typed state through TUI, scripting, Go/C APIs, IPC, and network consumers.',
      'Define a capability-negotiated board request/opcode for Monitor Off that routes through the host action dispatcher, returns an explicit accepted/denied/unsupported/result state, and emits the same typed audit event as every other command source.',
      'Negotiate optional capabilities semantically: tolerate unknown or unavailable operations with bounded errors, preserve recognizable common framing across feature-set drift, and carry no explicit compatibility guards or migration baggage for unpublished development builds.',
      'Retain SET_STREAM and MENU_ACTION as fully implemented inbound operations; schedule any consolidation/removal analysis for a later explicit approval and do not change their contract in this phase.',
      'Make changed-state delivery push-first on every capable board and fan it through IPC, WebSocket, Socket.IO, and bridges; polling is limited to initial sync, explicit refresh, sequence-gap repair, or bounded legacy fallback.',
      'Keep one-shot events on an ordered reliable lane and continuous measurements/rendered frames on bounded coalesced lanes that do not spam ordinary logs; preserve unknown events for the opaque #184 tunnel.',
    ], 'Broad command coverage plus immediate door, BT Audio, key, RF, PWM, relay, RF-learning, macro, reset, firmware-fault, and HOT transition events pass source tests. The Monitor Off board request/opcode and its typed outcome event remain unimplemented, and final-image cross-surface event latency and typed delivery through every packaged consumer still need live validation.'),
  requirement('protocol-frontpanel-menu-uptime', 6, 'Extend protocol schemas for live menus, front-panel snapshots, host state, and uptime', 'open',
    ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '🔌 protocol-api', '🐛 regression'], 'Configuration, menus, melodies, and programming surfaces', [
      'Query the live menu catalog with IDs, labels, descriptions, current page, and submode.',
      'Transport exact TM1637/LCD/key/front-panel snapshot state and remote input gestures.',
      'Expose host-connected/disconnected state, date/time, optional labels, and host-owned menu/session messages.',
      'Expose a host-owned Idle/Running program state with source/reason text; consumers, APIs, macros, and the host UI may acquire/release named ownership claims and transient reference-counted leases without coupling state to the enclosure door.',
      'Do not let completion of one macro or automation clear an explicit consumer-owned Running claim; publish every effective transition through status, events, history, scripting, IPC, and network APIs.',
      'Keep raw device uptime and render readable uptime in every monitoring/API/history/scripting surface.',
    ], 'The live menu catalog, exact front-panel snapshot, remote gestures, compact layout, host-owned Idle/Running state heartbeat/claims, raw/readable uptime, host-provided DATE/TIME values, and editable 1-4 character HOST-menu labels are source/test complete. The LCD path preserves the full value rather than truncating it to the short label. Final aggregate validation, live final-image observation, and every external surface still need end-to-end acceptance.'),
  requirement('protocol-simulator-transport', 6, 'Maintain deterministic native-protocol simulator and fragmented-transport tests', 'closed',
    ['🔌 protocol-api', '🧪 testing', '✅ verified'], 'Native virtual board', [
      'Model the current bounded COBS/CRC/opcode shapes over a desktop transport.',
      'Cover fragmented/delayed frames, HELLO, status, settings, displays, outputs, reset telemetry, and events.',
      'Keep virtual EEPROM and reset state separate from host configuration.',
      'Run repeatable unit and raw protocol smoke tests.',
    ], 'Verified by merged PR #81: VirtualBoard now emits the production compact schema-3 HELLO, exact parser/authentication and formatter regressions are covered, all GitHub checks passed, and a fresh host authenticated over TCP and rendered the build hash plus packed timestamp.'),

  requirement('host-foundation-config-library', 7, 'Provide the Go host, Charm TUI foundation, separate hot-reloaded config, and reusable APIs', 'open',
    ['🖥️ host', '🔌 protocol-api', '💾 storage', '🐛 regression', '⚡ priority: high', '🚧 in progress'], 'Host application, TUI, configuration, shell, IPC, and library', [
      'Implement the host in Go with Bubble Tea, Bubbles, and Lip Gloss.',
      'Keep watched PC JSON configuration separate from MCU EEPROM.',
      'Provide shell, one-shot CLI, scripting, JSON-RPC IPC, Go API, and C-compatible JSON ABI.',
      'Expose board commands and events consistently and prove the shared library from an external C caller.',
      'Accept the current remote_policy configuration, eliminate repeated configuration reload rejected events, atomically retain the last valid config on an actual invalid edit, and hot-apply watched peripheral names and UI options.',
      'Allow every relay, motion side, PWM/MOSFET, display, sensor, and other exposed peripheral to have a watched host-side name editable through TUI F2, config, IPC, and bridge APIs.',
      'Apply configuration and browser-preference mutations with stable before/after diff semantics, preserve deliberate explicit-empty values, suppress no-op notifications, and never let stale browser cache resurrect a newer host-owned value.',
      'Persist appearance, locale, direction, reduced-motion, numeric, and audio preferences in the watched host configuration and diff-deduplicate their updates across native/TUI, WebUI, IPC, and allowed cross-tab consumers.',
      'Treat local console columns, rows, and font family/size as watched host settings with the documented runtime-flag, environment, config-file, package/build-default precedence; never claim or attempt local window/font mutation for an SSH or otherwise remote terminal.',
      'Use direct native Win32, WinRT, and COM adapters for desktop integration instead of spawning runtime PowerShell; show native TaskDialog or an explicit platform fallback for pre-main, missing-dependency, and fatal errors.',
    ], 'The host foundation, command surfaces, DLL/header, file watcher, current remote_policy schema, last-known-good invalid-edit handling, duplicate reload-error suppression, watched UI/peripheral naming, semantic configuration diffs, explicit false/zero retention, no-op suppression, and host-authoritative appearance synchronization are source/test complete across native/TUI, WebUI, IPC, and tabs. Local Windows console sizing/font configuration, settings interaction, package/build/config/environment/runtime precedence, SSH/RDP remote-terminal detection, and rollback behavior are source-tested; direct injected ConPTY coverage and packaged first-open visual observation remain open. A stable native Windows external-C executable loaded the freshly built DLL and invoked the ports JSON ABI without opening COM. Toast/shortcut paths still spawn PowerShell, and packaged hot-reload/native-boundary acceptance remains open.'),
  requirement('tui-pages-controls', 7, 'Build polished multipage TUI controls for board, settings, RF, programming, and automation', 'open',
    ['🖥️ host', '🧪 testing', '✨ ux', '⚡ priority: high', '💡 enhancement', '🚧 in progress', '🐛 regression'], 'TUI structure and interaction', [
      'Provide navigable dashboard, measurements, outputs, app/board settings, menus, RF, programming, automation, history, events, and console pages.',
      'Support arrow and mouse navigation with clear focus and responsive layouts.',
      'Add visible port, reset, relay/motion, PWM slider, RGB, sound/melody, menu, RF, and programming controls.',
      'Distinguish live versus persisted board values and expose all watched host settings.',
      'Exercise the actual Windows TUI and inspect representative screenshots before completion.',
      'Rename Outputs to Control; use centered rounded Charm tables with configurable compact/expanded layouts, complete border/action coloring, two-line grouped headers, correct visible-width padding, nested submenus, and arrow keys reserved for page interaction.',
      'Hide the integrated terminal on navigation-heavy pages and toggle it with tilde; fix menu click offsets and mouse-wheel navigation while keeping configured Control-page hotkeys active.',
      'Default to one Open/Close toggle with optional split buttons; show transient braille progress such as Rebooting, color Execute semantically, and render actual ON/OFF instead of a redundant toggle label.',
      'Use modal/dialog editors by default, with slider plus typed 0..100% values/units for brightness and outputs; fix stale/off-by-one setting activation, provide default-page selection from Board and Menus, group precision fields, and edit every host-owned status color independently with live previews while clearly distinguishing the MCU-persistent Ready color/global brightness.',
      'Refresh Board values on page entry; hide build-only Swap temperature roles; group unrelated board/settings fields, show BT Audio only, retain a hidden TODO for future Bluetooth Serial, and never expose ownership internals; use HOST in user-facing text.',
      'Show one dim LCD not-detected-at-0x27-or-0x3F row linking to LCD settings, suppress repeated console notices, hide zero protocol errors but show nonzero errors prominently, and split Last Reset/Reset Count into one aligned row.',
      'Make RF actions state-sensitive (Learn only while idle, Cancel only while learning), use View In rather than Radix, remove static UNMAPPED/USBasp/internal hints, and keep timer/single/one-shot duration visible.',
      'On a local interactive console, apply configured first-open columns/rows and expose safe controls for changing the current console size and font family/size; detect SSH and other remote terminals and report the operation as unavailable without mutating their display.',
      'Implement #189 and verify the exact reported Control-page off-by-one, GROUP/separator, Enter-hint, header/highlight, ellipsis, configurable semantic color, horizontal adjustment, and mouse-drag regressions.',
      'Set and restore a sanitized dynamic terminal title, emit configured OSC 0/2 and update-progress OSC status codes where supported, and navigate matching local instances to Programming/Updates while bridge operations publish real progress.',
    ], 'Typed numeric modals, brightness mapping, shared visible-width geometry, rendered-row hit-tests, settings editors, focus isolation, and state-sensitive actions are source-tested. The newly reported #189 Control-table defects and packaged keyboard/mouse rendering disprove a blanket interaction-complete claim and remain open, along with direct ConPTY, title/OSC restoration, update navigation/progress, first-open screenshots, and live-board feedback.'),
  requirement('ui-surface-capability-parity', 7, 'Generate and enforce declared capability parity across TUI, WebUI, native GUI, CLI, and APIs', 'open',
    ['🖥️ host', '🔌 protocol-api', '🧪 testing', '✨ ux', '⏳ finalization', '💡 enhancement'], 'Host application, TUI, configuration, shell, IPC, and library', [
      'Define one typed capability/page/action registry with stable IDs, domains, query-versus-mutation classification, schemas, connection/capability/policy requirements, event bindings, and availability reasons.',
      'Declare the disposition of every capability on TUI, WebUI, native GUI/tray, CLI, REST/JSON-RPC/WebSocket, and bridge surfaces as implemented, intentionally not applicable, or pending with a linked issue.',
      'Generate or adapt navigation, validation, help, API schemas, and availability metadata from the registry instead of repeating hand-maintained definitions in each controller.',
      'Bring useful TUI-only board-menu, RF, programming, automation, and console workflows to WebUI/native surfaces where applicable, and expose WebUI-only data/device workspaces through equivalent TUI/native workflows.',
      'Keep genuinely local console sizing/font operations explicitly local-only and never simulate unsupported behavior over SSH or a remote browser.',
      'Add repository checks and golden tests that reject duplicate capability IDs, undeclared omissions, stale labels, incompatible schemas, or surface drift.',
    ], 'The TUI currently declares ten pages, the WebUI eight top-level routes, and the native tray a smaller command set. Several capabilities are reachable only through differently grouped workspaces, while native Device/Data/Events and dedicated board workflows are absent. Availability, navigation, and validation metadata are repeated across the command engine, TUI, WebUI, native shell, and API schema layers. A canonical registry and automated parity gate do not yet exist.'),
  requirement('monitoring-format-history', 7, 'Improve monitoring presentation, adaptive units, subscriptions, graphs, and timeline', 'open',
    ['🖥️ host', '🔌 protocol-api', '💾 storage', '🧪 testing', '✨ ux', '💡 enhancement', '🚧 in progress', '🐛 regression'], 'TUI structure and interaction', [
      'Style grouped key/value monitoring and expand LED Temperature and BT Audio Temperature names/states.',
      'Use adaptive SI units, independent field visibility and precision, and suppress age text while samples are under 500 ms old.',
      'Configure source sampling and client render/refresh rates independently; prefer board-pushed changed values and use polling only for initial sync, explicit refresh, sequence-gap repair, or a bounded visible legacy fallback.',
      'Retain configurable history for 24 hours by default, graph measurements, and show important events in a timeline.',
      'Reflect authoritative relay/PWM/motion state from every source rather than optimistic local UI state.',
      'Color voltage/current/power/temperature and important state values semantically, add aligned expandable mini-graphs, and use terminal-safe success/warning/error indicators without noisy static blink labels.',
      'Use consistent measurement names without an isolated INA219 prefix, group unrelated values, show each PWM channel as named 0..100%, and navigate a selected channel to its mixer-style slider.',
      'Retain structured event metadata and expose it through accessible disclosure, copy, filter, selection, clear, and export actions instead of dropping it from the visual timeline; preserve stable viewport anchoring during live prepends and reconnects.',
      'Separate continuous measurements, LED/display frames, and similar high-rate state from useful one-shot events on every UI/logging surface; coalesce with freshness/backpressure counters and show them in ordinary logs only when explicit debug filtering enables them.',
      'Implement #190 with per-opcode UART byte/rate/occupancy and latency baselines, largest-consumer reporting, before/after budgets, and physical-board verification.',
    ], 'Monitoring data, adaptive units, demand accounting, semantic grouped TUI tables, immediate changed-frame preview, named 0..100% PWM rows, aligned graphs, authoritative event reconciliation, timeline storage, restart-durable measurement history, and the typed Web data/event workspace are source-tested. The Web workspace retains structured metadata with recursive disclosure, sort/filter/selection/column actions, JSON/CSV export, bounded windows, and stable live-prepend/reconnect anchoring. Packaged visual review and live-board reconciliation remain open.'),
  requirement('console-command-ux', 7, 'Finish console history, nested completion, command organization, and clean output', 'open',
    ['🖥️ host', '🧪 testing', '✨ ux', '🐛 regression', '🚧 in progress'], 'TUI structure and interaction', [
      'Recall the previous command with Right Arrow on an empty prompt.',
      'Complete nested subcommands with Tab or Right Arrow without printing literal completion diagnostics.',
      'Organize commands by task and use semantic color instead of one all-green style.',
      'Provide clear, quit, and exit and hide raw HELLO bytes outside debug mode.',
      'Provide menu list and grouped discoverable help for the native and Urboot/Urclock surfaces.',
      'Make config set ui.app_title a valid persistent hot-applied command and route interactive value requests to page dialogs rather than requiring inline-terminal editing.',
      'Keep internal implementation notes out of ordinary UI/help; retain expert troubleshooting details only in explicit advanced/debug documentation.',
    ], 'History/right-arrow recall works wherever the optional terminal is visible, nested completion starts on the first candidate without mutating render state, stale PWM-mode suggestions are removed, grouped help/semantic output/clear/quit/exit/menu list/raw-HELLO suppression are covered, and persistent config edits route through page-owned modals. Rebuilt packaged console/TUI interaction still requires live visual verification.'),
  requirement('host-automation-hotkeys-os', 7, 'Complete macros, melodies, automations, hotkeys, notifications, and guarded OS actions', 'open',
    ['🖥️ host', '✨ ux', '🛡️ safety', '💡 enhancement'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Run/cancel named relay/PWM/display macros and create/preview/play/stop board or streamed melodies/effects.',
      'Add default-disabled, audited virtual-key actions and configurable global hotkeys for board/app commands.',
      'Support optional door/BT/RF/device-event scripting with no unsafe action enabled by default.',
      'When the enclosure opens while host-owned program state is Running, publish one immediate warning transition plus a cleared transition, use configurable host beep and actionable Stop toast cues, and expose both to scripts/APIs/history.',
      'Keep that warning host-owned and in PC configuration rather than AVR EEPROM, and never let the reed itself mutate Running/Idle state.',
      'Expose guarded IP/system/power actions with explicit policy and confirmation.',
      'Use native, platform-specific providers under every supported OS to query internal laptop-panel and external-monitor brightness, including per-display identity, range, and unsupported-state diagnostics; writes remain explicitly policy-gated.',
		'Query and set system master volume and mute through the native OS audio session/device API, publish authoritative changes, and keep harmless reads available when writes are denied.',
		'Query normalized system battery, AC/charging, remaining-capacity, and availability state through native OS power APIs without treating desktops as errors.',
      'Issue and track Suspend/Sleep, Hibernate, Shut down, and Reboot through native OS power APIs with explicit policy/confirmation, accepted/in-progress/completed/failed/interrupted states, session-transition evidence, and no false success when the OS denies or cancels a request.',
		'Expose the normalized brightness, volume/mute, battery, and power-operation state through the System Actions menu, TUI, tray, WebUI, CLI, IPC, REST, WebSocket, automations, and history; apply watched configuration immediately.',
      'Issue and track Monitor Off through the native per-OS display-power provider for internal and external displays where supported, with explicit policy/confirmation and authoritative accepted/denied/unsupported/completed/failed outcomes regardless of whether the request came from a board opcode, local TUI/WebUI/CLI, IPC/API/WebSocket, automation, bridge, or another authenticated instance.',
      'Show actionable desktop notifications whose buttons return through the authenticated safety path.',
      'Use branded Windows toast/balloon icons, concise emoji/semantic styling, and polished actionable layouts without exposing internal mechanics.',
      'Integrate Jump List tasks, taskbar progress/overlay state, and thumbnail actions where supported; optional external COM or secure JavaScript adapters must enter through the same authenticated command dispatcher and policy audit.',
    ], 'Macros/effects/melodies, event automations, configurable global hotkeys, audited virtual-key injection, actionable Windows notifications, Running-door warning/clear, guarded Windows OS actions, DDC/CI external brightness, and native WMI laptop-panel fallback are source-tested. Cross-platform multi-display enumeration, native system volume/mute, normalized battery state, durable power-operation outcome tracking, Monitor Off dispatch/outcome tracking, and complete UI/API exposure remain missing. Toast delivery and shortcut registration still spawn PowerShell, and Jump List/taskbar/thumbnail plus optional external-adapter contracts are absent. Live hotkey/toast action, harmless brightness, and physical macro observations also remain open.'),
  requirement('privileged-service-tray-controller', 7, 'Run a privileged background service with a separate interactive tray controller', 'open',
    ['🖥️ host', '🛡️ safety', '✨ ux', '🏗️ tooling-build', '⚡ priority: high', '💡 enhancement'], 'Host application, TUI, configuration, shell, IPC, and library', [
      'Install, repair, start, stop, and uninstall a native per-OS service/daemon that can own controller ports and authenticated APIs in the background with only the privileges required for configured hardware and OS actions.',
      'Keep the privileged service headless and session-independent; run a separate unelevated per-user tray/controller instance that authenticates over local IPC and never relies on service-side interactive desktop access.',
      'Allow the tray instance to launch or foreground the native Win32 GUI, a new or existing TUI console, and the WebUI, while remaining useful when no board is connected.',
      'Expose quick tray controls for menu navigation, watched setting/value selection, multi-port selection, open/close/reconnect, current board/host state, and separate Exit Tray versus guarded Stop Service actions.',
      'Support service-only, tray-only attachment to an existing primary, and service-plus-tray startup policies without creating a second serial owner or silently elevating an interactive client.',
      'Use canonical per-machine and per-user data/config paths, bounded startup/recovery, durable logs, least-privilege ACLs, and explicit unsupported-platform behavior.',
    ], 'The current Windows tray is an in-process companion of controller web and already exposes connection state plus page links, but Controller has no native service/daemon installer or privileged headless primary, no separate unelevated tray client, no tray port/settings editor, and no independent tray/service lifecycle.'),
  requirement('host-macro-recording-playback-sync', 7, 'Stream recorded macros into an MCU-timed queue with synchronized progress and safety', 'open',
    ['🧩 firmware', '🖥️ host', '🎛️ front-panel', '🔌 protocol-api', '🛡️ safety', '💾 storage', '🧪 testing', '🔥 priority: critical', '🚧 in progress'], 'Configuration, menus, melodies, and programming surfaces', [
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
      'Allow compact RGB/effect/profile activation as timed macro steps; ordinary effects remain board-native descriptors, while host frame streaming is an explicit preview or legacy fallback rather than the default macro representation.',
    ], 'The bounded AVR queue/scheduler, MCU-clock acknowledgements, host recorder/refill/faithfulness engine, durable library, automation invocation, hosted-menu model, and rich searchable keyboard/mouse TUI CRUD/progress workspace are source/test complete; the timestamped-event parser regression is fixed and virtual playback is faithful. Physical refill/underrun/cancel/timing/display/output validation remains open.'),
  requirement('host-keyboard-bindings-output-state', 7, 'Add configurable keyboard motion/output bindings with authoritative live-state reconciliation', 'open',
    ['🖥️ host', '✨ ux', '🛡️ safety', '⚡ priority: high'], 'TUI structure and interaction', [
      'Provide factory mappings A/S for Side B Up/Down and K/L for Side A Up/Down.',
      'Use real keydown/keyup semantics so holding a motion key sustains safe motion and release stops it.',
      'Make digits 1-9 configurable action bindings that may target relays or PWM outputs rather than fixed relay numbers.',
      'Let every binding select momentary or toggle/latch behavior and use Ctrl for its configured alternate behavior.',
      'Render authoritative relay, PWM, and motion state after actions from keyboard, RF, physical controls, automation, IPC, or a remote bridge.',
    ], 'Watched bindings provide the A/S and K/L motion defaults plus configurable digit actions, Ctrl alternate semantics, and paired key-down/up handling with held-key release on disconnect/exit; unit tests cover configuration and injection. Real TUI key-hold behavior and authoritative physical/RF/bridge state reconciliation remain open.'),
  requirement('embedded-webui-native-experience', 7, 'Deliver the embedded responsive WebUI as a complete native-feeling controller client', 'open',
    ['🖥️ host', '🔌 protocol-api', '✨ ux', '🧪 testing', '⚡ priority: high', '🐛 regression', '🚧 in progress'], 'Embedded WebUI', [
      'Build and embed one production single-page application plus a deterministic portable export; support responsive desktop/mobile layouts, English/Persian RTL/LTR, the bundled Persian font, light/dark/system themes, reduced motion, and network-only installable-app behavior.',
      'Keep connection truth and capability gating authoritative: never claim Live or show device-only controls without an authenticated controller and never auto-open a browser while disconnected, while host diagnostics, discovery, and reconnect remain available.',
      'Render real telemetry charts and typed controls for every supported board and host capability; normalize and validate input while editing, keep field actions aligned to their inputs, and preserve drafts through authoritative live-state reconciliation.',
      'Provide complete accessible iconography, keyboard and global-hotkey integration, one kbd element per physical key, dialogs, context menus, non-jumping transitions, dynamic state copy, and optional non-semantic audio/haptic feedback.',
      'Carry authenticated terminal commands and important events full duplex through correlated WebSocket JSON-RPC with bounded REST fallback; validate BroadcastChannel messages and render safe console.* levels, substitutions, objects, tables, and %c styles without evaluating HTML.',
      'Serve GET/HEAD with correct MIME types, validators, favicon, closed/open/suffix/multipart byte ranges, If-Range and 416 handling, and force a safe reload when the embedded resource identity changes.',
      'Treat an unstyled or text-only root page as a release-blocking regression: the embedded and portable builds at `/` must load the complete hashed CSS/JavaScript/font/icon assets and restore the styled navigation, actions, colors, and controller functionality instead of silently falling back to placeholder text.',
      'Present structured data and event collections through one professional formatter instead of object coercion, with accessible sort/filter/columns/selection/keyboard context actions, all/filtered export, copy/clear actions, expandable event metadata, and bounded or virtualized large-data rendering.',
      'Make watched host appearance/UI preferences authoritative across WebUI, native/TUI surfaces, and tabs with diff-deduplicated synchronization; browser storage is only a resilient local cache.',
      'Run packaged Browser acceptance across widths, locales, themes, keyboard-only and reduced-motion flows, two-tab synchronization, disconnect/reconnect, terminal/event duplex, and console/network cleanliness.',
    ], 'The embedded application, portable export, localization/theme system, connection and capability gating, telemetry charts, device workbench, validated settings, accessible shortcuts/dialogs/context actions, optional feedback, safe console renderer, WebSocket/REST/BroadcastChannel transport, favicon, complete range-serving contract, typed data/event workspace, and host-authoritative ETag-guarded appearance synchronization have source tests. The rebuilt Windows executable now serves the root and every referenced hashed JavaScript, CSS, configuration, icon, and manifest asset with HTTP 200; stale Vite entry requests receive a no-store 307 redirect to the current embedded entry, with source tests covering the recovery path. David-PC also fetched the edge root over the LAN with HTTP 200. Final packaged Browser acceptance across a live board connection, responsive widths, locales/themes, two-tab synchronization, terminal/event duplex, and console/network cleanliness remains open.'),

  requirement('ipc-websocket-api-suite', 8, 'Provide unversioned living IPC, REST, JSON-RPC, WebSocket, and bridge APIs', 'open',
    ['🔌 protocol-api', '🌐 networking', '🔒 security', '⚡ priority: high', '🚧 in progress'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Keep one living JSON-RPC and REST surface with correlated errors/results and no selectable product API versions.',
      'Run an authenticated or safely local WebSocket server alongside IPC for commands and typed subscriptions.',
      'Cover status, USB, RF, door, BT, keys, outputs, programming, reset, automation, and shutdown.',
      'Allow open, close, reconnect, reset, quit, programming, and every ordinary controller command.',
      'Expose one correlated Monitor Off operation and outcome stream across local IPC, JSON-RPC, REST, WebSocket, bridge forwarding, TUI/WebUI clients, board-originated requests, and authenticated peer instances without bypassing the shared host action and policy dispatcher.',
      'Keep typed canonical operations distinct from the opaque #184 escape hatch: an old bridge forwards bounded unknown opcode IDs/payloads and subscriptions without requiring a new registry, product API version, or feature-specific decoder.',
    ], 'Removal of /api/v1, product api_version fields, and versioned WebSocket product labels is in progress. JSON-RPC 2.0 remains as a standards-required wire marker. The same pass repairs dispatcher concurrency and extends typed display/buzzer events across every transport. A transport-parity Monitor Off operation and outcome stream remain unimplemented.'),
  requirement('network-bridge-discovery', 8, 'Connect, mirror, and synchronize multiple local and remote boards', 'open',
    ['🖥️ host', '🔌 protocol-api', '🛡️ safety', '🧪 testing', '🌐 networking', '🔒 security', '⚡ priority: high', '💡 enhancement'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Introduce a role-neutral board-session registry so one primary may own multiple local serial boards and authenticated remote bridge-backed boards while retaining exactly one owner per physical device.',
      'Namespace snapshots, events, commands, history, programming state, and errors by stable board ID; require an explicit target when more than one board is available.',
      'Support named leader/follower and bidirectional mirror groups using validated semantic intents, coordinator epochs, monotonic sequence, operation/trace IDs, bounded deduplication, hop limits, and per-target acknowledgements rather than raw frame copying or wall-clock conflict resolution.',
      'Mirror only explicitly allowlisted safe domains; never implicitly fan out programming, EEPROM, reset, RF learning, calibration, raw #184 opcodes, host OS actions, scripts, or motion.',
      'Authenticate and obtain one fresh baseline on enrollment/reconnect, then converge through pushed events; use refresh only for explicit requests, gap recovery, coordinator restart, or bounded legacy fallback.',
      'Expose fleet/group targeting, health, divergence, topology, and policy through CLI, TUI, WebUI, IPC, APIs, bridges, generated contracts, and privacy-safe discovery metadata.',
      'Keep focused bounded mDNS/SSDP board/application/WebUI advertisement and live multicast acceptance in #186 rather than overloading the mirroring implementation.',
      'Validate two local physical boards and at least one authenticated remote physical board, including echo prevention, partial failure, reconnect, denied capability, safe-off, and rollback evidence; all programming remains bridge-owned.',
    ], 'Correlated host-to-host calls, typed event forwarding, discovery, one-primary IPC delegation, and in-process two-host tests exist. The board boundary, configuration, snapshots, public client, and host runtime remain centered on one board, so a multi-board registry, mirror coordinator, desired-versus-actual convergence, per-target acknowledgements, and required physical topology proof remain unimplemented. Focused discovery completion is separately tracked in #186.'),
  requirement('network-artifact-import-export-sync', 8, 'Serve, fetch, import, export, and synchronize controller artifacts between hosts', 'open',
    ['🖥️ host', '🔌 protocol-api', '🌐 networking', '🔒 security', '💾 storage', '🚀 programming', '💡 enhancement', '🚧 in progress'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'On explicit request, let the primary serial owner capture and then serve verified current board flash and EEPROM; also serve any validated backup, staged firmware/bootloader/default image, and the exact running host executable by immutable SHA-256.',
      'Provide authenticated GET/HEAD byte-range downloads, portable manifests, CLI/TUI/WebUI export, and content-addressed deduplication without publishing local paths, credentials, incomplete captures, or mutable files.',
      'Fetch the same artifact classes from an authenticated remote Controller instance, validate type, bounds, digest, build identity, and target compatibility, then stage them inertly before any separately authorized import, restore, programming, or host update.',
      'Synchronize selected artifact inventories and updates between peers with idempotent resumable progress, conflict reporting, audit events, and the existing single-owner programming/self-update transaction lanes.',
      'Export and import a complete portable project bundle covering allow-listed source, configuration templates, manifests, documentation, and reusable assets while excluding secrets, private audit data, machine identities, caches, generated binaries unless explicitly selected, and unsafe archive entries.',
      'Expose all import/export/sync operations over authenticated local IPC and network API/REST/WebSocket paths with least-privilege read, artifact-transfer, programming, and host-update capabilities kept distinct.',
    ], 'The current artifact store already serves ranged content-addressed firmware, EEPROM, flash readbacks, and host executables; can explicitly capture current flash/EEPROM; publishes a peer manifest; fetches/stages verified remote artifacts; and routes guarded board/host updates through one transaction lane. Arbitrary validated backup inventory export, bootloader/project-source bundles, peer inventory synchronization/conflict handling, resumable transfer, and complete cross-instance acceptance remain open.'),
  requirement('http-webhooks-socketio-messages', 8, 'Add bidirectional HTTP, webhooks, WebSocket client/server, Socket.IO, and actionable messages', 'open',
    ['🔌 protocol-api', '🌐 networking', '🔒 security', '💡 enhancement', '🚧 in progress'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Provide inbound HTTP and configurable outbound GET/POST/PUT/PATCH/DELETE webhooks.',
      'Support WebSocket client and server roles and genuine Socket.IO compatibility as a separate protocol.',
      'Carry a typed source-tagged text envelope among local clients, servers, bridges, the board, and LCD.',
      'Authenticate, authorize, audit, and safety-check every actionable message.',
      'Deliver outbound webhooks through a bounded durable queue with attempt IDs, target timeouts, exponential backoff with jitter and Retry-After, idempotency/deduplication, shutdown drain/recovery, and explicit dead-letter inspection and replay.',
      'Use lossless JSON encoding and optional receiver-verifiable HMAC timestamp/nonce signatures; event text, quotes, and newlines must never corrupt a configured JSON payload.',
    ], 'Inbound HTTP, all requested outbound methods, standard WebSocket client/server roles, genuine Engine.IO-v4/Socket.IO, typed actionable messages, masking, correlation, bridge forwarding, and loopback delivery tests exist. Inbound hooks now discard credential-shaped query/header metadata, caller-reserved provenance, cookies, referrers, signatures, and raw RequestURI values before durable publication. Outbound delivery uses an atomically persisted bounded queue with restart recovery, stable idempotency and per-attempt identities, timeout/backoff/jitter/Retry-After, deduplication, dead-letter inspection/replay/clear, shutdown drain, lossless JSON templating, optional timestamp/nonce HMAC, secret-free durable state, and redirect rejection. Packaged/live receiver commissioning remains open.'),
  requirement('remote-control-security', 8, 'Define security and policy gates for every remote and disruptive control path', 'open',
    ['🔌 protocol-api', '🌐 networking', '🔒 security', '🛡️ safety', '🔥 priority: critical', '🚧 in progress'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Authenticate remote commands, subscriptions, toast actions, messages, bridges, and network APIs.',
      'Authorize operations by capability and route board commands through the same motion/programming safety guards.',
      'Keep disruptive OS actions and key injection disabled by default with explicit policy and confirmation.',
      'Classify Monitor Off as a disruptive OS action: default-deny it for board, peer, bridge, browser, IPC/API/WebSocket, automation, and TUI sources until the named principal has the explicit capability and any configured confirmation requirement is satisfied.',
      'Protect secrets and publish only non-secret discovery metadata with a durable audit trail.',
      'Never place a long-lived secret in a URL, browser history, access log, or diagnostic payload; establish WebSocket browser sessions through one-time or short-lived tickets or an equivalent header-safe authenticated handshake.',
      'Store durable integration secrets through operating-system-backed secret references where available and redact them from configuration, snapshots, logs, diagnostics, and exports.',
      'Use one named-principal and capability-decision model across HTTP, WebSocket, Socket.IO, local IPC, and host bridges; audit principal, transport, origin, capability, decision, and correlation identity while preserving an explicit missing-Origin policy and sending no pre-auth application frames.',
    ], 'Transport-assigned provenance, token authentication, file-watched capability authorization, default read/event-only access, default-denied mutation/programming/OS/bridge capabilities, and policy events are source-tested. Browser WebSocket and Socket.IO sessions use a 15-second, one-use, Origin/peer/transport-bound ticket in Sec-WebSocket-Protocol; upgrade URLs reject credentials, only ticket digests are retained, and concurrent replay has one winner. Inbound webhook events discard credential-shaped query/header metadata, caller-reserved provenance, cookies, referrers, signatures, and raw RequestURI values. Monitor Off still needs an explicit capability, confirmation, provenance, and audit test matrix across every originating surface. Operating-system-backed durable secret storage, a fully unified principal/correlation model across raw IPC and every bridge, and live adversarial network acceptance remain open.'),

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
  requirement('primary-serial-owner-ipc', 9, 'Enforce one serial owner and route secondary processes through IPC', 'open',
    ['🖥️ host', '🛡️ safety', '🔌 protocol-api', '🚧 in progress', '⚡ priority: high'], 'Host application, TUI, configuration, shell, IPC, and library', [
      'Let one long-running host own the serial port.',
      'Route secondary CLI, TUI, monitor, reset, shell, and programmer commands through correlated IPC.',
      'Fan board/USB events out without opening a second COM handle.',
      'Prove secondary commands against the authenticated primary owner.',
      'Have every instance automatically choose direct-primary or delegated-IPC operation, including firmware upload; stream correlated backup/program/verify/reconnect progress and final outcome to the requesting secondary instance without opening COM again.',
      'Keep program recover HEX [PORT] primary-owned and secondary-delegated: treat PORT only as an exact-device assertion, perform a read-only Urboot semantic verification of the already-written image, reconnect the same authenticated device, and never open a second serial handle or rewrite flash.',
      'When a local COM open fails as busy/access-denied, identify the owning process, PID, executable, and window where available; present a human-readable diagnosis plus explicit guarded actions to foreground it, request graceful close, or terminate without ever killing the current/primary controller process.',
      'Resolve Windows port ownership through a target-scoped, cancellable Restart Manager or equivalent query; do not repeatedly allocate a machine-wide NT handle snapshot or abandon a non-cancellable worker.',
    ], 'Secondary ordinary commands are verified through the running primary. A live primary-owned program recover operation pinned the COM18 device by Instance ID, read-only verified 32,228 programmed bytes plus reset target 0x7E80 and vector 25 target 0x024E, reauthenticated board identity B3F4CB11, and cleared the durable recovery marker without rewriting flash. Guarded TUI foreground/graceful-close/double-confirmed terminate actions are source-tested, but owner diagnosis still uses a bounded global handle-table snapshot. Target-scoped Restart Manager resolution and a full delegated upload remain open.'),
  requirement('controller-discovery-authority', 9, 'Make controller-owned discovery authoritative and explain platform inventory drift', 'closed',
    ['🖥️ host', '🌐 networking', '🐛 regression', '✅ verified'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Use the shipped controller executable as the authority for development and deployment discovery.',
      'Reproduce why a secondary system inventory saw only COM1 while Controller discovery found COM18 and COM19 through its native Windows device path.',
      'Document the actual discovery APIs, filters, identity data, and environmental cause of the difference.',
      'Add regression coverage and never block programming solely on a secondary inventory result.',
    ], 'Reproduced on the development host: legacy Win32_SerialPort returned only COM1 while the present-device SetupAPI Ports-class path returned both CH340 adapters on COM18 and COM19. Native enumeration, identity enrichment, HELLO authority, regression coverage, and the provider-coverage explanation are complete.'),
  requirement('serial-lifecycle-contract', 9, 'Keep the serial protocol connected independently of telemetry subscriptions', 'open',
    ['🖥️ host', '🔌 protocol-api', '🛡️ safety', '🧪 testing', '🚧 in progress'], 'IPC, WebSocket, USB lifecycle, and primary ownership', [
      'Keep application UART enabled, open, and auto-reconnecting by default even with no measurement subscribers.',
      'Use changed-only pushed state/events whenever the board advertises support; polling is a bounded legacy fallback or is used for initial sync, explicit refresh, and sequence-gap recovery only, and subscription accounting never suppresses asynchronous events or closes serial.',
      'Make explicit Close pause reconnect until Open resumes it.',
      'Expose open, close, reconnect, reset, quit/exit, and all commands through IPC/WebSocket with correlated results.',
    ], 'Raw-socket and deterministic VirtualBoard tests prove UART ownership remains independent from demand accounting and unsolicited events continue without a polling subscriber. Legacy STATUS demand can be stopped without closing transport; capable changed-state paths must now satisfy the stricter push-first #36/#190 contract. Explicit Close/Open lifecycle results remain correlated. Physical USB unplug/replug and event-driven Windows reconnect acceptance remain open.'),

  requirement('uart-urclock-programming', 10, 'Use UART Urboot/Urclock as the normal programming path and verify application return', 'closed',
    ['🚀 programming', '🛡️ safety', '✅ verified'], 'Bootloader, programming, build scripts, and packaging', [
      'Configure current MiniCore Urboot/Urclock for the ATmega328P board and preserve EEPROM as selected.',
      'Release serial ownership, run maintained Arduino CLI/AVRDUDE urclock operations, and reauthenticate application HELLO.',
      'Support probe, metadata, read, write, verify, and start without pretending the native application protocol is the bootloader protocol.',
      'Keep USBasp as an explicit troubleshooting fallback only.',
    ], 'Urboot/fuses were ISP-verified and current firmware was UART-uploaded, flash-verified, and reauthenticated; host commands delegate to the maintained backend. A fresh primary-owned recovery also completed a read-only Urboot semantic verification of all 32,228 programmed bytes and critical reset/vector targets before returning to the exact authenticated application, without rewriting flash.'),
  requirement('urboot-custom-progress-backend', 10, 'Maintain a reproducible Urboot-Custom progress-hook patch and safe ISP install plan', 'open',
    ['🧩 firmware', '🚀 programming', '🏗️ tooling-build', '🧪 testing', '🔍 needs-hardware'], 'Bootloader, programming, build scripts, and packaging', [
      'Name the extensible fork Urboot-Custom and keep the core as an upstream-applicable diff with a generic optional progress hook; isolate TM1637 or future peripheral implementations as selectable backends.',
      'Pin upstream Urboot source and hashes, reproduce the installed stock no-LED and PB5-LED images byte-for-byte with their matching AVR GCC/binutils, and fail before trusting a custom image on any mismatch.',
      'Generate address/metadata/RJMPWP/size/hash assertions and a feature matrix that reports the exact bytes gained and capability lost for every optional Urboot removal; select no removal without user-approved tradeoff.',
      'Enforce the reduced application ceiling, construct a vector-aware merged application-plus-bootloader image, and never use a generic chip-erase bootloader-only write that can erase page zero.',
      'Require read-only signature/fuse/lock/flash/EEPROM capture before the first ISP write, verify readback, then prove subsequent UART/Urclock progress plus normal application return on hardware.',
    ], 'The u8.0 patch-based prototype reproduces both stock MiniCore references exactly with GCC 7.3.0/binutils 2.26.20160125 and builds a validated 510-byte image in a 512-byte region; the generic hook, TM1637 backend, manifest, diff, feature-loss matrix, and bootstrap exist. It is deliberately not installed: first installation needs the agreed USBasp backup/attention sequence and vector-aware verified merged write.'),
  requirement('preflash-backup-dedup-restore', 10, 'Require atomic flash/EEPROM backup, hash deduplication, and verified restore before writes', 'open',
    ['🚀 programming', '💾 storage', '🛡️ safety', '🔥 priority: critical'], 'Development EEPROM, repository, licensing, and documentation', [
      'Before any flash write, read flash and EEPROM through Urclock into the host data directory.',
      'Store flash blobs by SHA-256, reference hashes in names/manifests, and never duplicate identical firmware.',
      'Block a write after failed backup unless an explicit logged override is provided.',
      'Mark partial reads incomplete, retain raw logs, and verify restore/readback.',
      'Use explicit --method usbasp recovery only when UART cannot work.',
      'Before releasing the application UART, snapshot the board identity and MCU settings separately from PC configuration, show Prog on TM1637 plus Programming.../Do not disconnect on LCD, and temporarily enable Silent only when the board was audible.',
      'Before latching programming mode, capture live relay/PWM/settings and host visual state, cancel any macro, release every relay, smoothly ramp enclosure and user MOSFET outputs to zero, apply the programming RGB cue, show Prog, and play a PC-streamed power-down melody to completion.',
      'Persist the programming bit across every intermediate reboot so Prog, Silent, zero outputs, and safe relays remain authoritative until the complete host transaction finishes.',
      'After authenticated application return, restore the exact original MCU settings/audible state, wait through deferred EEPROM persistence, compare readback, and recover unfinished lifecycle markers after a host crash.',
      'Restore the captured live PWM/relay/motion/RGB state through canonical safe controllers only after write, verify, reconnect, and HELLO succeed; explicitly report canceled macro playback position as non-restorable.',
      'When ISP is genuinely required, first force safe outputs, retain flash/EEPROM/fuse/lock backups, show WAIT/Connect USBasp with the agreed ringtone/LED attention cue, and make the first ISP operation read-only.',
      'For a failed transaction whose intended image is already present, provide program recover HEX [PORT]: require matching artifact and authenticated-device evidence, reassert the safe programming state, verify through read-only Urboot semantics, reconnect that exact device, restore or development-reinitialize MCU settings, and retain the recovery marker on every failure.',
    ], 'Content-addressed backup/manifests and a centralized crash-recoverable programming lifecycle with display, temporary-silence, independent MCU-settings snapshot, delayed restore, and readback tests are source-complete. A live failed transaction was completed with program recover against the Instance-ID-pinned COM18 device: the deployed image was not rewritten, 32,228 programmed bytes and critical reset/vector semantics were freshly verified, development EEPROM defaults were reinitialized, the B3F4CB11 application identity returned, and the marker cleared only after success. A deliberately interrupted ordinary transaction, an independent content-deduplication/restore exercise, and the ISP fallback remain live acceptance gaps.'),
  requirement('canonical-host-programming-entrypoint', 10, 'Route every build, upload, verify, backup, and recovery through the host tool', 'open',
    ['🧩 firmware', '🖥️ host', '🔌 protocol-api', '🚀 programming', '🛡️ safety', '🧪 testing', '🏗️ tooling-build', '⚡ priority: high'], 'Development EEPROM, repository, licensing, and documentation', [
      'Make the canonical controller executable the normal entry point for compile/upload/verify/backup/recovery.',
      'Keep platform wrappers thin and route Node/root tooling through the guarded host command plan.',
      'Reject stale binaries and mismatched command contracts using embedded source identity.',
      'Provide hardware-free plan tests and a live UART programming verification.',
      'Make the Build screen explain and expose dependency/profile resolution, source/artifact identity, compile/package progress, safe backup, reviewed upload, verify, restore, release discovery/download, and delegated-operation monitoring.',
      'Automatically delegate a secondary instance to the primary serial owner and preserve the same typed progress/result stream over local IPC or an authenticated remote bridge.',
      'Expose program recover HEX [PORT] through the same primary-owned command surface for matching failed transactions; it must perform read-only Urboot verification and exact-device reconnect rather than bypassing the host lifecycle or rewriting flash.',
      'Import firmware/EEPROM/host artifacts from a local file selector, HTTP manifest, release provider, or peer; serve immutable current/backed-up flash, EEPROM, firmware, and host artifacts with ranges, hashes, and portable metadata for browser download and fleet diagnostics.',
      'Optionally embed a validated default firmware plus EEPROM pair in a host release for explicit first-board recovery; disable that offer when either image is absent, never auto-program a merely older image, and require the same reviewed authorization and backup transaction.',
      'Support crash-safe host self-update plus remote firmware staging without conflating download with device programming; compare content hash and packed build time, deduplicate equal bytes, and publish progress through TUI, WebUI, IPC, HTTP, WebSocket, Socket.IO, and bridge clients.',
      'Require every AVR operation and Go host replacement to pass through the running bridge coordinator; if no coordinator is available, launch or restore one visibly before proceeding, and never let wrappers bypass it with direct programmer or executable replacement calls.',
      'Before replacing Go, request graceful exit through IPC, let secondary instances consume the pushed staged/update event, publish real progress to every surface, and navigate matching visible instances to Programming/Updates.',
    ], 'Controller-owned compile/program plans, the shared Node/CMD/Bash wrappers, and guarded host programming now converge on one policy implementation in source. The primary-owned program recover command has passed a live read-only Urboot verification, exact-device reconnect, settings reinitialization, and durable-marker completion without a direct serial/programmer bypass. A final source-identified packaged executable, a complete delegated upload lifecycle pass, and removal/rejection of every stale shadow artifact remain open.'),
  requirement('hex-patch-settings-export', 10, 'Finish guarded Intel HEX patching and separate live settings export from EEPROM parsing', 'open',
    ['🚀 programming', '💾 storage', '🛡️ safety', '🚧 in progress'], 'Development EEPROM, repository, licensing, and documentation', [
      'Inspect named application, bootloader, EEPROM, and metadata regions with checksum/address/bounds validation.',
      'Patch only declared regions, retain the original, show before/after SHA-256, and require verify/readback.',
      'Keep live native-protocol settings export separate from offline EEPROM-image parsing.',
      'Preserve unknown bytes and identify the supported layout/hash without mixing MCU state into host config.',
    ], 'Strict manifest inspection now validates the container digest, Intel HEX checksums/ranges/bounds, application, bootloader, EEPROM, and every declared metadata region with independent hashes. Live settings export remains separate from offline EEPROM parsing and imports preserve unknown bytes. Every programmer write rejects verification bypass and performs an independent second readback/byte comparison in injected offline tests. A live read-only Urboot semantic verification has now covered 32,228 programmed bytes and the critical reset/vector targets; an authorized declared-region patch with before/after readback plus final live-settings and offline-EEPROM export acceptance remains open.'),
  requirement('graceful-host-snapshot', 10, 'Write an atomic diagnostic board snapshot on graceful host exit', 'open',
    ['🖥️ host', '💾 storage', '🚀 programming', '💡 enhancement'], 'Development EEPROM, repository, licensing, and documentation', [
      'Atomically store board identity, last status/settings/menu, connection/reset metadata, active programming operation, and artifact hashes.',
      'Keep this diagnostic host data separate from EEPROM mirrors and configuration.',
      'Never present the snapshot as proof that an interrupted write completed.',
      'Use the snapshot as a safe input to future migration and recovery diagnostics.',
    ], 'Schema-2 last-session.json atomically records cached identity, connection, status/reset/settings/menu/front-panel state, recent typed events, active/latest programming operation, current/default artifact hashes, and privacy-safe durable recovery marker/session hashes without mixing host config or EEPROM bytes. Validated recovery diagnostics reject interrupted-write completion claims. Source tests cover replacement, corruption, partial completeness, deduplication, RPC, and marker consumption; a graceful exit during/after the live programming lifecycle remains open.'),

  requirement('arduino-go-dependencies', 11, 'Provision managed firmware and host toolchains plus globally discoverable UPX', 'open',
    ['🏗️ tooling-build', '🔒 security', '📦 dependencies', '🐛 regression', '🚧 in progress'], 'Firmware toolchain and dependencies', [
      'Audit and update installed Arduino cores and requested well-supported libraries through the configured network path.',
      'Declare all Go host dependencies and package checksums.',
      'Use fixed-size local AVR drivers where needed to fit the target without misrepresenting linked libraries.',
      'Install UPX globally on PATH without hard-coding an extraction directory.',
      'Bootstrap a clean machine from a resolved public profile with a SHA-verified dependency CLI, board core/compiler, libraries, caches, manifests, and compile/program prerequisites under project-owned data paths.',
      'Inherit HTTP_PROXY, HTTPS_PROXY, ALL_PROXY, and NO_PROXY case-insensitively into every dependency subprocess without logging secrets, with a bounded direct retry only when the configured proxy cannot reach the source.',
      'On Windows, provision the latest compatible native MinGW-w64/Windows-GNU compiler for Go c-shared packaging when absent, forward the configured proxy, and reject Git/MSYS/Cygwin gcc false positives by target and preprocessor identity.',
      'Expose generic public toolchain bootstrap/sync/profile/compile/core-info/install-bootloader commands while retaining dependency-specific names only internally or when invoking the dependency itself.',
      'On Linux, a privileged fresh-host bootstrap must assign the managed profile to an explicit non-root interactive/service account, install reviewed native packages and existing-group serial access declaratively, and leave reusable least-privilege state without a manual chown, direct dependency-CLI call, or broad udev rule.',
    ], 'Current core/library/Go dependency versions are recorded as the verified bootstrap baseline, UPX 5.2.0 is globally discoverable without a hard-coded source path, and the isolated managed profile downloads/verifies its CLI, installs MiniCore 3.1.2 plus requested libraries, inherits proxy semantics, inventories dependencies, and completes a Controller compile. The Windows host packager validates target/macros, rejects the Cygwin/MSYS gcc shadowing PATH, provisions/replays the resolved WinLibs package, links the real Go c-shared DLL/header, and passes an external-C ABI call. On an audited Ubuntu 26.04 host, Controller exposes a default-read-only provision-host path with APT simulation, trusted privileged command discovery, secret-safe proxy forwarding, reviewed packages, distribution UPX verification, existing dialout/uucp assignment, and nested target-user bootstrap. Live apply/reuse left managed files owned by the target account and completed firmware compilation through its persisted managed profile. This remains open until branch checks and post-merge fresh-host validation confirm the complete Go-owned path.'),
  requirement('latest-toolchain-update-automation', 11, 'Automate latest-compatible dependency updates with resolved-lock reproducibility', 'open',
    ['🏗️ tooling-build', '📦 dependencies', '🧪 testing', '💡 enhancement'], 'Firmware toolchain and dependencies', [
      'Resolve the latest compatible dependency CLI/core/libraries/Urboot, Go modules/toolchain, Node/npm packages, GitHub Actions, UPX, and go-winres instead of treating policy-file versions as permanent pins.',
      'Resolve the native Windows MinGW-w64/Windows-GNU package used for Go c-shared builds, record its package/version/target/provenance in the generated lock and host manifest, and replay that lock while update mode still proposes the latest compatible compiler.',
      'Generate an auditable resolved lock/manifest containing exact versions, sources, integrity hashes, compatibility decisions, and toolchain provenance; reproducible builds consume that lock.',
      'Provide scheduled and manual update discovery that opens a reviewed pull request with release notes, license/security/size impact, regenerated locks, and focused plus full validation.',
      'Pin every third-party GitHub Action to an immutable commit with a readable major-version comment while retaining automated update proposals.',
      'Keep update, bootstrap, compile, upload, and CI output polished and consistent across CMD/Bash/host tooling with VT-100 color, Unicode/emoji fallbacks, centered aligned tables, and secret-safe proxy diagnostics.',
      'Use Chalk for Node terminal styling and a maintained Unicode table-layout library for drawing, alignment, padding, and centered headers rather than hand-built table spacing.',
      'Test resolver determinism, no-update/idempotent behavior, partial/network failures, proxy/direct fallback, lock replay, and PR-plan generation; validate the real workflow in GitHub Actions before closing.',
    ], 'Latest-stable resolution now covers the firmware CLI, MiniCore, six libraries, Urboot, Go/toolchain modules, Node/npm, UPX, go-winres, native WinLibs, and immutable GitHub Actions. Exact replay locks, compiler package/version/target/manifest/archive provenance plus selected-binary host-manifest identity, u8.0.1 patch/source/image assertions, scheduled/manual validated PR automation, npm audit evidence, deterministic release/license/security/size PR plans, bounded proxy/direct fallback, Chalk plus cli-table3 presentation, and idempotent no-change resolution pass. Focused dependency tests pass 22/22, build tests pass 40/40, and validate-host reports the exact Windows GCC 16.1.0-14.0.0-r3 identity. The requirement remains open only until a real hosted Actions run proves artifact publication and PR/blocked-issue lifecycle.'),
  requirement('project-import-structure', 11, 'Preserve reusable project layers, merge LocalLib variants, and consolidate source/tool directories', 'closed',
    ['🏗️ tooling-build', '🧩 firmware', '🖥️ host', '✅ verified'], 'Project import, LocalLib merge, and structure', [
      'Preserve the reusable hardware/project layer without carrying application-specific business rules.',
      'Keep only the reusable LocalLib components that the current firmware imports and document their public contracts.',
      'Maintain a privacy-safe comparison of the reviewed LocalLib variants, their meaningful differences, the parts selected, and the application-specific behavior deliberately excluded.',
      'Retain reusable debounce/hold, nonblocking feedback, relay/event, delayed persistence, watchdog, and RF-repeat semantics while keeping board-specific values and the native protocol in current project-owned layers.',
      'Keep root LocalLib/Project aggregation exactly once and use canonical Tools/Controller, Tools/Firmware, and Tools/VirtualBoard locations.',
      'Remove stale duplicate host/tool directories and align root documentation/scripts to canonical paths.',
    ], 'Project/state layers are consolidated and current build references use canonical tool locations. The maintained Local Library Variant Comparison records the reviewed differences and selection without publishing unrelated project names or paths. Reusable behavior is represented by the nonblocking key/RF hold state machines, Timer1 feedback, native relay events/opcodes, watchdog service, and deferred CRC-backed EEPROM writes.'),
  requirement('authorized-reusable-component-porting', 11, 'Directly port all applicable generalized components from authorized sibling applications', 'open',
    ['🏗️ tooling-build', '🖥️ host', '🧪 testing', '✨ ux', '📦 dependencies', '📚 documentation', '🔒 security', '💡 enhancement'], 'Project import, LocalLib merge, and structure', [
      'Inventory every system-level, framework-level, infrastructure, and otherwise generalized component in the owner-authorized sibling applications and maintain a privacy-safe applicability/provenance matrix.',
      'When a component applies to Controller, import its implementation directly rather than merely imitating its appearance or rewriting it from inspiration; preserve source attribution, license terms, notices, meaningful history, and behavior-specific tests.',
      'Cover applicable loaders, keyboard/global-hotkey handling, native OS integrations, service/tray/update infrastructure, audio cues, button/dialog/data presentation, accessibility, animations/transitions, and operational quality features.',
      'Adapt imported code behind Controller interfaces, configuration, authentication, safety policy, localization, and platform boundaries without copying unrelated business logic, secrets, machine data, or obsolete compatibility baggage.',
      'For every component not imported, record a concrete not-applicable, superseded, unsafe, incompatible-license, or lower-quality rationale; absence of review is not an acceptable rationale.',
      'Run focused parity tests plus the full repository gates and keep third-party/authorized-source attribution current whenever a direct port changes.',
      'Complete #193 as the named Rayan Lamp and Patris-export source-level inventory, direct-port, before/after parity, enhancement, license/attribution, and explicit-exclusion record; general references or visual imitation do not satisfy it.',
    ], 'Selected reusable concepts and assets have already entered the host and embedded WebUI, but there is no exhaustive source-level applicability/provenance matrix proving that every relevant generalized component was directly ported or explicitly rejected. The previous work therefore does not satisfy this stronger direct-import requirement.'),
  requirement('native-virtual-board', 11, 'Provide a desktop virtual board for fast native protocol and behavior tests', 'closed',
    ['🏗️ tooling-build', '🧪 testing', '🔌 protocol-api', '✅ verified'], 'Native virtual board', [
      'Build a C++17/CMake virtual board with desktop GCC-compatible tooling.',
      'Model settings, independent virtual EEPROM, sensors, inputs, RF, outputs, displays, macros, strip, and resets.',
      'Speak the native protocol over TCP and support interactive injection.',
      'Pass native unit, raw protocol, and host fragmented-transport tests.',
    ], 'The simulator builds and its tests/smokes pass, including full status shape and reset journal behavior; cycle-accurate shared AVR translation was not required for this completed behavioral scope.'),
  requirement('canonical-cross-language-contracts', 11, 'Generate protocol, settings, hardware, and capability definitions from canonical contracts', 'open',
    ['🧩 firmware', '🖥️ host', '🔌 protocol-api', '💾 storage', '🧪 testing', '🏗️ tooling-build', '⏳ finalization'], 'Project import, LocalLib merge, and structure', [
      'Keep controllers general-purpose by separating reusable contracts and mechanisms from board-profile wiring, product presentation, and application-specific policy.',
      'Define one machine-readable source for opcodes, payloads, capabilities, errors, events, settings fields, EEPROM ownership, hardware pins/addresses, menu/action/page identities, and feature/profile availability.',
      'Generate AVR C++, VirtualBoard C++, Go, TypeScript, test vectors, and maintained reference tables from that source; never maintain competing handwritten constants for the same contract.',
      'Represent alpha feature/profile differences as explicit capabilities rather than adding version-to-version migration or compatibility baggage before layouts freeze.',
      'Share one firmware behavior core behind AVR and VirtualBoard hardware-abstraction layers, while keeping target-only physical drivers isolated.',
      'Add deterministic generation, golden wire/storage vectors, and a check mode that fails CI on duplicate IDs, manually edited outputs, undocumented divergence, or generated-document drift.',
    ], 'Confirmed drift exists today: the Go and VirtualBoard opcode catalogs contain 0x42–0x44 and 0x9A–0x9B entries absent from the AVR UART header. EEPROM/settings semantics, pins/addresses/defaults, VirtualBoard behavior, UI capability metadata, and generated documentation also have multiple hand-maintained sources. The recovery work must converge these definitions rather than creating another parallel controller or authentication implementation.'),
  requirement('tooling-entrypoint-consolidation', 11, 'Unify build and programmer policy behind one command-plan implementation', 'open',
    ['🏗️ tooling-build', '🚀 programming', '🐛 regression', '⚡ priority: high'], 'Tooling entry-point consolidation audit', [
      'Move board profile and build/programming policy out of divergent PowerShell, Bash, Node, and Go implementations.',
      'Keep public development/deployment paths PowerShell-free: canonical Controller/Node implementations own behavior while CMD and Bash remain thin launchers.',
      'Keep CMD/Bash/platform launchers thin and generate/test equivalent plans, help, failures, artifacts, and USBasp method selection.',
      'Use CMake presets for the virtual board rather than duplicated platform pipelines.',
      'Use the project controller tool for development/deployment discovery and programming.',
      'Compile and run Go test executables from stable project-owned paths; ordinary tests must not bind wildcard interfaces or create changing temporary ipcjson executables that repeatedly trigger Windows Firewall prompts.',
    ], 'The canonical toolchain profile now owns FQBN plus board identity and memory geometry; generated Go constants and the shared Node command-policy module consume it. Build planning, real execution, and the firmware studio use the same Controller argv/artifact builder for compile, Urclock, and explicit USBasp routes. Both CMD/Bash launcher pairs emit equivalent JSON plans with canonical target and artifact paths, VirtualBoard CI uses CMake presets, and stable-path Go test execution avoids randomized firewall identities. The remaining acceptance boundary is a final packaged/hardware lifecycle pass and deciding whether non-command host packaging stages should become a separately replayable serialized plan.'),
  requirement('canonical-host-artifact-packaging', 11, 'Produce one current source-identified controller artifact with verified packaging', 'open',
    ['🏗️ tooling-build', '🖥️ host', '🐛 regression', '⚡ priority: high', '🚧 in progress'], 'Tooling entry-point consolidation audit', [
      'Choose one generated controller executable location and make every launcher resolve exactly it.',
      'Remove or reject stale shadow copies and embed a verifiable source hash in release and development builds.',
      'Stamp accurate Windows resources, explicitly apply the packaged APP icon to attached classic conhost windows, collect notices, compress with UPX, and verify hashes/version/icon metadata.',
      'Generate the executable icon, WebUI logo/favicon, and documentation SVG marks from one canonical vector/logo source; reject broken or visually divergent copies and verify representative rendered outputs.',
      'Complete #194 with a GitHub-attached before/current/candidate comparison across favicon, WebUI, executable, toast/tray, console, installer, and docs at representative sizes/backgrounds, retaining user-selection evidence and deterministic render-diff enforcement.',
      'Derive mutable user-facing product titles, taglines, and branding from canonical package/product metadata or watched host configuration rather than hard-coding them; support documented build defaults, package metadata, watched JSON/YAML/TOML configuration, environment overrides, and runtime flags, with runtime flags highest priority and APP_NAME overriding config.',
      'Clean reusable host jobs before downloading the exact same-run firmware artifact; embed its validated application plus complete 1 KiB safe-default EEPROM and assert both independent host-manifest flags.',
      'Rebuild DLL/header and repeat an external caller smoke test for the final source.',
      'Produce a polished per-user installer/uninstaller with a signed or hash-bound package inventory, assisted repair, preserve-configuration/data by default, and an explicit separately confirmed purge path.',
      'Keep install, uninstall, repair, URI/AUMID/shortcut registration, and desktop resources behind direct native adapters with explicit unsupported-platform behavior and no runtime PowerShell dependency.',
    ], 'Canonical product metadata drives mutable host/WebUI/resource/document titles while stable technical identifiers remain fixed. Package metadata plus build switches/environment now seed one Go/WebUI application-name and first-run-tagline default beneath watched config, APP_NAME/APP_TAGLINE, and runtime flags. The packaged executable contains the seven-size APP icon and the host explicitly applies that named resource to classic conhost windows instead of relying on inconsistent inherited/default icon selection. Documentation SVG logo copies now use the canonical executable/WebUI geometry; representative rendered/package observation and single-source render-diff enforcement remain open. The current native Windows c-shared DLL/header build and a stable external-C ports-ABI smoke pass record compiler provenance and binary integrity. Final canonical executable/DLL packaging, exact resource/UPX/embedded-default identity, stale-shadow rejection, polished installer/repair/uninstall inventory, direct native desktop registration, and packaged console-icon observation across launch paths remain open.'),

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
      'Make the repository README an inspiring present-tense product landing page with the canonical brand palette, accessible SVG art, purposeful badges/pills, emoji accents, quick-start navigation, architecture, capabilities, safety, and documentation links.',
      'Remove origin stories, external-project/repository names, unrelated hardware/application references, stale development images/checkpoints, and obsolete implementation notes from maintained documentation and user-facing source text.',
      'Cover architecture, hardware, firmware, host/TUI, protocol/API/RPC/WebSocket, configuration, build/upload/Urclock, simulation, troubleshooting, safety, and licensing.',
      'Keep operating instructions separate from evidence/status while maintaining the checklist as acceptance truth.',
      'Explain exact feature/size tradeoffs and distinguish source proof, automated tests, and live hardware proof.',
      'Include a board-menu versus host-menu ownership catalog for every page, with build-identified measured or clearly labelled estimated flash/SRAM deltas, lost offline behavior, protocol cost, feature gains, and user-selectable recommendations.',
      'Catalog initialization for INA219, PWM/PCA9685, DS18B20/OneWire, TM1637, shift registers, 433 MHz RF, LCD/I2C, Timer1 buzzer, relays, WS2811/WS2812/status RGB, UART, and Urboot/Urclock.',
      'For each peripheral record address/pins, rate/resolution/averaging/timing/polarity/calibration or pull-up parameters as applicable, whether each value is compiled, EEPROM-owned, or host-owned, why it was selected, safe alternatives, and verification evidence.',
      'Generate machine-readable OpenAPI for REST, AsyncAPI for WebSocket/events, and a JSON-RPC method/schema catalog plus an offline rendered reference; keep routes, security, idempotency, errors, examples, and intentionally unsupported transports in parity with implementation.',
      'Publish and maintain a navigable GitHub Wiki whose start page links the getting-started, initialization, host/network, requirements/status, and contribution material, and provide a plain-English remaining-work handoff for non-specialist operators.',
      'Document push-first architecture as a durable invariant: ordinary board state is never polled merely for implementation convenience; only initial sync, explicit refresh, sequence-gap recovery, and bounded visible legacy fallback are permitted, with continuous streams separated from one-shot logs.',
      'Complete #192 with generated or CI-exercised copy-paste recipes for display, RGB, raw/typed opcodes, buzzer routing, EEPROM lifecycle, and coordinated UI navigation against the current versionless contracts.',
    ], 'Several focused guides and the canonical checklist exist, and a current maintained-text scan finds no prohibited origin story, unrelated hardware/application, historical development-image, or external-project/repository references. Generated OpenAPI 3.1, AsyncAPI 3.0, and JSON-RPC catalogs include capability, idempotency, error, and unsupported-transport metadata plus a standalone offline reference; the repository gate rejects dispatcher/route-family drift. The live GitHub Wiki now provides Home, Getting Started, Board Initialization, Host/Network, Requirements/Status, Contributing, and navigation pages linked to the delivery board, and a layperson remaining-work handoff is present on the edge operator Desktop. Final naming/link coverage and post-freeze documentation review remain unfinished.'),
  requirement('final-code-documentation-gate', 12, 'Run the final concise code-comment and missing-requirement audit after layouts freeze', 'open',
    ['📚 documentation', '⏳ finalization', '🧪 testing'], 'Development EEPROM, repository, licensing, and documentation', [
      'Comment public/domain functions, state, configuration, hardware assumptions, and non-obvious safety/timing/unit constraints.',
      'Avoid comments that merely repeat syntax.',
      'Audit every normalized request against implementation and current evidence after protocol/EEPROM/flash layouts freeze.',
      'Run a final missing, regression, contradiction, and documentation review without promoting planned work to complete.',
      'Audit AVR string literals after layouts freeze and use `F()`, `PSTR`, or other PROGMEM placement wherever it saves SRAM/flash without breaking format, lifetime, or wire behavior.',
      'Run the project JSONL user-turn extractor in an independent final audit task, compare every user request against the final checklist, issue criteria, implementation, and evidence, and report every missing, drifted, contradictory, duplicate, or deliberately pending item.',
    ], 'An independent pre-freeze extraction audited all 204 user turns from the two root product discussions and found the missing embedded-WebUI requirement plus several portable acceptance drifts without publishing private text. Active layouts and behavior are not frozen, so the required final post-freeze code, comment, PROGMEM, contradiction, and coverage audit remains open.'),
  requirement('requirements-backlog-publication', 12, 'Maintain a deduplicated public requirements map and true GitHub sub-issue hierarchy', 'open',
    ['📚 documentation', '🧪 testing', '🚧 in progress'], 'Development EEPROM, repository, licensing, and documentation', [
      'Normalize all distinct checklist and audited user requirements while keeping complete transcripts and private paths local; prepend only the issue- or pull-request-relevant original excerpt, lightly grammar-corrected and publication-safe, to every applicable GitHub issue or pull request.',
      'Give every normalized item a stable requirement marker, clear acceptance criteria, evidence/gaps, labels, and evidence-based state.',
      'Give every epic, normalized requirement, supplemental issue, and active pull request at least one accurate product/engineering domain label so Kanban area views never depend on workflow or priority labels alone.',
      'Attach each requirement as a true GitHub sub-issue of exactly one epic and summarize open/closed counts on the epics.',
      'Keep a canonical repository map and an idempotent sync/validation helper.',
		'Before implementing every new task or request, reconcile it with existing GitHub issues, pull requests, requirements, and maintained documentation; update and link the existing records when scope overlaps, and create a new stable requirement only for genuinely distinct work.',
		'Update affected operating/architecture/API documentation in the same change, and link the issue, branch/commit or pull request, tests, live evidence, and remaining gaps without declaring partial work complete.',
      'Keep complete exact conversation text only in the ignored private audit cache; publish curated relevant excerpts with source session/turn annotations, redact credentials and private secrets, and create a new issue only when no existing requirement fits.',
      'Reconcile repository, issue, pull-request, project-board, Wiki, and local-checkout state so partial, in-progress, blocked, verified, and complete labels never overstate evidence.',
      'Maintain one repository-linked PCController Development project containing all 13 epics and every normalized requirement exactly once, with truthful workflow, Area, Priority, Verification metadata and practical backlog/area/hardware/completed views.',
      'Keep routine narrowly scoped fixes eligible for direct main commits, while substantial refactors and feature additions use an issue-linked branch and reviewed pull request before merge.',
      'Immediately approve and merge a pull request once it is qualifying, green, and non-WIP; mark incomplete work as draft/WIP with the exact blocker and preserve a resumable issue/PR handoff instead of waiting indefinitely for a separate approval round.',
      'Treat GitHub comments as full-duplex coordination: read new replies and review threads, acknowledge or answer them, update disposition/evidence/blockers, and carry decisions back into code, docs, issues, pull requests, the Project board, and every participating agent.',
      'Preserve original dirty worktrees, but promptly checkpoint semantically unique in-progress work to a named remote branch and issue-linked Draft/WIP pull request with immutable provenance, validation, blockers, owner, and next test; never publish superseded/generated/noise deltas.',
    ], 'The reconciled public graph now validates 129 issues and 65 pull requests with prepended publication-safe provenance, domain coverage, 13 epics, 71 normalized requirements, and true sub-issue links; the dry-run synchronizer reports zero repository/GitHub drift. The catalog now includes current supplemental issues/PRs and the distinct raw-opcode, host-owned EEPROM, discovery metadata, Control-page, UART-budget, display, command-recipe, named-source-port, and identity-visual trackers. Local issue/PR synchronization is current, but the active GitHub token lacks read:project, so the PCController Delivery project board itself could not be re-audited or updated in this pass; Project-field/view parity remains an explicit blocker rather than assumed complete. Ongoing Wiki maintenance, unique-WIP salvage, project-board reconciliation, and final implementation-state verification remain open.'),

  requirement('hardware-frontpanel-audio', 13, 'Validate final-image buttons, menus, reset stability, and audio cues on hardware', 'open',
    ['🧪 testing', '🔍 needs-hardware', '🎛️ front-panel', '🔥 priority: critical'], 'Final hardware validation and handoff', [
      'Test Buttons 1-4 Down/Up/Click, double-click, hold acceleration, and editors while monitoring reset count.',
      'Validate the nested settings fields, save/discard, default-page behavior, and no menu-navigation reset.',
      'Listen for boot melody, one clean beep per key, and save/discard/door/relay cues with Silent off.',
      'Confirm the first-run TUI remains synchronized through board ready and welcome-melody completion.',
      'Request this physical pass only after safe setup by showing WAIT and playing the unique repeating ringtone; stop the cue immediately after acknowledgement.',
    ], 'Earlier melody and buttons 1/2 evidence exists, but the final-image full key/menu/audio and synchronized first-run pass is not complete.'),
  requirement('hardware-door-bt-temperature', 13, 'Validate enclosure, BT Audio, and temperature-role transitions on hardware', 'open',
    ['🧪 testing', '🔍 needs-hardware', '🧩 firmware'], 'Final hardware validation and handoff', [
      'Toggle the reed and verify immediate open/close events, default-page return, light fade, RGB cue, and motion emergency behavior.',
      'Toggle BT Audio and verify Off/On/Blink classification plus named TUI/IPC events.',
      'Run controlled illumination on/off logging and prove tLED warms while tBT remains comparatively cool.',
      'Record the final firmware identity and reset/error counters during the pass.',
      'Request the prepared reed/BT/thermal actions through the WAIT/ringtone attention sequence and record each observation explicitly.',
    ], 'Ambient snapshots, ROM IDs, and basic event paths exist; controlled transitions and role proof remain unperformed.'),
  requirement('hardware-pwm-displays-lighting', 13, 'Visually validate TM1637, PWM, enclosure fade, power/RGB, and D6 strip', 'open',
    ['🧪 testing', '🔍 needs-hardware', '🎛️ front-panel'], 'Final hardware validation and handoff', [
      'Confirm smooth responsive TM1637 measurements and editor blink behavior.',
      'Identify and exercise every named PWM user channel plus the host demo macro without disturbing system-owned channels.',
      'Observe both enclosure fade directions, power indication, coherent RGB animations, and strip pixel order.',
      'Confirm emergency clear and ordinary mode-off recovery behavior.',
      'Verify door-open and door-closed TM1637 brightness persistence, true-off value 0, and both fade directions when prompted by WAIT/ringtone.',
    ], 'Source and read-only live telemetry are available, but the requested visual/output validation under safe conditions is pending.'),
  requirement('hardware-relay-motion', 13, 'Load-test relay identification, motion directions, interlocks, and door policy safely', 'open',
    ['🧪 testing', '🔍 needs-hardware', '🛡️ safety', '🔥 priority: critical'], 'Final hardware validation and handoff', [
      'Identify R1-R8 wiring and verify R5-R8 toggle/momentary behavior.',
      'Test Side A/B directions, break/settle timing, release-to-stop, cross-side isolation, and safe reset.',
      'Exercise closed/open/always/never policy for local, RF, host, macro, and automation sources.',
      'Verify door transition stops motion according to policy without an unsafe transient.',
      'Begin only after the host has prepared safe loads and issued the WAIT/ringtone prompt; restore all outputs off before ending the pass.',
    ], 'Implementation-level guards exist, but no complete safely prepared load test covers this matrix.'),
  requirement('hardware-rf-handset', 13, 'Complete real-handset RF capture, mapping, gesture, removal, and transmit validation', 'open',
    ['🧪 testing', '🔍 needs-hardware', '📡 rf-433'], 'Final hardware validation and handoff', [
      'Review/remove stale Remote A data and capture A/B/C/D one at a time.',
      'Map each stable RF identity to an explicitly confirmed action.',
      'Verify short burst, click, hold, repeat, inferred release, list/remap/remove, and latency.',
      'Transmit on INT1 and confirm reception on another receiver.',
      'Resume the guided human sequence with buttons B, C, and D after WAIT/ringtone; do not silently infer or auto-assign mappings.',
    ], 'Remote A has useful live evidence, but the complete handset set, CRUD, latency regression, stale record cleanup, and physical TX test remain open.'),
  requirement('hardware-lcd-usb-macro', 13, 'Validate optional LCD, USB lifecycle, and a harmless cancellable macro end to end', 'open',
    ['🧪 testing', '🔍 needs-hardware', '🖥️ host'], 'Final hardware validation and handoff', [
      'With a backpack connected, verify 0x27/0x3F detection, both LCD rows, host text, and concurrent TM1637.',
      'Unplug/replug USB and verify lifecycle events, authenticated reconnect, both DTR modes, TUI, and IPC updates.',
      'Run and cancel a harmless named macro that labels TM1637, writes LCD text, changes one PWM channel, and toggles one general relay.',
      'Capture logs/screenshots and restore all outputs to a safe state.',
      'Use WAIT/ringtone only when the prepared pass reaches a real cable, LCD, or physical-observation step; stop it as soon as the user responds.',
    ], 'Each path exists in source or tests, but no current physical end-to-end pass covers the connected LCD, USB reappearance, and macro cancellation together.'),
  requirement('release-handoff', 13, 'Complete final release evidence, launch, operating handoff, and acceptance closure', 'open',
    ['🧪 testing', '🔍 needs-hardware', '📚 documentation', '🔥 priority: critical'], 'Final hardware validation and handoff', [
      'Rebuild firmware/host from current source, record source/artifact hashes, upload/verify through the canonical host path, and authenticate HELLO.',
      'Run automated, simulator, and screenshot-driven interaction checks across every TUI page, keyboard/mouse control, settings editor, console completion, CLI, public library API, IPC/RPC, REST/WebSocket bridge, reconnect, programming, backup, and restore surface.',
      'Capture the packaged embedded WebUI at desktop and narrow widths in English/Persian, RTL/LTR, light/dark, keyboard-only and reduced-motion modes; verify dialogs, graphs, structured data, disconnect/reconnect truth, two-tab synchronization, command/event duplex, and zero console/network errors.',
      'Exercise every load-safe board path: identity/settings/menu queries, front-panel preview and remote keys, measurements, displays/audio, door/BT events, illumination/PWM/RGB, RF, macro timing/cancel, and safe output reset; record relay/motion/LCD/load checks as passed, failed, or explicitly human-blocked rather than assuming them.',
      'Launch the final canonical host against the board and verify secondary IPC operation.',
      'Provide complete board/host operating, safety, programming, backup, recovery, and troubleshooting instructions.',
      'Publish a final per-area verification matrix with exact commands, artifact hashes, firmware identity, screenshots/logs, observed results, remaining blockers, and restored safe output state.',
      'For every remaining human-assisted item, first back up settings, force safe outputs, confirm COM ownership, and prepare the exact capture; then show WAIT and play the unique continuous attention ringtone, stopping it promptly after the response and never overriding Do Not Disturb/Silent.',
      'On final successful launch and handoff, leave the board in a safe-output state with the seven-segment display showing ok.',
      'Do not call an ad-hoc development completion a nightly firmware; publish the exact release-channel description plus host source/artifact identity and firmware/EEPROM identity.',
      'Treat bridge-owned Go replacement, AVR flashing, EEPROM provisioning/restore, readback verification, rollback evidence, and live board behavior as mandatory closure gates rather than substituting source or VirtualBoard success.',
      'Close parent epics only after every linked child has current completion evidence.',
    ], 'A prior host was launched and a current firmware image was verified, but source/tooling has continued to change and the outstanding physical/UX/network/finalization checks prevent release closure.'),
];

const SESSION = {
  main: 'origin-main',
  ci: 'origin-ci',
  web: 'origin-web',
  recovery: 'origin-recovery',
  edge: 'edge-current',
  server: 'linux-server-2026-08-12',
  cafe: 'cafe-pc-webui-2026-08-12',
  tonight: 'direct-user-request-2026-08-12',
};

function prompt(session, turn, text) {
  return { session, turn, text };
}

// These are intentionally short, publication-safe portions of the original requests.
// Full transcripts, local paths, host identities, credentials, and unrelated project names
// remain in the ignored local audit and are never consumed by this publisher.
const PROMPT_EXCERPTS = {
  firmwareArchitecture: prompt(SESSION.main, 40, 'Move functions to their own domains where possible, use classes, and keep graceful reset behavior that configures the status LED and turns all relays and MOSFETs off.'),
  eepromSettings: prompt(SESSION.main, 28, 'The MCU is still responsible for storing its own configuration in EEPROM; the host stores the PC-side configuration. Do not mix them up.'),
  boardAutomations: prompt(SESSION.main, 52, 'Allow control events, such as sending RF commands, to be bound to relays. Store this programmable automation in the board EEPROM.'),
  resetSafety: prompt(SESSION.main, 42, 'Can reset telemetry also track the reset count? Please fix the reset-cause output and ensure navigation does not reset the board.'),
  firmwareIdentity: prompt(SESSION.edge, 212, 'The host app should query board information and build date/time on connect or reconnect. The board should display the firmware hash in full on the LCD and alternate the first and last four characters on the seven-segment display.'),
  boardPins: prompt(SESSION.main, 8, '433 MHz receive is on INT0 and transmit is on INT1. The PWM controller uses address 0x41 while INA219 remains at 0x40.'),
  measurements: prompt(SESSION.main, 43, 'Keep the excellent display refresh rate, add smoothing or filtering, and do not reduce the immediate response to events such as high temperature.'),
  pwmLighting: prompt(SESSION.main, 22, 'The enclosure light is jittery when fading from on to off. Fix it, and implement RGB LEDs for the idle state as well.'),
  displaysAudio: prompt(SESSION.main, 5, 'Restore the boot-up melody, ensure the buttons produce a short beep, and restore WS2811/WS2812 support.'),
  cooperativeI2c: prompt(SESSION.main, 53, 'I2C detection and scanning can be offloaded to the PC side if it saves flash, while the LCD retains a short offline message when the PC is disconnected.'),
  relaySafety: prompt(SESSION.main, 7, 'The first four relays work in two groups for direction and enable/disable, ensuring Up and Down cannot activate at the same time. The last four relays are general-purpose user outputs.'),
  motionDoorPolicy: prompt(SESSION.main, 52, 'Add a motion-door setting: allow when closed, allow when open, allow always, or never. The default is allow always.'),
  relayUserControls: prompt(SESSION.main, 10, 'Allow R5–R8 to be configured for toggle or momentary on/off behavior, and provide a menu for the four motion controls.'),
  keyLatency: prompt(SESSION.edge, 208, 'The key-press latency is still very noticeable. Menus that do not use hold or double-click should react directly on key-down without an extra wait.'),
  boardMenus: prompt(SESSION.main, 19, 'Add a default menu-page setting, optionally remember the last page across power loss, and let the PC send commands to select a page or change values.'),
  firstRun: prompt(SESSION.edge, 205, 'First-time discovery must install the bootloader, flash and EEPROM, then perform discovery, diagnostics, HELLO/ping, and dependency setup so the board is completely prepared.'),
  frontPanelMirror: prompt(SESSION.main, 56, 'Preview the connected board’s seven-segment display, 2×16 LCD, and buttons so physical and remote interfaces show the same live data and accept the same keys.'),
  lcdConsole: prompt(SESSION.main, 57, 'Change the LCD offline message to “PC offline” or “Connect USB to PC”.'),
  rfCore: prompt(SESSION.main, 48, 'Provide reusable PC-side controls to learn RF remote codes, map them to useful actions, and transmit RF commands.'),
  rfSessions: prompt(SESSION.recovery, 135, 'Indefinite multi-code learning is the default RF mode. Rename the one-shot mode to timer mode and clearly show its time on every interface.'),
  rfLatency: prompt(SESSION.main, 52, 'RF receive-to-relay activation is too slow and repeat detection sometimes fails. Fix it and verify it with real button presses.'),
  rfMetadata: prompt(SESSION.main, 59, 'Display learned RF codes consistently in hexadecimal or decimal, make that view configurable, add names and color-coded categories, search actions, and support transactional reordering.'),
  nativeProtocol: prompt(SESSION.main, 7, 'Remove Firmata entirely; use our own responsive protocol for communication between the controller board and PC host, supporting current and future commands.'),
  commandCoverage: prompt(SESSION.main, 37, 'Expose the rest of the board peripherals as well.'),
  monitorOff: prompt(SESSION.edge, 215, 'The IPC, bridge, and board can issue Monitor Off. Requests can come from board opcodes, other instances, WebUI, TUI, API, IPC, WebSocket, or bridge.'),
  protocolMenus: prompt(SESSION.main, 52, 'The host must query the board’s live menu list, show the active page and descriptions, and navigate by menu ID or name.'),
  protocolSimulator: prompt(SESSION.recovery, 159, 'Keep VirtualBoard and AVR in sync by compiling the same firmware for the PC with physical-device implementations replaced by virtual ones; do not maintain a separate implementation.'),
  canonicalContracts: prompt(SESSION.edge, 218, 'Ensure the controllers are general purpose and there are no duplicated definitions in the project. Find violations, choose remedies, and add documentation and checks that prevent recurrence.'),
  hostFoundation: prompt(SESSION.main, 10, 'The PC host needs monitoring, CLI and scripting capabilities, IPC, and the ability for the entire project to be consumed as a dynamic library.'),
  tuiPages: prompt(SESSION.main, 52, 'The controller needs dedicated pages for app configuration, board EEPROM settings, live controls, RF, programming, monitoring, and a polished mouse-aware TUI.'),
  tuiConsole: prompt(SESSION.edge, 215, 'Let the TUI manage its window size, use the correct rows and columns on first open, and set the console font. This applies only to local instances, not remote sessions such as SSH.'),
  surfaceParity: prompt(SESSION.edge, 218, 'Ensure TUI functions are not missing from the WebUI, and vice versa, with the same comparison for the native GUI. Backlog every real gap.'),
  monitoring: prompt(SESSION.main, 52, 'Let users choose monitored fields and polling rates, format adaptive SI units, retain graphs and history, and show important events in a clear timeline.'),
  eventNoise: prompt(SESSION.recovery, 186, 'Continuous events such as LED frames and measurements must not spam the logging console; use separate paths unless explicitly enabled for debugging.'),
  consoleUx: prompt(SESSION.main, 52, 'Finish nested completion, history recall, command grouping, clean colorized output, quit/exit, and clear-console behavior.'),
  hostAutomation: prompt(SESSION.main, 58, 'Register global hotkeys, show actionable notifications for important events, and support configurable host-side automation.'),
  osActions: prompt(SESSION.edge, 210, 'Use the proper native mechanism on each OS to query display brightness, volume/mute, and battery, and to issue and track suspend, sleep, hibernate, shutdown, and reboot.'),
  serviceTray: prompt(SESSION.edge, 210, 'Run the project as a privileged background service with a separate tray instance that can launch the GUI, TUI, and WebUI and provide quick configuration and port controls.'),
  macroPlayback: prompt(SESSION.main, 68, 'Record effects on the PC, stream them on request, show macro name, elapsed time, duration, and steps on the board, and keep completion or cancellation synchronized with the host.'),
  keyboardBindings: prompt(SESSION.main, 67, 'Map keyboard keys to motion, relays, and PWM while held, and always show authoritative output states even when commands come from RF, physical keys, automation, or a bridge.'),
  webUi: prompt(SESSION.web, 102, 'Design a polished responsive WebUI with RTL/LTR, dark mode, semantic colors, icons, dashboards, graphs, dialogs, settings, action controls, and thoughtful animations; embed it in the executable.'),
  webRoot: prompt(SESSION.edge, 215, 'The Web app root displays plain text on white with no styling, color, or actions. Restore the complete interface.'),
  ipcApi: prompt(SESSION.main, 53, 'Provide durable JSON-RPC, REST, and WebSocket APIs for remote programming, diagnostics, monitoring, configuration, commands, and bridge-to-bridge operation.'),
  networkDiscovery: prompt(SESSION.main, 58, 'Broadcast and discover controller instances with SSDP or mDNS so peers can find one another.'),
  httpMessages: prompt(SESSION.main, 58, 'Support web services, webhooks, bidirectional HTTP and WebSocket communication, Socket.IO, and actionable text messages across clients, servers, bridges, and boards.'),
  remoteSecurity: prompt(SESSION.edge, 210, 'Coordinate every new task with existing issues, pull requests, and documentation, while keeping disruptive OS and device actions behind the proper native and policy-controlled path.'),
  stableDevice: prompt(SESSION.edge, 201, 'Let the Go tooling set a board name of at most eight characters and store it on the board.'),
  usbReconnect: prompt(SESSION.main, 34, 'Add an opt-in reset-on-reconnect setting for DTR, disabled by default, and handle disconnect and reconnect immediately.'),
  serialOwner: prompt(SESSION.main, 116, 'Detect which process owns a controller port and provide an actionable way to release it instead of only reporting access denied.'),
  discoveryAuthority: prompt(SESSION.main, 58, 'Other instances should discover one another and use the running controller as the authoritative bridge to the connected board.'),
  serialLifecycle: prompt(SESSION.main, 54, 'Serial communication must remain enabled; DTR reset is disabled by default.'),
  urclock: prompt(SESSION.main, 33, 'The PC host controller should send Urboot/Urclock commands as well as native commands while the device is in boot mode.'),
  backups: prompt(SESSION.main, 59, 'Back up flash and EEPROM through Urboot/Urclock, use USBasp only as a fallback, deduplicate backups by hash, and keep a safe image for verified restoration.'),
  programmingEntrypoint: prompt(SESSION.main, 59, 'Firmware compiling and flashing must always go through the PC host app, including inspection, patching, backup, and restore.'),
  hexPatching: prompt(SESSION.main, 59, 'The host must inspect and patch the final flash image at known locations while keeping live settings export separate from offline EEPROM parsing.'),
  exitSnapshot: prompt(SESSION.main, 59, 'On graceful exit, store useful board configuration, data, and metadata in the host data directory for future diagnostics or migration.'),
  dependencies: prompt(SESSION.edge, 198, 'Handle missing arduino-cli, cores, and libraries and install them, preferably globally, while also offering a fresh portable toolchain.'),
  proxyPolicy: prompt(SESSION.edge, 200, 'Do not hardcode a proxy. Pass the proper environment variables to dependencies, force them onto arduino-cli when needed, and bypass loopback and the local network by default.'),
  projectImport: prompt(SESSION.main, 2, 'Compare the reusable local-library variants, merge the best parts into PCController, and keep a clean project skeleton without unrelated business logic.'),
  toolingEntrypoint: prompt(SESSION.main, 64, 'Use the project’s own controller tooling for all future development and deployment work.'),
  packagingIdentity: prompt(SESSION.recovery, 195, 'Unify the favicon, WebUI logo, and executable icon from one source asset, and align product name and version metadata across Win32, TUI, WebUI, manifests, and package metadata without hardcoding.'),
  configurationSources: prompt(SESSION.edge, 215, 'Make PCController configurable through environment variables, build flags, runtime flags, package metadata, and JSON/YAML/TOML configuration files.'),
  configurationPrecedence: prompt(SESSION.edge, 205, 'Make the tagline configurable. APP_NAME must override configuration, and runtime flags or switches have the highest priority.'),
  repositoryPublication: prompt(SESSION.main, 53, 'Publish the repository through the authenticated GitHub tooling and retain the requested dual licensing and required third-party terms.'),
  documentation: prompt(SESSION.main, 77, 'Fully set up the Wiki, Markdown, README, and docs; organize, write, verify, and use the documentation.'),
  wikiAndHandoff: prompt(SESSION.edge, 215, 'Complete the Wiki and repository settings, and place a simple-English version of the remaining tasks and next steps on the Desktop.'),
  repositoryMap: prompt(SESSION.edge, 219, 'Add a map of all files and how code, assets, and documentation are organized so a first-time contributor can find exactly what they need.'),
  wikiStyling: prompt(SESSION.edge, 222, 'Improve the Wiki with real styling, branding, emoji accents, and useful GitHub-specific Markdown.'),
  finalAudit: prompt(SESSION.main, 61, 'After the codebase is finalized, add concise comments for functions, settings, wiring, units, timing, and non-obvious behavior, then perform a final missing-requirement audit.'),
  tracker: prompt(SESSION.main, 66, 'Publish my requests from the checklist and JSONL turns as deduplicated GitHub issues and sub-issues, apply correct labels, and keep their open or closed states accurate.'),
  trackerReconciliation: prompt(SESSION.edge, 215, 'Extract all relevant local and origin-host prompts, prepend the applicable original portions to GitHub issues and pull requests, and synchronize issue, pull-request, project-board, Wiki, and local repository states.'),
  mergePolicy: prompt(SESSION.edge, 215, 'Immediately approve and merge pull requests that qualify. Mark work-in-progress pull requests as such and explain the reason.'),
  domainTracking: prompt(SESSION.edge, 221, 'Tag every GitHub issue by domain so it can be filtered clearly in the Kanban view, and checkpoint genuine in-progress work to remote branches instead of leaving it only in a dirty local tree.'),
  fullDuplexTracking: prompt(SESSION.edge, 218, 'Make GitHub issue comments two-way and full duplex, and keep every agent coordinated through the same issues, pull requests, project board, evidence, blockers, and replies.'),
  hardwareFrontPanel: prompt(SESSION.edge, 204, 'Physical front-panel keys, PC-sent virtual buttons, and RF-received keys must always respond immediately; fix the root cause and protect against regression.'),
  hardwareDoor: prompt(SESSION.main, 82, 'Verify door and BT Audio transitions, smooth informational LED changes, keep warning states immediate, and validate the temperature roles on real hardware.'),
  hardwarePwm: prompt(SESSION.main, 82, 'Perform final verification on the real board, including illumination, status LED transitions, displays, and every safely testable output.'),
  hardwareRelay: prompt(SESSION.main, 82, 'Do final verification by interacting with all parts of the host and board, and record the result for every area rather than assuming hardware behavior.'),
  hardwareRf: prompt(SESSION.main, 82, 'Resume real RF key learning when human interaction is needed, and use a unique repeating attention melody for the assisted steps.'),
  hardwareLcdUsbMacro: prompt(SESSION.main, 82, 'Complete real-board verification for the remaining LCD, USB lifecycle, macro, and safe interaction paths, explicitly recording any human-blocked checks.'),
  releaseHandoff: prompt(SESSION.main, 82, 'Generate a final report covering the entire project and what real interaction with the PC host and board found in each area.'),
  urbootCustom: prompt(SESSION.main, 94, 'Investigate a bootloader progress hook that can update the seven-segment display during read/write operations, and document whether an ISP connection is required.'),
  latestDependencies: prompt(SESSION.ci, 125, 'Ensure Dependabot and CodeQL are properly configured for the entire repository.'),
  zadig: prompt(SESSION.edge, 205, 'When USBasp is detected without a driver, offer to download and launch the latest Zadig, and track a native library or direct driver-installation replacement that avoids GUI interaction.'),
  artifactSync: prompt(SESSION.edge, 211, 'Serve backups, current board flash and EEPROM, and the running executable on request; let remote instances fetch, import, export, and synchronize them through the API.'),
  componentPorting: prompt(SESSION.edge, 210, 'Directly import applicable generalized system-level and framework-level components from the authorized sibling applications, rather than merely taking inspiration, while excluding their business logic.'),
  charmap: prompt(SESSION.tonight, 1, 'Create a live interactive opcode explorer that lists every opcode in a charmap-like table and expands descriptions when selected.'),
  rawOpcode: prompt(SESSION.recovery, 159, 'Allow clients to send arbitrary opcodes through the bridge and subscribe to their results so experimental and newly introduced operations remain inspectable.'),
  sharedFirmware: prompt(SESSION.recovery, 159, 'VirtualBoard must compile the same firmware as AVR with only physical-device implementations replaced by virtual ones; it must not be a separate implementation.'),
  staticHints: prompt(SESSION.recovery, 175, 'Stop adding persistent static hints, and remove similar redundant hints from the interfaces.'),
  authDefer: prompt(SESSION.cafe, 1, 'For now, disable the unintended authentication gate; localhost remains unauthenticated by default, while the complete remote session and approval model is deferred until explicitly resumed.'),
  highCpu: prompt(SESSION.cafe, 2, 'Investigate and fix the host process high CPU usage without discarding the evidence needed to identify the cause.'),
  usbLifecycleBroadcast: prompt(SESSION.cafe, 3, 'Reconnect to the same USB board after unplug and replug even if its COM port changes, and broadcast normalized disconnect and reconnect state to every client surface.'),
  urbootHandoff: prompt(SESSION.cafe, 4, 'Make the Urboot flashing handoff safe and recoverable, preserving silent mode and recovering cleanly when the application or COM identity changes.'),
  remoteHostPolicy: prompt(SESSION.cafe, 5, 'Make intended LAN hosts such as cafe-pc and cafe-pc.local reachable through one configurable remote-host exposure policy across config, environment, and command-line overrides.'),
  dashboardCards: prompt(SESSION.cafe, 6, 'Make dashboard cards and board actions reflect live socket and device state, support meaningful board-aware controls, and persist a useful rearrangeable layout.'),
  webTables: prompt(SESSION.cafe, 7, 'Repair WebUI table and event-stream filtering, menus, column layout, and debug-only handling of excessive telemetry.'),
  webShellSettings: prompt(SESSION.cafe, 8, 'Repair the WebUI shell, settings, navigation, shortcut hints, diagnostics, and developer affordances while removing static copy that does not communicate useful current state.'),
  crossSurfaceControl: prompt(SESSION.cafe, 9, 'Use the same board-control commands, validation, state, events, persistence, and names across Web, TUI, CLI, IPC, API, and firmware.'),
  telemetryAssets: prompt(SESSION.cafe, 10, 'Smooth browser telemetry, expose useful userscript diagnostics, and make production assets deterministic without serving stale bundles.'),
  messagingFabric: prompt(SESSION.cafe, 11, 'Route commands, events, actionable notifications, and board or host operations through one correlated messaging fabric across every supported interface and bridge.'),
  fullRemoteTui: prompt(SESSION.server, 2, 'Make the normal full Bubble Tea TUI work as a remote IPC client with all pages, prompt, shell, console, styling, and commands; Simple mode remains only an explicit fallback.'),
  peripheralMenus: prompt(SESSION.cafe, 12, 'Organize the WebUI Peripheral Workbench into nested, discoverable capability menus while preserving deep links, accessibility, and live state.'),
  toastIcon: prompt(SESSION.cafe, 13, 'Investigate and fix the missing Windows toast and notification icon using the canonical application identity.'),
  firmwareDisplayRegression: prompt(SESSION.cafe, 14, 'Restore the ability to un-silent the board from the seven-segment menu and repair seven-segment rollover without making the live board noisy.'),
  keyIdentification: prompt(SESSION.cafe, 15, 'Make the front-panel key-identification page escapable and keep the standalone identification behavior build-time selectable.'),
  clockDrift: prompt(SESSION.server, 1, 'The application must identify clock drift between instances and query NTP to determine which instance or instances are incorrect and by how much.'),
  hostEepromDefaults: prompt(SESSION.tonight, 2, 'Move non-safety factory defaults out of AVR flash where worthwhile; let Go provision default EEPROM on a new flash, back up and restore EEPROM, restore factory defaults, and apply UI setting changes immediately.'),
  discoveryMetadata: prompt(SESSION.tonight, 3, 'SSDP and mDNS must present board values, application values, and the WebUI, while avoiding continuous board polling.'),
  tuiControlRegression: prompt(SESSION.tonight, 4, 'Fix the TUI Control page off-by-one problem, remove Enter turns X and the GROUP column, use group separators, repair highlighting and headers, expose configurable state colors, and support direct horizontal and mouse slider control.'),
  uartProfile: prompt(SESSION.tonight, 5, 'Measure what consumes the most UART bandwidth and reduce it where possible while avoiding unnecessary polling and retaining immediate pushed events.'),
  displayPresentation: prompt(SESSION.tonight, 6, 'Finish the seven-segment scroll with an all-empty final frame; scroll only when text exceeds the display unless forced, expose timing and repeat policies, and support arbitrary seven-segment and LCD messages across every interface.'),
  commandRecipes: prompt(SESSION.tonight, 7, 'Provide terminal examples for arbitrary display messages, scrolling and timing, LED colors and effects, opcodes, buzzer routing, EEPROM operations, and coordinated UI navigation.'),
  namedSiblingPorts: prompt(SESSION.tonight, 8, 'Bring the applicable Rayan Lamp and Patris-export implementations into this project in full and enhance them; do not merely refer to or imitate them.'),
  identityVisuals: prompt(SESSION.tonight, 9, 'Align the favicon, WebUI logo, executable and console icons as generated forms of one unique source asset, and show before and after variants in GitHub for selection.'),
  liveMeasurementTiming: prompt(SESSION.tonight, 10, 'Expose host-synchronized Live Measurements refresh rate and freshness window in WebUI and TUI Settings. Default to 250 ms (4 Hz) and 1500 ms, restrict refresh to 200–500 ms, require freshness to include headroom, persist it in host config, and immediately propagate changes to every WebUI/TUI instance including remote TUIs.'),
  webAcceptanceRepair: prompt(SESSION.cafe, 16, 'Repair the embedded controller WebUI temperature truth and charts, relay controls, terminal behavior, nested workbench routes, settings and tables, updates, and automatic client resource refresh.'),
  ledClientExperience: prompt(SESSION.cafe, 17, 'Keep WebUI and TUI synchronized for status LED ownership, profiles, effects, live preview, stream ordering, accessibility, RTL and LTR, and responsive dark-mode behavior.'),
  buzzerFallback: prompt(SESSION.recovery, 177, 'Add a fallback for audio-frequency generation when port 0x61 is unavailable.'),
  linuxParity: prompt(SESSION.recovery, 178, 'Introduce Linux host parity, including buzzer playback support.'),
  rgbEffects: prompt(SESSION.recovery, 160, 'Send parameterized status-LED colors and effects to the board, including breathing, flashing, cycling, and transitions, while keeping the AVR implementation compact and mirroring it in the UI.'),
  instanceCoordination: prompt(SESSION.recovery, 169, 'Let instances communicate, control one another, navigate pages and tabs, query running-instance information, and keep settings and UI state synchronized.'),
  redis: prompt(SESSION.recovery, 170, 'Add direct Redis support to the Go tooling where it is relevant.'),
  stuckUpdate: prompt(SESSION.recovery, 189, 'Track and fix stuck bridge or host update processes so the failure does not recur.'),
  dependencyBlocker: prompt(SESSION.ci, 147, 'Keep dependency and core updates automated, validated, and compatible with the complete repository build rather than accepting a partially failing update.'),
  canonicalLogo: prompt(SESSION.edge, 215, 'Fix the broken documentation SVG logo and use the same correct icon as the executable and WebUI.'),
  consoleIcon: prompt(SESSION.edge, 213, 'The build does not show the app icon in the conhost.exe window. Investigate why and apply the needed fix.'),
  ciDelivery: prompt(SESSION.ci, 93, 'Implement complete GitHub CI/CD for the AVR firmware and cross-platform host, including builds, release artifacts, summaries, dependency automation, and a draft alpha release.'),
  ciSummaries: prompt(SESSION.ci, 113, 'Generate one summary per codebase, merge platform results into comparison tables, and consolidate VirtualBoard summaries.'),
  apiRace: prompt(SESSION.web, 110, 'Provide full-duplex events between WebUI, TUI, bridge, and board without losing controller state.'),
  timelineMemory: prompt(SESSION.main, 52, 'Retain graph history and important events without wasteful or flickering presentation.'),
  configReload: prompt(SESSION.recovery, 138, 'Investigate repeated configuration-reload rejection and ensure watched settings update the interface immediately.'),
  releaseSecurity: prompt(SESSION.ci, 93, 'Make release builds and artifact upload reproducible, source-identified, cross-platform, and safe for a draft alpha release.'),
};

const ORIGINAL_REQUESTS = {
  'fw-core-architecture': [PROMPT_EXCERPTS.firmwareArchitecture],
  'mcu-eeprom-settings': [PROMPT_EXCERPTS.eepromSettings],
  'mcu-event-automation': [PROMPT_EXCERPTS.boardAutomations],
  'reset-safety-journal': [PROMPT_EXCERPTS.resetSafety],
  'firmware-identity-layout-time': [PROMPT_EXCERPTS.firmwareIdentity],
  'board-pin-map-inputs': [PROMPT_EXCERPTS.boardPins],
  'measurement-sensors-i2c': [PROMPT_EXCERPTS.measurements],
  'pwm-lighting-rgb-strip': [PROMPT_EXCERPTS.pwmLighting],
  'displays-audio': [PROMPT_EXCERPTS.displaysAudio],
  'cooperative-host-i2c-profile': [PROMPT_EXCERPTS.cooperativeI2c],
  'relay-motion-interlocks': [PROMPT_EXCERPTS.relaySafety],
  'motion-door-policy': [PROMPT_EXCERPTS.motionDoorPolicy],
  'relay-user-controls-break-setting': [PROMPT_EXCERPTS.relayUserControls],
  'frontpanel-key-gestures': [PROMPT_EXCERPTS.keyLatency],
  'board-menu-hierarchy-settings': [PROMPT_EXCERPTS.boardMenus],
  'first-run-board-synchronization': [PROMPT_EXCERPTS.firstRun],
  'frontpanel-snapshot-remote-menus': [PROMPT_EXCERPTS.frontPanelMirror],
  'lcd-console-status-events': [PROMPT_EXCERPTS.lcdConsole],
  'rf-transport-learning-core': [PROMPT_EXCERPTS.rfCore],
  'rf-learning-sessions-capacity': [PROMPT_EXCERPTS.rfSessions],
  'rf-latency-gestures-guided': [PROMPT_EXCERPTS.rfLatency],
  'rf-metadata-format-reorder': [PROMPT_EXCERPTS.rfMetadata],
  'protocol-native-uart': [PROMPT_EXCERPTS.nativeProtocol],
  'protocol-command-event-coverage': [PROMPT_EXCERPTS.commandCoverage, PROMPT_EXCERPTS.monitorOff],
  'protocol-frontpanel-menu-uptime': [PROMPT_EXCERPTS.protocolMenus],
  'protocol-simulator-transport': [PROMPT_EXCERPTS.protocolSimulator],
  'host-foundation-config-library': [PROMPT_EXCERPTS.hostFoundation, PROMPT_EXCERPTS.tuiConsole, PROMPT_EXCERPTS.configurationSources, PROMPT_EXCERPTS.configurationPrecedence],
  'tui-pages-controls': [PROMPT_EXCERPTS.tuiPages, PROMPT_EXCERPTS.tuiConsole],
  'ui-surface-capability-parity': [PROMPT_EXCERPTS.surfaceParity, PROMPT_EXCERPTS.canonicalContracts],
  'monitoring-format-history': [PROMPT_EXCERPTS.monitoring, PROMPT_EXCERPTS.eventNoise],
  'console-command-ux': [PROMPT_EXCERPTS.consoleUx],
  'host-automation-hotkeys-os': [PROMPT_EXCERPTS.hostAutomation, PROMPT_EXCERPTS.osActions, PROMPT_EXCERPTS.monitorOff],
  'privileged-service-tray-controller': [PROMPT_EXCERPTS.serviceTray],
  'host-macro-recording-playback-sync': [PROMPT_EXCERPTS.macroPlayback],
  'host-keyboard-bindings-output-state': [PROMPT_EXCERPTS.keyboardBindings],
  'embedded-webui-native-experience': [PROMPT_EXCERPTS.webUi, PROMPT_EXCERPTS.webRoot, PROMPT_EXCERPTS.configurationSources],
  'ipc-websocket-api-suite': [PROMPT_EXCERPTS.ipcApi, PROMPT_EXCERPTS.monitorOff],
  'network-bridge-discovery': [PROMPT_EXCERPTS.networkDiscovery, PROMPT_EXCERPTS.monitorOff],
  'http-webhooks-socketio-messages': [PROMPT_EXCERPTS.httpMessages],
  'remote-control-security': [PROMPT_EXCERPTS.remoteSecurity, PROMPT_EXCERPTS.monitorOff],
  'stable-device-selection': [PROMPT_EXCERPTS.stableDevice],
  'usb-reconnect-notifications': [PROMPT_EXCERPTS.usbReconnect],
  'primary-serial-owner-ipc': [PROMPT_EXCERPTS.serialOwner],
  'controller-discovery-authority': [PROMPT_EXCERPTS.discoveryAuthority],
  'serial-lifecycle-contract': [PROMPT_EXCERPTS.serialLifecycle],
  'uart-urclock-programming': [PROMPT_EXCERPTS.urclock],
  'preflash-backup-dedup-restore': [PROMPT_EXCERPTS.backups],
  'canonical-host-programming-entrypoint': [PROMPT_EXCERPTS.programmingEntrypoint],
  'hex-patch-settings-export': [PROMPT_EXCERPTS.hexPatching],
  'graceful-host-snapshot': [PROMPT_EXCERPTS.exitSnapshot],
  'arduino-go-dependencies': [PROMPT_EXCERPTS.dependencies, PROMPT_EXCERPTS.proxyPolicy],
  'latest-toolchain-update-automation': [PROMPT_EXCERPTS.latestDependencies, PROMPT_EXCERPTS.proxyPolicy],
  'project-import-structure': [PROMPT_EXCERPTS.projectImport],
  'native-virtual-board': [PROMPT_EXCERPTS.protocolSimulator],
  'canonical-cross-language-contracts': [PROMPT_EXCERPTS.canonicalContracts, PROMPT_EXCERPTS.sharedFirmware],
  'tooling-entrypoint-consolidation': [PROMPT_EXCERPTS.toolingEntrypoint],
  'canonical-host-artifact-packaging': [PROMPT_EXCERPTS.packagingIdentity, PROMPT_EXCERPTS.canonicalLogo, PROMPT_EXCERPTS.configurationSources, PROMPT_EXCERPTS.configurationPrecedence],
  'github-license-notices': [PROMPT_EXCERPTS.repositoryPublication],
  'canonical-documentation-guide': [PROMPT_EXCERPTS.documentation, PROMPT_EXCERPTS.wikiAndHandoff, PROMPT_EXCERPTS.canonicalLogo, PROMPT_EXCERPTS.repositoryMap, PROMPT_EXCERPTS.wikiStyling],
  'final-code-documentation-gate': [PROMPT_EXCERPTS.finalAudit],
  'requirements-backlog-publication': [PROMPT_EXCERPTS.tracker, PROMPT_EXCERPTS.trackerReconciliation, PROMPT_EXCERPTS.mergePolicy, PROMPT_EXCERPTS.domainTracking, PROMPT_EXCERPTS.fullDuplexTracking],
  'hardware-frontpanel-audio': [PROMPT_EXCERPTS.hardwareFrontPanel],
  'hardware-door-bt-temperature': [PROMPT_EXCERPTS.hardwareDoor],
  'hardware-pwm-displays-lighting': [PROMPT_EXCERPTS.hardwarePwm],
  'hardware-relay-motion': [PROMPT_EXCERPTS.hardwareRelay],
  'hardware-rf-handset': [PROMPT_EXCERPTS.hardwareRf],
  'hardware-lcd-usb-macro': [PROMPT_EXCERPTS.hardwareLcdUsbMacro],
  'release-handoff': [PROMPT_EXCERPTS.releaseHandoff],
  'urboot-custom-progress-backend': [PROMPT_EXCERPTS.urbootCustom],
  'network-artifact-import-export-sync': [PROMPT_EXCERPTS.artifactSync],
  'authorized-reusable-component-porting': [PROMPT_EXCERPTS.componentPorting],
};

const EPIC_ORIGINAL_REQUESTS = {
  1: [PROMPT_EXCERPTS.firmwareArchitecture, PROMPT_EXCERPTS.eepromSettings],
  2: [PROMPT_EXCERPTS.measurements, PROMPT_EXCERPTS.pwmLighting, PROMPT_EXCERPTS.displaysAudio],
  3: [PROMPT_EXCERPTS.relaySafety, PROMPT_EXCERPTS.motionDoorPolicy],
  4: [PROMPT_EXCERPTS.keyLatency, PROMPT_EXCERPTS.boardMenus],
  5: [PROMPT_EXCERPTS.rfCore, PROMPT_EXCERPTS.rfSessions],
  6: [PROMPT_EXCERPTS.nativeProtocol, PROMPT_EXCERPTS.commandCoverage],
  7: [PROMPT_EXCERPTS.hostFoundation, PROMPT_EXCERPTS.tuiPages, PROMPT_EXCERPTS.osActions, PROMPT_EXCERPTS.surfaceParity],
  8: [PROMPT_EXCERPTS.ipcApi, PROMPT_EXCERPTS.networkDiscovery, PROMPT_EXCERPTS.monitorOff],
  9: [PROMPT_EXCERPTS.usbReconnect, PROMPT_EXCERPTS.serialOwner],
  10: [PROMPT_EXCERPTS.urclock, PROMPT_EXCERPTS.backups, PROMPT_EXCERPTS.programmingEntrypoint],
  11: [PROMPT_EXCERPTS.dependencies, PROMPT_EXCERPTS.protocolSimulator, PROMPT_EXCERPTS.packagingIdentity, PROMPT_EXCERPTS.canonicalContracts],
  12: [PROMPT_EXCERPTS.documentation, PROMPT_EXCERPTS.trackerReconciliation, PROMPT_EXCERPTS.repositoryMap, PROMPT_EXCERPTS.wikiStyling],
  13: [PROMPT_EXCERPTS.releaseHandoff],
};

const EXTRA_ISSUE_ORIGINAL_REQUESTS = {
  102: [PROMPT_EXCERPTS.charmap],
  103: [PROMPT_EXCERPTS.sharedFirmware],
  104: [PROMPT_EXCERPTS.staticHints],
  105: [PROMPT_EXCERPTS.buzzerFallback],
  106: [PROMPT_EXCERPTS.linuxParity],
  107: [PROMPT_EXCERPTS.rgbEffects],
  108: [PROMPT_EXCERPTS.instanceCoordination],
  109: [PROMPT_EXCERPTS.redis],
  110: [PROMPT_EXCERPTS.stuckUpdate],
  112: [PROMPT_EXCERPTS.zadig],
  115: [PROMPT_EXCERPTS.dependencyBlocker],
  131: [PROMPT_EXCERPTS.artifactSync, PROMPT_EXCERPTS.trackerReconciliation],
  134: [PROMPT_EXCERPTS.dependencyBlocker, PROMPT_EXCERPTS.latestDependencies],
  135: [PROMPT_EXCERPTS.linuxParity, PROMPT_EXCERPTS.osActions, PROMPT_EXCERPTS.serviceTray],
  137: [PROMPT_EXCERPTS.dependencies, PROMPT_EXCERPTS.proxyPolicy, PROMPT_EXCERPTS.linuxParity, PROMPT_EXCERPTS.serviceTray],
  139: [PROMPT_EXCERPTS.linuxParity, PROMPT_EXCERPTS.dependencies, PROMPT_EXCERPTS.proxyPolicy],
  145: [PROMPT_EXCERPTS.dependencyBlocker],
  148: [PROMPT_EXCERPTS.authDefer],
  149: [PROMPT_EXCERPTS.highCpu],
  154: [PROMPT_EXCERPTS.usbLifecycleBroadcast],
  155: [PROMPT_EXCERPTS.urbootHandoff],
  156: [PROMPT_EXCERPTS.remoteHostPolicy],
  157: [PROMPT_EXCERPTS.sharedFirmware, PROMPT_EXCERPTS.dependencies],
  159: [PROMPT_EXCERPTS.dashboardCards],
  160: [PROMPT_EXCERPTS.webTables],
  161: [PROMPT_EXCERPTS.webShellSettings],
  162: [PROMPT_EXCERPTS.crossSurfaceControl],
  163: [PROMPT_EXCERPTS.telemetryAssets],
  164: [PROMPT_EXCERPTS.messagingFabric],
  165: [PROMPT_EXCERPTS.messagingFabric],
  166: [PROMPT_EXCERPTS.messagingFabric, PROMPT_EXCERPTS.crossSurfaceControl],
  171: [PROMPT_EXCERPTS.fullRemoteTui],
  172: [PROMPT_EXCERPTS.peripheralMenus],
  177: [PROMPT_EXCERPTS.clockDrift],
  182: [PROMPT_EXCERPTS.firstRun, PROMPT_EXCERPTS.backups, PROMPT_EXCERPTS.programmingEntrypoint, PROMPT_EXCERPTS.zadig],
  183: [PROMPT_EXCERPTS.rgbEffects, PROMPT_EXCERPTS.sharedFirmware, PROMPT_EXCERPTS.mergePolicy, PROMPT_EXCERPTS.hardwarePwm, PROMPT_EXCERPTS.surfaceParity, PROMPT_EXCERPTS.canonicalContracts],
  184: [PROMPT_EXCERPTS.rawOpcode],
  185: [PROMPT_EXCERPTS.hostEepromDefaults],
  186: [PROMPT_EXCERPTS.discoveryMetadata],
  189: [PROMPT_EXCERPTS.tuiControlRegression],
  190: [PROMPT_EXCERPTS.uartProfile],
  191: [PROMPT_EXCERPTS.displayPresentation],
  192: [PROMPT_EXCERPTS.commandRecipes],
  193: [PROMPT_EXCERPTS.namedSiblingPorts],
  194: [PROMPT_EXCERPTS.identityVisuals],
};

const PR_ORIGINAL_REQUESTS = {
  76: [PROMPT_EXCERPTS.latestDependencies],
  77: [PROMPT_EXCERPTS.latestDependencies],
  78: [PROMPT_EXCERPTS.latestDependencies],
  79: [PROMPT_EXCERPTS.latestDependencies],
  80: [PROMPT_EXCERPTS.firmwareArchitecture],
  81: [PROMPT_EXCERPTS.nativeProtocol],
  82: [PROMPT_EXCERPTS.toolingEntrypoint, PROMPT_EXCERPTS.packagingIdentity],
  83: [PROMPT_EXCERPTS.eepromSettings, PROMPT_EXCERPTS.backups],
  84: [PROMPT_EXCERPTS.ciDelivery],
  85: [PROMPT_EXCERPTS.ciDelivery, PROMPT_EXCERPTS.releaseSecurity],
  86: [PROMPT_EXCERPTS.apiRace],
  90: [PROMPT_EXCERPTS.ciDelivery],
  91: [PROMPT_EXCERPTS.releaseSecurity],
  92: [PROMPT_EXCERPTS.releaseSecurity],
  93: [PROMPT_EXCERPTS.ciSummaries],
  94: [PROMPT_EXCERPTS.latestDependencies],
  95: [PROMPT_EXCERPTS.latestDependencies],
  96: [PROMPT_EXCERPTS.timelineMemory],
  97: [PROMPT_EXCERPTS.configReload],
  98: [PROMPT_EXCERPTS.latestDependencies],
  99: [PROMPT_EXCERPTS.latestDependencies, PROMPT_EXCERPTS.proxyPolicy],
  100: [PROMPT_EXCERPTS.releaseHandoff, PROMPT_EXCERPTS.tracker],
  111: [PROMPT_EXCERPTS.latestDependencies, PROMPT_EXCERPTS.mergePolicy],
  113: [PROMPT_EXCERPTS.releaseHandoff, PROMPT_EXCERPTS.ciDelivery],
  114: [PROMPT_EXCERPTS.dependencyBlocker, PROMPT_EXCERPTS.mergePolicy],
  119: [PROMPT_EXCERPTS.consoleIcon, PROMPT_EXCERPTS.osActions, PROMPT_EXCERPTS.artifactSync],
  120: [PROMPT_EXCERPTS.tuiConsole, PROMPT_EXCERPTS.webRoot, PROMPT_EXCERPTS.configurationSources, PROMPT_EXCERPTS.canonicalLogo, PROMPT_EXCERPTS.trackerReconciliation],
  121: [PROMPT_EXCERPTS.packagingIdentity, PROMPT_EXCERPTS.configurationSources, PROMPT_EXCERPTS.configurationPrecedence],
  122: [PROMPT_EXCERPTS.httpMessages, PROMPT_EXCERPTS.remoteSecurity],
  123: [PROMPT_EXCERPTS.repositoryMap, PROMPT_EXCERPTS.wikiStyling, PROMPT_EXCERPTS.documentation],
  126: [PROMPT_EXCERPTS.artifactSync, PROMPT_EXCERPTS.remoteSecurity, PROMPT_EXCERPTS.proxyPolicy],
  127: [PROMPT_EXCERPTS.domainTracking, PROMPT_EXCERPTS.fullDuplexTracking, PROMPT_EXCERPTS.canonicalContracts, PROMPT_EXCERPTS.surfaceParity],
  128: [PROMPT_EXCERPTS.wikiStyling, PROMPT_EXCERPTS.repositoryMap],
  129: [PROMPT_EXCERPTS.dependencyBlocker, PROMPT_EXCERPTS.latestDependencies],
  130: [PROMPT_EXCERPTS.packagingIdentity, PROMPT_EXCERPTS.serviceTray, PROMPT_EXCERPTS.releaseHandoff],
  132: [PROMPT_EXCERPTS.linuxParity, PROMPT_EXCERPTS.dependencies, PROMPT_EXCERPTS.proxyPolicy, PROMPT_EXCERPTS.serviceTray],
  133: [PROMPT_EXCERPTS.dependencies, PROMPT_EXCERPTS.proxyPolicy, PROMPT_EXCERPTS.toolingEntrypoint],
  136: [PROMPT_EXCERPTS.dependencyBlocker, PROMPT_EXCERPTS.latestDependencies, PROMPT_EXCERPTS.proxyPolicy],
  138: [PROMPT_EXCERPTS.httpMessages, PROMPT_EXCERPTS.remoteSecurity],
  140: [PROMPT_EXCERPTS.trackerReconciliation, PROMPT_EXCERPTS.domainTracking, PROMPT_EXCERPTS.fullDuplexTracking],
  141: [PROMPT_EXCERPTS.dependencies, PROMPT_EXCERPTS.linuxParity, PROMPT_EXCERPTS.remoteSecurity, PROMPT_EXCERPTS.domainTracking],
  142: [PROMPT_EXCERPTS.macroPlayback, PROMPT_EXCERPTS.keyboardBindings],
  143: [PROMPT_EXCERPTS.latestDependencies],
  144: [PROMPT_EXCERPTS.latestDependencies],
  146: [PROMPT_EXCERPTS.latestDependencies],
  147: [PROMPT_EXCERPTS.latestDependencies],
  150: [PROMPT_EXCERPTS.toastIcon, PROMPT_EXCERPTS.packagingIdentity],
  151: [PROMPT_EXCERPTS.firmwareDisplayRegression],
  152: [PROMPT_EXCERPTS.authDefer, PROMPT_EXCERPTS.webUi],
  153: [PROMPT_EXCERPTS.firmwareDisplayRegression, PROMPT_EXCERPTS.authDefer, PROMPT_EXCERPTS.macroPlayback],
  158: [PROMPT_EXCERPTS.keyIdentification, PROMPT_EXCERPTS.hardwareFrontPanel],
  167: [PROMPT_EXCERPTS.dashboardCards],
  168: [PROMPT_EXCERPTS.webTables, PROMPT_EXCERPTS.webShellSettings],
  169: [PROMPT_EXCERPTS.crossSurfaceControl],
  170: [PROMPT_EXCERPTS.telemetryAssets],
  173: [PROMPT_EXCERPTS.messagingFabric],
  174: [PROMPT_EXCERPTS.dashboardCards, PROMPT_EXCERPTS.webTables, PROMPT_EXCERPTS.webShellSettings, PROMPT_EXCERPTS.crossSurfaceControl, PROMPT_EXCERPTS.telemetryAssets],
  175: [PROMPT_EXCERPTS.macroPlayback],
  176: [PROMPT_EXCERPTS.macroPlayback, PROMPT_EXCERPTS.keyboardBindings],
  178: [PROMPT_EXCERPTS.messagingFabric, PROMPT_EXCERPTS.crossSurfaceControl, PROMPT_EXCERPTS.apiRace, PROMPT_EXCERPTS.fullDuplexTracking],
  179: [PROMPT_EXCERPTS.packagingIdentity, PROMPT_EXCERPTS.releaseSecurity, PROMPT_EXCERPTS.ciDelivery],
  180: [PROMPT_EXCERPTS.rgbEffects, PROMPT_EXCERPTS.sharedFirmware, PROMPT_EXCERPTS.hardwarePwm],
  181: [PROMPT_EXCERPTS.rgbEffects, PROMPT_EXCERPTS.hardwarePwm],
  187: [PROMPT_EXCERPTS.webAcceptanceRepair],
  188: [PROMPT_EXCERPTS.ledClientExperience],
  195: [PROMPT_EXCERPTS.firstRun, PROMPT_EXCERPTS.backups, PROMPT_EXCERPTS.programmingEntrypoint, PROMPT_EXCERPTS.zadig],
  196: [PROMPT_EXCERPTS.programmingEntrypoint, PROMPT_EXCERPTS.hardwarePwm],
  197: [PROMPT_EXCERPTS.trackerReconciliation, PROMPT_EXCERPTS.domainTracking, PROMPT_EXCERPTS.fullDuplexTracking],
  198: [PROMPT_EXCERPTS.timelineMemory, PROMPT_EXCERPTS.fullRemoteTui],
  199: [PROMPT_EXCERPTS.tuiControlRegression],
  200: [PROMPT_EXCERPTS.liveMeasurementTiming],
  201: [PROMPT_EXCERPTS.programmingEntrypoint, PROMPT_EXCERPTS.stuckUpdate],
  202: [PROMPT_EXCERPTS.liveMeasurementTiming],
};

const TRACE_START = '<!-- prompt-provenance:v1 -->';
const TRACE_END = '<!-- /prompt-provenance:v1 -->';

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

async function allPullRequests() {
  const pulls = [];
  for (let page = 1; ; page += 1) {
    const batch = api('GET', `repos/${REPO}/pulls?state=all&per_page=100&page=${page}`);
    pulls.push(...batch);
    if (batch.length < 100) break;
  }
  return pulls;
}

function marker(id) {
  return `<!-- requirement-id: ${id} -->`;
}

function blockquote(text) {
  return text.split('\n').map((line) => line ? `> ${line}` : '>').join('\n');
}

function originalRequestSection(excerpts) {
  if (!excerpts.length) return [];
  return [
    TRACE_START,
    '## Original request',
    '',
    '_Relevant excerpt, lightly grammar-corrected for publication. Credentials, personal data, private paths, and unrelated transcript content are omitted._',
    '',
    ...excerpts.flatMap((excerpt) => [
      blockquote(excerpt.text),
      '',
      `_Source: session \`${excerpt.session}\`, authored turn ${excerpt.turn}._`,
      '',
    ]),
    TRACE_END,
    '',
  ];
}

function removeExistingOriginalRequestSection(body = '') {
  const normalized = body.replaceAll('\r\n', '\n');
  const start = normalized.indexOf(TRACE_START);
  if (start === -1) return normalized;

  const end = normalized.indexOf(TRACE_END, start);
  if (end !== -1) {
    let remainder = `${normalized.slice(0, start)}${normalized.slice(end + TRACE_END.length)}`.replace(/^\n+/, '');
    if (remainder.startsWith('---\n')) remainder = remainder.slice(4);
    return remainder.replace(/^\n+/, '');
  }

  // Migration for the first publication pass, which had a start marker but no
  // explicit end marker. Stop at the source annotation so WIP/status markers and
  // all human-authored content after it remain byte-for-byte in place.
  const legacy = normalized.slice(start).match(/^<!-- prompt-provenance:v1 -->\n## Original request\n[\s\S]*?_Source: session `[^`]+`, authored turn \d+\._\n*/);
  if (!legacy) throw new Error('found an unrecognized legacy prompt-provenance block');
  let remainder = `${normalized.slice(0, start)}${normalized.slice(start + legacy[0].length)}`.replace(/^\n+/, '');
  if (remainder.startsWith('---\n')) remainder = remainder.slice(4);
  return remainder.replace(/^\n+/, '');
}

function bodyWithOriginalRequests(body, excerpts) {
  const remainder = removeExistingOriginalRequestSection(body).trimEnd();
  const section = originalRequestSection(excerpts).join('\n').trimEnd();
  return remainder ? `${section}\n\n---\n\n${remainder}\n` : `${section}\n`;
}

async function syncSupplementalTraceability(issues) {
  const issueByNumber = new Map(issues.map((issue) => [issue.number, issue]));
  for (const [numberText, excerpts] of Object.entries(EXTRA_ISSUE_ORIGINAL_REQUESTS)) {
    const number = Number(numberText);
    const issue = issueByNumber.get(number);
    if (!issue) throw new Error(`supplemental tracker issue #${number} is missing`);
    const body = bodyWithOriginalRequests(issue.body ?? '', excerpts);
    const requiredLabels = EXTRA_ISSUE_REQUIRED_LABELS[number];
    if (!requiredLabels?.length) throw new Error(`supplemental tracker issue #${number} has no domain-label contract`);
    const needsBody = issue.body !== body;
    const needsLabels = !hasRequiredLabels(issue, requiredLabels);
    if (!needsBody && !needsLabels) continue;
    if (!APPLY) {
      process.stdout.write(`UPDATE #${number} supplemental tracker: ${[needsBody && 'body', needsLabels && 'domain labels'].filter(Boolean).join(', ')}\n`);
      continue;
    }
    const updated = api('PATCH', `repos/${REPO}/issues/${number}`, {
      body,
      labels: labelsWithRequired(issue, requiredLabels),
    });
    issueByNumber.set(number, updated);
    process.stdout.write(`updated #${number} supplemental prompt provenance/domain labels\n`);
  }

  const pulls = await allPullRequests();
  const pullByNumber = new Map(pulls.map((pull) => [pull.number, pull]));
  const untrackedPulls = pulls.filter((pull) => !PR_ORIGINAL_REQUESTS[pull.number]).map((pull) => pull.number);
  if (untrackedPulls.length) throw new Error(`pull requests missing static prompt provenance: ${untrackedPulls.map((number) => `#${number}`).join(', ')}`);
  for (const [numberText, excerpts] of Object.entries(PR_ORIGINAL_REQUESTS)) {
    const number = Number(numberText);
    const pull = pullByNumber.get(number);
    if (!pull) throw new Error(`prompt-tracked pull request #${number} is missing`);
    const body = bodyWithOriginalRequests(pull.body ?? '', excerpts);
    if (pull.body === body) continue;
    if (!APPLY) {
      process.stdout.write(`UPDATE PR #${number} prompt provenance: body\n`);
      continue;
    }
    api('PATCH', `repos/${REPO}/pulls/${number}`, { body });
    process.stdout.write(`updated PR #${number} prompt provenance\n`);
  }
}

function bodyFor(item) {
  return [
    ...originalRequestSection(ORIGINAL_REQUESTS[item.id] ?? []),
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
    ORIGINAL_REQUESTS[item.id]?.length
      ? '- Includes only curated, issue-relevant prompt excerpts; complete transcripts, credentials, secrets, and private paths remain unpublished.'
      : '- Normalized from the project checklist and private local request audit; no raw conversation text is published.',
    '',
  ].join('\n');
}

function sameLabels(issue, expected) {
  const actual = issueLabelNames(issue).sort();
  return JSON.stringify(actual) === JSON.stringify([...expected].sort());
}

function issueLabelNames(issue) {
  return issue.labels.map((label) => typeof label === 'string' ? label : label.name);
}

function hasRequiredLabels(issue, required) {
  const actual = new Set(issueLabelNames(issue));
  return required.every((label) => actual.has(label));
}

function labelsWithRequired(issue, required) {
  return [...new Set([...issueLabelNames(issue), ...required])];
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
  const hasCompletePromptTrace = (body) => body?.startsWith(`${TRACE_START}\n`)
    && body.includes('\n## Original request\n')
    && body.includes('\n> ')
    && body.includes(`\n${TRACE_END}\n`);
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
    const expected = [
      ...published.filter((item) => item.parent === parent).map((item) => item.number),
      ...Object.entries(EXTRA_ISSUE_EPIC_PARENTS)
        .filter(([, expectedParent]) => expectedParent === parent)
        .map(([number]) => Number(number)),
    ].sort((a, b) => a - b);
    const actualNumbers = actual.map((item) => item.number).sort((a, b) => a - b);
    if (JSON.stringify(actualNumbers) !== JSON.stringify(expected)) {
      errors.push(`epic #${parent}: sub-issues differ; actual=${actualNumbers.join(',')} expected=${expected.join(',')}`);
    }
    for (const number of actualNumbers) seen.set(number, (seen.get(number) ?? 0) + 1);
  }
  for (const item of published) {
    if (seen.get(item.number) !== 1) errors.push(`${item.id}: linked to ${seen.get(item.number) ?? 0} epics`);
  }
  for (const number of Object.keys(EXTRA_ISSUE_EPIC_PARENTS).map(Number)) {
    if (seen.get(number) !== 1) errors.push(`supplemental issue #${number}: linked to ${seen.get(number) ?? 0} epics`);
  }
  const issueTraceMissing = fresh.filter((issue) => !hasCompletePromptTrace(issue.body));
  if (issueTraceMissing.length) {
    errors.push(`issues missing prepended prompt provenance: ${issueTraceMissing.map((issue) => `#${issue.number}`).join(', ')}`);
  }
  const issuesWithoutDomain = fresh.filter((issue) => !issueLabelNames(issue).some((label) => DOMAIN_LABELS.has(label)));
  if (issuesWithoutDomain.length) {
    errors.push(`issues missing a Kanban domain label: ${issuesWithoutDomain.map((issue) => `#${issue.number}`).join(', ')}`);
  }
  for (const [numberText, labels] of Object.entries(EPIC_LABELS)) {
    const number = Number(numberText);
    const issue = fresh.find((candidate) => candidate.number === number);
    if (!issue || !sameLabels(issue, labels)) errors.push(`epic #${number}: labels drifted`);
  }
  for (const [numberText, labels] of Object.entries(EXTRA_ISSUE_REQUIRED_LABELS)) {
    const number = Number(numberText);
    const issue = fresh.find((candidate) => candidate.number === number);
    if (!issue || !hasRequiredLabels(issue, labels)) errors.push(`supplemental issue #${number}: required domain labels drifted`);
  }

  const pulls = await allPullRequests();
  const openPullsWithoutDomain = pulls.filter((pull) => pull.state === 'open' && !issueLabelNames(pull).some((label) => DOMAIN_LABELS.has(label)));
  if (openPullsWithoutDomain.length) {
    errors.push(`open pull requests missing a Kanban domain label: ${openPullsWithoutDomain.map((pull) => `#${pull.number}`).join(', ')}`);
  }
  const pullTraceMissing = pulls.filter((pull) => !hasCompletePromptTrace(pull.body));
  if (pullTraceMissing.length) {
    errors.push(`pull requests missing prepended prompt provenance: ${pullTraceMissing.map((pull) => `#${pull.number}`).join(', ')}`);
  }
  if (pulls.length !== Object.keys(PR_ORIGINAL_REQUESTS).length) {
    errors.push(`pull-request provenance catalog count differs; remote=${pulls.length} static=${Object.keys(PR_ORIGINAL_REQUESTS).length}`);
  }
  if (errors.length) throw new Error(`remote validation failed:\n- ${errors.join('\n- ')}`);

  const closed = published.filter((item) => item.state === 'closed').length;
  process.stdout.write(`validated remote: ${fresh.length}/${fresh.length} issues and ${pulls.length}/${pulls.length} pull requests have prepended prompt provenance; ${published.length} requirements (${published.length - closed} open, ${closed} closed); ${Object.keys(EPICS).length} epics; every requirement linked exactly once\n`);
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
    ...originalRequestSection(EPIC_ORIGINAL_REQUESTS[number] ?? []),
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
    `- Repository: [${REPO}](${REPO_URL})`,
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
    lines.push(`## [#${parent} — ${displayTitle}](${REPO_URL}/issues/${parent})`, '', `${epicOpen} open / ${children.length - epicOpen} closed / ${children.length} total`, '');
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
  const missingRequirementDomains = R.filter((item) => !item.labels.some((label) => DOMAIN_LABELS.has(label))).map((item) => item.id);
  const missingEpicDomains = Object.entries(EPIC_LABELS).filter(([, labels]) => !labels.some((label) => DOMAIN_LABELS.has(label))).map(([number]) => number);
  const missingSupplementalDomains = Object.entries(EXTRA_ISSUE_REQUIRED_LABELS).filter(([, labels]) => !labels.some((label) => DOMAIN_LABELS.has(label))).map(([number]) => number);
  if (missingRequirementDomains.length || missingEpicDomains.length || missingSupplementalDomains.length) {
    throw new Error(`domain-label contract drift; requirements=${missingRequirementDomains.join(',') || 'none'} epics=${missingEpicDomains.join(',') || 'none'} supplemental=${missingSupplementalDomains.join(',') || 'none'}`);
  }
  const missingRequirementPrompts = R.filter((item) => !(ORIGINAL_REQUESTS[item.id]?.length)).map((item) => item.id);
  const staleRequirementPrompts = Object.keys(ORIGINAL_REQUESTS).filter((id) => !R.some((item) => item.id === id));
  if (missingRequirementPrompts.length || staleRequirementPrompts.length) {
    throw new Error(`prompt provenance catalog drift; missing=${missingRequirementPrompts.join(',') || 'none'} stale=${staleRequirementPrompts.join(',') || 'none'}`);
  }
  const missingEpicPrompts = Object.keys(EPICS).map(Number).filter((number) => !(EPIC_ORIGINAL_REQUESTS[number]?.length));
  if (missingEpicPrompts.length) throw new Error(`epics missing prompt provenance: ${missingEpicPrompts.join(', ')}`);
  const labels = JSON.parse(gh(['label', 'list', '--repo', REPO, '--limit', '200', '--json', 'name,color']));
  const labelNames = new Set(labels.map((label) => label.name));
  const managedLabels = new Set([
    ...R.flatMap((item) => item.labels),
    ...Object.values(EPIC_LABELS).flat(),
    ...Object.values(EXTRA_ISSUE_REQUIRED_LABELS).flat(),
  ]);
  const missingLabels = [...managedLabels].filter((label) => !labelNames.has(label));
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
    if (!APPLY && needsUpdate) {
      const drift = [];
      if (issue.title !== item.title) drift.push('title');
      if (issue.body !== expectedBody) drift.push('body');
      if (!sameLabels(issue, item.labels)) drift.push('labels');
      if (issue.state.toLowerCase() !== item.state) drift.push('state');
      process.stdout.write(`UPDATE #${issue.number} ${item.id}: ${drift.join(', ')}\n`);
    }
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

  for (const parent of Object.keys(EPICS).map(Number)) {
    const linked = await currentSubIssues(parent);
    const linkedNumbers = new Set(linked.map((issue) => issue.number));
    const parentIssue = parents.get(parent);
    const expectedNumbers = new Set([
      ...published.filter((item) => item.parent === parent).map((item) => item.number),
      ...Object.entries(EXTRA_ISSUE_EPIC_PARENTS)
        .filter(([, expectedParent]) => expectedParent === parent)
        .map(([number]) => Number(number)),
    ]);
    for (const child of linked) {
      const managed = published.find((item) => item.number === child.number);
      const expectedParent = managed?.parent ?? EXTRA_ISSUE_EPIC_PARENTS[child.number];
      if (!expectedParent || expectedNumbers.has(child.number)) continue;
      if (!APPLY) {
        process.stdout.write(`RELINK #${child.number}: remove from epic #${parent}, expected #${expectedParent}\n`);
        continue;
      }
      api('DELETE', `repos/${REPO}/issues/${parent}/sub_issue`, { sub_issue_id: child.id });
      linkedNumbers.delete(child.number);
      process.stdout.write(`unlinked #${child.number} from epic #${parent}\n`);
    }
    for (const childNumber of expectedNumbers) {
      if (linkedNumbers.has(childNumber)) continue;
      if (!APPLY) {
        process.stdout.write(`LINK #${childNumber} under epic #${parent}\n`);
        continue;
      }
      const childIssue = issues.find((issue) => issue.number === childNumber);
      if (!childIssue) throw new Error(`expected epic child #${childNumber} is missing`);
      gqlAddSubIssue(parentIssue.node_id, childIssue.node_id);
      process.stdout.write(`linked #${childNumber} under #${parent}\n`);
    }
  }

  const managedIssueNumbers = new Set([...Object.keys(EPICS).map(Number), ...published.map((item) => item.number)]);
  const supplementalIssueNumbers = issues.filter((issue) => !managedIssueNumbers.has(issue.number)).map((issue) => issue.number).sort((a, b) => a - b);
  const expectedSupplementalNumbers = Object.keys(EXTRA_ISSUE_ORIGINAL_REQUESTS).map(Number).sort((a, b) => a - b);
  const expectedSupplementalLabelNumbers = Object.keys(EXTRA_ISSUE_REQUIRED_LABELS).map(Number).sort((a, b) => a - b);
  if (JSON.stringify(expectedSupplementalLabelNumbers) !== JSON.stringify(expectedSupplementalNumbers)) {
    throw new Error(`supplemental issue label/provenance catalog drift; labels=${expectedSupplementalLabelNumbers.join(',')} provenance=${expectedSupplementalNumbers.join(',')}`);
  }
  if (JSON.stringify(supplementalIssueNumbers) !== JSON.stringify(expectedSupplementalNumbers)) {
    throw new Error(`supplemental issue provenance catalog drift; remote=${supplementalIssueNumbers.join(',')} static=${expectedSupplementalNumbers.join(',')}`);
  }
  await syncSupplementalTraceability(issues);

  if (!APPLY) {
    for (const parent of Object.keys(EPICS).map(Number)) {
      const children = published.filter((item) => item.parent === parent).sort((a, b) => a.number - b.number);
      const desiredState = children.length > 0 && children.every((child) => child.state === 'closed') ? 'closed' : 'open';
      const parentIssue = parents.get(parent);
      const bodyDrift = parentIssue.body !== epicBody(parent, children);
      const stateDrift = parentIssue.state.toLowerCase() !== desiredState;
      const labelDrift = !sameLabels(parentIssue, EPIC_LABELS[parent]);
      if (bodyDrift || stateDrift || labelDrift) {
        process.stdout.write(`UPDATE epic #${parent}: ${[bodyDrift && 'body', stateDrift && 'state', labelDrift && 'domain labels'].filter(Boolean).join(', ')}\n`);
      }
    }
    process.stdout.write(`dry run: ${R.length} normalized requirements; body/title/label/state and hierarchy drift shown above; rerun with --apply to mutate GitHub and write ${OUTPUT}\n`);
    return;
  }

  for (const parent of Object.keys(EPICS).map(Number)) {
    const children = published.filter((item) => item.parent === parent).sort((a, b) => a.number - b.number);
    const desiredState = children.length > 0 && children.every((child) => child.state === 'closed') ? 'closed' : 'open';
    const parentIssue = parents.get(parent);
    const body = epicBody(parent, children);
    if (parentIssue.body !== body || parentIssue.state.toLowerCase() !== desiredState || !sameLabels(parentIssue, EPIC_LABELS[parent])) {
      api('PATCH', `repos/${REPO}/issues/${parent}`, {
        body,
        labels: EPIC_LABELS[parent],
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
