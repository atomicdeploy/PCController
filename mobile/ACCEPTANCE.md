# Unified native controller acceptance

## User acceptance requirements, 2026-10-13

**The deployed app is not accepted.** This list remains durable even where a
feature has source coverage or an earlier deployment receipt. Check items only
when the named behavior is demonstrated; human acceptance remains separate.

- [ ] Professional unified visual design at least matching Pealayer egui and
  both projects' Web/native UIs, in light/dark, phone/tablet and scaled text.
- [ ] Channel indicators at the trailing end of card headers, truthful
  On/Off/Unknown state, three-dot management menus and polished activity tabs.
- [ ] Accessible settings, appearance/preferences, launcher icon selection
  and selectable multiple branded launcher entries.
- [ ] Actual Android beep mirroring with bounded timing, cancellation,
  reconnect ownership and background cutoff verified.
- [ ] One contextual front-panel button row, no duplicated menu/key rows.
- [ ] Complete live measurement charts and working live seven-segment frames;
  partial LCD/menu availability must not hide independent segment authority.
- [ ] Board monitoring, telemetry, firmware import/upload/backup/progress,
  front panel, seven-segment, keys, buzzer, mode and channel management.
- [ ] Branded Pealayer mini-app, playback/workspaces/media/effects/designer,
  migrated desktop/Web controls and unified activities.
- [ ] Mutual PCController/Pealayer peer bridges with scoped admission and
  real acknowledgement; no unrestricted recursive proxy.
- [ ] Local JSON-RPC/IPC, NDJSON, Android Intents and supported ADB, Termux,
  root-shell, Tasker, WebSocket and TCP send/receive paths verified on Android.
- [ ] Remote and full native standalone control, with the actual supported
  hardware/coordinator route stated (offline drafts alone are insufficient).
- [ ] Deploy, launch and test on every reachable distinct Android device,
  with screenshots and interaction receipts; physical tablet tests if found.

The active improvement goal must preserve unfinished gates and resolve
observed defects. Implementation, analyzer/tests, deployment, physical/audio
behavior and human visual acceptance are distinct evidence categories.

## Implemented source scope

The app is Android-native with responsive phone/tablet layouts, shared routing,
PCController control/telemetry/front-panel/firmware activities, Pealayer remote
activities, and offline drafts/sequence editing. Existing host protocols and
physical safety admission are reused without adding an unrestricted proxy.

## Improvement pass, preview 0.1.2+3

- Channel states moved into trailing card headers beside three-dot management.
  Presentation editing uses a native icon picker, color swatches, folder/name
  fields, host profile revision guards and host-owned channel locking.
- Activity tabs use a shared selected surface and stable horizontal scrolling.
  Front-panel navigation and HOST keys now share one contextual four-button
  row; unknown panel mode cannot dispatch a key. Host mode is its own control.
- Settings contains System/Light/Dark, automatic/mobile/tablet layout, compact
  controls, mirror volume and one/both branded launcher entries. Both aliases
  target the same single-task app; launcher changes never kill the app.
- Current `buzzer.note` frames now reach the existing native frequency-driven
  PCM receiver. Timing preserves bounded MCU deltas, shortening/dropping late
  notes, duplicate rejection, zero-note Stop, peer/generation ownership,
  background/disconnect cutoff and a finite native receiver lease. Mirroring
  is explicitly enabled in Settings; physical audibility remains unverified.
- Local HTTP/WS JSON-RPC, TCP NDJSON and Intent dispatch now additionally cover
  current-mode panel keys, bounded display messages/tones, buzzer Stop and host
  mode. Target, opt-in actuation, E-STOP and mutation concurrency guards remain.
- Pealayer playback uses advertised media-session actions in one transport row,
  stable seek/volume drag previews, the actual 0..130 volume domain, contextual
  mute, actual workspace profiles, speed, chapters, audio and subtitles.
- All five bounded telemetry histories/charts and independent segment-frame
  authority remain present. Renderer captures replay earlier measured values;
  they are not evidence of a currently connected Android physical board.

Verification: locked offline restore, analyzer with no issues, 117 full tests
passed (two opt-in tests skipped), followed by 13 focused final checks and the
opt-in renderer. Read-only snapshot/catalog/subscription smoke passed against
the isolated local shared-firmware development host, not physical hardware.

Both distinct reachable Samsung devices (phone SM-S908E and tablet SM-X906B)
received the development-signed ARM64 APK in place and launched. Repeated
Controller launcher invocation reused its activity. Android Intent requests
with an invalid token reached the Dart router on both devices and returned
JSON-RPC `-32001`. Both screens remained locked: touch/visual/native launcher
selection, authorized local transports, audible mirroring, live seven-segment
and physical peer acceptance are still pending. Android refused shell-based
component toggling; no launcher selection is claimed from that rejected check.

The reported Cafe peer endpoint was unreachable from this workstation during
this pass. The earlier blanket Cafe smoke-launch denial is superseded by
Pealayer's current paired-peer-deployment receipt; live Android routing must
still be verified against a reachable authoritative host. Full native/Web
designer/settings parity and direct standalone hardware control remain open.

## Native authoring pass, preview 0.1.3+4

- Replaced static sequence bars with a native interactive timeline: shared step
  selection, release-only start/duration edits, 10 ms snapping, zoom/Fit,
  horizontal scrolling and dimmed repetition previews. Moving preserves the
  existing duration and unknown descriptor fields. Changed or removed step
  ownership cancels a gesture instead of retargeting it.
- Added RGB/brightness sliders and color swatches to sequence steps. Inspector
  timing fields refresh after canvas edits without recreating input on every
  typed character.
- Pealayer cues now open a native timing editor. Unchanged Save sends nothing;
  moving uses `timeline.cue.place`, explicit permitted resizing uses
  `effect_cue.update`. Exact millisecond parsing, the current 1 ms..24 hour
  resize contract, intrinsic durations, peer/cue identity, unsaved edits and
  uncertain/rejected requests retain their appropriate guards.
- Cafe's live COM3 controller and paused Pealayer were verified read-only over
  the existing SSH path. Direct Android HTTP reachability to both published
  endpoints timed out on the phone and tablet. These are separate observations.
- The existing Go C API successfully cross-compiled with the installed Android
  NDK as an AArch64 ELF shared object. This is a concrete shared-core path for
  standalone work, but it is not yet packaged, started or connected to Android
  USB, and does not satisfy standalone hardware acceptance.
- The prior Android CI passed. The broader host Linux-x64 job failed at the
  status-LED observation in the two-board mirroring test; that failure is not
  described as all-green CI and is independent of the Flutter source checks.

The existing user checklist remains open. Complete native cue properties,
direct USB/programmer ownership, Android foreground service/receiver lifecycle,
native UDP discovery/command/event integration and physical acceptance are
continuation work, not claims from this preview.

## Evidence to record at handoff

- Locked offline Flutter restore, analyzer, full inherited + new tests.
- Exact APK SHA-256 and preview/development signing status.
- Deduplicated Android device identity, install receipt, foreground package,
  screenshot inspection and read-only physical-host connection.
- GitHub source ref, PR and observed CI status (not inferred green).

## Observed delivery, 2026-10-13

- Preview 0.1.1+2: ARM64 release build succeeded using the existing development
  signing key. Full tests, analysis and CI waiting were skipped at the user's
  request; only the requested opt-in Flutter screenshot renderer was executed.
- APK: 19,576,759 bytes, SHA-256
  `46dbf150bcee92776d4a1bd3b81ec40a0b6ff85bf9449ab0fd6d35754fda26ec`.
- The same APK was transferred and hash-verified via Termux SSH port 5022 at
  `/sdcard/Download/PCController-0.1.1-preview-46dbf150.apk`. The previous
  0.1.0 APK is retained as a named rollback, not deleted generically.
- ADB became available again. Two advertisements were deduplicated by device
  identity; one selected transport performed an in-place `install -r` with
  `Success`. Installed version is 0.1.1/code 2; installed base.apk SHA matches
  the delivered APK. No app data or drafts were cleared.
- Actual installed-phone captures were inspected: live voltage/current charts,
  connected state, raw mirrored seven-segment digits and `TM1637: Detected`.
  Existing saved peer settings reconnected without inserting a test peer or
  creating a new network tunnel. No board writes/tone/flash were triggered.
- Separate phone/tablet light/dark renderer previews replay real read-only
  board measurements with their original relative intervals. Their wall-clock
  timestamps are shifted for replay; they are not live/tablet-device evidence.
- Android explicit broadcast reached the Dart JSON-RPC router and rejected an
  invalid token with JSON-RPC `-32001`, as intended. Authorized HTTP/WS/TCP and
  Intent command acceptance still need the user-enabled token and live client
  acceptance. Automation defaults disabled; actuation is separately disabled.
- Implemented local automation and its limits are in [AUTOMATION.md](AUTOMATION.md).
  No gRPC, background service, root grant or full native/Web parity is claimed.

## Outstanding acceptance and feature work

- Pealayer is currently offline on this workstation. Direct playback, cue
  dispatch, mutual live application acknowledgement and its Pealayer→board
  route need a reachable approved host. A separate host delivery receipt does
  not establish Android connectivity or mutual live application acceptance.
- No AVR flash, reset, motion, relay/PWM write or physical tone is used merely
  to verify app/UI styling. Host/programming admission, physical recovery,
  flash/readback and firmware persistence still need explicit live acceptance.
- Native standalone means offline profiles/drafts today, not direct Android
  USB/Bluetooth serial ownership or a packaged Go/Rust coordinator. Clarify
  the intended standalone hardware route before implementing that expansion.
- Full Web/native parity is unfinished: graphical multi-track cue dragging,
  full RF/I2C/strip settings, discovery onboarding, palette/icon/folder editing,
  actual Android buzzer timing/audibility, tablet interaction acceptance and a
  production signing/update channel remain investigation/continuation items.
- The retained Pakar implementations are donor/reference coverage, not proof
  that their ESP-specific activities control the PCController board.
- Pealayer cue timing edits are native on Android. Its complete cue properties,
  channel modes, track routing and effect designer are not yet fully ported.

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

## Unified expert workspace redesign, source 0.1.4+5

The previous deployed design is explicitly rejected. This source pass removes
separate product navigation, simultaneous peer connection/disconnection buttons,
unavailable-only telemetry cards, dormant host token inputs and empty-state
narration. Live has one compact peer strip, phone two-column numeric telemetry,
short measured sparks and a single top-level emergency-stop action. Known relay
states have one contextual header action beside the state pill and channel menu.

Preferences now include custom color seeds and the original PakarController dark
surface/background, persistent branding, native Android task presentation and
one/both launcher aliases. Discovery integrates source-verified products,
unique vacant-role auto-connect, explicit Disconnect opt-out and Android network
changes without authority takeover. The coordinated peer upgrade modules are
wired to protected local RPC, optional sharing, incoming offer approval/expiry,
push/download, verified staging and separate permission/install/discard actions.

Focused source checks covered contextual connection cancellation and persisted
opt-out, sparse telemetry, dormant auth, native task adapter arguments, original
Pakar colors, RTL/enlarged text, current cue ownership, sliders, trailing channel
indicators, front-panel modes, unsupported LCD disappearance and update permission/
installer receipt semantics. Analyzer and Android Kotlin compilation passed.
Controlled phone/tablet renders use a recorded Cafe snapshot, 12 read-only measured
telemetry samples and generic Pealayer media fixtures. These are renderer previews,
not screenshots of deployed Android interaction or physical/audio acceptance.

An inherited suite run exposed obsolete UI expectations and the old unverified
MNDP fixture. Their current contracts were repaired with owner-scoped changes.
Further full inherited suites/CI were not repeated during this iteration. No
all-green CI or all-device acceptance claim is made.

That source checkpoint preceded the mobile bug-priority pass and the direct
saved-drafts confirmation recorded below. Design, unlocked UI, standalone shared
core/USB and physical mirroring/peer/firmware acceptance remain open.

## Mobile bug priority: MOSFET setpoints and relay layout

PWM edits now keep the drag/pending thumb through older snapshots until the
typed setter returns its native post-ACK readback. Pre-write read revisions are
fenced, owner/profile changes cancel old results and an uncertain write is never
replayed. Unknown levels use an empty bounded input rather than a false zero.
Protected PWM automation preserves fractional percentages and its JSON receipt.

Preferences save compact one-line relay cards or two-line explicit ON/OFF cards.
Unknown compact state offers explicit power choices; action callbacks pin the
rendered target and revalidate current catalog/lock/hidden state. Authored groups
are preserved; ungrouped MOSFET controls use the PWM group. Package-sharing
network changes refresh actual interfaces independently of auto-connect choices.

Eight focused pure widget/transport/layout regressions passed, alongside the
controlled renderer, analyzer and one ARM64 release APK build. Cafe read-only
queries reported PWM available with valid channel values; no physical output
mutation was performed. Screenshots are recorded-value renderer evidence.

David directly confirmed "All drafts saved; safe to update" for both Android
devices. The APK is 0.1.4+5, 19,861,439 bytes, development signed, SHA-256
`05100434e1695e6e3e2e8bc7d62bf5230c2f9c32f6e5ee1587f2b73e44504641`,
built from UI checkpoint `7200fe3b` atop the coordinated transport fixes.

| Device | Actual delivery | Installed identity |
| --- | --- | --- |
| Samsung SM-X906B / R52T60C6V9B | Initial streamed install failed to load/parse its temporary base.apk; one ordinary non-streamed `install -r` succeeded, retaining data. ControllerLauncher started. | 0.1.4 / code 5; installed base.apk SHA-256 matches the deliverable. |
| Samsung SM-S908E / R5CT44LZ42K | In-place install rejected: Requested internal only, but not enough space. 439,388 KiB available; vendor low-storage threshold 1 GiB. No unrelated data/cache was removed and no reserve bypass was used. | Still 0.1.2 / code 3; base.apk retains its prior `e298b2d2...54880df` hash. |

Both distinct devices were launched through the ordinary ControllerLauncher and
remain locked. Invalid-token ordered Intent requests reached their Dart routers
and returned JSON-RPC `-32001`. This proves rejection/routing, not authorized
transport, visual/touch, native launcher/task, peer APK, audible/segment timing or
physical PWM acceptance. The phone needs available storage before another update;
the continuous improvement goal and all unfinished user acceptance gates remain
active. No full suites, extra APK builds or CI were run for this delivery.

## Appearance and uncompact follow-up

The latest requested uncompact layout restores full peer connection cards with
the actual Connected pill and a full-width row for each available chart, including
separate lighting and audio temperature series. Compact mode retains its grid.
Disconnect is menu-only in both layouts; the compact strip also exposes Connect/
Cancel in its visible More menu so peer names fit. Explicit disconnect still
persists the discovery opt-out.

Preferences retain the former neutral light/dark surfaces and classic headers as
Pastel (muted), alongside the current Vibrant appearance. Header styles can follow
the selected appearance or use Classic, Icon tile, Accent band or Plain. Real
channel action buttons support Outlined, Soft fill and Solid fill, with the same
ownership, lock, pending PWM and safety guards. All choices persist independently.

Ten focused appearance/PWM/relay regressions passed; five appearance regressions
were rerun after the compact-menu adjustment. Analyzer and phone/tablet controlled
rendering passed. Renderer images replay the recorded read-only telemetry and
are not deployed-device visual or physical acceptance. Release version is 0.1.5+6;
one ARM64 release build passed from checkpoint `0db47657`. APK bytes: 19,861,303;
SHA-256: `999506a72adfb3271be5815b06210eb963cafecbf0927b4c57c6baa18f3a2935`.

The SM-X906B tablet updated in place using an ordinary non-streamed `install -r`;
installed version/code 0.1.5/6 and base.apk SHA-256 match this deliverable. The
SM-S908E phone remains 0.1.2/3 with its prior APK hash; current available storage
is 472,384 KiB, below its previously verified 1 GiB vendor low-storage threshold.
No repeat phone install, data removal or storage-reserve bypass was performed.

Both ordinary launchers were invoked and both invalid-token read-only app.ping
Intent requests reached Dart and returned -32001. The phone remained locked.
The tablet became unlocked during verification, with Termux in the foreground
when its current display was inspected; its live use was preserved. This is not
a deployed Flutter visual/touch test. Authorized RPC/peer flows, physical behavior,
standalone support and human acceptance remain open. The coordinated chat owns
the subsequent semantic relay-color/caption-alignment source follow-up; 0.1.5 is
the delivered appearance checkpoint, not proof of that subsequent requested fix.

## Front-panel loading scroll stability

Global command progress now reserves a constant two-pixel slot instead of inserting
and removing a sibling above the scroll viewport. Front-panel loading therefore
does not change viewport geometry, content extent or the current scroll offset.
The animation exists only while a command is pending.

Two focused phone/tablet widget regressions click a real front-panel widget through
a memory-only deferred RPC, verify exactly one command, and compare viewport,
button position, scroll extent and offset before/during/after acknowledgement.
Both passed; Flutter analysis passed. No real board action was sent. This source
fix is subsequent to delivered 0.1.5 and must be included in the next coordinated
APK; source regression proof is not installed-device acceptance.
