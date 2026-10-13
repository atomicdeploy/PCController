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
