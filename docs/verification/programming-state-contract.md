# Programming state versus connection loss

`controller.snapshot.connected` remains the application's UART-session fact,
not a claim of physical USB presence. A programming handoff must not falsify it.
The public snapshot now includes a separate `programming` object:

| Field | Meaning |
| --- | --- |
| `active`, `phase` | An observed guarded lifecycle phase; host completion/failure stops active work, not necessarily restoration |
| `restoration_pending` | The lifecycle is not fully restored/completed |
| `intentional_uart_release` | This guarded lifecycle deliberately closed its application UART |
| `latch_known`, `programming_latch` | Verified settings knowledge and bit 0x02; an unknown historical value is not a clear/active-latch claim |
| `device` | Retained programming device identity: port, VID/PID, serial/instance identity and optional friendly name |
| `connection_generation` | The generation associated with the observation |
| `revision`, `updated_at` | Monotonic host-process ordering and last changed observation; unchanged reads do not churn either |

`programming.lifecycle` events carry the same typed `programming` object and
trigger a low-rate, host-identified `controller.snapshot` notification. Settings
changes already trigger a snapshot. Late subscribers therefore hydrate current
state without parsing lifecycle prose, error messages, or update-stage guesses.

Verified latch knowledge survives only a deliberate guarded UART handoff for
the same device. Unexpected transport loss clears knowledge without inventing
that EEPROM was cleared. Attach of another device resets the old lifecycle;
fresh authenticated settings replace prior knowledge. A host restart starts
unknown until normal authenticated read/recovery; it does not assume the old
operation succeeded or replay outputs. No new polling, EEPROM write, latch
release, firmware command, or per-frame persistence is introduced by this state.

The lifecycle publishes explicit completion only after normal readback/restore
and recovery-marker removal. Host programmer completion alone is not restoration.
Existing programming safety and recovery operations retain their authority.

Pealayer's paired change consumes the typed snapshots, distinguishes programming,
latched/recovery, temporary board unavailability and genuine coordinator loss,
and retains domain-rejected command connections without retrying mutations.
Both current packages must be delivered together; there are no legacy aliases.

## Verification and delivery boundary

Stable-path focused and full 45-package Go verification passed; `go vet ./...`
passed. Fixtures cover verified-latch retention, unexpected-loss knowledge,
unchanged observation revisions, restoration versus active work, and matching
typed public snapshot/event serialization without physical hardware.

These checks do not establish installed programming-latch acceptance. No AVR
programming, reset, settings change, latch release, output actuation or service
replacement was performed to test presentation. The live read-only baseline
reported an authenticated session and settings flag 0, not an active latch.
Pealayer's existing local/Cafe launch/package gates remain explicit. Source
alignment is not paired runtime delivery; use approved host-compatible updaters
and verify real latch/handoff/disconnect states when safe, without clearing a
safety latch merely to make a status label green.
