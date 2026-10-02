<div align="center"><a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Firmware autonomy and storage ownership

PCController has one living hardware contract and two cooperating execution
tiers. The board owns bounded hardware engines and immediate offline behavior;
the host owns rich editing, libraries, networking, presentation, and long
orchestration. Both tiers use the same advertised operations rather than
maintaining duplicate effect or macro engines.

## Effect execution boundary

An effect is a PCController-owned definition that may contain relay or motion
steps, PWM output, display or audio actions, RF transmission, addressable-strip
frames, or a combination of those domains. `macro` remains an internal name
for the bounded MCU queue; it is not a second user-facing library.

| Concern | Board responsibility | Host responsibility |
|---|---|---|
| Relay and motion | Break-before-make, door policy, applied state, all-off and emergency-stop enforcement | Semantic board profile, names, editing, recording coordination and long sequences |
| Timed actions | Validate ordinary opcodes and execute the bounded `MacroRing` queue from the MCU clock | Store definitions, compile steps, refill, monitor evidence and present progress |
| Recording | Retain a bounded circular relay capture with MCU timestamps | Combine board evidence with accepted host actions and save the named effect |
| PWM and status light | Direct channel engine and board-owned status policy | Rich overrides, names, previews and effect sequencing |
| Addressable strip | Bounded frame receiver using shared RAM | Render editable effects and stream frames without consuming AVR flash |
| Audio and displays | Bounded tone/display operations and essential local feedback | Libraries, text layout, long arrangements and localization |
| RF | Receive, learn, map and dispatch supported actions | Search, naming, bulk management and audit presentation |

The firmware services one due macro action before accepting ordinary UART
traffic. This prevents a continuous presentation or strip stream from turning
an MCU-timed delta into serial-backlog latency while preserving bounded turns
for UART, physical keys, RF, sensing and watchdog service.

## Host loss and cancellation

Disconnect never transfers ownership to guessed state. The board cancels a
host-dependent active queue, returns macro-owned outputs to their safe state,
and continues its advertised local policy. A host reconnects by authenticating,
reading the current profile/capabilities, and reconciling retained capture or
live output state; it does not replay stale commands into a replacement board
generation.

The host binds every in-flight macro request and cleanup action to the original
authenticated connection generation. VirtualBoard and native tests cover
replacement-session rejection, queue validation, cancellation, retained
capture decoding, timing evidence, and event convergence.

## Persistent storage ownership

Storage is assigned by mutability and failure semantics, not simply by free
bytes.

| Storage | Current contents | Must not contain |
|---|---|---|
| Application EEPROM | Audio cue record, boot opcode record, settings and board name, learned RF records, reset journal, and the selected tail profile | Host secrets, an unbounded effect library, or duplicate user-facing catalogs |
| Application identity footer | Exact source identity, build time and selected profile identity | Mutable settings or board-specific user data |
| Urboot metadata | Bootloader-owned facts used for guarded programming | Application settings, board name, effects, or mutable hashes |
| Host configuration/data | Unified effect library, rich names/categories, profiles, UI policy, history, integrations and backups | The only copy of a board-enforced interlock |

The default EEPROM map is defined by `Project/EepromLayout.h`. Bytes `0..31`
belong to audio cues and the boot-opcode record; the 41-byte settings/name
record occupies `32..72`; learned RF starts at `80`; the reset journal starts
at `336`; and the EEPROM tail is selected at compile time for status profiles
plus menu labels or for the dual-bank board-automation store. Static assertions
must reject every overlap.

Persistent records use CRC or a commit marker and deterministic invalid-record
fallbacks. The automation store and menu-label store publish a new generation
or commit marker only after its payload is complete. Programming tooling keeps
raw EEPROM backup and readback evidence separate from semantic settings, and
does not describe a short semantic response as a full EEPROM image.

## Alpha contract policy

There is no legacy product contract to preserve. Before a deliberate release,
PCController maintains one living wire and storage contract. Development
layouts are replaced directly; firmware does not accumulate old decoders,
aliases, versioned endpoints, or migration chains.

Preservation still matters: before a destructive programming operation the Go
tooling captures the complete raw EEPROM and firmware evidence required for
recovery. A controlled development reinitialization may translate explicitly
selected live values, but that tooling transaction does not make an obsolete
layout part of the running firmware.

## Capacity rule

The ATmega328P image is constrained by the fixed application identity boundary.
Every recovery or new feature must pass the exact default-profile flash and SRAM
gate. A coherent historical implementation may therefore be valuable without
being safe to copy verbatim. Prefer the smallest current-architecture behavior,
or keep the implementation in the host, rather than weakening identity,
interlocks, cancellation, watchdog service or truthful capability reporting.

For the current measured image and exact EEPROM allocation, see
[Memory and Feature Tradeoffs](Memory-and-Feature-Tradeoffs.md). For effect
recording, playback and import/export usage, see
[Host Macro Recording](Host-Macro-Recording.md).

## Change checklist

Every ownership or profile change records:

- the board engine retained and the host policy added or removed;
- exact default-profile flash, static SRAM, peak SRAM and EEPROM deltas;
- the capability/profile fields that change;
- disconnect, cancellation, torn-write and replacement-session behavior;
- focused native/VirtualBoard evidence and any remaining physical-board gate.

<p align="center"><a href="README.md">← Return to the documentation hub</a></p>
