## Problem

The machine-level randomized-test guard correctly makes `%LOCALAPPDATA%\PCController\go-noexec-temp` non-executable, but the normal Windows full-build path still attempts to execute a `go run` helper from that denied temporary directory.

Observed on 2026-08-13 with `build.cmd --all` from an exact merged-main worktree:

```text
fork/exec ...\go-noexec-temp\...\controller.exe: Access is denied
```

The failure occurred before any build/device mutation. The guard was deliberately not weakened; deployment used the immutable successful GitHub Actions artifacts instead.

## Required work

- inventory every `go run`/temporary executable in `build.cmd`, `build.sh`, and Node/Go orchestration;
- compile executable helpers to deterministic product-owned paths (with the same cross-worktree lock/cache identity principles as `Tools/Build/go-tests.mjs`) or invoke an already-packaged tool;
- retain the deny-execute ACL for random Go temp output;
- make `build.cmd --all`, host-only, firmware, packaging, and API-generation paths succeed under that policy;
- add a Windows contract test proving the build never executes from `%TEMP%`, `GOTMPDIR`, or a randomized `*.test.exe` path;
- document the distinction between deterministic build helpers and randomized Go test/temp binaries.

## Acceptance evidence

- exact command and source SHA;
- deterministic helper path and hash;
- successful local full build with the no-execute guard still active;
- randomized running `*.test.exe` count remains zero and no repeated Firewall prompt is produced.

This is a build/tooling gap, not authorization to relax the machine guard.
