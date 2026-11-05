# First USB connection: startup investigation and reset recovery

2026-11-05. Report: AVR does not respond after first USB connection until its
physical RESET button is pressed. **Board unavailable; cold-start root cause
not confirmed.** No firmware, fuse, EEPROM or output changes were performed.

## Confirmed host defects repaired

1. Standalone `controller reset` called `EnsureConnected` before `PulseResetFor`.
   A silent AVR therefore failed HELLO before the host could pulse its reset
   line. Remove this prerequisite: use the existing selected/remembered/unique
   serial-port recovery path, then reconnect and authenticate. The existing
   primary IPC path already reaches that pre-authentication reset mechanism.
   Keep primary ownership, ambiguous-port rejection, network-port rejection,
   cancellation and failed-close quarantine intact.
2. `PulseDTR` assumed DTR was already inactive. It now explicitly releases DTR,
   waits 20 ms, asserts it for the requested duration (default 120 ms), and
   releases it again. RTS remains untouched. This establishes an edge even if
   the adapter inherited an asserted DTR state. Assertion failure or cancellation
   still attempts release; release errors are returned rather than suppressed.
3. Provisional authentication's deadline included startup and HELLO retries but
   omitted the configurable reset duration. It now reserves both pulse time and
   pre-release settle time without consuming the one-shot reset permit while
   calculating the budget.

New/updated regression cases cover edge ordering, cancellation, driver errors,
reset budget, no repeated reset during HELLO retries, and the CLI prerequisite.
The full stable-path Controller Go suite passed (49 packages) during the
2026-11-05 integration pass, including these regression cases. Go vet passed
as well. CI and physical cold-start acceptance are separate gates.

## Why automatic recovery did not cover first connection

Ordinary serial open intentionally starts DTR/RTS inactive. The
`ResetOnReconnect` policy requires a previously authenticated session's
disconnect/reappearance and is opt-in. It is not a first-connect reset policy;
repeated authentication failures do not repeatedly reset devices. Those safety
semantics are unchanged. Explicit reset now works without a preceding HELLO.

The installed `go.bug.st/serial` v1.8.0 Windows backend uses GetCommState /
SetCommState for DTR. Its source documents problematic RTS/DTR interactions with
EscapeCommFunction on some drivers. No RTS manipulation or driver substitution
was added. A successful Windows API call confirms only driver acceptance, not
voltage at the AVR RESET pin.

## Firmware checks and limits

- `Project/ResetTelemetry.cpp` captures MCUSR (or Urboot's preserved r2 value),
  clears MCUSR and disables the watchdog in `.init3`, before constructors.
  The existing local firmware listing also contains that sequence at 0x230,
  ahead of `.data` initialization. This is an inspected pre-existing artifact,
  not a new firmware build or proof of what is installed on the absent board.
- `initializeController` enables a two-second watchdog and services UART between
  initialization stages. Non-HELLO commands are rejected until firmware-ready.
- OneWire discovery is capped at eight searches; TM1637 uses bounded byte/ACK
  cycles; the compact TWI driver has timed waits and bounded STOP handling.
  These checks did not establish a particular peripheral as the reported cause.
- Power-ramp/brownout, RESET electrical state, bootloader behavior, installed
  firmware/fuse identity, RF interrupt load and actual USB adapter behavior remain
  candidates, not diagnoses. No watchdog relaxation, blind startup reset loop,
  peripheral removal or speculative fuse change was made.

## Retry when the board is available

Use safe loads/normal hardware precautions: a reset restarts existing firmware
and may restore its persisted outputs. Do not flash or erase learned RF records
just to test this failure.

1. Record host executable identity, configured physical port/USB identity, board
   build identity if available, last reset count/cause and uptime. Keep exactly
   one serial owner. An active primary receives reset over IPC; use its selected
   board, rather than assuming a secondary CLI port flag changes its target.
2. Cold-connect USB without pressing RESET. Capture the initial HELLO attempts,
   reset lifecycle events and whether display/local keys respond. Distinguish
   an absent COM device from an enumerated device with silent application UART.
3. Explicitly request `controller reset --pulse 120ms` through the current owner
   (or specify the physical port when running standalone with no primary).
   Require fresh application HELLO; if possible compare reset count/cause and
   uptime with the pre-reset observations. HELLO proves the application is
   responding, not by itself that a reset occurred.
4. If DTR is accepted but the AVR remains silent, observe DTR and AVR RESET
   electrically. Compare with the physical button. Determine whether the reset
   edge reaches the MCU, whether RESET remains asserted, and whether supply is
   stable. Do not infer a wiring fault from a serial timeout alone.
5. Repeat true cold-power startup separately from USB reconnection to an already
   powered board. Read back the actual bootloader/fuses using the approved
   read-only procedure before considering a firmware/bootloader/fuse fix.

DTR auto-reset depends on the hardware connection: Arduino documents coupling
DTR to RESET through a 100 nF capacitor, which does not establish this board's
actual wiring. See [Arduino's auto-reset description](https://docs.arduino.cc/retired/boards/arduino-uno-rev3-with-long-pins/)
and [AVR-LibC's early watchdog-reset guidance](https://avrdudes.github.io/avr-libc/avr-libc-user-manual/group__avr__watchdog.html).

Completion gate: the host recovery defects are addressed in source; preventing
the reported cold-start failure remains open until its cause is reproduced and
the correction passes cold-power and DTR-reset tests on the actual board.
