# September delivery integration

Request: reconcile interrupted work and conflicts, merge completed changes,
complete usable recording/playback, and deliver the current host, Web UI and
board firmware. Source-only completion is not deployment evidence.

## Reconciled lanes

| Scope | Integration | Verification and remaining gate |
|---|---|---|
| Dependencies | PRs #301 and #302 merged | Canonical lock, Web typecheck and 208 tests passed |
| Typed actions and remote compiler progress | PR #303, merged through #307; preserves #274/#275 histories | Included in installed canonical CI644 package |
| Host macro recording and playback | PR #304, merged through #307 | Live CLI and Web record/save/play/monitor verified; first-ACK timing correction remains tracked in #44 |
| Peer update contracts | PR #305, merged through #307; supersedes #272 | Bounded uploads, checked receipts, safe retry/staging, Origin and bridge guards; real second-installation acceptance remains open |
| Windows serial-owner helper | PR #306, merged through #307 | Independent two-second child lifetime, including an uncooperative native scan; regression checks passed |
| Native configuration watcher | PR #309, merged | Bounded retry for transient missing paths during watcher registration; full affected configuration suite passed |
| Browser resource identity on reconnect | PR #310 | 230 Web tests, typecheck and production assets passed; live replacement acceptance still required |
| Firmware | Current project-owned build | Flash/SRAM gates passed; two UART attempts stopped before any flash write; existing firmware recovered and operational |

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
  not write firmware. A separate fresh attempt opened the port but timed out
  during the Urboot handshake, also before writing. Project-owned DTR reset received a fresh HELLO;
  `program abandon` restored and verified original EEPROM/live settings, cleared
  the recovery marker and left `programming_latch=false`. This recovers operation,
  but does not deliver the new firmware. Repeating the CLI upload returned its
  old failed idempotent operation; explicit safe retry remains to be addressed.
  The stale helper mechanism is fixed, but it alone does not prove the cause of
  every busy-port failure. USBasp is not assumed present.
- Issue #216 / PR #308: explicit development-upload policy skips raw archival
  capture while retaining settings/recovery and write-safety checks. Retained
  artifact reuse, known-good promotion and portable EEPROM history remain separate.
- Issues #64/#75: installed CI644 passed canonical Windows tests, vet, icon/version
  resources, current embedded Web, UPX integrity and C ABI smoke validation.
  Installation reports healthy with user data preserved. Do not equate that
  installed version with newer PRs until their packages are deployed and verified.

## Live host-macro evidence

Only display commands and an already-off relay state were used. No loaded motor,
relay-on, incoming RF or physical-key acceptance is claimed.

| Check | Observed result |
|---|---|
| CLI record, save, rename and playback | Three-step `delivery-verified` take persisted; completed with 15.733 ms maximum ACK timing error after redundant compiler load was removed |
| Web record and save with CLI-origin commands | Existing browser received the count and shared library entry without manual refresh |
| Web playback | Three steps completed, but the first ACK was 539.722 ms late; retained as a failure of the 100 ms target |
| Same short take via CLI | Completed with 78.715 ms maximum ACK timing error and zero reported violations |
| CLI playback followed by `macro cancel keep` | Existing Web client showed playing then cancelled at 2/3 without refresh; no dispatch error; outputs deliberately preserved |

The first slow Web ACK exposed a scheduler-origin mismatch: recording measures
relative to the first ACK, but playback originally used a pre-dispatch epoch.
That compressed the next recorded gap by 535.064 ms in this run. The correction
must anchor once to the first successful ACK while retaining the startup delay
in timing evidence; later overruns must not be hidden by repeated re-anchoring.
Host ACK measurements are not MCU actuation timestamps or a hard real-time bound.

An old browser also reproduced a missing lazy asset after external host
replacement. One controlled reload restored CI644. PR #310 checks identity on
ordinary socket reconnects; it cannot retroactively change JavaScript already
loaded by a pre-fix browser.

## Refined firmware checkpoint policy

The operator clarified that a live board can run disposable development builds.
Production status belongs to a known-good firmware checkpoint, not automatically
to every image installed on that board. Successful flashing alone must not
promote a build to known-good; defined acceptance or explicit operator confirmation
is required.

| Evidence | Intended optimized behavior | Current delivery status |
|---|---|---|
| Exact prior firmware retained and reliably matched to device | Reuse the verified artifact instead of reading identical flash again | Tracked in #216; not yet implemented |
| Mutable settings | Capture semantic settings with firmware/layout identity; restore and verify after update | Existing guarded programming captures settings; durable portable JSON/history needs completion |
| Raw EEPROM required for rollback | Pair with exact firmware/layout, content-deduplicate and apply retention | Complete capability-aware strategy remains in #216 |
| Explicit development iteration | Skip new archival raw backup, preserve settings and write-safety checks | Separate implementation lane underway |
| Missing or ambiguous prior identity | Read back or request explicit disposition; never claim unverified rollback | Conservative fallback remains required |

Reuse the existing Go `firmware identity` / `firmware patch-identity` tooling and
evaluate Urboot metadata. A short build hash alone is not proof of exact artifact
identity: use the retained full digest and validated identity manifest. Keep
protected known-good checkpoints while removing duplicate content, not useful
history. Do not interpret this policy as permission to bypass target verification
or erase settings.

All other unresolved requirements remain in the [canonical backlog](Requirements-Backlog.md)
and [chronological delivery ledger](Alpha-Delivery-Ledger.md). Local source moves,
private configuration and raw diagnostic logs are deliberately not published.
