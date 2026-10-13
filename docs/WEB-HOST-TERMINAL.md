# Web host terminal presentation

`controller web` now presents the full existing TUI in an interactive terminal,
including when `--no-open --no-tray` suppresses browser/tray surfaces. Those flags
are not a terminal-disable policy. The TUI attaches to the already-owned primary
over the current IPC/event contract: it cannot acquire another UART or create
another board coordinator.

`--no-tui` explicitly disables it. Noninteractive redirected streams and embedded
service hosts also cannot run an interactive TUI. These paths hide only the
process's sole-owned classic Windows console using existing pure-Go
`GetConsoleWindow` / `GetConsoleProcessList` / `ShowWindow(SW_HIDE)` calls. No
cmdow, shell helper, arbitrary window hiding or shared terminal hiding is used.
The startup splash appears immediately for terminal Web hosts; one bounded
hide recheck remains for an updater still sharing its console during health
commit. Exiting/failing the TUI ends the foreground host rather than leaving a
blank visible host window. Service/redirected hosts remain background services.

New startup-mode fixtures cover explicit no-TUI, false overrides and preservation
of terminal UI with browser/tray disabled. Go vet passes; tests/CI were skipped
at the user's request. Packaging and live host identities are separate delivery
evidence; no board actions or programming were used for presentation checks.

The stage-only Windows package completed in 44.6 seconds from source topic
`6fb26090` with source digest
`b8f39b956baa62f53a489569f3d3977192ef4ea56ea5ca3f40c4b5f5f11e7242`.
Candidate executable SHA-256:
`d4204a0754740a12285d993f51448202aa74807c620909e8c6ac69a198e10e8c`.
Web typecheck/build/integrity, Win32 resources and existing programmer adapter
checks passed. This pass skipped tests, package vet (narrow vet had already
passed separately), UPX and C-ABI library rebuilding; it did not remove existing
C-ABI implementations or flash the embedded firmware/default EEPROM images.

The generated Web build was preserved in a scoped named Git stash
`generated-web-output-web-host-terminal-package`; package output remains in the
product-owned `.build/package` directory. It is not authored source for a new
generated-only PR. No running controller was restarted/replaced for this pass.
Thus the TUI's production visibility is not yet live acceptance.

[PR #662](https://github.com/atomicdeploy/PCController/pull/662) is stacked on
the frozen mobile UI #661 and retains WS #660. Do not reset or overwrite these
prerequisites to flatten the source history. Runtime activation must use the
verified host's ordinary updater and preserve its coordinator/mirroring state.

## Required checks and Cafe activation follow-up

The user subsequently authorized required merge checks. The integrated topic
retains the open controller/mobile and dependency branches rather than replacing
their implementations. Required checks exposed a duplicated PWM deadline helper,
obsolete unconfigured-board/TUI assertions and a missing environment bootstrap
in the isolated UDP example; these have focused repairs. The mobile owner also
reproduced and repaired the narrow RTL connection-header overflow and updated
two obsolete assertions, with 26 focused checks and a clean analyzer.

Cafe's primary-owned host updater acknowledged candidate source `352e6371`
after restarting in the existing interactive session. Its executable SHA-256 is
`8c22bdc95caa50f8d584327b46da97c44a23184fafc09df0958946c2340c2636`.
Pealayer's integrated `5b3d5c1` package was separately accepted by its own updater
with Cafe's retained libmpv, and Cafe acquired publishing authority while paused.
Seven controller effects and a ready, error-free empty hardware plan were read
back. This is authoring readiness, not physical playback or CPU acceptance.

Paused visible CPU remains high. Explicit `POST /api/diagnostics/cpu-profile`
provides one five-second application-only Go profile to loopback callers;
existing Origin/authentication checks still apply. It accepts no arbitrary file,
URL, duration or board action, stores nothing itself and exposes no public pprof
listener. Use the private response with `go tool pprof`; do not publish profiles
or enable OS profiling policies to work around their denial. Profiling is not
automatic and does not change telemetry or audio cadence.

The full AVR check discovered overlap with the reserved firmware identity block.
Semantics-preserving wire-cursor and byte-damping/PWM-expansion improvements are
checked against the shared native firmware, including all 65,536 damping pairs
and all 256 calibrated byte expansions. The full default AVR image now builds
and passes the footprint/stack guards with 50 flash bytes and an estimated
231 SRAM bytes free (96 required). All 24 native firmware checks pass. No board
was flashed, identity moved into bootloader space or feature removed; the
fresh Linux/Windows required merge checks still need to confirm this tree.
