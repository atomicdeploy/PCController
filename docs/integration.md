# Commands and integrations

RayanLamp has one living command contract. UI buttons, the tray menu, global
hotkeys, the CLI, terminal UI, WM_COPYDATA, named pipe, loopback HTTP, and COM
Automation all reach the same dispatcher when operating through the GUI.

External native and Go applications can use the optional attach-only SDK
without reconstructing the pipe protocol. Its generic call reaches this same
table, while its explicit `EnsurePrimaryW` bootstrap is the only DLL operation
that may launch the unified executable. See [Native and Go SDK](sdk.md).

## Command methods

| Canonical method | Parameters | Result/purpose |
| --- | --- | --- |
| `ping` | none | App name, version, and primary-instance marker |
| `status` | none | Current snapshot; aliases: `state`, `lamp.get`, `get` |
| `lamp.on` | none | Turn on; alias: `on` |
| `lamp.off` | none | Turn off; alias: `off` |
| `lamp.toggle` | none | Toggle; alias: `toggle` |
| `lamp.refresh` | none | Force an HTTP `/lamp/get`; alias: `refresh` |
| `reconnect` | none | Restart device transports |
| `window.show` | none | Show/activate and remember visibility; alias: `show` |
| `window.hide` | none | Hide and remember visibility; alias: `hide` |
| `settings.show` | none | Open settings; alias: `settings` |
| `diagnostics.show` | none | Open diagnostics |
| `diagnostics.fetch` | `{"endpoint":"/info"}` | Fetch one of six public, noncredential GET endpoints |
| `terminal.status` | none | Telnet-style device-terminal connection metadata |
| `terminal.connect` | none | Manually connect to the configured lamp hostname and `telnet_port` |
| `terminal.read` | `{"after":0,"limit":32768,"wait_ms":1000}` | Ordered, cursor-based bounded terminal output |
| `terminal.write` | `{"data_base64":"c3RhdHVzDQo="}` | Send up to 4096 terminal bytes without putting plaintext in the JSON encoding |
| `terminal.disconnect` | none | Close the shared device-terminal session |
| `config.get` | none | Return complete configuration |
| `config.patch` | JSON object | Recursively merge, validate, apply, and save |
| `integration.registration.status` | none | Read exact-owned URI/Jump List and COM registration for the current executable |
| `integration.shell.register` | none | Explicitly register the current executable's URI handler and Jump List tasks |
| `integration.shell.unregister` | none | Ownership-safely remove this executable's URI and Jump List integration |
| `integration.com.register` | none | Explicitly register COM Automation for the current executable |
| `integration.com.unregister` | none | Ownership-safely remove this executable's COM Automation registration |
| `notifications.show` | `{"title":"…","message":"…"}` | Show a local notification; alias: `notify` |
| `audio.play` | `{"cue":"message","count":1}` | Play `on`, `off`, `message`, or `error` through the selected shared SFX source; count is 1–10; aliases: `audio.test`, `sound.test` |
| `audio.import` | `{"path":"C:\\Sounds\\cue.wav"}` | Validate and content-address one local WAV through the active primary; returns its ID and display name |
| `version` | none | Build product metadata |
| `quit` | none | Exit primary instance; alias: `exit` |

Method normalization trims whitespace, lowercases, and removes leading `/` or
`-`. Unknown methods return `not_found`. Titles are limited to 128 Unicode
characters and messages to 4096.

`diagnostics.fetch` intentionally rejects `/config/ap` and `/config/st`.
The coordinator uses those credential-bearing firmware responses only as
internal parsed projection sources; their exact bodies are never available over
CLI, TUI, named pipe, WM_COPYDATA, COM, or the loopback HTTP API.

## CLI

The single executable exposes both direct handoff flags and an explicit
controller-only mode:

```powershell
RayanLamp.exe /on
RayanLamp.exe /settings
RayanLamp.exe cli --status --json
RayanLamp.exe cli --endpoint /info
RayanLamp.exe cli --notify "Text" --title "Optional title"
RayanLamp.exe cli --audio --cue on --count 2
```

Windows aliases include `/on`, `/off`, `/toggle`, `/get`, `/status`, `/show`,
`/hide`, `/reconnect`, `/settings`, `/diagnostics`, `/quit`, `/version`,
`/help`, `/?`, `/json`, `/quiet`, and `/background`. GNU-style flags include
`--timeout`, `--config`, `--no-color`, and `--start-minimized`. `--audio`
defaults to the `message` cue once; `--cue` accepts `on`, `off`, `message`, or
`error`, and `--count` accepts 1 through 10. Exactly one command may be
selected.

The `cli` mode uses the named pipe and requires the primary app. Direct command
flags retain the original behavior: the executable first probes the named
pipe/window; mutations normally use WM_COPYDATA for fast handoff, while status,
version, config, and diagnostics queries use the named pipe for a structured
response.

## Terminal UI

`RayanLamp.exe tui` is the interactive console surface:

```powershell
RayanLamp.exe tui
RayanLamp.exe tui --mode ipc
RayanLamp.exe tui --mode api
RayanLamp.exe tui --mode standalone
RayanLamp.exe tui commands
RayanLamp.exe tui settings
RayanLamp.exe tui device
RayanLamp.exe tui serving
RayanLamp.exe tui diagnostics
RayanLamp.exe tui --terminal
```

An optional screen operand enters the requested terminal workspace without
first navigating through the dashboard. Canonical screens are `dashboard`,
`commands`, `settings`, `device`, `serving`, `diagnostics`, and `terminal`.
`configure`, `serve`, and `telnet` are positional aliases for `settings`,
`serving`, and `terminal`.

The default `--mode auto` probes the current-user named pipe, then the
authenticated loopback API from the resolved configuration, then attempts to
become the headless primary. `--mode ipc` and `--mode api` fail clearly when
their requested transport is unavailable; neither silently starts a direct
device connection. Their status views poll the primary instance's in-memory
state every second by default.

API attachment reads the loopback address and bearer token from the selected
protected configuration. A fully explicit `--api-url` plus `--api-token` does
not require the configuration file; explicit IPC never loads it. A token
supplied on a command line may be exposed through shell history or process
inspection and should be avoided for normal use. Tokens are never rendered in
the interface or written to its activity log.

Standalone mode starts no GUI process. The TUI loads and validates the same
configuration, atomically claims the ordinary per-user named pipe, and only
then connects to `/ws`. It performs lamp commands over `/lamp/*`, applies the
same reconnect and `.local` caching rules, and tears the connection down when
the terminal exits. While it runs, `RayanLamp.exe cli` and other named-pipe
clients attach to the TUI's headless primary instead of creating another lamp
controller. When `integration.local_api_enabled` is true, standalone mode also
owns the embedded loopback API/PWA listener. TUI serving commands gracefully
start, stop, or rebind it and report the observed URL or listener error rather
than inferring success from configuration alone.

Press `:`/`Ctrl+P` for the searchable command center, `s` for the complete
TUI-native application settings, `v` for the typed device workspace, and `w`
for serving. Settings are grouped across lamp, rendering, window behavior,
notifications, hotkeys, automation, integration, and an advanced complete JSON
editor. `Ctrl+S` validates and applies the active section through the canonical
`config.patch` dispatcher, so GUI/Web clients receive the same live update.
The Notifications & sound section controls the shared bundled/custom source
and content-addressed asset ID. Its optional local WAV path invokes
`audio.import`, then selects the returned ID without copying WAV bytes into
configuration JSON.
The integration workspace separately queries the exact-owned Windows
registration state and offers one-time Shell and COM actions. Those actions
are never stored in configuration or replayed at startup; unregister requires
an explicit ownership warning confirmation.

Device Settings is deliberately distinct from application settings. It renders
only the typed sanitized projection of audited firmware endpoints. The current
audited firmware routes its shell to Serial, so network parameter writes are
capability-locked and clearly reported as read only; `/config/ap` and
`/config/st` are not misrepresented as writable and their password fields are
never returned to the TUI.

## Headless web dashboard

The single executable can instead make the browser dashboard the presentation
surface of a true headless primary:

```powershell
RayanLamp.exe serve
RayanLamp.exe serve --no-open
RayanLamp.exe serve --config C:\path\config.json
RayanLamp.exe serve --listen 127.0.0.1:49123
```

This mode does not construct Walk, an HWND, tray state, global hotkeys,
notifications, or native dialogs. It atomically claims the same named pipe,
starts the shared lamp core, automation scripts and audible event effects, and
serves the embedded SPA/PWA through the authenticated loopback API. The local
API is explicitly enabled in the selected configuration; an explicit
`--listen` loopback address is validated and persisted.

The default launch opens the active `/app/` URL. `--no-open` only suppresses
that initial browser launch. `window.show`, `settings.show`,
`diagnostics.show`, and `web.show` handoffs open the dashboard or its matching
browser route, so a later ordinary `RayanLamp.exe` launch hands over cleanly.
Native-window, native-notification, and hotkey-capture capabilities are
reported false to the browser. `window.hide` and notification commands remain
unavailable because the headless process does not pretend to own those native
services.

Changing the API enabled state, listener, or token through IPC/API broadcasts
the authoritative configuration first, then asynchronously shuts down and
rebinds the listener. This avoids waiting for an HTTP server from inside the
configuration request it is currently serving. Disable leaves the coordinator
and named-pipe IPC alive; it only removes the HTTP dashboard. Stop the host
with Ctrl+C or `RayanLamp.exe cli --quit`.

If another primary already exists, ordinary `serve` asks it to open its web
dashboard and exits. `serve --no-open` fails clearly because it did not become
the requested long-running owner. A standalone TUI owns the same loopback
listener when `integration.local_api_enabled` is true; its Serving workspace
can start, stop, and rebind that listener. `web.show` returns a real
unavailable error when serving is disabled or a bind failed instead of
claiming that a browser was opened.

The complete TUI launch surface is:

```text
RayanLamp.exe tui [screen]
  [--mode auto|ipc|api|standalone]
  [--config PATH]
  [--api-url URL] [--api-token TOKEN]
  [--poll DURATION] [--timeout DURATION]
  [--theme auto|dark|light]
  [--screen NAME]
  [--commands]
  [--settings|--configure]
  [--device-settings]
  [--serving]
  [--diagnostics]
  [--terminal|--telnet]
  [--no-alt-screen] [--no-mouse]
  [--version] [--help]
```

Defaults are a one-second attached-state poll, five-second command timeout,
dashboard entry, automatic theme, alternate-screen rendering, and mouse input.
Only one initial screen may be selected; equivalent aliases may be combined.
Automatic mode
starts from the Windows app-theme/system-color fallback so unsupported OSC
queries never cause a wrong-color flash, then lets the terminal's reported
background take precedence. `Ctrl+T` cycles `auto`, `dark`, `light`, and back
to `auto`; `--theme` selects the initial override. A non-empty `NO_COLOR`
environment variable disables TUI color without disabling layout or emphasis.
`TERM=dumb`, `CLICOLOR=0`, and `FORCE_COLOR=0` also select plain output;
`FORCE_COLOR=1` or `CLICOLOR_FORCE=1` may opt a known-compatible redirected
consumer into SGR styling. Emoji are automatic only for an interactive
Unicode-capable terminal. `NO_EMOJI=1` selects stable ASCII symbols, while
`RAYANLAMP_EMOJI=always` or `never` provides an explicit override. The TUI
itself requires interactive stdin and stdout; scripts and redirections should
use `RayanLamp.exe cli`, which never emits terminal controls by default.

`--telnet` is an alias for `--terminal`. Core keys are
`Space`/`Enter`/`t` to toggle, `o` and `f` for explicit on/off,
`r` to force a state refresh, `c` to reconnect, `d` to open the Huh
diagnostics workspace, `x` to open the shared device terminal,
`:`/`Ctrl+P` for commands, `s` for complete app settings, `v` for typed
device settings, and `w` for serving. `Ctrl+S` validates and live-applies the
active settings section. Arrow keys or `j`/`k` browse activity/results,
`Ctrl+T` to cycle the palette, `?` to expand help, and `q`/`Ctrl+C` to quit.
`Esc` leaves filter entry first, then closes the active workspace or result;
it never unexpectedly quits from the dashboard. In the raw device terminal,
`Ctrl+C` sends ETX and `Esc`/`Ctrl+]` detach.

The TUI activity stream and the Web dashboard's browser console use the same
stable event vocabulary and severity levels. Events such as `lamp.state`,
`lamp.command`, `websocket.connected`, `message.received`, `audio.cue`, and
`diagnostics.loaded` therefore mean the same thing in either surface. DevTools
renders them as theme-aware CSS badges with emoji and grouped, allow-listed
context; the TUI renders the same level and event name using its active
Lip Gloss palette. Poll heartbeats and unchanged snapshots are intentionally
quiet, and credentials, cookies, session material, arbitrary terminal
transcripts, and raw diagnostic bodies are never written to either stream.

The device terminal is an explicit, plaintext local-network tool and never
connects on application bootstrap. Output is kept only in a bounded in-memory
cursor ring and is not sent through activity events, notifications,
automation, logs, local storage, the service worker, or BroadcastChannel.
An explicit disconnect or a new device session purges the prior transcript.
The TUI, IPC/API clients, and browser terminal observe the same ordered
session. The audited firmware currently compiles `ShellOnSerial`, so its
network listener is banner/read-only until the firmware is intentionally
rebuilt with a secured network shell.

## IPC JSON envelope

Protocol version 1 uses one JSON request and response:

```json
{
  "protocol_version": 1,
  "id": "example-1",
  "method": "lamp.toggle"
}
```

Success:

```json
{
  "protocol_version": 1,
  "id": "example-1",
  "ok": true,
  "result": "on"
}
```

Failure:

```json
{
  "protocol_version": 1,
  "id": "example-1",
  "ok": false,
  "error": {
    "code": "device_unavailable",
    "message": "lamp GET /lamp/get: ..."
  }
}
```

IDs are caller-selected correlation strings. The primary dispatcher also
accepts protocol version zero for internal direct calls, but external clients
should always send version 1.

For example, an IPC client can request two selected message effects without
requesting a visual notification:

```json
{
  "protocol_version": 1,
  "id": "audio-1",
  "method": "audio.play",
  "params": {"cue": "message", "count": 2}
}
```

## Per-user named pipe

The pipe name has the form:

```text
\\.\pipe\RayanLamp.<current-user-SID-hash>
```

The suffix is the lowercase hex encoding of the first eight SHA-256 bytes of
the current user's SID. The Windows security descriptor grants full access
only to that SID and SYSTEM. The wire format is one JSON request followed by
`\n`, then one JSON response. Requests and responses are limited to 1 MiB;
connections and command execution have deadlines.

Use `RayanLamp.exe cli` or the Go IPC package instead of reconstructing this
private pipe name in normal automation.

## WM_COPYDATA

The primary top-level window is discovered by the property:

```text
RayanLamp.PrimaryWindow.{66F0C709-6E91-45BE-9013-5A96219E6D3D}
```

`WM_COPYDATA` payloads use tag `0x524C414D` (`RLAM`), contain the same request
JSON, and are limited to 64 KiB. Sending uses `SendMessageTimeout` with
abort-if-hung semantics. The return value only acknowledges acceptance; callers
requiring result data must use the named pipe or local HTTP API.

This surface is intended for same-session Windows handoff, not remote control.
Its receiver still validates magic, size, JSON fields, protocol, and method.

## Loopback HTTP API

The default base is:

```text
http://127.0.0.1:47831
```

`GET /healthz` is the unauthenticated JSON health endpoint. Non-browser
clients access the command routes in the table with exactly one header:

```text
Authorization: Bearer <integration.local_api_token>
```

Token comparison uses SHA-256 and constant-time comparison. The server refuses
non-loopback listeners, sets no-store and browser hardening headers, limits
headers/bodies, and applies read/write/command deadlines.

The embedded browser instead obtains a short-lived HttpOnly session from
`/app/session`. Its `/ui/*` and `/events` routes require that same-origin
session, and mutations additionally require the in-memory CSRF token.

| HTTP | Route | Dispatcher command / purpose |
| --- | --- | --- |
| `GET` | `/status` | `status` |
| `POST` | `/lamp/on` | `lamp.on` |
| `POST` | `/lamp/off` | `lamp.off` |
| `POST` | `/lamp/toggle` | `lamp.toggle` |
| `POST` | `/lamp/refresh` | `lamp.refresh` |
| `POST` | `/window/show` | `window.show` |
| `POST` | `/window/hide` | `window.hide` |
| `POST` | `/reconnect` | `reconnect` |
| `GET` | `/diagnostics?endpoint=/info` | `diagnostics.fetch` |
| `GET` | `/terminal/status` | `terminal.status` |
| `POST` | `/terminal/connect` | `terminal.connect` |
| `GET` | `/terminal/output?after=0&limit=32768&wait_ms=1000` | `terminal.read` |
| `POST` | `/terminal/input` | `terminal.write` |
| `POST` | `/terminal/disconnect` | `terminal.disconnect` |
| `GET` | `/config` | `config.get` |
| `PATCH` | `/config` | `config.patch` |
| `GET` | `/integration/registration` | `integration.registration.status` |
| `POST` | `/integration/registration` with an allowlisted `{"method":"…"}` body | selected registration mutation |
| `POST` | `/notifications` | `notifications.show` |
| `POST` | `/audio/play` with optional `{"cue":"message","count":1}` | `audio.play` |
| `POST` | `/audio/import` with `{"path":"…"}` | Bearer-authenticated local-file `audio.import` for TUI/API automation |
| `GET`, `HEAD` | `/ui/audio/{cue}` | Browser-session stream of the currently selected PCM WAV |
| `POST` | `/ui/audio/import` with raw `audio/wav` | Validate/store a custom WAV and atomically select its SHA-256 ID |
| `GET` | `/version` | `version` |

The loopback HTTP and WebSocket API is intentionally versionless: these paths
are the living protocol, and no `/v1/...` compatibility aliases are retained.
On graceful coordinator shutdown, `/events` is closed with WebSocket code
`1001` and reason `server shutting down` before slower service cleanup begins.
The Web UI immediately invalidates stale control availability, broadcasts the
loss to sibling tabs, switches its favicon to the offline lamp, and renews its
short-lived browser session before reconnecting to a restarted executable.
The embedded browser additionally uses `/ui/terminal`, a same-origin,
browser-session-authenticated WebSocket dedicated to terminal bytes. It does
not reuse `/events`; high-volume terminal output therefore cannot delay
lamp/configuration/diagnostics events. Binary browser frames carry terminal
bytes, while small JSON text frames carry connect/disconnect/status/error
control data. xterm's bare Enter keystroke is normalized to CRLF because the
audited firmware executes commands on LF; pasted and other raw bytes are left
unchanged. Direct/static browser mode cannot open arbitrary raw TCP sockets, so
the terminal is available only through the Go coordinator (including a static
deployment behind a same-origin coordinator proxy).

`POST /audio/play` with no body retains the `message`/one-count default. A
JSON body is strict and accepts only `cue` and `count`. Audio dispatch is
independent of toast/balloon delivery and returns the normalized cue/count.
The primary GUI, standalone TUI, or `serve` host owns actual playback, so a
coordinator-backed browser asks it to play once instead of echoing the sound
in every open tab.

The two `/ui/audio/*` routes use the short-lived browser session and require
CSRF protection plus the current `If-Match` configuration revision for import.
`GET /ui/audio/{cue}` accepts `on`, `off`,
`message`, or `error` and returns the exact resolved WAV with an ETag. Import
accepts at most 4 MiB of validated integer PCM WAV and an optional
`X-RayanLamp-Filename` display-name header; its response is the usual redacted
UI configuration/revision shape, never a filesystem path or bearer token.
Direct/static mode has no Go owner, so the elected realtime tab alone plays
the canonical `shared/sfx/{cue}.wav` files or a SHA-256-addressed custom WAV
from browser IndexedDB. BroadcastChannel followers update state and messages
without replaying audio. Coordinators advertise this surface as the
`audio_sfx` capability; direct mode supplies the equivalent browser
implementation.

Terminal reads are cursor based. When `reset` is true, discard the visible
transcript and still advance through every returned chunk. Session purges use
an empty, sequence-bearing chunk with `reset: true`, so consuming that marker
prevents a reset loop and fences later output to the new session.

No-body routes reject request bodies. Ordinary JSON routes require
`Content-Type: application/json`; bodies are limited to 1 MiB and reject
unknown members. The one deliberate exception is raw custom-audio import,
which requires `audio/wav` and has its own 4 MiB limit. Configuration patches
recursively merge into the current value, validate a new runtime client before
committing, save atomically, and only then update shell integrations.

The registration endpoint accepts exactly the four
`integration.shell.{register,unregister}` and
`integration.com.{register,unregister}` mutation names; it cannot dispatch an
arbitrary command. The browser-only equivalent is
`/ui/integration/registration`, which requires a same-origin browser session
and CSRF protection for `POST`. Settings clients refresh the read-only status
after every attempted mutation, including partial shell-registration failures.

The native shell also persists `ui.window_visible`, `ui.window_state`, and the
normal `ui.window_bounds` restore rectangle. Unrelated partial configuration
patches merge against the latest on-disk value so they cannot roll a newer
window session back. Explicit `window.show` and `window.hide` requests update
remembered visibility when they run. A command-only background bootstrap and
the background/start-minimized policy can start hidden without erasing the
remembered session.

PowerShell example:

```powershell
$config = Get-Content "$env:APPDATA\RayanLamp\config.json" -Raw |
  ConvertFrom-Json
$headers = @{
  Authorization = "Bearer $($config.integration.local_api_token)"
}

Invoke-RestMethod http://127.0.0.1:47831/status -Headers $headers
Invoke-RestMethod http://127.0.0.1:47831/lamp/toggle `
  -Method Post -Headers $headers
Invoke-RestMethod http://127.0.0.1:47831/notifications `
  -Method Post -Headers $headers -ContentType application/json `
  -Body '{"title":"Office","message":"Test notification"}'
Invoke-RestMethod http://127.0.0.1:47831/audio/play `
  -Method Post -Headers $headers -ContentType application/json `
  -Body '{"cue":"message","count":2}'
```

Avoid command-line token arguments, shell history, transcript logs, and CI
output. Read it at runtime from the protected configuration or another
current-user secret store.

## Optional Windows shell integration

Portable launch never writes shell registration. A user or installer opts the
current executable in and out explicitly:

```powershell
RayanLamp.exe /register-shell
RayanLamp.exe /unregister-shell
```

The native Settings window, TUI integration workspace, and coordinated Web
Settings expose the same explicit controls and exact executable path. Merely
opening any settings surface performs a read-only status query and never
registers the app. The static/direct-to-lamp Web UI correctly reports these
host-only controls as unavailable.

Both operations are per-user and require no elevation. Registry changes are
limited to `HKCU\Software\Classes`; Jump List data is published through the
current user's Windows shell store. Add `/quiet` for noninteractive installer
use. Manual use retains the native confirmation dialog.

Registration installs an ownership-checked `rayan-lamp` URI handler. Supported
activations are deliberately parameter-free:

| URI | Canonical command |
| --- | --- |
| `rayan-lamp://on` | `lamp.on` |
| `rayan-lamp://off` | `lamp.off` |
| `rayan-lamp://toggle` | `lamp.toggle` |
| `rayan-lamp://show` | `window.show` |
| `rayan-lamp://hide` | `window.hide` |
| `rayan-lamp://reconnect` | `reconnect` |
| `rayan-lamp://settings` | `settings.show` |
| `rayan-lamp://diagnostics` | `diagnostics.show` |
| `rayan-lamp://web` | `web.show` |

The URI parser accepts the equivalent single-path form
`rayan-lamp:/action`, is bounded to 2048 visible ASCII bytes, and rejects
unknown actions, parameters, queries, fragments, credentials, ports, escapes,
and extra path components. Activations enter the same single-instance
handoff/dispatcher as ordinary command flags.

The same opt-in command publishes native unpackaged Win32 Jump List tasks with
the stable AppUserModelID `YektaYar.RayanLamp`:

- Toggle lamp
- Turn lamp on
- Turn lamp off
- Show Rayan Lamp
- Open settings
- Reconnect

Tasks are static `IShellLink` entries that target the registered executable and
remain useful when the app is not already running. Windows policy can disable
Jump Lists. If URI registration succeeds but Jump List publication fails,
`/register-shell` preserves the useful URI association, reports a partial
result, and exits with code `3`.

Registration refuses to replace a URI association owned by another
executable. Unregistration rechecks the exact quoted command before deletion,
preserves foreign registrations, and independently attempts Jump List, URI,
and owned legacy toast-metadata cleanup. Moving or renaming a registered
portable executable invalidates the stored path; unregister before moving it
or register again from the new location.

## Optional COM Automation

Registration is explicit, per user, and requires no elevation:

```powershell
RayanLamp.exe /register-com
RayanLamp.exe /unregister-com
```

It writes only below `HKCU\Software\Classes`. Stable identifiers:

| Item | Value |
| --- | --- |
| ProgID | `YektaYar.RayanLamp` |
| Versioned ProgID | `YektaYar.RayanLamp.1` |
| CLSID | `{D9603C3B-7D90-53B2-AC01-7656B027603F}` |
| Server | `"RayanLamp.exe" /com-server` |

On 64-bit Windows, the local-server CLSID is published in both the 32-bit and
64-bit per-user registry views, so 32-bit Office/VBA or AutoHotkey and 64-bit
clients activate the same out-of-process server. Registration and removal
validate the executable, ProgID, `CurVer`, and CLSID ownership markers before
changing anything; foreign or incomplete registrations are preserved and
reported instead of overwritten.

PowerShell:

```powershell
$lamp = New-Object -ComObject YektaYar.RayanLamp
$lamp.On()
$lamp.Off()
$lamp.Toggle()
$lamp.Show()
$lamp.Hide()
$lamp.Reconnect()
$lamp.Notify("Hello from COM")
$state = $lamp.GetState()
$sameState = $lamp.State
```

`Show()` and `Hide()` control the native main window without changing the lamp.
`State` is read-only. State results are `on`, `off`, or `unknown`. COM
activation starts the primary app in the background if needed, then the local
server proxies every call through the per-user pipe with a 12-second deadline.
The COM server exits after the last activated object/lock is released.

## Automation

Automation is not an IPC transport; it reacts to confirmed state changes and
validated firmware messages. Each process receives:

| Variable | Value |
| --- | --- |
| `RAYANLAMP_EVENT` | `state`, `message`, `lock`, `unlock`, `suspend`, `resume`, or `network-change` |
| `RAYANLAMP_STATE` | `on`, `off`, or `unknown` |
| `RAYANLAMP_PREVIOUS_STATE` | previous state |
| `RAYANLAMP_SOURCE` | source such as `websocket`, `command`, or `windows` |
| `RAYANLAMP_MESSAGE` | validated message text, otherwise empty |
| `RAYANLAMP_DEVICE_URL` | normalized configured device URL |
| `RAYANLAMP_EVENT_TIME` | UTC RFC 3339 timestamp with nanoseconds |

Example `on.ps1`:

```powershell
$record = [ordered]@{
  event = $env:RAYANLAMP_EVENT
  state = $env:RAYANLAMP_STATE
  previous = $env:RAYANLAMP_PREVIOUS_STATE
  source = $env:RAYANLAMP_SOURCE
  at = $env:RAYANLAMP_EVENT_TIME
}
$record | ConvertTo-Json -Compress
```

Scripts run with the same user rights as RayanLamp and can access that user's
files, network, registry, and secrets. There is no sandbox. Use absolute paths,
keep scripts writable only by trusted users, avoid evaluating message text as
code, and leave automation disabled unless the configured scripts have been
reviewed. Execution details and limits are in
[Configuration](configuration.md#automation).
