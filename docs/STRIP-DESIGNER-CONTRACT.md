# Host-owned strip designs

PCController owns every persisted strip effect and its stable ID. Pealayer is
an authoring/preview client; timeline cues refer to `effect:<id>`, not copied
programs or client-owned effects. Existing effect upsert, export/import, group,
delete, prepare and run contracts remain authoritative.

## Built-in versus examples

Rainbow remains the single built-in strip animation. Fresh configurations and
missing host providers no longer install or synthesize Police, White thunder
or Converging dots. `effect restore-examples` is the explicit, idempotent,
non-overwriting seed action. Existing saved definitions are user data and are
not deleted or replaced.

## Segments and layers

`StripProgram.primitive = "layers"` adds `pixels_per_meter`, `segments` and
`layers` alongside `period_ms`. A segment has a stable lowercase token `id`,
Unicode display `name`, and ordered, unique, zero-based `pixels`. Segments may
overlap; later enabled layers overwrite earlier ones. Unselected pixels are
black. Density is design metadata, not a remapping or physical auto-detection.

Each layer requires `id`, `name`, `segment_id`, `enabled`, `primitive`,
`primary`/`secondary` RGB objects, `period_ms`, `phase_ms`, `duty_percent`,
`easing`, `reverse`. Patterns: solid, gradient, ramp, blink, breathe. Easing:
linear, ease-in, ease-out, ease-in-out. Gradient follows segment array order;
ramp follows cycle time; blink uses duty percent; breathing uses a cosine
envelope. The frame timestamp is relative to the start of the effect/cue.

The host validates 1–64 segments/layers, explicit bounded pixel indices,
1–1000 px/m, cycle 50–3,600,000 ms, phase below period, duty 0–100, and all
required layer fields. Existing effect limits remain 1–100 pixels, 1–120
requested FPS, duration 100–3,600,000 ms. Prepared timelines render these same
host definitions; they clear the strip exactly at the authored cue end.

## Monitoring and acceptance

`peripheral.settings` advertises `strip.color_order` and `strip.color_orders`.
`outputs.strip_stats` exposes `pixels`, `requested_fps`, `frames_acked`,
`frames_failed`, `last_ack_unix_ms`, `last_error`. These are host-observed
complete-frame results for standalone strip/Rainbow streams, not a measurement
of physical refresh. The existing media-timeline status reports prepared cue
ACK/lateness separately. RGB-input receiver telemetry is separately owned;
future UI unification must not mislabel one stream's counters as another's.

Focused host tests cover sparse/order-preserving segments, layer precedence,
patterns/easing, malformed JSON, no implicit demos, host upsert and prepared
10,000–11,000 ms cue boundaries. Native/Web source checks are in Pealayer.
Production packages, physical LEDs/color order, observed timing and soak are
separate acceptance gates. No firmware opcode or flash write was changed here.
