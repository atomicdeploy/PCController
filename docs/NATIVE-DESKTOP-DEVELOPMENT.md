# Desktop development and recovery

The native GUI is part of the main `controller.exe`, linked from
`Tools/NativeDesktop` through `Tools/Controller/cmd/controller/native_*windows.go`.
Use this integrated entry point, not the retained donor standalone executable:
the PCController core remains the sole transport and hardware owner.

## Launch

From the repository, in any working directory:

```powershell
.\desktop.ps1                          # Installed GUI, hardware auto-connect paused
.\desktop.ps1 --preview                # No core, network or hardware transport
.\desktop.ps1 --preview --page outputs # Offline channel-card preview
.\desktop.ps1 --attach 127.0.0.1:8788  # Attach to an explicitly selected running core
.\desktop.ps1 --package C:\staged-host # A package made by the canonical build
```

`desktop.cmd` provides the same options. `--config FILE` selects a separate
configuration. Only `--auto-connect` opts into automatic hardware connection for
a newly started core. Attaching never pauses, reconnects or replaces an existing
core. The launcher does not build, install, update or stop another application.

Without Node.js, use the equivalent direct installed command:

```powershell
& "$env:LOCALAPPDATA\Programs\PCController\bin\controller.exe" gui --no-auto
```

The existing no-argument Start-menu/Desktop shortcuts still open the TUI; this
is not evidence that the native GUI has been removed. GUI and TUI are distinct
entry points of the same host. The GUI's console toggle opens the TUI client
against that host, not another serial owner.

## Build and verify

```powershell
.\build.cmd --host-only
.\desktop.ps1 --development

# Prepare a package without replacing the canonical development output:
.\build.cmd --host-only --stage-only
# Pass the reported package directory to --package.

node --test Tools/Build/run-desktop.test.mjs
$env:GOFLAGS='-mod=mod'
node Tools/Build/go-tests.mjs --package cmd/controller --run 'TestDesktop' --retest
$env:GOFLAGS='-mod=vendor'
node Tools/Build/go-tests.mjs --module Tools/NativeDesktop --retest
```

Never run `go test` directly on Windows. The stable runner preserves firewall
identity. Native-window fixtures additionally require
`PCCONTROLLER_DESKTOP_WINDOW_TESTS=1`; `screenshots.cmd` regenerates the offline
README gallery. Build/package success and fixture screenshots are not proof of
physical hardware, audio, multi-DPI, or human acceptance.

An IPC/WebSocket subscription acknowledgement is not a board handshake. The
native header and controls follow the authoritative board snapshot; a lost IPC
stream revokes controls immediately. A locally owned core shutting down also
closes its GUI, rather than leaving a live-looking orphan window.

## Preserve and resume

- Host settings: `%APPDATA%\PCController\config.json` (or `controller config path`).
- Native layout/preferences: `native-desktop.json` beside the selected host
  configuration. Offline preview preferences use `native-preview.json` instead.
- Source and test fixtures: `Tools/NativeDesktop`, the native host adapters and
  their tests are tracked together in this repository.
- Full imported source provenance and the repeatable missing-file audit:
  [donor coverage](NATIVE-DONOR-COVERAGE.md).
- Remaining development/acceptance work, including RTL capture and specialized
  editors: [feature-parity ledger](NATIVE-FEATURE-PARITY.md). Overall parity is
  deliberately still open; do not replace that ledger with a blanket claim.

Do not reset dirty worktrees, remove preservation refs, overwrite configuration,
or replace a running authoring session to recover a launcher. Use the approved
installer/updater for installed packages; retain the source branch and package
identity independently of the installed binary. Archive known generated Web
bundle churn before restoring its tracked baseline after a local build.
