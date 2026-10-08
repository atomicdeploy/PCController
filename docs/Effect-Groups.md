# Effect groups

Groups are PCController-owned records in the host configuration's `effect_groups`.
They can exist without effects and are included in the live `effect_groups`
snapshot array (`name`, `icon`) for all consumers. Existing effect membership also
contributes groups to discovery; creating a case-insensitive duplicate is rejected.

```
effect group list
effect group create "Cinema lighting" lamp
effect group update "Cinema lighting" Lighting lamp
```

Creation never creates a placeholder effect. Updating an empty group is allowed;
renaming a populated group updates all its effects. Renaming over another group
is rejected rather than silently combining records. `effect export` / `effect
import` preserve empty groups and their icons in the `groups` document field.

Pealayer displays these records as **Group**, with a selection-only dropdown and
a separate **New...** action. Its `controller_effect.group.create` RPC forwards
the operation to PCController; it does not maintain a private group store.

For updates without replacing a running host package during compilation, use
`build.cmd --host-only --stage-only` and send the staged executable through the
running primary's `controller update host FILE` operation. The primary owns
graceful shutdown, replacement, restart and acknowledgement.
