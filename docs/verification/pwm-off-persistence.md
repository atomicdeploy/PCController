# Shared Off-intent brightness persistence

Follow-up to [#675](https://github.com/atomicdeploy/PCController/issues/675).
The accepted one-button RF implementation remains independently available on
main; this follow-up closes normal CLI/RPC/semantic Off persistence gaps.

| Route | Confirmation and save |
| --- | --- |
| `pwm off N`, `pwm set N 0`, `pwm raw N 0` | Primary read/write/readback; configured restore-policy duty saved once |
| `controller.pwm.set` with zero | Same primary Off operation, not a facade-owned draft |
| `controller.action.invoke` for a PWM action reaching zero | Same primary confirmation and store |
| `pwm off`, `controller.pwm.off` | Confirmed batch with at most one profile write |
| Positive streaming, telemetry, hold repeats | No per-frame configuration writes |
| Native E-STOP/cleanup zeroing | No operator-recall persistence side effect |

The primary command engine supplies the current configuration/store resolver.
Off readback retains the last confirmed nonzero **raw duty** for channels whose
policy is `restore`. Unknown memory is not invented. A save merges only the
selected slot(s) into the currently serialized profile, preserving unrelated
channels and edits. Duplicate duty skips persistence, including a concurrent
identical save. Explicit `pwm remember N` retains its existing release-intent
behavior; no arbitrary recall setter or memory-repair RPC is introduced.

The commit validates the authenticated connection generation, initial readback
revision and selected-channel zero/memory evidence. During the serialized store
callback, a replaced owner, changed selected slot or changed profile rejects the
save. Unrelated newer channel observations do not overwrite another slot. No
configuration or effect provider is invoked while holding the runtime lock.

`persistence_failed` distinguishes a confirmed hardware output from failure to
save its recall: `write_acknowledged` and `readback_confirmed` are true;
`retryable` is false. Neither the failed save nor a lost readback retries an
output mutation. The existing peripheral-change event announces a successful
profile change to connected clients.

## Verification boundary

Focused fixtures cover framed CLI and shared semantic operations, a real
loopback TCP board behind direct RPC dispatch, generation/revision replacement,
unknown/full-policy memory, serialized concurrent edits, batch saves, positive
streaming and native cleanup. These run through maintained hosted CI only;
no local compilation, test execution, production profile write or physical
actuation is part of this source pass. CI results, installed package identity
and human physical acceptance must be recorded separately.
