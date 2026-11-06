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

## Windows service ownership

Host staging is not terminal completion. The durable operation remains pending
across restart until its exact private helper transaction commits or rolls back.
Pending host activation rejects overlapping updates rather than launching a
second helper or queuing hardware writes during shutdown. Historical results
remain valid after later upgrades. Identical images report current without
staging, helper launch or restart.

The installed service now uses the same verified replacement/rollback engine,
with SCM stop/start instead of launching a second interactive host. Preparation
checks the fixed service name, configured executable, current SCM PID and needed
permissions before staging. Only that SCM-owned replacement can acknowledge its
private health journal after startup survival. Normal account, arguments,
startup and recovery configuration remain unchanged. A failed candidate exits
cleanly to SCM while the helper owns rollback, avoiding a crash-recovery race.

Administrative `service install` / `service repair` grants only the dedicated
service SID start/stop/query on itself and modify access in its installation
directory. It does not grant SCM administration or service configuration access.
The update RPC never elevates privileges or repairs an installation. An older
service that explicitly disables self-update needs one administrative upgrade
and service repair before it can participate. It cannot be bootstrapped through
an API it never implemented. `service install --no-auto` keeps hardware discovery
paused for isolated verification and preserves this choice across restarts.

The opt-in `TestSoftwareGatewayLivePealayerUpdate` exercise replaced a real,
isolated older HTTP-only player through the controller gateway, then verified
the exact running digest and healthy replacement. Service lifecycle tests cover
healthy commit, health timeout rollback and startup failure restoration; these
are not evidence of an actual production SCM restart.

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
