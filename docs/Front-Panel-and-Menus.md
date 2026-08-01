# Board Features, Front Panel, and Menus

This is the starter guide to what the ATmega328P firmware actually owns. It
also separates the firmware currently running on the physical board from the
newer source tree, because those are not the same image yet.

## Which menu is on the board right now?

The latest recorded guarded upload/readback checkpoint on COM18 is
`4C980157`. It reports the 15-page directory whose stable page 0 is `STAT` and
retained all 15 EEPROM menu pages in identity order. The latest explicit
physical-key checkpoint remains `2FD9F81C`: reset returned to Status, and
Previous/Next/Decrease/Increase left reset count 9 unchanged. Seeing `STAT`
and then `OPEN`/`CLSd` is therefore expected on both checkpoints.

The current pinned source/CI profile keeps that same 15-page layout and builds
at 32,228/32,384 application-flash bytes, with 1,444/2,048 static SRAM and a
conservative 1,764/2,048 estimated peak. That source profile is newer than the
recorded hardware checkpoints; a successful CI build is not a claim that its
exact HEX has completed physical upload and acceptance. The earlier
`5DF10D05` image had a 14-page menu whose page 0 was `VOLT`; it explains the
old VOLT observation but is historical.

| Image | Root page 0 | Root pages | Status |
|---|---|---:|---|
| Current pinned source/CI profile | `STAT` | 15 | 32,228/32,384 application flash; 1,444 static and 1,764 estimated-peak SRAM; physical acceptance pending |
| Guarded upload/readback checkpoint `4C980157` | `STAT` | 15 | 32,226/32,384 application flash; uploaded and independently flash-verified on COM18; final human button/RF/load-safe pass pending |
| Physical-key checkpoint `2FD9F81C` | `STAT` | 15 | UART-uploaded and flash-verified on COM18; four menu keys did not reset it |
| Historical `5DF10D05` | `VOLT` | 14 | Superseded accepted evidence; no longer the current source or hardware checkpoint |

The previous 5DF order was:

```text
VOLT -> CURR -> tLED -> t-bt -> LItE -> bt -> Snd -> PWM
     -> rELY -> KEY -> uPWM -> r5-8 -> MOVE -> LErn -> VOLT
```

## Physical connections and ownership

| Function | Connection | Firmware behavior |
|---|---|---|
| Four keys | First four active-low 74HC165 bits | Previous, Next, Decrease, Increase/Enter |
| System sense | 74HC165 bits 4 and 5 | Reserved; never treated as keys |
| BT Audio indicator | 74HC165 bit 6 | Classifies the BT-5.0-Pro Audio LED as Off, On, or Blinking |
| Door reed | 74HC165 bit 7 | Door events, illumination target, default-page return, motion handling |
| Relays R1-R8 | Active-low 74HC595 outputs | Two interlocked motion sides plus four general outputs |
| Buzzer | D9 / PB1 | Nonblocking Timer1 tones, boot melody, keys, door, relay, save/error cues |
| Two DS18B20s | D10 / PB2 / CS | `Temperature LED` and `Temperature BT`; external 4.7 kOhm pull-up to VCC is required |
| 433 MHz receive | D2 / INT0 | rc-switch receive, learning, repeat handling, mapped actions, events |
| 433 MHz transmit | D3 / INT1 | Host/protocol transmission; receiver is paused only for the send |
| TM1637 | D13/SCK clock, D11/MOSI data | Cached four-digit local display with 20 ms service cadence |
| Addressable LEDs | D6 | Fixed 11-pixel WS2811/WS2812-compatible sender; current profile uses WS2811/BRG order |
| I2C | A4/SDA, A5/SCL | PWM at `0x41`, INA219 at `0x40`, plus cooperative host transactions |

PWM channel ownership is fixed and deliberately called **PWM**, not PCA, in
the code and UI:

| PWM channels | Role |
|---|---|
| 0-7 | Eight external MOSFET lighting/user outputs |
| 8-10 | Three additional user PWM outputs |
| 11 | Enclosure illumination |
| 12 | Power/On signal light |
| 13-15 | Status RGB: red, green, blue |

The expected I2C addresses do not conflict:

| Address | Device | Notes |
|---:|---|---|
| `0x40` | INA219 | Fixed by the compact measurement driver |
| `0x41` | 16-channel PWM | The address was changed from `0x40` specifically to remove the INA219 collision |
| `0x27` or `0x3F` | Optional PCF8574 LCD backpack | Host-scanned in the offloaded profile; only one should be selected |

Any future device strapped to one of those same addresses would conflict on
the shared bus. DS18B20 and 433 MHz devices are not I2C devices.

The firmware discovers at most two valid DS18B20 ROMs, sorts their 64-bit ROM
codes, and reports each ROM, logical role, and current value through
`TemperatureList`. The current canonical default maps the lower sorted ROM to
`Temperature LED` and the higher ROM to `Temperature BT`; the EEPROM Swap
Temperature Sensors flag reverses that assignment. A controlled illumination
heating test is still the authority for the physical harness.

R1/R2 and R3/R4 are not two independent Up/Down relay pairs. R1 and R3 select
direction; R2 and R4 enable power:

| Side | Direction relay | Enable relay | General relays |
|---|---|---|---|
| A | R1 | R2 | R5-R8 are independent user outputs |
| B | R3 | R4 | |

Changing direction while enabled is sequenced as disable, configurable 1 ms
or 100 ms break, direction change, 50 ms settling, then enable. The two
direction relays are separated by at least 5 ms. Reset and All Off disable the
enable relays before changing directions or other outputs.

## Peripheral initialization profile

This section is the canonical hardware-configuration reference for the
**current working source**. It is not proof that every value below is already
running on COM18: `4C980157` is the latest recorded guarded upload/readback
checkpoint, `2FD9F81C` is the latest explicit physical-key checkpoint, and the
current 32,228-byte source profile still needs its own hardware acceptance.
Values marked EEPROM default apply after an erased or invalid settings record;
a valid board-owned EEPROM record overrides them.

The production target is an ATmega328P at 16 MHz with a 2 s watchdog. UART0 is
started first so the host can receive an early `HELLO`; relay outputs are made
safe before peripherals that can take longer to probe. The relevant startup
order is UART/watchdog, shift registers and relays, system inputs, buzzer,
addressable LEDs, EEPROM/display, recovered I2C plus PWM/INA219, temperatures,
433 MHz, then the nonblocking boot melody.

| Peripheral | Wiring/address | Active initialization | Startup state |
|---|---|---|---|
| INA219 | I2C `0x40`, A4/A5 | 32 V, gain /8, 64-sample bus and 128-sample shunt averaging, continuous conversion | Probed after I2C recovery; unavailable if probe/configuration fails |
| 16-channel PWM (PCA9685 hardware) | I2C `0x41`, A4/A5 | 1 kHz, auto-increment, active-high logical mapping | Every channel is forced off with one retry before EEPROM mode is applied; factory mode is Auto on channels 0-10 |
| Two DS18B20 probes | D10/PB2/CS, shared 1-Wire | Externally powered, 11-bit, asynchronous conversion, maximum two valid ROMs | Scan, sort ROMs, request first conversion; no blocking wait |
| TM1637 | D13/PB5 clock, D11/PB3 data | Fixed-pin open-drain-style sender, 3 us half-cycle, brightness 0-7 | Clear, EEPROM brightness (factory 5), show `boot`; changed segments only thereafter |
| 74HC165/74HC595 | A0-A3, D4, D5, D7, D8 | Active-low inputs and outputs, 5 ms service interval | `/OE` held inactive until an all-off frame has been latched |
| 433 MHz | RX D2/INT0, TX D3/INT1 | rc-switch 2.6.4, receive tolerance 70%, transmit/receive enabled | Receiver active; temporarily paused only while transmitting |
| Cooperative I2C/LCD | A4/A5; LCD commonly `0x27` or `0x3F` | Compact 100 kHz master; 25 ms reset-on-timeout; bounded 16-byte host transfers and 0-10 s leases | Host owns LCD discovery/rendering; MCU renderer is disabled |
| Buzzer | D9/PB1/OC1A | Timer1 CTC hardware toggle, no audio-rate ISR, ten-step queue | Output low and queue empty, then EEPROM mute (factory audible), then boot melody |
| Relays | Eight active-low 74HC595 outputs | R1/R3 direction, R2/R4 enable, R5-R8 general | Both enable relays off first, then every relay off |
| PWM status LEDs | PWM 12-15 | Power signal on channel 12; RGB on 13-15 | Power signal on, RGB Boot mode; EEPROM brightness factory 128 |
| Addressable LEDs | D6/PD6 | 11 pixels, 800 kHz; current build is WS2811/BRG | Pixel buffer cleared and one all-black frame sent |
| UART and bootloader | UART0, 115200 baud | COBS opcode protocol in the application; UART0 Urboot/urclock in boot mode | UART always enabled; host DTR reset-on-reconnect is independently off by default |

### INA219 measurement profile

The INA219 configuration word is `0x3F7F`: 32 V bus range, gain /8, 64x bus
averaging, 128x shunt averaging, and continuous bus-plus-shunt conversion. A
complete averaged conversion is approximately 102 ms. Calibration `4096`
assumes the common 0.1 ohm shunt and gives a 0.1 mA current LSB; bus, shunt,
and power LSBs are 4 mV, 10 uV, and 2 mW respectively. The driver refreshes
calibration before each read because compatible parts can clear it after a
reset.

The firmware reads every 100 ms while the door is open and every 500 ms while
closed. This deliberately favors a responsive open-enclosure display while
giving current the strongest hardware filtering. There is no second firmware
EMA over the INA219 values, so the displayed value is the newest completed
hardware-averaged result.

Safe changes:

- Reduce bus/shunt averaging for a faster but visibly noisier value. Keep the
  read interval no shorter than the chosen conversion time or repeated reads
  will return the same conversion.
- Change calibration only after measuring the installed shunt and choosing a
  new current LSB; otherwise current and power will be numerically wrong.
- Keep the INA219 at `0x40`. `0x41` belongs to PWM specifically to prevent the
  collision that previously produced incorrect voltage behavior.

Canonical sources: [Ina219Sensor.cpp](../Project/Ina219Sensor.cpp),
[BoardPins.h](../LocalLib/BoardPins.h), and the sampling constants in
[PCController.ino](../PCController.ino).

### PWM expander profile

The PCA9685 silicon is intentionally named **PWM** in code, menus, protocol,
and documentation. At address `0x41`, the compact driver enables MODE1
auto-increment, waits 1 ms, calculates an integer prescaler from the nominal
25 MHz oscillator, sleeps the device while writing the prescaler, waits 500
us, then restarts it. The requested 1 kHz setting uses prescale 5 and is about
1.017 kHz with an exact 25 MHz oscillator. The driver permits 24-1526 Hz, but
frequency is global to all 16 channels.

MODE2 is normalized to `0x04` for this active-high hardware. Logical values
are always 0=inactive and 4095=fully active; the compact driver uses the
PCA9685 FULL_OFF/FULL_ON bits at the endpoints. A future electrically inverted
stage can be built with `PCCONTROLLER_PWM_ACTIVE_LOW=1`, which changes the
mapping and MODE2 to `0x05`; it must not be changed merely to compensate for a
wrong address or wiring fault.

At startup the controller writes logical zero to all 16 channels, with one
forced retry per failed channel. Three consecutive later write failures mark
PWM unavailable. The EEPROM factory mode is Auto: channels 0-10 only fade one
at a time in steps of 128 every 20 ms. Channels 11-15 are excluded because
they are owned by enclosure illumination, Power/On, and status RGB. Changed
values are cached, preventing redundant I2C writes and visible jitter.

Safe changes:

- Use Off or Manual as the saved boot mode after hardware commissioning. Auto
  is intentionally conspicuous and changes user outputs 0-10 after every
  reset.
- Frequency can be changed within 24-1526 Hz, but it affects MOSFET lighting,
  illumination, Power/On, and RGB together. Revalidate flicker, switching
  losses, and attached-module input limits.
- Never add channels 11-15 to the automatic test mask during normal use.
- Change active polarity only for a verified inverted output stage, then test
  the all-off frame before connecting loads.

Canonical sources: [PwmExpanderDriver.cpp](../Project/PwmExpanderDriver.cpp),
[PwmController.h](../Project/PwmController.h),
[PwmController.cpp](../Project/PwmController.cpp), and
[ProjectConfig.h](../ProjectConfig.h).

### DS18B20 temperature profile

Both externally powered DS18B20s share D10/PB2/CS. The bus **requires an
external 4.7 kOhm pull-up to VCC**; the bounded custom driver returns a
disconnected value rather than waiting forever if presence is absent. Parasite
power is not supported because conversion would also need a strong pull-up.

The scan keeps at most two valid, CRC-checked DS18B20 ROMs, sorts them
lexicographically, configures 11-bit resolution (0.125 C, 375 ms maximum), and
uses one Skip-ROM conversion for both probes. Conversion and scratchpad reads
are asynchronous: requests begin every 450 ms while the door is open and every
1000 ms while closed. A 50/50 integer EMA smooths each valid sample; a sample
at or above 50.00 C bypasses the filter so HOT indication is not delayed.
1-Wire service is suspended during RF learning because its brief interrupt
masking can disturb received pulse timing.

The current canonical default maps the lower sorted ROM to `Temperature LED`
and the higher ROM to `Temperature BT`. The EEPROM Swap Temperature Sensors
setting reverses logical roles without editing firmware. The host can query
both ROM identities and current values; confirm the physical identity by
heating the enclosure illumination and verifying only tLED rises materially.

Safe changes:

- 9- or 10-bit resolution reduces conversion time but quantizes temperature
  more coarsely; 12-bit gives 0.0625 C but needs up to 750 ms. Change the
  firmware conversion deadline together with resolution.
- Use the EEPROM role-swap setting when probes move. Do not hard-code a newly
  observed ROM order into the driver.
- Do not remove the pull-up or enable parasite power without adding a proper
  strong-pull-up design.

Canonical sources: [DallasTemperatureBus.h](../LocalLib/DallasTemperatureBus.h),
[DallasTemperatureBus.cpp](../LocalLib/DallasTemperatureBus.cpp), and the
discovery/service logic in [PCController.ino](../PCController.ino).

### TM1637 and shift-register profile

The TM1637 driver owns the board's SCK and MOSI-labelled pins as ordinary GPIO;
it is not using hardware SPI. D13 is clock and D11 is bidirectional data. It
uses a 3 us bus delay, sends `0x40` data and `0xC0` address commands, releases
the data line for the TM1637 ACK slot, and clamps brightness to 0-7. EEPROM
factory brightness is 5. The main display service runs every 20 ms, but the
driver compares all four segment bytes and sends nothing when they are
unchanged. That preserves the responsive UI without continuously refreshing
identical data.

The shared 74HC165/74HC595 driver maps A0 to 165 QH, A1 to 595 RCLK, A2 to the
shared clock, A3 to 595 SER, D4 to 165 `/PL`, D5 to 595 `/SRCLR`, D7 to 165
`/CE`, and D8 to 595 `/OE`. Outputs are active-low and shifted LSb-first;
captured inputs are read MSb-first then inverted into logical active bits. The
input load pulse is 5 us. `/OE` starts as an input with pull-up so outputs stay
disabled, an all-ones/off frame is shifted and latched, and only then is `/OE`
driven low. The combined chain is serviced every 5 ms. Keys use 50 ms debounce;
door and BT sense inputs use 40 ms debounce.

Safe changes:

- TM1637 brightness and decimal placement are EEPROM/menu settings. Increasing
  the 20 ms UI cadence provides little benefit because writes are already
  changed-only; reducing it makes button and live-value feedback feel slower.
- Change shift bit order or active polarity only after tracing the actual PCB.
  A wrong output polarity can energize all relays at startup.
- Preserve the `/OE`-disabled, all-off-frame, `/OE`-enable ordering even if the
  driver is refactored.

Canonical sources: [SevenSegments.cpp](../LocalLib/SevenSegments.cpp),
[ShiftRegisters.cpp](../LocalLib/ShiftRegisters.cpp),
[BoardPins.h](../LocalLib/BoardPins.h), and [SystemInputs.cpp](../Project/SystemInputs.cpp).

### 433 MHz receive/transmit profile

Receive is on hardware interrupt INT0/D2 and transmit is on INT1-labelled D3.
Two rc-switch 2.6.4 instances are used so receiving can remain configured while
the transmitter changes protocol. Startup selects 70% receive tolerance and
enables both pins. A send supplies a nonzero 1-32-bit code, protocol 1-12, and
an optional learned pulse length; the receiver is disabled for the blocking
send and immediately re-enabled. The rc-switch transmitter retains its
library default of ten repeats unless that library setting is changed.

Received codes carry code, bit count, protocol, and pulse length. Identical
codes within 400 ms are treated as repeats: ordinary toggle actions are
suppressed, while Momentary, Up, and Down mappings refresh their active window.
Learning suppresses duplicate repeats, supports single, multiple, timed, and
indefinite sessions, and stores at most 20 learned entries in board EEPROM.

Safe changes:

- Lower receive tolerance rejects noisy or drifting remotes; higher tolerance
  accepts a wider pulse range and more interference. Record real remote pulse
  samples before changing 70%.
- Reducing transmit repeats shortens the time the receiver is unavailable but
  can reduce receiver reliability. Increasing repeats does the opposite.
- Keep 1-Wire reads paused during learning and keep the buzzer on hardware
  Timer1; both choices protect INT0 pulse timing.

Canonical sources: the radio service and `transmitRadio()` in
[PCController.ino](../PCController.ino), [BoardPins.h](../LocalLib/BoardPins.h),
and the pinned dependency record in [Project Checklist](Project-Checklist.md).

### Cooperative I2C and PC-owned LCD profile

The firmware uses its fixed-hardware `CompactI2c` master rather than MiniCore
`Wire`. At 16 MHz it selects prescaler 1 and `TWBR=72`, giving an explicit
100 kHz bus. Before enabling TWI, SDA and SCL are released with their weak
internal pull-ups and sampled; reliable hardware still requires appropriate
external bus pull-ups. A stuck-low SCL aborts startup. A stuck-low SDA receives
nine recovery clocks (5 us low plus 5 us high) followed by a generated STOP,
then both lines must read high. Every TWI wait has a 25 ms deadline and resets
the peripheral—not the MCU—on timeout; the 2 s watchdog remains the final
system-level bound. The unstable MiniCore `WIRE_TIMEOUT` path is not linked.

The generic `I2cTransfer` opcode intentionally has no device allow-list. It can
write up to 16 bytes and read up to 16 bytes from any 7-bit device. A 0-10 s
cooperative lease pauses firmware-owned INA219/PWM bus work so the host can
perform a multi-transfer operation; address zero releases the lease. Keep
leases short because measurements, illumination, and PWM output updates wait
while the lease is active.

The full AVR HD44780 renderer is compiled out. The host's LCD service is on by
default, scans conventional PCF8574 addresses `0x27` then `0x3F`, initializes
the first match, and owns rich 16x2 rendering. Prompt mirroring is a separate
host setting and is off by default. The host preloads hidden DDRAM with
`PC offline      ` and `Connect USB toPC`; after more than 5 s without host
activity, the MCU can reveal that preloaded page using display shifts without
carrying a second renderer or those runtime strings.

Safe changes:

- Other I2C addresses can already be accessed through the generic opcode.
  Extend the host LCD address list if a backpack is strapped elsewhere.
- Do not place a new device at `0x40` or `0x41`. Avoid two LCD backpacks on the
  same address.
- A faster I2C clock may work, but validate every installed module, cable
  length, pull-up, and the host-driven LCD before changing it.
- Re-enabling the AVR LCD renderer restores standalone rich LCD text but costs
  the measured 1328 flash bytes and 49 SRAM bytes documented in
  [Firmware Size, Feature Cost, and Removal Tradeoffs](Memory-and-Feature-Tradeoffs.md#avr-lcd-renderer-disabled-standalone-physical-lcd-lost).

Canonical sources: the compact master in
[CompactI2c.cpp](../Project/CompactI2c.cpp), I2C leasing/offline-shift logic in
[PCController.ino](../PCController.ino), [ProjectConfig.h](../ProjectConfig.h),
and host presentation defaults in
[appconfig/config.go](../Tools/Controller/internal/appconfig/config.go).

### Buzzer, relays, illumination, and status-light profile

The buzzer is fixed to D9/PB1/OC1A. Timer1 runs CTC with OCR1A as TOP and
hardware-toggles OC1A, selecting the first usable prescaler from 1, 8, 64, 256,
or 1024. No audio-rate interrupt runs, so tones cannot starve INT0/INT1 radio
edges; Timer0 (`millis`/`micros`) and Timer2 remain untouched. The nonblocking
queue holds ten frequency/duration steps, including zero-frequency pauses.
The ordinary key cue is 40 ms at 2 kHz. Silent mode stops electrical tone
output while queue timing continues; factory EEPROM is audible. Servo or
`analogWrite()` on D9/D10 would conflict with this Timer1 ownership and must
not be introduced.

All relay stages are active-low behind the 74HC595. Startup and All Off first
commit R2/R4 disabled, then clear directions and R5-R8. A reversal while
enabled is sequenced as Enable off, EEPROM-selectable 1 ms (factory) or 100 ms
break, direction relay change, 50 ms mechanical settle, then Enable on. The A
and B direction relays cannot change in the same service pass and are separated
by at least 5 ms. Motion door policy is an EEPROM setting: Always (factory),
Closed Only, Open Only, or Never. R5-R8 are independent general outputs.

Enclosure illumination is PWM channel 11. Auto mode targets the configured On
brightness while the debounced door is open and Off brightness while closed;
factory values are 128 and 0. It moves by four 8-bit levels every 20 ms and
accounts for delayed service without wrapping, preventing the previously seen
fade jitter. Off and On modes select the same stored endpoints directly.

The Power/On indicator is PWM channel 12 and turns on when status control
starts. Status RGB is channels 13-15. EEPROM factory brightness is 128; Ready
color choices are red, blue, violet, green, and white. Boot is amber, Learning
breathes blue, HOT/Warning breathes orange, Fault breathes red, and short cues
cover door, BT Audio, menu, radio, save, discard, and graceful reset. Animated
levels change by four every 20 ms.

Safe changes:

- Melody notes/durations, cue enable flags, display/status brightness, Ready
  palette choice, illumination mode/endpoints, motion policy, and 1/100 ms
  break are safe EEPROM/host settings.
- Keep the 50 ms direction settle and 5 ms cross-side interlock unless relay
  hardware has been measured. Removing them changes physical safety behavior,
  not just UI feel.
- Never use Timer1, D9, or D10 for Servo/Arduino PWM while the buzzer driver is
  linked.
- Status palettes can be changed in flash or driven temporarily through the
  RGB opcode; do not move channel ownership without changing the wiring map and
  all-off logic together.

Canonical sources: [TonePlayer.cpp](../LocalLib/TonePlayer.cpp),
[RelayController.cpp](../Project/RelayController.cpp),
[IlluminationController.cpp](../Project/IlluminationController.cpp), and
[StatusLedController.cpp](../Project/StatusLedController.cpp).

### Addressable LED profile

The separate addressable strip owns D6/PD6 and has a fixed count of 11 pixels.
At 16 MHz the compact sender emits an 800 kHz frame with interrupts masked for
approximately 330 us for 33 color bytes, restores interrupts, then waits 80 us
for the latch. The current `PCCONTROLLER_USE_WS2812B=0` profile sends BRG order
for the inherited WS2811 hardware; setting it to 1 builds GRB order for
WS2812B. Startup drives D6 low, clears the 33-byte pixel buffer, and sends an
all-black frame.

Brightness is deliberately host-pre-scaled: the fifth opcode byte is retained
for wire compatibility, but this tight AVR implementation ignores it and
reports brightness 255. Keeping a second unscaled buffer or multiplying all
pixels on the AVR would consume flash/RAM that the timed macro queue now uses.

Safe changes:

- Switch BRG/GRB with the compile-time flag only after identifying the strip;
  a wrong order changes colors but is not an electrical fix.
- A different pixel count requires code, global buffer RAM, stack encoding
  space, and a longer interrupt-off interval. Re-run flash/SRAM and RF receive
  acceptance tests.
- Pre-scale color/brightness in the host. Old clients that rely on the fifth
  byte changing brightness are not compatible with this compact profile.

Canonical sources: [AddressableLeds.cpp](../Project/AddressableLeds.cpp),
[AddressableLeds.h](../Project/AddressableLeds.h), and
[ProjectConfig.h](../ProjectConfig.h).

### UART application link and Urboot/urclock profile

The MiniCore target is:

```text
MiniCore:avr:328:bootloader=uart0,eeprom=keep,baudrate=115200,
variant=modelP,BOD=2v7,LTO=Os_flto,clock=16MHz_external
```

UART0 is always enabled at 115200 baud and is the primary application link,
not a debug-text console. Application frames use zero-delimited COBS, magic
`0xA5`, protocol version 1, CRC-8, sequence IDs, opcodes, and a maximum 48-byte
payload. Timed events and ACKs append the MCU `micros()` timestamp so the host
can distinguish device execution time from USB/network arrival time. Firmware
starts UART and emits an early `HELLO` before slower peripheral initialization,
then sends final `HELLO` and telemetry after the boot melody is queued.

The application limit is 32,384 bytes (`0x0000`-`0x7E7F`). The 384-byte UART0
Urboot/urclock loader occupies `0x7E80`-`0x7FFF`; application opcodes and
bootloader commands are mutually exclusive modes on the same UART. EEPROM is
kept across programming and brown-out detection is 2.7 V. The PC host opens
serial with DTR and RTS inactive and `reset_on_reconnect=false`, so merely
opening the app does not reset the board. An explicit DTR pulse or programming
workflow can enter the boot window.

Safe changes:

- Baud must match the application, host, and chosen bootloader build. Changing
  only one side produces `HELLO` timeouts or failed programming.
- A no-bootloader build recovers 384 flash bytes but removes UART programming,
  Urboot/urclock backup/restore, and graceful boot-mode integration; ISP then
  becomes mandatory.
- Changing BOD, clock source, or EEPROM preserve policy is a fuse/bootloader
  operation. It must be done through the guarded programming workflow, not as
  an ordinary board setting.
- Keep DTR reset-on-reconnect disabled unless a specific deployment wants a
  reset after physical USB reattachment.

Canonical sources: [ProjectConfig.h](../ProjectConfig.h),
[UartProtocol.h](../Project/UartProtocol.h),
[UartProtocol.cpp](../Project/UartProtocol.cpp), the latest checkpoint
[firmware-manifest.json](../.build/firmware/firmware-manifest.json), and the
host serial mode in [session.go](../Tools/Controller/internal/link/session.go).

## Key gestures

The staged key engine debounces for 50 ms. A physical down edge performs the
first action immediately and emits an event to the host. It then recognizes:

| Gesture | Timing and effect |
|---|---|
| Press | One menu action immediately after debounce; a brief beep unless Silent is enabled |
| Double click | Second release within 300 ms; double-click K1 returns to the configured default page |
| Hold start | 600 ms after press |
| Hold repeat | Every 150 ms, then every 60 ms after 1.8 s |
| Hold release | Emitted when a held key is released |
| Raw Down/Up | Emitted for host automation and faithful virtual-key mirroring |

Motion and Push-relay control use true down/up behavior rather than repeated
menu actions. The KEY identification page suppresses ordinary hold repeat;
holding K1 or K2 leaves it in the corresponding direction.

The requested alternative gesture policy—defer the single action until the
hold threshold, then invoke a distinct long-hold action—is **not** the current
firmware behavior. The current behavior remains immediate press plus later
hold/repeat events.

## Staged local menu directory

On every ordinary root page, K1 goes to the previous page, K2 to the next, K3
is Decrease/Off, and K4 is Increase/On/Enter unless the page overrides it.
Page navigation wraps at both ends. A page label is shown for about 650 ms,
then the live value is rendered.

| ID | Label | What the display shows | K3/K4 behavior |
|---:|---|---|---|
| 0 | `STAT` | Door `OPEN` or `CLSd` | Read-only |
| 1 | `VOLT` | INA219 supply voltage using 0-2 configured decimals | Read-only |
| 2 | `CURR` | INA219 current using 0-2 configured decimals | Read-only |
| 3 | `tLED` | Enclosure-light sensor as `LxxC` | Read-only |
| 4 | `t-bt` | BT Audio sensor as `bxxC` | Read-only |
| 5 | `LItE` | Illumination `oFF`, `Auto`, or `on` | K4 opens Illumination editor |
| 6 | `bt` | BT Audio `boFF`, `b-on`, or `bLnk` | Read-only |
| 7 | `Snd` | `Mute` or `Snd` | K4 opens Board Settings editor |
| 8 | `PWM` | Alternates mode/channel (`A-00`, `M-00`, `O-00`) and value | K4 opens all-channel commissioning editor |
| 9 | `rELY` | Alternates selected R1-R8 and Off/On | K3 All Off; K4 opens relay commissioning |
| 10 | `KEY` | `KEY`, then 1-4 for about 900 ms | Any key identifies itself; hold K1/K2 to leave |
| 11 | `uPWM` | Alternates user channel 1-8 and its stored 8-bit value | K4 opens persistent user-PWM editor |
| 12 | `r5-8` | Active R5-R8 mask as 0-15 | K4 opens general-relay control |
| 13 | `MOVE` | Door `OPEN` or `CLSd` | K4 enters motion if the configured door policy permits it |
| 14 | `LErn` | Learned RF count, or unavailable dashes | K4 starts one-code 15-second learning |

The board-authoritative `MenuList` opcode returns these IDs, program-mode IDs,
and four-character labels in pages. New host code must use that response rather
than assuming the older 5DF order.

The current `STAT` renderer is a door-status home page. It does not yet
automatically replace the four digits with every relay/RF/BT/macro event as
originally requested. The host can inject temporary segment text, and events
already arrive over UART, but the automatic local event-overlay UX remains an
acceptance item.

The TM1637 service itself runs every 20 ms and writes only changed segments.
INA219 conversions are hardware-filtered with 64x bus and 128x shunt/current
averaging; the firmware reads them every 100 ms while the enclosure is open
and every 500 ms while closed. There is no second voltage/current software EMA,
so the display is responsive but holds the last averaged sample between reads.
Temperatures use asynchronous 11-bit conversions, 450/1,000 ms open/closed
request periods, and a 50/50 EMA after the first valid sample; HOT readings are
not delayed by that EMA.

### Program state-machine modes

Telemetry and `FrontPanelGet` expose the active `ProgramMode`, not just the
root page. This is the restored mode manager used to keep menu transitions and
modal editors deterministic:

| Mode ID(s) | State(s) | Purpose |
|---:|---|---|
| 0 | Boot | Shows `BOOT`, initializes hardware, then enters the EEPROM default page |
| 1-15 | Status through RF | One mode for each root page ID 0-14; mode ID is page ID + 1 |
| 16-18 | Illumination Mode/On/Off Edit | Three-field illumination transaction |
| 19 | Sound/Board Settings Edit | Six-item board-settings submenu |
| 20-22 | PWM Mode/Channel/Value Edit | All-channel commissioning transaction |
| 23-24 | Relay Channel/Value Edit | Live R1-R8 commissioning |
| 25-26 | User PWM Channel/Value Edit | EEPROM PWM 0-7 transaction |
| 27-29 | User Relay Channel/Behavior/Control | R5-R8 Toggle/Push control |
| 30 | Motion Control | Held Side A/B motion keys and combined-key exit |
| 31 | Save Prompt | Alternates `SAVE`/`diSC` until the user confirms |
| 32 | Flash Message | About 900 ms of flashing saved/discarded feedback |
| 33 | RF Learning | Timed, multi-code, or indefinite learning lifecycle |
| 34 | Fault | Reserved `Err`/audio/RGB terminal state; no current source path enters it automatically |
| 255 | Undefined | Sentinel only; never a normal display state |

Host-panel capture is a separate flag layered over these modes. It forwards
keys to the PC without deleting the underlying local/default state.

### Illumination editor

Enter from `LItE` with K4:

1. `L-Md`: Off, Auto, or On.
2. `L-on`: configured On brightness, 0-255.
3. `L-oF`: configured Off brightness, 0-255.
4. `SAVE`/`diSC` confirmation.

K1/K2 move between fields, while K3/K4 decrease/increase. Modes and values
wrap; brightness changes in nominal 16-count steps. Auto selects On brightness
while the door is open and Off brightness while closed. Channel 11 moves in
four-count steps every 20 ms toward the target; it is not supposed to jump.

### Board Settings editor

Enter from `Snd` with K4. This modal sequence is the closest thing the staged
local UI has to a settings submenu:

| Item | Label | Values | K3/K4 |
|---:|---|---|---|
| 1 | `Snd` | Mute or Sound | K3 Mute, K4 Sound |
| 2 | `diSP` | TM1637 brightness 0-7 | Decrease/increase with rollover |
| 3 | `StBr` | Status RGB brightness 0-255 | Nominal step 16 with rollover |
| 4 | `CoLr` | Ready color 0-4: red, blue, violet, green, white | Decrease/increase with rollover |
| 5 | `V-dP` | Voltage decimals 0-2 | Decrease/increase with rollover |
| 6 | `A-dP` | Current decimals 0-2 | Decrease/increase with rollover |

Each field label is shown for about 650 ms and its value then blinks at about
300 ms. Sound, display brightness, and RGB settings preview during editing;
Discard restores the entire snapshot. EEPROM stores the compact numeric Ready
color index; the five names above are its fixed local order. Host-side named
categories and their independently assigned colors remain PC configuration.

### All-channel PWM commissioning

Enter from `PWM` with K4:

1. `P-Md`: Off, Manual, or Auto.
2. `P-Ch`: channel 0-15, when Manual is selected.
3. `P-u`: logical value 0-4095, adjusted in 256-count steps with rollover.
4. Save/Discard.

Auto fades one channel up and down in 128-count steps every 20 ms and advances
through channels 0-10. It deliberately excludes system-owned channels 11-15.
Manual can select all 16 channels, but the enclosure/status controllers may
overwrite their owned channels 11 and 13-15 during normal service. Off clears
the commissioning mask 0-10, not the system-owned outputs.

Factory settings currently select Auto at boot. That is useful for identifying
MOSFET wiring but should be changed to Off or Manual once commissioning ends.

### Relay commissioning

On `rELY`, K3 immediately requests All Off. K4 opens:

1. `r-Ch`: select R1-R8.
2. `r-on`: K3 Off, K4 On.

K1 returns to channel selection and K2 returns to the root page. This editor
changes live outputs and has no EEPROM Save prompt. For R1/R3, Off/On means
Forward/Reverse direction request; for R2/R4 it means Disable/Enable. R5-R8
are direct general outputs. The motion door policy is still enforced for
activating R1-R4.

### Persistent user PWM

Enter from `uPWM` with K4:

1. `uP-C`: select channel 1-8.
2. `uP-u`: edit its 8-bit value, 0-255, in nominal 16-count steps.
3. Save/Discard.

Entering selects Manual mode and applies all eight stored values to PWM 0-7.
The firmware expands each 8-bit setting to the 12-bit hardware range. This
page does not persist channels 8-10; use the host/direct PWM API for them.

### R5-R8 general relay control

Enter from `r5-8` with K4:

1. `ur-C`: select R5-R8.
2. `ur-M`: choose `toGL` with K3 or `PuSH` with K4.
3. Control the selected relay.

In control, K3 forces Off. In Toggle mode, K4 toggles. In Push mode, K4 is On
only while held and release turns it Off. K1 returns to behavior selection;
K2 turns the relay Off and returns to `r5-8`. The selected behavior is runtime
menu state, not an EEPROM setting.

### Two-side motion control

Enter from `MOVE` with K4 when the EEPROM motion-door policy allows it:

| Key | While held |
|---:|---|
| 1 | Side A Forward |
| 2 | Side A Reverse |
| 3 | Side B Forward |
| 4 | Side B Reverse |

Release stops that side. Hold K1+K2 or K3+K4 together for 600 ms to stop all
relays and exit. A door-close edge exits a local motion session and returns to
the default page. The host-configurable policy is Always, Closed only, Open
only, or Never; the erased/factory default is Always. Host/API motion that is
not running through the local MOVE page follows the selected policy.

### Save and discard

Illumination, Board Settings, PWM mode, and user-PWM editors snapshot their
settings on entry. At the confirmation display:

- K2 or K4 saves to CRC-checked EEPROM.
- K1 or K3 discards and restores the snapshot.
- `SAVE` or `diSC` flashes for about 900 ms.
- Save uses a rising audio/RGB cue; Discard uses an error/descending cue.
- Silent mode mutes the audio but not the visual cue.

### 433 MHz learning and mappings

K4 on `LErn` starts a 15-second, one-new-code learning session. `LErn` remains
on the display. K3 cancels; timeout, cancel, full storage, and successful end
are sent as distinct events. Unlike the old guide, K3 on the idle RF root does
**not** clear all records.

The staged EEPROM holds 20 records. Each record stores ID, code, bit count,
protocol, pulse length, action kind/value, behavior, and checksum. Newly
learned codes are deliberately unmapped; the user or host must choose an
action. The host can also start multi-code or indefinite sessions, reorder and
atomically replace records, remove records, and choose hexadecimal or decimal
presentation without changing the stored RF value.

Board-executable mappings currently include:

- physical-equivalent Key or Menu action;
- R5-R8/general relay Press, Toggle, or 350 ms Momentary action;
- Side A/B Up, Down, or Stop through the motion interlock;
- PWM channel 0-10 Press, Toggle, or 350 ms Momentary action;
- no action.

All received codes are reported to the host, including unknown codes. The host
may map them to host commands, OS virtual keys, IPC, webhooks, macros, or other
automations without consuming more AVR mapping code.

## Local menus versus PC-hosted menus

There are two intentionally different menu systems:

| Property | Local AVR menu | PC-hosted menu |
|---|---|---|
| Definition lives in | Firmware flash | Host JSON/YAML/TOML configuration |
| Available without PC | Yes | No |
| Capacity | 15 fixed root pages plus modal editors | Up to 32 menus, 32 items each in current host validation |
| Nesting | Only the fixed editors above | Read-only, text, number, bool, select, submenu, and action items |
| Physical keys | Handled by AVR | Forwarded as events to host while captured |
| TM1637/LCD content | Rendered/staged by AVR | Host pushes an exact shared front-panel representation |
| Persistence | Board EEPROM | Host configuration/data files |

### Current PC-hosted menu inventory

The host's built-in configuration currently defines three hosted menus. These
are not additional AVR page IDs and do not consume board EEPROM:

| Hosted menu | Current items | Availability |
|---|---|---|
| `host` / `HOST` | Host status, device status, host IP, API/link status, PC Settings submenu, System Actions submenu | Implemented in host configuration |
| `pc-settings` / `CFG` | Application title, poll interval, PC LCD service, LCD prompt mirroring, DTR reset-on-reconnect | Implemented; writes the PC configuration, not MCU EEPROM |
| `system-actions` / `SYS` | Lock, sleep, and shut down Windows | Present but guarded and disabled by default |

A hosted Macro library/record/playback page, RF organizer, richer MCU-settings
page, and other PC/OS panels are requested migration targets; they are not
part of the three built-in defaults above yet. Custom definitions can add up
to 32 menus with 32 items each, but a configured definition is not proof that
its read/write/action callback is implemented.

The `lcd_service_enabled` PC setting defaults to true; prompt mirroring remains
independently false. New/default configurations get both `CFG` items. Existing
user-customized `host_menus` arrays are not silently rewritten, so users may
add the service item explicitly if they want it on an older custom panel.

### Menu-placement decision matrix

No local page is migrated by this document. The following table makes each
choice explicit so a flash cut can be approved with its offline loss visible.
Per-page flash/SRAM deltas are **not isolated**: the archived whole local-menu
area was about 1,570 named flash bytes plus about 722 bytes for TM1637, and
labels, render paths, editors, and shared state overlap. “Unmeasured” therefore
means an A/B compile is required; it does not mean zero.

| AVR page | Proposed placement | Why keep or move | What is lost offline if moved | Required host/protocol path | Size or host-side gain |
|---|---|---|---|---|---|
| 0 `STAT` | **Keep local** | Safe home page, door state, and hosted-menu request point | No standalone door/default status and no simple front-panel host-menu entry | Door/status events, `FrontPanelGet`, capture/release, `DisplayText` | Flash/SRAM unmeasured; host can add history but cannot replace the offline home |
| 1 `VOLT` | Keep local; optional cut only by choice | The user explicitly uses the segment display to validate the 12 V supply | No supply-voltage reading without a connected host | `Status`/telemetry plus host-rendered display | Likely small render/label saving, unmeasured; sensor driver remains |
| 2 `CURR` | Keep local; optional cut only by choice | Immediate current diagnosis is useful at the enclosure | No standalone current view | `Status`/telemetry plus host-rendered display | Likely small render/label saving, unmeasured; host gains richer SI formatting/graphs |
| 3 `tLED` | **Keep local** | Confirms the illumination sensor and HOT behavior | No standalone enclosure-light temperature view | `TemperatureList` or telemetry plus hosted display | Small display-path saving only unless the sensor feature is also removed |
| 4 `t-bt` | Keep local; medium-priority optional move | Identifies the BT Audio sensor independently of the PC | No standalone BT-module temperature view | `TemperatureList` or telemetry plus hosted display | Small display-path saving, unmeasured; host gains ROM/name/detail presentation |
| 5 `LItE` + editor | **Keep controller local; editor is splittable** | Door Auto behavior and brightness must continue without a PC | If the editor moves, stored lighting still works but cannot be changed locally | `GetSettings`/`SetSettings`, door events, hosted number/select items | Editor delta unmeasured; host gains precise sliders, names, and validation |
| 6 `bt` | **Keep local** | Gives direct BT Audio Off/On/Blinking diagnosis | No standalone BT Audio connection indication | Status plus immediate BT event | Small render/label saving, unmeasured; host gains event history and automations |
| 7 `Snd` + Board Settings | Split candidate: keep quick Silent control, move advanced fields if space is needed | Silent must remain reachable; detailed display/RGB/decimal settings are easier in a host table | Moved fields retain EEPROM values but cannot be changed from the panel | `GetSettings`/`SetSettings` and hosted bool/number/select items | Good host UX gain; marginal flash/SRAM requires A/B because Save/Discard is shared |
| 8 `PWM` commissioning | **Best local-to-host candidate** | Auto channel identification is development tooling, while direct PWM remains | No offline Off/Manual/Auto channel test or local fade demo | `PwmGet`, `PwmSet`, `PwmMode`, macro playback, output-state events | Delta unmeasured; host gains 16 sliders, labels, graphs, and repeatable tests |
| 9 `rELY` commissioning | Good move candidate after retaining an obvious All Off | Relay-by-relay tests fit the host better; interlocks stay in firmware | No offline selected-relay test; K3 All Off on this page disappears unless relocated | Relay/motion commands and relay events | Delta unmeasured; host gains labeled buttons and live source attribution |
| 10 `KEY` | **Keep local** | Tiny, valuable wiring/identity test that does not require a working UART | No offline way to identify K1-K4 | Raw key events and front-panel capture could emulate it | Likely small, unmeasured saving; little host gain beyond event logging |
| 11 `uPWM` editor | Good move candidate | Host sliders are more precise and can retain named presets/macros | PWM 0-7 values still run from EEPROM but cannot be edited/saved locally | `PwmGet`/`PwmSet`, settings read/write or an explicit stored-values opcode | Delta unmeasured; host gains names, exact 0-4095 values, presets, and macros |
| 12 `r5-8` | Keep unless PC-always-connected operation is accepted | It is the requested standalone general-output control | No local Toggle/Push control of R5-R8 | Relay commands, key capture, relay events | Delta unmeasured; host gains keyboard/mouse/hotkey bindings and state attribution |
| 13 `MOVE` | **Keep local** | Direct motion should not depend on Windows, USB, or a network bridge | No front-panel Side A/B held motion when the PC is absent | Motion commands/events plus continued MCU interlock and door policy | Safety/availability cost dominates any unmeasured saving |
| 14 `LErn` | Move only after host learning UX is physically accepted | Rich naming, categories, reordering, multi/indefinite learn belong on the PC | Existing RF mappings still run, but new learning/cancel/count needs a PC | Learn start/cancel/list/replace/map opcodes and RF events | Delta unmeasured; substantial host UX gain and richer strings |
| Shared Save/Discard UI | Keep while any local EEPROM editor remains | Gives atomic preview, confirmation, audio, and flashing feedback | Removing it makes local edits immediate or host-only | Settings ACK/readback if hosted | Shared-code delta unmeasured; cannot claim the whole saving for one removed editor |
| `MenuList` directory | **Keep** | Prevents host/firmware ID drift and describes the actual image | Host must hard-code labels/order and can navigate the wrong page | Paginated `MenuList` response | Likely small saving, unmeasured; removing it creates a protocol compatibility risk |

Recommended decision order is: keep the safe/status/measurement/motion core;
first A/B-test moving `PWM`, then `rELY`, `uPWM`, and finally `LErn`; consider
splitting advanced `Snd` settings after those. Preserve local `STAT`, `LItE`,
`KEY`, `r5-8`, and `MOVE` unless the user explicitly accepts their stated
offline losses.

The host opens a captured session with `DisplayText` target 3 and releases it
with target 4. While captured, the board does not execute local key actions;
it sends the key gestures to the host. If host traffic disappears for more
than five seconds, the board releases capture and returns to its configured
default page. Holding K4 on `STAT` can request the configured hosted menu when
the host enables that gesture.

Typical commands are:

```text
host-menu list
host-menu open host
host-menu status
host-menu key K2 press
host-menu key K4 press
host-menu close
```

See [Hosted Front-Panel Menus](../Tools/Controller/docs/Hosted-Front-Panel-Menus.md)
for the host schema and command/API equivalents.

### PC-owned operational state

`ProgramState` is a PC-host concept with the values Idle and Running. It is
not the reed-switch state, not a local AVR menu mode, and not an MCU EEPROM
setting. The TUI, CLI, JSON-RPC/IPC/network API, and embedded C/Go consumer can
set it through the one primary host. The host publishes the change as an event
and mirrors appropriate event/text presentation to the physical and virtual
front panels.

Door remains an immediate hardware state reported by the board. When the door
opens while ProgramState is Running, the host can emit its configured warning
event, beep, and actionable desktop toast. That host warning complements, but
does not replace, the firmware's relay sequencing, motion-door policy, or
door-close local-session stop.

## LCD ownership in the staged profile

The full AVR HD44780/PCF8574 renderer is disabled in `ProjectConfig.h` to make
room for the timed macro queue. The generic cooperative I2C transaction opcode
remains enabled, and firmware-owned PWM/INA polling pauses during a bounded
host lease.

`DisplayText` still retains 32 characters of desired LCD text so the TUI,
front-panel snapshot, bridge, and hosted menus share one logical state. In the
current source snapshot, however, that alone does not make characters appear
on the physical LCD. The PC host needs to:

1. scan the expected PCF8574 addresses (normally `0x27` and `0x3F`);
2. initialize the HD44780 in 4-bit, two-line mode;
3. translate each 16-character row into PCF8574 nibble/Enable writes through
   the bounded generic I2C opcode;
4. cache rows so unchanged text is not constantly rewritten;
5. best-effort render the offline page before a planned host disconnect.

The latest source adds a compact fallback without restoring the 1.3 KiB AVR
renderer. After the host finds and initializes the backpack, it can preload
hidden HD44780 DDRAM with exactly `PC offline      ` and
`Connect USB toPC`. Successful generic writes to `0x27` or `0x3F` let the MCU
remember the backpack address. After five seconds without host traffic, the
MCU releases a captured panel and sends sixteen display-shift commands to
reveal the preloaded page. A returning heartbeat lets the host restore the
normal home position. The missing space in `Connect USB toPC` is the explicit
16-column compaction of the requested 17-character phrase.

This fallback is **staged, not physically accepted**. It works only if the PC
successfully initialized and preloaded that exact LCD first; otherwise an
unknown hidden area is shifted into view, or no write occurs when the MCU has
not learned an address. The logical 32-byte `FrontPanelGet` LCD mirror retains
the last text on abrupt loss; the compact MCU path shifts physical DDRAM and
does not synthesize new rich LCD text.

Host integration now separates `lcd_service_enabled` (default true) from
`mirror_prompt_to_lcd` (default false). Captured hosted menus are routed to the
physical presenter, and runtime detach calls `PrepareDisconnect` best-effort
with a 350 ms bound before removing the serial session. Those are implemented
source paths, not hardware acceptance: reconnect/preload/detach tests are still
being finalized, and the 2x16 display has not been observed through the full
loss/recovery sequence. The TM1637 and all local menu behavior remain
standalone.

## Other staged board features

The local menu exposes only the most useful commissioning functions. These
additional firmware features are controlled or observed through the opcode
protocol and host:

- 115200-baud COBS/CRC native UART protocol; Firmata and debug-string traffic
  are absent;
- a 384-byte UART0 Urboot/urclock region is preserved above the 32,384-byte
  application limit. Bootloader and application opcodes are different modes;
  the PC host must reset/hand off before programming, backup, or restore;
- compact HELLO identity using source/build hash and packed date/time rather
  than a manually versioned firmware number;
- periodic or on-demand telemetry with uptime, voltage, bus voltage, current,
  power, two temperatures, inputs, relay/PWM/menu state, protocol errors,
  reset cause, and persistent reset count;
- instant key, door, BT Audio, relay, PWM-channel, RF, learning, macro, and
  reset events;
- exact TM1637 segment/front-panel snapshot and host-injected key gestures;
- direct PWM, relay, side motion, RGB status, WS2811/WS2812, buzzer, RF send,
  display, menu, reset, and generic I2C commands;
- safe reset sequence that stops the buzzer, RF momentary action, motion,
  relays, and user PWM outputs while playing the Reset RGB cue;
- boot melody and boot/status RGB indication; door, BT Audio, RF, navigation,
  save/discard, warning/hot, fault, and reset RGB cues;
- door and relay audio enable flags plus global Silent mode in EEPROM;
- two DS18B20 ROM identities, role swap setting, asynchronous 11-bit
  conversion, and temperature smoothing; OneWire work pauses during RF learn;
- INA219 hardware averaging and faster 100 ms reads while the enclosure is
  open (500 ms while closed);
- reset watchdog and startup SDA/SCL recovery;
- a 128-byte MCU-timed macro byte ring that dispatches ordinary opcodes and
  reports accepted/executed bytes and steps, fill, underruns, dispatch
  failures, and the MCU start timestamp. Timed reserved-sequence responses
  provide exact board-clock execution deltas.

### Staged macro-queue behavior

Macro schema 2 uses a 128-byte circular byte array with 127 usable bytes.
BEGIN carries schema, macro ID, cancel flags, and total step count. Variable
records contain a microsecond due-time offset, one
ordinary opcode, payload length, and that opcode's native payload. APPEND
frames carry a stream-byte offset and complete-step count and may split the
byte stream across USB packets. RUN starts the local clock; QUERY returns
status. The MCU executes all records that are due on its own `micros()` clock,
so USB/network arrival jitter does not become relay or PWM timing jitter once
a step is buffered.

The board reports accepted/executed steps and accepted bytes, queue fill (free
space is `127 - fill`), an 8-bit underrun count, an 8-bit dispatch-error count,
the RUN `startedAtUs` MCU timestamp, and total steps. Planned duration,
late-step aggregate, maximum-error aggregate, and rich name/category metadata
remain host-owned instead of being duplicated in AVR RAM. Each dispatched
ordinary opcode uses reserved sequence `0xFE`. Its ACK or ErrorResponse carries
a trailing `deviceMicros` value captured by the AVR. The host can therefore
compute an exact signed board-clock error for each step, including `micros()`
wraparound:

```text
signedErrorUs = executionDeviceMicros - (startedAtUs + dueOffsetUs)
```

This does not depend on USB/network arrival time. Lifecycle Events are also
MCU-timestamped, so the host can derive completion/cancellation elapsed time
and aggregate late/max-error statistics without a large per-step status event.

The queue reuses the ordinary opcode dispatcher and its relay/PWM validation
instead of carrying a second macro-only peripheral policy table. Macro control
opcodes cannot invoke themselves recursively. The two compact error counters
wrap after 255 and do not preserve pathological higher totals.

Normal completion preserves the final outputs. Cancel defaults to a safe stop
of all relays and PWM 0-10; a deliberate Keep Outputs flag is available for a
host-requested cancel. If an active playback loses host traffic for more than
five seconds, firmware cancels and safe-stops regardless. Automatic macro
name/elapsed/step presentation on the physical displays is not yet proven;
the host can include ordinary display steps, and the requested hosted Macro
menu remains part of final integration acceptance.

The latest compiled fitting, non-live checkpoint is `0E5FE035`,
32,382/32,384
application bytes with 2 bytes free and 1,543/2,048 static-SRAM bytes. It
supersedes fitting checkpoint `6DCF6A68` (32,372 bytes) and followed a
106-byte-over diagnostic that was correctly rejected. Current source is newer
than that manifest and may move again before the final build. Host workflow,
physical display presentation, exact timing acceptance, and live behavior
still require the final COM18
upload/read-back and harmless macro test. Do not treat those items as proven
merely because this checkpoint fits.

## EEPROM settings owned by the MCU

The board, not the PC config file, owns these settings in EEPROM:

- Silent mode;
- Off/Auto/On illumination plus On and Off brightness;
- TM1637 brightness and status RGB brightness/color index;
- PWM boot mode and eight stored user-PWM values;
- telemetry period (0 disables periodic streaming; otherwise at least 100 ms);
- default menu page and Save Last Page option;
- voltage/current decimal count, 0-2;
- tLED/tBT role swap;
- motion door policy and 1/100 ms break-before-direction choice;
- door and relay audio enable flags;
- 20 learned RF records/mappings;
- reset-count journal.

When Save Last Page is enabled, navigating to a root page updates the default
page and a door close is a forced commit point. When it is disabled, reset and
door-close-without-an-active-edit return to the explicitly configured default.

The PC host persists only PC-side preferences, names, colors, automations,
macros, hotkeys, integration endpoints, and device-selection policy. It may
query or write the MCU settings, but it must not confuse its config file with
the EEPROM source of truth.
