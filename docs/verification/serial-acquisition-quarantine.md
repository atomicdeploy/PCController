# Serial acquisition quarantine

Owning issue: [#667](https://github.com/atomicdeploy/PCController/issues/667).

## Reproduced defect

A canceled serial acquisition can return a close-only `Session` while the OS
open is still pending. It has no port yet. The runtime retains that owner so
late acquisition and retryable cancellation failures cannot leak a handle or
permit a competing open. Previously, ownership was mistaken for connectivity:
status subscribers could issue a request to this owner before the first Close,
and `writeRawContext` dereferenced the missing port, terminating the host.

`TestPendingSerialAcquisitionRejectsOperationsBeforeFirstClose` reproduced the
same nil-pointer failure at `session.go:572` before the fix, without hardware.

## Corrected contract

- A pending-acquisition owner starts with writer/request/DTR admission closed.
  It remains responsible for closing the exact late handle, including retries.
- Retained close failures and closing sessions are not authenticated devices.
  The shared snapshot reports `connected:false`; failed cleanup is `blocked`.
- Application reads and mutations reject quarantined sessions with `ErrClosed`.
  No status bytes or stale output command are sent to these cleanup owners.
- New opens remain prohibited until cleanup succeeds. Successful cleanup drops
  ownership; it does not automatically replay commands or claim output state.

## Verification

Run on Windows through the maintained stable-path runner:

```text
node Tools/Build/go-tests.mjs --package internal/link --package internal/control
```

Tests cover rejection before first Close, status and mutation admission, truthful
snapshot phase, preserved cleanup ownership, exact late-handle closure, canceled
opens, retryable cleanup and competing-attachment safeguards. Source tests and
packaged-build checks do not constitute live or physical-output acceptance:
after the exact merged host is installed, separately verify health, identity-
based reconnection and bounded authenticated status reads without actuation.
