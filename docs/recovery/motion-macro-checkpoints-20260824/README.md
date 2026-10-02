# Motion and macro recovery checkpoints

This directory preserves two historical August 2026 checkpoints that were once
published as temporary recovery branches. Those branch refs were later removed,
while the underlying commits remained available. The patches make the work
durable and reviewable from a branch based on current `main` without importing
the obsolete branch histories.

These patches are preservation evidence, not merge-ready changes. Reconcile
their behavior against current `main`, issue #44, and issue #554 before porting
any implementation.

## Firmware motion and macro checkpoint

- Original commit: `3caedc92f20a8b1544583c643365fa826bcb93ad`
- Original parent: `c4fa2521b763b547269daaf91a74cc3d5a316255`
- Patch: `0001-checkpoint-preserve-firmware-motion-macro-work.patch`
- Patch SHA-256: `9E168EAE11EBC1FA096DF3012BC42AE2375655E625491B8034B37C7A0223D460`
- Recovery verification: all 29 changed paths still present in the orphaned
  Windows worktree matched this commit byte-for-byte after Git clean filters.
  The thirtieth diff path is the source side of a recorded rename and is fully
  represented by the patch.

## Interrupted host integration checkpoint

- Original commit: `a2e62f38cfe21de9c0c86ff564dfc824d4699ec9`
- Original parent: `1895bee5f9a8bc9e68864e00d10c18d77fb5dca8`
- Patch: `0001-checkpoint-preserve-interrupted-motion-macro-integra.patch`
- Patch SHA-256: `04C7996B6196A775BAC3F42B3AD33A1867FFDF5107078E7B88BC7539C83CE7BF`
- Recovery note: this independent 45-path snapshot intentionally preserves an
  interrupted integration state and may include obsolete or conflicting design
  choices. Review and port coherent slices rather than applying it wholesale.

To reconstruct either original commit for analysis, create an isolated worktree
at its recorded parent and apply the corresponding patch with `git am --3way`.
Do not apply these patches over a dirty checkout.
