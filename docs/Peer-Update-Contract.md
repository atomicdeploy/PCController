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
retry, bridge-state and UI slices are extracted onto current main;
current-main alpha authentication and newer buzzer scheduling are retained.
The obsolete generated Web/API files from that branch are not restored.
Regenerate outputs from the integrated source before delivery.

| Predecessor work | Reconciliation |
|---|---|
| Upload reservation, TTL, startup cleanup, Close/Finish serialization | Retained with deterministic tests. |
| Transfer/staging ACKs, idempotency, truthful progress | Retained across service, CLI and Web; optional presentation progress is not an integrity gate. |
| Immediate/wrapped/persisted peer pivots | Canonical bridge guard; harmless non-routing configuration remains usable. |
| State-stream buzzer delivery | Both JSON-RPC and Socket.IO subscribe to state; current native timeline implementation is retained, not replaced by the older scheduler. |
| Alpha credentials/discovery | Current disabled-alpha constructors and newer telemetry retained; dormant proof advertisements removed. |
| Origin protection | Exact configured host/port identity; attacker-controlled Host does not grant browser authority. |
| Web transports | Current stream RPC retained with strict reply correlation and stale-socket guards; uncertain retry keys also work without browser storage. |
| Old generated artifacts | Regenerated from reconciled source; no stale bundles or metadata restored. |
| Older macOS test failure | Short deterministic semaphore test identity fits Darwin limits; cross-platform CI verifies it. |

Origin entries use `HOST:PORT` or `HOST:*`, without a URL scheme. Include each
intended LAN hostname/address explicitly. Native clients that omit Origin
still use the listener exposure policy; application login remains dormant.

No live peer replacement, physical board operation or deployment is performed
by these software tests. #110 still owns multi-instance candidate health,
reconnect, active executable identity and rollback acceptance. A merge of the
software contract must not close that physical/integration evidence gap.
