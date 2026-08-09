<div align="center"><a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Board Features, Front Panel, and Menus

This is the starter guide to the ATmega328P-owned controls and state. The
current firmware exposes 14 stable leaf pages beneath four fixed categories;
stable page 0 is `door`. Visibility and ordering are EEPROM-backed while each
page's category is compiled into the firmware. Exact flash/SRAM figures and
build identity belong to the firmware manifest produced from the source being
deployed, while physical acceptance belongs to that same image after upload
and readback.

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

Changing direction while enabled is sequenced as disable, the configured
1..255 ms break, direction change, then enable. The two
direction relays are separated by at least 5 ms. Reset and All Off disable the
enable relays before changing directions or other outputs.

## Peripheral initialization profile

This section is the canonical hardware-configuration reference for the current
source. Exact build identity and resource use come from the candidate's
firmware manifest; physical behavior must be accepted after that same image is
uploaded and read back. Values marked EEPROM default apply after an erased or
invalid settings record; a valid board-owned EEPROM record overrides them.

The production target is an ATmega328P at 16 MHz with a 2 s watchdog. UART0 is
started first so the host can receive an early `HELLO`; relay outputs are made
safe before peripherals that can take longer to probe. The relevant startup
order is UART/watchdog, shift registers and relays, system inputs, buzzer,
addressable LEDs, EEPROM/display, recovered I2C plus PWM/INA219, temperatures,
433 MHz, then the nonblocking boot melody.

| Peripheral | Wiring/address | Active initialization | Startup state |
|---|---|---|---|
| INA219 | I2C `0x40`, A4/A5 | 32 V, gain /8, 64-sample bus and 128-sample shunt averaging, continuous conversion | Probed after I2C recovery; unavailable if probe/configuration fails |
| 16-channel PWM (PCA9685 hardware) | I2C `0x41`, A4/A5 | 1 kHz, auto-increment, active-high logical mapping | Every channel is forced off with one retry before only explicitly enabled stored outputs are restored |
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
giving current the strongest hardware filtering. Completed INA219 samples
also pass through a software EMA without another history buffer: voltage and
bus voltage use 1/4 of the new delta with a one-unit deadband, while current
and power use 1/8 with a two-unit deadband. The first valid reading is applied
immediately.

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
PWM unavailable. It then applies direct stored values only for the eight
EEPROM-backed user channels when output-persistence bit 2 is enabled. Channels
11-15 remain owned by enclosure illumination, Power/On, and status RGB.
Changed values are cached, preventing redundant I2C writes and visible jitter.

Safe changes:

- Set each channel directly and use a named host macro or automation for a
  repeatable commissioning sweep. Do not encode autonomous test behavior in
  board settings.
- Frequency can be changed within 24-1526 Hz, but it affects MOSFET lighting,
  illumination, Power/On, and RGB together. Revalidate flicker, switching
  losses, and attached-module input limits.
- Do not drive channels 11-15 from a generic pattern while their system owner
  is active.
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
Learning suppresses duplicate repeats and stores at most 20 learned entries in
board EEPROM. The default **Learn** session is indefinite and accepts multiple
codes. The bounded **timer** session exposes its configured/remaining duration;
`single` and `one-shot` remain accepted names for that same timer mode.

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
and the pinned dependency record in [Project Acceptance](Project-Checklist.md).

### Cooperative I2C and HOST LCD profile

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
- Re-enabling the AVR LCD renderer restores standalone rich LCD text but spends
  scarce flash and SRAM. Treat it as an explicit
  [feature tradeoff](Memory-and-Feature-Tradeoffs.md#best-offload-candidates) and
  remeasure it with the current locked toolchain before changing the release
  profile.

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
enabled is sequenced as Enable off, EEPROM-selectable 1..255 ms break (1 ms
factory), direction relay change, then Enable on, with no hidden settling delay. The A
and B direction relays cannot change in the same service pass and are separated
by at least 5 ms. Motion door policy is an EEPROM setting: Always (factory),
Closed Only, Open Only, or Never. R5-R8 are independent general outputs.

Enclosure illumination is PWM channel 11. Auto mode targets the configured On
brightness while the debounced door is open and Off brightness while closed;
factory values are 128 and 0. It moves by four 8-bit levels every 20 ms and
accounts for delayed service without wrapping, preventing the previously seen
fade jitter. Off and On modes select the same stored endpoints directly.

The Power/On indicator is PWM channel 12 and turns on when status control
starts. Status RGB is channels 13-15. EEPROM factory brightness is 128.
Animated levels change by four every 20 ms, and informational transitions ease
toward their new color instead of inserting a black or unrelated frame.

Production full-peripheral builds omit the 1,918-byte local status-effect
engine to remain inside the ATmega328P application limit. They still drive the
critical PCA9685 Power/On signal directly on channel 12; the connected Go host
owns RGB channels 13-15 through the ordinary RGB opcode. The table below
describes the optional status-engine feature profile, not offline production
behavior. Local boot/fault animations are therefore unavailable without the
host. A tiny fixed critical offline indicator remains tracked in
[#21](https://github.com/atomicdeploy/PCController/issues/21); it must fit the
proven stack/flash margin before being enabled.

| Priority/state | RGB behavior |
|---|---|
| Programming latch | Power indicator and RGB remain off until the host completes verify/reconnect/restore |
| Fault, host offline, or Running with door open | Immediate hard red flash |
| HOT | Orange/red breathing plus the configured audio warning |
| RF learning | Violet breathing; received RF activity overlays a smooth violet cue |
| Host status override | Host-supplied color/effect |
| Running with door closed | Orange/yellow |
| BT Audio connected (solid indicator) | Blue |
| BT Audio waiting/not connected (blinking indicator) | Calm blue breathing |
| BT Audio powered off | Green/red breathing |

Door open/closed, menu, BT Audio, radio, save, discard, and Reboot are bounded
cues layered over the base state. Informational cues restore smoothly; critical
warnings intentionally do not fade slowly. Individual status colors remain a
host/TUI configuration acceptance item and must not be described as already
persisted in EEPROM until that final settings layout is implemented.

Safe changes:

- Melody notes/durations, cue enable flags, display/status brightness, Ready
  palette choice, illumination mode/endpoints, motion policy, and 1..255 ms
  break are safe EEPROM/host settings.
- Keep the configured break and 5 ms cross-side interlock unless relay
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
for the installed WS2811 strip; setting it to 1 builds GRB order for
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

The MiniCore target is the `fqbn` field in the canonical
[toolchain profile](../Tools/Controller/toolchain-profile.json). Build,
programming, host defaults, and documentation validation derive from that one
authored value; the exact toolchain lock retains only its generated copy.

UART0 is always enabled at 115200 baud and is the primary application link,
not a debug-text console. Application frames use zero-delimited COBS, magic
`0xA5`, an advisory envelope-revision byte, CRC-8, sequence IDs, opcodes, and a maximum 48-byte
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
[UartProtocol.cpp](../Project/UartProtocol.cpp), the generated
`firmware-manifest.json`, and the host serial mode in
[session.go](../Tools/Controller/internal/link/session.go).

## Key gestures

The key engine polls the front register every 5 ms and debounces physical edges
for 20 ms. The first debounced `Down` runs the primary action within a guarded
25 ms worst-case input budget; it never waits for release, click
classification, audio feedback, a display update, UART, or the 300 ms
double-click window. PC-injected lifecycle input follows the same rule, while
the stateless `MENU_ACTION` command and a learned RF mapping execute in the
same service pass in which they are received. An accepted RF key frame is also
reported as `Down`, not as the later physical `Click` classification.

| Gesture | Timing and effect |
|---|---|
| Down | Primary action at the 20 ms physical debounce deadline, or immediately for injected input |
| Short press | Later Click classification after the 300 ms double-click window; telemetry only |
| Double click | Second release within 300 ms; double-click K1 returns to the configured default page without delaying either Down action |
| Hold start | Classification 600 ms after press; no second primary action and no Click is emitted |
| Hold repeat | Repeats the action every 150 ms, then every 60 ms after 1.8 s |
| Hold release | Emitted when a held key is released |
| Up | Stops momentary motion/push-relay output and is emitted for telemetry/mirroring |

Motion and Push-relay control use true down/up behavior rather than repeated
menu actions, so their output starts immediately after debounce and stops on
release. The KEY identification page reports one classified press and
suppresses ordinary hold repeat.

This is a non-negotiable responsiveness invariant. Future work must not move
the initial action back to `Click`/`HoldStart`, debounce it twice, wait for a
double-click decision, perform EEPROM or network work before dispatch, or let
buzzer/display/overlay feedback block input service. Hardware-free tests cap
the complete scan-plus-debounce path at 25 ms and assert that only `Down` and
`HoldRepeat` drive primary actions. Physical acceptance must additionally
verify all four panel keys, PC virtual buttons, and learned RF keys against the
exact flashed image, and record a live HELLO build hash that matches the tested
artifact. A source-only fix on a board running an older hash does not close a
latency regression.

## Local menu directory

The current hierarchy has four category parents. On an ordinary leaf, K1/K2
move through visible pages in that category, K3 goes Back, and K4 enters the
page's editor/action. `KEY` owns all four keys for identification and `rELY`
owns K3 as immediate All Off. On a category parent, K1/K2 move among non-empty
categories, K4 enters the first visible child in EEPROM order, and K3 returns
to the previously active leaf. Navigation wraps. A page/category label is
shown for about 650 ms before the live value.

Category parents are compact views represented by an existing leaf label, not
additional stable page IDs:

| Category | Parent label | Stable leaf pages |
|---|---|---|
| Monitoring | `door` | 0 `door`, 1 `VOLT`, 2 `CURR`, 3 `tLED`, 4 `tBT` |
| Environment | `LItE` | 5 `LItE`, 6 `bEEP` |
| Outputs | `PWM` | 7 `PWM`, 8 `rELY`, 10 `uPWM`, 11 `r5-8`, 12 `MOVE` |
| Inputs/RF | `KEY` | 9 `KEY`, 13 `LErn` |

| ID | Label | What the display shows | K3/K4 behavior |
|---:|---|---|---|
| 0 | `door` | Door `OPEN` or `CLSd` | K3 Back; K4 currently gives the common read-only error cue |
| 1 | `VOLT` | INA219 supply voltage using 0-2 configured decimals | K3 Back; K4 read-only error cue |
| 2 | `CURR` | INA219 current using 0-2 configured decimals | K3 Back; K4 read-only error cue |
| 3 | `tLED` | Enclosure-light sensor as `LxxC` | K3 Back; K4 read-only error cue |
| 4 | `tBT` | BT Audio sensor as `bxxC` | K3 Back; K4 read-only error cue |
| 5 | `LItE` | Illumination `oFF`, `Auto`, or `on` | K3 Back; K4 opens Illumination editor |
| 6 | `bEEP` | `Mute` or `bEEP` | K3 Back; K4 opens Board Settings editor |
| 7 | `PWM` | Alternates channel (`P-00`..`P-15`) and current 0-4095 value | K3 Back; K4 opens all-channel commissioning editor |
| 8 | `rELY` | Alternates selected R1-R8 and Off/On | K3 immediately turns all relays off; K4 first turns all relays off, then opens commissioning |
| 9 | `KEY` | `KEY`, then the identified number for about 900 ms | K1-K4 identify 1-4; double-click K1 returns to the configured default page |
| 10 | `uPWM` | Alternates user channel 1-8 and its stored 8-bit value | K3 Back; K4 opens persistent user-PWM editor |
| 11 | `r5-8` | Active R5-R8 mask as 0-15 | K3 Back; K4 opens general-relay control |
| 12 | `MOVE` | Door `OPEN` or `CLSd` | K3 Back; K4 enters motion if the configured door policy permits it |
| 13 | `LErn` | Learned RF count, or unavailable dashes | K3 Back; K4 starts the default indefinite, multi-code learning session |

The board-authoritative `MenuList` opcode returns these 14 dense IDs,
program-mode IDs, and four-character labels in pages. Category membership is
the fixed mapping above and is not part of that six-byte entry. The retired
`bt` page was the redundant BT Audio **connection-state** page and remains
removed: BT input sensing, telemetry/events, automations, host monitoring, and
RGB status convey that state. The distinct `tBT` page above is intentionally
retained because it displays the BT-module temperature probe.

The current `door` renderer is the door-status home page. It does not
automatically replace the four digits with every relay/RF/BT/macro event. The
host can inject temporary segment text and events arrive over UART; automatic
local event overlays remain an explicit capability gap.

The TM1637 service itself runs every 20 ms and writes only changed segments.
INA219 conversions are hardware-filtered with 64x bus and 128x shunt/current
averaging; the firmware reads them every 100 ms while the enclosure is open
and every 500 ms while closed. Voltage/bus voltage then use a 1/4 software EMA;
current/power use 1/8, preserving fast display service while damping samples.
Temperatures use asynchronous 11-bit conversions, 450/1,000 ms open/closed
request periods, and a 50/50 EMA after the first valid sample; HOT readings are
not delayed by that EMA.

### Program state-machine modes

Telemetry and `FrontPanelGet` expose the active `ProgramMode`, not just the
active leaf page. This is the restored mode manager used to keep menu transitions and
modal editors deterministic:

| Mode ID(s) | State(s) | Purpose |
|---:|---|---|
| 0 | Boot | Shows `BOOT`, initializes hardware, then enters the EEPROM default page |
| 1-14 | Door through RF | One mode for each stable leaf page ID 0-13; mode ID is page ID + 1 |
| 15-17 | Illumination Mode/On/Off Edit | Three-field illumination transaction |
| 18 | Sound/Board Settings Edit | Eight-item board-settings submenu |
| 19-20 | PWM Channel/Value Edit | Direct per-channel `0..4095` control |
| 21-22 | Relay Channel/Value Edit | Live R1-R8 commissioning |
| 23-24 | User PWM Channel/Value Edit | EEPROM PWM 0-7 transaction |
| 25-27 | User Relay Channel/Behavior/Control | R5-R8 Toggle/Push control |
| 28 | Motion Control | Held Side A/B motion keys and combined-key exit |
| 29 | Save Prompt | Alternates `SAVE`/`diSC` until the user confirms |
| 30 | Flash Message | About 900 ms of flashing saved/discarded feedback |
| 31 | RF Learning | Timed, multi-code, or indefinite learning lifecycle |
| 32 | Fault | Reserved `Err`/audio/RGB terminal state; no current source path enters it automatically |
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

Enter from `bEEP` with K4. This modal sequence is the closest thing the current
local UI has to a settings submenu:

| Item | Label | Values | K3/K4 |
|---:|---|---|---|
| 1 | `bEEP` | Mute or Beep | K3 Mute, K4 Beep |
| 2 | `diSP` | TM1637 door-open brightness 0-7 | Decrease/increase with rollover |
| 3 | `dCLS` | TM1637 door-closed brightness 0-7; factory 0/off | Decrease/increase with rollover |
| 4 | `StBr` | Status RGB brightness 0-255 | Nominal step 16 with rollover |
| 5 | `V-dP` | Voltage decimals 0-2 | Decrease/increase with rollover |
| 6 | `A-dP` | Current decimals 0-2 | Decrease/increase with rollover |
| 7 | `SAFE` | Motion policy: 0 Always, 1 Closed only, 2 Open only, 3 Never | Decrease/increase with rollover |

Each field label is shown for about 650 ms and its value then blinks at about
300 ms. Sound, both display brightness targets, RGB brightness, and the motion
gate preview during editing. Selecting a policy that denies the current door
state immediately revokes motion through the ordinary fail-safe relay path.
Save persists the compact two-bit policy; Discard restores the exact locally
editable snapshot and reapplies the prior gate. Ready and event colors are
host-owned persistent status profiles rather than a fixed local color index.

The reed input selects the two EEPROM-backed TM1637 targets. The display walks
one intensity step every 70 ms toward the open or closed target without slowing
the 20 ms content refresh. Brightness 0 sends the TM1637 display-off command;
factory values are 5 while open and 0 while closed.

### Direct PWM control

Enter from `PWM` with K4:

1. `P-Ch`: select channel 0-15.
2. `P-u`: set its logical value 0-4095 in 256-count steps with rollover.
3. Finish the live transaction and return to the page.

There is no global state or autonomous sweep. Values are applied directly;
repeatable patterns belong in host macros or automations. The enclosure,
power, and status controllers may subsequently update their owned channels
11-15 during normal service. This live editor does not define boot behavior;
use the EEPROM user-output editor for restorable channels 0-7.

### Relay commissioning

On the `rELY` leaf, K3 immediately requests All Off. K4 first requests All Off,
then opens:

1. `r-Ch`: select R1-R8.
2. `r-on`: K3 Off, K4 On.

K1 returns to channel selection and K2 returns to the `rELY` leaf. This editor
changes live outputs and has no EEPROM Save prompt. For R1/R3, Off/On means
Forward/Reverse direction request; for R2/R4 it means Disable/Enable. R5-R8
are direct general outputs. The motion door policy is still enforced for
activating R1-R4.

### Persistent user PWM

Enter from `uPWM` with K4:

1. `uP-C`: select channel 1-8.
2. `uP-u`: edit its 8-bit value, 0-255, in nominal 16-count steps.
3. Save/Discard.

Entering applies all eight stored values directly to PWM 0-7.
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
the default page. The locally and host-configurable policy is Always, Closed
only, Open only, or Never; the erased/factory default is Always. Edit it as
item 8 (`SAFE`) in Board Settings. Host/API motion that is not running through
the local MOVE page follows the same selected policy.

### Save and discard

Illumination, Board Settings, and user-PWM editors snapshot their settings on
entry. At the confirmation display:

- K2 or K4 saves to CRC-checked EEPROM.
- K1 or K3 discards and restores the snapshot.
- A default-page double-click is accepted only on an ordinary leaf, so it
  cannot bypass an active editor or its Save/Discard decision.
- `SAVE` or `diSC` flashes for about 900 ms.
- Save uses a rising audio/RGB cue; Discard uses an error/descending cue.
- Silent mode mutes the audio but not the visual cue.

### 433 MHz learning and mappings

K4 on `LErn` starts the default indefinite, multi-code learning session.
`LErn` remains on the display and each distinct received code can be stored
until the host cancels, storage fills, or another lifecycle command ends the
session. The timer/single/one-shot mode is host-started with an explicit 1-255
second timeout; its display alternates total (`tNNN`) and remaining (`rNNN`)
seconds. Start, progress, timeout/end, cancel, full storage, and learned-code
events are distinct on the UART.

On the idle RF leaf K3 is hierarchy Back. Once learning mode is active there
is no dedicated physical Cancel branch, so cancellation is a host operation in
this candidate. K3 on the idle RF leaf does **not** clear all records.

EEPROM holds 20 records. Each record stores ID, code, bit count,
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
| Capacity | 14 stable leaves, four fixed categories, and modal editors | Up to eight host nodes/overrides, with 32 items per host node |
| Nesting | Four compiled categories plus the fixed editors above | Read-only, text, number, bool, select, submenu, and action items |
| Physical keys | Handled by AVR | Forwarded as events to host while captured |
| TM1637/LCD content | Retained by AVR | Host pushes an exact shared front-panel representation |
| Persistence | Board EEPROM | Host configuration/data files |

### Current PC-hosted menu inventory

The host's built-in configuration currently defines six hosted menus. These
are not additional AVR page IDs and do not consume board EEPROM:

| Hosted menu | Current items | Availability |
|---|---|---|
| `host` / `HOST` | Host/device status, date/time, host IP, API/link status, Macro Library, PC Settings, and System Actions submenus | Implemented in host configuration |
| `pc-settings` / `CFG` | Application title, poll interval, PC LCD service, LCD prompt mirroring, DTR reset-on-reconnect | Implemented; writes the PC configuration, not MCU EEPROM |
| `system-actions` / `SYS` | Monitor brightness, lock, suspend, hibernate, restart, and shutdown | All are guarded. Default policy allows brightness plus confirmed lock/sleep/hibernate; restart/shutdown need an explicit allow-list change. |
| `macro-library` / `MACR` | File-watched, ID-sorted macro selector, selected definition, guarded play, Recording and Playback submenus | Implemented through the shared MacroRunner |
| `macro-recording` / `REC` | Recording status, start with an automatically unique panel name, save, and guarded discard | MCU acknowledgement timestamps remain authoritative |
| `macro-playback` / `RUN` | Live step/elapsed status, safe cancel, and guarded cancel-while-keeping-outputs | Uses the same commands as TUI, CLI, IPC, and WebUI |

An RF organizer, richer MCU-settings page, and other PC/OS panels remain
possible migration targets. Custom definitions can add items within the
eight-node volatile directory limit, but a configured definition is not proof
that its read/write/action callback is implemented.

### Current physical-hosted-menu boundary

The six definitions above are usable on the current AVR through the fallback
host-push path. `DisplayText` target 3 captures the panel and carries one exact
four-character/32-character representation; physical key events go to the
host; target 4 releases capture. Definition changes and selection changes make
the host push a new representation, and five seconds without host traffic
releases the panel.

The richer board-pull protocol is **not implemented in this AVR image**.
Host and Virtual Board source reserve `HOST_MENU_DIRECTORY`/`CONTENT`/`STATE`
operations (`0x42..0x44`, `0x9A..0x9B`), but current firmware does not declare
those opcodes or a capability for them; the host's semantic support probe is
deliberately false. Consequently the physical AVR does not currently:

- retain the eight `{id,parent,flags}` descriptors in RAM;
- request a selected node's content from the host;
- track directory generation/revision and loading/ready/failed phases; or
- display `----`, retry a failed fetch, then report an unable-to-open state.

Those are genuine remaining firmware features, not documentation-only work.
The fallback still provides synchronized physical/remote interaction, but it
is host-pushed rather than board-pulled.

The `lcd_service_enabled` PC setting defaults to true; prompt mirroring remains
independently false. New/default configurations get both `CFG` items. Existing
user-customized `host_menus` arrays are not silently rewritten, so users may
add the service item explicitly if they want it on an older custom panel.

Local `door`, measurements, lighting, key identification, user relays, motion,
and RF learning remain available without a PC. The host adds richer labels,
search, graphs, exact numeric controls, and automation without replacing the
firmware's offline safety behavior. Future size tradeoffs must follow
[Firmware Size and Feature Tradeoffs](Memory-and-Feature-Tradeoffs.md) and
measure the complete image rather than relying on per-page estimates.

The host opens a captured session with `DisplayText` target 3 and releases it
with target 4. While captured, the board does not execute local key actions;
it sends the key gestures to the host. If host traffic disappears for more
than five seconds, the board releases capture and returns to its configured
default page. Holding K4 on `door` can request the configured hosted menu when
the host enables that gesture.

Typical commands are:

```text
host-menu list
host-menu open host
host-menu open macro-library
host-menu status
host-menu key K2 press
host-menu key K4 press
host-menu close
```

See [Hosted Front-Panel Menus](../Tools/Controller/docs/Hosted-Front-Panel-Menus.md)
for the host schema and command/API equivalents.

### Host-owned operational state

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

## Host-owned LCD presentation

The full AVR HD44780/PCF8574 renderer is disabled in `ProjectConfig.h` to make
room for the timed macro queue. The generic cooperative I2C transaction opcode
remains enabled, and firmware-owned PWM/INA polling pauses during a bounded
host lease.

`DisplayText` retains 32 characters of desired LCD text so the TUI,
front-panel snapshot, bridge, and hosted menus share one logical state. That
alone does not make characters appear on the physical LCD. The PC host:

1. scan the expected PCF8574 addresses (normally `0x27` and `0x3F`);
2. initialize the HD44780 in 4-bit, two-line mode;
3. translate each 16-character row into PCF8574 nibble/Enable writes through
   the bounded generic I2C opcode;
4. cache rows so unchanged text is not constantly rewritten;
5. best-effort render the offline page before a planned host disconnect.

Firmware includes a compact fallback without restoring the full AVR
renderer. After the host finds and initializes the backpack, it can preload
hidden HD44780 DDRAM with exactly `PC offline      ` and
`Connect USB toPC`. Successful generic writes to `0x27` or `0x3F` let the MCU
remember the backpack address. After five seconds without host traffic, the
MCU releases a captured panel and sends sixteen display-shift commands to
reveal the preloaded page. A returning heartbeat lets the host restore the
normal home position. The missing space in `Connect USB toPC` is the explicit
16-column compaction of the requested 17-character phrase.

This fallback works only if the PC
successfully initialized and preloaded that exact LCD first; otherwise an
unknown hidden area is shifted into view, or no write occurs when the MCU has
not learned an address. The logical 32-byte `FrontPanelGet` LCD mirror retains
the last text on abrupt loss; the compact MCU path shifts physical DDRAM and
does not synthesize new rich LCD text.

Host integration separates `lcd_service_enabled` (default true) from
`mirror_prompt_to_lcd` (default false). Captured hosted menus are routed to the
physical presenter, and runtime detach calls `PrepareDisconnect` best-effort
with a 350 ms bound before removing the serial session. Source tests cover the
bounded presenter contracts; the actual 2×16 backpack still requires the full
loss/recovery observation in Project Acceptance. The TM1637 and all local menu
behavior remain standalone.

## Other board features

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
- host connect/reconnect surfaces query that HELLO identity and must discard it
  on disconnect. A tracked local firmware-identity page will show the complete
  hash on LCD and nonblockingly alternate its first/last four characters on
  TM1637; this board-owned page is not yet part of the current menu image and
  remains tracked in the
  [firmware-identity requirement](Requirements-Backlog.md);
- periodic or on-demand telemetry with uptime, voltage, bus voltage, current,
  power, two temperatures, inputs, relay/PWM/menu state, protocol errors,
  reset cause, and persistent reset count;
- instant key, door, BT Audio, relay, PWM-channel, RF, learning, macro, and
  reset events;
- exact TM1637 segment/front-panel snapshot and host-injected key gestures;
- direct PWM, relay, side motion, RGB status, WS2811/WS2812, buzzer, RF send,
  display, menu, reset, and generic I2C commands;
- graceful reboot sequence that stops the buzzer, RF momentary action, motion,
  relays, and user PWM outputs while playing the Reset RGB cue;
- boot melody plus direct PCA9685 Power/On indication; when the host is
  connected it supplies door, BT Audio, RF, navigation, save/discard,
  warning/hot, fault, and reset RGB cues through the native opcode;
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

### Macro-queue behavior

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
five seconds, firmware cancels and safe-stops regardless. The host mirrors the
macro ID/name, current step, elapsed time, planned duration, and terminal state
to TM1637 and the optional 2x16 LCD through a latest-only presentation queue.
That queue cannot delay the authoritative MCU execution or refill loop; a macro
may also include ordinary display steps.

Exact flash/SRAM use comes from the final candidate manifest. Host workflow,
physical display presentation, timing, and live behavior require
upload/readback plus a harmless physical macro test; a fitting compile does not
prove those behaviors.

## EEPROM settings owned by the MCU

The board, not the PC config file, owns these settings in EEPROM:

- Silent mode;
- Off/Auto/On illumination plus On and Off brightness;
- TM1637 door-open/door-closed brightness and status RGB brightness/color index;
- output-persistence policy, last R1-R8 restore mask, and eight stored user-PWM
  values;
- telemetry period (0 disables periodic streaming; otherwise at least 100 ms);
- default menu page and Save Last Page option;
- the 14-bit menu-visibility mask and seven-byte packed order for all 14 stable
  leaf IDs;
- voltage/current decimal count, 0-2;
- tLED/tBT role swap;
- motion door policy and exact 1..255 ms break-before-direction interval;
- door and relay audio enable flags;
- 20 learned RF records/mappings;
- reset-count journal.

When Save Last Page is enabled, navigating to a stable leaf page updates the default
page and a door close is a forced commit point. When it is disabled, reset and
door-close-without-an-active-edit return to the explicitly configured default.

The PC host persists only PC-side preferences, names, colors, automations,
macros, hotkeys, integration endpoints, and device-selection policy. It may
query or write the MCU settings, but it must not confuse its config file with
the EEPROM source of truth.

The current firmware has no on-board EEPROM migration handler. Menu validation
accepts only the dense IDs 0-13 and a 14-bit visibility mask; an older record
that is not already semantically valid is rejected. The Go host provisions its
canonical settings and status-profile defaults through current opcodes, while
explicit development reinitialization programs and independently reads back
the complete generated EEPROM image. The physical settings record and UART
layout both use exactly seven menu-order bytes for the 14 packed IDs; there is
no spare order byte.

The current logical EEPROM map is:

| Range | Bytes | Owner |
|---:|---:|---|
| 0-31 | 32 | Unallocated |
| 32-63 | 32 | Packed settings plus checksum |
| 64-307 | 244 | RF header plus 20 learned records |
| 308-319 | 12 | Unallocated |
| 320-703 | 384 | 64-slot reset-count journal |
| 704-950 | 247 | Nineteen status-effect condition descriptors plus CRCs |
| 951-1023 | 73 | Unallocated |

That leaves 117 logically unallocated bytes. The generated safe-default EEPROM
image still covers all 1,024 bytes so a programming/restore operation is
deterministic; that does not make the erased regions owned records.

The following requested behavior is **not** EEPROM-backed in this candidate:

| Area | What exists | What is still missing |
|---|---|---|
| Configurable buzzer cues | Global Silent plus door/relay enable bits; door and relay tones are fixed in flash | Persistent cue IDs or note/frequency/duration descriptors for door-open, door-close, relay-on, and relay-off |
| Board automation | Twenty RF records map codes directly to Key, Menu, Relay, Side, or PWM actions; host automations can consume events | A generic board rule table for door, BT Audio, relay, host-loss, temperature, RF transmit, macro start, or other opcode actions |

Structured host-menu pull is also not implemented by the AVR: the current
physical-board path is the documented display-capture fallback. The AVR does
not retain the proposed eight-node RAM directory, request content by menu ID,
track a host-menu generation, or render loading/retry/failure states. Those are
wire/RAM/flash additions rather than EEPROM settings and must not be inferred
from the host/Virtual Board implementation.
