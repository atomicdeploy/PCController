<div align="center"><a href="../../README.md"><img src="../../docs/assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# PCController native virtual board

This directory contains a native C++17 substitute for the ATmega328P
controller. It implements the same bounded COBS + CRC-8 + opcode protocol as
the firmware and listens on:

```text
tcp://127.0.0.1:8765
```

It is intentionally isolated from the PC host configuration. Board settings
are stored in a separate 1 KiB virtual MCU EEPROM image
(`virtual-mcu-eeprom.bin` by default). The current 40-byte settings/name value plus
its CRC byte, 20 learned-RF records, and wear-levelled reset journal use the
same canonical addresses, checksums, and no-version migration policy as the
physical MCU.

## Architecture

The emulator is split behind injectable hardware interfaces:

- `ISensors`: INA-like supply/current values, tLED, tBT, door, and Bluetooth
- `IRelays`: R1-R8 plus the two direction/enable side mappings
- `IPwm`: availability, selection, and all 16 logical 0-4095 PWM channels
- `IAddressableLeds`: 11 WS2811/WS2812 pixels plus global brightness
- `IDisplays`: TM1637 text, 16x2 LCD text, and buzzer state
- `IEeprom`: byte-addressable persistent MCU EEPROM

`VirtualBoard` owns protocol/state-machine behavior and accepts those
interfaces in its constructor, so tests or future firmware-facing adapters can
replace any emulated peripheral independently.

## Build

Requirements available from `PATH`:

- CMake 3.20+
- Ninja
- a working GCC `g++` with C++17 support

Windows Git Bash, Linux, or macOS shell:

```sh
chmod +x ./build.sh
./build.sh
```

The script selects the matching project-owned CMake configure, build, and test
presets, then discovers tools from `PATH` (or `CXX`). It contains no
machine-specific toolchain paths. The same presets can be used directly:

```sh
cd Tools/VirtualBoard
cmake --preset release
cmake --build --preset release --parallel
ctest --preset release
```

## Run and connect

Start the board:

```sh
./.build/release/bin/virtual_board.exe
```

Choose another EEPROM image or endpoint if desired:

```sh
./.build/release/bin/virtual_board.exe \
  --eeprom ./state/test-board.eeprom \
  --bind 127.0.0.1 \
  --port 8765
```

Point the PC host at it exactly like a serial device:

```sh
controller exec --port tcp://127.0.0.1:8765 hello
controller monitor --port tcp://127.0.0.1:8765
controller tui --port tcp://127.0.0.1:8765
```

The server accepts one active controller client at a time and automatically
accepts a replacement after disconnect.

## Interactive hardware controls

Type `help` in the virtual-board console. Important controls are:

```text
door open|closed|toggle
tled 28.25
tbt 25.75
bt off|on|blink
voltage 12.10
current 325
key 1 down
key 1 up
relay 5 on
pwm 0 2048
strip pixel 3 255 80 0 128
strip fill 0 0 255
strip clear
menu 4
segments DEMO
lcd Enclosure open
reset 0x08
show
quit
```

Door and Bluetooth changes generate immediate asynchronous device events.
`key` supports click, double, hold, repeat, and release gestures. To simulate
an RF learn, first issue `RF_LEARN_START` from the host and then enter:

```text
rflearn 0xABCDEF 24 1 350
```

Generate the raw receive event used by host automation with:

```text
rfrecv 0xABCDEF 24 1 350
```

The emulator looks up the code in its learned-remote table and includes that
entry ID, or `0xFF` when unmatched.

`reset` defaults to watchdog cause `0x08`; an explicit cause can be supplied
to simulate another MCU reset source. Both protocol reset modes and this
console control advance the same wear-levelled counter in the virtual MCU
EEPROM. That EEPROM image remains independent of the PC host's JSON
configuration.

Use `--no-stdin` for unattended test runs.

## Implemented wire behavior

- HELLO with build hash/date/time identity
- fixed 48-byte live STATUS and configurable streaming; byte 43 is the
  captured reset cause and bytes 44..47 are the persistent reset count in
  little-endian order; RF-received, buzzer-busy, Running, host-offline, and
  hot-temperature flags have the production meanings in bits 7 and 12..15
- exact schema-3 GET/SET_SETTINGS, including output-persistence, packed
  display/motion-hold options, relay-restore state, and the optional eight-byte
  operator name, persisted as the current 40-byte MCU value plus CRC
- tLED/tBT temperature identities and values
- direct PWM set/get/all-off and RGB; STATUS/PWM_VALUES report hardware
  availability, and the removed mode opcode `0x13` remains reserved
- addressable LED opcode `0x16`, including per-pixel/fill RGB and brightness
- safe side relay mapping, direct relays, and all-off; the firmware's local
  front-panel relay commissioning page is represented through menu state,
  while unadvertised opcode `0x34` remains unsupported
- the current 14-page board menu directory, schema-2 visibility/order, direct
  page selection, and save-last-page behavior
- exact front-panel snapshot plus TM1637/LCD text overrides; DisplayText
  targets 3/4 capture and release the PC-presented panel just like the MCU
- host-owned Idle/Running application state through opcode `0x45`
- schema-2 MCU-timed macro begin/append/run/query/cancel over a 127-byte
  circular queue; queued records reuse ordinary peripheral opcodes
- macro buffer/status events plus reserved sequence `0xFE` dispatch evidence,
  letting the host refill ahead and verify each device-side execution time
- bounded cooperative I2C write/read/repeated-start transactions and leases;
  simulated devices answer at `0x27`, `0x3F`, `0x40`, and `0x41`
- RF transmit plus single, multi, indefinite, list/remove/replace learning
  over all 20 persistent MCU slots; new codes remain unmapped, while assigned
  key/menu/relay/side/PWM actions use production repeat and 350 ms momentary
  semantics; removed partial-map opcode `0x26` remains reserved
- key events including gestures 5=down and 6=up
- timestamped door, Bluetooth, PWM-channel, RF-learn, macro, and reset events;
  the high bit marks the appended device-microsecond timestamp
- reset event type 7 encoded as
  `[0x87, cause, count LE u32, deviceMicros LE u32]`
- raw RF receive event type 8 with code/timing fields and learned ID
- application/bootloader reset requests as a persistent-EEPROM soft reset;
  each request advances the MCU-owned reset journal and emits reset event 7

The emulator deliberately does not claim AVR-cycle or electrical fidelity:

- wall-clock scheduling substitutes for AVR timers and interrupt latency;
- connected I2C devices use deterministic register bytes, not analog/bus-fault
  models or a PCF8574/INA219/PWM electrical implementation;
- relay direction/enable semantics and policy are preserved, but native tests
  (not the virtual relay bank) verify the configured 1..255 ms break and 5 ms
  cross-side direction interlock;
- host-menu directory opcodes `0x42..0x44` are unsupported because the current
  production firmware does not advertise them; PC-presented menus use the
  production DisplayText capture/release path instead.

Every acknowledgement carries
`[requestOpcode, error, deviceMicros LE u32]`. Macro timing is therefore
measured at the emulated MCU rather than inferred from host/network arrival.
Automations invoke named macros through the same playback path, so direct,
scripted, TUI, API, and bridge requests share cancellation and timing rules.

## Verification

Unit tests cover framing/CRC, maximum payloads, HELLO capabilities, STATUS,
temperature/front-panel/menu/I2C schemas, semantic extension tails, all 20 RF
slots, mapped-action execution/repeat suppression/momentary expiry, macro
cancellation, events, and canonical EEPROM persistence. A second
native target compiles the production key, relay, buzzer, shift-register, and
DS18B20 sources against an Arduino mock.

```sh
./build.sh
```

For a real TCP protocol smoke test, run the server and then:

```sh
./.build/release/bin/virtual_board_smoke.exe 127.0.0.1 8765
```

A successful exchange prints the authenticated `PCController` identity,
its native build hash, confirmation that a raw addressable-LED fill command
was acknowledged, the 48-byte reset telemetry fields, and a validated
type-7 reset event whose persistent count advances by one.
