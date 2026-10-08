# Media publishing authority

PCController owns one media-clock and prepared-timeline publisher. Connection,
monitoring and publishing are separate concepts. No second serial owner is
created by a monitoring client.

Register an identity through `controller.app.instance.report`, then use
`controller.media.authority.get` and `controller.media.authority.change`.
The change parameters are `client_id`, `operation`, and optional `requester_id`.
Operations are `request`, `accept`, `reject`, `release`, `lock`, and `unlock`.
The response contains `owner_id`, `owner_label`, `exclusive`, `revision`, and
`pending` requests. Labels come from registered client information, not a claim.

- An unowned publisher may be acquired; otherwise a request waits for owner consent.
- Accept/release require paused or expired media playback. Cancellation completes
  before transfer; an existing hardware plan also requires acknowledged safe cleanup.
- Preparing successfully reserves ownership before the first media-clock update.
- Nonexclusive ownership expires with the existing three-second clock lease.
- Production lock does not expire with that lease. Its stable owner unlocks it.
- Normal observer controls are allowed when the prepared timeline is idle. During
  playback the existing scheduler exclusion still prevents competing output commands.
- Production lock rejects foreign live output actions, including high-level actions
  and autonomous MCU-program admission. Reads remain available. E-STOP activation
  remains available; a foreign client cannot release it during production.

Current scope: authority is runtime-resident and resets when PCController restarts.
Client IDs are operational attribution, not credentials. Existing authentication
and access policy remain the security boundary. Do not expose an unauthenticated
controller to an untrusted network.

Denials have JSON-RPC code `-32009`, with `data.kind` (`authority_conflict` or
`authority_locked`), `data.resource = media_authority`, and current `data.authority`.
The C ABI preserves this JSON-RPC detail as `rpc_error` rather than flattening it.
The `media.authority` event announces successful changes; clients pull the current
authority to reconcile missed events.

Status sampling errors now include authoritative `board_connected`. A sampling
timeout alone must not be displayed as a physical unplug or coordinator disconnect.

Verification: focused control and IPC tests cover owner-consented paused handoff,
exclusive reservation after lease expiry, anonymous-output rejection, E-STOP safe
cleanup, prepared-plan reservation, expiry, snapshot isolation and typed RPC denials.
An existing asynchronous timeline fixture once observed its log before the second
acknowledged edge was copied; a repeated focused run passed. Live deployment and
physical timing are separate acceptance gates.
