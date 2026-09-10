# Peer executable update contract

Related GitHub records: #252 (contract), #272 (preserved predecessor),
#110 (live replacement acceptance), #297 (tolerant integration).

| Boundary | Contract |
|---|---|
| Source intent | Each logical update has a caller-generated retry identity. CLI generates and prints it on failure; Web retains it across uncertain replies/reloads. Reuse exactly that identity after an uncertain response. |
| Transfer | Verified executable identity, platform, transfer ID, total bytes and exact next offset must agree before progress advances. Optional unrelated response metadata is accepted. |
| Partial storage | Reserve bounded capacity before opening a file; at most eight transfers and 512 MiB reserved. Idle partials expire after 15 minutes, are checked at every operation and cleaned periodically. Startup removes crashed orphan partials. Close waits for reserved allocation/finalization safely. |
| Topology | Bridge ingress cannot call another peer or persist topology/automation configuration that later creates a third-peer hop. This remains independent of dormant alpha authentication. |
| Replies | JSON-RPC identity and result/error exclusivity are mandatory. Matching-ID malformed acknowledgements are outcome-uncertain, never success. Unknown optional fields are tolerated. |
| Shared state | `peer-update.*` events carry peer, logical intent, operation identity and progress separately from local `update.*` state. All subscribed clients receive them. |
| Outcome | `remote-queued`/`remote-staged` means acceptance by the target coordinator only. It never proves restart health, successful rollback, reconnect or which executable is running. |

## CLI retry

```text
peer-update host PEER ARTIFACT_SHA256
peer-update host PEER ARTIFACT_SHA256 IDEMPOTENCY_KEY
```

The first form generates a logical intent. If the reply is uncertain, the error
includes the exact retry command. API callers use `controller.peer.update.host`
with `peer`, `artifact_sha256`, `authorized: true`, and `idempotency_key`.
Authorization here is explicit update consent, not an application login.
Application authentication remains disabled per #148; listener exposure,
configured browser Origins, topology and programming safety remain enforced.

## Reconciliation and remaining acceptance

The old #272 aggregate is preserved, not merged wholesale. Its transfer,
retry, bridge-state and UI slices are being extracted onto current main;
current-main alpha authentication and newer buzzer scheduling are retained.
The obsolete generated Web/API files from that branch are not restored.
Regenerate outputs from the integrated source before delivery.

No live peer replacement, physical board operation or deployment is performed
by these software tests. #110 still owns multi-instance candidate health,
reconnect, active executable identity and rollback acceptance. A merge of the
software contract must not close that physical/integration evidence gap.
