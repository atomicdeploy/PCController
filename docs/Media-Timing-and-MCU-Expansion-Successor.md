# MCU expansion: supplied successor checklist and reconciliation

## Preservation status and scope

The previously clipped final answer from **Explain media clock command**
(conversation reference `6ac7ce34-0ec0-83ed-bd65-0a993c8bc241`) has now been
supplied from “For each one, report…” through its final paragraph. The complete
supplied engineering tail is preserved below, with no inferred missing text.
Together with the [existing roadmap](Media-Timing-and-MCU-Expansion.md), this
resolves the **missing-tail reconciliation** blocker from PR #608. It does not
assert that an unprovided raw conversation export has been archived.

The quoted checklist is source material for later work, not a record of completed
implementation or authorization to flash/actuate hardware in this documentation
pass. Its driver, workspace and physical-failure descriptions are claims from the
original discussion: future work must verify the exact current source and deployed
artifact rather than treat those descriptions as fresh measurements.

## Requirement-by-requirement disposition

The existing [backlog ownership table](Media-Timing-and-MCU-Expansion.md#backlog-ownership-and-acceptance)
remains authoritative. No duplicate epic, unsupported target or new production
feature is created by this reconciliation.

| Supplied requirement | Existing owner and required later evidence |
| --- | --- |
| For every retained/disabled feature, establish implementation existence and why disabled | #18: source path, exact target/profile and reason, including retained versus removed/offloaded classification |
| Per-feature flash/SRAM/EEPROM costs and dependencies/conflicts | #18: measured or historical provenance, explicit unknowns, isolated and combined A/B budgets, stack/identity accounting; never invent a per-feature delta from an aggregate saving |
| Enable as-is, redesign, or keep host-owned; existing and missing tests | #18: explicit disposition and rationale, offline/concurrency/maintainability/extensibility consequences, named existing test cases and gaps, then acceptance evidence |
| Search Git and merged PR history for actually deleted behavior | #18: historical-code recovery notes and links; port behavior/tests into current contracts, do not blindly revert files |
| Preserve BSS/explicit initialization, shared result/protocol helpers, canonical RF object, bounded serialization, menu tables, equivalent division elimination and generated protocol definitions | #18/#103: retain equivalent optimizations; restore only useful capability loss and prove reset/default/safety semantics unchanged |
| Independent macro/strip RAM (discussion cites 300 bytes), larger bounded buffers/queues | #44/#390/#103: verify current shared workspace, then measure independent macro/strip/event/UART RAM, stack, contention, queue bounds and backpressure |
| Local LCD alongside host rendering; full menu directory/hierarchy/layout | #28/#30/#31/#18: offline behavior and deliberate host ownership, target storage bounds, exact combined feature/profile tests |
| Board automations, general local event/startup executor, EEPROM-customizable audio, scheduler, richer autonomous status/effects and beneficial board-native timing | #87/#22/#18/#554: shared safe action paths, explicit unsupported events, defaults/CRC/torn writes, bounded recursion/scheduling, safe startup order, timing proof and host/board ownership |
| Isolated strip diagnostic before production changes, managed/pinned FastLED | #71/#390: standalone sketch or named diagnostic-only profile, normal dependency provenance, existing configured WS2811 data pin, small count/low brightness, exact rollback artifact |
| Diagnostic stimuli, plausible color orders, no relay/motion actuation | #71: solid red/green/blue, safe low-brightness white, black, moving/rainbow sequence and independent order variants; isolate relay/motion outputs |
| Physical diagnostic record | #71: first-segment supply voltage, common ground, DIN/DOUT, MCU-to-strip continuity, idle data level, waveform when instrument available, custom sender result, FastLED result and optional known-good external MCU result |
| Conditional interpretation and no premature driver replacement | #71/#390: library-only success points to custom timing/ownership; both failing requires electrical/protocol/first-pixel checks; intermittent results require signal/power/interrupt investigation; compare 328P flash/RAM/timing/interrupt behavior before choosing production FastLED |
| Stable public AddressableLeds API and backend boundary | #103/#390/#227: constrained AVR sender, appropriate library adapter, exact STM32 timer/DMA adapter and no-hardware/native adapter share semantics, not parallel dispatchers |
| Preserve 328P production/capacity gates; target-specific flash/SRAM/EEPROM limits and first-class named feature profiles | #18/#103: pin/toolchain/programmer/bootloader/identity/storage policy moves together; no hidden-flag-only bundles; compile practical supported target/profile matrix |
| Truthful HELLO/build features, target-independent host/native tests, unchanged motion/relay/reset/watchdog/CRC/bounds/programming safety | #18/#103/#227: capabilities reflect commissioned behavior; deterministic shared-core tests remain hardware-independent; safety is never traded for fit |
| All deliverables and focused PR breakdown | Owners above: architecture/portability report, recovery matrix/history notes, exact Mega/STM32 proposal, strip plan **and observed results**, documentation, tests/resource manifests and evidence-linked GitHub checkpoints; keep 328P working through every focused PR |

### Feature-recovery report contract

Every matrix entry must include all six requested audit fields, not merely a flag
and its default. Use this schema in #18's implementation audit:

| Field | Required content |
| --- | --- |
| Feature and retained status | Named gate/behavior, source path, exact inspected revision/profile; distinguish retained, removed and host-offloaded |
| Why disabled | Capacity, dormant use, ownership, storage conflict or commissioning reason, with evidence |
| Resource cost | Flash, static/dynamic SRAM, stack and EEPROM; measured A/B or historical identity/provenance; explicitly unknown if unavailable |
| Dependencies/conflicts | Required gates, peripheral/timer/buffer/storage ownership and combined-profile constraints |
| Disposition | Enable as-is, redesign, deliberately host-owned or unsupported with reason; describe useful gains/losses |
| Tests | Named existing test paths/cases, missing scenarios and later results; include physical evidence where native tests cannot prove behavior |

## Focused implementation sequence for later owners

1. #18: inventory source/history and publish the complete feature/test/resource
   matrix plus portability report; retain useful compaction.
2. #71: perform the minimal managed FastLED diagnostic before changing the custom
   sender, with physical-owner coordination, no relay/motion actuation and exact
   bridge-managed rollback. Record all measurements and inconclusive outcomes.
3. #103/#227: review exact ATmega2560 and selected STM32 architectures and shared
   adapters, including the stable strip API. Do not claim family-level support
   is a complete build/programming/recovery port.
4. #18: introduce named targets/profiles and target-specific capacity/capability
   gates with practical CI combinations, without breaking constrained production.
5. #87/#22/#28/#30/#31/#44/#390/#554: deliver independently reviewable capability,
   offline, buffering and timing increments after dependencies are proven.

## Complete supplied tail

This bounded engineering excerpt is preserved verbatim apart from line-ending
normalization. It contains no credentials, private filesystem paths or unrelated
conversation content.

```text
For each one, report:
- whether its implementation still exists in current source;
- why it is disabled;
- measured or historical flash/SRAM/EEPROM cost;
- dependencies/conflicts;
- whether it should be enabled as-is, redesigned, or left host-owned on larger targets;
- tests that already cover it and missing tests.
Also inspect Git history and merged PR history for behavior that was actually deleted rather than merely gated. Recover old code only as reference unless it cleanly matches current contracts. Prefer porting its behavior/tests into current architecture over blindly reverting whole historical files.
Do not “uncompact” optimizations that simply removed duplication. Preserve good changes such as:
- BSS plus explicit initialization;
- shared protocol/result helpers;
- one canonical RF object where simultaneous RX/TX is unnecessary;
- direct bounded serialization;
- common menu transition/data tables;
- eliminated AVR division where behavior is equivalent;
- canonical generated protocol definitions.
Restore or redesign only compactions that reduced useful capability, offline behavior, concurrency, maintainability, or extensibility.
For expanded MCU profiles, investigate:
- independent macro and addressable-strip RAM rather than the current shared 300-byte workspace;
- larger queues/buffers where bounded;
- full local LCD operation while preserving host rendering;
- full board automations;
- general local event/startup executor;
- complete menu directory/hierarchy/layout;
- EEPROM-customizable audio;
- task scheduler;
- richer autonomous status/effect handling;
- board-native timing/automation that meaningfully benefits from local execution.
Addressable strip diagnostic:
The current strip is reported physically non-working. Do not assume the strip, wiring, or firmware is at fault yet.
Current AddressableLeds::show() uses a custom AVR direct-port/assembly sender derived from Adafruit NeoPixel timing, while native strip tests cover buffer/order/bounds and do not prove the actual physical 800 kHz waveform.
Create an isolated diagnostic path before changing the production driver:
- Prefer a standalone diagnostic sketch or explicitly named diagnostic-only firmware profile.
- Add/pin FastLED through the normal dependency-management system if used; do not introduce an unmanaged library.
- Drive the existing configured data pin with a minimal WS2811 example.
- Start with a small pixel count and low brightness.
- Exercise solid red, green, blue, white/low-brightness if power permits, black, and a basic moving/rainbow test.
- Test plausible color orders independently of whether wrong order would merely swap colors.
- Do not operate relay/motion outputs.
- Preserve a clean rollback to the exact currently deployed firmware.
Record physical diagnostic outcomes:
- strip supply voltage at the first segment;
- common ground;
- DIN versus DOUT orientation;
- continuity from MCU output to strip;
- data idle level;
- observed data waveform if a scope/logic analyzer is available;
- result using PCController custom sender;
- result using FastLED;
- optionally result using an external known-good microcontroller.
Interpretation:
- FastLED works while PCController fails -> treat the custom strip backend/timing/ownership path as the primary suspect.
- Both fail -> investigate electrical, strip protocol/type, first-pixel failure, supply, grounding, direction, or logic-level problems before rewriting production firmware.
- If both work intermittently -> investigate signal integrity, power, interrupt/timing interactions, or first-pixel behavior.
Then design an addressable LED backend boundary while keeping the public AddressableLeds API stable:
- constrained AVR direct sender;
- FastLED/library backend where appropriate;
- future STM32 timer/DMA backend;
- VirtualBoard/no-hardware backend.
Do not make FastLED the production replacement solely because the diagnostic succeeds. First compare flash/RAM/timing/interruption behavior on the 328P.
CI/build requirements:
- Preserve the exact current 328P production build and its capacity gates.
- Add target-specific flash/SRAM/EEPROM limits rather than global 328P constants.
- Make expanded features first-class named profiles; do not rely on raw hidden compiler flags.
- Compile/test every supported target/profile combination that is practical.
- Maintain truthful HELLO capability/build-feature reporting.
- Keep VirtualBoard and host protocol tests independent of the physical target.
- Never weaken motion, relay, reset, watchdog, CRC/bounds, or programming safety to make a target fit.
Deliverables:
- architecture/portability report;
- feature-recovery matrix;
- historical-code recovery notes;
- recommended ATmega2560 and STM32 target architecture;
- exact strip diagnostic plan/results;
- proposed issue/PR breakdown;
- documentation updates;
- tests/resource measurements;
- GitHub comments/checkpoints with exact evidence;
- no claim of physical strip success without actual hardware observation.
Prefer multiple focused PRs over one giant MCU-port PR. Keep the 328P profile working throughout the migration.



That prompt should give a Codex agent enough structure to coordinate the work properly rather than just creating a Mega2560 branch and breaking half the programming toolchain.
And for the very next physical step, I would prioritize the minimal FastLED test before touching the current WS2811 assembly sender. It is a very cheap experiment and gives us a huge amount of diagnostic information.
```
