# Firmware update progress

The updater reports the **current measured stage**, not a guessed percentage of
the entire backup/write/verification/reconnection transaction. Every interface
consumes the same operation status and pushed `update.*` events.

| Signal | Meaning |
| --- | --- |
| `state` | Current activity, or terminal `completed` / `failed` / `cancelled` |
| `stage` | Last observed stage, which may include reconnect/cleanup after an earlier error; the full diagnostic identifies the failure |
| `progress_known` | Whether the current stage has a measured denominator |
| `progress_percent` | Percentage of that stage, meaningful only when known |
| `started_at`, `stage_started_at`, `updated_at` | Real operation, stage, and latest activity timestamps |
| `detail`, `error_code` | Full diagnostic and machine-readable failure reason |

No percentages are assigned to tool resolution, serial release, backup planning,
reconnection or settings restoration. An indeterminate indicator means the stage
is active but its duration is unknown, not that progress has stopped. Completion
is published only after the executor returns successfully. A failed operation
stops the busy indicator and retains its complete diagnostic. A restart never
automatically replays a hardware write.

## AVRDUDE integration

AVRDUDE 8.0 explicitly supports GUI consumers through **unbuffered stderr pipes**.
Its non-terminal renderer emits `Reading | ` / `Writing | ` followed by one `#`
per two percentage points. The final percentage and elapsed time arrive only at
the end of the line. Waiting for a newline therefore hides live progress.
`-` characters represent an unfinished range and must not count as work done.
Terminal-style carriage-return percentage bars are also recognized.

The streaming parser consumes incremental chunks with bounded storage and keeps
the actual process exit status authoritative. A complete write bar is not a
successful whole update: verification, reconnect and restoration may still fail.
ConPTY is unnecessary for this supported output protocol. Linking libavrdude is
not required and is not part of this implementation.

Sources: [AVRDUDE 8.0 progress implementation](https://github.com/avrdudes/avrdude/blob/v8.0/src/term.c#L2908-L2942),
[official output documentation](https://avrdudes.github.io/avrdude/8.0/avrdude_7.html).

## Toolchain preflight

The configuration's `programming.toolchain_config` must reach the Go client,
command engine and programmer; otherwise Arduino CLI may inspect the wrong data
directory. Explicit AVRDUDE executable/config paths remain authoritative.
Tool resolution is checked before entering programming mode or releasing UART.
Reuse the existing Arduino installation; do not install a duplicate toolchain to
hide a configuration-forwarding error.

## UI and Windows integration

The TUI and Web view show the stage, elapsed time, current activity and complete
wrapped failures. Operation identifiers and hashes belong in secondary technical
details. Web progress is driven by pushed events, not one RPC refresh per tick.
Windows taskbar state and terminal progress distinguish measured from
indeterminate work and stop on terminal results. Native notifications honor the
configured preference and announce major stages/results, not every percentage.

Regression coverage includes split AVRDUDE output chunks, failure tails, unknown
durations, stage resets, event transport, configuration forwarding and terminal
failure rendering. Screenshot evidence must distinguish fixture-driven visual QA
from a real hardware write; simulated progress is never hardware acceptance.
