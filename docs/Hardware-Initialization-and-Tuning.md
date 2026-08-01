# Hardware Initialization and Tuning

This is the canonical, source-backed record of how the ControllerBoardMini
firmware initializes its peripherals. It records the exact compiled settings,
why they were selected, and the useful alternatives. A source setting is not
the same as a completed electrical test; the final verification report must
identify the firmware hash used on the board and list the observed devices and
measurements separately.

The source paths cited below are authoritative for the next build. A valid MCU
EEPROM record can override settings explicitly identified as persistent.

## Startup sequence and safety state

The ATmega328P starts a 2 s watchdog, opens the native COBS/opcode UART, sends
an early HELLO, then initializes shift I/O, relays, monitored inputs, buzzer,
addressable LEDs, EEPROM settings, TM1637, I2C, PWM, INA219, DS18B20, RF, and
the boot melody. Relays are forced off before their output-enable pin is driven,
and all 16 PWM channels are explicitly written off before saved PWM, enclosure,
power-indicator, and status-RGB state is applied.

| Area | Compiled initialization | Why this is the current choice | Useful alternatives |
|---|---|---|---|
| CPU/watchdog | MiniCore ATmega328P at 16 MHz; 2 s watchdog | Bounds a genuinely stalled peripheral path while leaving startup headroom | Change only with a complete timing/build/upload verification because I2C, UART, TM1637, WS2811, and tone timing depend on it |
| Native UART | 115200 baud, normal AVR 8N1; COBS frames with opcode/CRC; always enabled | Responsive control and telemetry without using UART as a debug console | Lower baud only for a demonstrated electrical integrity problem; DTR reset is a separate host setting and is off by default |
| I2C/TWI | Compact master, 100 kHz, prescaler 1, `TWBR=72`, 25,000 us wait timeout with peripheral reset, repeated START, 16-byte host read/write cap | Stable INA/PWM traffic and bounded faults with much lower flash/SRAM than generic Wire | 400 kHz is possible only after validating every device, cable, pull-up, generic host transfer, and LCD operation |
| INA219 | Address `0x40`; config `0x3F7F`; calibration `4096`; continuous shunt+bus conversion | Correct 32 V range for a 12 V system and strong hardware averaging, especially for current | See the detailed INA219 section before changing range, averaging, or shunt calibration |
| PWM/PCA9685 | Address `0x41`; requested 1000 Hz; prescale `5`; MODE2 `0x04`; active-high logical outputs | Separates it from INA219 and moves lighting PWM well above visibly flickery rates | Frequency 24..1526 Hz is supported; polarity is a compile-time electrical choice, not a display preference |
| DS18B20 pair | D10/CS 1-Wire bus; external 4.7 kOhm pull-up; externally powered; at most two family-`0x28` ROMs; 11-bit; asynchronous 375 ms conversion | Responsive temperatures without blocking UART/RF; bounded search survives a missing pull-up | 9/10-bit are faster and coarser; 12-bit gives 0.0625 C resolution but requires 750 ms |
| TM1637 | D13/SCK clock, D11/MOSI data, 3 us bit timing; 20 ms render service; cached four-segment writes; EEPROM brightness 0..7, factory 5 | Fast menu response while avoiding repeated writes of unchanged segments | Brightness and voltage/current decimal places are EEPROM settings |
| 433 MHz | RX D2/INT0 on CHANGE, TX D3/INT1 pin; rc-switch 2.6.4; 70% receive tolerance; TX defaults to protocol 1, 350 us pulse, 10 repeats | Broad compatibility with the observed low-cost remote family | Lower tolerance reduces false matches; protocol/pulse can be supplied per transmit command |
| Shift I/O | 74HC165 input and 74HC595 active-low output; 5 ms scan; first four input bits are keys; outputs start `0xFF`/off | Deterministic safe relay state and responsive keys | Electrical polarity constants must change only after a raw-input/output test |
| Buzzer | D9/PB1/OC1A; Timer1 CTC hardware toggle; no audio-rate ISR | Removes the former timer/interrupt jitter and leaves Timer0 timing intact | Do not combine with Servo or `analogWrite()` on D9/D10 |
| Addressable LEDs | D6/PD6, 11 pixels, 800 kHz, WS2811 BRG order, cleared at startup | Preserves the inherited strip without heap-heavy libraries | Set `PCCONTROLLER_USE_WS2812B=1` for a confirmed WS2812B/GRB strip |
| PC-owned LCD | Firmware HD44780 renderer disabled; host scans common PCF8574 addresses `0x27` and `0x3F` and uses generic I2C transfers | Saves AVR flash while retaining richer text whenever the host is connected | An MCU LCD renderer can be restored at a measurable flash cost |

## I2C bus initialization

The firmware uses [`Project/CompactI2c.cpp`](../Project/CompactI2c.cpp), not
MiniCore's `Wire` implementation.

- A4/SDA and A5/SCL are released with their weak internal pull-ups for startup
  recovery. Correct external I2C pull-ups are still required for reliable bus
  operation.
- A low SCL aborts initialization. If SDA is low while SCL is available, the
  firmware generates nine recovery clocks, 5 us low plus 5 us high, followed
  by a STOP, then requires both lines to be high.
- The master runs at 100 kHz at the fixed 16 MHz CPU clock. Every wait is
  bounded to 25 ms and resets the TWI peripheral on timeout.
- Register reads use a repeated START. Cooperative raw host transfers can read
  or write any 7-bit address, with at most 16 bytes in each direction and an
  optional lease of at most 10 seconds.

The compact master intentionally gives up generic Wire slave mode,
multi-instance support, and Wire's separate 32-byte TX/RX buffering. It does
not give up on-board INA/PWM ownership, repeated-START register reads, or the
cooperative arbitrary host I2C bridge.

Current expected addresses are:

| Device | Address | Ownership/conflict note |
|---|---:|---|
| INA219 | `0x40` | Fixed current/voltage monitor |
| PWM/PCA9685 | `0x41` | Hardware strap changed specifically to avoid the former `0x40` collision |
| PCF8574 LCD backpack | commonly `0x27` or `0x3F` | Host-scanned and host-rendered; no conflict with `0x40`/`0x41` |

## INA219 voltage/current monitor

[`Project/Ina219Sensor.cpp`](../Project/Ina219Sensor.cpp) writes calibration
`4096` and configuration `0x3F7F` after probing `0x40`. Calibration is written
again before every reading because compatible parts can clear it after reset.

Configuration `0x3F7F` decodes as:

| Field | Value | Result |
|---|---:|---|
| BRNG | `1` | 32 V bus range |
| PGA | `3` | gain `/8`, plus/minus 320 mV shunt range |
| BADC | `14` | 12-bit bus ADC with 64-sample averaging, about 34.05 ms |
| SADC | `15` | 12-bit shunt ADC with 128-sample averaging, about 68.10 ms |
| MODE | `7` | continuous shunt-and-bus conversion |

One full bus-plus-shunt cycle is therefore about 102.15 ms. The firmware polls
the completed registers every 100 ms while the door is open and every 500 ms
while closed. This keeps the open-enclosure display responsive but does not
pretend that polling faster than the ADC conversion produces a new sample.

Calibration `4096` assumes the board's common `0.1 ohm` shunt and selects a
`0.1 mA` current LSB. The code reports:

- bus voltage at 4 mV per raw bus count;
- supply voltage as bus voltage plus signed shunt voltage;
- current at 0.1 mA per current-register count, rounded to integer mA; and
- power at 2 mW per power-register count.

This is a deliberate smoothing/speed balance: current receives 128-sample
hardware averaging because it was the noisier value, while bus voltage uses
64 samples. The firmware does not add another INA EMA, so host graphing can
apply presentation-only smoothing without delaying MCU safety/event logic.

Before modifying INA settings:

- If the fitted shunt is not `0.1 ohm`, change the calibration and conversion
  scale together. Software filtering cannot correct a wrong shunt value.
- Reducing ADC averages makes controls respond sooner but increases displayed
  noise. Increasing them further is not available on INA219.
- A smaller PGA range can catch shunt over-range earlier, but must still cover
  the maximum load current.
- Compare a calibrated meter at the INA219 bus pins and across the shunt before
  adding a software voltage offset.

## PWM/PCA9685 outputs

The codebase calls this module **PWM**; PCA9685 is retained here only to identify
the chip. [`LocalLib/BoardPins.h`](../LocalLib/BoardPins.h) fixes its address at
`0x41`, while INA219 remains at `0x40`.

Initialization performs the following operations:

1. Probe `0x41` and write MODE1 `0x20` (auto-increment).
2. Request 1000 Hz. With the internal nominal 25 MHz oscillator, integer
   rounding selects prescale `5`, corresponding to about 1017.25 Hz nominal.
3. Enter sleep, write the prescaler, restore MODE1, wait 500 us, then issue
   RESTART plus auto-increment.
4. Write MODE2 `0x04`: non-inverted, totem-pole output, update on STOP. The
   alternative active-low build writes `0x05`.
5. Force all 16 channels off, retrying one failed all-off pass. Three
   consecutive write errors mark PWM unavailable rather than claiming success.
6. Apply EEPROM PWM mode/user values, enclosure illumination, the power signal,
   and status RGB.

Logical values are always `0..4095`, where 0 is electrically inactive and
4095 is fully active. This build has `PCCONTROLLER_PWM_ACTIVE_LOW=0`; zero uses
the PCA9685 FULL_OFF bit and 4095 uses FULL_ON.

| Channels | Function |
|---|---|
| 0..7 | External MOSFET lighting/user outputs |
| 8..10 | Additional general PWM user outputs |
| 11 | Enclosure illumination |
| 12 | Power/on signal light |
| 13, 14, 15 | Status RGB red, green, blue |

An erased/invalid EEPROM selects Auto test at boot. Auto test is limited to
channels 0..10, increments by 128 every 20 ms, reaches 4095, fades back to zero,
then advances to the next selected channel. A saved EEPROM mode overrides the
factory choice. Status/power/illumination channels are never included in this
identification sweep.

One kilohertz was chosen to remove visible lighting flicker while staying well
inside the PCA9685 range. A different frequency can be requested from 24 to
1526 Hz, but the actual value depends on the module oscillator tolerance. For
precision, measure the output frequency and add an oscillator calibration
constant rather than assuming every module is exactly 25 MHz.

## DS18B20 temperature sensors

The two sensors share D10, the board's `CS` pin. The data line requires an
external **4.7 kOhm pull-up to VCC**. The custom Dallas driver assumes the VDD
pins are externally powered; parasite power would additionally require a
strong pull-up during conversion and is not enabled.

At startup the driver:

- first requires an idle-high bus, so a missing pull-up or short-to-ground
  returns promptly instead of locking the MCU;
- caps ROM-search attempts at eight and retains at most two valid family
  `0x28` addresses whose ROM CRC passes;
- sorts the ROMs lexicographically for deterministic identity;
- writes 11-bit scratchpad resolution to each detected sensor; and
- starts conversion asynchronously, keeping UART and RF service available.

Eleven-bit conversion takes 375 ms and has 0.125 C resolution. New conversions
start every 450 ms while the door is open and every 1000 ms while it is closed.
Valid temperature samples use a 50/50 EMA. A first sample, a disconnected
sample, or a sample at/above the 50.00 C HOT threshold bypasses the EMA so a
fault or warning is not hidden by smoothing.

For the current harness, the lexicographically first ROM is mapped to
`Temperature BT` and the second to `Temperature LED`. The persistent
`swapTemperatureSensors` flag reverses that assignment if a probe is replaced
or the harness changes. The host reports each full 64-bit ROM so identity can
be confirmed electrically rather than inferred only from heat response.

Resolution alternatives are:

| Resolution | Step | Maximum conversion |
|---:|---:|---:|
| 9-bit | 0.5 C | 93.75 ms |
| 10-bit | 0.25 C | 187.5 ms |
| **11-bit** | **0.125 C** | **375 ms** |
| 12-bit | 0.0625 C | 750 ms |

## Displays and illumination

The TM1637 is a fixed-pin, allocation-free driver rather than the installed
generic TM1637 library. It services presentation every 20 ms but compares the
four encoded segment bytes with a cache and sends nothing when they have not
changed. This provides a 50 Hz decision rate without the old constant bus
refresh flicker. The low-level bit delay is 3 us, and the chip brightness range
is 0..7 with factory EEPROM value 5.

Voltage and current decimal places are independent persistent settings in the
range 0..2; erased EEPROM decodes both to two decimals. Measurement acquisition
rate and display render rate remain separate, so a fast display never invents
new sensor samples.

The enclosure light on PWM channel 11 uses Auto by factory default, with
on-level 128/255 and off-level 0/255. Its linear transition advances 4/255 every
20 ms, about 1.28 s for a full-scale fade, and catches up by at most 16 steps
after a delayed loop. Opening the door selects the on level; closing selects the
off level. Both levels and Off/Auto/On mode are EEPROM settings.

The optional 16x2 LCD is PC-owned. The host discovers common backpacks,
renders richer menu/event/prompt text, and can preload `PC offline` / `Connect
USB to PC` behavior. The AVR retains only compact transfer/offline support.

## RF initialization

The firmware links the installed `rc-switch` 2.6.4 library. Receive uses
D2/INT0 with edge-change interrupts and a configured 70% pulse tolerance.
The library accepts a decoded frame after the expected repeated transmission,
which rejects isolated noise but means a transmitter must repeat consistently.

Transmit uses D3, physically the INT1 pin, as a digital output. The rc-switch
constructor defaults to protocol 1, a 350 us base pulse, and 10 repeats.
Every native transmit command can select protocol 1..12, 1..32 bits, and an
optional pulse length. Receive is disabled during a send and re-enabled on
INT0 immediately afterward.

Learning defaults to 15 seconds, is capped at 120 seconds for timed sessions,
and also supports indefinite and multi-code modes. Up to 20 learned records
are stored in the board's EEPROM; names/categories/colors remain host-owned.

Seventy percent tolerance is intentionally permissive. If false decodes appear,
capture actual pulse lengths first and try 60% or a remote-specific value. Do
not narrow it merely to save bytes; it changes RF acceptance behavior.

## Shift registers, keys, relays, and monitored inputs

The 74HC165/74HC595 chain initializes with relay output enable inactive, writes
`0xFF` (all active-low outputs off), samples inputs, and only then enables the
outputs. It polls every 5 ms.

Keys occupy 74HC165 bits 0..3 and use:

- 50 ms debounce;
- 300 ms double-click window;
- hold start at 600 ms;
- held repeat every 150 ms;
- acceleration after 1800 ms to one repeat every 60 ms.

Bits 4 and 5 are reserved senses. Bit 6 is the active-low BT Audio LED input;
bit 7 is door-open-active-high. Both are debounced for 40 ms. BT transitions
within 2500 ms are classified as blinking; a stable active LED means BT Audio
connected, a blinking LED means its module is advertising/not connected, and
off means the module indicator is off.

Relay outputs are active-low and start off:

| Relay | Meaning |
|---|---|
| R1 | Side A direction; de-energized is Forward, energized is Reverse |
| R2 | Side A enable/output |
| R3 | Side B direction; de-energized is Forward, energized is Reverse |
| R4 | Side B enable/output |
| R5..R8 | Independent general outputs |

A reversal is sequenced as enable off, configurable break, direction change,
50 ms settle, then enable. The EEPROM default break is 1 ms; the compact
alternate setting is 100 ms. Direction changes between the two sides are
separated by at least 5 ms. Factory motion-door policy is `always`; `closed`,
`open`, and `never` are the other persistent policies.

## Buzzer, RGB, and addressable LEDs

The buzzer is driven by Timer1 CTC hardware output on OC1A/D9. No Timer1 compare
ISR runs at audio frequency, so buzzer playback does not starve INT0/INT1 RF or
UART service. Factory EEPROM is audible; the persistent silent flag suppresses
tones without changing command acknowledgement.

PWM channel 12 is the power indicator, and channels 13..15 are status RGB. The
factory status brightness is 128/255. Animation service runs every 20 ms with a
step of 4/255. The user-ready palette order is red, blue, violet, green, white;
status modes and transient event cues can override the ready color.

The addressable output is 11 pixels on D6 at 800 kHz. The inherited hardware
default is WS2811 BRG byte order. The startup frame is black/off. The current
compact driver reports full brightness and its compatibility brightness setter
does not scale pixels; brightness-sensitive effects should scale RGB values or
add a tested compact scaler.

## Factory EEPROM values versus live settings

These are the defaults used only when the EEPROM record is erased, invalid, or
explicitly reset. They are not proof of the settings currently stored on a
particular board.

| Setting | Factory value |
|---|---:|
| Silent | off / audible |
| Illumination mode | Auto |
| Illumination on / off | 128 / 0 |
| TM1637 brightness | 5 of 7 |
| Status brightness | 128 of 255 |
| PWM boot mode | Auto test |
| Telemetry stream | 500 ms |
| User PWM 0..7 | all zero |
| Default menu page | 0, Status |
| Save last page as default | off |
| Voltage / current decimals | 2 / 2 |
| Motion door policy | Always |
| Break before direction | 1 ms |
| Door / relay audio cues | enabled |
| Temperature identity swap | off; sorted ROM 0 is BT, ROM 1 is LED |

## What to verify before tuning

For each final firmware hash, record the following in the acceptance report:

1. I2C scan results and absence of a `0x40`/`0x41` collision.
2. INA219 bus, shunt, supply, current, and power compared with calibrated
   instruments at at least idle and one known load.
3. Both DS18B20 ROMs, role mapping, unplugged-pull-up behavior, and the expected
   LED-probe temperature rise while the BT probe stays comparatively cool.
4. PWM frequency, polarity, all-off startup, channels 0..10 sweep, channel 11
   enclosure fade, channel 12 power signal, and channels 13..15 RGB order.
5. TM1637 responsiveness, caching/no visible flicker, brightness, decimal
   choices, confirmation blink, and host-owned preview.
6. RF receive codes/repeats and a known transmit captured by another receiver.
7. Relay mapping and loaded direction interlock with safe physical observation.
8. UART reconnect without default DTR reset, boot melody, silent setting, and
   reset-count telemetry.

Only after those measurements should a setting be called "best" for the final
hardware. The values above are the current engineering choices and are exposed
here so any requested change can be made deliberately and compared against a
known baseline.
