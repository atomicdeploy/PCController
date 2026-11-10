# Windows desktop OSD

PCController can present a transient overlay directly above the desktop, with
the main GUI closed. The existing host is the presentation owner; no board,
serial connection, browser or second GUI is required. Windows session 0 and
secure/UAC desktops are not supported, and exclusive full-screen applications
may cover ordinary desktop windows.

## Appearance and interaction

- PCController's native charcoal/violet or light palette, semantic event icons,
  antialiased rounded edges, DPI-sized system text and original-aspect artwork.
- A short cubic fade/slide at entrance and dismissal. Long headings/body text
  scroll at a fixed speed with pauses at both ends, independently of frame rate.
- Bottom/top center or right placement within the primary display work area.
- Up to five compact action buttons. Their URIs are inert until an explicit
  click; only `pccontroller:`, `http:` and `https:` routes are admitted. No
  arbitrary executable, remote icon download or script evaluation is accepted.
- The overlay does not take foreground focus or appear in the taskbar. Passive
  text/background lets desktop clicks through; the close/action targets remain
  interactive. Buttons are pointer-operated; keyboard focus remains in the
  foreground application.
- Windows' client-area animation preference and the host's reduced-motion
  preference disable automatic movement. Static overlays sleep until expiry;
  fonts, icons and DIB surfaces are not recreated on animation frames.

One overlay is current per host process. A new show replaces it. Use the returned
`id` when dismissing so a delayed dismissal cannot close a replacement.
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

## HTTP, JSON-RPC, WebSocket and NDJSON

`GET /api/osd` reads state; `POST /api/osd` shows; `DELETE /api/osd` dismisses,
optionally supplying `{"id":"..."}`. The same operations are
`controller.osd.status`, `controller.osd.show` and `controller.osd.dismiss` on
existing `/api/rpc`, `/ipc`, NDJSON and binary-UDP RPC transports. Existing host
remote/origin and read/message capability policies still apply. There is no
additional listener or permission bypass.

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
{"presentation":"osd","osd_duration_ms":7000}
```

The default is unchanged (`native`). Existing enable/important-kind filters,
door safety rules, semantic icons, configured actions and audio preferences
still apply. Configuration reload is hot-applied; no output is actuated to
preview this setting. Scheduled tasks and external automation can also call the
same CLI/API explicitly.

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
