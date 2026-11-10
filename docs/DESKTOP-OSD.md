# Windows desktop OSD

PCController can present a transient overlay directly above the desktop, with
the main GUI closed. The existing host is the presentation owner; no board,
serial connection, browser or second GUI is required. Windows session 0 and
secure/UAC desktops are not supported, and exclusive full-screen applications
may cover ordinary desktop windows.

## Appearance and interaction

- PCController's native charcoal/violet or light palette, semantic event icons,
  antialiased rounded edges, DPI-sized system text and original-aspect artwork.
- Typography uses a true Segoe UI Semibold (600) face for the 16-DIP heading,
  Regular (400) at 14 DIP for body/actions, and Regular for the close glyph.
  On Windows x64, DirectWrite supplies fractional-size layout, shaping and font
  fallback; Direct2D renders actual coverage into a premultiplied-alpha surface.
  The primary-family weight is measured, not assumed from the request. Missing
  Semibold falls back to Regular rather than silently substituting Bold.
  Transparent compositing deliberately uses grayscale antialiasing, not colored
  ClearType subpixels. This is native DirectWrite rasterization, not a GDI mask
  or a supersampled image scaled back down.
  The existing GDI path remains an automatic whole-card fallback for unsupported
  APIs/architectures, target loss or text exceeding Direct2D surface limits.
  `text_renderer` in OSD status reports `directwrite` or `gdi` for that presentation.
- A short cubic fade/slide at entrance and dismissal. Long headings/body text
  scroll at a fixed speed with pauses at both ends, independently of frame rate.
- Clicking ×, choosing an action and API dismissal share the same short downward
  fade, starting at the current opacity/position even during entrance. Repeated
  dismissal does not restart it. Fading control targets absorb duplicate clicks
  until they disappear, without taking focus; passive content remains click-through.
  Reduced-motion mode remains immediate.
- Button and close-target hover fades over 120 ms, reversing from its current
  tint if the pointer moves quickly. Only the input targets track pointer leave;
  passive visual content never competes for hover. Surface opacity remains stable.
- Body text preserves up to six explicit LF/CRLF lines, with height and action
  placement adapting to them. Each long line scrolls independently; short lines
  remain still. Oversized messages fail rather than covering off-screen controls.
- Top/bottom left, center or right, plus screen-center placement within the
  primary display work area. Default: bottom-center.
- Up to five compact action buttons. Their URIs are inert until an explicit
  click; only `pccontroller:`, `http:` and `https:` routes are admitted. Script
  choices instead supply an `id` and omit `uri`, so nothing is launched. No
  arbitrary executable, remote icon download or script evaluation is accepted.
- The overlay does not take foreground focus or appear in the taskbar. Passive
  text/background lets desktop clicks through; the close/action targets remain
  interactive. Buttons are pointer-operated; keyboard focus remains in the
  foreground application.
  The visual HWND is always `WS_EX_TRANSPARENT`; small owned, non-activating
  input surfaces cover only the buttons and close target. This works across
  applications/threads, unlike relying on `HTTRANSPARENT` alone.
- Windows' client-area animation preference and the host's reduced-motion
  preference disable automatic movement. Static overlays sleep until expiry;
  fonts, icons and DIB surfaces are not recreated on animation frames.

Default `behavior: replace` replaces the active overlays. Opt-in `stack` keeps up
to six independently timed overlays, grouped by position, with a 10-DIP gap and
event-driven reflow on dismissal. The oldest remains nearest the screen edge;
new overlays extend inward. A full stack or insufficient display space fails
without evicting an existing choice. Supplying an active ID replaces that exact
item even in stack mode. Use the returned `id` when dismissing so a delayed
dismissal cannot close a replacement. An omitted dismissal ID targets only the
most recently active overlay, not the entire stack.
`duration_ms` includes both transitions, defaults to 5000 and accepts 500..120000.
The lifetime is not renewed by hover or status reads.

## CLI

```powershell
controller osd show --addr 127.0.0.1:8788 --title "PCController" --text "Desktop OSD preview" --duration 6s --icon info --action "Events=pccontroller://page/events"
controller osd status --addr 127.0.0.1:8788
controller osd dismiss --addr 127.0.0.1:8788 --id osd-1
# No running host: this process stays alive until dismissal/expiry.
controller osd show --standalone --text "Desktop OSD preview" --duration 6s
```

Use the actual configured listener (often 8787; GUI-owned hosts may use 8788).
The convenience CLI uses `PCCONTROLLER_IPC_TOKEN` if a credential is needed.
The existing `ipc call` CLI also supports its normal token-reference mechanism.

### Multiline messages and blocking script choices

```powershell
$controller = "$env:LOCALAPPDATA\Programs\PCController\bin\controller.exe"
$result = & $controller osd show --title "Choose an action" --icon info `
  --text "First line`nSecond line of the message" --duration 30s `
  --button "continue=Continue" --button "cancel=Cancel" --wait | ConvertFrom-Json
if ($result.last_action_id -eq 'continue') { 'Continue selected' }
```

`--wait` prints one final JSON result after expiry, dismissal or selection, rather
than an initial acceptance receipt. `dismissal` is `expired`, `dismissed`, `action`,
`replaced`, `closed` or `failed`; `last_action_id` and `last_action` identify the
selected choice and its caption. Terminal notification outcomes return exit 0;
invalid input, transport/presentation failure or interrupted waiting returns a
nonzero exit code. Ctrl+C stops waiting without replaying or implicitly dismissing
a host-owned notification. Standalone process exit closes its own overlay.

Without `--wait`, a host-owned show returns immediately. Standalone mode already
stays alive until completion; adding `--wait` changes its output to the final
result. An explicit wait performs bounded status reads; it neither holds a
long-running mutation request open nor starts another host or board connection.
The host retains the most recent 64 terminal receipts until process exit, so a
later notification cannot erase a choice before the script's next read.

Icons: `app`, `info`, `success`, `warning`, `error`, `door`, `hot`,
`temperature-cleared`, `motion`, `relay`, `radio`. `--severity` independently chooses
`info`, `success`, `warning` or `error` accents. `--theme system|dark|light` and
`--position top-left|top-center|top-right|center|bottom-left|bottom-center|bottom-right`
are also available.

### Optional sound effects and taskbar attention

```powershell
& $controller osd show --title 'Download complete' --text 'Your package is ready' `
  --icon success --sound success --flash-taskbar
```

Both options are off by default and work with stacking, replacement, actions and
`--wait`. HTTP/JSON-RPC use `"sound":"success","flash_taskbar":true` in the show
request. Sounds: `none`, `select`, `success`, `warning`, `error`, `connect`,
`disconnect`. These are asynchronous Windows system sound cues, honoring the
Windows sound scheme/volume and host mute or zero-volume policy. They do not
drive the board buzzer, read arbitrary files or download audio. Standalone mode
reads existing host audio preferences without creating or changing configuration.

Taskbar attention requests three flashes, without foreground activation or
caption flashing. It targets only this process's registered, visible primary
GUI, or its own visible classic console. A tray-only/background process, hidden
console or Windows Terminal pseudoconsole has no eligible taskbar window: no
unrelated window is flashed. An already foreground window needs no flash.

The per-ID `attention` receipt reports `sound: requested|suppressed|failed` and
`taskbar: requested|foreground|no-window|unavailable`, with an optional `error`.
`requested` means Windows received the request, not proof of audible output or
human attention. Attention errors do not cancel the displayed OSD; do not retry
the show mutation to repair them. Status reads, reflow and repaint never replay
attention effects. Each successfully admitted stacked/replacement OSD can request
its own cue.

See Microsoft's [system sound behavior](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-messagebeep)
and [non-activating window flash API](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-flashwindowex).

### Stacking and backgrounds

```powershell
& $controller osd show --text 'First message' --behavior stack --duration 12s
& $controller osd show --text 'Second message' --behavior stack --background acrylic
& $controller osd show --text 'Transparent overlay' --background none --position top-right
```

`--background current` preserves the charcoal/light card (default). `none` is an
alias for `transparent`: no card fill, outline or shadow; text, icons and button
surfaces remain. `acrylic` and `blur` reuse the existing native desktop composition
stack with a light tint over the real background, not a captured wallpaper.
Windows accessibility/transparency/composition policy may require a normal card;
the response's `background` reports the effective mode. A raster preview can show
the foreground but cannot prove a live compositor blur. `active_count` and `items`
list the active surfaces; each has its own identity, DPI, geometry and result.

## HTTP, JSON-RPC, WebSocket and NDJSON

`GET /api/osd` reads state; `POST /api/osd` shows; `DELETE /api/osd` dismisses,
optionally supplying `{"id":"..."}`. The same operations are
`controller.osd.status`, `controller.osd.show` and `controller.osd.dismiss` on
existing `/api/rpc`, `/ipc`, NDJSON and binary-UDP RPC transports. Existing host
remote/origin and read/message capability policies still apply. There is no
additional listener or permission bypass.
Status accepts an optional `id` (`GET /api/osd?id=...` over HTTP) to retrieve the
current or retained terminal result for that exact notification. Unknown/evicted
identities fail explicitly. Prefer generated unique IDs for concurrent scripts;
deliberately reusing an ID replaces that presentation.

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "controller.osd.show",
  "params": {
    "id": "preview",
    "title": "PCController",
    "text": "Desktop OSD preview",
    "icon": "info",
    "duration_ms": 6000,
    "position": "bottom-center",
    "actions": [{"label": "Events", "uri": "pccontroller://page/events"}]
  }
}
```

Schemas, enums and examples are in the generated `/api/docs/` explorer. API
success confirms native presentation acceptance, not completion of any button
action. A timeout after submission is an unknown outcome; do not blindly replay
it. `last_action` and `last_error` describe the current/last presentation; an
action link re-enters the normal application/OS route rather than bypassing
hardware admission.

## Autonomous events

The existing bounded/coalescing important-event notification worker can use the
OSD instead of native toasts. Merge these fields into the existing
`integrations.notifications` object in the host configuration; preserve its
other rules/actions:

```json
{"presentation":"osd","osd_duration_ms":7000,"osd_position":"bottom-center","osd_behavior":"replace","osd_background":"current","osd_flash_taskbar":true}
```

The default is unchanged (`native`). Existing enable/important-kind filters,
door safety rules, semantic icons, configured actions and audio preferences
still apply. Configuration reload is hot-applied; no output is actuated to
preview this setting. Scheduled tasks and external automation can also call the
same CLI/API explicitly.
`osd_flash_taskbar` defaults to false and applies only to these autonomous OSD
alerts. Their existing configured audio cue is played once by the notification
worker, not duplicated by the OSD sound option.
Position, behavior and background preferences also apply to API/CLI requests
that omit those fields; explicitly supplied values override them.

If the OSD cannot be presented (for example on a non-interactive host), autonomous
alerts fall back to the existing native notifier and record the degraded route.
Explicit OSD API calls return the presentation error instead of silently changing
the requested surface.

The composition path follows Microsoft's
[layered-window API](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-updatelayeredwindow)
and respects the [client-area animation preference](https://learn.microsoft.com/en-us/windows/win32/winauto/client-area-animation).

The dedicated rendering thread selects
[per-monitor DPI awareness](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setthreaddpiawarenesscontext)
before creating the window and restores its previous context on exit. This also
applies to standalone previews without an application manifest: Windows must not
stretch a 96-DPI backing bitmap on a high-density screen. Text, icons and rounded
edges are rasterized at the window's actual DPI, and rebuilt on DPI changes.
The process-wide DPI setting and other GUI windows are not changed. Older Windows
versions without thread-local DPI support retain the packaged host manifest.

## Verification

Normal tests use fake surfaces and never create a window. The opt-in
`PCCONTROLLER_OSD_LIVE=1` fixtures verify an actual source-owned window's expiry,
focus preservation, stale-ID dismissal and single human-input-path activation
using an inert callback, with no board connection or output. Set
`PCCONTROLLER_OSD_CAPTURE` to an absolute artifact directory to render dark/light
144-DPI previews using the actual renderer and embedded product icon.
The native-module fixture also asserts per-monitor window awareness and physical
backing dimensions; a non-visual test verifies restoration of an unaware caller.
An additional opt-in compositor fixture captures only an app-owned checkerboard
behind its own OSD for current/transparent/acrylic/blur comparison. It does not
capture user windows or inject global input. Source-owned input-window messages
verify URI actions and script choices without executing external commands.
