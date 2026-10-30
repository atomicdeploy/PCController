# Shared production firmware and target acceptance

The cross-project transport and future repository-merger work is tracked in
[Controller-Convergence.md](Controller-Convergence.md). Read-only machine inventory:
`node Tools/Firmware/targets.mjs list`. This does not upload or commission a target.

## Source ownership

Issue [#103](https://github.com/atomicdeploy/PCController/issues/103) requires one
firmware implementation; [#227](https://github.com/atomicdeploy/PCController/issues/227)
owns its native transport/package adapters. This pass follows the earlier
firmware-platform task `019fadf5-2ee6-7053-96df-555488449b74` and the supplied
[MCU roadmap](Media-Timing-and-MCU-Expansion.md) / [successor checklist](Media-Timing-and-MCU-Expansion-Successor.md).
It does not create another architecture epic or change physical ownership.

All targets compile `PCController.ino`, `BoardSupportSources.cpp` and
`ControllerDomainSources.cpp`. Native startup, UART, dispatch, scheduling,
settings/CRC/persistence, relay/motion safety, macros, RF learning, menus, status
effects and reset are those production sources. Only platform adapters differ.
The source-contract test rejects resurrection of the independent VirtualBoard
dispatcher/hardware policy. CMake/source manifests and larger-target manifests
record exact source inventories rather than claiming same-source from protocol parity.

## Drift/recovery audit and retained richer behavior

The previous native runtime is preserved by Git at `5b0b60d5`, not built or kept
as a competing implementation. Do not wholesale restore it. Port valid behavior
through the production owner and retain the richer backend/profile where useful.

| Former separate behavior / drift | Shared owner and disposition | Evidence / remaining boundary |
| --- | --- | --- |
| Duplicate opcode/COBS/CRC dispatcher and state | Production UartProtocol/generated contract/Runtime; removed native switch and enum registry | UART/component, full-runtime and source-contract tests |
| Desktop settings padding / independent defaults | Packed production SettingsStore/EEPROM layout on every target | Compile-time layout, settings/reset tests, native image persistence |
| Own relay/motion bank and shortcut commands | Production RelayController/MotionPolicy/SystemInputs only | Safety adapters/machine tests, full macro cancellation; physical gates still required |
| Independent macro and RF state/persistence | Production MacroQueue/CoreMacroRing, RemoteLearningStore and RadioRuntime | Full-runtime queued dispatch/RF fixture tests plus existing component coverage |
| 11-pixel strip versus production 100; richer native brightness | Production AddressableLeds in all targets; expanded profiles preserve rendered global brightness and independent strip RAM | Full-runtime profile-dependent tests; no physical waveform claim |
| Native menu directory/order always present while 328P gates off | Existing production rich menu code retained as named expanded profiles | All profiles compile/test; optional hardware/EEPROM commissioning remains separate |
| Invented INA/temperature defaults, unconditional I2C successes | Removed false sensor defaults; explicit bounded DS fixtures; unknown devices NACK and partial reads fail | Unavailable-sentinel/CRC/I2C tests; live host providers are future commissioned adapters |
| Own soft reset that leaves unrelated C++ state alive | Production SafeReset cue/all-off followed by actual process restart | TCP reconnect, persistent count/cause, RAM-only restart image; C ABI returns RESET_REQUIRED |
| Relay stdout/demo use | Thin transport observes production relay outputs; ON:n/OFF:n retained | Real TCP conformance; verbose PWM logging opt-in to limit write churn |

Historical tests that asserted invented sensor/default/brightness/menu behavior
were not used to dictate the constrained hardware contract. Valid scenarios now
run through production component/full-runtime/transport tests. AVR-specific
optimized backends remain for ATmega328P; portable pin adapters and richer
feature bundles are retained for larger actual hardware.

## Exact target profiles

| Target | Build/profile policy | Deployment boundary |
| --- | --- | --- |
| ATmega328P, MiniCore, external 16 MHz | Existing guarded toolchain, UART0 Urboot/urclock, 32384 application / 32768 flash / 2048 SRAM / 1024 EEPROM; constrained defaults | Existing programmer/backup/readback/identity/stack safety gates unchanged |
| ATmega2560, MegaCore >=3.0.3, external 16 MHz | Explicit Arduino Mega pinout/UART0 Urboot 115200; 261632 application / 262144 flash / 8192 SRAM / 4096 EEPROM; constrained, expanded-panel, expanded-autonomous | Build-only until selected physical wiring/programmer/default-EEPROM/rollback acceptance; never route through 328P programmer |
| Windows x64; Linux x64/ARM64; macOS Intel/ARM64 | Production sources plus native adapters, all three profiles; TCP/binary stdio/in-memory/C ABI; source manifests/SDK | Demo/integration target, not physical peripheral or cycle-fidelity acceptance |
| Other AVR / STM32 / real Raspberry Pi I/O | Core reusable; exact chip, pins, timers/buses, storage/clock/reset and toolchain still must be selected | Not claimed as an implemented/commissioned target |

Mega pin ownership is explicit in `LocalLib/BoardPins.h`: A0..A3 for the shift
chain, D4/5/7/8 controls, D20/21 I2C, **D11 Timer1 OC1A buzzer**, D12 1-Wire,
D10 TM1637 data/D13 clock, D2/3 RF and D6/PH3 strip. Direct Mini port accesses
are guarded to 328P; Mega uses the configured pin map. Its fixed 20-cycle strip
sender uses the selected output-register address. Identity near/far flash reads
are target-specific; Mega identity occupies the last 12 application bytes at
0x3FDF4. These mappings follow [official Mega pinout](https://docs.arduino.cc/resources/pinouts/A000067-full-pinout.pdf)
and [MegaCore configuration](https://github.com/MCUdude/MegaCore), not assumed Mini equivalence.

## Reproducible build-only larger MCU lane

```console
node Tools/Firmware/targets.mjs build mega2560 constrained
node Tools/Firmware/targets.mjs build mega2560 expanded-panel
node Tools/Firmware/targets.mjs build mega2560 expanded-autonomous
```

This lane verifies installed core/FQBN/capacities before compilation, reuses the
configured Arduino CLI cache and records sources/artifact checksums and HEX
boundaries, resolved CLI/core/radio-library versions and the fixed PCID record.
CLI and radio-library resolution must match the configured toolchain lock;
`ARDUINO_CLI` / `ARDUINO_CLI_CONFIG_FILE` select an existing installation.
Changed sources during compilation invalidate delivery evidence.
It never opens a serial device, writes fuses or provisions another toolchain
copy. CI installs the declared Mega core in the existing CLI cache and preserves
build-only evidence separately from the eleven commissioned distribution packages.
The existing `firmware.mjs` remains the only commissioned 328P programming lane.

Named expanded bundles enable already-retained menu/local-LCD/scheduler/audio/
boot functionality, and profile 2 adds automations under existing storage
conflicts. Larger macro queues are deliberately not enabled behind the host's
127-byte wire contract. Per-feature isolated flash/stack/EEPROM deltas remain
unknown until #18 A/B measurements; aggregate profile sizes are not those deltas.
No dormant task, optional invalid EEPROM record or peripheral is advertised as
commissioned merely because it fits a larger chip.

## Delivery gates still requiring external evidence

Native CI verifies source/build/runtime and transport behavior, not physical
328P/Mega actuation, readback/reboot/soak, strip voltage/waveform or human UI
acceptance. No live board was flashed or relay/motion hardware actuated in this
migration. #227's Windows named-pipe/Unix-domain/pseudo-serial adapter matrix and
multi-instance in-process ownership remain follow-up work; current multiple-board
isolation is explicitly one process/image/port each. STM32 needs an exact chip and
pin/peripheral profile, not a generic family claim.
