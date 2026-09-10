# Contextual UI and no narration

This is the standing design contract for all Digitalogic products, ecosystem applications and interfaces, including PCController. Apply it to new work and to related surfaces when fixing existing work; do not wait for each individual control to be reported.

## State determines meaning and availability

Use contextual UI, state-driven rendering, progressive disclosure, semantic actions and conditional affordances. Derive information, wording and operations from the actual object, authoritative current state, available capabilities, applicable permissions and user intent. Never make the user resolve a state that the application already knows or perform UI bookkeeping for the computer.

If an element has no current utility and teaches nothing important, remove it. If its presence communicates a useful capability, restriction, state or next step, retain it and represent that state truthfully. This is not a blanket ban on disabled controls: a disabled Publish action with three real validation errors can be informative; View RAW with no RAW files is not.

Prefer direct manipulation and routinely useful inputs over unnecessary reveal steps. Disclose secondary actions when their context exists. Choose field actions by meaning rather than giving every field the same generic action.

| Current context | Expected interface |
| --- | --- |
| No clearable filters | No Clear filters action |
| Zero selected objects | No irrelevant bulk-operation controls |
| Selected object | Deselect, not Select / Deselect |
| Zero RAW files | No View RAW action |
| Editable filename | Rename where useful, not a mechanical Copy action |
| Routinely edited note | Editor directly available; save/cancel only for actual edits where that workflow requires them |
| Already assigned object | Current assignment and appropriate change action, not wording implying it is unassigned |
| Missing field value | No action on a nonexistent value |
| Restricted action | Omit it unless explaining the restriction or next step is useful |

Do not invent permissions, capabilities or successful state. Preserve the project's existing authentication and safety contracts; this policy does not introduce authentication into products where it is explicitly out of scope.

## Keep structure stable

Let state determine meaning and availability; let usability determine whether a state change alters layout. Preserve predictable action areas, focus, keyboard navigation and pointer targets. Avoid jumping toolbars, unexpected menu reordering and controls appearing under the pointer. Cover loading, empty, selected, mixed, dirty, pending, failed and recovered states where applicable. Do not conflate unknown/loading data with zero or replay a mutation to repair stale presentation.

## No narration

Architecture, caching, synchronization contracts, API/database mechanics, fallback strategies and acceptance criteria belong in code, tests and developer documentation. Do not convert them into permanent helper text, subtitles, cards, badges, banners or tooltips.

Normal expected behavior should be experienced. UI copy must add information that cannot already be reasonably inferred from the visible interface. Explain actual exceptions, ambiguity, consequences, actionable errors or genuinely non-obvious behavior when needed for a decision or recovery.

A visible renamed filename is usually sufficient feedback. Avoid persistent success prose and tooltips that merely repeat visible labels. Keep accessible names and useful explanations for unfamiliar icons. Real conflicts, partial failures and consequential bulk changes deserve precise contextual feedback.

## Implementation and review

Model the interaction as a state system, not a collection of accumulated controls. Apply each discovered principle across related fields, menus, toolbars, dialogs and views. Shared components must allow semantic actions and state-specific labels rather than imposing generic behavior.

For UI changes, verify relevant transitions, authoritative object binding, applicable permissions, keyboard/focus behavior and stable layout. Inspect real rendered results in supported themes and narrow layouts. Documentation or a screenshot of one state alone does not prove runtime acceptance.

Shorthands: **Contextual** means truthful state-aware semantics with stable usable structure. **No narration** means no specification leakage or explanations of obvious interface mechanics.
