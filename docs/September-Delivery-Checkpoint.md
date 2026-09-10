# September delivery integration

Request: reconcile interrupted work and conflicts, merge completed changes,
complete usable recording/playback, and deliver the current host, Web UI and
board firmware. Source-only completion is not deployment evidence.

## Reconciled lanes

| Scope | Integration | Verification and remaining gate |
|---|---|---|
| Dependencies | PRs #301 and #302 merged | Canonical lock, Web typecheck and 208 tests passed |
| Typed actions and remote compiler progress | PR #303 preserves #274/#275 histories | Focused Go, Web, API and generated asset checks passed; exact integrated delivery pending |
| Host macro recording and playback | PR #304 | Named mixed-opcode recording, synchronized Web/TUI state, failure retention, cancellation and original-session binding; focused macro/TUI checks passed |
| Peer update contracts | PR #305 reconciles #272 | Bounded uploads, checked receipts, safe retry/staging, Origin and bridge routing guards; 225 Web tests and focused Go checks passed |
| Windows serial-owner helper | PR #306 | Independent two-second child lifetime, including an uncooperative native scan; focused regression checks passed |
| Firmware | Current project-owned build | Flash and SRAM gates passed; UART delivery attempt stopped before any write because the port was busy |

The final integration uses normal merge ancestry and regenerates shared API
references and embedded Web assets. It does not choose an old generated bundle
to resolve conflicts. Authentication remains disabled; misleading authentication
copy in the update view was corrected without resuming the deferred login work.

## Acceptance that must not be silently closed

- Issues #44/#74: precise MCU timing, physical/RF-origin recording, offline
  circular recovery and loaded-motion acceptance are distinct from the default
  host-command prototype (100 ms tolerance). See [Host macro recording](Host-Macro-Recording.md).
- Issues #110/#252: real peer process replacement, rollback, reconnect and active
  image identity require a second live installation, not merely a staged ACK.
- Issues #154/#155/#216: the failed UART attempt reported access denied and did
  not write firmware. Port ownership, exact settings recovery, fresh HELLO and
  clearing the programming latch must be verified before claiming board delivery.
  The stale helper mechanism is fixed, but it alone does not prove the cause of
  every busy-port failure. USBasp is not assumed present.
- Issue #216: UART-first/capability-aware backup policy and removal of the old
  incomplete-backup switch remain unfinished in current code/docs.
- Issues #64/#75: stamped executable identity, current embedded Web, installation,
  startup, multi-client state and live record/save/play/cancel evidence remain
  explicit final deployment gates.

All other unresolved requirements remain in the [canonical backlog](Requirements-Backlog.md)
and [chronological delivery ledger](Alpha-Delivery-Ledger.md). Local source moves,
private configuration and raw diagnostic logs are deliberately not published.
