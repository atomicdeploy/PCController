# PakarController convergence and MCU portability

Linked work: [PCController#702](https://github.com/atomicdeploy/PCController/issues/702)
and [PakarController#41](https://github.com/atomicdeploy/PakarController/issues/41).
The detailed comparison and phased decision live in PakarController's
[TRANSPORT-CONVERGENCE.md](https://github.com/atomicdeploy/PakarController/blob/main/docs/TRANSPORT-CONVERGENCE.md).
Existing issues103/227/18/125/375 retain their firmware/MCU/contract ownership.

## This source checkpoint

COBS is shared framing, not another USB carrier. The bounded firmware codec and
Go host decoder now reject embedded zeroes inside encoded data blocks, agreeing
with Pealayer's current behavior. Valid encodings and numeric opcodes are unchanged.
The host UDP encoder additionally enforces the same full nonfinal-fragment rule
as its decoder. Shared framing fixtures and native/UDP regressions exercise the
actual implementations without contacting hardware or opening LAN listeners.

Do not attach a Pakar native protocol as PCController's by discarding its envelope
checks. A5/COBS/CRC and48-byte buffers are common, but opcode0x11 means different
things, and HELLO/status/settings/timed receipts/storage have distinct semantics.
Future convergence uses a living capability/service contract, not version-selected
adapters, exact-build equality or a third independent dispatcher. CRC is not auth;
unknown required operations fail closed and uncertain mutations are not replayed.

The opcode follow-up now owns both namespace allocations in
Project/ProtocolContract.json. The portable generator emits the actual C++, Go,
Rust and Dart views; consumer imports are pinned to exact canonical source bytes
and commit. Independent published-allocation fixtures and CI base comparisons
reject renumbering, removal or reuse. Fixed A5/1 native versus A5/0 Pakar admission
is mandatory before dispatch; no fallback reinterprets one namespace as the other.
Unknown optional operations inside the correct namespace remain supported by the
normal Unsupported path. This does not unify the distinct payload semantics.

## Real target inventory, not hypothetical ports

`node Tools/Firmware/targets.mjs list` reports the existing primary ATmega328P
tool, additional Mega2560 lane, feature profiles, capacities and the native CMake
conformance entrypoint without invoking Arduino CLI or a programmer. The extra
target is explicitly architecture=avr/buildAdapter=arduino-avr and build-only,
with programmingCommissioned=false. An unsupported adapter fails before compiler
or upload work. See Shared-Production-Firmware.md for the actual source owners,
pins, timers, EEPROM/identity and safety boundaries; those remain unchanged.

The one production sketch plus BoardSupportSources.cpp and
ControllerDomainSources.cpp continue to serve all current firmware targets.
Future STM32/ESP targets require actual GPIO/UART/USB, timer/clock, ISR, storage,
bus/watchdog/reset and peripheral adapters, selected chips and resource/physical
evidence. Merely adding a target record does not implement those ports.

## Planned integration and acceptance

Next: shared operation/event/identity/correlation/deadline/error/transfer semantics,
explicit full/slim build capabilities and cross-carrier/client conformance.
Then: fault/loss/reconnect/queue/lease, resource-budget and hardware/soak tests.
Preserve standard ROM/Urboot/ISP recovery adapters and richer MCU timing data.

The later repository merger must retain both unrelated Git graphs and original
commits, compare every file and license using the existing review ledger, preserve
private state separately, and consolidate implementations only after behavior has
a canonical source owner. Use guarded semantic configuration imports, not raw
EEPROM copies. No unrelated-history merge, deployment or physical acceptance is
claimed by this pass. Open PRs/drafts and retained stashes are not deleted simply
to claim a clean handoff.
