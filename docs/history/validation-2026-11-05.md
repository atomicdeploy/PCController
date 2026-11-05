# Current-tree validation — 2026-11-05

## Source and history

The complete current-source reconciliation is commit
`abfd5a39f58c9e137009c350d61689b5824449f5`, after 43 recovered source checkpoints.
This receipt is a later documentation-only commit; no application code was
modified during repository preparation.

- `node tools/verify-history.mjs`: passed, 43 checkpoints, 851 net path changes,
  837 distinct source blobs verified against recorded SHA-256 hashes.
- `git fsck --full`: passed.
- Source export: `git archive` of the reconciliation commit, unpacked into a
  new directory; all 2,427 tracked files matched the working source after
  documented text newline normalization. No untracked source was copied in.
- No build binaries, test executables, resource objects, work folders,
  node_modules, raw JSONL logs, or private recovery JSON were found among the
  tracked files or any historical path.
- Relative links in the README and project documentation resolved.
- Existing assets, vendored dependencies/local Walk modifications, frontend
  lockfile and generated embedded Web distribution are included.

The 43 checkpoints are deliberately incomplete recovery evidence, not 43
certified historical releases. See [method and limits](README.md).

## Build and tests

From the Git-only source export, with an existing offline Go tool/module cache:

```powershell
go vet -tags rayanlamp_develop ./...
go test -tags rayanlamp_develop -count=1 ./...
.\tools\build.ps1 -Edition develop `
  -Commit abfd5a39f58c9e137009c350d61689b5824449f5 `
  -SkipWebBuild -SkipTests -Force
```

- Go vet passed, both independently and inside the canonical build.
- The full Go test command was attempted: **40 packages passed; seven failed**.
  The failures are in `appcore`, `commands`, `headless`, `lamp`, `localapi`,
  `tui`, and `tools/mock-lamp`, where loopback socket access is denied by the
  execution sandbox (and dependent state-wait assertions time out). The full
  suite is **not green** and must be rerun in an unrestricted local/CI environment.
  Tests were not changed or weakened to hide these failures.
- The canonical develop build passed with `CGO_ENABLED=0`, using the tracked
  embedded frontend. PE validation passed for architecture, console subsystem,
  product icon, manifest, VERSIONINFO, debug data, and embedded Web edition.
- Output: Windows/amd64 `RayanLamp.exe`, 41,675,264 bytes.
- SHA-256: `F6525740A62D5CE474C27D2C4E2706067B66B4791642290D3E11AEC3F79BA003`.
- Executed `RayanLamp.exe cli --version` and `cli --help`: both exited zero
  and returned the expected text; version output identified the exact
  reconciliation commit above. No lamp commands were sent.
- TypeScript checking and **193 frontend tests in 23 files passed** against
  the matching working source. Existing installed dependencies were reused;
  all 14 declared direct/development versions matched `package.json`.
  This was not a fresh `npm ci` or a new frontend build in the exported directory.
- All top-level PowerShell scripts parsed. `tools/test-terminal.ps1` passed
  its VT100/reset/fallback/NO_COLOR checks.
- Nine publication-helper mock scenarios passed: existing/new repository,
  dirty tree, wrong branch/account, private repository, wrong origin, rejected
  push, and remote-SHA mismatch. Mocks made no external calls or remote changes.

Native screenshot results from the preceding regression pass remain in the
[regression audit](../regression-audit-2026-11-05.md). This repository task did
not repeat interactive GUI/TUI, physical device, multi-monitor, Linux-host,
production-edition, or GitHub Actions acceptance.

## Publication status

**Not pushed.** Intended origin is `https://github.com/AtomicDeploy/RayanLamp.git`.
GitHub access through both the original and the user-specified proxy was denied
by Windows socket access permissions in this session. The connected repository
lookup returned 404; the connector cannot create repositories. The owner then
requested a local completion and a publishing command instead.

Run [the publication helper](../publishing.md) from an authenticated,
network-enabled shell. It creates the public destination if necessary and
checks the remote SHA after a normal push. No GitHub CI result is claimed here.

Private build/test logs and the verified source-export executable remain in
the enclosing workspace's `work/history-recovery` directory, outside public Git.
