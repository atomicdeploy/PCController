# Web UI recovery audit

Tracked by issue #344. A merged-looking or preserved branch is not proof that its
changes exist in the current source or deployed package. Compare actual patches.

| Change | Evidence | Current disposition |
| --- | --- | --- |
| Search shortcut spacing | `f8a86f7`, repeated in `4218f63`, narrows the broad descendant span selector | Recovered; regression test prevents nested key labels growing |
| Compact sidebar | Newer fix from PR #339 | Retain newer square-target implementation; do not replace with older CSS |
| PWM scrollbar | Current CSS already reserves stable scrollbar space and adds minimum-width constraint | Retain; runtime acceptance still needed |
| Column drag controls, header menu and resize visibility | Prior collection changes in `f8a86f7`; current CSS lacks their action classes | Still needs component-level reconciliation and interaction tests |
| Dashboard controls, telemetry filtering, settings and peripheral navigation | Broad patch `4218f63` touches 51 files | Not blanket-imported; reconcile feature by feature with current protocols and tests |

The key-combo fix was absent from current main source, not merely missing from a
browser cache. Its original commit is preserved on older branches but is not an
ancestor of current main. Remote uncommitted work must remain intact during
reconciliation. Do not claim that all historical fixes have been recovered yet.
