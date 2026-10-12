# PCController native controller

An independently packaged Flutter Android phone/tablet app with PCController
and Pealayer mini-apps. It is a native Flutter surface, not an embedded Web page.
PakarController remains a separate project and Android installation.

## Activities

- PCController: live power/temperature/HOT monitoring, host-resolved channel
  names/folders and semantic relay/motion actions, PWM setpoints, E-STOP,
  physical TM1637 frames/detection, LCD, menu navigation, HOST-captured keys,
  bounded buzzer tones, host Idle/Running state, effect catalog and cancellation.
- Firmware: Android SAF import of bounded HEX/BIN files, SHA-256 immutable
  artifacts, explicit programming confirmation, flash capture, verified export,
  durable operation status and WebSocket-triggered progress refresh. Toolchain
  provisioning is indeterminate, never a fabricated 0% progress bar.
- Pealayer: playback/stop/seek/volume/mute, media opening, workspaces,
  effect playback/cue placement and a native cue timing editor. Intrinsic
  duration remains read-only; moving and resizing use the current host APIs.
- Workspace: local drafts and a native sequence designer with selectable step
  rows, timeline dragging/resizing, zoom, snapping, RGB controls,
  timing/duration/repetition fields and dimmed repetition previews. Existing
  descriptors retain their unknown fields; technical editing is collapsed.
  Publishing to PCController is explicit and does not run the effect.
- Activities: bounded in-memory host events and request receipts. Request
  acceptance is not reported as physical completion or applied playback.

Phones use bottom navigation; tablets use a rail and responsive multi-column
cards. System light/dark preference and platform text scaling are retained.
Settings also selects Light/Dark explicitly, layout and control density. The
PCController and Pealayer launcher icons may be enabled separately or together;
both open their own section of the same app. Android applies component changes
from the app's own UID without restarting it. Beep mirroring is explicitly
enabled in Settings and stops on backgrounding, loss of the selected peer or
generation change; actual audible/timing acceptance is tracked separately.
The neutral card/action styling shares Pealayer's purple accent and semantic
state indicators rather than coloring every action as selected. Relay, motion
and PWM cards distinguish observed state from commands/setpoints; stale values
become Unknown. In-memory voltage/current/power and both temperature histories
retain at most 600 observations (at most 2 Hz), with gaps for missing readings.
No chart samples are persisted to disk. Live segment frames and TM1637 ACK
detection work independently of full LCD/menu snapshot availability.

Optional authenticated local JSON-RPC (HTTP/WebSocket), TCP NDJSON and Android
Intent adapters are described in [Automation](AUTOMATION.md), including ADB,
Termux and Tasker usage, exact safety fences and background limitations.

## Connections and mutually routed controls

Configure HTTP(S) origins without paths, user-info, query strings or fragments.
Access tokens are stored only in Android secure storage, not SharedPreferences,
URLs, logs or build arguments. Use HTTPS on untrusted networks.

Direct PCController uses `/api/snapshot`, current `/api/rpc` and the bootstrap's
advertised `websocket_path` with native
Bearer headers. Direct Pealayer uses `/api/player/status`, `/api/rpc` and its
authoritative `/ws` state. No API-version or obsolete-protocol fallback exists.

Pealayer → PCController uses its existing `/api/peer/controller` typed dispatcher.
The explicit bridge route refreshes at a bounded two-second cadence because it
does not advertise the controller's live frame stream. Direct PCController
remains the route for high-rate frames and artifact bytes. Recursive bridges
and third-peer host updates are denied in this client and remain host guarded.

PCController → Pealayer uses registered `app_actions`, exact instance targets,
and `controller.app.action`. The host owns admission, leases, acknowledgement
and application outcome. Never forward the destination's credential from a
different peer. Direct host sessions and bridge authority remain distinct.

Disconnect/background revokes live state, closes sockets and cancels retries.
Explicit Disconnect stays disconnected after resume; a selected Pealayer board
route remains selected instead of silently returning to an earlier direct host.
Android SAF pickers may pause the app. Import/export reacquires the same actual
host/board/profile and explicitly selected route after resume before sending
bytes or consuming a one-use destination. A changed owner cancels the operation.
Reconnect performs an authoritative refresh; it never replays mutations.
Confirmation and document-picker returns are fenced by connection/host/board
identity. A lost acknowledgement is uncertain, not successful and not retried.

## Reproducible low-write build

Reuse the installed Flutter 3.47.3/Dart 3.13.3 and Android SDK; dependencies are
locked. No SDK copy, dependency upgrade, global proxy change, cache clean,
per-frame persistence or physical action occurs during verification.

```powershell
./tools/build.ps1 -Mode Verify
./tools/build.ps1 -Mode ApkPreview
```

The wrapper checks free space/build-cache budget, restores packages offline,
runs analysis/tests and optionally builds one ARM64 APK. It scopes localhost
`NO_PROXY` only around Flutter and restores the previous setting. Preview APKs
use the inherited development signing key, not production signing.

Android application ID: `ir.digitalogic.pccontroller.mobile`.
APK: `build/app/outputs/flutter-apk/app-release.apk` (ignored, never source).
Use an explicitly selected ADB device; deduplicate devices by `ro.serialno`.
Never reset shared ADB or automate a PIN/password/pattern. The user authorizes
installation/launch of this separate app, not destructive physical board tests.

## Provenance and remaining acceptance

### Unified workspace, discovery and self-package RPC

The earlier basic Flutter design is rejected. `AGENTS.md` requires one contextual
activity workspace, not product tabs; no unavailable-only cards or dormant peer
token fields. The optional protected local automation API still keeps its actual
security boundary. Android preview delivery is not design or physical acceptance.

Discovery browses `_pccontroller._tcp`, `_pealayer._tcp` and `_digitalogic._tcp`
using Android DNS-SD/mDNS, plus SSDP and MNDP. Each candidate is pinned to its
responder's real origin and verified by a bounded read-only health probe with no
proxy, redirects or credentials. Existing PCController health responses can be
verified against their own native public directory. Duplicate endpoints collapse;
this app's own sharing endpoint is excluded. Automatic connection applies only
to a unique verified vacant role under saved intent; explicit Disconnect and
live selections win. Actual network changes are coalesced, not probed per frame.

Standard MNDP has no HTTP port. Digitalogic application advertisements add
big-endian TLVs `0xff01` (product) and `0xff02` (two-byte API port). An identity-only
RouterOS neighbour is not an invented controller on port 80. The MAC field is
explicitly an application-scoped locally administered identity, not a board/NIC
MAC. Generic advertisements never authorize hardware actions.

`app.package.info/read` export only the installed single APK. The same protected
local HTTP/WS/TCP/Android Intent RPC offers `app.upgrade.begin/write/finish/status/
abort/install/permission/pull/push`. Chunks are at most 16 KiB, request/response
JSON at most 32 KiB, one incoming transfer at most 128 MiB. Offsets are exact;
lost mutation acknowledgements are not blindly retried. Storage is checked
before 0% or staging creation. Digest, self-package, signer, newer version,
minimum SDK and CPU compatibility are verified before Android installation.

Opt-in foreground peer sharing uses port 18992 and the same package RPC. Public
read-only export reveals no authoring, credentials or board/player proxy. A push
requires an expiring receiver approval dialog and a short-lived capability pinned
to one transfer/digest; the remote sender cannot invoke Install. Pull also verifies
the source manifest. Android installation permission and consent remain explicit;
`awaiting_android`/pending consent is not an installed receipt. There is no root,
shell installer, uninstall, silent update or cross-app APK import. Staging is in
the app's bounded private cache; only this module's own expired files are removed.
Split installs cannot currently be exported as one APK.

Source gates: focused Dart analysis, Android Kotlin compilation, Go build/vet,
PCController Web type-check and Pealayer Cargo check passed. All-four-protocol LAN
round trips, mutual device APK transfer/install, locked-device UI and hardware
acceptance remain separate. Full CI/test-suite waits are not required for this
iteration; retain those acceptance gaps rather than claiming production-ready.

See [provenance](PROVENANCE.md) and [acceptance](ACCEPTANCE.md). The reference
Pakar screens are retained in source/tests but are not the shipped entrypoint.
Their ESP protocol is not an alias for PCController. Native offline authoring
does not imply direct Android USB/Bluetooth firmware ownership or an embedded
Go/Rust host; those are explicitly separate unfinished work.
