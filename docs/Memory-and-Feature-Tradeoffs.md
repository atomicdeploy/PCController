# Firmware Size, Feature Cost, and Removal Tradeoffs

Use this document before removing a feature from the ATmega328P image. It
separates exact whole-image measurements from symbol estimates and, most
importantly, states what a person will no longer be able to do after each cut.

## Decision summary

- The latest source-only audit build is `F6D76FE4`: 32,240/32,384
  application bytes, 1,441/2,048 static-SRAM bytes, and a conservative
  278-byte stack/RF margin. It was built and tested without opening COM18, so
  it is not live-board evidence.
- Stock MiniCore Urboot leaves 144 application bytes for that image. The
  optional four-page TM1637-progress Urboot lowers the application ceiling to
  32,256 and leaves only 16 bytes; its build remains compile-verified, not
  installed by this audit.
- The physical COM18 board now runs interim pre-macro `32FAAD86` with
  capabilities `0x003FF7FF` and Status page 0. It is not the staged macro
  image.
- That live interim uses 32,382 of the 32,384 application bytes. It left only
  2 bytes and cannot represent the current source after the macro/LCD changes.
- A rejected macro-integration checkpoint measured 32,490 application bytes
  and 1,545 static-SRAM bytes, 106 flash bytes over the Urboot limit. The
  latest fitting non-live checkpoint is `0E5FE035`: 32,382 flash bytes and
  1,543 static-SRAM bytes. It leaves 2 application bytes, but is not live-board
  evidence until upload/read-back succeeds.
- The staged profile now compiles the full AVR LCD renderer out and keeps
  generic cooperative I2C access. This is the largest measured offloadable
  cut: 1,328 flash bytes and 49 static-SRAM bytes in the archived A/B build.
- The latest fitting checkpoint has exact section sizes and content hashes.
  Current source may still change, so the final identity remains pending.
  Marginal A/B cost for the macro queue and several optional UI cuts remains
  unmeasured. Do not use the live interim's 2-byte figure to decide staged
  space.
- Do not remove relay sequencing, motion policy enforcement, safe reset,
  protocol bounds/CRC, EEPROM checksums, watchdog, or startup I2C recovery for
  space. Those protect hardware or stored state.

## Image identities and evidence level

### Live board: interim `32FAAD86`

The controller-owned build and current HELLO evidence are:

```text
Build/source hash: 32FAAD86
Capabilities:      0x003FF7FF
Generated:         2026-08-01T13:06:08Z
Packed date/time:  0x35018451 (260801163434)
Flash:             32,382 / 32,384 bytes (2 bytes free)
.text:             32,148 bytes
.data:             234 bytes
.bss:              1,187 bytes
.noinit:           1 byte
Static SRAM:       1,422 / 2,048 bytes (626 bytes before stack/interrupt use)
Application SHA:   C159D01492A5616C077D1ACEF63E8833B586EBB370BAE48E2DBF5A019A7FF57E
Merged SHA:        E6A5C11A1F38AE246CA45F645107E065B2818054BF026BEA954FFB06F3178C00
```

COM18 now authenticates this hash and reports Status page 0. This proves the
interim application is running; it does not prove the newer macro queue or its
offloaded-LCD profile.

### Previous live board: `5DF10D05`

The following is the last accepted 5DF artifact record. It explains the
earlier `VOLT` observation and remains useful historical evidence, but COM18
has since been updated to interim `32FAAD86`. These sizes/hashes come from its
earlier source-keyed build.

```text
Flash: 32,374 bytes = .text 32,120 + initialized .data image 254
SRAM:   1,455 bytes = .data 254 + .bss 1,200 + .noinit 1
Free:      10 application-flash bytes; 593 static-SRAM bytes
Hash:   5DF10D05
Source: 6416EB92A694C4CBEE7FFFFD66BA757033E3DE0FFBADAA44F044A46306BB7783
HEX:    8BF7AE02FDCD6B10FF6B335FF49EEB55CCF59E4EE417CD27C3CE5AA5430FBC49
Merged: E9AFF099A95862E36512BA4D1343487D219E6E68F58A1F589A7E5416C8327EBE
EEPROM: 788E5FFC44AE4EE912FE01F495951F96B0ACCD5705E184DCAEA197D8B64856A6
```

Earlier 5DF validation covered its 14 root pages with reset count stable,
relays off, PWM Off, and no UART framing/CRC errors. It does **not** validate
the new Status page, menu directory, host panel capture, 20 RF slots, 1 ms
motion break, macro queue, or offloaded LCD architecture.

### Proven rollback: `E2DCE296`

```text
Flash: 32,382 bytes = .text 32,130 + initialized .data image 252
SRAM:   1,452 bytes = .data 252 + .bss 1,199 + .noinit 1
Free:       2 application-flash bytes; 596 static-SRAM bytes
Hash:   E2DCE296
HEX:    30E79054D5DF1BA359217066CBCE0E0138DAB6A74EFCE14E8898247EACCBF7A4
Merged: 588A2DBEB37723C064324432C8D48C5F270BDFDADA80D3CB88A09EA44FFF140E
```

E2 remains historical physical-button/reset evidence only. It is not a
feature-complete rollback for the new protocol.

### Final source/live acceptance: pending

```text
Final source/build hash: PENDING FINAL CLEAN COMPILE
Final flash/SRAM:        PENDING FINAL CLEAN COMPILE
Final artifact hashes:   PENDING FINAL CLEAN COMPILE
COM18 upload/read-back:  PENDING FINAL HARDWARE ACCEPTANCE
```

Do not replace these fields with a checkpoint identity. The final values must
come from one atomic controller-tool build and the same image's COM18 HELLO and
read-back evidence.

### Latest source-only firmware/native audit: `F6D76FE4`

```text
Build/source hash:        F6D76FE4
Source SHA:               F6D76FE453258C99CC5BF00B34F0B82202839AEE8F1B84118F7FBC1621CA5144
Packed date/time:         0x35020B0C (260802012424)
Application flash:        32,240 / 32,384 bytes (144 bytes free)
Static SRAM:              1,441 / 2,048 bytes
Guarded stack/RF margin:  278 bytes
Application SHA:          A64E76C341B4FB4762BD0A7C0C8750C09C9BA103C6C74AEBA6350BA35957D021
Stock merged SHA:         94B568296905C91DA55009EA4667A398DB9760045EC0AC7C8DF4D1573A42873E
EEPROM SHA:               788E5FFC44AE4EE912FE01F495951F96B0ACCD5705E184DCAEA197D8B64856A6
Live read-back:           NOT UPLOADED OR VERIFIED
```

Against the earlier hardware-free cleanup baseline `CE472A67`
(31,910 flash/1,436 static SRAM), this source is an aggregate **+330 flash
bytes and +5 static-SRAM bytes**. That is a whole-image delta across several
interacting changes, not a defensible per-feature price. The retained changes
include centralized fail-safe motion-policy revocation, Idle/Running program
state and status flags, eased illumination/RGB transitions, measurement
smoothing, bounded DS18B20 behavior, corrected temperature-role defaults,
expanded key semantics, exact RF20/current EEPROM parity, and native-simulator
protocol parity.

Two isolated measurements are available. Clamping accepted DisplayText LCD
content to its physical 32 cells costs six flash bytes versus the immediately
preceding build and prevents a buffer overwrite; it removes no displayable
text. Replacing every remaining operation-local timestamp with the shared
loop `now` grew the application by 62 bytes, so that experiment was reverted;
the current design samples once per loop/semantic dispatch where codegen is
smaller, with no timing feature removed.

### Latest compiled fitting non-live macro checkpoint

```text
Build/source hash: 0E5FE035
Source SHA:        0E5FE035CEB55760257EB1EE86D4561009A1DD9B8A334FCFDA5EED4DD49D3335
Packed date/time:  0x35019038 (260801180148)
Flash:             32,382 / 32,384 bytes (2 bytes free)
.text:             32,144 bytes
.data:             238 bytes
.bss:              1,304 bytes
.noinit:           1 byte
Static SRAM:       1,543 / 2,048 bytes (505 bytes before stack/interrupt use)
Application SHA:   8E4405B5EDFAA62E2FE294D828825E7243566968334A909DC1D7017846E68907
Merged SHA:        B8C6BA61A7DAA42FE077A38AC81D6BBFF0A4AAF33E3F9265A58F112514063416
Live read-back:    NOT YET UPLOADED OR VERIFIED
```

The current source has changed since this manifest; these numbers remain the
latest compiled checkpoint, not a claim about final HEAD.

The earlier rejected diagnostic was 32,490/32,384 flash and 1,545/2,048 static
SRAM. Earlier fitting checkpoint `6DCF6A68` reached 32,372 flash and 1,541
static-SRAM bytes before the compact offline-LCD fallback and subsequent fit
work. The latest checkpoint is a net 108 flash bytes and two static-SRAM bytes
below the rejected diagnostic while retaining the 128-byte/127-usable byte
ring. The final report must still give a new compiler manifest and live
read-back evidence, and must treat the 505-byte static remainder as
stack/interrupt headroom, not spare feature RAM.

## Current compile profile

This table describes source-only `F6D76FE4`, not live interim `32FAAD86`. The
live interim was built before the later firmware/native work and must not be
used as evidence for those source changes.

| Setting | Staged value | User-visible consequence |
|---|---|---|
| Target | ATmega328P, 16 MHz, MiniCore 3.1.2 | 32 KiB flash, 2 KiB SRAM |
| Application limit | 32,384 bytes | Retains the UART Urboot/Urclock bootloader |
| UART | 115200 baud | Native COBS/opcode link is always enabled; Firmata/debug strings are absent |
| LTO | Enabled | Cross-file dead-code removal/inlining |
| `-mcall-prologues` | Enabled | Shares function entry/exit sequences; small deterministic call-cycle cost |
| Linker relaxation | Enabled | Shortens eligible calls/jumps; no feature loss |
| MiniCore `Wire`/`WIRE_TIMEOUT` | Not linked | Fixed-hardware I2C master provides bounded transactions and startup recovery; the 2 s watchdog remains |
| AVR LCD renderer | Disabled | Physical 2x16 LCD requires the PC host's generic-I2C renderer |
| Generic I2C lease | Enabled | Host may cooperatively read/write other addresses, up to 16 bytes per transfer |
| Menu directory | Enabled | Host can query exact staged menu IDs/labels instead of assuming them |
| Addressable order | WS2811/BRG | Change compile flag for WS2812B/GRB hardware |
| PWM polarity | Active-high logical outputs | Logical 0 is Off; 4095 is fully On |
| PWM/INA addresses | `0x41` / `0x40` | The former collision is removed |

The canonical EEPROM layout intentionally has no whole-project migration
history. Invalid settings fall back to defaults; an RF header is accepted by
record width/capacity rather than project version. This saves migration code
but means an unrelated development image need not load in current firmware.

## Hardware configuration versus code size

The exact address, pin, rate, averaging, resolution, polarity, startup state,
rationale, safe alternatives, and canonical source for every board peripheral
are documented in
[Peripheral initialization profile](Front-Panel-and-Menus.md#peripheral-initialization-profile).
Those values are part of the feature contract, not unexplained magic numbers.

Changing an EEPROM value or a constant usually changes behavior without
recovering meaningful flash. Space is recovered only when the associated
driver, menu, protocol path, strings, state, and callers become unreachable
and a clean linked A/B build proves the delta.

| Configuration choice | Why it is the staged default | What a cheaper alternative loses | Space evidence |
|---|---|---|---|
| INA219 64x bus/128x shunt averaging | Smooth current at the desired open-door 100 ms cadence | Lower averaging is faster/noisier; removing INA loses voltage, current, power, supply correction, and host graphs | Constant-only changes are negligible; whole archived INA/Wire ownership overlaps and needs A/B |
| PWM at `0x41`, 1 kHz, active-high | Avoids INA collision and visible MOSFET flicker while serving all 16 roles | Removing the Auto/menu layer loses offline channel identification; removing PWM loses eleven user outputs, illumination, Power/On, and status RGB | Controller/driver archived named lower bound about 1,172 bytes; marginal delta unknown |
| Two asynchronous 11-bit DS18B20s | 0.125 C resolution without blocking UART; HOT bypass remains prompt | A lower resolution is coarser but not smaller; whole removal loses both named temperatures, ROM identities, and HOT logic | Archived named area about 792 flash bytes; marginal delta unknown |
| Changed-only TM1637 at 20 ms | Fast front-panel feedback without redundant bus traffic or flicker | Host-only display loses offline Status, measurements, editors, commissioning, RF learn, and safety feedback | Archived local menu plus TM area exceeds 2 KiB conceptually, but is heavily shared; A/B required |
| Safe active-low shift startup | Prevents relay energization before the first known output frame | Removing `/OE` sequencing, polarity translation, or all-off ordering is unsafe and is not an optimization candidate | Protected; no saving is offered |
| rc-switch receive tolerance 70%, ten TX repeats | Accepts the observed remote family and provides reliable sends | Whole removal loses RX/TX, learning, 20 mappings, repeats, RF events, and RF-driven actions/macros | At least 1,010 named flash and about 184 static SRAM; true LTO delta requires A/B |
| PC-owned LCD plus generic I2C | Retains rich connected text and cooperative future devices without a second renderer | Offline rich 16x2 rendering is lost; only the preloaded compact offline page remains | Measured saving: 1,328 flash and 49 SRAM |
| Timer1 hardware-toggle buzzer | Fixes glitch/timer contention without starving RF interrupts | Whole removal loses boot proof, key/door/relay/save/error cues, host melodies, and Silent's purpose | About 324 archived named flash plus callers; marginal delta unknown |
| Relay sequencing: 1 ms break, 50 ms settle, 5 ms cross-side gap | Keeps direction and enable roles correct while remaining responsive | Removing interlocks risks powered reversal or simultaneous mechanical switching | Protected; no saving is offered |
| PWM status RGB and fixed D6 addressable sender | Gives local state/cues with small fixed drivers and no heap | Static-only status loses animation cues; strip removal loses 11-pixel effects and opcode/macro control | RGB about 332 named flash; strip about 219 flash/34 SRAM lower bound |
| UART0 Urboot/urclock profile | One connector supports application control, programming, backup, and recovery | No-bootloader mode makes ISP mandatory and removes boot-mode integration | Exactly 384 additional application bytes, with the stated programming/recovery loss |

### Urboot-Custom progress-backend budget

The patch-based [`Urboot-Custom`](../Tools/Bootloader/Urboot-Custom/README.md)
prototype moves the boot start from `0x7E80` to `0x7E00`. Its selected raw
TM1637 backend occupies 510 meaningful bytes in a 512-byte allocation, so it
costs the application exactly 128 bytes versus the installed 384-byte MiniCore
image. Current source `F6D76FE4` leaves **16 bytes** under the new 32,256-byte
ceiling (versus 144 bytes under stock Urboot).

The selected image removes no Urboot protocol feature: EEPROM access,
compare-before-write, application page writer, autobaud, chip erase, vector
loading, and reset/bootloader protection remain. The only replaced behavior is
the old single PB5 activity blink; PB5 is the TM1637 clock and the two drivers
cannot coexist electrically. Keeping both also measured 520 bytes, eight bytes
over four pages. A fifth page would consume another 128 application bytes.

These compile-only escape profiles are available for a larger future backend;
none is selected:

| Optional Urboot removal | Bytes gained | Exact lost behavior |
|---|---:|---|
| Chip erase | 28 | No bootloader-managed `STK_CHIP_ERASE` request |
| EEPROM access | 56 | No EEPROM settings backup or restore over Urboot |
| Update check | 26 | Identical requested pages are erased/written again |
| Application page writer | 10 | Application self-programming entry disappears; serial upload remains |
| Autobaud | 16 | Requires fixed 115200 baud at 16 MHz |
| Reset-vector protection | 14 | An unsafe page-zero write can strand the bootloader |

The first Urboot-Custom installation still requires ISP and a vector-aware
merged image. ISP cannot show its own progress because reset is held and the
same D13/D11 pins carry SCK/MOSI; the display backend applies to later UART
Urclock reads and writes.

The fixed drivers save space by deliberately omitting generic portability:
arbitrary TM1637 pin selection, arbitrary LED pixel counts/order at runtime,
generic Dallas device counts/parasite power, a full AVR LCD renderer, and the
large Adafruit INA/PWM/NeoPixel abstractions. Restoring those conveniences is
valid for another board profile, but it must be budgeted as new functionality
rather than treated as a free configuration change.

## What is actually linked

| Library | Version at last dependency audit | Purpose in application |
|---|---:|---|
| EEPROM | MiniCore 2.0 | Board settings, 20 RF records, reset-count journal |
| rc-switch | 2.6.4 | 433 MHz receive and transmit |

The installed Adafruit INA219, Adafruit PWM Servo Driver, Adafruit BusIO,
DallasTemperature, OneWire, TM1637 libraries, and Adafruit NeoPixel are not
linked into the AVR image. Fixed-hardware project drivers implement only this
board's required paths. Uninstalling an unused package frees PC disk space,
not one byte of MCU flash or SRAM.

`rc-switch` is therefore the heaviest linked third-party feature library.
The former generic Wire dependency is no longer linked; the project-owned
fixed I2C master serves INA219, PWM, and cooperative host transactions.

## Board-feature inventory by code area

The following named-symbol sizes come from archived `E5109CA1`, before the
macro queue and the current offload profile. LTO moves/inlines code across
files, so rows overlap conceptually and are **not** additive or exact current
removal savings.

| Area | Archived named flash bytes | Current staged responsibility |
|---|---:|---|
| Native UART dispatcher/events | 944 plus a 3,682-byte dispatcher | HELLO hash/time, settings, telemetry, outputs, menus, RF, I2C, front panel, events, macro dispatch |
| Local menu/display state machine | 1,570 action handler plus 722 TM1637 | 15 root pages, modal editors, blink/save feedback, 20 ms cached display |
| Wire/TWI | 1,284 | Shared PWM `0x41`, INA219 `0x40`, cooperative generic I2C |
| PWM driver/controller | 1,172 | Sixteen cached channels, roles, polarity, Off/Manual/Auto commissioning |
| Relay controller | 946 | R1/R2 and R3/R4 sequencing/interlocks, R5-R8, All Off/tests |
| rc-switch | 1,010+ lower bound | INT0 receive, INT1 transmit, repeat behavior, learning/action execution |
| DS18B20 | 792 | Bounded two-ROM scan, identities, role swap, async 11-bit conversion |
| I2C LCD | 712 named renderer; 1,328 whole-feature A/B | **Disabled in staged AVR profile**; generic I2C remains |
| RF EEPROM records | 476 | Twenty checksummed records, list/remove/map/replace |
| Status RGB | 332 | Power signal, ready/custom state, hot/fault/learn/event animations |
| Timer1 buzzer | 324 | Queued tones, boot/key/door/relay/save/error feedback, Silent |
| Addressable strip | 218 named | Fixed D6, 11 pixels, fill/per-pixel/brightness |
| Reset telemetry/safe reset | Not independently measured | Reset cause/count journal, safe output shutdown, reset cue |
| Macro queue | Final marginal A/B pending | 128-byte/127-usable ring, MCU scheduling, compact lifecycle/timing status |

Archived important static allocations were:

| Area | Approximate static SRAM bytes |
|---|---:|
| HardwareSerial object and buffers | 157 |
| Two rc-switch instances and timing buffer | 184 |
| Wire/TWI objects and buffers | about 186 |
| Four advanced key objects | 88 |
| Fixed task manager | 73 |
| Native UART receive/decoder | 68 |
| PWM cache/controller | 51 |
| Timer1 buzzer queue/state | 50 |
| AVR LCD cache/state | 43; absent when renderer is disabled |
| System-input debounce/history | 39 |
| Addressable LED pixel buffer | 33, with no heap duplicate |
| Macro byte ring | 128 plus compact report/state; final total pending map |

Protocol sends also use bounded local buffers on the stack. Static free SRAM is
not the usable stack guarantee; RF and TWI interrupt nesting, COBS frames,
macro dispatch payloads, and call depth all need margin.

## Space already recovered and exactly what was lost

### Compiler/linker savings: no feature loss

The controlled archived sequence was:

| Experiment | Flash bytes | Change at that step | User-visible loss |
|---|---:|---:|---|
| LTO baseline, no Wire timeout | 32,920 | Baseline | None |
| Add linker `--relax` | 32,496 | -424 | None |
| Add `-mcall-prologues` | 31,682 | -814 | None; a few more cycles on some calls |
| Add `WIRE_TIMEOUT` | 32,378 | +696 | Added recovery feature, but live behavior was unstable |

Because optimizations interact, removing one option from the final combination
gave 442 bytes for relaxation and 856 bytes for shared prologues. Do not add
those values together with the sequential table as though they were
independent.

Source-level serializer sharing recovered 214 bytes in 5DF: 162 bytes by
copying fixed telemetry blocks and 52 by sharing the settings prefix. EEPROM
output remained byte-identical and static SRAM stayed 1,455 bytes. No protocol
field or menu function was lost.

The current combined fit series is a net 108 flash bytes and two static-SRAM
bytes below rejected checkpoint 32,490/1,545, producing non-live checkpoint
32,382/1,543. An intermediate checkpoint reached 32,372/1,541 before compact
offline-LCD support and later fit edits. Because features and optimizations
changed together, the following losses must not be assigned invented per-edit
byte values:

- planned macro duration is host-owned rather than duplicated in the AVR
  report; the board cannot independently calculate percent complete or show a
  planned duration when the host does not provide that metadata;
- underrun and dispatch-error counters are 8-bit; more than 255 of one error
  type in a playback cannot be represented faithfully;
- late-step count and maximum timing error were removed from the AVR report;
  instead, the report carries the RUN `startedAtUs`, and reserved-sequence
  `0xFE` ACK/error responses carry the AVR `deviceMicros`. The host computes
  exact signed board-clock deltas as `deviceMicros - (startedAtUs + dueOffset)`
  and owns the aggregates. The loss is standalone on-board aggregates, not
  precise timing evidence;
- queue free space and overall fidelity are derived by the host from fill,
  lifecycle, counters, and ACK/error evidence instead of occupying explicit
  AVR report fields;
- full status is sent for lifecycle/query, not every step; observers get exact
  reserved-sequence ACK/error result and timestamp evidence but not a large
  per-step status snapshot;
- the supported host rejects empty macros, while the AVR omits a duplicate
  empty-RUN guard; a raw third-party client can start a zero-step session that
  requires Cancel or the five-second host-loss timeout to clear;
- read-only HELLO, Status, Settings, Temperature List, Front Panel, and PWM
  query handlers ignore surplus payload bytes instead of returning BadPayload.
  Valid requests and their responses are unchanged, but malformed read-only
  queries receive data rather than a strict payload error;
- addressable-strip brightness is pre-scaled by the supported host when a
  pixel/fill command is created. The AVR accepts the legacy fifth brightness
  byte but does not rescale its buffer, reports brightness 255, and cannot
  later change global brightness without the host resending source colors.
  Raw/older clients that send unscaled RGB plus a non-255 brightness value will
  therefore produce brighter output than requested;
- timed schema-2 responses append a four-byte MCU clock to ACK frames and set
  the high bit of timestamped Event types. A host parser that assumes the old
  two-byte ACK or untagged event type is incompatible unless it implements the
  dual-schema decoder.

Most are diagnostics/compatibility tradeoffs rather than removed output
functions; board-side addressable global re-scaling is the direct output UX
loss. Relay/PWM safety validation, native payload bounds, COBS/CRC,
cancellation, host-loss safe-stop, variable macro payloads, and MCU-clock
scheduling remain.

### `WIRE_TIMEOUT` disabled: one robustness feature lost

Disabling MiniCore's timeout path saved about 696 bytes relative to enabling
it. The loss is precise: a wedged runtime TWI transaction no longer gets a
25 ms TWI-only reset. Startup SDA/SCL recovery and the 2-second watchdog remain,
so the fallback may reset the whole MCU and lose volatile state. The timeout
was removed only after its enabled image produced repeatable live resets.

### AVR LCD renderer disabled: standalone physical LCD lost

The archived A/B build recovered exactly 1,328 flash bytes and 49 static SRAM
bytes by disabling the full PCF8574/HD44780 scan, initialization, cache, and
two-line writer. Re-measure the marginal delta on the final macro source.

User-visible losses in the staged profile are:

- the MCU cannot discover or initialize the backpack by itself;
- local menu/status text does not appear on the physical LCD without the PC;
- the MCU cannot render arbitrary new offline text after the host disappears;
- a legacy `DisplayText LCD` command only updates the logical 32-character
  mirror unless the host also performs physical generic-I2C writes.

Still retained:

- the TM1637 and its complete standalone menu;
- host-visible desired LCD text in the exact front-panel snapshot;
- cooperative generic I2C reads/writes to any 7-bit device address;
- the ability for the PC host to scan `0x27`/`0x3F`, initialize the LCD, cache
  rows, and drive it physically through those generic transactions;
- a small staged fallback: after a host has preloaded hidden DDRAM with
  `PC offline      ` / `Connect USB toPC` and taught the MCU the address, five
  seconds of heartbeat loss makes the MCU issue sixteen display-left shifts
  to reveal that prepared page.

The host-side physical renderer and fallback need physical test evidence before
this can be called feature-equivalent. The compact MCU helper cannot initialize
the backpack or verify the hidden contents; without successful host preload it
either shifts unknown DDRAM or does nothing when no address was learned. The
logical 32-byte mirror retains its last text during abrupt loss; only physical
DDRAM is shifted.

Host integration now separates `lcd_service_enabled` (default true) from
`mirror_prompt_to_lcd` (default false), routes captured hosted-menu LCD rows to
the physical presenter, and calls `PrepareDisconnect` best-effort with a
350 ms bound during runtime detach. The remaining gap is proof, not the basic
source path: pass host tests and observe preload, abrupt loss, planned detach,
and reconnect on the physical display before claiming feature equivalence.

### Generic libraries replaced by fixed drivers: portability lost

The compact drivers keep this board's required operations but intentionally
lose unused generic APIs:

- DS18B20: no parasite-power strong pull-up, alarms, scratchpad save/recall,
  arbitrary family/device count, or broad OneWire helpers;
- INA219: no alternate calibrations, arbitrary Wire instance, power-save/reset
  convenience API;
- PWM: no servo-microsecond helpers, external oscillator calibration,
  arbitrary bus, or generic sleep/output-mode API;
- WS LEDs: no arbitrary pin/count/chipset, gamma/palette framework, runtime
  pixel type, or heap-managed buffer;
- TM1637: no arbitrary display abstraction or generic animation library.

The current requested fixed pins, addresses, two temperatures, measurements,
16 PWM outputs, and 11 addressable pixels remain.

### Firmata and text-debug protocol removed: ecosystem compatibility lost

Firmata and the old newline/string command/debug channel are not linked. The
exact marginal saving was not preserved as an isolated A/B build. The loss is
support for generic Firmata clients and convenient human-readable serial
sessions. The gain is one always-enabled 115200-baud UART dedicated to the
bounded COBS/CRC opcode protocol, bootloader handoff, precise events, and host
bridge. Restoring Firmata would also reintroduce UART ownership ambiguity and
is not a space-neutral compatibility switch.

### Firmware-side EEPROM migrations removed: historical layouts are not imported

The firmware validates only canonical checksummed settings and semantically
self-described RF records. It does not branch on build or layout versions.
Unrecognized development data falls back to defaults; RF records and the reset
journal start fresh when their current shape/checksum is absent. This does not
remove the PC host's backup/readback capability.

### Changes that do not solve the flash limit

- Reducing learned RF capacity from 20 back to 8 would free 144 EEPROM bytes,
  but essentially no application flash or static SRAM; it would simply lose
  twelve remote-code slots.
- Shortening the reset journal likewise frees EEPROM endurance/storage, not
  the application flash that caused the earlier 106-byte-over diagnostic.
- Selecting Silent, PWM Off, or hiding a menu at runtime does not remove its
  compiled implementation.
- Uninstalling unused Arduino libraries frees PC disk only.
- UPX compresses the Windows host executable and has no effect on AVR flash.

## Local-versus-host menu placement options

The complete per-page decision matrix, including offline behavior and protocol
dependencies, is in
[Board Features, Front Panel, and Menus](Front-Panel-and-Menus.md#menu-placement-decision-matrix).
This size-oriented summary does not authorize a migration. No page should move
silently: select it, perform an isolated A/B build, and accept the offline loss
first.

| Page/candidate | Default decision | Offline impact if host-only | Protocol dependency | Measured/estimated result |
|---|---|---|---|---|
| `STAT` | Keep | Loses standalone home/door state and hosted-menu request point | Status/events, front-panel capture/display | Unmeasured; small likely cut, high availability cost |
| `VOLT` | Keep | Loses standalone supply diagnosis | Telemetry/status | Unmeasured render-only cut; INA stays |
| `CURR` | Keep | Loses standalone current diagnosis | Telemetry/status | Unmeasured render-only cut; INA stays; host gains SI formatting/graphs |
| `tLED` | Keep | Loses local illumination-temperature/HOT view | Temperature list/telemetry | Unmeasured render-only cut unless DS18B20 is also removed |
| `t-bt` | Optional move | Loses local BT-module temperature | Temperature list/telemetry | Unmeasured small cut; host gains ROM/name presentation |
| `LItE` editor | Keep controller; optionally move editor | Lighting still runs, but mode/on/off brightness cannot be changed locally | Settings read/write and hosted fields | Unmeasured; host gains exact sliders/validation |
| `bt` | Keep | Loses local Off/On/Blinking diagnosis | BT status/event | Unmeasured small cut; host gains history/automation |
| `Snd`/Board Settings | Split: keep Silent, consider moving advanced fields | Moved EEPROM values still apply but cannot be edited locally | Settings read/write | Unmeasured shared editor; strong host table/color UX gain |
| `PWM` commissioning | Move first if bytes are needed | Loses offline fade/channel identification and local Off/Manual/Auto | PWM get/set/mode and macro engine | Best candidate; delta requires A/B; host gains sliders/names/repeatable tests |
| `rELY` commissioning | Move after relocating an obvious All Off | Loses local per-relay test | Relay/motion commands and events | Good candidate; delta unmeasured; host gains labeled live controls |
| `KEY` | Keep | Loses UART-independent key/wiring identification | Key events/capture | Likely small unmeasured saving, little gain |
| `uPWM` editor | Move after commissioning pages | Stored values run but cannot be edited/saved locally | PWM and stored-settings path | Delta unmeasured; host gains precision/presets/macros |
| `r5-8` | Keep unless PC-only output control is accepted | Loses local Toggle/Push general outputs | Relay commands/events and capture | Delta unmeasured; host gains hotkeys/source attribution |
| `MOVE` | Keep | Loses Windows/USB-independent held motion | Motion commands/events; MCU interlock still mandatory | Unmeasured; availability/safety cost is too high |
| `LErn` | Move only after host RF UX acceptance | Learned mappings still run; new learning/count/cancel needs PC | Learn/list/replace/map and RF events | Delta unmeasured; major host naming/category/reorder gain |
| Shared Save/Discard | Keep while any local editor remains | Local writes become immediate or unavailable | Settings ACK/readback | Shared delta; cannot assign to one page |
| `MenuList` | Keep | Host must hard-code page IDs/labels and can drift | Paginated menu directory | Likely small unmeasured cut; compatibility cost is high |

Current built-in PC-hosted menus are `host`, `pc-settings`, and
`system-actions`. Macro/RF-library and richer board-settings hosted menus are
still acceptance work, so their planned UX must not be used as proof that a
local page is already replaceable.

## Prioritized optional removal table

“Unknown” means an A/B build has not isolated the marginal delta. A named
symbol lower bound is useful for scale but must never be promised as recovered
space. Rows marked **protected** should not be used as routine flash cuts.

| Priority | Candidate | Current evidence | Exact user-visible loss if removed | Recommendation |
|---:|---|---|---|---|
| 1 | Keep AVR LCD renderer off | Archived A/B: **1,328 flash, 49 SRAM** | No standalone discovery/init or arbitrary LCD rendering; only a host-preloaded compact offline page can be shifted after loss | Already applied; physically test host preload, detach, loss, and reconnect |
| 2 | Move all-channel PWM commissioning/Auto demo to host macros | Delta unknown; PWM/menu code overlaps | No offline channel-identification fade or local Off/Manual/Auto editor; direct PWM, saved 0-7 values, illumination/status remain if carefully split | Best next A/B cut after macro compiles |
| 3 | Move R1-R8 commissioning page to host | Delta unknown | No offline relay-by-relay test page or local K3 All Off from that page; host direct relay/motion and safe sequencer remain | Good offload candidate; retain a global safe-stop gesture |
| 4 | Remove local `uPWM` editor | Delta unknown | Front-panel cannot edit/save PWM 0-7; host/API can still do it and EEPROM can retain values | Reasonable if PC is normally attached |
| 5 | Remove local RF learning root | Delta unknown | Cannot start/cancel learning or see count without host; RF receive/transmit/mappings may remain | Reasonable only after host learning UX is accepted |
| 6 | Remove board MenuList directory | Delta unknown | Host loses authoritative IDs/labels and can drift back to wrong Status/Voltage numbering | Small likely saving; strongly discouraged |
| 7 | Remove addressable strip support | Named lower bound **219 flash, 34 SRAM** | No D6 WS2811/WS2812 pixels, fill, brightness, custom colors, or future strip effects | Remove only if the strip is physically unused |
| 8 | Simplify status RGB animations to static state | Archived area about **332 named flash**; marginal unknown | Loses breathing/flashing and distinct door/BT/RF/menu/save/discard/hot/fault/reset cues; static power/ready could be retained | High UX cost; A/B before deciding |
| 9 | Remove buzzer/melodies | Archived area about **324 named flash** plus inlined callers; marginal unknown | Loses boot health confirmation, key beep, door/relay cues, save/discard/error sound, streamed tones, and Silent's purpose | Not recommended; user explicitly relies on boot melody |
| 10 | Remove reset telemetry journal | Delta unknown | No persistent reset count and reduced cause diagnostics; safe reset can remain separately | Poor trade during hardware debugging |
| 11 | Remove DS18B20 support | Archived area about **792 named flash** | Loses Temperature LED/BT, ROM identification, host readings, hot warning, and illumination thermal test | Large functional loss |
| 12 | Remove the MCU macro timing queue | Final A/B delta pending | Host could still send individual commands, but precise MCU-clock playback, USB-jitter buffering, queue/fidelity metrics, synchronized cancel, and safe host-loss handling are lost | Do not remove; it is a current core requirement |
| 13 | Remove rc-switch completely | Named lower bound **1,010 flash, 184 SRAM**; true LTO delta larger | Loses 433 MHz RX, TX, learning, 20 mappings, repeats, RF events, and RF-triggered actions/macros | Biggest third-party cut, but contradicts core requirements |
| 14 | Remove front-panel snapshot/host capture | Delta unknown | No exact remote 7-seg/LCD preview, remote physical-equivalent keys, or PC-hosted menus | Do not remove; core PC integration |
| 15 | Use no-bootloader memory profile | Exactly **384 additional application bytes** | Loses normal UART Urboot/Urclock programming, backup, and recovery path; ISP becomes necessary | Do not use for this project |
| 16 | Remove relay interlocks, CRC/bounds, EEPROM validation, watchdog, startup I2C recovery, or safe reset | Not offered | Can energize unsafe outputs, accept corrupt commands/settings, hang, or reset unsafely | **Protected: never a flash optimization** |

If the first five offloads are insufficient, prefer a commissioning/production
profile split or a flash-larger MCU revision. A profile split keeps every
feature in source but means one flashed image cannot use features compiled only
into the other profile. A larger MCU is the only path that preserves all local
UI, richer animations, the macro engine, and future protocol growth without
continually trading them against one another.

## What can be removed without changing current behavior?

Only compiler/linker dead code and redundant representations are truly free.
The remaining useful approaches are:

- keep one canonical payload prefix per writable operation, allow harmless
  appended extension fields, and avoid build-specific decode branches;
- consolidate repeated labels/serializers and let LTO share helpers;
- keep fixed hardware drivers instead of generic APIs;
- keep menus and macro steps data-driven rather than duplicating dispatch;
- reuse ordinary opcode validation for macro playback instead of adding a
  second board allowlist or policy tree;
- offload rich strings, categories, histories, graphs, and custom menus to the
  PC while retaining compact IDs/events on the MCU.

Every such source change still needs a clean A/B build because AVR LTO and
layout-sensitive menu code can make an apparently small edit grow or move
other functions.

## Finalization-only architecture work

These are maintainability requirements, not byte-removal options, and are
deliberately deferred until the protocol and EEPROM layouts stop moving:

1. Add concise, human-readable one-line or short multi-line documentation to
   otherwise unexplained functions, variables, and domain state.
2. Keep `PCController.ino` as a high-level lifecycle/composition file by
   moving remaining domain variables and functions into focused classes and
   custom logic files.

Neither item is complete merely because several controllers already live in
`Project/`. The finalization audit must inspect the master sketch again and
must not claim completion until the final behavior/size build still passes.

## Reproducing the final audit

Use the project controller's compile command so source hash, packed build time,
HEX, merged image, and manifest are regenerated atomically. Do not hard-code a
machine-specific Arduino package path.

```text
Tools\Controller\bin\controller.exe toolchain compile
avr-size.exe -A .build\firmware\PCController.ino.elf
avr-nm.exe -S -l --size-sort --radix=d -C .build\firmware\PCController.ino.elf
```

Report `.text + .data` as application flash and
`.data + .bss + .noinit` as static SRAM. The filesystem length of Intel HEX is
ASCII transport size, not bytes occupied in MCU flash.

For a proposed feature cut:

1. compile the exact final source with the feature enabled;
2. compile the same source with only that feature disabled;
3. record both flash and static SRAM totals;
4. run host/protocol tests for retained paths;
5. if hardware-facing, upload only through the guarded backup-first path and
   verify HELLO hash plus physical behavior;
6. add the exact delta and observed losses to this table.
