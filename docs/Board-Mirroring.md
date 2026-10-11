# Capability-based board mirroring

One primary PCController runtime owns each board transport. One explicitly
named coordinator owns a group; it reads those owners through the existing
JSON-RPC WebSocket peers and fans out **typed volatile operations**, never UART
frames. Do not configure reciprocal coordinators for the same boards. No
distributed leader election or automatic coordinator failover is implemented.

## Implemented scope

| Domain | Observation and write contract |
| --- | --- |
| Menu | Live menu page; requires the advertised HELLO build feature `0x40`. `MENU_SET_PAGE [page \| 0x80]` is volatile and does not save the boot page. An absent capability is unavailable, not silently downgraded. |
| Segments | Exact four raw cells through a renewable two-second presentation lease. Brightness, TM1637 detection, local safety and editing remain physical-board policy. |
| LCD | Exact two 16-byte ASCII lines, only when the board reports an available LCD. |
| Static status RGB | Available for safe steady base/manual conditions, never safety overrides, running state, transient cues or animations. Exact rendered RGB uses receiver brightness 255 to avoid double-scaling; the source's physical brightness remains an observation, not a settings write. |
| Buzzer | Pushed finite tones/rests, bounded receipt queue and echo matching; ACK means accepted, not audible. Repeated identical simultaneous notes are ambiguous on legacy UART (no route ID in the device event). |
| General relays | Explicit R5..R8 allowlist; SDK readback required. R1..R4 are interlocked motion outputs in this firmware and are rejected. UI profile labels do not change the wire safety mapping. |
| PWM / PCA9685 | Explicit logical channels 0..15 and exact 12-bit values; actual availability and full readback required. This is the device-specific I2C output path, not motherboard-bus access. |
| Inputs / sensors / detection | Board-scoped `observations`, never fabricated sensor values or writes of another board's physical key/door state. Either board's inputs can produce mirrored supported outputs. |

Motion, raw I2C, RF learning/transmit, settings/EEPROM, programming, reset,
strip streaming and distributed effect schedulers are **not** fanout domains.
Coordinated motion needs a cross-board E-stop/lease/interlock commissioning
contract. Other I2C devices need a device-specific address/register policy.
Firmware-managed edit modes and persisted preferences are not a synchronized
distributed state machine. Do not claim that every physical input/effect is
already full-duplex merely because supported output readback matches.

## Configuration and API

All methods use existing IPC provenance/capability policy. `configure` requires
host-configuration permission, `inspect/status` read permission, and
`apply/resync` board-command permission. The alpha product's existing disabled
authorization contract is unchanged: receive allowlists are not authentication.
Keep demo listeners loopback-only; use an authorized tunnel for remote access.

`controller.mirror.configure` replaces `integrations.board_mirror`. Disabled
configuration is `{ "receive": {} }`. Configure the **receiver first**:

```json
{
  "local_board_id": "panel-b",
  "receive_from": "panel-coordinator",
  "receive": {"domains": ["menu", "segments", "buzzer", "pwm"], "pwm_channels": [0]}
}
```

Coordinator / panel A (also a receiver of observations originating on B):

```json
{
  "local_board_id": "panel-a",
  "coordinator_id": "panel-coordinator",
  "receive_from": "panel-coordinator",
  "receive": {"domains": ["menu", "segments", "buzzer", "pwm"], "pwm_channels": [0]},
  "groups": [{
    "id": "front-panels",
    "members": [{"board_id": "panel-a"}, {"board_id": "panel-b", "peer": "second-panel"}],
    "policy": {"domains": ["menu", "segments", "buzzer", "pwm"], "pwm_channels": [0]}
  }]
}
```

`second-panel` must already be an enabled JSON-RPC WebSocket client with
commands permitted. Board IDs are explicit stable identities, not inferred
from a COM port or a peer label. Duplicate/overlapping IDs/endpoints are
rejected; a peer returning the wrong identity is quarantined.

- `controller.mirror.inspect {}`: explicit refresh of availability, generation, volatile
  values and board-scoped physical observations, plus unsupported domains.
  `{"cached":true}` uses the authoritative push cache and labelled bounded legacy
  readback instead; it is the coordinator's observation path. `observation_paths`
  distinguishes firmware push, bounded legacy readback and initial-only LCD state.
- `controller.mirror.status {}`: members, per-member errors, coordinator
  sequence, last origin, ACK/verified dispositions, dropped-note count and
  `resync_required` boards. A new configuration or connection needs explicit
  selected-source resync before receiving mirrored actuation, not an automatic
  timer-based promotion. Local standalone control remains available.
- `controller.mirror.resync {"group_id":"front-panels","source_board_id":"panel-a"}`:
  explicit initial/reconnect synchronization from a selected connected source.
  Buzzer notes are never replayed by resync. Results are per operation/target;
  partial failure is not rolled back or disguised as success.
- `controller.mirror.apply`: bounded routed write used by the coordinator.
  Receiver validates matching semantic intent, identity, coordinator allowlist,
  deadline, connection generation, sequence and replay disposition. Not a raw
  opcode gateway.

Changed segment/RGB/PWM events wake the single coordinator immediately; segment
and RGB push never require a repeating snapshot read. The 250 ms owner tick
checks leases/connections and provides labelled bounded legacy status/PWM
readback (and LCD only when opted in, since no LCD push is advertised). Exact
panel reads are reserved for initial connection, explicit refresh/verification,
or that opt-in legacy domain. Slower endpoints reduce cadence. Conflicts use
serialized observation order (configured member
order within one poll), not remote wall-clock last-write-wins. First observation
and new connection generations only establish a baseline. Disconnected writes
are reported unavailable and are **not queued for reconnect**. Reconnect needs
explicit resync; no actuation is restored automatically. Receiver ordering uses
a boot epoch and per-epoch sequence; a backwards coordinator clock is rejected
rather than silently weakening ordering.

Segment leases renew at 750 ms only while connected readback agrees. On drift
or lost ownership they expire to the real local panel. A host-side segment
intention revision is checked under the same wire
send lock, so renewal cannot overwrite a newly accepted local display/menu
command before its rendered push arrives. Local intentions and any reconnect
end the old lease ownership; a reconnect is never an automatic lease restore.
LCD and RGB safety remain local. Buzzer event matching is bounded to two seconds; there is no audio-clock
phase lock across hosts. Host buzzer mirroring retains its existing timestamped
note scheduler, rest cancellation, backend diagnostics and overflow reporting.

## Linux and second-AVR acceptance

VirtualBoard executes the same production firmware on Linux, with RAM-only
`--memory --quiet` mode and independent processes per board. The native HAL
implements virtual relay/PCA9685/display outputs and explicit input fixtures,
not arbitrary real GPIO. `ON:n`/`OFF:n` stdout is available without `--quiet`.

The inspected `root@server` host is x86-64 Ubuntu, not an ARM SBC. It exposes a
PC speaker and ALSA hardware, but no GPIO chips or attached AVR serial devices.
Do not scan/write its motherboard I2C buses. Native Linux buzzer playback uses
kernel-duration `KDMKTONE`, cancellation-aware serialization and explicit stop;
permission/backend failure stays visible. No audio/PC-speaker physical
audibility is inferred from ioctl success. See the dated verification record
for actual live evidence.

Before enabling physical channels on the second AVR:

1. Verify each board's exact HELLO, pin/wiring profile, transport owner,
   E-stop/door behavior and available peripherals. Upgrade firmware for volatile
   menu capability if absent; preserve EEPROM and firmware backups first.
2. Start with only menu/segments/buzzer. Select an initial resync source.
   Confirm both directions using real buttons and listen to both buzzers.
3. Opt in one unloaded PWM/general relay channel; verify actual outputs and
   readback both ways. Never enable motor PWM merely from the example `[0]`.
4. Disconnect/reconnect either board; prove no queued actuation, no echo storm,
   lease expiry and explicit resync. Test simultaneous input conflict ordering.
5. Commission motion and additional device-specific I2C separately; do not
   relabel their absent contract as production-ready mirroring.
