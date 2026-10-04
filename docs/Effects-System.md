# Unified effects system

This document is the delivery contract for recording, editing, scheduling, and
executing every hardware effect. It describes one living alpha contract; it is
not a compatibility or schema-version document.

## The one concept

An **effect** is a reusable, PCController-owned definition. It may contain one
or several synchronized lanes:

- motion/seat actions;
- relay states;
- PWM output values and curves;
- addressable-strip programs or frames;
- status-RGB changes;
- seven-segment or LCD text;
- buzzer/tone actions;
- RF transmission;
- front-panel/menu actions; and
- explicitly enabled raw protocol actions for diagnostics.

“Macro”, “sequence”, “lighting effect”, and “recording” are implementation or
authoring details, not separate libraries. User interfaces call the saved item
an effect. A Pealayer timeline item is an **effect cue**: a stable reference to
the PCController definition plus its position, duration, and cue overrides. It
does not copy the definition into the Pealayer project.

## Ownership, persistence, and RAM

There is no board-owned versus host-owned effect catalog.

1. PCController persists the authoritative definition and presentation data.
2. Pealayer, the PCController Web UI, TUI, CLI, and RPC clients discover and
   edit that same catalog.
3. Starting an effect compiles an ephemeral run plan.
4. The run plan is staged into volatile host and/or device RAM according to
   current capabilities. Nothing about that staging changes ownership.
5. Device-retained recording is a bounded temporary take that can survive a
   host interruption. Saving it imports it into the same PCController catalog.

For the current AVR target, motion/relay timing records and the strip frame
buffer share a small workspace. PCController therefore cannot cache a whole
100-pixel animation on the board. It can prebuffer and refill timed command
records while it streams only the current strip frame. Rich definitions remain
on the host and are recoverable, editable, importable, and exportable.

## Adaptive execution

The default execution policy is `auto`:

- If the connected firmware advertises the timed queue and every step is
  compatible with its workspace, PCController uses the device clock.
- Otherwise it uses the acknowledged host monotonic scheduler.
- Addressable-strip programs use the host renderer and paced frame transport.
- A forced host or device policy is an advanced diagnostic option, not a
  second kind of effect.

The selected executor, buffer fill, accepted bytes/steps, underruns, timing
error, and final outcome are runtime evidence. They must be visible in
diagnostic details without making ordinary users choose where an effect lives.

## End-to-end data path

```text
PCController effect catalog
        |
        +--> TUI / Web / CLI / JSON-RPC editors
        |
        +--> Pealayer Effects Library editor
                    |
                    +--> timeline cue: effect:<stable-id>
                                      + start/duration/overrides
                                      |
                                      v
                           PCController effect play
                                      |
                       compile capability-aware run plan
                           /                     \
                  device timed queue       host scheduler
                         |                       |
                         +---- acknowledged board commands ----+
                                                              |
                                              relay/PWM/RF/display/audio/LED
```

One PCController instance owns a physical serial board. Remote Pealayer or
PCController clients route authenticated commands to that owner. They do not
open the same serial port or silently redirect an active run to a replacement
board.

## Authoring and recording workflow

1. Connect PCController to the board and resolve its profile/capabilities.
2. Open the Effects Library from PCController or Pealayer.
3. Create an empty effect or start a live recording.
4. Exercise seat, relay, PWM, display, RF, buzzer, and LED controls. Recording
   captures acknowledged actions; relay motion uses device edge timestamps.
5. Stop and save the take. A device-retained take is transferred into the same
   catalog during Save.
6. Open Manage and edit the take on the lane timeline. Cues can be moved,
   edge-resized, duplicated, deleted, snapped, quantized, or shifted together
   to remove leading delay. The selected cue exposes exact time, channel,
   value, duration, curve, repetition, semantic action IDs, and presentation
   metadata.
7. Preview through PCController and inspect timing evidence.
8. Drag the effect into Pealayer. The cue stores `effect:<stable-id>` and is
   movable/resizable without cloning the definition.
9. During video playback, Pealayer sends the stable reference at the scheduled
   time. PCController performs capability checks, stages the run plan, and
   reports progress/outcome.
10. Export or import `effects.json` when moving the catalog between hosts.

## Peripheral authoring contract

Every editor must be driven by the attached board's advertised action and
parameter schema. A control is offered only when its capability is present.

| Lane | Required authoring fields | Execution evidence |
|---|---|---|
| Motion | semantic action such as `seat.a.up`, duration/stop | applied relay edges and device timestamps |
| Relay | stable channel/action ID and state | acknowledged applied mask |
| PWM | stable channel ID, value/curve, optional duration | acknowledged value and live telemetry |
| Strip | pixel count, FPS, declarative program or frame source | frame/chunk ACK, shown frame, skipped stale frames |
| Status RGB | RGB and brightness | acknowledged applied color |
| Display | destination, Unicode text, visibility duration | acknowledged accepted text |
| Audio | frequency/melody and duration | acknowledged start/stop |
| RF | learned mapping or code/bits/protocol/pulse | acknowledged transmit result |
| Front panel | advertised page/action ID | acknowledged menu state |
| Raw | allowlisted opcode and payload | exact response; advanced diagnostics only |

Timeline authoring metadata is stored with the authoritative effect. Boolean
and motion cues use `duration_ms` to generate a deterministic release/stop.
PWM and RGB/addressable cues may add a target value or target color,
`easing`, and a bounded `sample_rate_hz`. Any cue may add `repeat_count` and
`repeat_interval_ms`. PCController expands these editor-friendly blocks into
ordinary acknowledged commands only in the volatile run plan; export/import
retains the editable curve and repetition data.

Semantic action IDs are preferred over physical relay numbers. Exact applied
relay masks may still be retained as recording evidence so playback remains
faithful when the same board profile is attached.

## Current implementation state

| Area | State | Remaining delivery work |
|---|---|---|
| One PCController discovery catalog and `effect:*` command family | Implemented | Remove remaining internal/UI “macro” nouns where they escape diagnostics. |
| Import/export and complete-definition upsert | Implemented | Add conflict preview and transactional UI feedback. |
| Relay/motion/PWM/display/RF/buzzer/RGB/addressable/raw timed steps | Implemented in the sequence compiler | Replace the remaining capability-bit checks with one advertised parameter schema shared by every editor. |
| Live host recording and device-timestamped relay capture | Implemented | Surface source/action evidence and gaps in both editors. |
| Device-retained bounded recording | Implemented | Present it only as a temporary retained take; verify recovery after a real disconnect. |
| Adaptive host/device execution | Implemented in this branch | Show policy and resolved clock consistently in Web, TUI, Pealayer, and runtime events. |
| MCU prebuffer/refill and timing evidence | Implemented | Complete physical long-run acceptance with cinema-seat output disconnected or safely isolated. |
| Host strip renderer and ACK-paced chunks | Implemented | Complete physical 100-pixel quality/latency acceptance and expose frame progress. |
| Mixed peripheral sequence | Implemented for command steps | Add first-class nested strip-program lanes so a declarative strip program and motion can share one effect. |
| Pealayer stable cues and hardware lanes | Partial | Consolidate its dual sequence/strip cue structs into one opaque controller-effect reference. |
| Pealayer exact sequence editor | Implemented for single-cue editing | The live-capability step catalog, lane ruler, zoom, snap/quantize, move/resize, leading-delay removal, type-aware curves/repetition, and recording handoff are wired to the PCController catalog. Add undo/redo, multi-select/copy/paste, and nested strip-program lanes. |
| Web editor | Partial | Reach feature parity with egui and consume the same editor schema. |
| TUI authoring | Partial | Add complete step/property editing; keep monitoring and run/stop compact. |
| Runtime progress/outcome | Partial | Publish one effect run ID with buffering, playing, stopping, completed/failed/cancelled, byte/step/frame progress, and timing evidence. |
| Cross-host routing | Implemented for authenticated command transport | Bind every run to one board session and show owner/consumer identity everywhere. |
| Live catalogs on DAVID-PC and CAFE-PC | Empty by deliberate user action | Create/import production effects; do not silently restore sample data. |

## Delivery order and acceptance gates

### 1. Contract convergence

- One `EffectDefinition`, one stable textual ID namespace, one catalog.
- Internal numeric MCU run IDs are allocated per run and never exposed as the
  durable effect identity.
- One capability-derived action schema covers all peripherals.
- Pealayer projects store only the stable effect reference and cue data.

Acceptance: create, rename, edit, export, delete, import, and rediscover the
same effect from CLI, TUI, Web, JSON-RPC, and Pealayer without duplicate data.

### 2. Professional editor

- Lane ruler with exact microsecond/millisecond entry.
- Add, reorder, duplicate, delete, enable, and group actions.
- Capability-aware parameter editors and validation.
- Recorded-versus-edited evidence, undo/redo, copy/paste, zoom, snapping, and
  preview from the selected step or range.
- Declarative strip program editor with color stops, envelope keyframes,
  direction, tails, period, FPS, pixel count, and live preview.
- Clear offline/read-only states without sample controls.

Acceptance: author a mixed seat + PWM + display + RF + strip effect without
editing JSON or typing physical protocol values that the board already knows.

### 3. Runtime orchestration

- Compile each lane into device-timed, host-timed, and strip-stream segments.
- Prebuffer before the cue deadline; start against one declared epoch.
- Refill with watermarks and acknowledgements; skip stale strip frames rather
  than accumulating latency.
- Abort the complete run on E-STOP, board-session replacement, capability
  drift, underrun beyond policy, or an unrecoverable command failure.

Acceptance: seek, pause, resume, resize, overlap-policy rejection, E-STOP, USB
disconnect, and reconnect all produce deterministic output and a truthful
terminal result.

### 4. Physical cinema acceptance

- Safely verify `seat.a` and `seat.b` up/down/stop recordings and replay.
- Verify raw relay visibility does not bypass semantic profile checks.
- Run the red/blue police, white thunder, and converging red-dot programs on
  the real strip at representative lengths and FPS.
- Measure cue-to-output latency, maximum timing error, strip frame rate,
  underruns, and disconnect behavior.
- Save screenshots and logs from PCController Web/TUI and Pealayer egui/Web.

Acceptance: the production catalog is available after restart, Pealayer cues
remain valid, and every run has evidence tied to the exact host, board session,
profile, effect ID, and commit.

## Commands available today

The living command family is:

```text
effect list
effect inspect effect:<id>
effect play effect:<id>
effect stop effect:<id>
effect record start <name> [category] [color]
effect record status
effect record save
effect record discard
effect export <path>
effect import <path> merge|replace
effect cancel
```

The same commands are executed through `controller.command.execute` for RPC
clients. Pealayer should never bypass PCController to write the library.
