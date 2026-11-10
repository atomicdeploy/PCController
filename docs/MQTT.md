# MQTT

PCController can connect to an MQTT 3.1.1 broker from the primary host. The
integration is off by default and does not enable the LAN HTTP or IPC listener.
An unattached board can publish host availability, but cannot accept board
commands. There is no MQTT broker embedded in PCController.

## Enable it

In the local Web UI, open **Advanced → Messages, discovery & history → MQTT**.
Enter an `mqtts://broker.example:8883` URL and a unique topic prefix, such as
`pccontroller/cinema`. The prefix is the complete per-device path. For existing
broker naming conventions it can be set to a Pakar-style path; do not give two
live controllers the same prefix. The username is optional. Use an `os:` or
`env:` password reference; the password itself is never returned or stored in
the host configuration. The CA bundle field optionally adds local PEM trust
anchors to system roots. TLS verifies the broker hostname and requires TLS
1.2 or newer. Plain `mqtt://` is accepted only for a literal loopback IP.

The local IPC equivalent:

```powershell
controller ipc call --method controller.mqtt.config.set --params '{"enabled":true,"broker":"mqtts://broker.example:8883","topic":"pccontroller/cinema","username":"pccontroller-cinema","password_ref":"os:mqtt/cinema","telemetry_seconds":5,"allow_commands":false}'
controller ipc call --method controller.mqtt.status
```

`config.set` replaces the MQTT configuration, not the rest of the host
configuration. Omit or clear `password_ref` to remove that reference; the vault
entry is not deleted. `status` returns `{config,status}`, including build
availability, saved enablement, broker connection state and the last connection
error. Saving is not a broker connection acknowledgement. The primary applies
changes without restart and gracefully closes the previous topic session.
Use `build.cmd --host-only --no-mqtt` (or `./build.sh --host-only --no-mqtt`)
for a package that omits the client. The corresponding Go build tag is
`no_mqtt`; saved MQTT settings remain portable and are not executed by that build.

## Topics and scope

| Suffix | Direction | Retained | Payload |
| --- | --- | --- | --- |
| `availability` | Host → broker | Yes | `online` after a successful subscription and status publish; `offline` on shutdown or broker Last Will. This is host availability, not board connection. |
| `status` | Host → broker | Yes | `board_id`, `boot_id`, `utc_epoch`, and `state`: connected state, generation, valid measurements, relay mask, observed PWM and front panel when available, and configured control catalog/ownership. |
| `event` | Host → broker | No | Selected board event notices and correlated `command-result` receipts. No host logs, RF waveforms, file paths, or secrets. |
| `command` | Broker → host | **Never publish retained** | JSON object up to 16 KiB. A bounded queue drops overflow without retrying commands. |

Use a dedicated broker account: read its one `command` topic; write its
`status`, `availability` and `event` topics. Grant observers read access only;
grant command-topic write access only to operators who may control outputs.
MQTT 3.1.1 does not attach publisher identity to a delivery. Broker permissions,
not a claimed username in the JSON, establish authority. Do not configure
broker rule engines to replay command publications.

Status uses current cached observations (no extra serial polling), with
coalesced changes and a periodic heartbeat (default 5 seconds; 1–3600
configurable). Missing or stale measurements are omitted, not reported as zero.
The retained state can outlive a disconnect; interpret it with availability,
`connected`, and the observation timestamp.

### Query and command examples

Send `{"command":"status","request_id":"read_1"}` (or `telemetry`) to
`pccontroller/cinema/command` for a correlated status result on `event`.
This works in the default read-only mode.

To send a write, explicitly enable **Allow board commands**. Use the current
`board_id` and `boot_id` from status, a new 1–40 character ASCII request ID
(letters, digits, dot, underscore or hyphen), and an expiry in UTC seconds,
at most 60 seconds ahead. Copy the current `state.control_owner` fields into
`params` alongside the typed method inputs. A configured attached board
profile is required. For example:

```json
{
  "command": "rpc",
  "method": "controller.pwm.set",
  "request_id": "change_27",
  "board_id": "<from status>",
  "boot_id": "<from status>",
  "expires_at": 1800000030,
  "params": {
    "channel": 0,
    "percent": 35,
    "expected_host_instance_id": "<from state.control_owner>",
    "expected_connection_generation": 4,
    "board_identity": "<from state.control_owner>",
    "expected_revision": "<from state.control_owner>",
    "expected_pwm_configuration_revision": "<from state.control_owner>"
  }
}
```

Replace the sample expiry and ownership values with fresh ones. Available
writes are catalog relay/PWM setpoint actions (`controller.action.invoke`),
`controller.pwm.set`, `controller.display.send`, `controller.buzzer.tone`,
`controller.menu.jump` and `controller.illumination.set`. They use the
same owner, playback and safety path as local commands. Raw opcodes, arbitrary
shell commands, motion, reset, firmware installation, host settings, and
cross-peer pivots are deliberately not available on MQTT. Pakar-specific
cell/snake commands target different hardware and are not PCController aliases.

Receipts echo `request_id`, `board_id`, and `boot_id`, and contain
`accepted`, a stable `code`, and the method result or rejection message.
An accepted handler is not proof of a physical output change. Missing receipts
are an unknown outcome; read back state before deciding what to do. The host
rejects retained deliveries on subscription, MQTT DUP deliveries and reused
IDs across reconnects within the command validity window. MQTT 3.1.1 brokers
do not preserve the RETAIN flag when forwarding a new live publication, so
operators must never publish retained commands; deadlines and owner checks
still apply to every write. New host, board or MQTT configuration generations
invalidate old command envelopes.
