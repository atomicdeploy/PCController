<div align="center"><a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Local library variant comparison

Three pre-existing local-library variants were reviewed before the current
`LocalLib` and `Project` layers were consolidated. They are described here as
**Flat A**, **Flat B**, and **Structured** so this maintained document records
the engineering decision without publishing unrelated project names or paths.

## What differed

Flat A and Flat B shared 13 byte-identical helper files for keys, rotary input,
seven-segment output, shift registers, tasks, tones, and addressable LEDs. Flat
A additionally supplied `pitches.h`; Flat B therefore was not independently
complete because its tone header referenced that missing file.

The Structured variant separated helpers by domain and contained several useful
low-level refinements:

- key release callbacks and a const state query;
- named shift-register input/output aliases and service entry points;
- an explicit task-service wrapper;
- tone-player state reset during buzzer setup;
- a heap-free rotary-input fallback and dual-input sampling;
- a 16x2 I2C LCD wrapper; and
- separate reusable melody definitions.

It also mixed those helpers with application-specific UI, actuator, calibration,
and fixed-list behavior. Some modules owned mutable objects in headers, performed
I/O during static construction, coupled displays to application state, or claimed
the only hardware UART. Those parts were deliberately excluded.

All three variants included implementation files directly or defined mutable
storage in headers. That was fragile outside a single sketch translation unit
and was replaced with one explicit implementation owner per module.

## What the final library retained

The current merge selects behavior rather than copying one variant wholesale:

| Domain | Retained and improved behavior |
|---|---|
| Pins | `BoardPins` centralizes the actual controller wiring and bus ownership. |
| Keys | Fixed-size, heap-free debounce plus down/up, click, double-click, hold-start, repeat, and release with real callback context. |
| Shift I/O | Proven clock/latch order, typed aliases, observable raw inputs, active-low semantics, and safe inactive startup. |
| Tasks | Fixed slots, zero-delay work, rollover-safe deadlines, and explicit active state. |
| Audio | Fixed nonblocking queue, clean reset/mute behavior, and Timer1 compare output on D9 without an audio-rate ISR. |
| Seven segment | Correct D11/D13 wiring plus cached text, decimal bits, brightness, and changed-only transmission. |
| Modes | `ModeManager` keeps explicit current/previous modes and separates one-time entry work from steady service. |
| Notes | `pitches.h` remains guarded and shared by reusable board/host melodies. |

The addressable-LED layer retains a fixed 11-pixel D6 buffer with selectable
WS2811/BRG or WS2812/GRB ordering, starts black, and exposes pixel/fill/clear/
brightness/show primitives. Application-specific effects were not imported;
current event effects build on this reusable layer.

Controller-specific behavior—sensors, RF, PWM ownership, illumination, status
RGB, relay/motion safety, EEPROM settings, reset telemetry, and the native UART
protocol—lives under `Project` rather than being hidden inside generic helpers.

## Deliberate exclusions

- Rotary input is inactive because D2/INT0 and D3/INT1 belong to 433 MHz receive
  and transmit.
- Application-specific UI, actuator, calibration, and fixed business-rule state
  machines were not imported.
- Mutable header-owned globals and heap-owned device objects were removed.
- Generic display/LED dependencies are retained in the managed toolchain for
  comparison, while the production AVR uses fixed-size local drivers where that
  materially reduces flash or SRAM.

The result preserves the reusable project layer, including the requested mode
manager, melody, input, display, and addressable-LED foundations, without
restoring unrelated business logic.

<p align="center"><a href="README.md">← Documentation hub</a></p>
