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

The snapshot separately reports client freshness and board acknowledgement:
`connected`, `received_at`, `board_synced`, `board_error`, `board_sequence`,
`board_synced_at`, `board_round_trip_ms`. An unsupported firmware response is
visible and retried at most once per second, rather than silently accepted.

## Board contract

Native opcode `MEDIA_CLOCK` (`0x4A`) carries fourteen bytes:

| Offset | Value |
| --- | --- |
| 0 | schema 3 |
| 1 | bit 0 loaded, bit 1 advancing; only 0, 1, 3 valid |
| 2–5 | elapsed milliseconds, little-endian uint32 |
| 6–7 | rate Q8, little-endian uint16 |
| 8–9 | host lease 3000 ms, little-endian uint16 |
| 10–13 | exact four TM1637 segment cells |

Numeric/rate metadata remains available on the host; the constrained AVR
retains only exact raw cells in the **existing** host-display buffer with a
fixed three-second lease. No EEPROM writes, feature removal, or duplicate
physical presenter is needed. VirtualBoard models the same retention/expiry.
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
cmake --build Tools/VirtualBoard/.build/release --target virtual_board media_clock_tests virtual_board_tests
ctest --test-dir Tools/VirtualBoard/.build/release -R 'media_clock_unit|virtual_board_unit' --output-on-failure
build.cmd --firmware-only --skip-tests
```

Deploy host/firmware only through the owned bridge/update transaction. Physical
acceptance additionally requires a connected real board, verified firmware
identity, play/pause/seek/speed/unload checks, and observed front-panel output.
Virtual-board success is not physical acceptance evidence.
