<div align="center"><a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Firmware size and feature tradeoffs

PCController intentionally keeps the ATmega328P image close to its application
ceiling while moving presentation, orchestration, history, and integrations to
the host. Use this guide before adding or removing firmware behavior.

## Evidence rule

Never copy a flash or SRAM number from documentation into a release claim. The
only authoritative values are those produced by the exact candidate build:

- `.build/firmware/firmware-manifest.json` for image identity and flash usage;
- the generated stack-budget section for static and estimated peak SRAM;
- strict Intel HEX bounds validation for application and full-flash images;
- authenticated HELLO plus readback for the physical image actually installed.

A source compile is software evidence. A board identity is deployment evidence.
Neither alone proves the loaded peripherals are safe or behaving correctly.

## Current candidate measurement

This audit describes the exact canonical source candidate in
`.build/firmware`. Its manifest source digest was independently recalculated
over the same 70 firmware files and matches the candidate. The separately
identified deployed image is recorded below rather than being treated as a
byte-identical container merely because it has the same source identity.

| Evidence | Current value |
|---|---:|
| Source/build identity | `800A5B70` |
| Source SHA-256 | `800a5b70865c5894f934638004ed152660225a29e3f54909912e6bb2064a666d` |
| Application HEX SHA-256 | `bb928b7b680d6e393d842bd28412b6760340a2ff61ae40b21a926036358bb092` |
| ELF SHA-256 | `639a73db6b6d5ef096a9fb8e9615537532877d81389180d69b08841fe1c4b95d` |
| `.text` | 32,006 bytes |
| Initialized `.data` image | 226 bytes |
| Linker-reported program | 32,232 bytes (`.text + .data`) |
| Fixed firmware identity | 12 bytes at `0x7DF4..0x7DFF` |
| Application HEX data | 32,244 bytes; highest address `0x7DFF` |
| Static SRAM | 1,432 bytes (`.data` 226 + `.bss` 1,205 + `.noinit` 1) |
| Estimated peak SRAM | 1,761/2,048 bytes |
| Estimated free SRAM | 287 bytes, against the enforced 96-byte minimum |

The peak estimate includes a measured 269-byte serial response path and a
60-byte concurrent INT0/RF allowance. The 616-byte difference between static
allocation and the top of SRAM is therefore not all available for new global
objects.

### Deployed-board checkpoint

The COM18 board authenticated as source identity `800A5B70`. Its
exact deployed application artifact is SHA-256
`bb928b7b680d6e393d842bd28412b6760340a2ff61ae40b21a926036358bb092`.
The primary-owned Urclock transaction backed up, wrote, semantically verified
32,244 programmed bytes, and reauthenticated the application before reporting
success.

The host cleared the durable recovery marker only after reconnecting through
the saved COM18 device Instance ID and completing development EEPROM
reinitialization. The verified board defaults are Silent off, illumination Off,
output persistence off, relay restore mask zero, and motion break 1 ms. The
latest live safe-state sample reported 12.282 V, PWM available, zero active
relays, zero framing/CRC protocol errors, and reset count 22. No optional LCD
was detected; that is a warning-only capability absence rather than a firmware
or recovery failure.

### Bootloader ceilings and the fixed identity gap

The application image already fits both supported boot allocations; no feature
must be removed merely to flash this candidate. Growth headroom is more subtle
than the HEX data-byte count because the identity record has a fixed address:

| Boot profile | Application range | Data-byte headroom | Immediately linkable headroom | Consequence |
|---|---:|---:|---:|---|
| Stock 384-byte Urboot | 0..32,383 | 140 bytes | 12 bytes before `0x7DF4` | The 128 erased bytes at `0x7E00..0x7E7F` are after the fixed identity. They become useful only in a stock-only layout that relocates the identity and gives up drop-in 512-byte compatibility. |
| 512-byte Urboot-Custom | 0..32,255 | 12 bytes | 12 bytes before `0x7DF4` | The identity ends at the final application byte. There is no space after it. |

Thus the current common layout has 12 bytes of practical growth under either
bootloader. Urboot-Custom itself occupies 510 meaningful bytes in a 512-byte
allocation; its two erased bytes do not increase the application ceiling.

### Current EEPROM allocation

The generated safe-default EEPROM artifact deliberately covers all 1,024
bytes, but the firmware does not semantically own every byte:

| EEPROM range | Bytes | Current owner |
|---|---:|---|
| `0..31` | 32 | Unallocated |
| `32..63` | 32 | 31-byte `ControllerSettings` plus CRC |
| `64..307` | 244 | Four-byte RF header plus 20 checksummed 12-byte learned-code records |
| `308..319` | 12 | Unallocated alignment gap |
| `320..703` | 384 | 64-slot, six-byte reset-count journal |
| `704..1023` | 320 | Unallocated |

There are therefore 364 logically unallocated EEPROM bytes. Reducing RF or
reset-journal capacity would free EEPROM only; it would not materially solve
the application-flash ceiling.

## Current measured symbol envelope

AVR LTO folds and inlines page branches, so symbol size is not the same as
removal saving. The current ELF nevertheless gives useful, reproducible bounds:

| Current linked area | Measured bytes | Interpretation |
|---|---:|---|
| `handleMenuAction` | 1,902 flash | All leaf navigation and modal edit behavior; individual pages are inlined into this shared function |
| `serviceDisplay` | 1,444 flash | All ordinary-page and modal rendering; individual page removal cannot claim this whole value |
| Named menu labels/helpers around those two functions | 863 flash | Exact sum of the packed label tables plus named visibility/order/category helpers |
| Total named local-menu envelope | 4,209 flash | Scale only; removing the menu would also remove essential offline control and does not necessarily recover the sum |
| Visibility/order/category named helpers | 424 flash | Lower-bound symbol envelope; their inline dispatcher portions remain unseparated |
| Menu visibility/order fields | 9 SRAM and 9 EEPROM bytes | Exact difference between the current 31-byte settings record and the 22-byte no-layout record |
| MCU macro playback object | 157 static-SRAM bytes | Exact object allocation; flash dispatcher paths are shared |
| Addressable-pixel buffer | 33 static-SRAM bytes | Exact 11-pixel RGB buffer |
| rc-switch receive timings | 134 static-SRAM bytes | Exact pulse-timing array, before receiver/transmitter state |

Only a clean feature-on/feature-off build can convert these envelopes into a
net saving. The table below labels every non-isolated value as an estimate.

## Ownership principle

Firmware owns behavior that must remain safe or useful when the host is absent:

- protocol framing, CRC, bounds, sequencing, and watchdog service;
- input sampling, debouncing, hold/repeat, and emergency stop;
- relay break-before-make and reed interlocks;
- sensor acquisition and essential telemetry;
- persistent board settings and their CRC;
- minimal front-panel status, navigation, and save/discard editing;
- RF receive/transmit and guarded learned-action execution;
- safe output defaults and reset behavior.

The host owns behavior that benefits from memory, a filesystem, networking, or
richer presentation:

- responsive WebUI/TUI rendering, graphs, histories, search, and localization;
- names, categories, icons, color themes, audio arrangements, and help text;
- scripts, macros, automations, global hotkeys, notifications, and bridges;
- optional LCD composition and hosted menu overlays;
- toolchain management, artifacts, backups, programming, and recovery records;
- discovery enrichment, desktop integration, and remote authorization policy.

Moving a feature to the host must not move its hardware safety invariant out of
firmware.

## Features that must not be removed for space

| Area | Required invariant |
|---|---|
| Native protocol | COBS delimiter, CRC-8, sequence correlation, payload bounds, supported-opcode validation |
| Relays and motion | Disable/break/direction/enable sequencing, stop availability, local reed gating |
| Settings | CRC validation, canonical defaults, bounded decode, deferred writes |
| Reset | Safe output initialization, reset-cause capture, watchdog behavior |
| I²C | Startup recovery, bounded transactions, cooperative ownership |
| RF | Record bounds/CRC and no unsafe direct motion mapping |
| Programming | Authenticated handoff state and deterministic boot/application identity |

If one of these no longer fits, reduce optional capability or change the target;
do not silently weaken the invariant.

## Best offload candidates

| Candidate | Firmware responsibility retained | Host responsibility |
|---|---|---|
| Rich LCD pages | Generic bounded I²C transfer and essential status | Text layout, caching, menus, localization |
| Long melodies/effects | One bounded tone/frame operation and silent setting | Sequencing, names, cancellation, timing |
| Telemetry history/graphs | Current samples and timestampable events | Storage, aggregation, charts, export |
| Macro libraries | Safe individual operations and small deterministic queue where needed | Names, long sequences, branching, scheduling |
| Menu metadata | Stable page IDs and minimal local labels | Full titles, descriptions, categories, icons |
| RF catalog UX | Learn/list/map opcodes and guarded execution | Search, formatting, bulk editing, audit history |
| Update workflow | Boot/application identity and protocol handoff | Download, hashing, backup, programmer, verify, recovery |

## Board features that are still genuinely missing

These are implementation gaps in the current AVR candidate, not merely absent
TUI labels:

| Requested board capability | What exists now | Exact missing portion | Planning estimate, not an A/B measurement |
|---|---|---|---:|
| Board-pull hosted menus | The host has six file-watched menu definitions. The AVR supports pushed `DisplayText` capture/release, forwards physical keys, and releases capture after host loss. | AVR opcodes `0x42..0x44` and events `0x9A..0x9B`, the eight-entry RAM directory, generation/state, content request on selection, retry timing, `----`, and terminal failure presentation are not in `ControllerProtocol::Opcode` and no capability advertises them. | 450-850 flash, 30-40 SRAM. The directory alone is exactly 24 bytes for eight `{id,parent,flags}` entries; existing 4+32 display buffers can be reused. |
| EEPROM-configurable audio cues | Door and relay cue families have EEPROM enable bits, but their tones are fixed: door open/closed are 1,700/1,100 Hz for 45 ms; relay on/off are 1,900/1,250 Hz for 35 ms. | Persistent selectable cue IDs or note/frequency/duration definitions for door-open, door-close, relay-on, and relay-off, plus settings/protocol/menu fields. | 180-360 flash, 4-8 SRAM, 12-24 EEPROM for compact single-note descriptors. Arbitrary multi-note EEPROM melodies cost more. |
| Board EEPROM automation | Twenty learned RF records can directly map one code to Key/Menu/Relay/Side/PWM behavior. Host automations can react to all events. | There is no generic EEPROM event-to-action rule table for door, BT Audio, relay, host loss, temperature, or other events; no board rule can invoke RF transmit or a macro on those events. Host-loss handling is fixed, not programmable. | 700-1,400 flash, 16-32 SRAM, 96-192 EEPROM for a small streamed rule table that reuses ordinary opcode validation. |
| Per-state persistent RGB/cue configuration | EEPROM stores one Ready palette index and one global RGB brightness; the host can set a volatile custom RGB value. | Door, BT Audio, RF, Running, warning, hot, fault, and transition colors/effects are fixed in flash rather than independently configurable in board EEPROM. | 180-420 flash, 8-20 SRAM, 24-64 EEPROM, depending on whether only colors or complete effect timing is persisted. |

The current 364-byte unallocated EEPROM area can hold compact cue and
automation records. Flash, not EEPROM, is the limiting resource.

## Menu migration candidates and exact losses

The estimates below are based on the current 4,209-byte named menu envelope
and source branches in the shared 1,902-byte action and 1,444-byte render
functions. They are deliberately ranges: none has a current isolated A/B
build, and ranges must not be added to a release manifest as measured bytes.

| Candidate moved to the host | Estimated net flash | SRAM effect | Functionality and improvement lost on the board |
|---|---:|---:|---|
| `PWM`, `rELY`, `uPWM`, and `r5-8` commissioning/editors as one group | 900-1,500 | 4-10 bytes | No offline channel-by-channel PWM test, relay commissioning, local EEPROM user-PWM editing, or local R5-R8 Toggle/Push control. Direct opcodes, host sliders, macros, output persistence, relay interlocks, and `MOVE` can remain. |
| Advanced fields in the `bEEP` settings editor, retaining a small Mute/Beep action | 300-600 | 1-4 bytes | No local adjustment of open/closed segment brightness, status brightness/Ready color, or voltage/current decimals; stored values still apply and remain host-editable. Removing the whole page also loses offline Silent control. |
| Local RF learning page/UI, retaining RX/TX and stored mappings | 120-260 | 0-6 bytes | The front panel cannot start learning or show learned count/progress; a host is required to learn/cancel. Previously stored mappings still execute locally. |
| Voltage/current/tLED/tBT render pages only | 160-320 | Approximately 0 | Sensors, HOT handling, and telemetry remain, but measurements cannot be read from the four-digit display while the host is absent. |
| Persistent visibility, order, hierarchy, and layout protocol | 500-850 | Exactly 9 static bytes | Loses EEPROM show/hide and reordering, four nested categories, and host `MENU_LAYOUT` read/write. Stable dense pages could still exist in fixed order. The named current lower-bound envelope is 424 flash bytes. |
| Board `MenuList` directory | 80-180 | Approximately 0 | The host must hard-code local IDs/modes/labels and can silently drift from the firmware. Not recommended. |
| Addressable D6 strip | 200-350 | Exactly 33 bytes | Loses all 11 WS2811/WS2812 pixels, fill/per-pixel commands, and future strip effects. Current `show()` alone is 158 linked bytes, so 158 is a lower bound, not the net saving. |
| Smooth status RGB animations/cues, retaining static status colors | 200-420 | 3-10 bytes | Loses eased door/BT/RF/menu/save/discard/reset transitions and breathing/flashing distinctions. Hard warning indications can remain. |
| Buzzer and melodies | 300-550 | About 50 bytes | Loses boot health melody, key feedback, door/relay/save/discard/error cues, host-streamed tones, and the purpose of Silent mode. |
| MCU macro timing queue | 500-900 | Exactly 157 bytes for the playback object | Loses board-clock scheduling, USB-jitter buffering, precise execution deltas, queue/fidelity metrics, synchronized cancellation, and host-loss safe-stop. |
| rc-switch receive/transmit/learning | At least 900 | About 180 bytes | Loses all 433 MHz RX/TX, repeat recognition, learning, 20 mappings, RF events, and RF-driven actions. The 632-byte interrupt handler plus visible support symbols already exceed 900 linked bytes. |

Relay interlocks, stop paths, protocol bounds/CRC, settings CRC, watchdog,
startup I2C recovery, and reset-safe output ordering are excluded from this
list: removing any of them would trade bytes for unsafe behavior.

### Minimum combinations

For the **current feature set alone**, the minimum removal is **none** under
both bootloaders. The image fits, but only 12 bytes can be added before the
fixed identity in the shared layout.

For the missing features above, the smallest defensible planning combinations
are:

| Goal | Estimated new flash | Minimum migration to measure first | Stock 384-byte consequence | 512-byte Urboot-Custom consequence |
|---|---:|---|---|---|
| Structured board-pull hosted menus only | 450-850 | Move the four output commissioning/edit pages (estimated 900-1,500) | Reclaim 438-838 bytes with the shared identity layout, or 310-710 after a stock-only identity relocation contributes 128 bytes. | Reclaim 438-838 bytes; no post-identity reserve exists. |
| Board-pull menus plus compact configurable door/relay cues | 630-1,210 | Same output-editor migration; add the local RF-learning UI migration only if its A/B result is short | Reclaim 618-1,198 bytes shared, or 490-1,070 after stock-only identity relocation. | Reclaim 618-1,198 bytes because only 12 bytes precede the identity. |
| Board-pull menus, compact cues, and generic EEPROM automation | 1,330-2,610 | Output editors + advanced settings fields + local RF-learning UI (combined estimate 1,320-2,360); measure, then add render-only measurement-page migration if still short | Reclaim 1,318-2,598 bytes shared, or 1,190-2,470 after stock-only identity relocation; the proposed migration is not guaranteed at the high estimate. | Reclaim 1,318-2,598 bytes; the full shortfall must come from firmware. |

The last combination is only a **minimum experiment**, not a promise that it
will link. If its isolated A/B result does not cover the chosen concrete rule
schema, removing persistent nested-menu layout would probably close the gap but
would directly undo a requested feature. Prefer a commissioning/production
profile split or a larger-flash MCU over sacrificing motion safety, the macro
queue, RF, buzzer, addressable LEDs, or persistent nested-menu behavior.

## Optional cuts, in preferred order

When the candidate exceeds its budget, measure each change independently. The
usual order is:

1. remove duplicate strings and diagnostics already available from the host;
2. simplify optional local animations while preserving clear state feedback;
3. move rich display composition to the host-owned LCD path;
4. reduce optional convenience aliases, not canonical protocol operations;
5. reduce rarely used local editors only after the setting remains safely
   reachable and visible through a maintained host surface;
6. reconsider the bootloader progress backend or application ceiling only with
   a complete recovery plan.

Do not estimate savings from source line count. AVR linker relaxation, shared
templates, constant folding, vtables, and library selection make intuition
unreliable.

## Measurement procedure

For every proposed tradeoff:

1. Start from a clean candidate source identity.
2. Build with the same locked toolchain, flags, bootloader ceiling, and feature
   profile used for delivery.
3. Record full-image flash, `.data`, `.bss`, `.noinit`, estimated peak SRAM,
   artifact SHA-256, and toolchain identity.
4. Change one feature boundary.
5. Rebuild cleanly and compare manifests, not console recollection.
6. Run the focused protocol/settings/menu/output tests affected by the change.
7. Run the full firmware, host, and Virtual Board compatibility gate.
8. Update the capability matrix and user-facing documentation.
9. If the image will ship, upload that exact image, authenticate it, read it
   back, and perform the applicable physical checks.

## SRAM rules

Flash headroom alone is not enough. Reserve space for:

- the deepest normal call chain;
- interrupt entry and nested library calls;
- UART/native frame buffers;
- Wire/TWI buffers and sensor library state;
- RF receive activity;
- local menu/editor snapshots;
- stack variation during reset, settings, and programming handoff.

Avoid large automatic arrays, recursive flows, `String`, unbounded formatting,
and duplicate frame buffers. Prefer fixed-size structures, explicit maximums,
streaming, and one shared ownership path.

## Protocol compatibility rules

- Stable opcodes and known payload semantics are not removable UI aliases.
- Unknown operations fail explicitly.
- Supported trailing extensions may be ignored only where the protocol says so.
- Exact-shape settings/menu records remain exact; do not invent migration from
  an unsupported EEPROM layout.
- The host and Virtual Board must be updated and tested in the same change as a
  firmware contract change.
- User-editable branding must never alter wire identity.

## Decision record template

Use this compact record in a pull request when a memory tradeoff is made:

```text
Candidate source:
Toolchain/feature profile:
Before flash / static SRAM / peak estimate:
After flash / static SRAM / peak estimate:
User-visible capability changed:
Firmware safety invariant retained:
Host or UI replacement surface:
Focused tests:
Full build manifest:
Physical acceptance required:
```

## Release boundary

An image is not release-ready merely because it fits. It must also preserve the
safety invariants above, pass exact-source automated validation, authenticate on
the target, survive backup/program/verify/restore, and complete the relevant
physical peripheral and loaded-output checks in
[Project Acceptance](Project-Checklist.md).

<p align="center"><a href="../README.md">← Return to the PCController main page</a></p>
