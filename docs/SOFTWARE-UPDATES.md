# Mutually routed software updates

The primary controller retains immutable artifacts, explicit authorization,
idempotent host transactions and its own restart/rollback owner. Pealayer retains
its own updater, draft protections and host-specific media runtime. Neither
gateway replaces the other product directly or treats upload acceptance as a
healthy installed result.

`controller.software.targets` lists the primary executable owner and live native
Pealayer instances with usable registered endpoints. `controller.software.call`
requires an explicit target ID, current revision, `authorized: true` and a
software-only nested request. Remote calls require programming and bridge-call
capabilities. The existing bridge can carry this narrow operation, not arbitrary
third-peer HTTP, board actions or another relay. Target changes/expiry and
redirects fail closed; uncertain mutations are not replayed.

Self operations reuse `controller.artifact.*` and `controller.update.host/status`.
Player operations map to the existing Pealayer HTTP updater so an older player
can be updated before exposing update RPC. Raw artifact pulls use bounded
`controller.artifact.download.begin/chunk/close` sessions; verified files are
pinned once, quota/TTL limited and closed through the existing service lifecycle.
Windows staging/helper/backup/health files are hidden; an installed or rolled-back
executable is visible.

`controller.software.bundle.list/read` publishes explicitly packaged candidates
from `software-bundle.json` beside the executable. The shared format is
`digitalogic-software-bundle`, with at most 16 `{id, path, manifest}` entries.
Manifests identify `service` (`pccontroller` or `pealayer`), `sha256`, `size` and
target platform/runtime metadata. Relative paths are confined through `os.Root`;
public listings omit them, and reads verify bytes before serving bounded chunks.
Catalogs do not trigger installation or scan unrelated directories.

Use the single [reference transfer client and full contract](https://github.com/ToghrolTP/pealayer/blob/feat/mutual-software-updates/docs/SOFTWARE-UPDATES.md)
for file push, verified pull and streaming peer-to-peer copy. Through Pealayer's
reachable port its connected controller can be updated; through this controller's
reachable port its registered player can be updated. Existing artifact methods
bootstrap an older controller, and the existing player HTTP contract bootstraps
an older player. First update an older reachable gateway itself when necessary;
do not add version-specific schemas or weaken bridge guards.

Continues the transfer/terminal-truth work in #252 and atomic-artifact concerns
in #562. Keep device, service restart, package, live cross-product and remote
deployment acceptance separate from source/mock tests. Concurrent native desktop
work is preserved, not included in this update slice.
