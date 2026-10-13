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
