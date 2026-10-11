# Linux shared-firmware / mirroring verification

2026-10-11. Scope: the existing canonical checkout and `root@server`; no new
worktree, no physical AVR firmware programming, no Cafe/Erfan deployment.

## Actual host and preservation

- Ubuntu 26.04, Linux 7.0.0-30-generic, x86-64 ASUS motherboard.
- No exposed GPIO chips or attached USB AVR serial ports. Host I2C buses were
  inventoried by device names only, never scanned or written.
- `pcspkr` and console devices present. Native Controller `pc-speaker
  --frequency 440 --duration 80` exited successfully as root. This proves
  finite kernel playback/stop was accepted, **not audible physical output**.
- `/srv/projects/PCController` was clean, owned by `asus`, on
  `agent/linux-platform-recovery@a4f54fd30f899dce88dd38b56acda624688494a4`.
  Three unique live-measurement/TUI commits were fetched without a clone,
  worktree, reset or rebase and preserved on GitHub as
  `investigate/live-measurement-policy`. Retain that branch: its strict cadence,
  freshness, TUI and Web preferences need semantic reconciliation with current
  policy; an old whole-tree replacement would regress newer work.
- The existing server checkout was fast-forwarded to current main, then the
  tested semantic feature branch. Old linked worktrees were not changed.
- Native builds reused `.build/release`; live integration configs/history and
  the cross-built controller were in task-specific `/dev/shm` fixtures. The
  test closes all four children and removes only its own mkdtemp directory.
  No dependency reinstall, destructive cache purge or global TEMP/RAM-disk
  configuration was used.

## Reproducible evidence

- Stable Windows full Go runner: **45 packages passed**; `go vet ./...` passed.
- Linux production-core CMake build: **23/23 tests passed**, including actual
  UART framing, HAL, C ABI, EEPROM, transport, RESET and source-parity checks.
- New firmware regression checks cover capability advertisement, volatile
  navigation leaving EEPROM settings unchanged, and invalid request rejection.
- The server's Go 1.27.2 download failed with HTTP 403 from the official module
  proxy (and direct archive retrieval was unavailable). The Linux controller
  was therefore cross-built with the installed declared Go toolchain on David's
  PC, `GOOS=linux GOARCH=amd64 CGO_ENABLED=0`, using real TCP board transports.
  Do not infer a successful on-server Go build or C ABI from that smoke.
- `Tools/Controller/tests/board-mirroring-linux.test.mjs` starts two distinct
  production-firmware processes and two real primary controller owners. The
  live server run passed:
  - PWM/PCA9685 in both directions, exact readback and stable no-echo sequence;
  - R5 on/off both directions and real `ON:5` / `OFF:5` stdout;
  - volatile receiver menu navigation both directions;
  - pushed finite buzzer notes both directions, ACK-only disposition and no
    recurring note sequence;
  - four raw segment cells both directions, including renewal beyond lease;
  - disconnect quarantine, no actuation replay on reconnect, explicit verified
    resync from a selected board;
  - TM1637 fixture disappearance/recovery without RESET;
  - independent door state and unavailable real sensors, not invented readings.

```sh
node Tools/Controller/tests/board-mirroring-linux.test.mjs \
  /path/to/controller \
  Tools/VirtualBoard/.build/release/bin/virtual_board
```

The test deliberately uses real board messages rather than a mock coordinator.
It exposed and fixed both pre-write polling baselines losing an immediate
reverse change and JSON omitting meaningful zero/off state.

## Remaining gates

Second-AVR physical commissioning is pending connection. No motion safety,
arbitrary I2C, strip effect phase-lock, multi-local-board in-process registry,
GPIO/ALSA deployment, Pi hardware acceptance, production daemon rollout or
human audio/display acceptance is claimed. Broader #366/#106/#103/#227 remain
open; this is a supported semantic mirror implementation, not closure of every
architecture/deployment dependency. See [commissioning](../Board-Mirroring.md).

The extended capability lives in the existing additive HELLO feature fields;
runtime authorization/availability decisions use its feature bit, not a
version selector. The broader generated negotiation work has not been
reimplemented or declared complete here.
