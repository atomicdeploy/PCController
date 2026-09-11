# September 11 reconciliation and delivery

GitHub is the shared coordination record. Use short commit identifiers in prose,
retain recovery branches, and distinguish merged source, installed packages,
running processes, and physical acceptance. Do not publish credentials or private
machine inventories. Owners should reply on coordination issue #327
with their branch, uncommitted work, verified behavior, blockers and successor.

## Recovered work

| Work | Disposition |
| --- | --- |
| Updater and bridge fixture checkpoint #328 | Updater already merged via #267; missing test isolation recovered and merged in #335. Original branch preserved. |
| Artifact test timeout #330 | Exact patch already merged through #268; closed with provenance. |
| Navigation replay #331 | Anti-rollback invariant already covered by #273; current idempotent reply preserved instead of restoring obsolete rejection. |
| EEPROM validation #329 | Current CRC/commit/per-cell safety tests pass. Sanitized UART bulk-copy optimization and feature-size measurements remain unfinished. |
| Historical Linux/UI aggregate #132 | Contains work absent from later main; icon/relay/PWM repairs delivered in #325. Broader unique slices still require decomposition, not wholesale replay. |

Nineteen named historical branch heads and nine dirty-worktree snapshots were
verified preserved. A checkpoint coordinator actually acknowledged a handoff-only
Codex CLI resume; individual domain-owner replies are not fresh verification.

## Runtime findings

| Finding | Result / next step |
| --- | --- |
| Letters instead of logo; decorative relay knob; PWM scrollbar overlap | #325 merged. Existing browser automatically loaded the replacement bundle; real SVG loaded, eight knob buttons and stable scrollbar gutter observed. Physical relay actuation not implied by DOM evidence. |
| Connected serial owner reported unknown | #332/#334 uses the active session as direct ownership evidence. Disconnected OS errors remain visible; stale lookup results are discarded. Live owner PID matched the running host. |
| Desktop shortcut absent | #336/#337 adds actual Desktop KnownFolder support, independent readiness and owned-link repair/removal. Existing installer only created a Start Menu link. Taskbar pin is not inferred from link creation. |
| Top-level `program abandon` rejected | #334 routes it through the same authenticated, durable recovery engine as `program recover`. Exact target hash and confirmation checks unchanged. |
| Initial writer / independent UART readback timeouts | #319 remains open. #333 explicit re-entry candidate passed unit tests but failed live qualification; production activation was reverted and source preserved as Draft. No successful guarded-upload claim. |
| LCD absent | Still not detected. Episode-level warning suppression is merged; software suppression is not an electrical repair. |

Firmware CI679 was installed and a separate recovery read verified 32,258 written
bytes, including explicit validation of Urboot vector redirection. Later candidate
testing failed bootloader entry/readback. Existing recovery restored settings and
cleared the latch without another flash. This does not certify physical RGB
smoothness, front-panel gestures, MOSFET wiring or macro timing under real loads.

## Build capability

Two Windows machines completed `build.cmd --host-only --skip-tests` package
benchmarks in **112.301 s** and **68.017 s**. Both included Web typecheck/build,
native resource verification, UPX validation and C ABI smoke. They excluded the
Go/Web test suites, vet and embedded firmware defaults; those benchmark packages
were not deployment artifacts. Dependency acquisition time is excluded.

Toolchain gaps tracked in #60: discover real App Installer when its alias is
missing, handle policy-restricted command-line proxies without weakening policy,
and install WinLibs under a space-free managed path when its linker requires it.

## Remaining acceptance

- Finish and prove bounded UART bootloader entry/re-entry; retain byte verification.
- Verify taskbar pin through a working native UI path; do not claim a pin merely
  because a `.lnk` exists. Desktop automation encountered an unsupported interface.
- Complete the #329 optimization/resource audit and broader historical feature
  recovery; original branches and issue links are the handoff, not discarded WIP.
- Validate physical input/output behavior with a known-safe connected load and
  report electrical limitations separately from software findings.
