RayanLamp Static Web Dashboard
==============================

This archive contains only the compiled RayanLamp browser interface. It does
not contain RayanLamp.exe, a web server, an API token, or frontend source code.

Running the dashboard
---------------------

1. Extract the complete archive without changing its directory structure.
2. Serve the extracted directory with a trusted static HTTP server.
3. Open the HTTP address reported by that server in a modern browser.

Opening index.html directly through a file:// URL is unsupported. JavaScript
modules, service workers, browser storage, and device requests have additional
restrictions under file:// that make the result unreliable.

Connection modes
----------------

This static edition starts in Direct mode. Use the Connection control in the
dashboard to change the lamp origin or transport.

Managed mode is used when these same assets are served by RayanLamp.exe. The
Go manager coordinates lamp state, native settings, automation, notifications,
diagnostics, and synchronization between native and browser interfaces.

A plain static server cannot provide the manager's private same-origin browser
session. Coordinator mode is available only when this directory is hosted
behind an origin that forwards /app/session and the versionless coordinator
routes (/status, /lamp/*, /ui/*, /events, and the other documented API paths)
to RayanLamp. The embedded /app/ copy already has that arrangement.

Direct mode connects from the browser to the configured lamp URL. The default
device is http://rayanlamp.local and the firmware endpoints are /lamp/get,
/lamp/on, /lamp/off, and /ws. Direct mode can expose
only browser and lamp features; native-only manager settings are unavailable.

Browser security notes
----------------------

The dashboard origin and lamp must be compatible with browser CORS and
WebSocket Origin policy. An HTTPS-hosted dashboard cannot connect to an
HTTP/WS-only lamp because browsers block mixed content. For the default
HTTP/WS firmware, serve this dashboard over HTTP on the trusted local network.

Keep the extracted files together. Hashed assets and the service worker are
part of one versioned build and should not be mixed with files from another
RayanLamp release.
