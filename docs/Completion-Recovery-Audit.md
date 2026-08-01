# Completion Recovery Audit

This audit separates source presence from demonstrated behavior. It was made
after the user correctly reported that several implemented features had not
been visibly handed off, the TUI had not been demonstrated to their
satisfaction, and macro work had been described more completely than it had
been exercised.

Audit date: 2026-08-01; source/test update: 2026-08-02. This pass did not open COM18, reset the board, upload
firmware, or replace the user's running controller process. `tasklist` showed a
running `controller.exe` at PID 46868, but the executable predates the TUI
alignment changes in this pass and must be rebuilt/relaunched before those
changes are visible.

## Result at a glance

| Area | Classification | Evidence now available | What is still required |
|---|---|---|---|
| Host TUI framework | Implemented and automated-test verified | Ten Charm pages, keyboard/mouse routing, completion, configuration, front-panel preview, and deterministic preview tests pass; the current focused suite passes in `go test ./internal/tui` | Complete a human visual/interaction sweep in the real Windows Terminal after rebuilding the executable |
| TUI table alignment | Source-fixed and render verified | ANSI-aware cell padding, centered section headers, correct bordered-card sizing, and aligned wrapped values pass at 88, 120, 132, and 160 columns; the 120-column dashboard keeps both right borders | Rebuild/relaunch the packaged executable; visually confirm the user's actual terminal font/zoom |
| Macro recorder and library | Implemented, automated-test verified, live-unverified | The Automations page now exposes a searchable ID-sorted library, PC metadata, create/record/save/discard/play/inspect/two-step-delete, both cancel policies, keyboard/mouse control, and live elapsed/duration/step/buffer/timing/lifecycle/faithfulness state from the same command-engine runner; focused tests pass at 88/120/160 columns | Rebuild/relaunch the packaged executable and perform a harmless live AVR recording/playback/cancel acceptance pass |
| MCU-timed macro playback | Implemented and virtual-board verified | Chunked circular-buffer playback completed `output-demo` with 5/5 steps, zero underruns/errors/violations, and `faithful=true` on the virtual board | Run a harmless physical-board macro, observe TM1637/LCD progress, cancel it, and verify one relay/PWM output safely |
| Timed macro events | Fixed and automated-test verified | The host now accepts the timestamp marker on event type `0x86` (`EventMacro | 0x80`) and parses the appended MCU clock | Include the fix in the next host binary and observe a physical macro event stream |
| Live TUI launch | Process present, acceptance incomplete | `controller.exe` PID 46868 was present during this audit and the checklist records prior COM18/IPC authentication | No automated Windows Terminal control was used. Screenshots and page-by-page user acceptance remain open |
| RF learning | Partly live-verified, human-dependent | Remote A is recorded as `0x00F30BC8`, 24 bits, protocol 1, and mapped to Side A Up | Review stale `0x00F40BC8`; guide the user through B, C, and D one at a time; verify latency/repeat/hold and final mappings |
| Attention workflow | Implemented path, not invoked | Named attention melody/effect commands and tests exist | When human input is genuinely needed and Do Not Disturb is off, display `WAIT`, start the repeating ringtone plus visible effect, then cancel both after acknowledgement |
| Firmware/live peripherals | Mixed, primarily live-unverified in this audit | Earlier checklist evidence covers boot melody, UART repair, approximately 12 V, buzzer repair, menu sweep, and current firmware checks | Door/BT/RGB fades, all buttons/gestures, load-safe motion/relays, LCD, RF transmit, USB removal/reconnect, and live macro safety still need physical observation |
| Latest firmware/native pass | Source-built and native-test verified; not uploaded | `F6D76FE4` builds at 32,240/32,384 flash and 1,441/2,048 static SRAM; all three native suites pass; the custom TM1637 Urboot leaves 16 application bytes | Guarded upload/read-back and every physical peripheral check remain open; this pass deliberately did not open COM18 |
| Documentation in the repository | Implemented as files; handoff was incomplete | Starter guide, hardware tuning, menus, host configuration, protocol/API, memory tradeoffs, merge history, checklist, and this recovery audit are linked from `docs/README.md` | Deliver a final user-facing report only after the remaining verification; do not describe repository files as if they had already been handed to the user |
| GitHub issue synchronization | Implemented and verified | The dry-run synchronizer reports 62 normalized requirements; GitHub currently has 63 open and 12 closed issues, comprising 13 open epics plus 50 open/12 closed normalized items | Work and close the remaining issues using evidence; an open issue is not a delivered feature |
| GitHub pull requests | Verified current state | No pull requests were open at audit time | Continue issue/PR discipline for later large changes |
| GitHub Project board | Unverified due credential scope | `gh project list` failed because the authenticated token lacks `read:project` | Refresh `gh` authorization with project scope, then create/verify the board and populate it |
| GitHub wiki | Enabled but uninitialized | Repository metadata reports `has_wiki=true`, but the `.wiki.git` endpoint still returns repository not found because no first wiki page exists | Create the first page once through the GitHub web UI, then push the organized canonical pages to the newly initialized wiki repository |
| Canonical packaged host artifact | Not proven current | `Tools/Controller/bin/controller.exe version` reports source hash `240db76d...`, while the worktree contains later source changes | Rebuild, resource-stamp, test, UPX-pack, hash, and launch one canonical artifact; remove or ignore stale generated copies only through the approved packaging cleanup |
| Final acceptance report | Missing | The checklist contains detailed evidence and this audit identifies the boundary | Perform the final live/virtual/API/TUI sweep and issue the requested all-area report, menu migration choices, initialization catalog, byte tradeoffs, and unresolved human checks |

## What happened

The main failure was an acceptance-boundary mismatch. Substantial firmware and
host infrastructure was added, and many automated or virtual tests passed, but
some checklist prose and status updates compressed “source exists” into “the
feature is delivered.” The user expected the newest binary to be launched,
visible TUI pages to be exercised, macros to be demonstrated, and the promised
documentation/report to be handed over. Those are separate acceptance steps.

The TUI alignment complaint also had two concrete rendering causes:

1. Go formatting widths counted ANSI escape bytes instead of terminal cells,
   so styled labels shifted later columns.
2. Lip Gloss `Width` includes padding when it calculates wrapping, while borders
   add their own frame. The dashboard used a content width as a card width,
   which could wrap long cells and lose the expected right-edge geometry.

The current renderer pads by visible cells, sizes each card from the terminal
width and style frame, centers its section header, and explicitly wraps long
values beneath the value column.

The earlier Automations page was also genuinely incomplete as a user
interface: it exposed only list/cancel shortcuts while the recorder and runner
were hidden behind Console commands. It now consumes the single runner already
owned by the command engine, supplies a searchable and mouse-operable macro
library, and exposes recorder/playback health directly. This closes the
source-level TUI gap, but it does not replace the required physical-board
timing/output/display demonstration.

The macro event failure was similarly concrete. A timed device event uses the
high bit on the event type and appends a 32-bit MCU timestamp. Dispatch masked
that bit, but `ParseMacroStatus` checked the original `0x86` byte against plain
`0x06` and rejected valid status events as `unsupported MACRO_STATUS envelope
134/schema 2`. The parser now compares the low seven bits and a regression test
covers the complete timed envelope.

## Firmware and native-simulator recovery pass

The 2026-08-02 firmware-only pass did not open COM18, reset the board, or
upload an image. It produced a fresh canonical source build with this exact
identity:

```text
Build/source hash:        F6D76FE4
Packed date/time:         0x35020B0C (260802012424)
Application flash:        32,240 / 32,384 bytes (144 free)
Static SRAM:              1,441 / 2,048 bytes
Guarded stack/RF margin:  278 bytes
Application SHA-256:      A64E76C341B4FB4762BD0A7C0C8750C09C9BA103C6C74AEBA6350BA35957D021
Stock merged SHA-256:     94B568296905C91DA55009EA4667A398DB9760045EC0AC7C8DF4D1573A42873E
EEPROM SHA-256:           788E5FFC44AE4EE912FE01F495951F96B0ACCD5705E184DCAEA197D8B64856A6
```

The custom TM1637-progress Urboot was independently rebuilt from its pinned
upstream source. It remains 510/512 meaningful bytes and has SHA-256
`27A053DCF384818A4B18B806A1EB0F4020EBCE1051D422AFEE8017DD48C615E0`.
Its 32,256-byte application ceiling leaves **16 bytes** for this firmware,
rather than the 144 bytes available under stock MiniCore Urboot.

Three native CTest targets passed: the virtual protocol/EEPROM model, the
production UART codec, and production keys/shift-registers/relays/buzzer/
DS18B20 domains compiled against the desktop Arduino mock. Coverage now
includes all 20 RF records, single/multi/indefinite learning, canonical
settings and reset-journal persistence, semantic `PROGRAM_STATE`, exact
status flags, front-panel/menu payloads, cooperative I2C, safe motion policy,
1 ms direction break/50 ms settle logic, key click/double/hold/repeat/release,
mapped RF action execution/repeat suppression/momentary expiry, Timer1 buzzer
operation, bounded DS18B20 behavior without a pull-up, smoothing, and
fade/rollover math.

This pass also found and fixed a real memory-safety defect: DisplayText could
accept a 40-byte LCD payload into a 32-byte `hostLcdText` buffer. The protocol
still accepts its bounded extension payload, but the visible 2x16 LCD copy is
now clamped to 32 cells. The fix costs six flash bytes and removes the overwrite
without sacrificing any displayable character.

The VirtualBoard now mirrors the production unversioned EEPROM regions,
20-record RF semantics, status/capability bits, DisplayText targets, menu-list
schema, `PROGRAM_STATE`, and cooperative I2C naming/transactions. Its deliberate
limits remain explicit: wall-clock scheduling replaces AVR cycles, simulated
I2C devices return deterministic bytes rather than model electrical faults,
relay waveforms are checked by native production-domain tests rather than the
virtual bank, and unadvertised host-menu directory opcodes remain unsupported.

## Verification executed in this pass

From `Tools/Controller`:

```text
go test ./internal/tui
go test ./...
go run ./cmd/tui-preview -page dashboard -width 120 -height 38
go run ./cmd/tui-preview -page outputs -width 120 -height 36
go run ./cmd/tui-preview -page board -width 120 -height 36
go run ./cmd/tui-preview -page app -width 120 -height 36
```

The current focused TUI package passed; an earlier pass also ran all Go
packages before other concurrent source work began. The deterministic render suite now checks primary
dashboard/output/board-settings/app-settings pages at 88, 120, 132, and 160
columns; exact header centering; stable visible value columns; two-card border
fit; continuation indentation for long dashboard values; and the macro
workspace's stable layout, search, create/record/play/save/discard/cancel/
inspect/delete actions, mouse targeting, and shared runner provider.

Additional read-only checks:

```text
tasklist /fi "IMAGENAME eq controller.exe" /fo list
node Tools\Audit\sync-github-requirements.mjs
gh issue list --repo atomicdeploy/PCController --state open --limit 200 --json number --jq length
gh issue list --repo atomicdeploy/PCController --state closed --limit 200 --json number --jq length
gh pr list --repo atomicdeploy/PCController --state open --limit 100 --json number,title,isDraft
gh project list --owner atomicdeploy --format json
git ls-remote https://github.com/atomicdeploy/PCController.wiki.git
```

Results were: one running controller process; 62 normalized requirements in
the synchronization dry run; 63 open issues; 12 closed issues; no open pull
requests; project-board access blocked by missing token scope; and an enabled
but uninitialized wiki whose first page must be created through GitHub's web UI
before the Git-backed wiki repository becomes available.

## What the user still had not received at this checkpoint

The searchable macro TUI was the main source gap repaired by the 2026-08-02
update. The following are different kinds of unfinished delivery and must not
be collapsed into a single “done” statement:

- **Current executable and visible UX:** the source changes have not yet been
  packaged into the canonical resource-stamped/UPX executable, substituted for
  the running older process, and walked page by page in the user's terminal.
- **Current firmware on the device:** newer concurrent source work is not
  physical evidence until it is built, guarded-backup flashed, read back,
  authenticated by HELLO hash/timestamp, and checked with safe outputs.
- **Human hardware acceptance:** Buttons 1-4 and click/double/hold/repeat,
  B/C/D RF learning and latency, door/BT transitions, smooth enclosure/RGB/LED
  fades, audible cues, LCD behavior, RF transmit, relays/motion under a safe
  load, and disconnect/reconnect still require direct observation where the
  checklist marks them yellow or hardware-only.
- **Macro hardware acceptance:** the virtual-board queue is verified, but one
  harmless physical record/play/cancel run must prove MCU-local deltas, refill,
  TM1637/LCD progress, event synchronization, and the chosen safe/keep cancel
  policy on real outputs.
- **Programming and bootloader UX:** host-owned backup/program/restore work and
  bootloader progress-display feasibility need their final integrated build
  and live Urclock/Urboot evidence; an ISP-only experiment must be called out
  instead of silently presented as ordinary UART support.
- **Network/platform acceptance:** IPC/API/WebSocket/bridge code still needs
  the final primary/secondary-process, USB lifecycle, file-watch, and later
  second-PC remote-board exercise. Platform integrations such as toast actions,
  global hotkeys, discovery, monitor DDC/CI, and power actions must be reported
  as supported, unavailable, or live-tested individually.
- **Repository delivery:** most synchronized GitHub requirements remain open;
  Project-board access needs the missing token scope, and the enabled wiki needs
  its first web-created page before Git push can populate it.
- **Final handoff:** the user still needs the promised consolidated operating
  guide and evidence report covering every board/host menu, optional migration
  between AVR and host ownership with byte costs, module initialization values,
  flash/SRAM/EEPROM tradeoffs, exact functionality sacrificed by any space
  optimization, and every unresolved physical test.

These are acceptance boundaries, not evidence that the corresponding source is
absent. The canonical checklist remains the itemized authority and should be
refreshed after concurrent firmware, tooling, and network work lands.

## Required completion order

1. Rebuild and launch the canonical host binary so the user sees the corrected
   renderer, without opening a second serial owner.
2. Walk the real TUI pages at narrow and wide sizes and record screenshots or
   explicit user observations; fix any font/terminal-specific defect.
3. Run the harmless physical macro recording/playback/cancel acceptance
   sequence against the now-complete recorder/library/progress TUI.
4. Use the `WAIT` + repeating-ringtone + visible-effect sequence only when the
   next B/C/D RF or physical test genuinely needs the user.
5. Complete load-safe peripheral, USB reconnect, API/bridge, backup/programming,
   and hosted-front-panel checks.
6. Resolve the GitHub Project/wiki gaps, reconcile the checklist to evidence,
   and deliver the final report rather than merely leaving it in the worktree.
