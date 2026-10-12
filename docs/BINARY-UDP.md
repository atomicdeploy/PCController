# Binary UDP between PCController and Pealayer

This is a host transport, not an alternative firmware implementation or a raw
UART bypass. Both apps dispatch the same typed RPC methods, errors, authority
checks and board admission rules used by their existing APIs. Firmware remains
unchanged. Discovery does not provision a command target or grant authority.

## Provisioning

UDP is disabled by default. PCController stores the following in `ipc.udp`;
Pealayer stores the same object in `binary_udp`:

```json
{
  "enabled": true,
  "listen": "127.0.0.1:8787",
  "discovery": false,
  "broadcast_events": false,
  "peers": { "player": "127.0.0.1:8080" }
}
```

Use the destination's actual address and port. UDP and TCP can use the same
numeric port without conflict. Listening and named peers currently use literal
IPv4 addresses. LAN discovery requires a LAN or wildcard bind; PCController
also requires its existing `ipc.allow_remote`. Never enable state broadcast on
an untrusted network: CRC detects corruption, not malicious senders. The current
alpha authentication contract is unchanged. No token is advertised and discovery
is not authenticated. Remote-access/firewall/tunnel services are not modified.

PCController's Advanced network panel manages transport settings and named peers.
Pealayer's shared native/Web Preferences includes the Binary UDP group. Named
peers can be supplied through the complete typed UDP configuration API. Changing
UDP settings closes the previous endpoint and starts a new process identity;
clients reconnect and refresh rather than following stale commands.

Both dispatchers expose these method suffixes:

| PCController prefix `controller.udp.` / Pealayer prefix `pealayer.udp.` | Meaning |
| --- | --- |
| `config.get`, `config.set` | Read/provision the transport configuration |
| `status` | Running state, bind error, process identity, discovered peers, event count |
| `call` | `{peer, request: {jsonrpc:"2.0", id, method, params}, timeout_ms}` |
| `subscribe`, `unsubscribe` | `{peer, timeout_ms}`; receive remote state/event pushes |
| `events.get` | Bounded received observations, sequence and refresh-required state |

Provisioning and outbound routing require local IPC/loopback; they are not
remote bridge pivots. PCController exposes these through its normal IPC/HTTP/WS
dispatcher. Pealayer exposes them through native IPC, `/api/rpc`, `/ws`, and the
process-local `/api/udp` endpoint, even in consumer mode. Its configuration write
returns `accepted:true` when queued; inspect config/status to confirm application.

For example, a local PCController RPC call to the configured player is:

```json
{"jsonrpc":"2.0","id":1,"method":"controller.udp.call","params":{"peer":"player","request":{"jsonrpc":"2.0","id":2,"method":"pealayer.status","params":{}}}}
```

Commands are asynchronous correlated RPCs delivered on packet receipt, not a
polling command loop. Existing board/menu/firmware/automation APIs retain their
semantics. Use their existing bounded chunk APIs for large uploads, not one
oversize UDP message. Subscription leases last 30 seconds; renew before expiry
(the native Pealayer bridge renews every 10 seconds). No event is automatically
converted to a hardware command or rebroadcast, preventing peer echo loops.

Pealayer's hardware endpoint accepts `udp://IP:port`. It uses binary RPC and
pushed controller events/snapshots, including the existing acknowledged front
panel action path. The Go RPC client also accepts `rpc.TransportUDP`; retain a
dialled client per session and call `Close` afterward.

## Wire contract

One datagram is `COBS(raw) + 00`, reusing the production firmware's COBS and
CRC-8/ATM implementation (polynomial 07, initial 00). The host envelope does not
use firmware opcodes or the firmware's 48-byte payload limit.

| Raw offset | Bytes | Meaning |
| --- | --- | --- |
| 0 | 4 | ASCII `DGLU` |
| 4 | 1 | Kind: discover 1, announce 2, request 3, response 4, subscribe 5, event 6, unsubscribe 7 |
| 5 | 16 | Random sender process/session UUID bytes |
| 21 | 8 | Little-endian nonzero message/correlation ID |
| 29 | 2 | Little-endian zero-based fragment index |
| 31 | 2 | Little-endian fragment count, 1..256 |
| 33 | 0..1024 | Binary-value chunk; all non-final chunks are exactly 1024 bytes |
| last | 1 | CRC of every preceding raw byte |

Maximum assembled message is 256 KiB, nesting 32. Datagram size stays below
1100 bytes, avoiding ordinary Ethernet IP fragmentation. Reassembly is keyed by
source address, UUID, ID and kind, bounded to 16 messages, and expires after
three seconds. Inconsistent fragments, bad CRC/length/UTF-8, duplicate object
keys, trailing bytes and nonfinite floats are rejected.

Binary value tags are 0 null, 1 false, 2 true, 3 signed i64 LE, 4 unsigned u64
LE, 5 IEEE-754 f64 LE, 6 UTF-8 string (u32 LE byte length), 7 array (u32 LE
element count), 8 object (u32 LE entry count, then string key/value pairs).
Object keys are encoded in sorted order. Integers are never routed through a
floating-point intermediary. JSON is used only inside the application adapter,
not on the UDP wire.

Discover sends an empty object; announce carries `id` (sender UUID as 32 hex
characters), `identity`, `port`, `transport`, `commands` and `max_message`.
Announcements use `239.255.87.87:8790` and IPv4 broadcast every 15 seconds;
discover also receives a unicast announce with the request correlation ID.
Both applications can share the discovery listener. Peers expire after 45
seconds. The source IP plus announced port determines the observed endpoint;
advertised arbitrary URLs are never followed.

Before a call, resolve the configured endpoint's current identity. Request,
subscribe and unsubscribe payloads MUST be `{target_id: <UUID hex>, body: ...}`.
An old identity or a missing target cannot actuate a host. Only discovery and
events are admitted on the shared discovery socket. Responses preserve the
request ID. Events are `{sequence: <per-source event counter>, event: <typed
notification>}`. Consumers suppress old/duplicate events and refresh the
authoritative source after gaps/queue overflow/restart; observations alone do
not replace board state. Discovery metadata, commands and events have separate
semantics.

Command receipts use a 64-ID replay window per sender session. Replayed or
too-old commands are not executed. The 256-sender ledger is never evicted to
make an old command eligible again; exhaustion fails closed until the operator
restarts/reconfigures the endpoint. Workers, subscriptions, pending calls and
received-event history are bounded (16/64/64/64 respectively). A duplicate
command does not receive a second execution or a fabricated acknowledgement.
There are no automatic mutation retransmissions: after a timeout the outcome is
unknown. Query again or reconcile the authoritative state before deciding to
issue a new command. UDP is not guaranteed delivery and cannot guarantee an
actuation latency; existing media timing guards are unchanged.

## Evidence and remaining acceptance

The initial implementation passes Go compilation/static vet, PCController Web
typechecking and Pealayer offline `cargo check`. A pure Rust transport probe
used the actual isolated Go Host dispatcher on loopback for discovery/identity,
ping, snapshot, a 13,536-byte fragmented binary UI catalog, and a pushed
`udp_probe` event. No board, player, production settings or LAN discovery was
used. Full suites and CI waits were skipped at the user's request.
The Go/Rust golden fixture is byte-identical (109 bytes), including i64 minimum,
u64 maximum, f64, UTF-8, null, booleans and a collection. Run each probe with
`--codec` to inspect it without opening any socket.

Developer probes are `Tools/Controller/examples/udp-peer` and Pealayer's
`tools/udp-probe`; both use production codec/client code. They are not alternate
implementations or production services. The Go fixture disables native IPC,
autoconnection and integrations, binds only 127.0.0.1:18877/18878, uses temporary
private configuration and exits after 60 seconds. Rust only contacts that fixture.

Installed Cafe/Erfan binaries do not yet contain this new implementation.
Packaging, approved host updater activation, live mutual LAN discovery,
loss/reordering stress, sustained media/board timing and human/physical
acceptance remain separate gates. Erfan's previous Pealayer updater policy
blocker still applies; do not bypass it. No firmware flash, movement, relay or
tone is needed to validate UDP framing/discovery.
