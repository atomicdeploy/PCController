# Semantic channel routing

A **semantic channel** names an installation function, such as `lighting.aisle`.
A **physical output** identifies the actual board connection, such as `relay.5`.
Its **binding** connects the two. Names and icons belong to the semantic channel
and do not move to another function when bindings swap.

## Operator workflow

1. Configure the board profile, then open **Settings → Semantic channel routing**.
2. Add a unique lowercase key, a display name, and a compatible physical output.
   The key is permanent vocabulary for integrations; edit the display name when
   changing its caption. Enable semantic channels and save the bindings.
3. Select another compatible output to rebind it. If occupied, the editor swaps
   both assignments in the draft. Save commits the pair in one transaction.
4. The Web Control page and local/remote TUI Control tab expose the enabled
   semantic channels. Their observed values follow the bound physical output.

Before changing wiring, stop effect playback/recording, clear the prepared media
timeline, and turn the routed outputs off. Saving reads the affected output
state; unknown, stale or active values block the change. An offline existing
profile may be edited without connecting or issuing any board command.

The initial implementation routes **general-purpose relays R5–R8 and PWM0–PWM10**.
The firmware owns interlocked motion R1–R4 and autonomous illumination/power/status
PWM11–PWM15; those are deliberately excluded. Changing a host binding cannot
relocate their firmware safety or autonomous logic. No EEPROM or firmware writes
are performed by this feature.

## One authoritative host contract

`controller.channel_routing.get` returns the current board identity/revision,
enabled mode, semantic channels and eligible physical targets.

`controller.channel_routing.update` takes `board_identity`, `expected_revision`
and the complete `routing` object. Both sides of a swap must be in that one
replacement. Duplicate keys/targets, reserved namespaces, electrical kind changes
and malformed values are rejected. Failed persistence does not mutate the prior
configuration snapshot. Normal profile revision conflicts preserve client drafts.

`controller.channel.set` takes `expected_host_instance_id`,
`expected_connection_generation`, `board_identity`, `expected_revision`, `key`, and
`value` (relay 0/1, PWM 0–4095). It resolves the binding on the authoritative host,
checks the target's lock, and pins dispatch and readback to one authenticated
board generation. Existing E-STOP, media authority, serial scheduling and PWM
cancellation policies remain in force. An ACK without matching readback is not
reported as success. Mutations are never automatically replayed on failure.

`controller.output.set` uses the same required owner/revision/session fields
but takes a physical `target` instead of `key`. It provides a fenced, observed
single-output write for explicit physical scene restoration without requiring
semantic routing to be enabled. It is **not an atomic output batch**: scene
callers must track each confirmed result and preserve partial/unknown outcomes;
they must not automatically replay or roll back hardware actions.

For PWM values in either write API, pass integer `percent_milli` (0–100000, meaning 0–100%)
instead of `value`. Exactly one is required. The host applies its existing
per-channel logical curve once; clients must not substitute linear raw duty.
These requests also require `expected_pwm_configuration_revision`, the opaque
string advertised as `pwm_configuration_revision` in both catalog APIs. PWM
configuration is host-owned separately from board profiles, so both revisions
are fenced. The PWM configuration editor and semantic binding editor serialize
against these writes.

A successful output result contains `accepted: true`, `outcome: "confirmed"`,
`target`, raw `value` and matching raw `observed_value`, plus `host_instance_id`,
`board_identity`, `revision`, and `connection_generation`. Logical writes also
echo `percent_milli` and `pwm_configuration_revision`. The JSON-RPC response ID
correlates the request. An RPC error with `data.outcome: "rejected"` means this
API rejected the operation before runtime dispatch. `data.outcome: "unknown"`
means runtime dispatch started and the caller must not assume no actuation.
A missing response or unclassified error is also unknown, never permission to
retry automatically. These statements describe observed board registers, not an
independent electrical measurement at the output terminal.

Changes publish the existing `peripherals.changed` state event with the profile
revision and `changed_fields: channel_routing`. `controller.peripherals.get` also
includes `channel_routing`; no client-local wiring configuration is authoritative.
Clients must refresh on this event and reconnect, not poll at telemetry cadence.

## Explicit boundaries and follow-up acceptance

- Existing numeric commands, recorded effects and cues remain **physical**. They
  are not silently reinterpreted or migrated when this optional mode is enabled.
- Pealayer's cue authoring and the sequence compiler still need a semantic-key
  reference format and a revision-fenced prepare path before semantic cues can be
  advertised. Do not claim that existing timeline content follows a new binding.
- Native desktop mapping authoring, a full TUI wiring editor, and firmware-owned
  role remapping are follow-up work, not capabilities inferred from a host alias.
- Direct/raw diagnostics deliberately retain physical meaning. The idle check
  is an observation, not an electrical lock against independently triggered RF
  or firmware activity; do not rewire live machinery based on software alone.
- Source tests, rendered fixture screenshots, installation, cross-client live
  propagation and actual electrical routing are separate acceptance gates.

## Verification

Run Go checks with the repository's stable-path runner, not direct `go test` on
Windows. Focused packages: `internal/appconfig`, `internal/control`,
`internal/ipcjson`, `internal/tui`, and `cmd/controller`. Tests cover persistence,
atomic swaps, stale owners/revisions, disabled aliases, protected outputs,
curve conversion, events, and framed relay/PWM ACK plus readback behavior.

In `Tools/Controller/web`, run `npm run typecheck`, `npm test`, and `npm run build`.
For the isolated UI acceptance fixture, start Vite on loopback port 4179, then
run `node Tools/Build/verify-channel-routing.mjs <browser> <evidence-directory>`
from the repository root. It opens its own headless browser profile and checks
swap/save/conflict/reload at exact 390, 768 and 1280 px widths, including Persian
RTL and light/dark schemes. The fixture intercepts its own API calls and cannot
control a board. Screenshots are rendered UI evidence, not live installation or
hardware evidence.
