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

This audit describes the current **pre-final aggregate candidate** in
`.build/firmware`. It is exact build evidence for the source aggregation at the
time shown, but it is not yet the frozen release or proof of what is deployed
on the board. A later frozen build timestamp changes the 12-byte identity record
and can therefore change the application artifact SHA-256 even when the linked
sketch size is unchanged. The separately identified deployed image is recorded
below rather than being treated as byte-identical merely because it is related
to the same source tree.

| Evidence | Current value |
|---|---:|
| Embedded build identity | `6B075495` |
| Independently aggregated source SHA-256 | `6b075495cb0d270ca8e5b65b86354fda18e66fd5605d765e9960abf5c2c0ac13` |
| Application HEX SHA-256 | `7fc43de5c342cd02a9c0d604e84972c4a3614d8192da85fc25f62dd922487cbd` |
| `.text` | 32,140 bytes |
| Initialized `.data` image | 204 bytes |
| Linker-reported sketch program | 32,344 bytes (`.text + .data`) |
| Profile-derived firmware identity | 12 bytes at `0x7E74..0x7E7F` |
| Application HEX data | 32,356 bytes; highest address `0x7E7F` |
| Stock 32,384-byte application-range free | 28 bytes |
| Immediately linkable before identity | 28 bytes |
| Static SRAM | 1,474 bytes (`.data` 204 + `.bss` 1,269 + `.noinit` 1) |
| Estimated peak SRAM | 1,779/2,048 bytes |
| Estimated free SRAM | 269 bytes, against the enforced 96-byte minimum |

The peak estimate includes a measured 245-byte serial response path and a
60-byte concurrent INT0/RF allowance. The 574-byte difference between static
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

### Bootloader ceilings and profile-derived identity

The application image fits the selected stock-core boot allocation. The
identity footer is computed from that profile's generated application limit;
it is not a universal address shared with every bootloader profile:

| Boot profile | Application range | Data-byte headroom | Immediately linkable headroom | Consequence |
|---|---:|---:|---:|---|
| Stock 384-byte Urboot | 0..32,383 | 28 bytes | 28 bytes before `0x7E74` | Current selected profile; the identity occupies the final 12 application bytes. |
| 512-byte Urboot-Custom | 0..32,255 | Does not fit | At least 100 bytes must be reclaimed | A custom feature profile must link at or below 32,244 bytes so its own 12-byte identity can occupy `0x7DF4..0x7DFF`. |

Thus the stock profile has 28 bytes of practical growth. Urboot-Custom itself
occupies 510 meaningful bytes in a 512-byte allocation; its two erased bytes do
not increase the application ceiling. It remains a separate feature profile,
not a compatibility promise for this alpha version build.

### Current EEPROM allocation

The generated safe-default EEPROM artifact deliberately covers all 1,024
bytes, but the firmware does not semantically own every byte:

| EEPROM range | Bytes | Current owner |
|---|---:|---|
| `0..31` | 32 | Semantically free; factory EEPROM pre-provisions an ignored boot record |
| `32..72` | 41 | 31-byte `ControllerSettings`, one name length, eight name bytes, and CRC |
| `73..79` | 7 | Unallocated alignment gap |
| `80..323` | 244 | Four-byte RF header plus 20 checksummed 12-byte learned-code records |
| `324..335` | 12 | Unallocated alignment gap |
| `336..719` | 384 | 64-slot, six-byte reset-count journal |
| `720..966` | 247 | Nineteen 12-byte status-effect condition descriptors, each with CRC |
| `967..1023` | 57 | Unallocated |

There are therefore 108 semantically free EEPROM bytes. The generated factory
image deliberately pre-provisions `0..31` for the optional boot profile, but
feature-off firmware ignores that record; it is not an active default-layout
owner. Reducing RF or reset-journal capacity would free EEPROM only; it would
not materially solve the application-flash ceiling.

### Draft EEPROM boot-opcode profile

The default delivery profile keeps the hard-coded startup melody and ignores
the pre-provisioned `0..31` record. The experimental, disabled-by-default
`eeprom-boot-opcodes` profile reserves that 32-byte slot for a CRC-validated,
commit-last record:

```text
0..4    magic, schema, used-byte count, CRC-8
5..30   compact presentation-only opcode groups
31      commit marker, written last
```

It runs only after normal watchdog, safe-output, settings, display, sensor,
and radio initialization, and it is skipped in Programming Mode. A valid
record may issue only Buzzer, Status RGB, or Status Effect frames through the
ordinary opcode dispatcher. Blank, torn, corrupt, malformed, or unsafe data
is a quiet no-op; boot reads never write EEPROM. The factory image stores the
existing six-step welcome melody in the 26-byte data area.

This is a configurability experiment, **not** a current flash-saving profile.
On the fixed-identity `0x5EED0001` source build after the runtime-layout
refactor, the base compiler report is 32,316 bytes; its identity-aware
manifest occupies 32,328/32,384 bytes and leaves 56 physical bytes free.
The enabled measurement-only image occupies 32,738 bytes (+410) and uses
1,471 static SRAM bytes (+1), which is 354 bytes above the delivery ceiling.
The normal enabled build therefore fails its identity/link gate as intended
and must not be flashed. A future feature profile needs a measured ≥354-byte
flash recovery before this source can become release-ready.

The host tool accepts only named feature selections, for example
`--firmware-feature eeprom-boot-opcodes`; the selection is included in the
source identity, cache path, build hash, and firmware manifest. Arbitrary raw
compiler flags are intentionally rejected.

## Current measured symbol envelope

AVR LTO folds and inlines page branches, so symbol size is not the same as
removal saving. The current ELF nevertheless gives useful, reproducible bounds:

| Current linked area | Measured bytes | Interpretation |
|---|---:|---|
| `handleMenuAction` | 1,956 flash | All leaf navigation and modal edit behavior; individual pages are inlined into this shared function |
| `serviceDisplay` | 1,468 flash | All ordinary-page and modal rendering; individual page removal cannot claim this whole value |
| Named menu labels/helpers around those two functions | 741 flash | Exact sum of the packed label tables plus named visibility/order/category helpers |
| Total named local-menu envelope | 4,165 flash | Scale only; removing the menu would also remove essential offline control and does not necessarily recover the sum |
| Visibility/order/category named helpers | 370 flash | Lower-bound symbol envelope; their inline dispatcher portions remain unseparated |
| Menu visibility/order fields | 9 SRAM and 9 EEPROM bytes | Exact difference between the current 31-byte settings record and the 22-byte no-layout record |
| Operator board name | 9 SRAM and 9 EEPROM bytes | One length byte plus eight printable ASCII bytes inside the current profile's CRC-backed settings record |
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

The compact on-board motion-door policy editor is no longer a gap. Board
Settings item `SAFE` rolls through numeric values 0 Always, 1 Closed only,
2 Open only, and 3 Never, previews the fail-safe gate immediately, and uses the
same atomic Save/Discard transaction as the other local settings. Folding the
previously duplicated category-label renderer into the shared packed-label
path more than paid for it: compared with candidate `800A5B70`, application
and full-flash data both fell by 26 bytes, static SRAM rose by one byte, and the
EEPROM layout did not change.

| Requested board capability | What exists now | Exact missing portion | Cost/evidence |
|---|---|---|---:|
| Board-pull hosted menus | The host has six file-watched menu definitions. The AVR supports pushed `DisplayText` capture/release, forwards physical keys, and releases capture after host loss. | AVR opcodes `0x42..0x44` and events `0x9A..0x9B`, the eight-entry RAM directory, generation/state, content request on selection, retry timing, `----`, and terminal failure presentation are not in `ControllerProtocol::Opcode` and no capability advertises them. | 450-850 flash, 30-40 SRAM. The directory alone is exactly 24 bytes for eight `{id,parent,flags}` entries; existing 4+32 display buffers can be reused. |
| EEPROM-configurable buzzer cues | Door and relay cue families have EEPROM enable bits, but their tones are fixed: door open/closed are 1,700/1,100 Hz for 45 ms; relay on/off are 1,900/1,250 Hz for 35 ms. | Persistent selectable cue IDs or note/frequency/duration definitions for door-open, door-close, relay-on, and relay-off, plus settings/protocol/menu fields. | Two retained A/B measurements bound the choice: the compact five-byte choice-table candidate used 33,032 program, 1,442 static-SRAM, and 5 EEPROM bytes; the full four-descriptor candidate used 33,238 program, 1,455 static-SRAM, and 13 EEPROM bytes. Against the current 32,244-byte fixed-identity boundary, they exceed it by 788 and 994 bytes respectively. Neither candidate is shippable in the shared image layout. |
| Board EEPROM automation | Twenty learned RF records can directly map one code to Key/Menu/Relay/Side/PWM behavior. Host automations can react to all events. | There is no generic EEPROM event-to-action rule table for door, BT Audio, relay, host loss, temperature, or other events; no board rule can invoke RF transmit or a macro on those events. Host-loss handling is fixed, not programmable. | 700-1,400 flash, 16-24 SRAM, and about 108 EEPROM bytes for eight compact rules plus an atomic header and CRC that reuse ordinary opcode validation. |

The current 108-byte unallocated EEPROM area can hold compact cue and
automation records. Flash, not EEPROM, is the limiting resource.

## Menu migration candidates and exact losses

The estimates below are based on the current 4,165-byte named menu envelope
and source branches in the shared 1,956-byte action and 1,468-byte render
functions. They are deliberately ranges: none has a current isolated A/B
build, and ranges must not be added to a release manifest as measured bytes.

| Candidate moved to the host | Estimated net flash | SRAM effect | Functionality and improvement lost on the board |
|---|---:|---:|---|
| `PWM`, `rELY`, `uPWM`, and `r5-8` commissioning/editors as one group | 900-1,500 | 4-10 bytes | No offline channel-by-channel PWM test, relay commissioning, local EEPROM user-PWM editing, or local R5-R8 Toggle/Push control. Direct opcodes, host sliders, macros, output persistence, relay interlocks, and `MOVE` can remain. |
| Advanced fields in the `bEEP` settings editor, retaining a small Mute/Beep action | 300-600 | 1-4 bytes | No local adjustment of open/closed segment brightness, status brightness/Ready color, or voltage/current decimals; stored values still apply and remain host-editable. Removing the whole page also loses offline Silent control. |
| Local RF learning page/UI, retaining RX/TX and stored mappings | 120-260 | 0-6 bytes | The front panel cannot start learning or show learned count/progress; a host is required to learn/cancel. Previously stored mappings still execute locally. |
| Voltage/current/tLED/tBT render pages only | 160-320 | Approximately 0 | Sensors, HOT handling, and telemetry remain, but measurements cannot be read from the four-digit display while the host is absent. |
| Persistent visibility, order, hierarchy, and layout protocol | 500-850 | Exactly 9 static bytes | Loses EEPROM show/hide and reordering, four nested categories, and host `MENU_LAYOUT` read/write. Stable dense pages could still exist in fixed order. The named current lower-bound envelope is 370 flash bytes. |
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

For the **current stock-core feature profile**, the minimum removal is
**none**. It has 28 bytes before its derived identity footer. The current image
does not fit the 512-byte Urboot-Custom profile; that separate profile needs at
least 100 bytes reclaimed before it can reserve its own footer.

For the missing features above, the smallest defensible planning combinations
are:

| Goal | Estimated new flash | Minimum migration to measure first | Stock 384-byte consequence | 512-byte Urboot-Custom consequence |
|---|---:|---|---|---|
| Structured board-pull hosted menus only | 450-850 | Move the four output commissioning/edit pages (estimated 900-1,500) | Reclaim about 424-824 bytes beyond the current 26-byte reserve. | Reclaim about 552-952 bytes including the custom profile's existing 102-byte deficit. |
| Board-pull menus plus compact configurable door/relay cues | 630-1,210 | Same output-editor migration; add the local RF-learning UI migration only if its A/B result is short | Reclaim about 604-1,184 bytes beyond the current reserve. | Reclaim about 732-1,312 bytes including the profile deficit. |
| Board-pull menus, compact cues, and generic EEPROM automation | 1,330-2,610 | Output editors + advanced settings fields + local RF-learning UI (combined estimate 1,320-2,360); measure, then add render-only measurement-page migration if still short | Reclaim about 1,304-2,584 bytes beyond the current reserve. | Reclaim about 1,432-2,712 bytes including the profile deficit; the proposed migration is not guaranteed at the high estimate. |

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

## Alpha version and feature-profile compatibility rules

- A new alpha **version build** may directly replace an unpublished protocol,
  flash, or EEPROM layout. Do not add a migration chain, dual decoder,
  compatibility alias, or preservation branch solely for an earlier alpha
  version; retain the raw backup and reinitialize explicitly.
- Compatibility/preservation is implemented only for distinct
  **profile/feature builds that remain supported concurrently**. Those profiles
  advertise capabilities and declare their application limit, identity address,
  and persistence layout independently.

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
