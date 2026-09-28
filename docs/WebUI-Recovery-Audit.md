# Web UI recovery audit

Tracked by issue #344. A merged-looking or preserved branch is not proof that its
changes exist in the current source or deployed package. Compare actual patches.

| Change | Evidence | Current disposition |
| --- | --- | --- |
| Search shortcut spacing | `f8a86f7`, repeated in `4218f63`, narrows the broad descendant span selector | Recovered; regression test prevents nested key labels growing |
| Dashboard PWM metric | `c086745` on `agent/webui-defects` removes the metric | Recovered without removing the actual PWM controls; no empty metric section for PWM-only boards |
| Compact sidebar | Newer fix from PR #339 | Retain newer square-target implementation; do not replace with older CSS |
| PWM scrollbar | Current CSS already reserves stable scrollbar space and adds minimum-width constraint | Retain; runtime acceptance still needed |
| Column drag controls, header menu and resize visibility | Prior collection changes in `f8a86f7`; current CSS lacks their action classes | Still needs component-level reconciliation and interaction tests |
| Dashboard controls, telemetry filtering, settings and peripheral navigation | Broad patch `4218f63` touches 51 files | Not blanket-imported; reconcile feature by feature with current protocols and tests |

The key-combo fix was absent from current main source, not merely missing from a
browser cache. Its original commit is preserved on older branches but is not an
ancestor of current main. Remote uncommitted work must remain intact during
reconciliation. Do not claim that all historical fixes have been recovered yet.

## Full-branch reconciliation checkpoint

A trial merge of `agent/webui-defects` into current main produced 86 conflicted
paths, including firmware configuration/runtime, protocol and macro contracts,
host configuration/control, generated bundles and Web UI components. It is kept
separate from the deployable source. Do not resolve these by choosing one side
wholesale or deploy conflict-marked code.

The original unfinished working copy also has three distinct work groups:

| Work group | Files / assignment for resumption |
| --- | --- |
| Windows self-update | `selfupdate.go`, `selfupdate_windows.go`, `selfupdate_process_windows_test.go`: finish and verify interactive process handling |
| Stable Go test runner | `AGENTS.md`, build README, `build.test.mjs`, `go-tests.mjs`: reconcile runner changes with current main and run build-tool tests |
| Host bridge tests | manager, webhook delivery and wire interop tests plus untracked `test_helpers_test.go`: preserve helper, finish and run focused tests |

Tracked and untracked files were copied into a private checkpoint without
modifying the original index or worktree. These are preserved, not declared
finished or deployed. Issue #344 remains the public coordination point.
