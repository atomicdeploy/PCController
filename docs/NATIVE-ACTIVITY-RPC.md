# Native activity RPC additions

These methods use the existing IPC/JSON-RPC authentication and remote capability
policy. They do not create a second board owner or an unrestricted transport.

| Method | Request/result | Capability |
| --- | --- | --- |
| `controller.ui.preferences.get` | `{}` → `{settings: UI, etag: string}` | Read |
| `controller.ui.preferences.set` | `{settings: UI, if_match: etag}` → same result | Host configuration |
| `controller.automations.get` | `{}` → `{items: Automation[], etag}` | Read |
| `controller.automations.set` | `{items: Automation[], if_match: etag}` → same result | Host configuration; bridge pivot forbidden |
| `controller.melodies.get` | `{}` → `{items: Melody[], etag}` (stored overrides) | Read |
| `controller.melodies.set` | `{items: Melody[], if_match: etag}` → same result | Host configuration |
| `controller.effects.library.get` | `{}` → `{library: {effects, groups}, etag}` | Read |
| `controller.effects.library.set` | `{library, mode: merge/replace, if_match: etag}` → same result | Host configuration |
| `controller.status_led.policy.get` | `{}` → `StatusLEDPolicy` | Read |
| `controller.status_led.policy.set` | Policy fields plus mandatory `if_match` | Host configuration |
| `controller.motion.hold.start` | `{id, side, generation, direction, expires_at}` → lease | Board commands |
| `controller.motion.hold.renew` | Lease → renewed lease | Board commands |
| `controller.motion.hold.release` | Lease → `{released: boolean}` | Board commands |

UI preferences replace only the typed `appconfig.UI` object, validate the full
candidate, suppress no-ops and recheck the ETag inside the store writer callback.
They do not include credentials, scripts or transport configuration. Successful
saves emit `ui.preferences.changed`.

The authoring collections also use strict bounded requests, full configuration
validation, no-op suppression and a second revision check inside the serialized
store writer. Automation writes do not implicitly enable virtual keys or power
permissions; new/changed RF bindings require a complete waveform identity and an
advertised control action. Enabled duplicate RF gestures are rejected. Unchanged
disabled assignments outside the current profile are preserved. These endpoints
do not explicitly run rules, play audio, learn RF or flash a board. Existing
configured event automations remain active according to their saved enabled state.
Removing a melody override does not remove built-in recovery fallbacks.

Status-policy revisions are SHA-256 of canonical JSON (object keys sorted,
values unchanged). The revision is computed from the policy itself, without
`if_match`. Saves recheck inside the writer callback and emit
`ui.status_policy.changed`.

`controller.network.peers.set` and `controller.hotkeys.set` also accept optional
`if_match` canonical-JSON digests, of `{peers:[...]}` and the bindings array
respectively. Native editors supply them. Legacy clients may omit them. Peer
inventories never disclose plaintext tokens. Metadata-only edits preserve a
legacy token only when name, endpoint and secret reference are unchanged.

## Channel intent fences

`controller.action.invoke` and `controller.pwm.set` accept these optional fields:

```json
{
  "expected_host_instance_id": "advertised host identity",
  "expected_connection_generation": 42,
  "board_identity": "advertised board identity",
  "expected_revision": "advertised board profile revision",
  "expected_pwm_configuration_revision": "advertised PWM configuration revision"
}
```

If any field is supplied, the complete host/generation/board/profile fence is
mandatory. PWM operations additionally require the conversion revision. Native
controls capture these values when rendered (sliders retain the press owner).
The service rechecks both at admission and at the actual serial write. Omission
retains the existing legacy-client contract; a partial fence cannot downgrade to
legacy behavior. A failed/unknown output result must not be automatically retried.

## Motion holds

Side is 1 or 2; direction is `up` or `down`; `id` is a fresh 128-bit hex token.
The client press deadline must still be in the future and at most two seconds
away. It bounds pending admission as well as the write. Clocks must agree;
stale presses fail closed. The resulting core lease expires after 700 ms;
native held input renews every 200 ms without resending movement.

The lease is `{id, side, generation, expires_at}`. Release is idempotent for that
token, revokes an unacknowledged start and cannot stop a successor intent.
Focus/capture loss, window deactivation/closing, owner replacement or invalid
door/safety state release the token. Core expiry requests a safety-priority STOP,
with at most three bounded stop attempts; it never retries movement. Failure to
confirm STOP is published as `motion.hold.stop_failed`, not reported as success.
Release retains authentication/board capability checks but can release its own
lease through a newly acquired media reservation. It cannot take that authority.

These source and fixture checks are not proof of physical motor stopping or door
interlocks. Real hardware acceptance remains mandatory.
