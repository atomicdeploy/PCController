# Immediate styled TUI startup

The former splash was literal unstyled text emitted at `main` entry, before
Bubble Tea initialized terminal colors. It did not use the TUI palette, and
classic Windows conhost did not yet have VT processing enabled. The remote
attachment path also printed another unstyled connecting caption and performed
independent snapshot/catalog and UI/RF preference reads sequentially before the
first TUI frame.

The immediate startup frame now shares the TUI's cyan/violet palette and rounded
panel geometry, with a fixed-slot chip emblem and terminal identity. Rendering is
pure and does not wait for settings, network, UART, welcome audio or TUI setup.
It fits the current terminal width, collapses on narrow terminals, sanitizes
caption controls, and never claims a percentage or connected board. Explicit
interactive presentation uses a private true-color renderer; ambient build-log
`NO_COLOR`/`TERM=dumb` does not make it white. Classic Windows VT processing is
enabled before the first frame. Redirected CLI output, helper processes and
explicit `web --no-tui` remain free of the splash.

The duplicate remote connecting text is removed. Independent snapshot/catalog
reads run concurrently under the existing eight-second context; UI/RF settings
reads run concurrently under the existing four-second context. Authentication,
ownership, navigation event-cursor handshake, runtime safety, original errors
and cancellation remain required. Board connection is still managed by the TUI's
existing asynchronous lifecycle; startup decoration never opens or actuates it.
There is no animation worker or minimum splash delay to race the TUI or keep it
waiting. Bubble Tea replaces the frame through its normal alternate-screen handoff.

Focused tests cover width bounds, ANSI colors, fixed chip slots, sanitized copy,
redirected-output silence, parallel read barriers, cancellation and failure
retention. `tui-preview --page startup` renders the actual presentation without
hardware access. A rendered preview is not a screenshot of the production console
or a measured cold-launch timing claim.
