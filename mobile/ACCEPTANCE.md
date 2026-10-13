# Unified native controller acceptance

## Implemented source scope

The app is Android-native with responsive phone/tablet layouts, shared routing,
PCController control/telemetry/front-panel/firmware activities, Pealayer remote
activities, and offline drafts/sequence editing. Existing host protocols and
physical safety admission are reused without adding an unrestricted proxy.

## Evidence to record at handoff

- Locked offline Flutter restore, analyzer, full inherited + new tests.
- Exact APK SHA-256 and preview/development signing status.
- Deduplicated Android device identity, install receipt, foreground package,
  screenshot inspection and read-only physical-host connection.
- GitHub source ref, PR and observed CI status (not inferred green).

## Outstanding acceptance and feature work

- Pealayer is currently offline on this workstation. Direct playback, cue
  dispatch, mutual live application acknowledgement and its Pealayer→board
  route need a running approved host. Existing local GUI/Cafe smoke-launch
  denials are not bypassed by this app delivery.
- No AVR flash, reset, motion, relay/PWM write or physical tone is used merely
  to verify app/UI styling. Host/programming admission, physical recovery,
  flash/readback and firmware persistence still need explicit live acceptance.
- Native standalone means offline profiles/drafts today, not direct Android
  USB/Bluetooth serial ownership or a packaged Go/Rust coordinator. Clarify
  the intended standalone hardware route before implementing that expansion.
- Full Web/native parity is unfinished: graphical multi-track cue dragging,
  full RF/I2C/strip settings, discovery onboarding, palette/icon/folder editing,
  exact Android buzzer mirroring, tablet physical-device acceptance and a
  production signing/update channel remain investigation/continuation items.
- The retained Pakar implementations are donor/reference coverage, not proof
  that their ESP-specific activities control the PCController board.
- Pealayer cue editing currently opens the existing host editor. This does not
  claim that its entire native designer has been ported into Flutter.

Keep physical, renderer, source/CI and deployment claims separate.

## Observed verification, 2026-10-12

- Flutter 3.47.3 / Dart 3.13.3: locked offline restore and analyzer passed.
- Full suite: 105 passed, two opt-in renderer/live tests skipped by default.
- Opt-in renderer and live-host tests: both passed. Direct PCController
  snapshot, peripheral catalog and advertised WebSocket subscription were
  exercised read-only against a host connected to the physical board.
- Offline phone (412×915) and tablet (1280×800) captures were inspected,
  including native sequence editing and dimmed repeat previews. These are
  explicitly renderer previews, not installed-device screenshots.
- ARM64 preview APK: 18.5 MB, development signing, SHA-256
  `353fc77c6af7778d138144079236cb26bc50212f794d9021baf10e92c3803d14`.
  Local and CI delivery previews open without a preconfigured peer.
- Native SAF resume/owner revalidation, retained bridge selection, explicit
  disconnect persistence, partial panel authority and disappearing LCD choices
  have source/unit/renderer coverage. Actual Android SAF interaction remains
  unverified because installation is blocked; mocks are not device acceptance.
- The discovered Samsung Android device refused installation:
  `Requested internal only, but not enough space`. Its installer reserve
  remains authoritative; no app/data was deleted and no alternative install
  location was used to bypass it. The unused test ADB reverse was removed.
- Pealayer live playback/mutual routing, physical output/programming and
  Android physical/tablet acceptance are not passed by the above evidence.
