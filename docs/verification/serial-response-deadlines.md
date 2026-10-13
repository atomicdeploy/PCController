# Serial admission and PWM response budgets

The reported GET_STATUS (0x02) expired at 1500.8 ms behind an ADDRESSABLE_LED
(0x16) request registered for 1550.4 ms. The strip request formerly allowed a
two-second response wait. It now has a 750 ms response budget after admission;
queued strip packets yield to ordinary status/control requests, while prepared
timeline work keeps its highest priority and shorter existing deadline. Nothing
interrupts or retries an in-flight mutation. Three consecutive missing strip
frame ACKs terminate the stream with unconfirmed-output evidence instead of
occupying the UART indefinitely. Isolated transient drops still render from the
current animation time, never replay an old frame.

PWM RPC formerly inherited the 15-second server budget, while Pealayer waits
three seconds on its ordinary control socket. Mutation and all-channel readback
now share one two-second context across TCP/WebSocket/UDP JSON-RPC and HTTP.
Read-only PWM queries are also bounded. No mutation retry is added. A mutation
failure does not issue a following read. An ACK followed by failed readback is
reported as `confirmation_unavailable`, resource `pwm`, `write_acknowledged=true`,
`retryable=false`, not a fake write rejection or invented board values. HTTP and
RPC share the lock/safety gates and deadlines. Pealayer keeps a healthy socket
for this typed server rejection, instead of pausing on a false TCP timeout.

The paired Pealayer change restores its short socket timeout on every preparation
failure path and gives late/unrelated replies one absolute request budget.
Transport failures still pause/discard the failed socket and never replay the
original mutation. Existing 50 ms execution and 250 ms freshness guards remain
unchanged; no firmware, remote-access service or authorization change is needed.

Added fake-port/pure regressions for strip timeout/priority, repeated frame failure,
shared PWM deadlines and acknowledged-write/failed-readback distinctions. Suites
and CI are skipped under the user's instruction. One staged host build completed
in 30.3 seconds with selected native/Web/package checks. Package
`.build/package/host-136280` has host source digest
`d4e7a7b87601ef5328498542527dc03e90c8048308eb3f561e85c4e97fe4fe92`
and executable SHA-256
`37e4ccbf511bc30cac0dcbafaa38a4a55ffad5d02e187a499fddcb9e58109342`.
This package preserves the already deployed development lineage; it is not a
package of the isolated main-based PR tree. Actual physical failure injection
and PWM changes are not authorized merely to reproduce these errors.

## Cafe delivery, 13 October 2026

Primary-owned update `op-091610e2d50112ba` completed with
`terminal_verified=true`; the installed executable hash matches the package.
The controller configuration remained semantically identical to private backup
`verification/serial-response-deadlines/config.json` (SHA-256
`82a8fd03af13add569555f741905a9d91cb650ce95d133e5e71215adcfbedba5`).
Three live GET_STATUS calls and three full PWM reads over the same 3-second
NDJSON TCP transport returned in 19.1/8.4/8.8/11.8/13.5/23.5 ms respectively,
without errors. The first diagnostic writer used a BOM and was rejected as
invalid input; the corrected UTF-8-without-BOM probe matches native wire output.
No physical output was changed. Pealayer remained paused and hardware/controller
connected, without hardware/sync error, at matching desired/prepared/clock ACK 88.

Source checkpoint `db407fce` was isolated onto main as `969969ca` for
[PR #659](https://github.com/atomicdeploy/PCController/pull/659), preserving
concurrent mobile work without switching the shared checkout or creating a
worktree. Protected branch checks block merging; normal auto-merge is enabled,
with no policy override or CI run. Generated Web output is preserved in stash
`853e1a3616eb22203818ef60dd43f98d76df1e53`.

The paired [Pealayer PR #176](https://github.com/ToghrolTP/pealayer/pull/176)
has a compiled/smoke-verified candidate. Its runtime replacement is held for a
current Cafe draft-state confirmation. Successful read-only probes do not prove
physical PWM gesture acceptance, injected ACK-loss recovery or a playback soak.
