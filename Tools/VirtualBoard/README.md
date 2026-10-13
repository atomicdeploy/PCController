<div align="center"><a href="../../README.md"><img src="../../docs/assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# PCController production firmware on an OS

VirtualBoard is now a **native target of the production firmware**, not an
independent implementation. `src/native_firmware.cpp` includes `PCController.ino`;
CMake compiles `BoardSupportSources.cpp` and `ControllerDomainSources.cpp` too.
The same UART parser, generated opcode contract, dispatcher, menus, macro
executor, RF learning, safety/reset policy, status effects and EEPROM owners run
on both physical and OS targets. No old `virtual_board.cpp`/`hardware.cpp` runtime
is shipped. Git retains those files as historical recovery/reference material.

`platform/` implements only the hardware surface: monotonic/deterministic time,
bounded binary UART queues, GPIO/input/relay observation, EEPROM, RF packets,
PWM-expander registers, TM1637 ACK presence and strip output. There is no native
opcode switch or second settings/state model. The client codec helpers use the
production bounded codec and generated constants; they are not the server parser.

## Build and profiles

Use CMake 3.20+, Ninja and GCC/Clang C++17. Windows selects available clang++ if
the caller has not selected a compiler; MinGW builds link their runtime statically.
Use the existing cache rather than creating a new worktree/cache for each pass:

```sh
cmake --preset release -DPCCONTROLLER_FEATURE_PROFILE=0
cmake --build --preset release --parallel
ctest --preset release
```

| Profile | Shared production behavior |
| --- | --- |
| `0`: constrained | Commissioned 328P feature gates, 127-byte macro ring and shared 300-byte macro/strip RAM; fixed strip brightness |
| `1`: expanded-panel | Rich menu directory/visibility/order/hierarchy/layout, local LCD renderer, optional EEPROM audio/boot hooks, scheduler; independent strip RAM and rendered brightness |
| `2`: expanded-autonomous | Profile 1 plus dual-bank board automations; existing EEPROM conflicts intentionally disable incompatible status-profile storage |

Optional EEPROM hooks still require valid provisioned records. Compiling a
scheduler does not invent registered autonomous tasks. Queue size remains the
host's 127-byte contract: widening it requires capability/host negotiation.
Profiles 1/2 are rejected for ATmega328P. For physical Mega builds see
[shared firmware and target gates](../../docs/Shared-Production-Firmware.md).

All five native CI platforms build/test all three profiles in one cache. The
package uses profile 2 and includes the standalone program, C ABI library/header,
CMake export and source/target manifests. `cmake --install .build/release --prefix
.build/distribution` stages the same SDK locally. `firmware-sources.sha256` records
the actual production source inventory; `firmware-manifest.json` records the
target/profile and source identity. It does not assert physical acceptance.

## Run / demo without an AVR

```sh
./.build/release/bin/virtual_board --port 8765 --memory --no-stdin
controller exec --port tcp://127.0.0.1:8765 hello
controller exec --port tcp://127.0.0.1:8765 status
controller tui --port tcp://127.0.0.1:8765
```

On Windows use the corresponding `.exe`. Distribution executables are under
`bin/`. The default listener binds loopback only. One active host connection is
accepted; TCP fragmentation/coalescing goes directly through production UART.
Slow-client output is bounded/nonblocking and cannot stall the safety loop.
Disconnected partial frames are discarded, but board state/time are not reset;
production host-offline deadlines still govern output cancellation.

Without `--memory`, the default EEPROM is `native-firmware-eeprom-<port>.bin`.
`--eeprom FILE` selects another **exact 1024-byte** current-layout image. Each image
has exclusive process ownership; use distinct images and ports for multiple
boards. Dirty bytes are coalesced to atomic image replacement at most once per
second, with immediate boot/reset/shutdown flushes; unchanged loops do not write.
The old `virtual-mcu-eeprom.bin` is not silently overwritten/imported. Preserve it
and explicitly compare/provision useful settings through current host contracts.
RAM-only EEPROM survives protocol reset via an explicit restart image, without
SSD writes. Process reset clears all firmware globals/function statics, not just
calling `setup()` again. Persistent reset cause/count come from shared firmware.

TCP mode prints actual relay changes as `ON:n` / `OFF:n` on stdout, compatible
with simple Pealayer/demo log consumers. `--quiet` disables these and connection
messages. `--log-pwm` explicitly enables verbose PWM logs; procedural status LED
changes do not flood default disk-backed logs. No log file is created by default.

`--stdio` runs the same firmware over binary stdin/stdout; diagnostics go to
stderr. It never intermixes relay text with binary frames. `--pid-file FILE` is an
explicit process-management artifact useful for tracing Windows process reset.

## Truthful inputs and live values

Native relay/PWM/strip/display/buzzer values reflect the running shared drivers.
There is no electrical waveform, physical motion or AVR interrupt-fidelity claim.
INA219 and DS18B20 values are unavailable by default, not synthetic 12 V/25 C.
CPU temperature is **not** mislabeled as LED/BT temperature. Native HELLO feature
`0x80` marks mapped/fixture inputs; the host supplies renamable Native sensor
defaults without overwriting operator/profile titles. A future commissioned
Linux/Raspberry Pi GPIO/I2C/sensor adapter can use this same production core.
The existing Linux ARM64 build is the no-hardware/demo adapter, not Pi wiring proof.

### Pealayer development without hardware

Build the native profile and current controller once, then run:

```sh
node Tools/VirtualBoard/dev-host.mjs
```

The launcher runs the shared profile-2 firmware in RAM on loopback `18765` and
an isolated, headless coordinator on `18987`. In Pealayer select hardware endpoint
`pccontroller://127.0.0.1:18987`. Its initial cinema profile advertises both seat
controls and user outputs for authoring; existing profile names, effects and
folders survive restart. Native settings/front-panel caches are read once so a
controller GUI is not required. The normal Pealayer transport, timeline,
recording, macro and safety paths are used, not a second mock engine.

Use `--board FILE` / `--controller FILE` for packaged binaries. The launcher
checks both ports and a directory-ownership marker, refuses unowned existing
configuration, and does not read or replace production settings. LAN discovery,
keyboard integration, measurement history and buzzer mirroring start disabled.
Ctrl+C stops only its children. The native board creates no EEPROM/log file;
explicit authoring/profile saves still persist in the isolated development root.
The launcher does not start/update Pealayer or change its existing authoring state.

### Explicit host sensor mappings

Native sensor paths are read-only scalar sources. Map known Linux thermal/hwmon
files under `/sys`. Custom adapters must write scalar files inside the owned
sensor directory: `%LOCALAPPDATA%/Programs/PCController/sensors` on Windows,
`~/Library/Application Support/PCController/sensors` on macOS, or
`$XDG_STATE_HOME/pccontroller/sensors` (default `~/.local/state/pccontroller/sensors`)
on Linux. Relative paths, traversal, prefix collisions and links escaping those
roots are rejected. Missing files within a permitted root still report unknown
and can recover when the adapter creates them. Arbitrary host-file reads are not
a sensor mapping feature. Map known thermal/hwmon
temperature files (milli-Celsius), or power_supply voltage/current files
(micro-volts/micro-amps), for example:

```sh
node Tools/VirtualBoard/dev-host.mjs \
  --sensor-temp0 /sys/class/hwmon/hwmon1/temp1_input
```

Inspect that device's `name`/`type` first; hwmon numbering is not a stable sensor
identity. Rename the advertised sensor to its actual source (e.g. Host CPU).
`--sensor-temp1 FILE` maps the second role without sliding it into the first.
`--sensor-voltage FILE --sensor-current FILE` must be supplied together: missing
current is not zero. Both native voltage slots reflect that same mapped voltage,
and power is derived from signed voltage/current; they are not independent INA219
electrical measurements. No bus at address 0x40 is fabricated. The same options
work directly on `virtual_board`, including stdio mode.

Files are parsed strictly with a 64-byte bound and sampled at most once per second,
then production conversion/smoothing/validity policies run normally. Missing,
malformed or out-of-range input becomes unavailable and recovers on later valid
input. A mapped temperature can trigger the shared HOT safety policy: select the
source deliberately. These are host readings, not temperatures of absent LED/BT
peripherals. Windows does not have a universally available standard CPU/rail
sensor provider; keep unavailable values absent or explicitly supply a trusted
sensor-export file in these units. No WMI polling subprocess or invented reading
is added to the firmware loop.

With interactive stdin enabled, `help` lists bounded, explicitly named fixtures:

```text
status
door open|closed
raw 0..255
tm1637 on|off
temp 0|1 <centiC>|none
rf <code> <bits> <protocol> <pulseUs>
quit
```

Door/raw input fixtures traverse production debounce/safety; RF fixtures traverse
the production learning/table/repeat/momentary-action code. Temperature fixtures
traverse production discovery/roles/conversion/smoothing and never masquerade as
live physical measurements. TM1637 fixtures exercise the real ACK detection and
reinitialization/recovery path without reset. Use controller protocol commands for
relay, PWM, menu, settings, macro or display authoring—console shortcuts cannot
bypass their dispatcher/safety owners. Generic I2C addresses other than the native
PWM device at 0x41 NACK; missing/partial reads are not successful fabricated data.

## Embedding and verification

`include/pccontroller_firmware.h` exposes a length-aware C ABI with bounded feed,
partial/lossless poll, error and flush/close calls. No exception crosses the ABI.
Each process/library lifetime owns **one MCU**, matching existing firmware global
ownership. A second instance is rejected; use isolated workers for multiple
boards. `RESET_REQUIRED` tells an embedding owner to restart its worker. Closing a
handle does not falsely promise a fresh reset/reopen of global C++ state.

CTest includes component, full-runtime, source-manifest, C ABI and independent
stdio/TCP conformance checks. The transport test proves identity/capability parity,
48-byte unavailable-sensor telemetry, real relay stdout, parser reconnect, exclusive
EEPROM ownership, reset all-off, actual process restart and persistent boot count.
It creates only a small private image and removes its own test state/processes.
Native/CI success does not replace physical AVR programming/readback, electrical
diagnostics, human Pealayer authoring acceptance or soak gates (#103/#227/#116).
