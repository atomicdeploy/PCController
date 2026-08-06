# Portable WebUI bundle

The production WebUI is embedded in the host executable and can also be
exported byte-for-byte as a deterministic ZIP. The portable form is useful for
auditing the shipped interface or serving the same interface from a trusted
static origin while the controller host remains the API and event authority.

## Archive contract

`internal/webui.ExportZIP(io.Writer)` writes the exact embedded `dist` tree at
the ZIP root and returns:

- the number of regular files;
- their total uncompressed byte count; and
- the SHA-256 digest of the complete ZIP stream.

Entries use forward-slash relative paths, lexical ordering, a fixed timestamp,
regular-file mode `0644`, and the ZIP `Store` method. Traversal paths,
backslashes, non-regular files, more than 4,096 files, and a total payload over
64 MiB are rejected. No token, host configuration, or user setting is added to
the archive.

The archive includes `index.html`, the multi-size `favicon.ico`, SVG icon,
manifest, network-only service worker, theme bootstrap, transport configuration,
hashed JavaScript/CSS assets, and the bundled Persian font. Extraction preserves
the same bytes that the executable serves.

Serve the extracted directory as the root of an HTTP or HTTPS static origin;
opening `index.html` through `file:` is intentionally unsupported. A static
server should not cache `index.html`, `controller-config.js`, or the service
worker permanently.

## Selecting the controller host

The embedded interface keeps its ordinary relative routes:

- REST and RPC use canonical `/api/...` URLs;
- events and full-duplex RPC use the configured `/ipc` WebSocket path; and
- no target override is stored for normal executable-hosted use.

An exported interface can select a controller in one of these explicit ways:

1. Add `?controller=http://127.0.0.1:8787` to the static page URL. A valid value
   is normalized and retained in local storage for later visits.
2. Call `setControllerOrigin(...)` from trusted page code to set the same local
   preference, or `clearControllerOrigin()` to return to same-origin routing.
3. Edit `controller-config.js` before deployment:

   ```js
   globalThis.PCControllerWebConfig = {
     controller_origin: 'https://controller.example',
     trusted_controller_origins: ['https://controller.example'],
   }
   ```

HTTP, HTTPS, WS, and WSS roots are accepted. Credentials, paths, query strings,
fragments, control characters, and non-network schemes are rejected. Loopback,
private, link-local, and local-DNS targets are allowed directly. A public target
must use HTTPS or WSS and must exactly match a generated
`trusted_controller_origins` entry. HTTP roots are mapped to WS for the event
stream; HTTPS roots are mapped to WSS.

After changing the local preference, reload the page so the existing event
socket is replaced. Browsers also enforce mixed-content rules, so an HTTPS
static page must use an HTTPS/WSS controller endpoint.

Never place bearer tokens in the query override or `controller-config.js`.
Session authorization remains in browser session storage and is sent only to
the validated controller origin. Tabs are partitioned by selected controller,
so tabs pointed at different hosts do not exchange terminal or event messages
through `BroadcastChannel`.

## Controller origin policy

Cross-origin browser access is still denied by default. The static page origin
must match the controller's explicit `ipc.allowed_origins` policy. For an
allowed origin, the host returns a narrowly scoped CORS response for control
plane routes and accepts only the required methods and request headers. It does
not emit a wildcard origin or enable credentialed cookie requests. Bearer and
capability authorization still run on the actual request after preflight.

For example, a bundle at `http://localhost:4177` can use the existing loopback
origin policy. A page at `https://console.example:9443` requires an explicit
`console.example:9443` entry in `ipc.allowed_origins`. If the static server adds
a Content Security Policy, its `connect-src` must likewise name the selected
HTTP(S) and WS(S) controller origins.
