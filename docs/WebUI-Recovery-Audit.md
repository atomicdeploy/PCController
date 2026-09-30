# Web UI recovery audit

Tracked by issues #344, #490, and #101. A merged-looking or preserved branch is
not proof that its changes exist in the current source or deployed package.
Compare actual patches, source behavior, tests, and generated artifacts.

| Change | Evidence | Current disposition |
| --- | --- | --- |
| Search shortcut spacing | `f8a86f7`, repeated in `4218f63`, narrows the broad descendant span selector | Recovered; regression test prevents nested key labels growing |
| Dashboard PWM metric | `c086745` on `agent/webui-defects` removes the metric | Recovered without removing the actual PWM controls; no empty metric section for PWM-only boards |
| Compact sidebar | Newer fix from PR #339 | Retain newer square-target implementation; do not replace with older CSS |
| PWM scrollbar | Current CSS already reserves stable scrollbar space and adds minimum-width constraint | Retain; runtime acceptance still needed |
| Column drag controls, header menu and resize visibility | Prior collection changes in `f8a86f7`; recovered through PR #368 and subsequent typed-collection tests | Recovered on the current typed-collection implementation |
| Dashboard controls, telemetry filtering, settings and peripheral navigation | Broad patch `4218f63` touches 51 files | Reconciled feature by feature through PRs #423, #439, #443, #444, and later current-main tests; do not restore the stale fixed catalogs or optimistic state from the snapshot |

The key-combo fix was absent from main when this audit began, not merely missing
from a browser cache. It was recovered through PR #345. Remote uncommitted work
must remain intact during reconciliation, and open product acceptance in #101,
#159, #160, #161, and #163 is not made complete merely by preserving history.

## Full-branch reconciliation

The original functional change is `c086745ee5e4c0b5ab50412fe471560d47b6e08b`
and its whitespace-only follow-up is
`f94aa2d69d1185890fafc62b4e7d8654cb000407`. The later exact working-copy
snapshot `4c94f9b490cfd2b2ce4d8b42717b7de32f0983ac` contains 1,856 of 1,870
nontrivial added source markers from `c086745e` byte-for-byte. The remaining 14
markers are accounted for by the `Project/Firmware` to `Project/Runtime` source
move. That snapshot became reachable from main through PR #483.

The original `f94aa2d` lineage is now also joined to current main by an ordinary
merge. Its conflicts were reviewed by subsystem against the already reconciled
snapshot and current contracts:

- current runtime, protocol, host, and TUI implementations supersede the old
  pre-contract versions;
- current Web modules retain the card layout, contextual actions, telemetry,
  dialog, terminal, notification, and update behavior under newer APIs and
  regression tests;
- retired duplicate modules and unhashed generated assets remain deleted;
- the current content-hashed embedded bundle remains the sole generated output.

The merge deliberately has no runtime tree delta apart from this audit. Its
purpose is to make the exact source lineage reachable without reintroducing
fixed catalogs, optimistic hardware state, retired protocol generations, or
obsolete generated bundles. Product behavior still requires its own source,
test, and runtime acceptance; ancestry alone is never treated as completion.

The original unfinished working copy also has three distinct work groups:

| Work group | Files / assignment for resumption |
| --- | --- |
| Windows self-update | `selfupdate.go`, `selfupdate_windows.go`, `selfupdate_process_windows_test.go`: finish and verify interactive process handling |
| Stable Go test runner | `AGENTS.md`, build README, `build.test.mjs`, `go-tests.mjs`: reconcile runner changes with current main and run build-tool tests |
| Host bridge tests | manager, webhook delivery and wire interop tests plus untracked `test_helpers_test.go`: preserve helper, finish and run focused tests |

The original dirty working-copy snapshot remains preserved by `4c94f9b4` and PR
#483. The self-update, stable-runner, and host-bridge groups have independent
history and must be compared against their focused PRs before any replay; they
must not be inferred complete from this Web reconciliation. Issue #490 is the
consolidation ledger, while #101 and its linked area issues remain the source of
truth for unfinished Web product acceptance.
