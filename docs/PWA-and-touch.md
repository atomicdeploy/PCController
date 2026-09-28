# Web PWA and touch behavior

PCController's embedded WebUI is an installable, standalone single-page
application. The HTML page declares the manifest, mobile and Apple web-app
metadata, safe-area viewport, light/dark theme colors, and a short Fuji-inspired
first-paint shell. React then replaces that shell with the accessible boot gate
and the normal hash-routed SPA.

The service worker deliberately has a narrow cache policy:

- It precaches the immutable UI shell and caches fingerprinted bundles, fonts,
  and icons after a successful online load.
- It uses a network-first navigation response, falling back to the cached SPA
  shell only when the network is unavailable.
- It never caches `/api`, `/ipc`, health endpoints, or generated controller
  configuration. An offline installation can open its shell, but can never
  represent an old board state as live.

The normal build already fingerprints application assets; the host serves them
immutable and serves the HTML, manifest, and worker with `no-cache`, allowing
the browser to promptly find a new application release.

## Touch behavior

Ordinary buttons, switches, and checkboxes activate on primary-touch
`pointerdown`. Pointer Events are the cross-browser equivalent of `touchstart`
and also preserve mouse and pen behavior. The follow-up compatibility click is
suppressed once, preventing duplicate relay/UI commands. Keyboard activation is
not suppressed.

Controls that must remain release-sensitive, including Side A/B motion buttons,
carry `data-touch-mode="hold"`; they do not activate through this fast path.
The CSS removes only the browser's tap-flash highlight. Visible focus rings and
selected-state indication remain accessible.

`navigator.vibrate()` is feature-detected and used for short semantic feedback
cues only after an eligible visible user interaction; unsupported browsers
silently omit it.
