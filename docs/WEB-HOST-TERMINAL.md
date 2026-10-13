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
