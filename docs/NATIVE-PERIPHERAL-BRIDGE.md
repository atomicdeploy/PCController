# Native board diagnostics and host peripheral bridge

The OS-native target still runs the production sketch, UART dispatcher and safety
owners. These adapters do not create another relay/PWM model or a text-command
backdoor. See [shared firmware ownership](Shared-Production-Firmware.md).

## History and current text format

Pealayer's original timeline stand-in printed `1:ON` / `1:OFF`; later versions
sent serial text and then framed binary commands. The native board's separate
`ON:1` / `OFF:1` observer was added in `265bc8b2` and retained through the shared
production-source migration `bf8106d3`. The history and visibility gap are tracked
in [Pealayer #293](https://github.com/ToghrolTP/pealayer/issues/293).

The native board's **default diagnostic format is now channel first**:

| Output | Meaning | Enabled by |
| --- | --- | --- |
| `R5:ON`, `R5:OFF` | Observed one-based relay 1..8 state change | TCP mode, without `--quiet` |
| `P1:4095` | Observed one-based PCA9685 logical channel 1..16 value, **0..4095** | `--log-pwm` |
| `BZ:440`, `BZ:0` | Observed buzzer frequency in Hz; zero is rest/off | `--log-peripherals` |
| `SEG:01020304` | Four raw segment-cell bytes, hex | `--log-peripherals` |
| `BRIGHTNESS:7`, `DISPLAY:OFF` | Segment brightness / detected display presence | `--log-peripherals` |
| `STRIP:...` | Rendered strip bytes in firmware **wire order**, hex | `--log-peripherals` |

PCA9685 register flags use 4096 internally for FULL_ON/FULL_OFF. They are **not**
the logical PWM range; 4095 remains fully active regardless of electrical
polarity. `P1` means logical channel index 0, not a configured user channel name.
Use the peripheral catalog for stable application identity and current labels.

`--output-format legacy` preserves `ON:n` / `OFF:n` and `PWMn:value` for old log
consumers. It does not change the binary protocol. Continuous output logging is
opt-in; `--quiet` suppresses all activity lines. In `--stdio`, stdout is binary
only, including when log flags are present. Diagnostics and input feedback use
stderr. No diagnostic log file is created automatically.

These are successive output-snapshot differences, not command acknowledgements,
an initial state dump, a lossless edge/timing trace, or physical load verification.
Applications must use structured controller snapshots/events/history. Do not
restore Pealayer's stdout parsing as its operational state owner.

## Stdin input adapter

TCP mode accepts input observations on stdin unless `--no-stdin` is set. Each
line is bounded to 256 bytes; oversized lines are discarded through the newline.
Trailing arguments, invalid ranges and incomplete values are rejected before
mutation. This path cannot execute shell commands or set relay/PWM outputs.

| Input | Interpretation |
| --- | --- |
| `input 1 on`, `I1:ON` / `I1:OFF` | One-based shift-register input 1..8; ON is active-low/pressed. Existing debounce and safety still run. |
| `gpio 42 0`, `GPIO42:1`, `gpio 42 none` | Raw input observation for native Arduino pin 0..63; `none` restores its unassigned pull-up behavior. Does not override output pins or the device-owned TM1637 ACK slot. |
| `temp 0 2500`, `T1:2500`, `T2:none` | Explicit temperature slot, centi-Celsius, -5500..12500 or unavailable. Slot identities do not slide. |
| `power 12000 -250`, `power none` | Supply mV and signed current mA, supplied together; derived power mW, or unavailable. |
| `door open`, `door closed`, `raw 255` | Existing explicit digital input fixtures. |
| `rf CODE BITS PROTOCOL PULSE_US` | Existing bounded RF reception fixture; not an output/learning authorization. |

Assigned physical GPIO sources own their input bits; stdin `input`, `door`, or
`raw` cannot overwrite them. A configured sensor file owns its temperature/power
source; conflicting stdin writes are rejected. Unavailable data is not zero.
Mapped temperature can trigger the shared HOT policy. Fixtures are not evidence
of physical measurements. `--stdio` deliberately has no text parser.

## Dedicated defaults versus explicit reuse

| Adapter | Default and support | Assignment / opt-out |
| --- | --- | --- |
| PC speaker | **Auto** when accessible. Linux uses finite kernel-duration KDMKTONE; Windows uses an already-loaded WinRing0 native driver. No driver installation/elevation or sound-card Beep fallback. | `--speaker off` |
| Raspberry Pi I2C/PCA9685 | **Auto** on an identified Raspberry Pi with an accessible, unclaimed device at 0x41 on `/dev/i2c-1`. No general bus scan. | `--i2c off`, or explicit `--i2c /dev/i2c-N` |
| GPIO relay output | Linux GPIO character-device v2 line, active high | `--relay-gpio 5=/dev/gpiochip0:17` |
| GPIO input | Linux GPIO character-device v2 line, active low, production debounce | `--input-gpio 1=/dev/gpiochip0:27` |
| Keyboard / other LED | Linux LED-class brightness; original value restored on clean release | `--relay-led 5=/sys/class/leds/DEVICE/brightness` |
| Windows Scroll Lock LED | OS keyboard indicator IOCTL, preserving other LEDs; no key injection | `--relay-scroll-lock 5` |
| Optical tray | Linux CD-ROM ioctl or Windows storage eject/load ioctl; no forced unmount or media-lock override | `--relay-optical 6=/dev/sr0`, Windows `--relay-optical 6=D:` |
| Parallel port | Linux ppdev claim, data bits 0..7 (DB25 pins 2..9); shared claim for multiple assigned bits, original data restored on clean release | `--relay-parallel 7=/dev/parport0:0` |
| System ARGB | Explicit OpenRGB SDK server/device; negotiated protocol 1/2, bounded metadata and LED count, rendered RGB colors | `--strip-openrgb 0@127.0.0.1:6742` |
| Host measurements | Existing bounded read-only scalar files; known sysfs sources or the owned PCController sensor directory | `--sensor-temp0 FILE`, `--sensor-temp1 FILE`, paired voltage/current files |

`--list-peripherals` inventories candidates without opening/actuating them. A
candidate is not confirmed usable: OS permissions, drivers, existing ownership,
exact wiring and electrical suitability still matter. `--no-host-peripherals`
disables the dedicated defaults and rejects any simultaneous physical assignment.
Normal native component/conformance tests disable host peripherals explicitly.
The C ABI does not auto-open host devices; the standalone adapter owns them.

Dedicated defaults are not a license to claim unrelated host devices. I2C auto
does not scan x86 motherboard SMBus, force a kernel-bound address or invent an
INA219 at 0x40. Explicit adapters require the normal OS device class/driver;
USB-to-I2C/GPIO/parallel devices work only when that driver exposes the supported
interface. Arbitrary vendor HID, FTDI bit-bang, direct x86 parallel I/O and macOS
physical backends are not implemented here. No raw arbitrary I2C gateway is added.

Only one explicit output sink is assigned per relay. Repurposed sinks do not
replay a saved ON state at attachment: they follow subsequent observed edges.
GPIO outputs start inactive when their lines are claimed. Optical ON means eject,
OFF means load; no extra tray action runs at shutdown. Relay state in the API is
still the firmware's output state, **not** an acknowledgement from a mapped load.

Host output work runs outside the firmware loop. Its edge queue is bounded;
backpressure disables mapped sinks, and stale queued actuation is not replayed.
Device failures are reported on stderr and are not automatically retried.
Linux PC-speaker tones expire in the kernel even after a process failure;
Windows driver shutdown is best-effort and is not a crash-safe hardware watchdog.
One owner must control each physical speaker/device; do not also enable a second
host buzzer mirror against the same speaker.

OpenRGB uses a separate latest-frame worker, converts native wire order to RGB,
and neither applies brightness twice nor resizes the target's zones. Extra target
LEDs are black; excess source pixels are trimmed. Assigning it selects Direct
mode. It leaves the last frame/mode for the operator to restore on release.
Topology change, malformed data, failure or disconnect disarms it until explicit
restart/reassignment; no automatic reconnect or unknown-delivery replay occurs.
Transport success is not physical color/latency acceptance. SignalRGB input
protocol support elsewhere in the host is not an OpenRGB/SignalRGB output-parity
claim for this adapter.

Native Linux interfaces follow the kernel's [I2C userspace API](https://docs.kernel.org/i2c/dev-interface.html)
and [GPIO v2 API](https://docs.kernel.org/userspace-api/gpio/chardev.html).
The ARGB sink follows the [OpenRGB SDK contract](https://gitlab.com/CalcProgrammer1/OpenRGB/-/blob/master/Documentation/OpenRGBSDK.md).

## Cross-project acceptance remains explicit

Producer monitoring/events belong to [PCController #41](https://github.com/atomicdeploy/PCController/issues/41)
and #36. Consumer visibility belongs to [Pealayer #293](https://github.com/ToghrolTP/pealayer/issues/293),
linked into [Pealayer #80](https://github.com/ToghrolTP/pealayer/issues/80).

- [ ] Native tests: new/legacy formatting, unchanged values, raw PWM endpoints,
  bounded input, ownership, unavailable sensors and real binary TCP/stdio parity.
- [ ] OpenRGB fixture: fragmented packets, negotiated metadata/count, wire-order
  conversion/brightness, topology disarm and no reconnect/replay.
- [ ] Platform CI for all native targets/profiles, including actual Linux branch
  compilation; Windows-only build success is not Linux GPIO/I2C evidence.
- [ ] Physical commissioning of each selected adapter: permissions, ownership,
  wiring, safe initial/failure state, actual outputs, release and process failure.
- [ ] Pealayer's activity view: bounded chronological structured events with
  configured channel names/icons, provenance/timebase, gaps and intent versus
  observations, filtering/copy/export and stable scrolling. This producer change
  does not implement or visually accept that view.
- [ ] Expose adapter health/assignment preferences through host/native/Web UI;
  current new assignments are standalone CLI configuration and stderr diagnostics.

Do not close the trackers from compilation, a simulated test, an ACK, a candidate
inventory, or documentation alone. No physical peripheral is commissioned by
merging this code.
