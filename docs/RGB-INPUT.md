# External RGB input

PCController's host can receive RGB strip frames from **OpenRGB** without giving
OpenRGB the AVR serial port. The board continues to use the existing PCController
protocol: configure, stage RGB chunks, then commit after their acknowledgements.
No firmware flash or virtual COM driver is required for the OpenRGB UDP route.

## Start in PCController

Run the updated persistent TUI, Web or native GUI host with the correct board
already connected. In Web UI → Workbench → LED strip, use **External RGB input**.
Choose OpenRGB, the physical LED count (1–100), and an output FPS limit (1–120).
The default is 60 FPS; actual throughput is limited by board acknowledgements.

Alternatively, in the TUI shell:

```text
rgb-input start openrgb --pixels 60 --fps 60
rgb-input status
rgb-input stop
```

From a second terminal, prefix these commands with `controller.exe exec` (or
`controller exec` on Linux), using the same `--config` as the persistent host.
The one-shot CLI refuses to start a listener without a persistent primary host.

Default OpenRGB endpoint: **127.0.0.1:65506/UDP**, disabled until explicitly started.
Nothing is written to the strip merely by opening the listener.

## Configure OpenRGB

In OpenRGB's LED Strip device settings, add:

| Field | Value |
|---|---|
| Name | PCController strip |
| Port | `udp:127.0.0.1` |
| Baud / UDP port | `65506` |
| Number of LEDs | Same physical count as PCController |
| Protocol | `keyboard_visualizer` / Keyboard Visualizer |

If configuring OpenRGB.json, merge this device into the existing
`LEDStripDevices.devices` array; do not overwrite other devices/settings:

```json
{
  "name": "PCController strip",
  "port": "udp:127.0.0.1",
  "baud": 65506,
  "num_leds": 60,
  "protocol": "keyboard_visualizer"
}
```

Save/reload OpenRGB's device configuration, select the resulting strip, and send
direct colors or use an OpenRGB effect. Start PCController input when the sender
is ready: an unused listener closes after 60 seconds.

This uses OpenRGB's existing [LED Strip detector](https://gitlab.com/CalcProgrammer1/OpenRGB/-/blob/master/Controllers/LEDStripController/LEDStripControllerDetect.cpp)
and [UDP output implementation](https://gitlab.com/CalcProgrammer1/OpenRGB/-/blob/master/Controllers/LEDStripController/LEDStripController.cpp).
OpenRGB's Adalight and TPM2 outputs in that driver are **serial-only**; do not
select either of those for the above UDP configuration.

## Other senders

`rgb-input start tpm2net --pixels 60` accepts TPM2.NET on the same endpoint.
One complete frame per UDP datagram: `9C DA length_hi length_lo 01 01`, RGB bytes,
then `36`. Fragmented/multi-packet frames, commands, polls and wrong lengths are
rejected. The current 100-pixel board maximum fits in one datagram.

`rgb-input start adalight --pixels 60` accepts standard Adafruit Adalight frames
**encapsulated in UDP**, not a serial listener: `Ada`, big-endian LED count minus
one, checksum `hi XOR lo XOR 55`, RGB bytes. This is for custom senders, not
OpenRGB's serial-only Adalight output.

## Additional network inputs

All endpoints default to loopback. Select the protocol in Web UI; its untouched
default port changes automatically. Custom addresses are retained. For a LAN
sender, bind the PC's specific local IP and set the trusted sender's IP.

| Input | Default UDP port | Sender configuration / supported scope |
|---|---:|---|
| DDP | 4048 | RGB24 display ID 1, offset 0, one complete PUSH frame |
| Art-Net | 6454 | Unicast ArtDmx, explicit 0-based port-address/universe and first DMX slot |
| sACN / E1.31 | 5568 | Unicast, explicit universe 1–63999 and first DMX slot |
| WLED realtime | 21324 | DRGB, DNRGB offset 0, or complete indexed WARLS snapshot |
| Hyperion | 19446 | Raw RGB UDP, protocol 0, exact configured LED count |
| TPM2.NET | 65506 | One complete RGB data packet |

```text
rgb-input start ddp --pixels 60
rgb-input start artnet --pixels 60 --universe 0 --channel 1
rgb-input start sacn --pixels 60 --universe 1 --channel 1
rgb-input start wled --pixels 60
rgb-input start hyperion --pixels 60
```

These are alternatives: stop the current input before selecting another.
For xLights or another lighting sender, configure a **unicast** destination at
the PCController host's address, matching protocol, universe, slot and count.
DMX RGB occupies three consecutive slots per pixel; the whole configured strip
must fit within one 512-slot universe. DDP and WLED frames must cover the whole
strip. Fragment assembly, multicast subscription, ArtPoll/ArtSync, sACN sync and
priority merging, RGBW, WLED notifier/JSON API emulation and DDP configuration
queries are not implemented. Do not configure those modes in the sender.

ArtDmx physical-port identity and sACN CID are pinned in addition to the UDP
endpoint. Sequenced DDP/ArtDmx/sACN duplicates and late packets are rejected;
sACN preview and synchronized packets cannot drive the strip. A valid termination
from the established sACN source ends its session without writing new colors.
WLED's remote timeout byte cannot disable PCController's local safety timeout.

Wire references: [Art-Net specification](https://art-net.org.uk/downloads/art-net.pdf),
[WLED protocol definitions](https://github.com/wled/WLED/blob/main/wled00/src/dependencies/e131/ESPAsyncE131.h),
[WLED realtime formats](https://kno.wled.ge/interfaces/udp-realtime/),
and [DDP integration](https://kno.wled.ge/interfaces/ddp/).

## Genuine serial inputs

The host can read a **separate virtual COM pair or PTY**. It does not create or
install a virtual-port driver, and never lends the physical board's serial port
to an RGB application. Configure/create the virtual pair separately: the RGB
application opens one end and PCController opens the other. Ports must differ
from each other and from the board port. The active board's port is rejected.

```text
rgb-input start adalight --pixels 60 --serial COM21 --baud 115200
rgb-input start adalight-openrgb --pixels 60 --serial COM21 --baud 115200
rgb-input start tpm2 --pixels 60 --serial COM21 --baud 115200
rgb-input start openrgb --pixels 60 --serial COM21 --baud 115200
```

Use only one command. In OpenRGB select the **other end** (for example COM22)
and protocol `adalight`, `tpm2`, or `keyboard_visualizer`, matching count/baud.
OpenRGB's current Adalight driver sends **N**, while Adafruit sends **N−1**;
select `adalight-openrgb` for that driver and `adalight` for standard senders.
The distinction is verified in the
[OpenRGB sender source](https://gitlab.com/CalcProgrammer1/OpenRGB/-/blob/master/Controllers/LEDStripController/LEDStripController.cpp).
No firmware is flashed and no bytes are written back to the virtual input.
The bounded parser handles split/concatenated frames and resynchronizes after
garbage; a 100 ms read gap discards an incomplete frame. Baud limits and serial
wire time still limit usable FPS. DTR/RTS are opened deasserted.

## SignalRGB custom network plugin

Source: `Tools/Integrations/SignalRGB/PCController.js`. Host packages include it
under `integrations/SignalRGB/`, alongside this guide at `integrations/RGB-INPUT.md`.
This is an **integration candidate, not yet SignalRGB-runtime verified**.

1. Copy `PCController.js` to SignalRGB's user plugin directory
   (`Documents/WhirlwindFX/Plugins`; use the application's documented custom
   plugin location if Documents is redirected).
2. Edit the `target` constant in that copy: host IP, UDP port and physical pixel
   count. Defaults are localhost, 4048 and 60 pixels. Restart/reload SignalRGB.
3. Start the matching PCController DDP input explicitly.
4. Select **PCController RGB input**, position its LEDs on the SignalRGB layout,
   and enable **Stream RGB**. Disable it to cease sending; the host then expires
   the input lease. Use PCController Stop for immediate listener shutdown.

The plugin registers an explicitly configured endpoint using the service
controller-registration approach, samples SignalRGB canvas colors, and sends
complete sequenced DDP frames at up to 60 FPS through the documented
[UDP socket API](https://docs.signalrgb.com/developer/plugins/advanced-communication/).
Registration is not a host/board health check. It neither spoofs WLED/Corsair
identity nor starts the host receiver, flashes, reconnects or changes outputs
on shutdown. After a host timeout or a SignalRGB restart, start input again.

## Hardware approaches from the design discussion

This host integration retains the board's existing native protocol and strip
capability. The current limit is **100 physical RGB pixels**, not 480, and there
is one exclusively owned strip lane. Increasing that requires board/protocol
capacity work rather than accepting oversized packets and silently truncating.
USB HID/Corsair Lighting Node Pro emulation needs appropriate USB-device MCU
firmware and descriptors; the host application cannot turn an AVR board's
USB-to-UART bridge into that device. No commercial VID/PID is impersonated here.
ESP32-S3/RP2040 firmware, multiple physical strips/zones, MT16703 driving and
electrical power/signal changes remain separate hardware-specific work.

## Ownership and recovery

- Start refuses an occupied strip lane; it never silently replaces an effect.
  Stop the existing strip owner deliberately first.
- The first valid sender endpoint owns the session. Other senders and malformed
  packets cannot change colors or refresh its timeout.
- At most one pending frame is retained. Intermediate frames are coalesced when
  the AVR cannot keep up; it does not replay an accumulated animation backlog.
- Host pixel-order preferences apply once to each complete RGB frame.
- Disconnect/replacement, E-stop, programming, or an unacknowledged frame ends
  the session. Restart explicitly after resolving the cause; no automatic
  reconnection, retry, learning, output restoration, or firmware changes.
- Five seconds of sender silence ends input. `--timeout 1s` through `60s` changes
  that limit. Stop and timeout retain the last acknowledged colors; they do not
  black out the strip or restore an unrelated effect. Use `strip clear` separately
  if you deliberately want darkness.
- `rgb-input stop` waits for its worker to finish and does not stop a newer strip
  owner. Lifecycle/failure events appear in the existing event stream.
- `rgb-input status` reports received, rejected, coalesced and acknowledged frame
  counts plus the most recent failure. Web counters refresh explicitly and on
  lifecycle events; there is no per-frame HTTP polling.
- These RGB protocols provide no cryptographic authentication; some include
  sequence numbers (checked when present). Loopback
  is the default. A LAN binding requires a specific local IP and `--source IP`
  for the trusted sender; do not expose it to untrusted networks. Sender filtering
  is not cryptographic authentication. UDP may lose/reorder packets.
- The listener is runtime-only: host restart leaves it disabled. Existing IPC
  integration authorization governs start/stop; status is read-only.

## Verification boundary

Protocol/admission, serial framing, malformed-packet, source/sequence and fuzz
fixtures accompany the implementation. Tests and CI remain skipped at the user's
request. A successful host/Web build is not an OpenRGB/SignalRGB/xLights session,
virtual-COM interoperability proof or physical strip acceptance. No listener is
started and no hardware is actuated by building or installing this change.
