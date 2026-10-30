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

## Bench checkpoint

The temporary tracing was removed for firmware identity `5B86029E`. The final
build uses 32,334 flash bytes and 1,571 static SRAM bytes; the existing final-LTO
serial-response-plus-INT0 guard estimates 269 bytes of margin. The linked strip
waveform fixture passes all 256 byte values, byte boundaries and a 300-byte frame.
The programmer's immediate verify passed, but its separate readback initially
failed to synchronize. A read-only retry succeeded. Guarded recovery then verified
32,346 written bytes including the identity section, restored and verified EEPROM
settings, cleared its recovery marker and released the programming latch.

Pealayer's individual-frame path was also exercised for 60 seconds before that
final upload: 166 frame requests were accepted, with 105 waits for its pending
operation to finish. No new board framing/CRC errors, resets or Pealayer clock
failures were observed. API acceptance is not an optical acknowledgement. This
path is suitable for manual diagnostics; smooth playback needs the host streaming
engine rather than frequent individual UI operations.

The host connection and Pealayer automatic connection were restored after
recovery. A fresh 38-pixel configuration was acknowledged. Playback remains
paused and standalone chase/rainbow streaming is stopped so the separate playback
investigation owns effects. Long streaming validation on the final trace-free
image, the isolated host deadline, physical RF reception, synchronized playback,
the three effect demonstrations and seat stress acceptance remain open. The next
action is to reproduce the reported playback stop at its effect boundary with
the recovered host available, recording new error deltas rather than historical
maintenance disconnects. This checkpoint does not claim those acceptance gates.

CI also exposed a flash overflow in the optional EEPROM menu-label profile.
Protocol and pulse indices now use bytes (12 protocols, at most 67 captured
durations), with a compile-time bound on buffer size. The regression fixture also
covers a full 32-bit identity, capture overflow and resynchronization. The optional
profile now builds at 32,314 flash bytes with 270 bytes of estimated stack margin;
its waveform and default-EEPROM contract checks pass. This size optimization is
source-validated but has not replaced the running `5B86029E` image, because live
playback investigation owns the board after the recovery handoff.
