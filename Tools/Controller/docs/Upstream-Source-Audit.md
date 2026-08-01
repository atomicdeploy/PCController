# Upstream Source Audit

Inventory was performed on 2026-07-30 before implementation. No reference
source was pasted into the new Go packages.

## Local project references

### Existing `PCController\host`

The original host used an obsolete serial stack that is incompatible with the
current native COBS/CRC protocol. It was inspected for expected host features,
then excluded from the final `Tools\Controller` source and build. No source
from that implementation remains in the active host.

### `..\Puzzles` and `..\Timer`

Both are Arduino sketches with the inherited flat LocalLib layer. They do not
contain a PC controller. `Timer\program.cmd` does show the historical remote
programming flow: download a HEX over HTTP, then invoke MiniCore AVRDUDE with
`-curclock`, a serial port, 115200 baud, and `-xnometadata`.

The new programmer launcher retains that useful workflow without its
machine-specific paths, COM5 assumption, unchecked HTTP download, or
destructive temporary-file commands.

### `..\..\motor_encoder_hmi`

This is an Arduino/Nextion/motor project, not a host tool. Its local Git remote
is `https://github.com/mohammad8f5/motor_encoder_hmi.git`; inspected HEAD was
`465ebadb14a06d176c70cb380cf2895ab0121dac`. The working tree contains local
changes, so it was treated as a read-only behavioral reference.

### `~/Desktop/ASA0002E`

This repository is `https://github.com/atomicdeploy/pu850-esp.git`; inspected
HEAD was `01105c8fe24984b8fe43aa2b066c7279e0cf7724`.

Its `Tool` directory contains Node, Go, C#, and C++ implementations of a
carefully verified HTTP OTA transfer client for ASA-family ESP firmware:
manifest validation, hashes, backup/download, watch mode, atomic replacement,
and post-upload verification. That HTTP/gzip/ESP contract does not apply to
ATmega328P AVRDUDE programming. This project reused only general reliability
principles: bounded input, content hashes, stable-file detection, explicit
verification, serialized programming, and temporary-file cleanup.

No root license was present locally or reported by GitHub, so no ASA source
was copied.

### PuzzleBoard `Tools\Server.js` and `Tools\Client.js`

Located at:

```text
~/Desktop/Projects/PuzzleBoard/Tools/Server.js
~/Desktop/Projects/PuzzleBoard/Tools/Client.js
```

The server watches one HEX file with chokidar, debounces for 500 ms, and emits
a Socket.IO `fileModified` event containing path, size, time, and ASCII data.
The client reconnects through Socket.IO, stores the received file temporarily,
and launches AVRDUDE/urclock. It hard-codes COM6 and relies on `AVR_HOME`.

There is no project license or Git repository at that path. The Go relay is
therefore an original, non-wire-compatible implementation of the workflow:
standard WebSocket, versioned JSON, bounded file size, SHA-256, safe base
names, reconnect, and configured programmer invocation.

## Network references

### `DRSDavidSoft/Ardush`

- URL: https://github.com/DRSDavidSoft/Ardush
- Inspected HEAD: `29be66cd4c40fb09475b683b794f11d97ee5abad`
- License: MIT, copyright David Refoua (2018)

Ardush is an Arduino VT100-style shell with a bounded command buffer, prompt,
cursor movement, insertion/deletion, Ctrl-C, basic commands, and terminal
escape handling. The Go shell adopts the interaction ideas through Bubbles
text input, its own tokenizer/registry/history, and the host terminal. Ardush
source code was not copied.

### Requested `AtomicDeploy/ps_shell`

The exact `AtomicDeploy/ps_shell` and hyphen/case variants returned repository
not found. The closest exact-owner match is:

- URL: https://github.com/atomicdeploy/portable-shell
- Inspected HEAD: `bbcb4b34110d839c116d8e1e0083e817eaafae33`
- Contents: design README only
- License: none declared

Its design calls for character I/O abstraction, history, completion,
VT100 editing, registered parameters, and a TUI editor on embedded and desktop
targets. The new host shell implements analogous host-side concepts in
original Go code. No source was available to copy and no license permission
was assumed.

`atomicdeploy/websocket-client` also exists at inspected HEAD
`93b92d32a8f36f8a62ef895ccf005ae85904ab57`, but it is a generic browser
WebSocket UI, not the PuzzleBoard upload tools, and declares no license. It was
not imported.

## Go dependencies

| Module | Version | License |
|---|---:|---|
| `github.com/charmbracelet/bubbletea` | 1.3.10 | MIT |
| `github.com/charmbracelet/bubbles` | 1.0.0 | MIT |
| `github.com/charmbracelet/lipgloss` | 1.1.0 | MIT |
| `github.com/coder/websocket` | 1.8.15 | ISC |
| `github.com/fsnotify/fsnotify` | 1.10.1 | BSD-3-Clause |
| `go.bug.st/serial` | 1.8.0 | BSD-3-Clause |

Transitive modules and exact versions are captured in `go.sum`; their license
files remain in the Go module distributions. No third-party source was
vendored. `fsnotify` is used through its public Go API to watch the persistent
host-config directory; the implementation also retains a slow safety poll for
file systems that omit replacement notifications.
