# Native standalone controller integration

The user requires full native standalone operation. Offline drafts do not
satisfy that requirement. The Flutter app currently remains a remote plus
native authoring surface; this file records concrete progress and the remaining
integration, without claiming that a compiled library controls a board.

## Shared core build evidence

The existing `Tools/Controller/cmd/controller-cabi` successfully builds with
Go 1.26.5, `GOOS=android`, `GOARCH=arm64`, `CGO_ENABLED=1`,
`-tags controllerlib -buildmode=c-shared`. The installed Android NDK
28.2.13676358's Clang targeted Android API 24. ELF inspection confirms an
AArch64 shared object. No SDK/library upgrade, Android root grant, board write
or standalone runtime launch was needed for this verification.

This C API already owns the production Go Host, RPC dispatcher, profile,
configuration, effects and controller domains. Integrate it rather than
duplicating that coordinator in Dart or restoring the donor ESP protocol.
Firmware stays the one shared production firmware; Android varies the native
entry point, device transport and packaging.

## Required integration before standalone acceptance

- Package the reproducibly built native core and its existing notices. Record
  exact source/binary identity and prove that it loads/runs on each device.
- Give it a dedicated app-private configuration/data root. Never point tests
  at the user's existing app configuration or start automatic board discovery
  during a native-core smoke check.
- Route native RPC, subscriptions and artifacts through a private app-owned
  endpoint. PCController's trusted loopback model alone is not a per-Android-UID
  boundary. Retain the mediated, authenticated LocalAutomation interface for
  ADB/Termux/Tasker clients; do not publish a second unrestricted board proxy.
- Implement explicit foreground service/lifecycle ownership and a verified
  graceful shutdown path. Android SAF dialogs must not destroy a programming
  coordinator or silently replace the pinned board owner.
- Acquire Android USB permission for an actually discovered supported device;
  supply serial byte I/O/reset metadata as a HAL/transport adapter. Keep the
  current framed protocol, semantic profile, timing and safety admission.
- Reconcile physical identity, busy/programming latch, capability and reconnect
  state. Lost acknowledgements never replay mutations or claim success.
- Prove actual monitoring, output/front-panel/audio/display timing, firmware
  backup/upload/readback and recovery through that Android-owned route.
- Complete local Pealayer/media ownership where required by standalone mode;
  a remote playback controller is not a complete standalone player.

Android USB access, native programming/toolchain packaging and full standalone
Pealayer are not delivered by preview 0.1.3. They remain explicit goal work.
