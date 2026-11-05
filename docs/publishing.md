# Repository verification and publishing

The intended upstream is **public `AtomicDeploy/RayanLamp`**. The Go module
path remains `rayanlamp.local/app`; it is an internal import namespace and
does not require that domain to host a Go module.

## Before pushing

```powershell
node .\tools\verify-history.mjs
git fsck --full
git status --short
.\tools\build.ps1 -Edition develop -Force
```

The canonical build runs the frontend checks, Go vet/tests, resource generation,
and PE verification. A failed or skipped suite must be reported as such; do not
turn test skipping into the release default. See [building](building.md) for
the Linux cross-build and production edition. The vendored Go tree includes
local Walk fixes: do not replace it with `go mod vendor` without preserving and
reviewing those patches. The bundled `internal/webui/dist` is intentional.

Keep `.gitignore` exclusions: build outputs, local work, test executables,
temporary resource objects, node_modules, raw conversations, and private
recovery files. `assets/fonts/yekanbakh-vf.ttf` has the separately documented
[redistribution authorization](attribution.md#yekanbakh-variable-font), not an
MIT font license.

## First publication

Run from a machine/session with authenticated GitHub CLI and Git network access:

```powershell
.\tools\publish.ps1
```

This requires the authenticated login to be `AtomicDeploy`, creates the public
repository if it is absent, validates any existing `origin`, and pushes only
`main` with normal fast-forward semantics. It never force-pushes, rewrites a
remote history, publishes a binary, or fabricates a release. An existing remote
README/root commit requires explicit reconciliation; the script stops instead
of overwriting it. GitHub Actions runs the Windows and Linux cross-build matrix
after a successful push. Check its result before declaring CI green.

An offline `git bundle create RayanLamp.bundle --all` preserves the complete
local history for transfer. A bundle is a transport artifact, not proof of a
successful GitHub push.
