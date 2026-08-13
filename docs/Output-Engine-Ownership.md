# Output engine ownership

The production controller keeps transport and animation policy separate so a
small ATmega328P build can remain autonomous without duplicating host logic.

| Output | Board-owned engine | Host path | Shared implementation |
|---|---|---|---|
| Status RGB (PCA9685 13–15) | 62.5 FPS descriptor compositor with offline and safety ownership | One `STATUS_EFFECT` descriptor on capable boards | Host fallback and MOSFET fades share `internal/transition`; the MCU keeps its compact three-channel phase renderer |
| User MOSFET/PWM (PCA9685 0–10) | Direct logical-value writer | `pwm set`, `pwm fade`, and `pwm demo` stream normalized 12-bit values | Host interpolation uses the same linear/ease curves as legacy RGB rendering |
| Enclosure illumination (PCA9685 11) | Door-aware Off/Auto/On controller, 20 ms nonblocking ease | Settings can change mode and endpoints | MCU reuses `TransitionMath::easedByte` |
| Addressable strip (D6, 11 pixels) | Fixed, allocation-free WS281x buffer/transport | `strip pixel`, `strip fill`, and macros | The raw transport stays separate: it has strict 800 kHz timing and pixel state, unlike PCA register outputs |

Merging the strip sender into the PCA status engine would not save flash: the
protocol, timing, and storage shapes are different. The reusable boundary is
the transition math and higher-level descriptor/event model. A future strip
animation controller can consume the same host transition package and a small
MCU phase helper without replacing either hardware writer.

## MOSFET transitions

Use the shared command path from CLI, TUI, Web console, RPC command execution,
or any other command surface:

```text
pwm fade user7 4095 1000 ease
pwm fade user7 0 1000 linear
pwm demo 600 ease
```

`pwm demo` visits user1 through user11, fades each on and off, and ends with all
user outputs off. The current ATmega328P image does not carry a multi-channel
PWM-transition scheduler; that belongs to a larger-flash profile or a future
compact opcode after SRAM/flash measurement.

## Autonomous boundaries

- Status RGB effects, the power indicator fallback, door-driven enclosure
  illumination, motion safety, relay sequencing, RF learning/mapping, buzzer
  tone playback, and macro replay remain board-owned.
- Rich RGB policy selection, arbitrary MOSFET streams, addressable-strip
  compositions, naming, Save/Discard presentation, and extended UI remain
  host-owned.
- Generic EEPROM startup-opcode execution is not implemented yet. It remains a
  separate design item because commands need strict allow-listing, bounded
  execution time, atomic storage, and a recovery path before they are safe to
  run before HELLO.
