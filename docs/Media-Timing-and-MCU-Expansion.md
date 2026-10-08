# Media timing and MCU expansion roadmap

PCController can grow beyond the ATmega328P without undoing useful compaction
or splitting its protocol and safety behavior into separate implementations.
The proposed path is one shared production core, explicit hardware targets,
measured feature bundles, and truthful capability reporting. Diagnose the
non-working strip independently before selecting a replacement output backend.

This roadmap reconciles the 8 October 2026 discussion titled **Explain media
clock command**, including its firmware-capacity and larger-MCU follow-ups.
It preserves requirements and engineering findings, not a raw private transcript.
The conversation reader returned three turns, but clipped the last answer at
20,000 characters within its successor checklist. **Full source preservation
remains pending the missing remainder.** Do not close that acceptance item from
this document alone.

## Current support and source boundaries

The reviewed main checkpoint is [b599b161](https://github.com/atomicdeploy/PCController/commit/b599b161).
Its [toolchain profile](../Tools/Controller/toolchain-profile.json) selects
MiniCore, ATmega328P, 16 MHz, UART Urboot/Urclock, a 32,384-byte application
range, 32,768-byte flash and 1,024-byte EEPROM. Portable domain helpers and
retained optional implementations do **not** constitute a supported ATmega2560
or STM32 build/programming target.

The prepared media-clock implementation discussed below is also represented
in active integration work: [PCController PR #607](https://github.com/atomicdeploy/PCController/pull/607)
and [Pealayer PR #87](https://github.com/ToghrolTP/pealayer/pull/87).
Do not infer that their source, a staged build, deployed host, physical firmware
and merged main are the same checkpoint.

## Media clock and prepared effects

The media clock represents playback position, loaded/playing state, rate,
sequence, epoch and prepared-plan revision. It is not an AVR oscillator or RTC.
libmpv supplies the media position; the host uses monotonic anchors between
samples; physical output timing still requires acknowledgement and measurement.

Pealayer prepares effect references and direct channel actions in PCController
RAM before playback. A matching plan acknowledgement and paused-clock epoch
acknowledgement arm the plan. Seeks create a new epoch; old acknowledgements and
old one-shots must not be reused. The host executes the prepared commands and
faults on stale clock, NACK, changed board session or excessive ACK lateness.
Paused/resumed and rebased actions must remain distinct in the execution ledger.

| Timing quantity in the reviewed integration | Meaning |
| --- | --- |
| 20 ms | Independent libmpv observer polling target, not a presentation timestamp guarantee |
| 40 ms while playing | Nominal media-clock publish interval; paused updates normally use one second, with state changes bypassing the interval |
| 50 ms | Default maximum action ACK lateness; the host plan accepts 5–250 ms |
| 250 ms | Prepared executor clock freshness and Pealayer playback-feedback gate |
| Three seconds | PCController media-presence/nonexclusive authority lease and leased board segment presentation |

The original explanation conflated the 250 ms execution gate with all clock
leases. These limits have different owners and purposes; preserve that
distinction in documentation, diagnostics and tests.

In the reviewed firmware candidate, native `MEDIA_CLOCK` is opcode `0x4A`.
The AVR handler validates a bounded 14-byte payload and reuses the leased
seven-segment presenter with four raw cells. It does **not** run an absolute
video-epoch hardware timeline or retain the host's full epoch/revision contract.
An older image may return Unsupported while serial transport is still healthy.
Firmware capability/identity mismatch must not be called a physical disconnect.

The historical host/firmware mismatch and guarded update are recorded in
[Pealayer's deployment checkpoint](https://github.com/ToghrolTP/pealayer/blob/release/preferences-organization/docs/verification/DEPLOYMENT-AND-MERGE-CHECKPOINT.md).
That checkpoint also records an ACK exceeding the 50 ms gate: actual precise
hardware playback remains an acceptance requirement, not a perfect-timing claim.
See [prepared hardware timeline](https://github.com/ToghrolTP/pealayer/blob/release/preferences-organization/docs/prepared-hardware-timeline.md)
and [publishing authority](https://github.com/atomicdeploy/PCController/blob/feat/media-authority/docs/MEDIA-AUTHORITY.md).

## How the AVR regained capacity

[PR #427](https://github.com/atomicdeploy/PCController/pull/427) combined MCU
capture/playback with a 100-pixel strip workspace. Its opening report records a
32,240-byte baseline and a 33,896-byte combined image, 1,512 bytes above the
32,384-byte application range. These are historical measurements, not the
current release's free space.

| Historical checkpoint | Evidence and qualification |
| --- | --- |
| BSS-safe keys and dormant scheduler gate | [7b224bb50 report](https://github.com/atomicdeploy/PCController/pull/427#issuecomment-5889570010): 398 flash and 73 SRAM bytes saved; full profile still over budget |
| Ring/counter and compiler-option compaction | [5125d2d2d report](https://github.com/atomicdeploy/PCController/pull/427#issuecomment-5889918178): redundant ring state removed, incremental counters and wrap-safe RF countdown, counterproductive split-wide-types flag removed |
| Full default profile fits | [974dc2ee8 report](https://github.com/atomicdeploy/PCController/pull/427#issuecomment-5890634133): 32,262 bytes reported, 1,520 static SRAM, 282-byte estimated stack margin; full RF administration retained |
| EEPROM menu-label profile fits | [2f72e0757 report](https://github.com/atomicdeploy/PCController/pull/427#issuecomment-5890800068): 32,338 bytes reported, shared ACK/error serialization and selected-relay reads |

Flash accounting needs care. `32,384 - 32,262` is 122, not 110; the historical
110-byte report corresponds to reserving another 12 bytes for identity. Likewise
`32,384 - 32,338` is 46, while the report says 34. Do not compare a linker sketch
number with identity-inclusive HEX data without checking the artifact convention.
The independently documented later candidate distinguishes a 32,248-byte sketch
plus 12-byte identity from 32,260 bytes of HEX data and 124 bytes free. All of
these are build-specific. Fresh ELF/HEX/identity/stack measurements govern release.

The conversation's 33,896-to-32,262 difference is 1,634 bytes arithmetically, but
an aggregate difference is not a sum of isolated per-feature savings. The
temporary `macro-strip-test` profile omitted new RF learning administration;
it was not accepted as a permanent substitute for the full default profile.

### Compaction to retain

- Zero-initialized globals with explicit `begin()` binding instead of expensive
  constructors/nonzero startup data; preserve exact defaults and reset behavior.
- One RF `RCSwitch` object for RX/TX with deliberate receive disable/re-enable.
- Shared PROGMEM menu transitions, label rendering and ACK/error helpers.
- One authoritative ring state, incremental recording counts and wrap-safe
  countdowns instead of repeated AVR division/modulo.
- RGB-triple walking and direct RF wire serialization into bounded existing
  response buffers rather than temporary copies.
- Measured inlining and LTO/compiler choices; retain a flag only when the exact
  candidate's size, timing and correctness results justify it.
- Host-owned rich names, icons, histories, libraries, localization, integrations
  and LCD composition, without moving hardware safety out of firmware.
- Bounded buffers and shared protocol definitions rather than duplicate state.

The existing [memory guide](Memory-and-Feature-Tradeoffs.md) retains LCD/offload
costs, EEPROM allocations and ownership-loss estimates. Its older symbol and
profile measurements are planning evidence, not today's manifest.

## Retained feature gates and restoration choices

These defaults were inspected in main's [ProjectConfig.h](../ProjectConfig.h).
Source retention means recoverable implementation, not that every combination
is commissioned or safe to enable together.

| Gate or feature | Current default and retained source | Expanded-target work |
| --- | --- | --- |
| `ENABLE_I2C_LCD` | 0; `LocalLib/I2cLcd.*` | Useful autonomous renderer alongside explicit host LCD ownership; remeasure historical ~1,328 flash/49 SRAM cost |
| `ENABLE_TASK_SCHEDULER` | 0; `LocalLib/Tasks.*` | Register and test cooperative jobs; merely enabling an unused scheduler adds no behavior |
| `ENABLE_LOCAL_AUDIO_CUES` | 1; `Project/AudioCues.*` | Preserve physical-state feedback, TonePlayer and Silent semantics |
| `ENABLE_EEPROM_AUDIO_CUES` | 0; optional cue loading retained | Validate CRC-backed customization, defaults, torn writes and larger storage choices |
| `ENABLE_MENU_DIRECTORY` | 0; retained front-panel/catalog branches | Restore truthful self-description without copied host tables |
| `MENU_VISIBILITY`, `MENU_ORDERING`, `MENU_HIERARCHY`, `MENU_LAYOUT_PROTOCOL` | 0; layout storage remains enabled | Test ordering requires visibility, hierarchy/protocol require ordering, and persistence bounds |
| `ENABLE_EEPROM_MENU_LABELS` | 0; `Project/EepromMenuLabels.*` | Keep corrupt/unprovisioned fallback and provisioning tests |
| `ENABLE_EEPROM_BOOT_OPCODES` | 0; `Project/BootOpcodeSequence.*` | Validate post-safety startup execution and storage ownership; extend only through the existing action validation |
| `ENABLE_BOARD_AUTOMATIONS` | 0; `Project/AutomationStore.*` | Complete offline event rules, CRC/CRUD, recursion/rate bounds and safety paths under #87 |

Gate names above omit the common `PCCONTROLLER_` prefix. Board automations and
EEPROM menu labels currently have an explicit constrained-tail conflict. Resolve
it per target and storage layout; do not simply remove the error. Current source
comments and old issue bodies contain differing capacity/state descriptions;
future A/B manifests and the actual implemented gates must reconcile those claims.

History recovery should extract behavior and tests, compare current contracts,
then port or redesign behind an explicit feature bundle. Do not blindly restore
old files, old wire generations or discarded duplication. Useful removed/offloaded
behavior must have a recorded disposition: retained, restored, redesigned,
deliberately host-owned or unsupported with its reason.

## Proposed target and hardware boundaries

Keep the current constrained target unchanged. Add a reviewed ATmega2560 expanded
target and a specific, later-selected STM32 target rather than assuming every
STM32 variant has the same peripherals or storage. [ATmega2560](https://www.microchip.com/en-us/product/ATmega2560)
provides 256 KB flash, 8 KB SRAM and 4 KB EEPROM. [MegaCore](https://github.com/MCUdude/MegaCore)
is a candidate toolchain, not an installed PCController target.

Separate target pin/electrical identity, hardware backend, feature bundle and
toolchain/programmer policy. Keep protocol/action identities shared; HELLO,
build features and validated capabilities explain which target can perform an
operation. Rich data remains host-owned where that improves authoring and reuse.

| Boundary | Porting requirement |
| --- | --- |
| GPIO and board pins | Review every direct PORT/register use, pin mapping, polarity, reed and shift-register ownership |
| Timers/PWM/buzzer/RF | Replace 328P timer/vector assumptions; measure conflicts, capture latency and safe stop |
| UART/I2C/SPI | Bound transactions, queues and interrupt masking; preserve framing/CRC and recovery |
| Persistence/reset | Target-specific EEPROM/flash semantics, power-loss safety, watchdog and fuse/bootloader policy |
| Monotonic clock | Wrap-safe timestamps, deterministic test clocks, lease expiry and epoch semantics |
| Strip | Compact AVR, candidate FastLED and future target-specific timer/DMA adapters behind one pixel/frame API |
| Native firmware | Follow #103/#227: same production semantics, not a new VirtualBoard dispatcher |

Larger RAM can separate macro, strip, event and UART storage. Double buffering
and asynchronous LED transmission are candidates for a suitably selected DMA
target. Budget actual buffers, stack, queue depth and backpressure first. Shared
workspace contention remains explicit and safe on the constrained target.

[FastLED's platform documentation](https://github.com/FastLED/FastLED/blob/master/src/platforms/readme.md)
lists AVR and STM32 families; its [STM32 hardware-resource guidance](https://github.com/FastLED/FastLED/blob/master/src/platforms/arm/stm32/HARDWARE_RESOURCES.md)
requires checking target-specific DMA/timer support. Family-level library support
does not prove a chosen MCU/core/pin/backend combination works.

## Strip diagnosis before backend changes

The current [AddressableLeds sender](../Project/AddressableLeds.cpp) is a compact
AVR assembly backend, not FastLED. It uses PD6/D6, a bounded pixel workspace,
an approximately 800 kHz timing loop at the supported AVR clock and an 80 µs
reset delay. Native fallback does no physical output. The pixel/bounds native
tests cannot validate its real waveform. A 100-pixel RGB frame takes roughly
three milliseconds of data transmission with interrupts masked; that is a
capacity/timing constraint, not proof that the strip is faulty.

The user proposed an isolated known-good FastLED test to distinguish firmware
and electrical causes. Preserve it as a controlled diagnostic, not authorization
to replace production firmware in this documentation pass.

1. Coordinate the physical-board owner, stop active output sessions, record
   current identity and settings, and obtain project-owned backup/readback evidence.
   Use `bridge` for any later flash/replacement; restore through the same path.
2. Verify the actual strip chipset, voltage, color order, DIN direction, common
   ground, D6 continuity and safe current limit. Ten pixels at low brightness is
   the proposed starting test, not a newly asserted hardware descriptor.
3. Run a minimal RGB/off library sequence on an isolated test board if possible;
   otherwise use an explicitly approved temporary target/artifact and rollback plan.
4. Compare the compact backend and known-good library on the same verified
   pin/buffer/count/order. Record waveform, reset interval, colors and timing.
5. If the library works but production does not, trace workspace ownership,
   frame staging/commit and custom waveform. If both fail, inspect wiring, power,
   first IC/pixel, logic level and chipset timing before guessing another patch.
   Two failures do not alone prove an electrical fault; a known-good external
   driver and scope/logic-analyzer evidence narrow the cause.
6. Restore production, verify HELLO/readback/settings and known output state.
   Attach sanitized measurements to #71/#390; keep private captures off GitHub.

## Backlog ownership and acceptance

No new duplicate issue is needed for the reviewed requirements. Existing owners
retain their full acceptance contracts; this table adds the conversation's scope.

| Work to complete later | Existing owner | Completion evidence |
| --- | --- | --- |
| Target/profile and capacity restoration matrix | [#18](https://github.com/atomicdeploy/PCController/issues/18) | Exact target manifests; per-gate dependencies and A/B flash/SRAM/EEPROM/stack results; recovery/disposition of useful old code |
| Shared HAL/core and native target parity | [#103](https://github.com/atomicdeploy/PCController/issues/103), [#227](https://github.com/atomicdeploy/PCController/issues/227) | Same-source manifests and deterministic traces; no second semantic dispatcher |
| Rich standalone menus and LCD coexistence | [#28](https://github.com/atomicdeploy/PCController/issues/28), [#30](https://github.com/atomicdeploy/PCController/issues/30), [#31](https://github.com/atomicdeploy/PCController/issues/31) | Target-owned persistence/capabilities; host-loss/offline UI and host takeover tested |
| Scheduler, startup actions and EEPROM audio | [#18](https://github.com/atomicdeploy/PCController/issues/18), [#22](https://github.com/atomicdeploy/PCController/issues/22), [#87](https://github.com/atomicdeploy/PCController/issues/87) | Safe initialization order, bounded scheduling, storage CRC/defaults and compatible feature bundles |
| Offline event automations | [#87](https://github.com/atomicdeploy/PCController/issues/87) | Door/BT/host-loss/relay/RF/temperature rules, safe actions, CRUD and recursion/rate limits |
| Isolated strip proof and backend A/B | [#71](https://github.com/atomicdeploy/PCController/issues/71), [#390](https://github.com/atomicdeploy/PCController/issues/390) | Physical waveform/colors/count/current and rollback evidence; software tests alone insufficient |
| Independent queues and optional asynchronous strip output | [#44](https://github.com/atomicdeploy/PCController/issues/44), [#390](https://github.com/atomicdeploy/PCController/issues/390), [#103](https://github.com/atomicdeploy/PCController/issues/103) | Measured RAM/stack, flow control, underrun/lease behavior, priority stop and concurrency tests |
| Prepared media timing and eventual MCU epoch scheduling | [#554](https://github.com/atomicdeploy/PCController/issues/554), [Pealayer #32](https://github.com/ToghrolTP/pealayer/issues/32), [Pealayer #80](https://github.com/ToghrolTP/pealayer/issues/80) | Dual ACK/epoch/revision, authority handoff, physical output-edge versus video-presentation measurements under load and reconnect |

## Successor checklist

- Read current issues/comments and exact main/active heads before editing; claim
  a path-disjoint lane and preserve useful branch ancestry and private state.
- Recover the missing conversation remainder and reconcile additional requirements
  before marking source preservation complete.
- Inventory portable logic, AVR-family code, 328P-specific registers, bootloader
  assumptions and HAL candidates; choose the exact expanded target explicitly.
- Classify retained versus removed/offloaded features, costs, dependencies and
  offline behavior. Propose feature bundles before changing defaults.
- Build current constrained and expanded candidates with source identity,
  flash/identity bounds, SRAM/stack/EEPROM checks and capability parity tests.
- Keep E-STOP, interlocks, CRC/bounds, safe reset and command priority intact;
  rich functionality must not claim resources it cannot support.
- Complete the isolated strip experiment with the physical owner before choosing
  FastLED/DMA production changes. Do not weaken timing limits to hide a failure.
- Publish verified checkpoints, tests, blockers and next owner on existing issues.
  A compile, merge or staged package is not peripheral/deployment acceptance.
