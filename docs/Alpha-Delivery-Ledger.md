# PCController alpha delivery ledger

> [!IMPORTANT]
> This is a chronology-preserving alpha delivery and requirements ledger imported from
> the former Desktop master tracker on 2026-08-24. Historical claims are
> preserved as checkpoint evidence; they are not automatically current and do
> not override the maintained product guides or [Project acceptance](Project-Checklist.md).
> A feature is complete only when its source, automated-test, deployed-artifact,
> and physical or interactive acceptance states are each supported.

> [!NOTE]
> Publication redactions generalize only private usernames, local filesystem
> paths, machine hostnames, serial-port assignments, process IDs, and transient
> local artifact identifiers. Project requirements, chronology, issue/PR
> numbers, commit and artifact identities, measured results, and unresolved
> work are retained. Explicit PR references link to the canonical repository;
> unqualified `#NNN` references use the shared GitHub issue/PR number space.

- Repository: [atomicdeploy/PCController](https://github.com/atomicdeploy/PCController)
- Project board: GitHub Project 3
- Maintained open-work index: [Requirements backlog](Requirements-Backlog.md)
- Migration baseline: `b99571f2f8d7caa1eb95fcc30eb927bd3ef70bd7`, which
  records the merge of [PR #282](https://github.com/atomicdeploy/PCController/pull/282).

---

Source snapshot updated 2026-08-21 04:23 (original local time)

## CURRENT ROOT MISSION — 2026-08-21
- Deliver a quiet, current, end-to-end PCController system: canonical source,
  firmware, host executable, embedded Web bundle, local/remote clients, live
  CAFE board, documentation, GitHub issues/PRs/project, and origin host handoff.
- Sound safety is the first gate. `controller-illumination.exe` is stopped and
  the `<local scheduled launcher>` is temporarily disabled. `UART_PORT` is
  enumerated as CH340, but both the current-source and previous project hosts
  fail before UART traffic with `Invalid serial port`; Windows PnP reports the
  driver healthy. Silent was NOT rewritten or re-read in this session. Replug
  CH340 while RESET is low/PROG is asserted before normal boot. USBasp is absent
  and must not be assumed.
- [PR #271](https://github.com/atomicdeploy/PCController/pull/271) (unified KEY motion/exit gestures) was made Ready and squash-merged to
  main at 42fc92d. [PR #276](https://github.com/atomicdeploy/PCController/pull/276) (complete illumination controls) passed 23/23 after
  the bounded-PWM CodeQL fix and was made Ready and squash-merged to main at
  7e3dbad. [PR #282](https://github.com/atomicdeploy/PCController/pull/282) carries the physical K1..K4 -> logical K4..K1 normalization;
  it is Ready and rebased onto 7e3dbad at 031f33c. Its focused production-
  adapter test passes; the first CI run exposed the tight EEPROM-menu-labels
  profile, so the bit reversal was compacted and that exact profile now builds
  at 32,354/32,384 bytes (18 bytes physical headroom). Merge only after the
  refreshed required checks complete.

## ACTIVE DELIVERY REGISTER — DO NOT DROP DURING COMPACTION
1. Quiet live-board session: reinitialize CH340 without application boot,
   persist/read back Silent=true, leave it muted throughout testing, and restore
   the edge launcher only after installing the current host.
2. Live seven-segment acceptance: enumerate every enabled/disabled page; test
   rollover, single-step and accelerated holds, four-key/long-hold exit,
   KEY-mapped Side A/B motion, host/RF/front-panel presentation parity, LErn,
   mute/unmute menu behavior, and event-driven Web/TUI updates after DTR reset.
3. Requested front-panel profile: one numbered relay page; one KEY page with
   K1..K4 mapped to Side A/B Up/Down; restore Lite; merge PWM/uPWM but keep it
   disabled; disable BT-LED detection and treat any future BT input active-HIGH;
   host-backed Save/Discard with offline autosave; keep LErn when host is present.
4. Input/feedback repair: reverse physical K1,K2,K3,K4 wiring into logical
   K4,K3,K2,K1 at the hardware boundary; restore rollover on beep and every
   numeric menu; make mute->beep produce one allowed confirmation; restore
   feedback, door open/close, and distinct motion tones without violating Silent.
5. Output repair: make enclosure illumination Off/Auto/On plus on/off endpoints,
   applied/target state and EEPROM durability available on Web/TUI/CLI/IPC/API;
   diagnose Auto+door behavior and PWM/MOSFET index 6 staying on; provide smooth
   linear/eased PWM destinations and addressable-strip color commands.
6. Host/UART resilience: diagnose CH340 GetCommState failure; 1-second UART
   response handling; bounded retries then configurable exponential reconnect
   backoff capped at 15 seconds; reconnect by authenticated USB identity across
   COM changes; emit reset/disconnect/reconnect/state events to every client;
   do not spam blank/nonresponsive boards; fix port.process.changed unknown.
7. Firmware/host deployment: build from current main with the current embedded
   Web bundle, use Arduino/global toolchains by default, flash through UART/Urboot
   when available, use USBasp only for initialization or fallback, validate HELLO,
   preserve Silent, replace the installed host, and verify supervisor/LAN clients.
8. Macro completion: accurate MCU deltas, connected recording beyond the AVR
   recovery prefix, offline truncation truth, circular/recovery behavior, names/
   categories, relay/motion/PWM/MOSFET/beep/display/message actions, playback and
   live monitor across Web/TUI/CLI/IPC/API/RPC, faithful timing under telemetry.
9. RGB/LED ownership: native descriptor engine only at >=50/60 FPS; eliminate
   host frame renderer and phase resets; make RF purple preempt green/red quickly
   without jitter and restore prior owner; retain offline response and document
   flash/EEPROM/profile costs. Investigate shared reusable engines for RGB,
   user PWM, and addressable strips without compromising timing or protocols.
10. Startup/audio architecture: retain TonePlayer engine and AudioCues policy;
    move finishMelody/errorBeep to host; restore autonomous door/motion cues;
    design CRC'd EEPROM startup opcode/melody records to save flash and allow
    first-boot peripheral setup; never silently remove features—gate/capability
    and publish the exact removal/cost/reason instead.
11. Programming/tooling: finish `board initialize` versus full provision and
    `board blank` using only project surfaces; fastest fuse-first XTAL recovery,
    normal-speed retry, bootloader/firmware/EEPROM/fuse/lock readback, timed
    benchmarks, factory blank recognition, TEST-board flow, BOM stripping,
    hash-deduplicated dated backups, no `allow-incomplete-backup`, global Arduino
    path/config/env/flag priority, and CLI/TUI/API/remote parity.
12. Host surfaces: full Charm TUI is default for secondary IPC clients; remove
    simple-console fallback except explicit `--simple`; board name CAFE get/set/
    detect/edit everywhere; `beep` canonical now and future nested `buzzer beep`
    / `buzzer silent`; no stale `buzzer` command references during alpha.
13. Web/UI acceptance: current-bundle enforcement, true multi-client server push,
    no manual refresh for fresh socket state, reset recovery, correct cards/
    charts/menus/sidebar/context menus/tables/icons/favicon, filtered debug-only
    spam, actionable shared messaging, editable/reorderable peripheral metadata,
    and comprehensive overview controls.
14. Windows integration: actual shared app icon on modern toast, legacy balloon
    fallback with automatic selection, correlated sync/async actions and outcomes.
15. Build/docs/release: concise Unicode/Markdown feature/profile/capability table
    in build.cmd, Go tooling and CI with links to source/docs; complete ownership
    atlas for board/host/shared components and interface exceptions; remove alpha
    schema-history baggage until release maturity; evaluate boot metadata use.
16. Operations/GitHub: merge completed PRs immediately; leave Draft only while
    incomplete; reconcile before branching; push/fetch periodically; update every
    issue/PR/project checkpoint; sync origin host without publishing machine details;
    archive obsolete EXEs/hash-duplicate backups into one pending-delete folder.
17. Sensor commissioning/data validity: replace the DS swap bit with CRC'd EEPROM
    ROM-to-role identifiers; let the host identify enclosure/lighting sensors by
    forcing illumination MAX then 0 even with the door closed, correlate readings,
    restore the prior output, and require user confirmation/manual selection when
    inference is inconclusive. Never expose absent/invalid -327 C values as real.
18. Authentication alpha policy: keep all auth/authZ disabled until explicitly
    resumed. The deferred design remains: localhost/native unauthenticated by
    default with optional process-image restriction; remote authenticated by
    default; persistent pre-login session identity; immediate WS/Socket.IO login
    request; OS credentials or local-toast approval; then update is_logged_in and
    complete client/browser/TUI/device metadata. Do not partially ship this design.
19. Autonomous/profile preservation: keep the reusable TonePlayer, status RGB,
    motion, MOSFET/PWM and strip engines intact; gate autonomous controllers/data
    with truthful build flags/capabilities/profiles. Publish what requires a host,
    exact flash/SRAM/EEPROM cost, and every removed obsolete/stale/redundant path
    with reason. Keep the task scheduler as an optional profile for larger MCUs.
20. BT-audio and inputs: discover/document the actual BT status indicator modes,
    keep current BT detection disabled in this profile, and treat any future BT
    status input as active-HIGH rather than inheriting active-LOW input semantics.
21. Architecture/performance backlog: investigate sustained host CPU and timeline
    growth; restructure the repository to conventional board/host/Web/tooling/docs
    domains; make menu/page/opcode/profile definitions one canonical source that
    can generate firmware, Go contracts, Web/TUI/CLI/API catalogs and distinct docs.
22. Boot metadata/storage: do not place ordinary mutable project storage in Urboot
    metadata ad hoc. Document its actual ownership and investigate safe, versioned
    use for immutable board capability/build identity while normal EEPROM remains
    the mutable startup/profile/melody store.

## LATEST AUTHORITATIVE UPDATE — 2026-08-12 16:48
- The original rollout JSONL was re-read turn by turn. The physical board
  programming ledger below now distinguishes source, automated test, live
  deployment, and human/physical acceptance instead of treating them as one
  completion flag.
- RGB regression fixed, committed and pushed: dc3d8a3 on
  `agent/rgb-heartbeat-ownership`; [PR #181](https://github.com/atomicdeploy/PCController/pull/181) passed the firmware, repository, and
  five-platform VirtualBoard gates and merged into canonical delivery [PR #174](https://github.com/atomicdeploy/PCController/pull/174).
- Guarded live-board backup/flash/readback completed on `UART_PORT`. HELLO now reports
  build C22E8666 / timestamp 260812163626. The exact app image SHA-256 is
  66606CD796EDC8A94FE1C2966B7F5A29CD8071C2F97C69BC78E0D70CA244D44C.
  The normal flash lifecycle incorrectly restored Silent off even though the
  fresh backup held Silent on. This was detected immediately, corrected through
  the shared live settings path, and authoritatively re-read as flags=1:
  Silent=true, Programming=false, persisted=true. No buzzer command was sent.
  The lifecycle regression and required tests are recorded on GitHub #56.
- Root cause was deterministic: every two-second PROGRAM_STATE liveness
  heartbeat released RGB ownership and restarted the 1.6-second native effect.
  Program-state and LED ownership are now independent, and only a byte-identical
  full 12-byte effect descriptor is phase-idempotent.
- Firmware/VirtualBoard CTest passed 6/6. AVR uses 32,338/32,384 bytes (46 free),
  with an estimated 261-byte SRAM margin. A 12.5-second live PCA observation
  crossed more than six heartbeats: the 1.6-second waveform remained continuous
  and the old two-second reset signature disappeared. The live autonomous color
  was red because lighting temperature was ~57.5 C and HOT warning correctly
  preempted decorative blue/host feedback.
- The exact unfinished test-board chain now has a single owner: GitHub #182,
  “Complete physical USBasp slow-fuse and second-board initialization
  acceptance.” USBasp is not presently enumerated, so the physical slow-fuse,
  TEST-01 blank/readback, and second-board guided TUI/WebUI passes remain open.

## GITHUB TWO-WAY RECONCILIATION — 2026-08-12 16:58
- Canonical delivery branch merged [PR #181](https://github.com/atomicdeploy/PCController/pull/181) at 57e079f. The live `UART_PORT` board is
  already on the verified C22E8666 image; GitHub source, tracker, and physical
  evidence agree for the focused RGB heartbeat repair.
- #56 owns the newly discovered programming-lifecycle regression: restore the
  original user Silent preference after a normal flash, clearing only temporary
  Programming state; authoritative settings persistence must be queried.
- #182 owns the complete remaining USBasp/test-board physical chain.
- #44 owns macro live timing, continuous recording/refill, cancellation, and
  cross-surface physical acceptance. #27 owns physical front-panel gestures;
  both share controller-loop latency and must coordinate.
- #183 remains open only for future RGB/profile/preview/VirtualBoard unification.
  It must not replace or regress the deployed [PR #181](https://github.com/atomicdeploy/PCController/pull/181) heartbeat repair.
- #174 received a delivery checkpoint linking all remaining owners. GitHub is
  the source of truth for source/issue state; this file is the local concise
  operational memory and must be updated after every live board mutation.

## REQUIREMENT-LEDGER CORRECTION — ORIGINAL JSONL RE-READ
- Canonical source for this ledger is the original Codex rollout JSONL. It now
  contains 100 user turns including the current correction. Requirements must
  not be reconstructed only from compacted summaries, issue titles, or PRs.
- Every requirement has four independent completion states: implemented in
  source; validated in automated tests; deployed to the intended machine/board;
  physically or interactively accepted. A grouped feature is not complete if
  any requested sub-step remains open.

## TEST-BOARD / USBASP PROGRAMMING CONTRACT — STILL OPEN
- Discover a newly connected ATmega328P through both USBasp and UART when both
  are present. Missing UART permits bootloader-only completion, but every serial
  check must be explicitly reported as skipped.
- Provision missing Arduino CLI, selected FQBN core, programmer, libraries and
  packages. Support the installed global Arduino CLI and a freshly downloaded
  managed/portable CLI from the same Go/TUI workflow.
- Attempt USBasp at normal speed. If the initial target-clock exchange fails,
  use conservative `-B32` only to apply the selected MiniCore fuse policy, then
  probe again at normal speed. Install the bootloader at normal speed whenever
  the repaired clock allows it; use slow bootloader programming only if the
  normal retry still fails.
- Before mutation, capture signature, complete flash, complete EEPROM, fuse and
  lock-bit evidence. Install and verify the core-provided bootloader/fuse/lock
  policy, then program and read back default/application EEPROM and firmware.
- With UART present, complete first-boot discovery, HELLO/ping, diagnostics,
  settings/default application, safe-output verification, peripheral discovery,
  and tolerance of absent INA/PCA/DS/LCD modules.
- Give the first test board a name of at most eight characters; demonstrate the
  complete flow through CLI while the TUI shows progress. Stream the requested
  test melody only when explicitly unmuted/authorized.
- Back up, then blank the first test board: application, bootloader and EEPROM
  must read back erased while the verified fuse configuration remains intact.
- Guide the user through Programming / Board Initialization in TUI/WebUI for the
  second board: select detected USBasp/UART, name it, review backup/safety, and
  initialize. The third board is the live production/feature-test board.
- Historical evidence is mixed, not absent. TEST-01 was physically initialized
  once: normal USBasp failed; `-B32` succeeded; a complete backup was captured;
  fuses changed 62/D9/FF -> F7/D7/FD; the MiniCore bootloader was installed and
  re-read; UART firmware/readback and HELLO build 6B075495 passed; the <=8-byte
  name persisted; missing peripherals were nonfatal; and `edge-ready` played.
- That physical run predated the refined algorithm and kept slow speed for the
  whole transaction. Commit 42361c0 and current tests implement slow fuse-only
  repair -> normal-speed reprobe -> normal bootloader, retaining slow only when
  the fast retry still fails, but this revised sequence has never run on real
  USBasp hardware. TEST-01 was never blanked/read back, and the second board was
  never initialized through the guided TUI/WebUI flow. Track with #29, #69-#75,
  #88 and #112. The exact multi-board acceptance chain is now owned by #182.

## LIVE RGB DOUBLE-RISE REGRESSION — FIXED, FLASHED, AND MEASURED
- Previous deployed source/host was a7e1b37 and board build 7FE34878 on `UART_PORT`.
- Host sends PROGRAM_STATE every two seconds as a presence heartbeat. The exact
  flashed firmware clears HOST_STATUS_OVERRIDE and calls cancelEffect() for
  every heartbeat, even when Idle/Running did not change. The host LED arbiter
  sees no policy change and does not reassert its descriptor, so firmware falls
  back to local blue Disconnected/Waiting breathe at phase zero.
- Passive live PCA channel-15 samples repeat at exactly 2.000-second intervals,
  proving deterministic phase reset rather than random PWM noise.
- Implemented repair: PROGRAM_STATE no longer destructively changes LED ownership;
  byte-identical continuous STATUS_EFFECT descriptors are phase-idempotent while
  same-kind descriptors with changed parameters still apply. Tests cover more
  than five heartbeats, a genuine program-state transition, explicit cancel,
  and native restore; live PCA observation covered 12.5 seconds.
- [PR #181](https://github.com/atomicdeploy/PCController/pull/181) contains the exact canonical fix and physical evidence. Draft [PR #180](https://github.com/atomicdeploy/PCController/pull/180)
  is on a divergent Linux branch and fixes a related but different
  ownership defect. Do not merge/cherry-pick it wholesale; port and test the
  relevant semantics on exact [PR #174](https://github.com/atomicdeploy/PCController/pull/174)/a7 source. The live board now runs
  C22E8666 and no longer shows the two-second reset signature.

## ACTIVE ACCEPTANCE CONTINUATION
- Canonical delivery [PR #174](https://github.com/atomicdeploy/PCController/pull/174) and remote branch `agent/webui-delivery-live` are
  now at exact checkpoint 34943af655628bd78db7066deb7c096101723d91.
  The edge-host and dedicated clean origin-host handoff worktrees are synchronized to
  that exact commit; neither worktree is dirty.
- Re-read the supplied WebUI checklist and the complete relevant user-request
  history. The public, source-backed acceptance contract is now
  `docs/Live-Delivery-Acceptance.md`; repository policy passes with 52 required
  files and 795 tracked/unignored source files.
- Reproduced a real live regression: `display segments --duration 2s -- SYNC`
  changed the board but neither of two connected browser clients updated. The
  exact cause is that the production build compiles
  `PCCONTROLLER_ENABLE_ASYNC_PRESENTATION_EVENTS=0`, so the MCU event producer
  is absent even though host/Web consumers exist. A size-gated push fix and
  two-client live acceptance are mandatory before merge.
- Reproduced the mixed macro path with harmless relay-off, motion-stop,
  PWM-off, and display steps. It recorded MCU deltas and played 5/5 with zero
  dispatch errors/underruns, but `buzzer 0 0` rejected the documented stop
  command and one display step exceeded the current timing bound by 5.352 ms.
  Both are active blockers; the timing threshold will not be relaxed merely to
  hide scheduling jitter.
- App-instance heartbeat updates are now state events rather than ordinary
  activity, closing the visible event-feed spam path across clients. Focused
  Go tests pass.
- Superseded PRs #142, #151–#153, #158, #167–#170, #173, and #175 were closed
  with durable cross-reference comments. Their intended work is already
  integrated or replaced on #174; no unique source was discarded. Independent
  Linux [PR #132](https://github.com/atomicdeploy/PCController/pull/132) remains open for a post-#174 rebase; workflow PRs #143/#144 and
  failing dependency PRs #146/#147 retain separate dispositions.
- Parallel delivery lanes are active for the remaining WebUI acceptance,
  continuous macro capture/all-surface state, and actionable messaging/client
  fan-out. No new live host or board image has been deployed from this interim
  checkpoint yet.

## CANONICAL SOURCES
- GitHub repository: https://github.com/atomicdeploy/PCController
- Project board: GitHub Project 3
- GitHub is the source of truth for source, issue state, PRs, and agent handoff.
- The edge and origin hosts keep machine-specific ports, paths, data, credentials, and configuration local.

## LIVE DELIVERY STATE
- Live board UART: `UART_PORT` (CH340).
- Live application SHA-256:
  4BC182A3CFF33CDA74049207DCFC5EE9C1FF47BF46EFD5E762D67F7C6C495FC2
- Board HELLO schema 4 is verified: build 25F2DC83, timestamp 260812074002,
  capabilities 0x917DFFBF, full-peripheral profile, features 0x49.
- The live image contains the actual MCU-owned RGB descriptor engine at a 16 ms
  cadence (62.5 FPS). Capability bit 28 is enabled; EEPROM profile/async-push
  bits 29/30 remain disabled truthfully. Host compatibility-frame streaming is
  gone. A 1.5-second live idle recording captured zero RGB/status actions.
- Firmware fit: 32,284/32,384 application bytes, 88 free; static SRAM 1,466;
  estimated peak 1,785/2,048 and estimated margin 263 bytes.
- EEPROM state verified after delivery: silent=true, programming_latch=false.
- Autonomous status brightness was changed from 0 to 255 through the shared live
  settings path so host-disconnect/local modes remain visible at full scale.
  Immediate readback matched; authoritative polling reached persisted=true in
  2.174 seconds; silent remained true.
- No motion, relay, MOSFET, or buzzer action was issued during delivery.
- Delivered host is running on the edge machine at http://edge-host.example:8787/ and
  http://edge-host.local.example:8787/. Localhost, edge-host.example, and edge-host.local.example return HTTP
  200 with byte-identical current HTML (SHA-256
  31BFF1257B63EB30752157A327212C1FAC55BD994F206AAC8F89EB0E045CC683).
- The origin host independently fetched both URLs after the replacement and received
  HTTP 200, 1,390 bytes, and that same SHA-256. Windows firewall rule
  `PCController-Web-8787` allows inbound TCP/8787 for LocalSubnet only across
  Domain/Private/Public profiles; it is port-scoped, not tied to a versioned EXE.
- The live host process ran the verified exact-current package
  `<local packaged-host build>`. Packed executable SHA-256:
  331D836FD3C6D7DEE95FD441569505373B238031646DDD23735E1B3FC71B7CE0.
  Embedded Web bundle SHA-256:
  9D3E223B2B87FAB0B73CD10F05DABC7BB618A7AA86C28918CBC2A57AFDDF919E.
  Source identity: DF0380BC5F16CE1E4E21FF6D3CE6450862C0787B89838FFF423FB6339503C7F5.
  Package inventory root:
  040162A23F845D53545CA9EB0F87FBE50C8A3738126A6985760716E227293D08.
  Exact-commit focused tests and vet passed; packaging rebuilt/typechecked Web,
  embedded resources, the Go host, UPX, runtime identity, C ABI, and inventory.
  The replacement host automatically reconnected to `UART_PORT` with zero framing/CRC
  errors and authoritative board settings still report silent=true/persisted.
- The host currently exposes deliberate alpha LAN access while auth is deferred.
  Restricting peer hosts through one config/env/flag policy is tracked by #156.
- [PR #176](https://github.com/atomicdeploy/PCController/pull/176) is Ready/verified and merged into the shared delivery base at
  `d9b56e6e1f5340659da3547bb708c4d36cf0bb85`; the delivered source head was
  `e7a77189caed135a15f06facd8561a69e5cef7ac`. Their trees are byte-identical.
  Parent [PR #174](https://github.com/atomicdeploy/PCController/pull/174) is now at `500fd3ae115ab55a64c408355a1a2285883b4aa2`:
  it explicitly checks display timing before uint16 conversion to close two
  CodeQL alerts; focused tests/vet passed and every hosted repository, CodeQL,
  AVR, VirtualBoard, and five-platform host check is now green.
- UPX 5.2.0 is installed system-wide in Program Files, discoverable from the
  machine PATH, and verified at SHA-256
  F4C0CC7ACA0F1FF0D0B750E966B44139F2FA1A2DB7281F48FC52194400712E1D.
  Managed Go/toolchain bootstrap acceptance is recorded on #60.

## LIVE MACRO ACCEPTANCE
- Recorded a named/category-tagged diagnostic macro with two ordinary
  seven-segment DisplayText actions through the shared command path.
- Stored exact MCU delta: 721,856 microseconds.
- Playback completed 2/2 steps with faithful=true, 0 underruns, 0 timing
  violations, and maximum timing error 952 microseconds.
- Live monitor observed playing step 1/2 then completed step 2/2 with an empty
  board buffer. Silent stayed true; active relays/keys stayed zero; framing/CRC
  stayed zero. The diagnostic macro was deleted after acceptance; DayPower was
  preserved untouched.
- Compact full-peripheral firmware uses its ordinary MCU segment renderer for
  display macros. MCU owns frames/hold timing; host owns only optional repeat
  boundaries when the larger autonomous segment scheduler is compiled out.
- Durable evidence: [PR #176](https://github.com/atomicdeploy/PCController/pull/176) issuecomment-5262352976; #44
  issuecomment-5262353268; #21 issuecomment-5262353554; #60
  issuecomment-5262353862.

## CURRENT HIGH-PRIORITY IMPLEMENTATION
- #153: integrated firmware/host delivery PR (USB COM reassignment, macro/HELLO
  contract, display/auth/offline UI fixes) — await normal GitHub CI disposition.
- #154: reconnect board after USB COM reassignment and broadcast lifecycle events.
- #155: harden Urboot flashing handoff/prevalidation/silent recovery.
- #156: unified remote-host allow-list plus flags > environment > config > defaults.
- #157: prevent firmware identity reservation/static-data overlap.
- #176/#44: exact-timed macro recording/recovery/playback plus the compact
  native status renderer. Source, live flash, live host, exact-delta display
  recording, board playback, and monitoring are now delivered. Physical-key
  recording and loaded motion remain human-observed gates.

WEBUI REMEDIATION PROGRAM (parent #101)
- #159: realtime dashboard cards, board controls, card layout and event-driven
  display updates. Draft [PR #167](https://github.com/atomicdeploy/PCController/pull/167) (`8fcf593`) now covers <1s socket freshness,
  conditional Refresh, direct relay toggles, parsed Hello/status toast
  suppression, live device feedback, and immediate hover/focus cues.
- #160: tables/event stream, normalized filters, menus, columns and debug-only
  high-volume event visibility. Draft [PR #168](https://github.com/atomicdeploy/PCController/pull/168) (`21cc899`) covers table
  padding/header/menu/resize/drag behavior and event filters.
- #161: shell/sidebar/settings/navigation/icons/userscript discoverability.
  [PR #168](https://github.com/atomicdeploy/PCController/pull/168) covers shortcut spacing, sidebar geometry and menu dismissal.
- #162: unified board-control names/semantics across Web, TUI, CLI, IPC and API.
- #163: smooth telemetry, browser diagnostics/userscripts, deterministic assets.
  Draft [PR #170](https://github.com/atomicdeploy/PCController/pull/170) supplies deterministic asset URLs/ETag revalidation, smooth
  telemetry, filtering, and the documented browser-console helper.
- #172: nested, capability-generated Peripheral Workbench menus (sub-issue of
  #101); preserves normalized page navigation and socket-driven state updates.
- #169 (Draft PR): cross-surface typed controls, silent-safe `beep`, immediate
  settings readback/persistence, and explicit versionless API enforcement.
- #174 (delivery integration): combines the compatible #159/#160/#161/#162/
  #163/#164/#165/#166/#169/#172 source slices, including the live `sidebar__status`
  context menu, smooth event-driven charts, fixed meaningful chart domains
  (roughly 11–12.5 V and 20–55 °C), stable asset delivery, and HELLO v4 support.
  It is the branch used for the running host. Its exact current head is fully
  green; it remains Draft only because broader WebUI child work still needs
  review/disposition before canonical merge.

## UNIFIED MESSAGING / EVENT FABRIC
- #164: parent architecture — one typed command, event, message and operation
  fabric for host, firmware and every interface surface.
- #165: cross-surface actionable notifications (native/Web/TUI/CLI/script/API/
  RPC/IPC/WebSocket/Socket.IO/bridge) with one async/sync correlation model.
- #166: route board and host operations—including buzzer and seven-segment—
  through generated canonical contracts rather than one-off code paths.

## RULES FOR ALL NEW WORK
1. Follow the canonical [GitHub Collaboration and Handoffs](GitHub-Collaboration-and-Handoffs.md)
   policy for issue/PR ownership, two-way synchronization, WIP preservation,
   prompt provenance, privacy, generated artifacts, merging, and handoff.
2. Keep interfaces synchronized: Web, TUI, CLI, IPC, API, bridge, and firmware
   use one normalized contract where applicable.
3. Board EEPROM writes must be immediately applied, then reported as persisted;
   accepted is not the same as durable.
4. Do not expose stale manual Refresh actions while a socket-driven view is fresh.
   A disconnected socket makes data stale.
5. Every accepted state/command change must be fanned out through the host event
   stream to all subscribed clients; refresh is recovery-only, never the normal
   synchronization mechanism.
6. Keep board silent until the user explicitly asks to change it. Do not actuate
   physical outputs when hardware safety/temperature state is not suitable.

## ORIGIN-HOST RECONCILIATION
- Clean canonical checkout:
  `<local origin-host canonical worktree>`
- Large interrupted Desktop checkout is preserved and must never be reset/cleaned
  until every unique source path is checkpointed remotely and a disposition
  manifest exists.
- Reconciliation instruction:
  `<local origin-host reconciliation instruction>`
- Edge-host audit:
  ORIGIN-HOST-WORKTREE-AUDIT-2026-08-12.md
- Artifact/installer/security WIPs require content comparison with merged PRs
  before any duplicate PR is opened.
- The dedicated macro-delivery handoff worktree is clean, now on local branch
  `handoff/macro-delivery-integrated`, and tracks shared delivery-base commit
  `500fd3ae115ab55a64c408355a1a2285883b4aa2`. The pre-squash delivered commit
  remains preserved locally; both commits have tree
  `7b1ad47693632c512bcaef35c0231afccfbafd34`.
  The origin-host agent is separately diagnosing server/cafe clock convergence;
  do not start a concurrent writer in that active task. Exchange a new handoff
  after the native-renderer checkpoint is pushed.

## DEFERRED / FOLLOW-UP
- #103: full VirtualBoard/main firmware runtime unification (deferred by user).
- #148: full persistent remote authentication and authorization design (deferred;
  alpha host currently bypasses it).
- #149: host CPU/timeline growth investigation.
- DS ROM role commissioning, EEPROM audio sequence research, RGB mode authoring,
  offline status patterns, and broader physical acceptance remain backlog work.
- EEPROM-authored RGB profiles and transient cue overlays remain deferred under
  #107; the actual autonomous base engine is live. The larger fully autonomous
  segment repeat/interval scheduler is compiled out on this 328P profile, with
  shared host orchestration used instead.

## HUMAN CHECKS NEEDED LATER
- Visually confirm the physical RGB breathe/transition is smooth at 62.5 FPS and
  the two live seven-segment messages appeared in the correct order.
- Press front-panel motion keys while a named recording is active to provide the
  final human-observed physical-key capture evidence; keep the load disconnected
  or mechanically safe until motion acceptance is explicitly authorized.
- Explicitly authorize unmute before testing any beep/audio path.
- Verify motion/relay/MOSFET behavior only with loads, doors, and supervision safe.
- Use GitHub Project #3 to assign the WebUI child issues and review their PRs.

This is the single Desktop tracking file. Older handoff files were archived after
their contents were consolidated here.

## USBASP / TEST-02 PHYSICAL PASS — 2026-08-12
- Go-tooling-only repeatable lifecycle acceptance (latest): [PR #211](https://github.com/atomicdeploy/PCController/pull/211) / commit
  `7cb5b29` adds canonical `controller board provision` and rewrites
  `controller board blank` to be the only project-owned path for this work.
  It is exposed by CLI, TUI Programming action, and the existing generic
  versionless command API/RPC route. It reports named phase durations in JSON;
  it does not use a user shell invocation of AVRDUDE or Arduino CLI.
- The initial controller-only trial exposed and corrected a real sequencing
  defect before any write: it began full 32 KiB backup under `-B32`. The final
  implementation captures only signature/fuse/lock evidence at slow SCK,
  applies selected FQBN crystal fuses, proves normal USBasp, then begins the
  long backup/erase/readback at fast speed. Regression tests enforce that
  ordering. This is the requested least-time reliable default with slow as a
  bounded fallback.
- Two complete controller-only USBasp cycles passed. Each began factory
  `62/D9/FF/FF`, fell back from normal USBasp to `-B32`, applied
  `F7/D7/FD`, proved normal-speed USBasp, took fast backup, installed the
  selected MiniCore bootloader, then blanked back to `62/D9/FF/FF` and verified
  32,768 flash bytes plus 1,024 EEPROM bytes as `FF`. Evidence logs and four timestamped backup manifests were retained in the local operations store.
- The final physical state is factory blank: signature `1E950F`, fuses/lock
  `62/D9/FF/FF`, complete flash `FF`, complete EEPROM `FF`. This is a state
  classification only, not evidence that the chip is TEST-01 or TEST-02.
- `Prog` latch behavior is implemented: when UART application identity exists,
  blanking authenticates it, acquires program state, writes SettingsProgrammingMode,
  and waits (bounded 2s) until EEPROM reports persisted before USBasp owns RESET.
  The current board had no UART enumeration, so that live latch branch remains
  an explicit acceptance item for TEST-01—not claimed as physically exercised.
- Correction: a fully erased flash/EEPROM image carries no board identity. The
  earlier claim that this was TEST-01 because its raw hashes matched a historical
  blank capture was unsupported. This physical unit is now designated TEST-02:
  the blank-board acceptance target provisioned in this pass.
- Fresh read-only raw capture: USBasp signature `1E950F`; initial fuses
  `lfuse=62 hfuse=D9 efuse=FF lock=FF`; all 32,768 flash bytes and all 1,024
  EEPROM bytes were `FF`. Those hashes match a historical blank capture, but
  do not identify a physical board.
- The managed normal-speed USBasp attempt failed first; automatic `-B32`
  fallback reached the target. `toolchain install-bootloader` then wrote the
  selected 16 MHz UART0/Urboot profile. A subsequent normal-speed USBasp backup
  completed without slow fallback with `lfuse=F7 hfuse=D7 efuse=FD lock=FF`.
  This proves slow is recovery-only and normal USBasp is restored afterward.
- Firmware `C22E8666` was flashed through the guarded Go-host recovery path
  after the complete raw backup. A second normal-speed ISP read verified the
  flash identity and native HELLO on `UART_PORT`: profile full-peripheral,
  capabilities `0x957DFFBF`, build features `0x49`.
- Board silence was deliberately set OFF live; `beep 880 300` and
  `melody play notify` were accepted. TonePlayer/Buzzer works. The power-on
  welcome melody does not play because `PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES=0`
  in this compact profile (HELLO feature bit 4 clear), not because the buzzer is
  broken. #22 contains the capability/fit acceptance follow-up.
- Outstanding: `boot probe`/`boot backup` over UART Urboot still fails despite
  the successful ISP bootloader burn and live application HELLO. #182 records
  this separately. Remote guarded programming parity is tracked in #184, and
  host identity parity (top-level version works, exec version does not) in #64.
- Follow-up recovery corrected a tooling error: ISP flashing the application-only
  HEX auto-erased Urboot. The merged `PCController.ino.with_bootloader.hex` was
  then ISP-written/readback-verified (32,722 bytes), and `UART_PORT` reauthenticated
  the application. UART Urclock nevertheless still logs ten `not in sync`
  retries at 115200 after DTR/reset. This is now isolated to UART boot entry,
  reset timing, or physical TX/RX path—not USBasp/fuses/application identity.
- Host patch `cdbe408` / [PR #196](https://github.com/atomicdeploy/PCController/pull/196) makes absent optional PCA non-fatal during the
  guarded programming preflight (both PWM fade and programming RGB cue); focused
  control test passes. The temporary test host is built at
  `<local test-host build>`.
- Audio command migration: [PR #203](https://github.com/atomicdeploy/PCController/pull/203) / `fd5d587` makes `beep` canonical in CLI,
  TUI, Web actions, docs, and newly recorded macro steps. The old spelling is
  parser-only compatibility for existing stored automation; a repository scan
  found no emitted legacy command or macro spelling.
- TEST-02 retirement for TEST-01 work: `controller board blank --confirm
  ERASE-BOARD --uart UART_PORT --json` created complete USBasp backup
  `<local backup operation>`
  (flash SHA-256 `c0c6a6dc...924235`, EEPROM SHA-256
  `c19f5394...9760bc`), then verified all 32,768 flash and 1,024 EEPROM bytes
  as `FF`. Fuses were deliberately preserved at F7/D7/FD/FF, so this is blank
  program/data, not a restoration to the original pre-provision 62/D9/FF/FF
  fuse configuration. The original raw blank capture remains preserved.
- Command-hierarchy backlog: once nested commands exist, replace the interim
  standalone `beep` surface with the alpha-native `buzzer beep` and `buzzer
  silent` hierarchy. Do not retain aliases or compatibility/migration behavior;
  issue #22 records this explicitly.
- Factory-blank final verification (TEST-02): with the host stopped, USBasp
  `-B4` direct readback confirmed ATmega328P signature `1E950F`, all 32,768
  flash bytes and all 1,024 EEPROM bytes are `FF`; fuses/lock are exactly
  `62/D9/FF/FF`. Evidence is
  `factory-blank-test02-20260812\final-verification.json`; this condition
  denotes a factory-blank chip, never TEST-01 identity. Backlogs: #208 removes
  obsolete alpha schema/compatibility material; #209 designs truthful compact
  capability discovery from metadata/opcodes before beta/RC.

## 2026-08-13 — CAFE production-board lifecycle terminology and UART preservation

- Correction: this connected application unit is the original production board,
  not TEST-01 or TEST-02. Its earlier EEPROM label was historical test data and
  was not physical identity evidence. The durable operator name is now **CAFE**.
- Lifecycle vocabulary is now explicit in commits `e762899` and `d83599e`,
  published as [PR #214](https://github.com/atomicdeploy/PCController/pull/214)
  against `agent/webui-delivery-live` and tracked on [#182](https://github.com/atomicdeploy/PCController/issues/182):
  `board initialize` means only USBasp fuse-policy and bootloader setup;
  `board provision` is the application lifecycle. A healthy authenticated app
  is retained. With `--firmware`, Controller uses the UART Urboot bootloader
  and mandatory readback; it probes Urboot before falling back to ISP if the
  application does not authenticate.
- Live Controller-Go acceptance on TEST-01 / `UART_PORT`:
  1. `board provision --uart UART_PORT --json` authenticated TEST-01 and returned
     `initialized:false`, `uart_programmed:false`; no USBasp/ISP work occurred.
  2. `board provision --uart UART_PORT --firmware PCController.ino.hex --name TEST-01
     --skip-toolchain --json` authenticated TEST-01 first, wrote 32,384 bytes
     through Urclock, completed mandatory readback of 32,338 written bytes,
     reauthenticated the application, and returned `initialized:false`.
     Measured Controller elapsed time: 32,433 ms. The source application SHA-256
     was `66606cd796edc8a94fe1c2966b7f5a29cd8071c2f97c69bc78e0d70ca244d44c`.
  3. The host previously misclassified a just-written name because its EEPROM
     record is cooperative: valid in-RAM name + `persisted=false` is now parsed,
     then `SetBoardName` polls for durable confirmation for up to two seconds.
     Live mutation `TEST-01 -> TEST-1A -> TEST-01` completed with independent
     durable readbacks. The board finished named **TEST-01**.
- Validation for this change: `go test ./internal/native ./internal/control
  ./cmd/controller ./internal/tui` passed. Full evidence log:
  `<local evidence log>`.

## 2026-08-13 — CAFE production front-panel/profile correction

- Canonical delivery branch: `agent/board-lifecycle-factory-blank`, head
  `c920c4f` (pushed). [PR #214](https://github.com/atomicdeploy/PCController/pull/214) remains the integration PR.
- Production menu catalog is now exactly: door, VOLT, CURR, tLED, tBT, LItE,
  bEEP, rELY, KEY, LErn. Retired r5-8/uPWM/Move page identities are compiled
  out; PWM/uPWM share one disabled PWM page; KEY owns the four motion bindings.
- BT indicator detection is compiled out and treated electrically as inactive;
  it no longer drives status policy or floods events. Active-HIGH semantics
  remain the requirement if that optional feature returns.
- LItE/Auto is restored independently of the disabled local PCA editor. With
  the door open, Auto targets enclosure illumination channel 11 at full scale;
  connected edits remain host-owned and offline edits auto-persist.
- bEEP now rolls Mute ↔ bEEP in either direction. Direct front-panel feedback
  uses the retained TonePlayer even though autonomous AudioCues are disabled;
  muted presses cannot queue stale tones, and entering bEEP immediately emits
  one confirmation beep. Numeric editors use 1 per press, 10 per ordinary
  hold repeat, and 100 after the fast-hold threshold, with rollover tests.
- Shared output-engine policy: host Status RGB fallback and MOSFET fades share
  transition math; firmware illumination and RGB share bounded transition
  primitives. The PCA RGB compositor, scalar MOSFET/PWM writer, and D6 800 kHz
  addressable-strip writer stay separate because their ownership, timing, and
  state shapes differ. Merging those hardware writers would add adapters/state
  rather than save ATmega328P flash. Descriptor/event semantics are the future
  reusable boundary; board-owned PWM destination+duration remains backlog.
- Exact firmware evidence (source DDDEC541): application 32,366/32,384 bytes,
  18 free; static SRAM 1,447; estimated free SRAM 282; HEX SHA-256
  `109EDDFC83B3EAD11FCBAEFFE082B99E39709C9D44568147653B30F0FA1A24E5`.
  Native VirtualBoard/firmware tests: 6/6 passed.
- Shared Arduino repair: the previous Arduino15 migration had only MiniCore
  metadata/bootloaders and lacked `cores/.../Arduino.h` and variants. New
  project command `controller toolchain adopt` stages and atomically swaps the
  complete verified package vendors/libraries into the shared Arduino roots,
  saves the system CLI/config paths, and requires no network. Physical adoption
  completed; a canonical build using `<configured system Arduino CLI>` and the
  Arduino15 config passed. The incomplete active core was never edited in
  place.
- Still intentionally not implemented: EEPROM startup-opcode interpreter and
  EEPROM melody authoring (#87); board-local multi-channel destination/duration
  PWM transition opcode (#21); autonomous transient AudioCues controller (#22).
  TonePlayer, native RGB compositor, illumination engine, PWM/MOSFET endpoints,
  addressable-strip writer, motion engine, RF learning/mapping, and macro
  ordinary-opcode playback remain present.

## 2026-08-13 — CAFE identity, shared rollover, and reset-durable clients

- Persistent board name **CAFE** is detected from the CRC-backed EEPROM record
  after every authenticated generation and exposed consistently through CLI
  (`board name get|set|clear`), typed Go client, JSON-RPC, versionless REST
  (`GET|PUT|DELETE /api/board/name`), TUI Board settings, Web dashboard editing,
  shared snapshots, and `board.name.changed` state events.
- A live REST mutation `CAFE -> CAFEX` updated the already-open Web tab through
  the server event stream; typed RPC restored `CAFE`, and the same tab updated
  again without a reload. Final EEPROM readback is `CAFE`, `persisted=true`.
- Shared menu-index rollover now covers illumination modes, relay selection,
  user-PWM selection, and both increment/decrement directions; direct PWM and
  scalar editors retain their bounded endpoint rollover. Native tests cover the
  wrap boundaries. Live silent acceptance cycled LItE and returned it to Auto
  (`light_mode=1`) with the Door page restored.
- Firmware source **E6B6D76B** is flashed on `UART_PORT` via Controller's guarded
  Urclock workflow. Application HEX SHA-256 is
  `2F080C6C22EF2178000391296538D67C4FDFDDBAFFB6A55715D0B1E80733C4D2`;
  firmware uses 32,354/32,384 application bytes (18 free).
- Installed edge host SHA-256 is
  `A3C800F6B4EFFC44EEEB10D3A566BE45EC8E30FB5606D0F5F0314C56B8A9B445`,
  source identity `479aaf843d449fe1d8b3ed383bb3b42e48398ecbbb25aa506eecdd864270ef33`.
  UPX 5.2, Win32 resources, Web typecheck/build, C ABI smoke, and focused Go
  suites passed.
- DTR regression found and fixed: changed-only segment output could occur before
  the replacement serial pump was ready. Every authenticated connection now
  queries one authoritative front-panel snapshot and publishes it to all app
  clients. On the same open Web tab, DTR advanced reset count 130 -> 131,
  restarted uptime, retained CAFE/build E6B6D76B, and restored LIVE segment
  bytes `3F737954` without polling or manual refresh.
- [PR #214](https://github.com/atomicdeploy/PCController/pull/214) merged into `agent/webui-delivery-live` as
  `6a2c3a5f912345fb6e575bbe021c5194a070e70b`. Delivery head `958cd0f`
  passed all 15 required GitHub checks: exact AVR, repository health, five
  VirtualBoard platforms, five native host packages, both summaries, and final
  build. The last DTR verification advanced reset count to 132; `UART_PORT` returned
  connected with CAFE persisted, front-panel bytes `3F737954`, Auto illumination,
  and silent mode retained.

## 2026-08-13 — UART-first programming policy and deduplicated backups

- Tracker issue #216 and Draft [PR #218](https://github.com/atomicdeploy/PCController/pull/218) now own the corrected programming policy.
  Exact implementation head: `23f8e5eea8d991a209d932143df99ca71800e92c`.
- Ordinary application firmware updates default to Arduino/Urboot Urclock UART.
  They attempt every region the connected bootloader exposes, but do not invent
  a fuse, ISP, USBasp, or complete-EEPROM prerequisite for a flash-only write.
- USBasp is not assumed connected. It is used only when explicitly selected for
  initialization/provisioning/recovery after an unavailable or unhealthy UART.
- The obsolete `allow-incomplete-backup` option was removed from CLI, command
  plans, Build, Web, RPC/API types, tests, and documentation.
- EEPROM-changing operations still fail closed unless the capture is a verified
  complete 1,024-byte EEPROM image. A settings response is never mislabeled as
  a full EEPROM backup.
- Flash, complete EEPROM, and programmer/fuse metadata are stored once by
  SHA-256. Timestamped operation manifests remain for audit; the current and
  immediately previous operation are protected. Older operations obey
  `programming.backup_retention_days` (30 by default), and unreferenced content
  blobs are garbage-collected only after their manifests expire.
- Toolchain bootstrap priority is saved config, `ARDUINO_CLI`, then `AVR_HOME`.
  If none exists, an interactive terminal asks before using the managed
  PCController directory; automation must explicitly pass `--managed-fallback`
  or `--no-managed-fallback`.
- Operational dry-run passed without USBasp:
  `controller program flash .build\firmware\PCController.ino.hex UART_PORT --dry-run`
  selected guarded Urclock application flash, capability-aware pre-update
  capture, and retention of the previous verified backup.
- Validation passed: focused Go suites/vet; 56 Build tests; Web typecheck,
  48 files/226 tests and production bundle; API 143 RPC/56 REST; repository
  check 52 required/817 source files; canonical host-only package build.
  GitHub repository-health is green; cross-platform build jobs are queued.
- Explicit follow-up remains in #216: add a running-firmware full-EEPROM transfer
  opcode or a temporary housekeeping image when protocol/flash budget permits.

## 2026-08-24 — Recoverable obsolete-artifact archive checkpoint

- Moved 24 unambiguously obsolete directories (12 Desktop `bin.failed-*`,
  `bin.next-*`, and `bin.pre-*` deployment generations; 12 abandoned Local
  Temp uninstall/program-plan/UPX staging directories) into the single
  recoverable archive:
  `<local recoverable archive>`.
- No files were deleted. Full destination verification compared every archived
  file by SHA-256 with zero missing files and zero mismatches. A private local
  inventory and human-readable manifest were retained with the archive.
- Preserved and rechecked: the current Desktop and installed host executables,
  Desktop/installer staging and rollback locations, all source worktrees,
  toolchains, runtime state, live board backups, ProgramData recovery backups,
  and Desktop tracking notes.
- An exhaustive targeted search of the PCController Desktop, Documents/Codex,
  AppData Local/Local Programs/Roaming, ProgramData, Program Files, Downloads,
  OneDrive, and Local Temp roots found no filesystem object named exactly
  `bak-20260806-104427` and no `.previous-*` match. Whole-profile/volume scans
  exceeded the bounded 60-second diagnostic window. If those names remain
  visible, capture their containing path because they may be UI/history entries
  or live outside the targeted PCController roots.
- Storage-growth follow-up: add lifecycle-managed retention and
  `controller storage inspect|prune --dry-run` for generated artifacts, builds,
  Arduino caches, deployment generations, temp helpers, and worktrees. It must
  expose bytes/age/ownership, preserve active and pinned generations,
  content-deduplicate by SHA-256, enforce configurable age/count/byte budgets,
  quarantine candidates before deletion, and atomically clean failed deployment
  staging.
- A read-only size inventory found that accumulated source worktrees dominate
  local application storage. None was moved or modified; their lifecycle is
  part of the storage-retention follow-up above.

## 2026-08-24 — Connected client identity and administrative inventory

- Existing tracker #108 owns the live instance/client registry; related
  transport, bridge, single-owner, surface-parity, and deferred authentication
  boundaries remain #46, #47, #52, #124, and #148.
- Every Web/PWA tab, native/tray process, service, TUI, CLI, script, IPC/RPC
  consumer, WebSocket/Socket.IO client, bridge, and remote instance must
  register a stable client identity plus a distinct connection/session ID and
  generation. A stale generation must never mutate or disconnect its
  replacement.
- The bounded, typed, privacy-safe record includes surface/client kind,
  display name, application/build/protocol identity, process/service mode,
  OS/platform/architecture, browser/device/PWA details, endpoint/transport,
  board or hardware affinity, capabilities/subscriptions, connection time,
  last-seen freshness, and eventual authentication state. Secrets,
  credentials, tokens, unrestricted environment/configuration, and
  unnecessary persistent hardware identifiers are excluded or redacted.
- The host owns one authoritative registry exposed through versionless
  `clients.list`, `clients.get`, and guarded `clients.disconnect` operations,
  with normalized connected/updated/disconnected events on every interface.
- WebUI must add a live **Clients & Instances** inventory with equivalent
  TUI/native access, details/search/filter, and push-only convergence across
  all clients without manual refresh.
- Disconnect targets exactly one session generation and is distinct from
  graceful process exit, stopping a service, or releasing board/serial
  ownership. Remote, self, last-administrator, service, and primary-owner
  operations require explicit policy/confirmation and a correlated audited
  outcome.
- Acceptance requires independent real clients observing the same registry,
  targeted disconnect affecting only the selected generation, stale-request
  rejection after reconnect, lifecycle/freshness convergence, privacy and
  malformed-input tests, and service/interactive coexistence.
