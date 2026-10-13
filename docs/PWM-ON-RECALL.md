# Shared PWM On and brightness recall

Choose the actual advertised channel and confirm that its output is safe before
using any of these commands. A MOSFET label is one-based; `pwm.N` is zero-based.

| Interface | Set policy | Invoke |
| --- | --- | --- |
| RPC / IPC / WebSocket / bridge | `controller.pwm.channel.configure {"channel":1,"on_behavior":"restore"}` | `controller.control.invoke {"action_id":"pwm.1.on"}` |
| CLI / TUI console | `pwm configure 1 lighting gamma 2.2 lightbulb 1 restore` | `pwm on 1`, `pwm off 1`, `pwm toggle 1` |
| Web PWM mixer | Channel's **On** behavior selector | **On** / **Off** use the same semantic actions |
| Host RF binding / other clients | Consume advertised config and semantic actions | `pwm.N.on`, `.off`, `.toggle`, `.increase`, `.decrease` |
| Config file | `ui.pwm_channels["pwm.1"].on_behavior` | Same primary-owned policy |

`full` (default) means On requests 100%. `restore` means Off→On restores the
last confirmed nonzero duty. On while already lit preserves its current duty.
Toggle uses a fresh board reading: nonzero→Off, zero→On under the same policy.
An absolute `controller.pwm.set {"channel":1,"percent":100}` remains 100%,
never recall. Relative actions and configured curve/step semantics are unchanged.
Indicator click choice (toggle/on/off) and action visibility are **client
presentation preferences**, not competing backend channel configuration.

Configuration updates preserve omitted fields, including custom step and recall
policy, inside the serialized mutation. `peripherals.changed` tells subscribed
clients to refresh their shared catalog; no output changes on merely opening a
menu or editing this policy. Readback events still carry authoritative values.

## Memory and safety

- One primary owns fixed-size RAM memory shared by all clients. Only complete,
  valid, available board readbacks update nonzero memory, including externally
  changed outputs. ACK alone, failed writes and optimistic drafts do not.
- Off does not erase memory. Unknown memory falls back to full; consumers must
  honor `remembered_known` instead of displaying unknown as remembered zero.
- Memory is scoped to the authenticated connection generation. Disconnect /
  replacement / restart cannot restore another board's value. There is **no
  persistence across restart/reconnect**, no per-frame disk writes and no
  inference that an adapter name identifies a physical board.
- Exact raw duty is remembered. Curve edits do not unexpectedly alter recall's
  electrical duty; the displayed remembered logical percent is derived through
  the **current** curve. Configuration itself persists normally.
- `controller.pwm.values.channels[]` exposes `config.on_behavior`,
  `remembered_known`, `remembered_raw`, `remembered_percent`. Shared peripheral
  and control descriptors expose `on_behavior` and advertise `.toggle`.
  Snapshot `pwm_recall[]` carries `{known,raw}` for the current owner only.
- Semantic actions retain fresh-read / one-write / ACK / readback, owner
  generation, profile lock, media authority and E-STOP fences. An acknowledged
  write without confirmation remains outcome-unknown and is not retried.
- Firmware-autonomous RF press/toggle still uses raw 0/full; host recall does
  **not** change autonomous firmware or silently rewrite existing RF/motion maps.

## Verification gates

Regression fixtures cover raw recall, Off preservation, unknown/default fallback,
curve edits, absolute100, external observations, malformed/unavailable readings,
ACK without readback, replaced generations, framed transport, two callers and
partial/concurrent config edits. CI must pass before deployment. Source/CI
receipts are not proof of physical light stability: the separate unexplained
cutout investigation remains [#672](https://github.com/atomicdeploy/PCController/issues/672).

For live acceptance on a human-approved channel: set a nonzero percent, Off,
On from a second client, compare fresh raw readback; toggle twice; explicitly
set 100%; check restart/reconnect unknown-memory fallback and final safe Off.
Do not operate motion outputs or stop an active program to run this test.
