# Browser automation API

The embedded WebUI publishes one small, frozen controller at `window.PCController`. It uses the same authenticated command dispatcher and live status/event stream as the CLI and TUI; it does not open the serial port independently.

## Inspect and control

```js
PCController.inspect()
await PCController.command('status')
await PCController.refresh()
PCController.navigate('workbench')
```

`inspect()` returns the current host version, route, controller connection truth, port, WebSocket state, and retained event count. `command()` accepts a normalized controller command and returns its text result. Sensitive values are never written into this object or browser storage.

## Audio

```js
await PCController.beep()                      // 2000 Hz, 40 ms, board + browser
await PCController.beep(440, 120, 'board')
await PCController.beep(1320, 60, 'browser')
```

Frequency is bounded to 20–20,000 Hz and duration to 1–60,000 ms. Targets are `board`, `browser`, or `both`. A board target rejects while no authenticated controller is connected; browser audio still follows the user’s audio preference and browser playback policy.

The Workbench terminal also accepts the shared `beep`/`buzzer` command family. Board audio policy—including global silence and relay cues—remains authoritative in controller settings.

## Live state and tab coordination

Listen for the bounded, immutable browser summary:

```js
addEventListener('pccontroller:state', ({ detail }) => console.log(detail))
```

The application also uses `BroadcastChannel` when supported so sibling tabs converge on appearance, terminal transcript, connection presence, and pushed controller state. Hardware commands still travel through authenticated RPC/WebSocket transport; BroadcastChannel is never an authority for device writes.

## Styled console records

Workbench implements the familiar `console.*()` methods and `%s`, `%d`, `%i`, `%f`, `%o`, `%O`, `%c`, and `%%` substitutions. Each style belongs in its own `%c` argument:

```js
console.info('%cBoard%c %s', 'color:#7656d6;font-weight:700', '', 'connected')
console.groupCollapsed('Diagnostics')
console.table([{ rail: '12 V', value: 12.21 }])
console.groupEnd()
```

Workbench mirrors these records into the browser console without evaluating arbitrary JavaScript. Groups, counters, timers, tables, structured objects, and styled spans are bounded before rendering.

## Connection meaning

The main header reports the authenticated controller/bridge connection. The separate Device page reports an optional local companion integration. `Offline` on that page does not mean the controller or WebSocket bridge is offline; see [Host configuration and integrations](Host-Configuration-and-Integrations.md).
