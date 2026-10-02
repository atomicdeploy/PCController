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

## Additional orphaned commit histories

Three older sandbox-owned worktrees retained useful commit histories that were
uploaded to GitHub by object ID but no longer had a live remote branch or pull
request. The Git bundles in `commit-bundles/` preserve their exact commit
objects and original branch tips without importing the old histories into
current `main`.

| Bundle | Tip | Prerequisite | Commits | SHA-256 |
|---|---|---|---:|---|
| `firmware-macro-priority-9265933.bundle` | `92659332caffecfe1d2e328399f3004f489601a8` | complete history | 7 | `3DF51C488468393F0CA22E1D38047192AFD83C8E5A1BA7D8C57D7EB0A856AD15` |
| `macro-host-resume-cc1eb8d.bundle` | `cc1eb8dc64030d24156de839bece7d3a31112c66` | `1895bee5f9a8bc9e68864e00d10c18d77fb5dca8` | 12 | `A5D604CE77AB82543451921E5649A6A80121F431B84E496A38CE4BC64E5CA6CE` |
| `autonomy-storage-docs-f859392.bundle` | `f859392421c546acdb44d9b7824d7af3c6b63c31` | `87b2c17a70e35910f67857477d7d3653b17c334d` | 2 | `20FA572EE4C38C83E0CD5C6A2BBCC2BC84376E06B1C49EE52FBCA84CECC5DC73` |

Each bundle passes `git bundle verify`. All commits in the three preserved
ranges were scanned for strong credential markers, embedded authenticated RTSP
URLs, private-key headers, and private Windows user paths before publication;
no matches were found.

For a prerequisite bundle, fetch its prerequisite from this repository before
fetching the bundle tip. The firmware bundle records a complete standalone
history. Review all recovered work against current requirements before porting
it; these bundles are evidence and handoff artifacts, not merge candidates.
