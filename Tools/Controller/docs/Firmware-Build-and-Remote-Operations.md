# Firmware build and remote operations

PCController has one canonical firmware source/build contract. Local launchers,
the TUI command terminal, embedders, JSON-RPC, WebSocket, Socket.IO, and the Web
UI all route into the same guarded host command and artifact services. No remote
caller can supply a filesystem path or an arbitrary compiler flag to the typed
build method.

## Local builds

From the repository root:

```console
build.cmd --firmware-only --clean --no-color
firmware.cmd build
```

The first command runs the repository build pipeline and validates the packaged
Intel HEX/EEPROM pair. The second opens the firmware-studio build surface. Add
`--plan-json` or `--dry-run` to inspect its canonical plan without starting a
tool. `firmware.cmd watch` watches only curated firmware inputs, hashes content
instead of timestamps, coalesces edits during a build, and never programs a
board unless `--upload` is explicit.

## Remote build and live log (credentialless alpha)

Use two terminals so the event cursor is established before the build starts:

```console
bin\controller.exe ipc monitor --addr CONTROLLER_HOST:8787 --kind program --after latest
bin\controller.exe ipc call --addr CONTROLLER_HOST:8787 --timeout 15m --method controller.firmware.build --params "{}"
```

The typed call accepts either a reviewed list such as
`{"firmware_features":["eeprom-menu-labels"]}` or
`{"no_firmware_features":true}`. It returns `operation_id` plus the final
normalized log. During the call, every event subscriber receives ordered
`program.started`, `program.output`, and `program.completed` or
`program.failed` events with the same operation ID and monotonically increasing
line sequence. Remote lines remove ANSI/control sequences, stable build noise,
and configured machine-local paths. Both retained command output and streamed
logs are bounded to 1 MiB per operation; lines are bounded to 4 KiB. A
`program.output.truncated` event reports dropped bytes and its sequence, and
retained command text explicitly marks truncation.

Validation and compiler failures preserve the operation ID: JSON-RPC errors
contain `error.data.operation_id` and normalized `error.data.output`. The
`program.started`/`program.failed` pair also covers preflight failures.
This method compiles only: it does not program a board or perform HELLO.

WebSocket clients subscribe with `controller.subscribe`. Socket.IO v4 clients
send the supported `subscribe` event and receive the same `controller.event`
envelopes. Long-poll clients use `controller.event.next`, which is also how
`ipc monitor` works. Current alpha deliberately disables authorization; remote
exposure still requires `allow_remote` and valid Origin handling. Tokens are
not an enforced security boundary. Use only trusted networks until the explicit
authentication issue is implemented. Capability policy metadata describes the
future boundary, not current authenticated acceptance.

## Control-surface parity

| Capability | CLI | TUI | API / IPC |
| --- | --- | --- | --- |
| Local compile | `program compile .` and `firmware.cmd build` | shared command terminal | `controller.firmware.build` |
| Content-aware watch | `firmware.cmd watch` | not duplicated; TUI receives resulting build/update events | deliberately local-only; no remote filesystem exposure |
| Remote artifact transfer | artifact upload/fetch commands | guarded Programming page and command terminal | REST artifact routes plus typed update RPC |
| Ordered live build log | `ipc monitor --kind program` | activity/event stream and Console | `program.*` through long poll, WebSocket, or Socket.IO |
| Update progress | terminal output and `update.*` monitor | active operations only; idle has no false bar | `controller.update.status` and `update.*` |
| Physical write | explicit guarded `controller program` / update authorization | selectable guarded Programming actions | primary-host update RPC with capability and authorization gates |

The standalone `ws serve/client` pair remains a loopback-oriented compatibility
relay for a watched Intel HEX file. It is not interchangeable with the
RPC/WebSocket/Socket.IO server: remote clients use
the latter so immutable hashes, ordered events,
backup/readback, and the single serial owner remain enforceable.

## Transfer and programming

The remote path is the artifact/update API with the alpha exposure boundary above:

1. upload an immutable Intel HEX artifact with byte count and SHA-256;
2. request `controller.update.firmware` with `authorized: true` and an
   idempotency key;
3. follow `controller.update.status` and `update.*` events through backup,
   UART release, programming, readback, application reconnect, and completion.

Application opcodes and the bootloader/programmer are mutually exclusive. A
normal flash therefore inspects the Intel HEX, captures flash, EEPROM, and
metadata into content-addressed SHA-256 storage, verifies the manifest, writes
and verifies the target, then requires an authenticated application HELLO. The
blank-board path synchronizes the toolchain, reads the ISP signature and backup,
installs the reviewed core bootloader/fuses, and optionally completes a guarded
UART flash and health check. These durable instructions live here instead of
occupying an idle TUI screen.

Client surfaces show a progress bar and percentage only while an operation is
active. With no operation, they show neither an idle-state badge nor a synthetic
zero-percent bar; completed, staged, downloaded, and failed results remain
truthful terminal outcomes without pretending that work is still advancing.

`controller program` already delegates to a running primary host, so only that
host owns the serial port. The compatibility `controller ws serve/client`
pair can watch and SHA-256-validate a HEX file before guarded flashing, but its
standalone relay is not the authenticated LAN boundary and should normally stay
on loopback or behind a separately authenticated tunnel.

## Output presentation contract

The older PowerShell-oriented build presentation performed broad regular-
expression rewriting over verbose Arduino CLI output: home/workspace/build
paths became stable labels, ANSI stages were recolored, and cached dependency,
board/core, alternative-library, and preference chatter was hidden.

PCController's equivalent is layered:

- Arduino CLI is invoked without verbose mode by default, avoiding most noise;
- the shared Node presenter renders named stages, tables, warnings, and failures;
- deterministic source SHA-256, FQBN, features, timestamp, output paths, and
  dependency pins live in the build manifest rather than being inferred from
  console text;
- remote `program.output` performs bounded control-sequence/path normalization
  and suppresses only known non-error Arduino discovery chatter;
- bounded local diagnostics and CI logs remain available when a compiler fails.

This deliberately does not copy weak behavior such as MD5-only identity,
unbounded unauthenticated Socket.IO payloads, arbitrary remote shell commands,
or programming without a verified backup/readback transaction.
