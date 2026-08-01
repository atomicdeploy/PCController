# Local Library Merge History

PCController began with `..\Puzzles`, then compared its `LocalLib` with
`..\Timer\LocalLib` and `..\..\motor_encoder_hmi\LocalLib`. The final merge is
under the root `LocalLib` directory; board-specific policy lives under the
root `Project` directory.

## Source differences

The compared source inventories were:

| Source | Files | Lines | Shape |
|---|---:|---:|---|
| Puzzles | 14 | 541 | flat board helpers plus `pitches.h` |
| Timer | 13 | 451 | flat board helpers, missing `pitches.h` |
| motor/encoder/HMI | 23 | 1,436 | helpers split into feature subdirectories |

### Puzzles and Timer

The two flat LocalLib directories contain 13 byte-identical files:

- `Keys.cpp/.h`
- `RotaryEncoder.cpp/.h`
- `SevenSegments.h`
- `ShiftRegisters.cpp/.h`
- `Tasks.cpp/.h`
- `TonePlayer.cpp/.h`
- `WS2811.cpp/.h`

Only Puzzles has `pitches.h`, although `TonePlayer.h` includes it. Timer is
therefore not independently complete without obtaining that header elsewhere.

Their WS2811 implementations are also byte-identical: 11 pixels on D6,
WS2811/BRG ordering, full brightness, a global pixel buffer, and an immediate
clear/show during setup. Effects and `FastLED.show()` calls were owned by the
old sketches rather than by a reusable controller.

### Motor/encoder/HMI branch

This branch reorganized LocalLib into subdirectories and made useful
low-level changes:

- `Keys` added release callbacks and a const state getter.
- `ShiftRegisters` added named `IN_1..IN_8` and `OUT_1..OUT_8` aliases and a
  `serviceShiftRegisters()` entry point.
- `Tasks` added `serviceTasks()`.
- `TonePlayer::setupBuzzer()` reset playback state.
- Its encoder avoided heap allocation and sampled both interrupt inputs.
- It added a 16x2 `LiquidCrystal_I2C` wrapper at `0x27`, although its display
  animation was coupled directly to the global motor state.
- It separated named welcome, finish, clear, and error melodies and carried
  its own `pitches.h`.

It also carried project-specific coupling that was not merged:

- key construction mutates a fixed global list and context callbacks always
  receive `nullptr`;
- HMI, Nextion, motor, rotation-control, fixed key-list, and named-melody
  modules contain application behavior rather than board primitives;
- the LCD wrapper owns mutable objects in its header and displays motor
  direction rather than exposing a reusable row API;
- Nextion owns the only hardware UART;
- `Motor` is defined in a header, performs I/O during static construction, and
  can inspect an uninitialized direction;
- `RotationControl` has early returns that leave calibration code unreachable;
- `HMI::initNextion()` has a value-returning path containing a bare `return`.

The motor/HMI encoder has two conditional implementations. With the external
RotaryEncoder library it constructs a static object and attaches both A and B
interrupts; its fallback is a small direct quadrature counter. Puzzles/Timer
instead allocate the library encoder with `new` during setup and attach only
the B interrupt. Neither was activated here because D2/INT0 and D3/INT1 now
belong to 433 MHz receive/transmit.

The inherited seven-segment wrapper assigned TM1637 to SDA/SCL, conflicting
with this board's I2C devices and its actual MOSI/SCK wiring. All three source
trees also include `.cpp` files directly and place mutable definitions in
headers. That works only accidentally in a single translation unit and causes
duplicate ownership in conventional C++ builds.

## Parts selected and improved

The merge retained useful behavior rather than copying one branch wholesale:

- `BoardPins` centralizes the actual ATmega328P port mapping, including
  TM1637 on D11/D13, 433 MHz RX/TX on D2/D3, and OneWire on D10.
- `ShiftRegisters` keeps the proven clock/latch sequence, active-low output
  semantics, and the motor branch's typed aliases. Storage moved to the
  `.cpp`, APIs are type-safe, raw inputs remain observable, and every output
  starts inactive.
- `Keys` keeps immediate debounced press/release behavior and passes the real
  callback context. It now also recognizes click, double-click, hold start,
  hold repeat, and hold release without heap allocation.
- `Tasks` keeps the fixed-size scheduler and wrapper. Slots have explicit
  active state, zero-delay work is supported, deadlines fire at `>=`, and
  comparisons are safe across `millis()` rollover.
- `TonePlayer` keeps the fixed nonblocking queue and reset behavior. Playback
  now uses Timer1 hardware compare on D9 without an audio-rate ISR, corrects
  completion/rollover handling, and supports global mute.
- `SevenSegments` uses the correct MOSI/SCK pins and caches content,
  decimals, and brightness so a frequent service loop does not retransmit
  unchanged frames.
- `I2cLcd` is a compact project-local PCF8574/HD44780 driver. It probes the
  common `0x27` and `0x3F` addresses and caches both 16-character rows.
- `ModeManager` preserves the useful inherited `mode`/`lastMode` transition
  pattern while separating one-time entry work from steady-state service.
- `pitches.h` is retained with an include guard.

The original Puzzles/Timer startup tune remains: 1032 Hz for 70 ms, 60 ms
pause, 2010 Hz for 70 ms, 60 ms pause, 2400 Hz for 120 ms, then 150 ms pause.
The reusable finish, lost, incorrect, error, and fault cues also remain
available, but no removed puzzle rule invokes them.

## Addressable LEDs

The inherited 11-pixel D6 hardware layer was restored under
`Project/AddressableLeds.*`. It keeps one explicit RAM buffer and separate
pixel/fill/clear/brightness/show operations, starts black, and supports the
original WS2811/BRG order or WS2812/GRB by configuration.

The production AVR path now uses a fixed D6/PD6 800 kHz sender instead of
FastLED or Adafruit NeoPixel. This avoids a second heap-owned pixel buffer and
keeps the narrow hardware layer buildable without a generic strip library.
No sweep, rainbow, puzzle, or other old business-rule effect was imported;
later effects can build on the retained layer.

## New project-layer modules

The compared projects did not provide the following reusable behavior, so it
was implemented specifically for PCController:

- `SystemInputs` debounces the Bluetooth indicator and reed switch, preserves
  raw readings, applies configurable polarity, and classifies Bluetooth as
  Off, On, or Blink.
- `PwmExpanderDriver` and `PwmController` provide compact I2C access, cached
  logical values, active-high/active-low mapping, and Off/Manual/Auto tests.
  Auto is limited to user channels 0-10 so enclosure, power, and status
  outputs remain under their owners.
- `IlluminationController` drives PWM channel 11 with saved Off/On levels,
  door-following Auto mode, and nonblocking fade.
- `StatusLedController` owns power channel 12 and RGB channels 13-15 for boot,
  ready, RF learning, warning, fault, and host-defined states.
- `RelayController` maps R1/R2 to direction/enable for Side A, R3/R4 for Side
  B, and R5-R8 to general outputs. Direction reversals enforce break and
  settling intervals before re-enabling a side.
- `Ina219Sensor` is a compact fixed-point driver that reports supply voltage
  as bus plus shunt voltage.
- `SettingsStore` keeps the canonical cap23 unversioned 29-byte settings
  value plus CRC-8, persists the eight user-PWM values and menu layout, and
  deliberately falls back to defaults instead of carrying migration code.
- `RemoteLearningStore` keeps 20 independently removable learned 433 MHz
  button records with CRCs and mappings to keys, menu actions, relays, side
  motion, or user PWM.
- `UartProtocol` supplies COBS framing, CRC-8, opcodes, bounded payloads,
  request sequences, status streaming, and asynchronous events. Its envelope
  revision is advisory; capability discovery and per-opcode semantic checks
  govern interoperability.

The program state set now covers Boot; VOLT, CURR, tLED, tBT, LITE, BT, SOUND,
PWM, RELAY, key identification, persistent user PWM, R5-R8 control, two-side
motion, and RF learning pages; editor submodes; save/discard feedback; RF
learning; and Fault. This restores the project structure and menu manager
without restoring removed business logic. This describes the proven
state-manager foundation used by the current `5DF10D05` image. 5DF retains
the flat 14-page root and adds a nested six-item Sound/display/status/
precision settings sequence; the broader hierarchical-menu request is tracked
separately and is not part of the LocalLib merge.

## Deliberately excluded

- Rotary/encoder support is inactive because D2/INT0 and D3/INT1 are assigned
  to the 433 MHz receiver and transmitter.
- Nextion, motor, HMI, rotation-control, and fixed application key-list
  modules were not imported.
- Old application-specific LED effects and puzzle/timer state rules were not
  imported.
- Mutable header-owned globals were removed. Because the Arduino sketch
  builder compiles sketch-root translation units but does not recurse through
  arbitrary root subdirectories, `PCControllerLocalLib.cpp` and
  `PCControllerProject.cpp` explicitly aggregate each implementation exactly
  once.
