# Go edition to Flutter parity ledger

2026-10-13; candidate 0.1.12/code 13. Dedicated Flutter forms, native command
access and platform-only workflows are distinct; this is not full standalone
or physical acceptance.

## Sources and findings

Only the packaged controller's early-exit `help` and `version` were executed.
Its source is 6064d41cd9b1561497dd7d114421a7fde1ebb76a3d25c91e36f8403e6ab6c7bc,
built 2026-10-12, older than current canonical source. Cross-checked top-level
help/dispatch in cmd/controller/cli_support.go and main.go, the current command
registry, native payload validators, display/settings/recording/RF/strip/effect
parsers and typed IPC endpoints. Read-only live Cafe controller.command.catalog
returned 52 definitions; the committed fixture preserves this snapshot. Flutter
exposes 50 useful families, excluding remote console clear/quit. Runtime uses the
selected host's actual advertised catalog. These are families, not 50 individual
features. Usage, aliases, groups and summaries are searchable.

Findings corrected in Flutter:

- Display previously allowed 240 characters and fixed five-second playback.
  Actual typed native bounds: 40 printable ASCII bytes, segment/LCD/both,
  scroll speed 80–5000 ms, duration 80–65535 ms, once/loop/interval repeats;
  intervals are 1–255 whole seconds. Empty text clears the override.
- Board names are eight printable ASCII characters; recording requires a
  printable ASCII name up to 64 characters. Automatic, MCU-timed and board-retained
  recording are distinct. RF timed mode is `timer`, not `timed`.
- EEPROM settings have 17 ordered semantic arguments. Actual status color is
  0–4 despite broader help. Decimal encoding 0 means two places, not zero.
  Programming latch/opaque flags must survive edits without guessed defaults.
- Strip programs require stable IDs, with human names as captions. Pixel/FPS
  ranges and modes come from hardware metadata. Native board/host buzzer routing
  is separate from Android PCM mirroring preferences.
- Native PWM -32010 confirmation_unavailable/write_acknowledged/retryable:false
  means an acknowledged write lacks confirmed readback. Preserve stale observations;
  do not disconnect or replay the write.

## Port inventory

| Go help/catalog scope | Flutter implementation | Remaining scope |
|---|---|---|
| TUI/Web/shell/exec/catalog | Unified workspace; Controller tools with searchable usage/aliases and explicit one-command workbench | No embedded Go executable/remote console lifecycle |
| status/event/voltage/current/temp/stream | Existing live telemetry/charts/events and explicit refresh; native options in workbench | Actual hardware acceptance |
| relay/PWM/E-STOP | Observed indicators/equal-width controls; priority Stop and reviewed release; CLI PWM shares draft/readback fences | Physical feedback/mixed-client soak |
| front-panel/menu/display | Existing panel/buttons/segments; menu pages/layout reads; new segment/LCD/both timing, repeats, intervals, scroll and clear form | Physical panel/display fidelity |
| settings/board name | Full live EEPROM editor/export and eight-byte name form | Atomic native CAS and physical persistence proof |
| RGB/strip | Capability-driven RGB color/brightness/Auto; strip solid/pixel/frame/rainbow/effect/count/FPS/clear/active-only Stop | Effect editing remains workbench/Studio; physical strip proof |
| buzzer/silent/melody/audio | Native routing/silence targets, melody library/repeats/active-only Stop; existing Android PCM mirror; other audio commands in workbench | Android audible cadence/timbre acceptance |
| macro/effect | Existing sequence editor/catalog/playback; named Automatic/MCU/board recording, active Save/Discard/status; remaining native authoring/property/import/export commands in workbench | Full visual editor parity/physical timing |
| RF/automation/board-automation | Capability-gated 30-second RF learning/catalog/cancel; host/board rule reads; other forms in workbench | Rich RF/rule editors and offline-board proof |
| ports/discovery/open/close/reconnect/hello/port-owner | Existing peer discovery/connection; serial ports/ownership quick reads; serial operations in workbench | Android USB standalone core |
| OS/host-menu/hotkeys/keyboard/webhook/config | Host facts/policy/hotkeys/webhooks quick reads; advertised options in workbench with mutation review | Dedicated visual editors/platform acceptance |
| program/program-state/boot/driver/toolchain | Existing Firmware operation/artifact/backup workflow; advertised native command options in workbench under receiver safety | Top-level provision-host/runtime/service installers are not inner RPC commands; no Android Windows-driver installer |
| I2C/reset/query/opcode/write | Expert workbench with current-owner/E-STOP checks and explicit mutation review | Native authorization/backup/exclusion mandatory; no physical/raw-write tests |
| bridge/peer-update/IPC/WebSocket/monitor | Existing typed Pealayer relay, protected local Android automation and peer package transfers; direct-native bridge/update commands in workbench | No recursive third-peer proxy; two-device consent, ADB/Termux/Tasker/root/API acceptance |
| batch/file-only eeprom/firmware/service/runtime | Existing Studio sequences; native host retains file/lifecycle tools | No automatic CLI batch replay or independent Android native-core execution |

## Execution contract and proof

Tools bind to the selected host/board generation and transport. Catalog reads
occur once on entry and on explicit refresh, with no periodic tools polling.
Refresh preserves staged values. Native receiver policy remains authoritative;
catalog entries are not permission to bypass it. Ambiguous forms require manual
review. Cancel sends nothing. Commands dispatch once; uncertain/replaced-owner
receipts cannot become applied proof or replay. Native compile/boot/reconnect
workflows need no normal-mode board at Flutter admission; native recovery gates
still apply. Priority Stop remains available while tools are busy. Release needs
review; other writes recheck owner/E-STOP after dialogs. Typed Pealayer routes
reject bridge/peer-update commands that would become a recursive third-peer proxy.

PWM writes fence pre-write reads, retain the last observation as stale and schedule
one post-command read. Acknowledged-but-unconfirmed native errors preserve the
peer connection and do not replay. Settings review exports current settings and
compares the semantic baseline before writing; conflicts keep the draft open.
This is not atomic CAS: native settings-set has no revision token, leaving a
race between preflight and write as a backend gap. Programming/opaque flags stay
preserved.

42 focused command/PWM/appearance/contextual/front-panel/pull-refresh checks and
three new tools/dialog widget checks passed. One opt-in source renderer produced
actual Flutter phone command/tablet lighting fixtures without overflow, not
Android screenshots. Analyzer clean. No physical relay/motor/beep/RF/firmware/player
action was sent. Package/device identities are recorded separately in ACCEPTANCE.md
and delivery receipts. Full Go/egui/Web parity, native Android standalone core/USB,
real command transports/multicast/branding/launchers, two-device upgrades and
physical/audio/human acceptance remain open. Continuous acceptance goal active.

Package delivery: source 1f9f2b2071d754222834da3d87b1cec3afa09547 built
0.1.12/code 13 (20,266,596 bytes; SHA-256
e124b356e95c41765636791c61e75b4347db9d14663ee44bf7094f67edfbb5d8).
Signer verified; one background tablet install succeeded and installed identity
matches. Locked video activity preserved, controller launch/live proof open.
Phone remains 0.1.2/code 3 with 491,588 KiB free; no repeated failed install.
Exact device observations: android-0.1.12-delivery.json. Source branches may be
reconciled separately; the APK identity remains tied to the exact build source.

Strip effects now use the current `effect play ID COUNT FPS` command from the
native workbench. The shared effect dialog includes typed strip programs and
Fade/Pulse envelope presets. Deliberate full-frame probes use `strip frame` with
peer fencing and no mutation replay. Current AVR firmware provides no RGB frame
readback; acknowledgement and physical observation remain distinct.
