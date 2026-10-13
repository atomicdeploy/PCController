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

Relay power actions now use semantic green/red instead of the appearance accent,
including explicit ON/OFF rows, compact power icons and unknown-state menu choices.
Filled, tonal and outlined preferences remain supported; disabled actions resolve
to muted surface colors and all existing target/lock/E-STOP/busy guards remain.
Card captions, action labels, fact rows and state pills share outer-leading trim
for centered text beside fixed icon boxes without changing font size or wrapping.
The desktop companion reserves density/font-aware header height while retaining
matching caption/inline-editor slots. Dart analyzer passed; a pure style regression
was added but not executed in this root pass, per the user's tests/CI skip request.
These source changes do not supersede the last exact Android delivery receipt.

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

## Outputs organization

Channel icon/grip targets support handle-only drag/drop with edge auto-scrolling, including responsive
tablet grids. More offers Move earlier/later as a keyboard/accessibility alternative.
Reorder uses one native presentation transaction with expected_revision and the
complete same-kind rank set, including hidden controls. Same-kind channels may
move between folders; mixed hardware kinds and raw-seat folder assignments are
rejected. This changes host presentation metadata, not output state or wiring.

Tap a caption or use More > Rename for inline editing. Keyboard Done or the
checkmark saves; More > Cancel rename or Escape cancels. The caption/editor share
font, inset and a constant 44-pixel slot. Cancelled, unchanged and stale-owner
edits send no mutation. Applied names/order come from the authoritative full
catalog receipt, with identity-keyed widgets and a fence against older reads.
Uncertain writes are not replayed automatically.

Fourteen focused Outputs, PWM, progress-scroll and semantic-color checks passed across the focused passes,
including actual handle gestures, delayed acknowledgement, hidden-rank accounting,
owner replacement, cancellation and stale catalog reads. Analyzer and controlled
phone/tablet renders passed. No real output or host metadata was mutated. This
source targets the combined 0.1.6+7 APK, including the prior progress-slot and
semantic-caption fixes. Android delivery and human/physical acceptance are separate.

A short 1280x400 viewport exposed a separate vertical activity-rail overflow;
that small-height responsive-layout gate remains open.

### Combined 0.1.6 delivery

The ARM64 APK built from `884f1a8167d58c474df67dc16b4d40c95f144124` is
0.1.6/code 7, 19,926,839 bytes, SHA-256
`e4e2548d280aef3a84a70dc7a2062c32a229557518569e1e1ed6f20f3098ac5b`.
Its signature verified with the same inherited Android development certificate.
After the fresh direct confirmation "All drafts saved; update the tablet", one
ordinary non-streamed in-place install succeeded on SM-X906B/R52T60C6V9B.
Its installed version/code and base.apk hash match the deliverable. Data was retained;
the ordinary launcher started and an invalid-token read-only Intent reached Dart
and returned -32001. The tablet was locked at verification, so these receipts do
not prove real drag/rename, peer metadata persistence, visual or physical acceptance.

SM-S908E/R5CT44LZ42K remained 0.1.2/code 3 with its earlier APK hash and 504,996 KiB
available, below the previously verified 1 GiB vendor low-storage threshold. No
repeat install, data/cache removal or reserve bypass was performed. Its live
unlocked use was left intact. The continuous goal and unfinished acceptance gates
remain active; no full suites or CI iteration was run for this package.

## MOSFET readback stability

The reported jumping card exposed a source timing bug: native default status
freshness is 1500ms, but Flutter PWM polling runs every 2000ms. Using the former
to discard PWM readback caused known sliders to become Unknown inputs between
ordinary reads. PWM observations also incorrectly depended on unrelated status
freshness/availability despite a valid direct PWM response.

PWM now has an independent bounded readback window (at least 6000ms for its poll
cadence). Same-owner late, unavailable or incomplete reads retain the last valid
percentage; Stale/Last known presentation disables edit while keeping slider/card
geometry. Fresh complete readback restores current state. Changed owners and
explicit disconnect clear the ownership-bound cache. Unknown with no observation
still has no fabricated value. Current automation PWM follows its own freshness.

Ten focused freshness, PWM interaction and transport checks passed; the three
typed-transport checks were repeated after strengthening complete-receipt validation.
Read-only tablet observations showed the app running but display Dozing/black;
the bounded Cafe read-only sampling attempt timed out. Neither is live flicker
reproduction proof. No physical output or host metadata was written. This source
targets 0.1.7+8; package and device identity are recorded after delivery.

The coordinated chat subsequently recovered the exited Cafe controller through
its user-approved installed recovery task. Eight read-only samples after recovery
confirmed one stable host/generation, PWM available with all 16 channels, no RPC
errors, and 9–16ms read latency. The live peer advertises 1500ms freshness and
500ms status interval, confirming the cadence mismatch. The earlier timeouts were
the stopped host, not proof of PWM-age behavior. No output mutation occurred here.

One ARM64 build from `19672cf0f06672be6e596d9412f5baa558486d76` passed. APK
0.1.7/code 8 is 19,926,839 bytes, SHA-256
`348528f8c311b942bae0534940592a3e3d6281a09e2d2c5b9022fb34d7c8a7e9`,
with its inherited development signature verified. Under the recent direct
saved-drafts/update confirmation and standing bug-fix delivery authorization,
one ordinary non-streamed in-place tablet install succeeded. Installed version/code
and base.apk hash match; the ordinary launcher and read-only invalid-token Intent
returned the expected routing/auth rejection. The tablet was locked/Dozing, so
actual on-screen stability still needs human verification. The phone remains
0.1.2/code 3 with 503,148 KiB available; no retry or data removal was performed.

## Android buzzer timing and waveform

The user reports missing, clipped, crackly, imprecise and muffled Android mirroring.
The referenced Linux work was read and checked against the current Go receiver.
Its bounded 50ms playout cushion, 250ms fresh-observation rebasing, full-duration
handoff, clock-wrap and immediate Stop policy informed this Android change.

The old Dart path published a session before native open completed, omitted
startup notes, and subtracted handoff/timer delays from short notes. The old
native singleton replaced burst notes, paused/flushed PCM on every transition
and wall-clock expiry, and could discard non-blocking short transfers. Its
sine-only waveform and 3ms ramps also attenuated very short beeps.

Android now receives ahead-of-time note scheduling on a persistent audio-frame
timeline. Startup bursts wait for one receiver open; fresh late notes/followers
shift equally with their original duration, while stale queued receipts expire.
PCM duration uses frame counts, including 48 frames for a 1ms note at 48kHz.
Partial writes retain the original tail/phase/envelope. Ordinary transitions
do not flush; explicit Stop, lost owner/focus/lifecycle and the independent
six-second lease still cut off sound. Temporary focus ducking reduces volume
and retains the receiver. A PolyBLEP square wave adds brighter harmonics with
0.25ms bounded edge ramps and headroom instead of a sine-only sound.

Thirteen focused Dart current/legacy mirroring tests and seven native PCM tests
passed, including delayed open, 1/3ms notes, clock wrap, stale startup, Stop/open
and owner races, delayed cadence, partial writes/chunk continuity, queue bounds,
silence, bounded integer conversion and harmonic/no-clipping checks. These are source/synthetic evidence,
not a measurement of Android route latency, real peer packet loss or speaker
acoustics. The selected-board bridge-source fence remains; explicit routed
source ownership is still needed for unrelated bridged-board playback.
This source targets 0.1.8+9. Device delivery and human listening are recorded
separately; no physical board tone, relay, motor or firmware action was sent.

One ARM64 build from db8bcb7b passed. APK 0.1.8/code 9 is 19,926,839 bytes,
SHA-256 fc40ac2748c6bb241ca273e1bc6083d469cf6907294e5ca7cb1714370c88b57f;
its inherited Android development signature verified. One ordinary non-streamed
in-place tablet install succeeded under the recent saved-drafts/update confirmation
and continuing bug-fix delivery authorization. Installed version/code/base.apk
hash match; the ordinary launcher and read-only invalid-token Intent passed
routing/auth-rejection checks. The tablet remained locked, so no actual audible
receiver/peer/speaker acceptance is claimed. A listening comparison was requested.
The phone remains 0.1.2/code 3 with 500,792 KiB free, below its prior verified
1 GiB installer threshold; no repeated install, data removal or reserve bypass.
The continuous goal stays active. No full inherited suite/CI iteration was run.

## Pull-to-refresh

All main activity views now support a downward pull at the top, including
content too short to scroll. A full-height scroll viewport and overlay spinner
preserve layout, scroll position and the fixed global progress slot. Refresh
uses selected PCController/Pealayer readback; Firmware additionally reads its
artifact catalog and current update status. Network and offline refresh scan
nearby peers without auto-connecting or changing saved connection intent.
Unsaved text remains intact, and read failures use an overlay SnackBar.

Concurrent refreshes join the same current-owner read/discovery future. A
disconnect retires the old read without blocking or overwriting its replacement.
Existing catalog revision, PWM-write fencing and command ownership remain.
Pulling never sends a hardware action, prepares/downloads/uploads firmware,
retries a mutation or changes a selected connection.

Six focused refresh checks and both existing phone/tablet front-panel progress
geometry regressions passed. They cover actual pull gestures on short pages,
coalesced reads, response waiting, read failures, draft preservation, replacement
owners, read-only firmware RPCs and discovery without connection. This source
targets 0.1.9+10; live device gesture acceptance and delivery are recorded separately.

Three PWM ownership/readback guards and one older-catalog fence check also
passed, making twelve relevant checks in this pass. Final analyzer and one
ARM64 build passed. APK from 0d978d6d is 0.1.9/code 10, 19,992,375 bytes,
SHA-256 65c380a077f69c915b6b9b9f793ad3af307ed43b97f0d2940e680a4b28b79e23,
with the inherited Android development signature verified. One ordinary
non-streamed in-place tablet install succeeded; installed version/code/base hash
match. The tablet's foreground video app was preserved, so the controller was
not launched over it and live gesture acceptance remains pending.
The phone remains 0.1.2/code 3 with 499,652 KiB free below its previously verified
1 GiB installer threshold. No retry, data removal, physical action or firmware
operation was performed. Concurrent generated Web outputs were left untouched.

## PU850 endpoint and dialog import, batch 1

The full 5,429-line MainActivity and active/inactive non-business network/UI
helpers and resource variants were reviewed without modifying or running PU850.
PU850-IMPORT-LEDGER.md records findings, exclusions, source digests and U01–U14
phased migration statuses. The first batch adds localized address normalization,
modern host/URL/IPv6/port validation, inline errors and canonical preview,
bounded same-host product detection and explicit offline targets, responsive
connection/confirmation dialogs and selected-peer native-network reconnect wakes.
Opening attempts coalesce; explicit disconnect, owner fences, typed relay
routing and command non-replay remain intact. Pastel/Vibrant styles are retained;
PU850 Droid Sans Mono is ported with its original digest and bundled license.

Twenty-five focused checks and one source-renderer check passed; analyzer clean.
Phone/tablet front-panel busy geometry checks still pass. Three modal PNGs are
source fixtures only. Read-only LAN probes to the previous Cafe :8787/:8080
addresses timed out; live endpoint/reconnect behavior and human design acceptance
remain open. No physical action or PU850 business command was sent. Source
targets 0.1.10+11; package/device identity is recorded separately after delivery.

## Batch 1 package/device receipt

One ARM64 release build from c561c207edf75bfc347b6bcda24ee1249e50cb9c passed.
APK 0.1.10/code 11 is 20,133,328 bytes, SHA-256
`3e40eda6c0d36589ebe04081d46dc578464555eee40592662aab2fbb97e4593d`.
Its inherited Android development signature verified. One ordinary non-streamed,
in-place tablet install succeeded under the existing saved-drafts/update and
continuing delivery authorization. Installed version/code/base APK hash match.
The tablet's active video foreground was preserved; no controller launch or
physical output/audio/firmware action was sent. Live interaction is still open.

The deduplicated SM-S908E phone remains 0.1.2/code 3, with 499,840 KiB available
below its previously verified 1 GiB installer threshold. No repeated install,
data removal, reserve bypass or unlock was attempted. The continuous acceptance
goal remains active. android-0.1.10-delivery.json records exact identities and
source/device/physical acceptance separately.

## Latest contextual utility rejection

The complete requirements are retained in root/mobile AGENTS.md and the scoped
source/device audit is CONTEXTUAL-UTILITY-AUDIT.md. This wave targets 0.1.11+12:
one lifecycle connection toggle, contextual effect cancellation and live cards,
clickable guarded relay indicators, balanced full-width action rows, kind-scoped
folder dropdown/dialog, event-first readback and only degraded-transport polling.
Thirty focused checks and one source renderer passed. Native Cafe directory and
Pealayer active-protocol readbacks are recorded separately from Android multicast,
OS launcher/branding, peer install, physical/audio and human acceptance.

## Package/device receipt

Final analyzer and one ARM64 release build passed. Source checkpoint
0eef88bb5736b7010efa990fc65278c0531641de produced 0.1.11/code 12,
20,133,536 bytes, SHA-256
`0af60c9b04f34964f19a0f072cd87df13fb6decd3bf585789bb9647c2986ba37`.
The inherited Android development signer verified. One ordinary non-streamed
in-place tablet install succeeded under continuing delivery authorization and
the recent saved-drafts/update confirmation. Installed version/code/base hash
match. Active video foreground remained intact; the controller was not launched
over it. No physical relay/motor/beep/firmware or player action was sent.

The deduplicated phone remains 0.1.2/code 3, with 498,272 KiB available below
its previously verified 1 GiB installer threshold. No repeated install, data
removal, reserve bypass or unlock was attempted. Exact receipts are in
android-0.1.11-delivery.json. Live Android discovery, OS branding/launcher changes,
two-device upgrade/installation consent, full egui parity and physical/audio/human
acceptance remain open. The continuous goal remains active.

## Go help and usage parity wave

0.1.12+13 adds Controller tools, with 50 useful advertised native command families
and dedicated settings/name/display/lighting/routing/melody/recording/RF forms.
GO-FLUTTER-PARITY.md records the help/runtime provenance, parser findings, exact
native limits, dedicated versus workbench/platform scope and remaining gaps.
Owner/E-STOP/uncertain fences, priority Stop, reviewed release and typed-relay
third-peer exclusions apply. EEPROM preflight conflicts retain edited drafts;
native CAS remains a gap. Native acknowledged-but-unconfirmed PWM stays connected,
retains stale last values and never retries. Expert PWM commands share read fencing.
Tools have no periodic polling; entry/explicit refresh reads preserve drafts.

42 focused source checks, three tools/dialog widget checks and one phone/tablet
source renderer passed; analyzer clean. Existing front-panel progress and pull
refresh viewport checks remain passing. Controlled renders are not device screenshots.
No physical relay/motor/beep/RF/firmware/player action was sent. Final package/device
receipt follows separately. Continuous acceptance goal remains active.

## Go parity package/device receipt

One ARM64 release build from 1f9f2b2071d754222834da3d87b1cec3afa09547
produced 0.1.12/code 13, 20,266,596 bytes, SHA-256
`e124b356e95c41765636791c61e75b4347db9d14663ee44bf7094f67edfbb5d8`.
The inherited Android development signer verified. One ordinary non-streamed
in-place tablet install succeeded; version/code/base APK hash match. The tablet
is locked and retains its observed video activity/task; no controller launch
replaced it. No physical output/audio/RF/firmware/player action was sent.

The deduplicated phone remains 0.1.2/code 3, with 491,588 KiB available below
its previously verified 1 GiB installer threshold. No repeated install, data
removal, reserve bypass or unlock was attempted. android-0.1.12-delivery.json
records current identities and open gates. Source/render/build/delivery are
separate from live physical/audio/human acceptance. Continuous goal active.
