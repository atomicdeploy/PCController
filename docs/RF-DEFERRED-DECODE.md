# Deferred rc-switch decoding

`LocalLib/RCSwitch.{h,cpp}` vendors rc-switch 2.6.4 from the installed pinned
Arduino library. Original attribution and LGPL-2.1-or-later notices are retained;
this distribution selects the LGPL-3.0-or-later option supplied in
`LICENSES/LGPL-3.0-or-later.txt`.

Upstream decodes all protocol candidates inside INT0. At 115200 baud, a UART
character arrives roughly every 87 microseconds. Protocol division/matching in
that interrupt can prevent UART receive servicing and corrupt framed commands.

The local change captures a completed pulse buffer in INT0, then decodes it from
`available()` in the normal loop. A one-byte pending count owns the existing
buffer until decoding completes. Edges during that ownership are dropped; the
next repeated frame resynchronizes at a gap. No nested interrupts or second
large buffer are introduced. Enabling receive discards an obsolete pending frame.
The native virtual-board boundary remains unchanged.

Run `node Tools/Firmware/verify-rf-deferred.mjs`. Its Arduino fixture rejects any
protocol-table access from the ISR and verifies delayed decoding, pending-buffer
integrity, waveform identity and receive rearming. The final AVR waveform and
flash/SRAM guards remain required.

Bench investigation is ongoing: the original 38-pixel run rejected a staged
packet after 498 acknowledged frames, increasing framing errors by one without
CRC errors or resets. The first deferred-decoding run acknowledged 588 frames
with zero framing/CRC errors, then hit a host request deadline. Longer serial and
host scheduling tests are required before claiming full resolution. A subsequent
180-second, 38-pixel chase acknowledged 2,442 of 2,443 attempted frames, with no
framing errors, CRC errors, or resets. One HTTP client request exceeded its
five-second deadline; its physical outcome is unknown. Later animation frames
continued without replaying that request. This is evidence for improved UART
reception, not proof that all host latency issues are resolved. Temporary
UART tracing is local bench instrumentation and must be removed before final
production delivery. No physical RF remote acceptance is claimed by the fixture.
