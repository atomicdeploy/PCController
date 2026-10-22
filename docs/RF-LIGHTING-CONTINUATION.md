# RF and lighting continuation checkpoint

The existing strip-serial-diagnostics branch at a48acbdf79ac0e409c19db2a78b78846bbb8fb27 remains the recovery source checkpoint (PR #697). This branch additionally preserves a previously uncommitted bench trace found during the RF setup pass. The opcode 0xE1 trace has not been validated or newly deployed in this pass. Do not merge or deploy it without reviewing its transport and timing effects. The original working copy is intentionally preserved unchanged.

Live checks: coordinator and Pealayer report a connected board. The four existing board seat mappings are left up/down and right up/down. A bounded first-button capture completed with no new signal; no existing mappings or learning preferences changed. New handset capture and physical motion confirmation remain pending.

The shared effect catalog contains police, white-thunder and converging-red. These entries are catalog evidence only. Next: capture one new handset key at a time, confirm its intended seat action, append matching board assignments without replacing the old handset, read back, then verify press/release and physical movement. After remote acceptance, test rainbow on the currently attached pixel count, verify the three effects individually, and resume MOSFET investigation (#672). Do not infer physical acceptance from software acknowledgements.

Validation for this preservation checkpoint: git diff --check only; no firmware build/upload or hardware stress test. Runtime configuration backups, screenshots and private hardware snapshots remain local and are excluded from Git.

## New handset configuration completed

Four distinct buttons were captured individually and appended to the board catalog: A left seat up, B left seat down, C right seat up, D right seat down. Each mapping was checked through a separate authoritative board read. Stable identities/actions of all original nine entries and all host bindings were compared and preserved. Learning is inactive after the final capture. No app binary or firmware was changed for this setup.

The board reports both seats stopped. Physical direction and release-to-stop confirmation remains the next human step; configuration readback does not establish movement acceptance. The subsequent phase remains the rainbow check, MOSFET investigation and individual checks of police, white-thunder and converging-red. Private RF identities, device settings and snapshots remain excluded from Git.
