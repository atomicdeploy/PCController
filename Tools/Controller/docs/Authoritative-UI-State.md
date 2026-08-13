# Authoritative UI state

Controller values are push-authoritative. A UI may render a board key and
value only after a live host stream has supplied a connected snapshot with a
STATUS report and the board has advertised the corresponding HELLO capability.
An absent capability means the row or control is absent; zero is never used to
infer support.

An advertised key whose fetch is still in progress may show a bounded loading
state without a value. Once fetched, readings must pass their native type and
supported-range validation before formatting. Invalid measurements, including
the DS18B20 `-32768` sentinel and invalid INA219 sentinel/range values, are
reported as unavailable without exposing the numeric sentinel. On stream loss,
the WebUI clears board identity, status, telemetry samples, and event history;
it does not relabel stale content as last-known state.

Automatic reconnect is enabled by default. Each real attempt is identified as
`connecting`; failed attempts wait with exponential jittered backoff capped at
12 seconds. The top-bar `DISCONNECTED` state is a control: activating it cancels
the pending wait and starts an immediate attempt. Both paths keep board values
cleared until a fresh open stream supplies authoritative status.

Optional board features use HELLO capability bits as their presence contract.
STATUS bits 0..3 further describe per-sample INA219, PWM, and temperature
availability. HELLO bit 11 advertises the wired BT Audio indicator; clients
must not show its state unless that bit is present.

These rules apply equally to WebUI, TUI, CLI summaries, discovery documents,
and any future GUI. Permission-aware availability is a separate deferred
design: current alpha auth/authZ remains disabled under #148.
