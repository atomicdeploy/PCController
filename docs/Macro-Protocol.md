# MCU macro protocol

The MCU stores one bounded, volatile 128-byte ring of raw ordinary protocol
records. A record is `[due_us LE32][opcode][payload_length][payload]`.
Names and categories remain host-library metadata; the MCU neither stores nor
interprets them.

Schema 3 adds capture while retaining the record shape. `MacroStart` is
`[3,id,flags,total_steps LE16]`; flag bit 1 starts MCU-timed capture. A
capturing board records only an ordinary command that already passed the same
validation and safety checks used by live host, front-panel, or learned-RF
input. Capture can therefore include relay, side-motion, PWM/MOSFET, buzzer,
display/message and LED operations without a parallel action vocabulary.

`MacroStep` selectors: `0` append, `1` play, `2` status, `3` clear, `4` fetch
(`[4,offset LE16,max]`), `5` finish capture. Fetch replies with
`[EventMacro,3,Exported,id,offset LE16,count,raw-record-bytes]`. Mutating
commands ACK; status, events, and fetch use `MacroStatusResponse`/`Event`.

State values are fixed: Idle 0, Buffering 1, Playing 2, Completed 3,
Cancelled 4, Failed 5, Recording 6, Captured 7, Exported 8. Cancellation of
playback requests the existing safe output stop; cancellation of capture does
not change live outputs. The scheduler releases at most one action per normal
service pass, and macro replay is marked with the reserved execution sequence
so it cannot recursively capture itself.
