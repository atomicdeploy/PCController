<div align="center"><a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Firmware autonomy, host extensions, and persistent storage

PCController uses one reusable operation engine in two operating tiers:

- the board owns the bounded, immediate, safety-critical behavior that must
  continue when USB/UART or the host is absent;
- the host adds richer policy, editing, presentation, history, networking, and
  long-running orchestration by calling those same board operations.

The host is an extension of the controller, not a replacement for its safety
core. A disconnected host must never prevent a physical stop, make motion
interlocks unavailable, or leave an active macro output uncontrolled.

## Engine and policy are separate layers

An **engine** is the small reusable mechanism that performs a bounded hardware
operation. A **controller or policy** decides when to invoke it and with which
data. Build profiles may omit autonomous policy tables and local editors while
retaining the engine and its protocol operation.

| Domain | Reusable board engine | Optional policy or data |
|---|---|---|
| Audio | `TonePlayer` and the bounded buzzer operation | `AudioCues`, startup/navigation melodies, and host or EEPROM cue definitions |
| PWM and LEDs | direct PCA9685 channel, RGB, strip, and power-indicator operations | idle/connected/fault animation policy, effect descriptors, and local editors |
| Motion and relays | break-before-make relay controller, reed interlock, stop, and all-off | front-panel mapping, macro sequences, and host automation rules |
| MOSFET outputs | bounded direct channel operation and safe reset/cancel behavior | names, presets, fades, schedules, and rich host presentation |
| Macros | validated ordinary operations, MCU timestamps, bounded circular capture, and deterministic playback | names, categories, long libraries, editing, history, and scheduling |
| Scheduling | cooperative task mechanism when the profile enables it | registered tasks and higher-level schedules; host scheduling remains available |

For example, `TonePlayer` is the engine; `AudioCues` is the autonomous
controller that selects cue timing and note data. Disabling autonomous cues
must not remove host-streamed tones, macro-recorded buzzer operations, Silent,
or the bounded tone engine itself.

## Minimum autonomous behavior

The production profile keeps these behaviors available without a host:

- immediate physical-key sampling and motion stop;
- relay break-before-make, direction arbitration, reed/door interlocks, reset
  all-off behavior, and watchdog service;
- recording physical/RF/board actions with MCU timestamps and replaying a
  retained bounded capture through the ordinary opcode dispatcher;
- persistent Silent, safe settings, RF mappings, and per-board probe identity;
- essential TM1637 state, programming indication, and unambiguous save/error
  feedback;
- direct buzzer, PWM/MOSFET, power LED, status RGB, addressable strip, and
  motion engines;
- the power indicator returning to full brightness after host loss;
- sensor acquisition and HOT/safety handling that does not depend on the host;
- cancellation/offline safe-stop that restores every macro-owned output
  domain to its documented safe state.

Rich animations are never a substitute for these invariants. If the image no
longer fits, omit optional policy data or choose a larger MCU; do not weaken
motion, cancellation, persistence, framing, or watchdog safety.

## Host-required and host-enhanced behavior

The exact production-candidate costs belong in
[Memory and Feature Tradeoffs](Memory-and-Feature-Tradeoffs.md). The final
candidate must update that table from clean feature-on/feature-off builds.

| Behavior | Offline result in the compact profile | What a connected host adds |
|---|---|---|
| LCD presentation | Generic bounded I2C remains available; no false native-renderer capability is advertised | PCF8574/HD44780 discovery, initialization, layout, and text rendering |
| Status RGB | Direct RGB/strip engines remain | Rich priority arbitration, configurable colors, easing, history, and UI preview |
| Power indicator | Board restores full brightness after disconnect | Boot fade-in and periodic idle dim pulses driven through the direct channel engine |
| Audio | Tone engine, Silent, macro buzzer steps, and any enabled compact autonomous cue bank remain | JSON melody library, streaming, editing, naming, previews, and long arrangements |
| Macros | Bounded circular recording/playback with exact MCU deltas remains | Durable library, names, categories, colors, editing, pagination, scheduling, and cross-instance APIs |
| DS18B20 roles | EEPROM ROM-to-role mapping is usable after commissioning | Assisted heating/cooling correlation, confidence display, and mandatory user confirmation/manual fallback |
| Local commissioning pages | Only pages enabled by the selected build profile exist | Complete TUI/WebUI/CLI configuration and diagnostics |
| RF | Receive, learning, mappings, and mapped actions remain | Search, bulk editing, audit history, and remote management; blocking transmit stays unavailable until cooperative TX exists |
| Long automation | Immediate safety reactions and bounded board mappings remain | Filesystem-backed rules, network events, OS actions, and long schedules |

The host must query truthful HELLO profile/capability fields on every connect.
It must not assume that a capture page, scheduler, autonomous cues, native LCD
renderer, RF transmitter, or local editor exists merely because a related
hardware engine exists.

## Storage ownership

Choose storage by mutability and failure semantics, not merely by available
bytes.

| Storage | Appropriate contents | Inappropriate contents |
|---|---|---|
| Application EEPROM | board name, Silent/settings, atomic settings banks, learned RF records, DS18B20 ROM roles, bounded cue/melody data, reset journal, other mutable per-board state | host secrets, unbounded libraries, build identity |
| Application flash identity footer | exact application source/content identity, build time, profile/layout contract needed before HELLO | mutable settings or board-specific commissioning data |
| Urboot property metadata | immutable bootloader facts already defined by Urboot, such as allocated boot pages, vector-bootloader slot/jump information, and feature encoding | application hash that changes on every UART upload, board name, DS roles, macros, melodies, or user settings |
| Host data/config | long macro and melody libraries, names/categories, histories, UI policy, networking, automation, backups, and migration tooling | the only copy of a safety-critical board setting |

### Why mutable data does not belong in bootloader metadata

The selected Urboot image exposes the
[standard six-byte property table](https://github.com/stefanrueger/urboot/blob/main/urprotocol.md#details-of-the-urprotocol)
at the top of boot flash. It already encodes the Urboot version, capabilities,
optional application page-writer jump, vector slot, and boot-page count. Those
bytes describe the installed bootloader and participate in vector-aware
programming; they are not a general-purpose mutable store. A
normal UART application upload changes the application without rewriting the
protected bootloader, so storing the application hash or settings there would
be stale or would require an unsafe ISP bootloader rewrite for routine changes.

PCController should consume and verify the existing Urboot metadata more
fully, and may define additional immutable fields only in a separately measured
bootloader profile with an explicit compatibility registry. It must never
silently repurpose upstream fields or trade away boot protection. Unknown or
stock bootloaders need a conservative fallback.

The corresponding backlog item must cover:

1. decode the stock and custom Urboot property table into a typed generated
   contract;
2. expose it in backup manifests, CLI/TUI/WebUI diagnostics, and programming
   preflight;
3. compare boot region, vector, write-page, EEPROM, and protection facts with
   the selected FQBN/profile and fail closed on dangerous mismatches;
4. measure whether any additional immutable PCController field can fit without
   enlarging the boot region or reducing application space;
5. preserve the application identity footer and application HELLO as the
   authoritative per-firmware identity;
6. cover stock, custom, unknown, corrupt, and future-metadata fixtures without
   adding mutable user data to boot flash.

## EEPROM data-driven autonomy

Moving **data** to EEPROM can preserve offline response while reducing
flash-resident tables. It does not remove the flash cost of the decoder,
validator, scheduler, or hardware engine.

Candidate records must be bounded, CRC-protected, written cooperatively, and
published only after the final hardware write has completed and read back.
Settings use redundant banks so a power loss cannot invalidate the last good
record. Variable records use header/CRC-last publication or another tested
transaction with deterministic torn-write recovery.

Good candidates include:

- compact note/duration cue or melody steps interpreted by `TonePlayer`;
- DS18B20 ROM-to-role identity unique to each enclosure;
- colors, brightness, fade rates, and bounded effect parameters consumed by
  the existing LED/PWM engines;
- small motion/MOSFET/audio macro captures using the ordinary validated action
  registry.

Poor candidates include large names, categories, help text, arbitrary scripts,
long macro libraries, or duplicated bytecode engines. Those remain host data.

An EEPROM melody bank is enabled only if the exact final profile proves enough
EEPROM, flash, stack, and write-latency margin after settings durability, DS
identity, and macro safety. Otherwise the host JSON example library and a
recorded buzzer macro are the canonical implementations, and EEPROM melodies
remain an explicitly measured backlog item.

## DS18B20 role commissioning

Temperature roles are keyed by the sensor's CRC-valid 64-bit ROM, not by
discovery order or a permanent swap bit. The assisted host workflow is:

1. read both ROMs and a stable baseline;
2. ask the user for permission to run the identification test;
3. drive enclosure illumination to maximum even if the door is closed, solely
   for this bounded commissioning operation;
4. correlate each sensor's rise, then drive illumination to zero and correlate
   cooling;
5. show confidence and the raw ROM/temperature traces;
6. require user confirmation, or ask the user to choose manually when the
   result is missing, tied, noisy, or otherwise ambiguous;
7. persist the chosen ROM-to-role mapping atomically and verify readback.

This commissioning override must restore the prior illumination state on
success, error, cancellation, disconnect, and timeout. It never bypasses HOT
handling or motion safety.

## Alpha compatibility policy

Before the first deliberate release, storage and wire schema numbers are
development diagnostics, not compatibility promises. An alpha version may
replace its EEPROM, protocol, or flash layout directly. Do not accumulate
version-to-version decoder chains, aliases, or baggage for unpublished builds.

Two exceptions do not create a compatibility promise:

- a one-time, host-side semantic conversion used to preserve the currently
  connected board's explicitly backed-up settings during a controlled flash;
- concurrently supported **feature/profile** builds whose different layouts
  are advertised and intentionally maintained at the same time.

At the first actual release, define supported upgrade sources, durable schema
identities, migration fixtures, and a retirement policy. Until then, preserve
raw backups and favor direct, tested layout replacement.

## Change checklist

Every feature-profile change records:

- the engine that remains and the policy/data that changed;
- exact flash, initialized data, BSS, peak stack/SRAM, and EEPROM deltas from a
  clean build;
- truthful HELLO capability/profile changes;
- offline behavior lost or gained;
- the host surface that replaces or extends it;
- persistence, torn-write, cancellation, host-loss, and latency tests;
- whether the physical board still needs sight, sound, sensor, or loaded-output
  acceptance.

<p align="center"><a href="README.md">← Return to the documentation hub</a></p>
