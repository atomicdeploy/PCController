# Bounded PWM writer diagnostics

Owning issue: #672. This is diagnostic instrumentation, not a claimed repair of
the reported 100% shutdown/transient dropouts.

| Operation | Existing shared RPC/CLI path |
| --- | --- |
| Capture one channel for 15 seconds | `controller.pwm.trace.start {"channel":1,"duration_ms":15000}` / `pwm trace start 1 15000` |
| Read bounded receipts | `controller.pwm.trace.get {}` / `pwm trace get` |
| Stop, retain receipts | `controller.pwm.trace.stop {}` / `pwm trace stop` |

CLI/TUI console and scripting/bridge/API consumers share these host operations.
Start/get/stop issue no UART operations, output changes, client disconnections,
mutation retries or firmware writes. Capture is off by default, expires without
a polling timer, requires explicit 1..60000 ms duration, and retains only the
latest 64 receipts with a dropped count. New captures fence late responses from
earlier captures. There is no disk log or credential/auth capture.

The central generation-pinned command path records only valid successful
`PWM_SET` acknowledgements for the selected channel, after existing admission,
response and owner fences. Records retain exact requested raw duty, registered
RPC actor (when supplied), method/transport, background/unknown source, host time,
original connection generation and optional real MCU acknowledgement timestamp.
`capture_id` fences separate diagnostic sessions; event opcode is the acknowledged
command so existing opcode filters can select it. An actor is a caller-reported
registered identifier, not authentication or proof of a physical cause.
`pwm.write.ack` is a DEBUG stream event, never normal activity or a default toast.
Failed/canceled/unconfirmed requests are not claimed acknowledged. ACK confirms
command acceptance, not physical duty or absence of a subsequent writer.

Unknown actor does not establish a human, remote client or board cause. Firmware
RF/front-panel/autonomous writes, raw UART bypasses and other serial owners are
not attributed by this host-command trace. Correlate receipts with authoritative
logical readback, safe pointer-only expander register reads, board uptime/reset
and real physical observation. Preserve unrelated outputs and mappings; do not
invert polarity, reset/reinitialize the expander or disable peers on a guess.
