# Configuration reference

RayanLamp loads strict JSON from:

```text
%APPDATA%\RayanLamp\config.json
```

Path precedence is:

1. `--config PATH`;
2. `RAYANLAMP_CONFIG`;
3. the default path above.

Environment variables in an explicit path are expanded, and relative explicit
paths become absolute. A missing file loads defaults. Existing files are
limited to 1 MiB, reject unknown fields and trailing JSON, and are overlaid on
defaults so omitted members retain their default values.

The modeless settings window is the recommended editor: the lamp window stays
interactive, appearance choices preview immediately, Cancel restores the
previous runtime presentation, and Save completes asynchronously. Saves
normalize the complete configuration, write a same-directory temporary file,
flush it, atomically replace the destination, and apply a current-user-only
ACL.

## Complete default shape

`local_api_token` below is descriptive: each new default configuration receives
a fresh, cryptographically random 256-bit URL-safe token.

```json
{
  "lamp": {
    "base_url": "http://rayanlamp.local",
    "request_timeout": "5s",
    "websocket": true,
    "reconnect_min": "1s",
    "reconnect_max": "30s",
    "http_poll": "30s",
    "telnet_port": 23
  },
  "ui": {
    "view_mode": "compact",
    "theme": "system",
    "locale": "auto",
    "backdrop": "acrylic",
    "render_backend": "auto",
    "rounded_corners": true,
    "layered_foreground": true,
    "title_bar_mode": "none",
    "border_mode": "auto",
    "settings_tab": 0,
    "settings_bounds": { "x": -1, "y": -1, "width": 780, "height": 640 },
    "show_title_bar": false,
    "always_on_top": false,
    "close_to_tray": true,
    "prewarm_tray_menu": true,
    "start_minimized": false,
    "window_visible": true,
    "window_state": "normal",
    "show_taskbar": true,
    "animations": true,
    "magnetic_snap": false,
    "magnetic_snap_distance": 16,
    "window_bounds": {
      "x": -1,
      "y": -1,
      "width": 176,
      "height": 176
    }
  },
  "notifications": {
    "enabled": true,
    "state_changes": true,
    "initial_sync": false,
    "custom_message": true,
    "sound": true,
    "sound_source": "bundled",
    "sound_file": "",
    "sound_name": "",
    "delivery": "auto"
  },
  "hotkeys": {
    "toggle": "Ctrl+Alt+L",
    "on": "Ctrl+Alt+Shift+O",
    "off": "Ctrl+Alt+Shift+F",
    "show": "Ctrl+Alt+Shift+L"
  },
  "automation": {
    "enabled": false,
    "on_script": "",
    "off_script": "",
    "message_script": "",
    "lock_script": "",
    "unlock_script": "",
    "suspend_script": "",
    "resume_script": "",
    "network_change_script": "",
    "working_directory": "",
    "timeout": "15s",
    "capture_output": true
  },
  "integration": {
    "start_with_windows": false,
    "local_api_enabled": true,
    "local_api_listen": "127.0.0.1:47831",
    "local_api_token": "<fresh random 256-bit URL-safe secret>"
  },
  "diagnostics": {
    "safe_endpoints": [
      "/info",
      "/update/info",
      "/wifi/signal",
      "/wifi/status",
      "/wifi/country",
      "/description.xml"
    ],
    "expert_enabled": false,
    "expert_endpoints": []
  }
}
```

Durations use Go duration strings such as `"750ms"`, `"5s"`, or `"1m30s"`.

## Lamp

| Key | Meaning and validation |
| --- | --- |
| `base_url` | HTTP/HTTPS root URL. Host required; credentials, query, fragment, and path prefix forbidden. |
| `request_timeout` | HTTP/diagnostic timeout from 100 ms through 5 minutes. |
| `websocket` | Continuously connect to `/ws` when true. |
| `reconnect_min` | Minimum retry delay, 100 ms through 5 minutes. |
| `reconnect_max` | At least `reconnect_min`, no more than 15 minutes. |
| `http_poll` | `"0s"` disables polling; otherwise 1 second through 24 hours. |
| `telnet_port` | TCP port used only when the TUI or Web UI manually opens the device terminal; 1 through 65535, default 23. |

Changing lamp networking settings creates a new client generation, cancels the
old listener/poller, disconnects an open device terminal, and reconnects with
the new values. The terminal never auto-connects at process startup.

## UI

| Key | Values |
| --- | --- |
| `view_mode` | `compact` for the lamp-only surface or `dashboard` for the larger control view. |
| `theme` | `system` follows the live Windows app theme; `light` and `dark` are explicit app-wide overrides. |
| `locale` | `auto` follows the Windows language; `en` selects English; `fa` selects Persian with RTL layout and Persian presentation digits. |
| `backdrop` | `auto`, `solid`, `transparent`, `mica`, `tabbed`, `acrylic`, `blur`. Legacy `none`, `off`, and `disabled` values normalize to `solid`. |
| `render_backend` | `auto` prefers the hardware DirectX path and falls back safely; `directx` explicitly requests it; `legacy` keeps the same UI features on the lower-private-memory GDI/`UpdateLayeredWindow` compatibility path. See the [measured memory profile](MEMORY.md). |
| `rounded_corners` | Uses native DWM corners, with a rounded-region fallback on older Windows. |
| `layered_foreground` | Enables the alpha-composited Fluent renderer, sharp SVG/APNG foreground, and glass controls. Disabling it selects compatible flat controls. |
| `title_bar_mode` | `none`, `custom`, or `native`. Custom uses RayanLamp's composited controls; native restores the Windows caption. |
| `border_mode` | `auto` keeps the native resize frame; `none` removes the outer frame and edge-resize affordance. A native title bar requires `auto`. |
| `settings_tab` | Zero-based last selected settings page, restored on the next open. Out-of-range values reset to zero. |
| `settings_bounds` | Settings position in virtual-screen pixels and size in 96-DPI logical units. Default 780×640; minimum 520×340; maximum 4096×4096, further constrained to the current monitor work area. Both X/Y = -1 centers the first open; closing remembers resized bounds, even on Cancel. |
| `show_title_bar` | Legacy compatibility mirror. It is true only when `title_bar_mode` is `custom`; new integrations should set `title_bar_mode`. |
| `always_on_top` | Applies the Windows topmost extended style. |
| `close_to_tray` | Closing hides the window while leaving the tray process active. |
| `prewarm_tray_menu` | Prepares the notification-area menu invisibly and reuses its native window between openings to avoid first-frame flashes. Disable it to recreate the menu each time. |
| `start_minimized` | Always starts without showing the main window. This explicit preference overrides remembered `window_visible` state without changing it. |
| `window_visible` | Runtime session state remembered when the main window is shown or hidden. Existing configurations default to `true`. |
| `window_state` | Runtime session state: `normal`, `minimized`, or `maximized`. Existing configurations default to `normal`. |
| `show_taskbar` | Controls taskbar visibility for the compact window. |
| `animations` | Enables APNG, window, control, dialog, and menu motion. Windows reduced-motion and High Contrast policy can still disable motion. |
| `magnetic_snap` | Snaps the moving rectangle to monitor work-area edges. Hold Ctrl while dragging to bypass it temporarily; release Ctrl to resume immediately. |
| `magnetic_snap_distance` | 1 through 128 device-independent pixels. |
| `window_bounds` | Compact: 128–640 px per side. Dashboard: 340–900 px wide and 520–1200 px high. X/Y: -100000–100000. The compact default remains 176×176. |

`render_backend` is a requested policy, not a promise that a graphics device is
available. `auto` probes the complete hardware D3D11, DXGI, and
DirectComposition presentation stack. `directx` makes the same accelerated
path explicit. Either selection automatically uses `legacy` if initialization
or presentation fails, including graphics-device removal or reset; `legacy`
skips the hardware probe. The DirectX components are loaded dynamically from
Windows by pure Go code and do not require CGO or an external redistributable.

`x: -1` or `y: -1` requests platform-default centering. `solid` paints the
compatible effect-free surface, while `transparent` explicitly requests no
backdrop. Every fully transparent, alpha-zero pixel is excluded from the
interactive window region so pointer input passes to the window beneath it;
that mode automatically uses the per-pixel legacy presenter because a
DirectComposition visual's alpha is not an HWND hit-test mask. Other requested
backdrops are best effort; capability, High Contrast, and the Windows
transparency setting can select a safer fallback. Neither renderer snapshots
nor reads back the desktop: DWM supplies live backdrop material and RayanLamp
presents only its own pixels.

The default `system` theme is live: the main window, native caption, shared
tray/main menu, open Settings and Diagnostics windows, embedded dashboard, and
browser chrome update when Windows changes between light and dark. Explicit
`light` or `dark` is the user customization override and is synchronized
through the normal configuration IPC/API and browser BroadcastChannel paths.
High Contrast always takes precedence over an explicit app palette, uses
Windows system colors, disables decorative transparency and motion, and routes
the custom popup menu through its native accessible fallback. Windows-owned
toasts and balloons intentionally continue to follow Windows itself.

`locale` is a presentation preference shared by the native shell, modeless
dialogs, tray/taskbar text, toast actions, TUI, coordinator Web UI, and
frontend-only Web UI. Changing it previews immediately and is broadcast to
open browser tabs. Persian uses the canonical embedded YekanBakh variable font,
RTL geometry, Persian digits, and Persian calendar formatting where a human
date is shown. URLs, IPs, ports, durations used as configuration values,
hotkeys, paths, endpoint and command names, hashes, and raw device/terminal
content never have their characters or ordering changed.

On Win32 the embedded VF is selected through its Regular/Bold named instances,
including native settings/diagnostics fields, headings, tab labels and custom
popup text. Language changes apply these fonts immediately. Windows-owned
caption bars, file pickers, notification surfaces and TaskDialogs continue to
use Windows' system typography.

`window_visible`, `window_state`, and `window_bounds` form the remembered
native-window session. Bounds always describe the normal restore rectangle, so
minimizing or maximizing does not replace them with an iconic or monitor-sized
rectangle. Hiding retains the last normal/minimized/maximized state, allowing a
later explicit show to restore predictably. `start_minimized` and the
`--start-minimized`/`--background` startup switches are policy overrides: they
start in the notification area but do not erase the remembered session.

`title_bar_mode` and `border_mode` are independent for captionless windows:
`none` or `custom` title bars can use either a resizable automatic frame or a
completely frameless border. The native Windows caption inherently owns a
non-client frame, so `title_bar_mode: "native"` with `border_mode: "none"` is
rejected rather than silently displaying a border.

## Notifications

`delivery` is `auto`, `toast`, or `balloon`. Auto attempts a modern toast and
uses a native notification-area balloon if toast setup or delivery fails.

- `state_changes` shows confirmed state transitions.
- `initial_sync` includes the first synchronized state; it is off by default to
  avoid a notification on every launch.
- `custom_message` allows validated `message:` and `msg:` firmware frames.
- `sound` plays state/message cues independently of whether visual toast or
  balloon delivery is enabled.

| Sound key | Meaning and validation |
| --- | --- |
| `sound_source` | `bundled` selects the four built-in on/off/message/error effects. `custom` maps the selected custom WAV to every semantic cue. |
| `sound_file` | Empty for bundled audio. Custom audio requires the exact lowercase 64-character SHA-256 asset ID. It is an identity, not a filesystem path. |
| `sound_name` | Optional display name, with no control characters and at most 255 Unicode characters. It is never used to locate the file. |

Native Settings and coordinator-backed Web Settings validate and import a
custom WAV into `sfx\<sha256>.wav` beside the active configuration
(`%APPDATA%\RayanLamp\sfx\…` by default), then atomically select its ID.
Re-importing identical bytes reuses the existing file. The import accepts
uncompressed integer PCM in RIFF/WAVE, mono or stereo, 8–96 kHz,
8/16/24/32-bit, no more than 4 MiB or 10 seconds. A standalone/direct Web UI
uses the same validation and SHA-256 identity in browser IndexedDB because no
Go filesystem owner exists. If that browser no longer holds the selected
asset, it repairs the setting to bundled audio, advances the configuration
revision, and synchronizes other tabs before playback. Re-importing the WAV
restores the custom choice.

If a configured custom asset was removed or is no longer a valid WAV, startup
logs the problem and uses the complete bundled catalog instead of failing or
going silently audio-less. Live and startup preflight also repair the effective
and persisted `sound_source` to `bundled`, so public configuration never claims
that an unavailable custom asset is active.

The TUI exposes all sound fields and can import a local WAV path directly
through its active GUI/headless primary; the returned content ID and display
name are selected in the same Apply workflow. Native GUI, attached TUI,
standalone TUI,
`serve`, and other GUI-free primaries all resolve and play the same selected
asset. The master `enabled` value gates visual notifications only. `sound`
gates incidental state/message effects, while an explicit `audio.play` command
or validated firmware `beep:` remains audible.

## Hotkeys

The native Settings window and web dashboard use shortcut recorders rather than
free-form text fields. Select the keycap surface or **Record**, then press the
complete chord. Held modifiers appear as live keycaps and the first supported
non-modifier key completes the recording. **Record** changes to **Cancel** while
listening; selecting it again or pressing `Escape` restores the previous
binding. Unmodified `Backspace` or `Delete`, or the separate **Clear** action,
removes the binding and disables that action's global shortcut. Modifier-only
input never commits an incomplete chord, and left/right modifier keys are
normalized to `Ctrl`, `Alt`, `Shift`, and `Win`.

Duplicate bindings are rejected with inline conflict feedback. Unsupported keys
also leave the recorder active so another chord can be tried. `F12` is reported
inline as unavailable because Windows reserves it for the debugger. While a
recorder is listening, RayanLamp temporarily unregisters its current global
shortcuts so the chord being learned cannot toggle the lamp. Native recording
also suppresses ordinary shell accelerators such as `Win+E` and `Alt+Tab`;
Windows' secure `Ctrl+Alt+Delete` sequence cannot be intercepted by an
application. The configured shortcuts are restored when recording completes
or is cancelled.

Completed and cleared bindings update the live settings preview immediately.
The web dashboard sends the change through the immediate settings channel, and
its existing WebSocket/BroadcastChannel synchronization keeps other app clients
and browser tabs current. Saving the native Settings window persists its
previewed values.

Canonical values still use `+`-separated modifiers and a key, for example
`Ctrl+Alt+L`, `Win+Shift+F3`, or `Ctrl+Alt+Num1`. Blank disables a binding.
Supported keys include:

- letters, top-row digits, and `F1` through `F24`;
- navigation and editing keys, including arrows, Home/End, Page Up/Down,
  Insert, Tab, Enter, Space, Pause, Caps Lock, and Print Screen;
- numpad digits, operators, separator, decimal, Num Lock, and Scroll Lock;
- browser, volume, media transport, mail/media-launch, and application-launch
  keys exposed by the keyboard;
- the common punctuation keys, including semicolon, equals, comma, minus,
  period, slash, backtick, brackets, backslash, and quote; and
- the Apps/context-menu and Sleep keys.

Bindings are canonicalized against the Windows virtual-key set and must not
duplicate one another. A completed native or browser-recorded chord is
preflighted against Windows while the previous working set remains safely
suspended. An unavailable chord stays inline as an error and is not allowed to
replace that working set. A different application can still win the inherently
racy interval after preflight; that rare registration failure is logged and the
other controls remain available.

## Automation

Automation supports `.ps1`, `.cmd`, `.bat`, `.exe`, and `.com`. Paths and the
working directory expand environment variables. Timeout must be 1 second
through 10 minutes. Jobs run serially through a 32-entry bounded queue;
near-duplicate events within 750 ms are suppressed.

`on_script`, `off_script`, and `message_script` react to device events.
`lock_script`, `unlock_script`, `suspend_script`, `resume_script`, and
`network_change_script` react to the corresponding Windows session, power, and
network-interface events. Resume and network recovery is coalesced before the
lamp reconnects, while every configured lifecycle script receives the original
event name through `RAYANLAMP_EVENT`.

PowerShell scripts run with:

```text
powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File <path>
```

Command scripts use `%ComSpec% /d /s /c`; executables are launched directly.
Every process is hidden, inherits the signed-in user's environment and
privileges, and is terminated when its context reaches the configured timeout.
Captured combined stdout/stderr is limited to 64 KiB and written to the
application log.

See [Automation security and environment](integration.md#automation).

## Integration

`start_with_windows` controls the current user's
`HKCU\Software\Microsoft\Windows\CurrentVersion\Run\RayanLamp` value. Loading
the GUI, standalone TUI, or GUI-free `serve` host only reads that value; it
never creates, rewrites, or deletes it. The primary reconciles the
displayed/configured checkbox with exact current-executable ownership after it
wins single-instance IPC, so an installer-created entry is shown correctly.
Registry persistence occurs only after an explicit saved change or the
installer's `-StartWithWindows` flag.
Enabling refuses to replace a foreign command, disabling removes only the exact
quoted current-executable command, and neither path writes HKLM.

The unified executable also exposes explicit self-service lifecycle commands:
`/install`, `/install-status`, and `/uninstall`. Self-install defaults to the
program file only; `/with-startup`, `/with-shell`, and `/with-com` must be
requested independently. Self-uninstall removes exact-owned integration state
and program files while preserving this configuration unless
`/remove-user-data` is confirmed.

The local API listen address must include a numeric port from 1 through 65535
and an explicit loopback host: `127.0.0.1`, `[::1]`, another loopback IP, or
`localhost`. Wildcard and LAN addresses are rejected.

`RayanLamp.exe serve` is an explicit request to make this API and `/app/`
dashboard the primary presentation surface. It enables `local_api_enabled` in
the selected configuration. `serve --listen HOST:PORT` validates and persists
the same `local_api_listen` field; it is not an unreported transient override.
Later disabling the API stops only the headless HTTP listener—the lamp
coordinator and named-pipe IPC remain alive until Ctrl+C or `quit`.

The token must contain 32 through 256 non-whitespace, non-control characters.
Blank causes secure regeneration during normalization. Treat it like a
password: processes running as the same user may already be able to read user
files or send named-pipe commands, but other accounts should not receive it.

## Diagnostics

`safe_endpoints` must be omitted/empty or exactly match the compiled canonical
lamp/ESP set. It is not user-extensible. The legacy `expert_enabled` and
`expert_endpoints` fields remain only so exact older configurations can migrate:
normalization always writes `false` and an empty array. The unrelated
`/datetime/get`, `/pu`, `/is_exec`, `/page_id`, `/eProcessState`, and
`/eResponseStatus` routes are never permissions. Every request remains
exact-path, query-free, and GET-only; unsafe configuration/update/login/crash
endpoints cannot be added.

`/config/ap` and `/config/st` are deliberately absent from `safe_endpoints`.
The coordinator and direct browser client may poll those two exact GET paths
internally to project SSID and network state, but callers cannot request their
raw responses through CLI, TUI, IPC, COM, either HTTP diagnostics API, or the
web raw-response action. The firmware includes Wi-Fi passwords in those
responses, so credential values are immediately redacted and never enter
diagnostic cache objects, broadcasts, or serialized output.

See [Firmware protocol](firmware-protocol.md#read-only-diagnostics).
