# RayanLamp

RayanLamp is a native Windows controller for the lamp at
`http://rayanlamp.local`. It is a clean Go implementation with a compact
blurred/acrylic window, animated lamp artwork, a notification-area presence,
global hotkeys, resilient WebSocket state updates, notifications, automation,
and several deliberately constrained integration surfaces.

The app connects to `/ws` at startup, requests one state snapshot, and then
listens for `ON`, `OFF`, `message:`, `msg:`, and `beep:` events. HTTP commands
remain available through `/lamp/get`, `/lamp/on`, `/lamp/off`, and
`/lamp/toggle`. The tray icon and UI follow the most recent confirmed state.

## Highlights

- Resizable compact and dashboard views with none/custom/native title-bar
  choices, system/light/dark themes, and the GlassDWM-inspired auto, Mica,
  tabbed, acrylic, blur, transparent, and solid modes.
- One live appearance policy across native chrome, main/tray menus, open
  dialogs, TaskDialogs, the web/PWA shell, and the TUI: Windows light/dark,
  High Contrast, transparency, and reduced-motion preferences are respected,
  while Light/Dark and terminal palette overrides remain available.
- Premultiplied-alpha Fluent controls with live DWM material, reveal lighting,
  elevation, animated hover/press states, and compatible flat fallbacks.
- Adaptive native rendering with `auto`, `directx`, and `legacy` choices.
  Supported machines use a pure-Go D3D11/DXGI/DirectComposition presentation
  path; initialization failures and graphics-device loss fall back
  automatically to the retained GDI/`UpdateLayeredWindow` renderer. The
  compatibility choice is also the explicit lower-RAM mode, with the same UI
  features, and hidden windows release their GPU and CPU presentation surfaces.
- Device-pixel SVG lamp rendering for sharp steady states, with an embedded
  512 px APNG used only while the lamp transition is moving.
- Individually rendered 16–256 px ICO packs for application, lamp state, and
  dialog artwork, including caption-optimized 16/20/24/28 px variants and
  alpha-correct Lanczos resizing for every remaining raster size.
- One canonical runtime asset bundle shared by native and Web surfaces: the
  embedded dashboard and static exporter reuse the same SFX and 512 px product
  artwork, while the product ICO exists only as the executable's PE resource.
- Notification-area menu, close-to-tray, always-on-top, startup, and magnetic
  monitor/work-area snapping.
- Macro-style native and web hotkey recorders for toggle, on, off, and show,
  with live keycaps, conflict/reserved-key feedback, safe capture suspension,
  and Windows registration preflight.
- Windows toast notifications with actionable buttons and legacy tray-balloon
  fallback, plus one shared PCM-WAV sound pipeline. The bundled on/off/message/
  error effects or one imported custom WAV play consistently from the native
  GUI, Web UI, TUI, and GUI-free host, independently of visual notifications.
- Bounded WebSocket reconnect with jitter, HTTP polling fallback, and
  failure-aware `.local` address caching.
- A single self-contained `RayanLamp.exe` for GUI, JSON/VT100 command-line,
  interactive terminal, GUI-free web-dashboard serving, standalone headless,
  and COM-server operation.
- Full-screen Charm terminal UI built with Bubble Tea, Lip Gloss, Bubbles,
  and Huh: responsive keyboard/mouse control, live activity, a themed
  allow-listed diagnostics prompt/viewer, a shared Telnet-style device
  terminal, and compact help. It can attach to the GUI through local IPC/API
  or own the lamp connection in standalone mode.
- Bundled responsive SPA/PWA at `/app/`, built with Preact and the same
  canonical design tokens as the native surfaces. It provides live state,
  controls, complete settings, parsed diagnostics, multi-tab synchronization,
  an xterm-powered device terminal with WebGL acceleration and an automatic
  canvas fallback, system light/dark mode, and synchronized English/Persian
  localization with proper RTL layout, Persian digits, and the embedded
  YekanBakh variable font,
  relay/direct/automatic transports, and an offline explanatory shell without
  exposing the long-lived API secret to JavaScript.
- Deterministic frontend-only ZIP export. Its direct-first build controls the
  firmware through CORS-safe `/lamp/get`, `/lamp/on`, `/lamp/off`, and `/ws`;
  the manager-served build can switch between the Go coordinator and the same
  direct client without changing UI code.
- One command dispatcher shared by UI actions, CLI, named pipe, WM_COPYDATA,
  loopback HTTP, and optional per-user COM Automation.
- Optional amd64/386 native SDK DLL and public Go client for external hosts;
  ordinary calls attach to the authoritative primary, while process bootstrap
  is a separate explicit API.
- Opt-in `rayan-lamp://` activation and native Jump List tasks under one
  stable Windows AppUserModelID, with portable startup remaining registry-free.
- Live taskbar thumbnail controls and connection/operation progress, plus
  lock/unlock, suspend/resume, and network-change recovery with separately
  configurable automation scripts.
- Six strict, public GET-only diagnostics plus two internal Wi-Fi
  projection sources used internally by the Go and direct-web clients.
  Unrelated ASA PU/debug routes are rejected, and
  credential-bearing `/config/ap` and `/config/st` responses are redacted
  before parsing and never exposed as raw diagnostics.
- A modeless, single-instance live-preview settings window plus diagnostics
  dialog, with rounded native frames, app icons, composition-safe Fluent
  material, themed controls, and explicitly readable dark/light tab states.
- Event automation with timeouts, a bounded queue, bounded captured output,
  and an explicit environment contract.

## Requirements

Release builds target Windows 10 or later and Windows Server 2016 or later,
which matches the supported Windows baseline for Go 1.21 and newer. Windows 11
receives the newest system backdrop; older supported builds gracefully fall
back through acrylic, blur, and solid rendering. High Contrast and disabled
transparency preferences suppress decorative effects.

Building requires:

- Go 1.25.8 or the version declared by `go.mod`;
- Node.js 22.22.2+ within 22.x, 24.15+ within 24.x, or 26+, and npm 10+ to reproduce the committed web
  distribution;
- PowerShell 5.1 or newer;
- network access on the first release build so the pinned
  `goversioninfo` tool can be downloaded.

The ordinary single-executable release is built with CGO disabled. Its
accelerated renderer dynamically uses the D3D11, DXGI, and DirectComposition
components supplied by supported Windows versions, so it needs no DirectX
redistributable, C compiler, .NET runtime, administrator access, or
machine-wide registration. Only the separately requested native SDK DLL build
uses a validated MinGW-w64 toolchain.

Rendering never snapshots or reads back the desktop. DWM supplies live material
behind the app, and RayanLamp presents only its own premultiplied-alpha pixels.
Fully transparent mode automatically retains the per-pixel Windows presenter,
even when DirectX was requested, because that is the path where alpha-zero
pixels have exact native pointer click-through semantics.

The native accelerated path keeps the proven SVG/APNG and text rasterizer, then
uses a persistent D3D11 flip-model swap chain plus DirectComposition for GPU
upload, buffering, opacity, synchronized presentation, and DWM composition.
The browser terminal separately prefers WebGL and automatically falls back to
canvas if its graphics context is lost.

## Build and install

The checked-in [build-edition.txt](build-edition.txt) is currently
`develop`. Development builds keep optimized Go code while retaining native
symbols/DWARF plus readable browser JavaScript and source maps. Production
builds keep every product feature, but strip those debugger payloads and
minify the embedded Web UI. Either edition can be selected without changing
the canonical default:

```powershell
.\tools\package.ps1 -Edition develop
.\tools\package.ps1 -Edition production
```

From a PowerShell prompt in the repository:

```powershell
.\tools\package.ps1
```

From a Linux host, the pure-Go cross-build needs no Wine or C toolchain:

```bash
make windows-cross FORCE=1
make windows-cross-production FORCE=1
```

Use `ARCH=386` or `ARCH=arm64` for the other supported Windows targets. The
Linux pipeline rebuilds the matching embedded SPA/PWA and validates the
resulting edition, debug-symbol policy, machine type, console subsystem, icon,
version resource, manifest, and embedded Web assets before publishing the same
single executable.

This runs edition-tagged `go vet` and tests, embeds architecture-correct icon/manifest/version
resources, type-checks/tests/builds the bundled web client, builds one
console-subsystem `RayanLamp.exe`, writes
`dist\RayanLamp.exe`, and reports its SHA-256 without producing sidecars or a
ZIP. The same executable contains the native GUI, CLI, and TUI modes; the
console subsystem makes an invoking shell wait for its real exit code. On
Windows 11 24H2, Windows Server 2025, and later releases, the detached-console
manifest policy prevents automatic allocation outside an existing console
session. On older supported Windows releases, startup detaches only a console
it exclusively owns and retains inherited/shared consoles; those older
releases may still present a brief pre-main flash. To replace an existing
output:

```powershell
.\tools\package.ps1 -Force
```

A plain GUI launch from an interactive shell intentionally keeps that shell
waiting for the primary process. Use `start "" RayanLamp.exe` from `cmd.exe`
when an asynchronous GUI launch is desired; explicit `cli`, `tui`, `serve`,
and one-shot handoff commands remain synchronous.

When producing the optional static dashboard ZIP as well, run the package
command first and then `.\tools\export-web.ps1`; the single-executable packager
deliberately requires `dist` to contain only `RayanLamp.exe`.

The unified executable can install itself for the current user. File
installation is the only default action; every persistent integration remains
an explicit opt-in:

```powershell
.\dist\RayanLamp.exe /install /with-startup /with-shell /with-com /launch
.\dist\RayanLamp.exe /install-status /json
```

The destination is exactly
`%LOCALAPPDATA%\Programs\RayanLamp`. Startup, URI/Jump List integration, and
COM registration are independent optional per-user operations. Portable launch
writes none of those registrations. It may read the exact Run command to keep
the displayed startup setting synchronized; foreign commands are preserved,
and registry changes require an explicit saved setting or install flag.
Use `/install /force` for a staged, SHA-256-verified in-place update. Remove the
installation and all exact-owned integrations with `RayanLamp.exe /uninstall`;
configuration and logs are preserved unless `/remove-user-data` is explicitly
added. The repository PowerShell helpers remain available for build/release
automation, but they are not required by the installed app. See
[Building and packaging](docs/building.md) for architecture options,
reproducibility details, and uninstall instructions.

## Use

Launch `RayanLamp.exe`; the default device URL is
`http://rayanlamp.local`. Click the lamp, use the tray menu, or press
`Ctrl+Alt+L` to toggle it. When magnetic edge snap is enabled, hold Ctrl while
dragging to bypass it temporarily and release Ctrl to resume snapping.

The private browser dashboard is served from
`http://127.0.0.1:47831/app/` while the local API is enabled. It establishes a
short-lived HttpOnly browser session; REST mutations also require an
in-memory CSRF token. The service worker caches only the application shell and
artwork, never API responses or diagnostic data.

The dashboard includes a relative, standalone Web Manifest and mobile/Apple
application metadata, so compatible browsers can install it or add it to the
home screen when it is served from an installable secure origin such as
localhost. Mobile layouts honor display cutouts and on-screen keyboards,
retain pinch zoom, and suppress the browser tap-highlight flash. When the
foreground browser exposes the Vibration API, short haptics acknowledge
explicit lamp commands and normalized message/audio events. Unsupported
browsers, hidden pages, reduced-motion users, startup, and background polling
remain silent.

Language is shared through `ui.locale`: `auto` follows Windows, while `en` and
`fa` select English or Persian explicitly. A change applies immediately to the
native GUI, modeless dialogs, tray/taskbar/notifications, TUI, and every open
dashboard tab. Persian presentation uses RTL geometry and Persian digits, but
protocol values such as URLs, IP addresses, ports, hashes, hotkeys, endpoint
names, command IDs, and raw terminal/diagnostic output remain exact LTR text.

To run the embedded dashboard as the primary controller without creating a
native window, tray icon, global hotkeys, or native dialogs:

```powershell
.\RayanLamp.exe serve
.\RayanLamp.exe serve --no-open
.\RayanLamp.exe serve --listen localhost:49123
```

`serve` explicitly enables the loopback API in the selected configuration,
claims the normal per-user named pipe, owns the lamp HTTP/WebSocket connection,
and keeps automation scripts and audible cues active. By default it opens the
dashboard in the default browser; `--no-open` leaves it running until Ctrl+C or
an IPC `quit`. A second ordinary launch opens the existing dashboard instead
of constructing a competing GUI. Use `serve --help` for dedicated options.

### Frontend-only static export

Build the reviewed direct-first SPA/PWA as a deterministic static archive:

```powershell
.\tools\export-web.ps1
```

The output is `dist\RayanLamp-Web-Static.zip`. Extract the complete archive
and serve that directory over HTTP; do not double-click `index.html` through
`file://`. The default target is `http://rayanlamp.local`, and the Connection
control can select another HTTP/HTTPS lamp origin.

The same frontend has three connection choices:

- **Automatic** uses the same-origin Go coordinator when present, then falls
  back to the configured lamp.
- **Go coordinator** uses the authenticated manager for native settings,
  automation, diagnostics, notifications, IPC coordination, and one shared
  lamp connection.
- **Direct to lamp** uses only the firmware HTTP/WebSocket endpoints and
  browser-local settings.

The standalone ZIP defaults to Direct because a plain static server has no Go
session endpoint. It can use coordinator mode when hosted behind the same
origin that forwards `/app/session` plus the versionless coordinator routes
(`/status`, `/lamp/*`, `/ui/*`, `/events`, and the other documented API paths)
to RayanLamp; the embedded
`/app/` copy already provides this arrangement. An HTTPS page cannot contact
an HTTP/WS-only lamp due to browser mixed-content rules. For the default lamp,
serve locally at an HTTP `localhost`/`127.0.0.1` URL.

Useful console commands:

```powershell
.\RayanLamp.exe cli --status
.\RayanLamp.exe cli --status --json
.\RayanLamp.exe cli --on
.\RayanLamp.exe cli --off
.\RayanLamp.exe cli --toggle
.\RayanLamp.exe cli --hide
.\RayanLamp.exe cli --reconnect
.\RayanLamp.exe cli --notify "Lamp maintenance starts soon" --title "Office"
.\RayanLamp.exe cli --audio --cue message --count 2
.\RayanLamp.exe cli --endpoint /info --json
```

The primary app must be running for the controller-only `cli` mode. Starting
`RayanLamp.exe /on`, `/off`, `/toggle`, `/show`, `/hide`, or `/settings`
retains the original handoff behavior and sends the request to the primary
instance instead of opening another one.

For an interactive terminal dashboard:

```powershell
.\RayanLamp.exe tui
.\RayanLamp.exe tui --mode ipc
.\RayanLamp.exe tui --mode api
.\RayanLamp.exe tui --mode standalone
.\RayanLamp.exe tui commands
.\RayanLamp.exe tui settings
.\RayanLamp.exe tui device
.\RayanLamp.exe tui serving
.\RayanLamp.exe tui diagnostics
.\RayanLamp.exe tui --terminal
.\RayanLamp.exe tui --telnet
```

The optional screen operand opens a dedicated TUI workspace directly.
`configure` aliases `settings`, `serve` aliases `serving`, and `telnet` aliases
`terminal`. The equivalent selectors are `--screen NAME`, `--commands`,
`--settings`/`--configure`, `--device-settings`, `--serving`,
`--diagnostics`, and `--terminal`/`--telnet`.

The default `auto` mode tries the current-user IPC pipe, then the authenticated
loopback API, then becomes the headless primary. Explicit `ipc` and `api` modes
fail clearly instead of silently changing transport; `standalone` never starts
the GUI, but it does own the local API/Web listener whenever configuration
enables it. Serving changes start, stop, or rebind that listener gracefully.
It owns the normal per-user pipe while running, so
`RayanLamp.exe cli` and other clients can attach without creating a competing
lamp connection. Press `:` for the searchable command center, `s` for the
complete TUI-native application settings, `v` for sanitized device parameters,
and `w` for serving settings. `Ctrl+S` validates, atomically persists, and
applies the active settings section. Press `?` for all keys and `d` for the
read-only raw diagnostics prompt. The command center includes separate
notification and audible-cue tests, so audio remains usable even when toast
delivery is disabled.
`--telnet` is an alias for `--terminal`. Press `x` to open the shared device
terminal from an already-running TUI. It connects manually to the
configured lamp hostname and `telnet_port` (23 by default), so normal startup
never occupies the firmware's single terminal-client slot. `Ctrl+]` or `Esc`
detaches from the panel without closing the shared session.

The audited firmware speaks a Telnet-style raw character stream with ANSI
output, but the checked firmware source currently enables `ShellOnSerial`.
That build reports “Shell is on Serial” and ignores network input; RayanLamp
detects the banner and labels the session read-only instead of pretending a
command succeeded. The Device Settings workspace still displays typed,
sanitized `/config/ap` and `/config/st` projections, but never exposes their
password fields or invents HTTP write behavior. Interactive parameter changes
require firmware that deliberately
enables and secures its network shell. Telnet is plaintext and unauthenticated
in the audited firmware, so use it only on a trusted local network. The Web
dashboard exposes the same shared session under **Terminal**; its Enter key is
translated to the firmware-compatible CRLF line ending.

Configuration lives at `%APPDATA%\RayanLamp\config.json` by default. An
explicit `--config PATH` takes precedence, followed by the
`RAYANLAMP_CONFIG` environment variable. The local API token is a secret;
do not paste it into logs, issue reports, or scripts that other users can read.

## Development firmware mock

Run the local firmware double:

```powershell
go run ./tools/mock-lamp `
  -listen 127.0.0.1:8080 `
  -telnet-listen 127.0.0.1:2323
```

Set the device URL to `http://127.0.0.1:8080`, then inject state, message, and
beep events through `/mock/event`. Set `lamp.telnet_port` to `2323` to test the
TUI/Web device terminal; the mock terminal is otherwise disabled. See the
[mock server guide](tools/mock-lamp/README.md).

## Documentation

- [Recovered development history and its limits](docs/history/README.md)
- [Repository verification and publishing](docs/publishing.md)

- [Architecture](docs/architecture.md)
- [Visual regression contract](docs/visual-regression-contract.md)
- [Configuration reference](docs/configuration.md)
- [Commands and integrations](docs/integration.md)
- [Firmware protocol](docs/firmware-protocol.md)
- [Building, assets, packaging, and installation](docs/building.md)
- [Memory profile and lower-RAM modes](docs/MEMORY.md)
- [Attribution and third-party notices](docs/attribution.md)

## Security model

RayanLamp treats both firmware messages and local integrations as untrusted
input. Network bodies and IPC packets are bounded, WebSocket messages are
validated, redirects are rejected, `.local` traffic bypasses proxies, the
named pipe is ACL-bound to the current user and SYSTEM, and the HTTP API is
loopback-only with constant-time bearer-token verification. Diagnostics cannot
turn their JSON endpoint arrays into new permissions. The two fixed Wi-Fi
configuration sources return firmware password fields; those values are
discarded before parsing and cannot reach cache snapshots, IPC/API responses,
retained or broadcast direct-web state, or an exact-raw diagnostics action.

Automation scripts are intentionally powerful and execute as the signed-in
user. Enable only scripts you own and have reviewed.

## License

RayanLamp source and original artwork are released under the
[MIT License](LICENSE). Dependency copyrights and licenses remain with their
respective authors; see [Attribution](docs/attribution.md).
