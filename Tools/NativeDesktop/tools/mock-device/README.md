# PCController firmware mock

This local development server implements the HTTP and WebSocket behavior used
by PCControllerDesktop. It binds to loopback by default and is deliberately separate from
the application's authenticated local API.

Start it from the repository root:

```powershell
go run ./tools/mock-device -listen 127.0.0.1:8080
```

Then set `device.base_url` to `http://127.0.0.1:8080`. Add `-cycle 5s` to
automatically toggle the state, or inject events while the desktop app is
connected:

```powershell
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:8080/mock/event `
  -ContentType application/json -Body '{"type":"on"}'

Invoke-RestMethod -Method Post -Uri http://127.0.0.1:8080/mock/event `
  -ContentType application/json -Body '{"type":"message","message":"Hello from the mock"}'

Invoke-RestMethod -Method Post -Uri http://127.0.0.1:8080/mock/event `
  -ContentType application/json -Body '{"type":"msg","icon":2,"message":"URL-encoded firmware message"}'

Invoke-RestMethod -Method Post -Uri http://127.0.0.1:8080/mock/event `
  -ContentType application/json -Body '{"type":"beep","count":3}'

Invoke-RestMethod -Method Post -Uri http://127.0.0.1:8080/mock/disconnect
```

`/mock/disconnect` closes every WebSocket connection with a service-restart
code, which exercises cached-address invalidation and reconnect backoff. The
`raw` event type can send an unsupported firmware frame to verify that the app
ignores it:

```powershell
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:8080/mock/event `
  -ContentType application/json -Body '{"type":"raw","raw":"unknown:test"}'
```

The mock also implements the six public diagnostic GET routes and the internal
`/config/ap` and `/config/st` projection sources. Its ASA mode
matches the firmware's numeric Wi-Fi status and line-oriented configuration
responses; `-diagnostics-format json` selects equivalent JSON fixtures. Both
configuration responses intentionally include unmistakably fake `pass` values
so tests can prove credentials are discarded before parsed data is cached or
serialized. They must never be used as real passwords.

An optional loopback-only raw/Telnet-style terminal is available for terminal
UI development. It is disabled unless `-telnet-listen` is supplied:

```powershell
go run ./tools/mock-device -listen 127.0.0.1:8080 `
  -telnet-listen 127.0.0.1:2323
```

The listener emits a small Telnet `WILL ECHO` banner and accepts bounded lines:
`help`, `state`, `on`, `off`, `toggle`, `message <text>`, `beep [1-10]`, and
`quit`. State, message, and beep commands also use the existing WebSocket
broadcast path, so GUI/web/TUI clients observe the same events. Active terminal
connections are closed and joined during mock shutdown.

Exposing the mock on a network interface requires the explicit
`-allow-non-loopback` switch. It has no authentication and should never be
treated as a production device emulator on an untrusted network.
