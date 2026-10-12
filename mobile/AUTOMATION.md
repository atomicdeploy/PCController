# Native-local automation (preview 0.1.2)

Enable **Settings → Automation → Enable local automation**, then copy the
token. Disabled by default. The app must remain running and foreground; pause
closes listeners, peers and subscriptions. Resume does not replay commands.
This is IPC (inter-process communication), not an unrestricted remote proxy.

## Implemented transports

| Interface | Endpoint | Authorization and receipt |
| --- | --- | --- |
| HTTP JSON-RPC 2.0 | `http://127.0.0.1:18990/rpc`, POST | `Authorization: Bearer TOKEN`; JSON response |
| WebSocket JSON-RPC | `ws://127.0.0.1:18990/ws` | Bearer upgrade header; request/response and subscribed app state |
| TCP NDJSON | `127.0.0.1:18991` | Each newline-delimited request includes `auth`; newline-delimited response |
| Android explicit Intent | `ir.digitalogic.pccontroller.mobile/.AutomationReceiver` | Action `ir.digitalogic.pccontroller.mobile.RPC`; string extras `rpc`, `auth`; ordered-broadcast result |

No gRPC, shell execution, root grant, background foreground-service or direct
USB/Bluetooth board driver is provided. ADB, Termux, Tasker and root shells are
clients of these interfaces, not additional claimed protocols. Root is not
required. No Android API permission grants are silently requested.

Only IPv4 loopback is bound. For ADB access from a trusted workstation:

```sh
adb -s DEVICE forward tcp:18990 tcp:18990
curl http://127.0.0.1:18990/rpc -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"app.state"}'
```

On Termux, use the same curl command without ADB forwarding. For Tasker, use
an HTTP Request with that Bearer header; or Send Intent with the explicit
component/action and string extras. Sending an Intent alone does not promise
a result in Tasker; use HTTP when a response is required. An ordered broadcast
from ADB/Termux can read the receiver result:

```sh
adb -s DEVICE shell am broadcast \
  -n ir.digitalogic.pccontroller.mobile/.AutomationReceiver \
  -a ir.digitalogic.pccontroller.mobile.RPC \
  --es auth TOKEN \
  --es rpc '{"jsonrpc":"2.0","id":1,"method":"app.ping"}'
```

Within Termux replace `adb -s DEVICE shell am` with `termux-am`. A caller must
quote JSON for its shell; never put secrets in shared scripts or published
logs. Android may restrict broadcast visibility/background execution. The
receiver never starts a headless Flutter engine or launches the UI itself.
Its receipt timeout is eight seconds; timeout means outcome unknown, not an
instruction to retry. HTTP is preferred for larger replies.

NDJSON example (single line, terminated by a newline):

```json
{"auth":"TOKEN","jsonrpc":"2.0","id":1,"method":"app.state"}
```

WebSocket clients send `app.subscribe` after authentication. State notifications
use `app.state.changed`, coalesced to 250 ms. TCP is request/response only;
use WebSocket for push, including live front-panel/status state.

## Methods and safety

Read/UI methods: `app.ping`, `app.capabilities`, `app.state`, `app.subscribe`,
`app.navigation.set`, `app.capture`, `board.refresh`.
Navigation takes `section` (`pccontroller`, `pealayer`, `activities`, `workspace`)
and an optional PCController `page` (`monitor`, `outputs`, `front_panel`,
`firmware`, `effects`). Capture returns an app-only PNG, base64, width/height;
HTTP recommended. No desktop/other-app screen is captured. Max one capture per
second and 2 MiB PNG; capture is rejected over Intent to avoid Binder limits.

Connection methods `board.connect` / `board.disconnect` use the already saved
route. They do not accept arbitrary destinations or forward peer credentials.

Output methods: `board.action` (`action_id`), `board.pwm.set` (`channel`,
`percent`), `board.stop`, `board.estop`. Each requires `expected_target` equal
to the current `app.state.board.target`. Output actuation additionally needs
the separately confirmed **Allow output and playback actions** option. Motion
requires `confirm_motion: true`; action IDs/channels must be advertised and
unlocked. E-STOP blocks actuation. Stop/E-STOP assertion remain available to
authenticated callers without enabling actuation; E-STOP release, flash,
reset and arbitrary command execution are not exposed. Concurrent automation
mutations are rejected (safe stop/E-STOP can interrupt); nothing is retried.
Host safety admission still applies. Receipts do not prove physical completion.

Front-panel methods use the same `expected_target` and separately enabled
actuation fence, with host admission still authoritative:

| Method | Parameters |
| --- | --- |
| `board.menu.press` | `button`: `prev`, `next`, `dec`, `inc` in normal mode, or `K1`..`K4` when HOST captured |
| `board.display.show` | `display`: `segments` or actually available `lcd`; nonempty `text` up to 240 characters; optional `duration_seconds`: 1..30, default 5 |
| `board.buzzer.play` | integer `frequency_hz`: 20..20000; integer `duration_ms`: 1..1000 |
| `board.buzzer.stop` | Stops device mirroring and requests host melody Stop; works without actuation opt-in and under E-STOP |
| `board.mode.set` | `mode`: `idle` or `running` |

These methods are shared by HTTP, WebSocket, NDJSON/TCP and Android Intents.
They accept no arbitrary command text, reset/flash/E-STOP release or shell
execution. `app.capabilities.board_methods` advertises the implemented list.

`player.call` requires opted-in actuation and the current
`app.state.player.target`. Its `method` allowlist is `pealayer.play`,
`pealayer.pause`, `pealayer.stop`, `pealayer.previous`, `pealayer.next`,
`pealayer.seek_to`, `pealayer.volume.set`, `pealayer.mute.toggle`, and
`pealayer.workspace.set`; peer arguments are in `params`. No arbitrary RPC
proxy or file-opening automation is exported.

All requests require `jsonrpc: "2.0"` and a string/integer `id`. Batches and
fire-and-forget notifications are not dispatched. Request bounds: 32 KiB;
four WebSocket clients, four TCP clients, eight active requests and eight
queued NDJSON requests per client. No disk-based request log/history exists.
Tokens live in secure storage; revoke rotates the token and closes old clients.
Snapshots omit credentials, origins and media filenames. App capture can show
what the user sees, so possession of a token is an explicit privacy authority.

## Evidence boundary

ARM64 build and Flutter renderer captures are separate from installed-device
acceptance. The interfaces require live Android acceptance after installation
and enabling automation. gRPC and persistent background control remain future
work, not advertised capabilities.
