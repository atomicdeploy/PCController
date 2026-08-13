# Output engine ownership

The production controller keeps transport and animation policy separate so a
small ATmega328P build can remain autonomous without duplicating host logic.

| Output | Board-owned engine | Host path | Shared implementation |
|---|---|---|---|
| Status RGB (PCA9685 13–15) | 62.5 FPS descriptor compositor with offline and safety ownership | One `STATUS_EFFECT` descriptor on capable boards | Host fallback and MOSFET fades share `internal/transition`; the MCU keeps its compact three-channel phase renderer |
| User MOSFET/PWM (PCA9685 0–10) | Direct logical-value writer | `pwm set`, `pwm fade`, and `pwm demo` stream normalized 12-bit values | Host interpolation uses the same linear/ease curves as legacy RGB rendering |
| Enclosure illumination (PCA9685 11) | Door-aware Off/Auto/On controller, 20 ms nonblocking ease | Settings can change mode and endpoints | MCU reuses `TransitionMath::easedByte` |
| Addressable strip (D6, 11 pixels) | Fixed, allocation-free WS281x buffer/transport | `strip pixel`, `strip fill`, and macros | The raw transport stays separate: it has strict 800 kHz timing and pixel state, unlike PCA register outputs |
| Buzzer (D9/Timer1) | Cooperative ten-step `TonePlayer`; door and motion/output cue selection reads a CRC-backed EEPROM record | Direct `beep`, macros, and named host melodies share the acknowledged Buzzer opcode | Exact rich sequences live in the host catalog; the MCU engine and compact offline cue controller remain reusable |

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
- The exact audio split and the rule prohibiting silent feature removal are in
  [Feature availability and removal ledger](Feature-Availability-and-Removal-Ledger.md).

## Front-panel production profile

The active local directory is `door`, `VOLT`, `CURR`, `tLED`, `tBT`, `LItE`,
`bEEP`, `rELY`, `KEY`, and `LErn`.

- `rELY` is the only relay page. K4 enters the editor, K3/K4 select R1..R8,
  and the value page applies Off/On. The former `r5-8` page is retired.
- `KEY` is the only motion/key page. Its four physical inputs map to Side A Up,
  Side A Down, Side B Up, and Side B Down. The former `MOVE` page is retired.
- The raw `PWM` and `uPWM` editors are one disabled production surface; direct
  host PWM controls remain available.
- `LItE` keeps Off/Auto/On and brightness editing. With a host present, edits
  remain live drafts for the host Save/Discard flow. Without a host they are
  persisted automatically.
- `bEEP` always provides K3 mute and K4 unmute. Extended local settings remain
  host-owned.
- `LErn` is visible and starts indefinite multi-code learning only while the
  host is connected.
- BT Audio LED detection is disabled in this profile; raw input cannot affect
  telemetry or RGB policy.
