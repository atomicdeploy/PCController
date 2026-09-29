# Linux desktop runtime provisioning

The packaged Controller owns the Linux desktop-runtime lifecycle. These
commands are dispatched before PCController opens a device configuration, so a
fresh host or a host with a damaged per-user config can still install, inspect,
roll back, or remove the runtime.

```sh
controller toolchain runtime-stage \
  --package /srv/projects/PCController/Tools/Controller/bin \
  --virtual-board /srv/projects/PCController/Tools/VirtualBoard/.build/release/bin/virtual_board \
  --dry-run --json

sudo controller toolchain runtime-install \
  --target-user asus \
  --package /srv/projects/PCController/Tools/Controller/bin \
  --virtual-board /srv/projects/PCController/Tools/VirtualBoard/.build/release/bin/virtual_board \
  --apply --json

controller toolchain runtime-status --json
sudo controller toolchain runtime-rollback --apply --json
sudo controller toolchain runtime-uninstall --apply --json
```

`runtime-install` is read-only unless `--apply` is present. When privileged
apply receives the target user's exact canonical repo outputs, the Go
provisioner first pins and copies them without execution into a full-hash,
root-owned release below `/var/lib/pccontroller/runtime-input`; no manual copy
or shell staging is required. A privileged install then accepts only that
root-owned, non-group/world-writable package, manifest, Controller, and
VirtualBoard beneath equally trusted non-symlink ancestry.
Validation and copying use pinned `O_NOFOLLOW` descriptors, so replacing an
input pathname cannot change the bytes that are published. Dry-run never
executes caller-selected artifacts. Apply first copies the pinned bytes into a
private root-owned stage, then runs bounded Controller `version` and
VirtualBoard `--help` smokes from that stage as the target user. The Controller
version and source hash must exactly match the canonical host manifest.
Chrome/Chromium and host tools resolve to root-owned, non-writable regular
executables.

## Publication and rollback

Validated files are copied to an immutable release directory below
`/opt/pccontroller/runtime/releases`. The stable entry points are deliberately
inside a separate namespace from the APT-mirror provisioner's
`/opt/pccontroller/bin/controller` executable:

```text
/opt/pccontroller/runtime/bin/controller
/opt/pccontroller/runtime/bin/virtual-board
/opt/pccontroller/runtime/manifest.json
```

Those root-owned links follow the atomically replaced `current` release link.
An upgrade retains the old `current` target as `previous`. Publication is
rolled back if target-user unit linking or activation fails, and an explicit
`runtime-rollback --apply` exchanges the verified current/previous releases.
Every release contains its own runtime manifest, original host manifest,
binaries, and root-owned user-unit definitions.

## Graphical user services

The installer links three immutable units into the selected target account:

- `pccontroller-virtual-board.service` listens only on `127.0.0.1:8765` and
  keeps its EEPROM below the user's data directory;
- `pccontroller-controller.service` uses the process-only
  `--listen 127.0.0.1:8787` override (without rewriting saved network policy)
  and connects to `tcp://127.0.0.1:8765`; its `ExecStartPre` runs
  `desktop ensure` from the final stable Controller executable;
- `pccontroller-window.service` opens `http://127.0.0.1:8787/` as a dedicated
  Chrome/Chromium application window with a persistent per-user profile.

The links are created by the published Controller while running through
`runuser` as the target UID; root never writes through user-writable home
ancestors. All three are wanted by `graphical-session.target`. An apply stops
the old window, restarts VirtualBoard and Controller, proves their active
MainPIDs use the selected release, proves the `127.0.0.1:8765` listener belongs
to VirtualBoard, waits for `/healthz`, and makes an authenticated snapshot call
that requires the expected TCP endpoint and PCController HELLO identity before
restarting Chrome. Chrome repeats the health and authenticated identity gate in
`ExecStartPre`. If any check fails, publication and services return to the old
release. Without an active graphical target and DISPLAY/Wayland environment,
the report is explicitly deferred and the enabled units start with the next
graphical session.

The native VirtualBoard protocol itself remains a loopback TCP protocol, not a
cryptographic authentication boundary. The provisioner mitigates local port
substitution by checking listener ownership, service MainPID/executable
identity, and the board HELLO before accepting readiness.

## Removal boundary

Uninstall stops the services when possible and removes only exact managed unit
links plus `/opt/pccontroller/runtime`. It cannot remove the sibling APT mirror
executable, compatibility shim, or mirror manifest. It deliberately preserves the target user's
PCController configuration, secrets, VirtualBoard EEPROM, Chrome profile, and
other XDG data.

This runtime provisioner installs no long-running privileged Controller
service. The least-privilege daemon design tracked by GitHub issue #116 is a
separate architecture and permission boundary; the runtime manifest records
`privileged_daemon_installed: false` so the two paths cannot be conflated.
