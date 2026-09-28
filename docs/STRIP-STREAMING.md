# Addressable strip streaming

The strip uses D6, WS2811 at 800 kHz in BRG wire order. The default is 100
addressable pixels; `strip config COUNT` selects 1–100 for the current board
session. Power-cycle resets the count to 100. WS2811 products can group several
physical LEDs into one addressable pixel: count the controllable groups.
For a GRB WS2812B strip, compile with `PCCONTROLLER_USE_WS2812B=1`.

| Command | Result |
| --- | --- |
| `strip config 100` | Configure and clear 100 pixels |
| `strip rainbow 100 20` | Start a host-owned rolling rainbow, up to 20 frames/s |
| `strip status` | Report the active host stream |
| `strip stop` | Cancel streaming and keep the final displayed frame |
| `strip clear` | Stop streaming and turn every configured pixel off |
| `strip fill 255 0 0` | Stop streaming and display full red |
| `strip pixel 99 0 0 255` | Set zero-based pixel 99 blue |
| `strip config 3` then `strip frame FF000000FF000000FF` | Display exact red, green, blue pixels |

Run these commands through the controller CLI, the TUI command input, scripting,
IPC, or the shared API command executor. The Web peripheral workbench has count,
frame rate, rainbow, stop, clear, RGB-frame and single-pixel controls.
RGB frame text has six hex digits per configured pixel in red/green/blue order.
Brightness 255 preserves each RGB byte exactly; lower single-pixel/fill brightness
is scaled once on the host. Raw `strip frame` bytes are never scaled.

Frames are sent as acknowledged staging chunks of at most 15 RGB pixels, followed
by one show command. A failed chunk aborts transmission without showing a partial
frame. The scheduler serializes complete frames, replaces old streams, observes
cancellation, and stops on transport failure. Slow links skip animation frames;
they do not accumulate a queue of stale colors. Configuring a shorter strip first
clears the previous tail. Frame length must not exceed the configured count.

## AVR timing and storage limits

The 100-pixel wire buffer is 300 bytes. It shares storage with the MCU macro ring;
retained MCU recordings must be saved/exported and cleared before strip use.
Strip transmission is rejected while a precise MCU recording or playback is
active. This protects the macro timeline and retained recording rather than
silently overwriting it.

The AVR sender masks interrupts for about 30 microseconds per pixel (3 ms at 100
pixels), then waits for the LED latch. During that window the MCU cannot capture
RF edges and Timer0 may lose overflow ticks. Therefore simultaneous strip
streaming and precise external-event recording are not supported by this
bit-banged target. Even standalone streaming can make the MCU uptime lag wall
time. A DMA-capable MCU or a separate strip controller is needed to remove this
hardware constraint; the host animation uses its monotonic clock. The firmware
ACK is emitted after show, preventing the next host frame from overflowing UART
while interrupts are masked. Unsolicited serial traffic must honor this pacing.

The 100-pixel limit is intentional on the AVR SRAM budget. Higher counts require
a firmware target/profile with enough memory and a matching advertised limit;
the UI and native encoder reject counts above this build's maximum.
