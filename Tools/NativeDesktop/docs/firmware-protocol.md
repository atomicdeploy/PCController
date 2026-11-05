# Firmware protocol

The default base URL is `http://rayanlamp.local`. The configured URL must use
HTTP or HTTPS, contain a host, and contain no credentials, query, fragment, or
path prefix. HTTPS automatically maps the WebSocket transport to WSS.

## Lamp commands

RayanLamp uses GET because that is the existing firmware contract:

| Method | Path | Meaning |
| --- | --- | --- |
| `GET` | `/lamp/get` | Return the current state |
| `GET` | `/lamp/on` | Turn the lamp on |
| `GET` | `/lamp/off` | Turn the lamp off |
| `GET` | `/lamp/toggle` | Toggle when implemented by the firmware |
| WebSocket | `/ws` | State, message, and cue event stream |

`/lamp/get` must include a recognizable state. The parser accepts a plain
state, a JSON string, or a JSON object whose case-insensitive `state`, `status`,
`lamp`, or `value` member is a state string or Boolean. Recognized strings
include the model's on/off aliases. A successful `/lamp/on` or `/lamp/off`
response with no state is still authoritative. Toggle can succeed with an
unknown returned state, in which case a later WebSocket or HTTP snapshot
confirms it.

All responses are limited to 1 MiB and redirects are returned as errors.

## WebSocket lifecycle

The client connects to the base URL's `/ws`, installs a 64 KiB read limit, and
sends one text frame:

```text
get
```

It sends this once per successful connection. No periodic state command is
sent because the audited firmware leaks memory per command. A ping control
frame is sent every 25 seconds; a pong must keep the read deadline within 55
seconds. Only text frames are interpreted.

Supported incoming frames:

| Frame | Result |
| --- | --- |
| `ON` | State snapshot: on |
| `OFF` | State snapshot: off |
| `message:<text>` | Custom notification message |
| `msg:<icon>,<URL-encoded-text>` | Custom notification with firmware icon number |
| `beep:<count>` | Play 1 through 10 audible cues |

Matching is case-insensitive for command prefixes. Leading/trailing whitespace
is removed. Unknown frames are ignored.

Validation limits:

- complete remote frame: 16 KiB;
- message payload: 4 KiB;
- icon: integer from 0 through 255;
- beep count: integer from 1 through 10;
- text: valid UTF-8, no NUL, and no unsupported control characters.

The current UI uses the validated message text and preserves the numeric icon
as event metadata. It does not download remote artwork or treat message text as
markup.

## Reconnect and HTTP fallback

The application-level defaults are a 1 second minimum, 30 second maximum, and
30 second HTTP poll. The WebSocket layer applies bounded exponential delays
with jitter, resets its failure count after a stable connection, and publishes
connecting/online/waiting/stopped phases to the model. Changing configuration
restarts the network generation so stale callbacks cannot update current state.

When polling is non-zero, `/lamp/get` provides a periodic reachability/state
fallback. Setting `lamp.http_poll` to `"0s"` disables it.

The `.local` address cache and invalidation behavior are documented in
[Architecture](architecture.md#local-resolution-cache).

## Read-only diagnostics

Normal mode exposes this immutable allowlist:

| Path | Typical content |
| --- | --- |
| `/info` | Device and firmware information |
| `/update/info` | Installed/update metadata |
| `/wifi/signal` | Signal strength |
| `/wifi/status` | Connection status |
| `/wifi/country` | Regulatory country |
| `/description.xml` | Device description |

Every diagnostic request must be an exact, query-free GET path from that
single list. Absolute URLs, fragments, alternate methods, traversal, and
unknown paths are rejected. `/datetime/get`, `/pu`, `/is_exec`, `/page_id`,
`/eProcessState`, and `/eResponseStatus` belong to the separate ASA PU/debug
project and are explicitly rejected, as are public requests for `/config` or
`/config/*`, `/crash`, `/sessions_debug`, `/login`, and `/update`. Older
configuration files that contain the exact retired arrays are migrated to the
safe policy; modified retired arrays fail closed.

The Go coordinator and direct browser client also poll two exact,
projection-only GET sources:

| Path | Projected content |
| --- | --- |
| `/config/ap` | Access-point SSID and network addressing |
| `/config/st` | Station SSID, DHCP state, and network addressing |

These two paths are not public diagnostic permissions: they cannot be selected
through `diagnostics.fetch`, the CLI/TUI raw viewer, the loopback diagnostics
API, or the direct/static browser's raw-response action. Both firmware
responses contain a `pass` field. The Go path removes that value before parsing;
the direct browser parses it immediately into a redacted document and discards
the response string. Neither credential is retained in `Document.Raw`,
`Field.Raw`, cache snapshots, broadcasts, IPC/API results, logs, saved output,
or UI state. Only the redacted typed projection is available to consumers.

The application refreshes the useful endpoints automatically after the lamp is
reachable and keeps a typed, last-known-good cache. Responses may be JSON, XML,
plain `key: value` lines, or the compact line-oriented ASA format. Parsed device,
network, firmware, and update values feed both native and web summaries. Exact
raw output remains available only for the six public, noncredential diagnostic
paths and only as an explicit advanced action; browser cache snapshots contain
a redacted `SafeRaw` projection. The two Wi-Fi configuration sources never have
an exact-raw path.

## Telnet-style device terminal

The audited ASA firmware has one `ESPTelnet` listener. Its default
`Telnet_Port` is 23, it is advertised as `_telnet._tcp` when initialization
succeeds, and it accepts one active client. The stream is plaintext and has no
login, password, TLS, ACL, or application authentication.

On connect the firmware emits `IAC WILL ECHO` (`FF FB 01`), an ANSI
clear-screen sequence, a colored welcome banner, and then character-oriented
output. The library is not a complete Telnet implementation: it does not parse
input-side IAC negotiation. RayanLamp therefore strips emitted Telnet commands
but does not reply with negotiation bytes that this firmware would mistake for
shell input. ANSI cursor/color/erase output is preserved; OSC and DCS control
strings are removed before reaching UI terminal emulators.

The checked source defines `ShellOnSerial`. In that build, `onTelnetInput`
returns without processing bytes, the network banner says `Shell is on
Serial`, and commands execute only on UART. RayanLamp detects that banner and
marks the session read-only. If firmware is rebuilt without `ShellOnSerial`,
its shell accepts character input, executes on LF (CRLF is accepted), and has
a 64-byte command buffer. Built-ins include read/write configuration and
reboot operations, and password getters expose plaintext credentials.

Consequently the app:

- never opens the terminal automatically;
- connects only to the configured lamp hostname and `lamp.telnet_port`;
- shares the lamp client's failure-aware `.local` resolution cache;
- bounds each input frame to 4096 bytes and in-memory scrollback to 256 KiB;
- maps the Web terminal's bare Enter key to CRLF, matching the firmware's LF
  command execution while leaving pasted/raw bytes unchanged;
- purges the prior transcript on explicit disconnect and at every new session;
- keeps transcript bytes out of logs, notifications, scripts, diagnostics,
  service-worker caches, browser storage, and BroadcastChannel;
- exposes direct/static Web UI terminal access only through a same-origin Go
  coordinator, because browsers cannot open raw TCP sockets.

Enabling the firmware's network shell should be paired with authentication,
removal/redaction of credential getters, interface/subnet restrictions, an
idle timeout, and rate limiting. HTTP and `/ws` remain the authoritative lamp
control/state transports.

## Development mock

The server under `tools/mock-lamp` implements all six public diagnostics and
both projection-only configuration sources, and adds test-only `/mock/event`
and `/mock/disconnect` POST routes. Its fake credential values exist solely to
exercise the redaction boundary. It binds to `127.0.0.1:8080` by default. See its
[usage guide](../tools/mock-lamp/README.md).
