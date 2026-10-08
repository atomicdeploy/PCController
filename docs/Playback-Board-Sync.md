# Client playback events and physical display synchronization

Pealayer is a consumer with a leased identity, not a second serial-port owner.
The existing `controller.app.instance.report` registry advertises its process
ID and `application`, `version`, `commit`, `os`, `arch` values. Report every
ten seconds with a forty-five-second lease; remove the identity on disconnect.
Query `controller.app.instances` to inspect consumers.

Register before calling `controller.media.playback.update`:

```json
{"jsonrpc":"2.0","id":1,"method":"controller.app.instance.report","params":{"id":"player:my-instance","surface":"pealayer","state":"active","lease_seconds":30,"self":{"kind":"process","pid":1234},"values":{"application":"Pealayer"}}}
{"jsonrpc":"2.0","id":2,"method":"controller.media.playback.update","params":{"client_id":"player:my-instance","sequence":1,"position_ms":65000,"duration_ms":120000,"loaded":true,"playing":true,"rate":1}}
{"jsonrpc":"2.0","id":3,"method":"controller.media.playback.get","params":{}}
```

Sequences increase per live identity. Replays and competing live clock owners
are rejected; unload releases ownership. Unknown duration is omitted/null,
not fabricated. Position and duration are bounded unsigned 32-bit milliseconds,
and rate is 0.25–4. Updates require the existing board-control permission.
Each accepted update emits `media.playback` on the state event stream, with
client identity, sequence, position, duration, advancing state, loaded and rate.
`controller.snapshot` also includes `media_playback`.

Use `/ipc` WebSocket topic `state` for these notifications, not the separate
activity topic `events`:

```json
{"jsonrpc":"2.0","id":4,"method":"controller.subscribe","params":{"topics":["state"],"interval_ms":100}}
```

Notifications have method `controller.state`, kind `media.playback`, and the
playback fields in `params.metadata`.

The snapshot separately reports client freshness and board acknowledgement:
`connected`, `received_at`, `board_synced`, `board_error`, `board_sequence`,
`board_synced_at`, `board_round_trip_ms`. An unsupported firmware response is
visible and retried at most once per second, rather than silently accepted.

## Board contract

The host reuses native `DISPLAY_TEXT` target 5, the board's advertised
scheduled-segment contract. Its twelve-byte raw-cell form is:

| Offset | Value |
| --- | --- |
| 0 | scheduled-segment target 5 |
| 1–2 | non-scrolling speed field, 80 ms |
| 3 | four cells |
| 4 | raw-cell option `0x20`, with no repeat or scrolling bits |
| 5–6 | host lease 3000 ms, little-endian uint16 |
| 7 | zero interval |
| 8–11 | exact four TM1637 segment cells |

An eight-byte target-5 payload with a zero cell count releases the display.
Numeric/rate metadata remains available on the host; the constrained AVR
retains only exact raw cells in the **existing** host-display buffer with a
fixed three-second lease. Opcode `0x4A` stays reserved and is not a second
media-clock protocol. No EEPROM writes, feature removal, or duplicate physical
presenter is needed. VirtualBoard models the same retention/expiry.
Existing `PROGRAM_STATE` claims communicate advancing/paused activity and
compose with other active owners rather than cancelling their work.

The host formats `mm:ss` through 99:59, then `hh:mm`, with a blinking separator
while advancing and steady separator when paused. It sends samples at roughly
10 Hz during playback, immediately on changes, and refreshes at least 5 Hz
between paused client heartbeats. Reconnect reasserts the newest clock.
Editors, warnings, programming and learning keep priority over ordinary host
display pages. Unload, stale-client expiry and board lease expiry return local
display ownership. E-STOP prevents advancement and releases this program claim.

Clock estimates use MPV decoded-time samples, not pending logical seek targets.
The host bounds RTT/2 transit compensation to 100 ms and exposes measured RTT.
This is best-effort bounded real-time presentation, not hard real-time or
frame-perfect synchronization over arbitrary serial/network latency.

## Verification

Use the stable Windows runner, not direct `go test`:

```powershell
node Tools/Build/go-tests.mjs --package internal/control --package internal/ipcjson --run TestMediaPlayback
cmake --build Tools/VirtualBoard/.build/release --target virtual_board virtual_board_tests
ctest --test-dir Tools/VirtualBoard/.build/release -R virtual_board_unit --output-on-failure
build.cmd --firmware-only --skip-tests
```

Deploy host/firmware only through the owned bridge/update transaction. Physical
acceptance additionally requires a connected real board, verified firmware
identity, play/pause/seek/speed/unload checks, and observed front-panel output.
Virtual-board success is not physical acceptance evidence.

The 2026-10-06 live check used actual decoded libmpv time, verified play/pause,
seek, duration, 2x speed, one shared registered identity and 31 subscribed
playback events. It restored the user's original position/rate/paused state.
The full default AVR build passed with source identity `7B81735D`, estimated
free SRAM 281 bytes and only 6 application-flash bytes spare. No features were
disabled. Firmware was not flashed: physical-board acceptance remains pending.
The host replacement was acknowledged by its owned updater operation
`op-cfa87047799ff23c` (`terminal_verified=true`).
Detailed evidence and the runnable check are maintained with
[Pealayer PR #46](https://github.com/ToghrolTP/pealayer/pull/46), under
`docs/verification/playback-board-results.md`.
