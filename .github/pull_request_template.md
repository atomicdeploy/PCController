## Outcome

<!-- Lead with the user-visible result and link the issue or sub-issue. -->

Closes #
Relates to #

## Scope

- Firmware:
- Windows host / TUI:
- Protocol / simulator:
- Documentation / tooling:

## Coordination

<!-- State the base used, related WIP/PRs reviewed, claimed lane, and conflict disposition. -->

## Verification

- [ ] Relevant Go tests pass.
- [ ] `go vet` passes for affected host packages.
- [ ] VirtualBoard CMake build and CTest pass when native code or protocol behavior changed.
- [ ] Build/Firmware Node tooling checks pass when affected.
- [ ] Protocol compatibility and failure paths were exercised.
- [ ] Physical board behavior was verified, or the exact pending human/hardware check is stated.

## Embedded resource impact

<!-- Required for firmware changes. Use exact final numbers from the build manifest. -->

- Flash before / after / delta:
- Static SRAM before / after / modeled stack reserve:
- EEPROM before / after / migration:
- Timing or interrupt impact:

## Safety and rollback

- [ ] Relay, motion, PWM, illumination, buzzer, reset, and programming effects were considered.
- [ ] Backup/rollback artifacts or a recovery procedure exist when firmware or EEPROM changes.
- [ ] No secrets, generated binaries, device backups, or private configuration were committed.

## Evidence

<!-- Paste concise command results, linked short commit IDs, screenshots, or hardware observations. Keep full hashes in integrity manifests/machine evidence. -->

## Generated content

<!-- Name the canonical generator and tracked outputs changed together, or state that none changed. -->

## Handoff

<!-- For WIP only: exact blocker, owner, and next command/test. Remove when complete. -->
