# Windows startup console and TUI first paint

Cafe's reported blank window was a Web host, not a running terminal UI:
`controller.exe web --no-open --no-tray`, interactive session 4. The background
primary survived with a visible classic console after the host updater restarted
a console-subsystem executable. No TUI renderer existed to fill that window.

The first candidate's live diagnostics found that this explicit background host
also inherited a console stream. A redirected-stream-only guard therefore
preserved the blank window. The corrected rule permits hiding an explicitly
UI-less Web host's own window only when the current process is its sole owner.
Ordinary Web commands with interactive streams, shared terminals, TUI commands
and machine-readable invocations retain their presentation. No arbitrary-window
lookup, console detachment, process termination or board command is introduced.

Windows updater helpers and replacements use CREATE_NO_WINDOW for background
work without combining conflicting CREATE_NEW_CONSOLE/DETACHED_PROCESS flags.
Interactive terminal I/O and the replacement's normal Ctrl+C behavior remain
preserved. A bounded, one-shot 12-second startup recheck handles an old helper
sharing the console until its 10-second health commit; no repeating poll is added.
The bridge diagnostic read reports current-process console state freshly.

Actual TUI invocations print the product title and startup text immediately,
before environment/configuration and transport initialization. Remote TUI setup
adds a connecting line before the initial IPC handshake and settings reads.
Both are restricted to real terminal output; JSON/version/helper output is not
contaminated. No fabricated percentage or board readiness is displayed. This
does not claim the startup handshakes have become instantaneous.

Pure regressions cover command classification, explicit background flags,
startup text/pipe omission and Windows replacement presentation. Tests and CI
are intentionally not run under the user's standing instruction. Staged builds
still perform TypeScript, fresh Web bundling, native compilation, C ABI smoke
and package integrity checks. The build now reuses npm's installed lock graph
only when declarations, lock metadata, installed versions and package-directory
timestamps match; invalid caches still use npm ci. This avoids destructive
dependency reinstall churn without reusing an old Web bundle.

Delivery, exact package identity and remaining visual acceptance are recorded
below after live verification. Concurrent mobile edits remain outside this fix.
