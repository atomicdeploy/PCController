# PR #592 motion/macro checkpoint reconciliation

This ledger records the feature-by-feature reconciliation of preservation PR
#592 into delivery PR #588. It is evidence,
not a request to merge or close either pull request.

## Preserved source identities

| Artifact lineage | Exact tip | Role |
|---|---|---|
| Firmware motion checkpoint | `3caedc92f20a8b1544583c643365fa826bcb93ad` | Early full-peripheral motion/macro integration |
| Firmware scheduling bundle | `92659332caffecfe1d2e328399f3004f489601a8` | Cooperative firmware experiments and macro-before-UART priority |
| Interrupted host checkpoint | `a2e62f38cfe21de9c0c86ff564dfc824d4699ec9` | Lossless unresolved integration snapshot |
| Host recovery bundle | `cc1eb8dc64030d24156de839bece7d3a31112c66` | Board capture, macro contract and programming experiments |
| Autonomy/storage documentation | `f859392421c546acdb44d9b7824d7af3c6b63c31` | Architecture and tracker context |

## Retained behavior

| Historical behavior | Current implementation/evidence |
|---|---|
| MCU-timed bounded queue and ordinary-opcode dispatch | `Project/Core/MacroRing.*`, `Project/MacroQueue.*`, and `Tools/VirtualBoard/tests/macro_*` |
| Macro dispatch before ordinary UART backlog | `Project/Runtime/LifecycleRuntime.inc.h`; the due action is serviced before `appProtocol.service()` |
| Exact host compilation, refill, timing, cancellation and generation binding | `Tools/Controller/internal/control/macros.go`, `macro_*_test.go`, and runtime generation guards |
| Board-origin relay capture and retained-ring import | `Project/MacroQueue.*`, `Tools/Controller/internal/control/macro_capture.go`, and focused capture tests |
| One canonical opcode vocabulary | `Project/ProtocolContract.json`, generated C++/Go contracts and protocol contract tests |
| VirtualBoard macro parity | `Tools/VirtualBoard/src/virtual_board.cpp` and `Tools/VirtualBoard/tests/test_main.cpp` |
| Reset-atomic guarded programming and persistence wait | Current programming lifecycle, write/readback tests and recovery markers |
| Power-safe dual-bank mutable records where the current layout allocates them | `Project/AutomationStore.*` and `automation_store_test.cpp` |
| Unified host-owned effect library, JSON import/export and strip renderer | Delivery PR #588 and `docs/Host-Macro-Recording.md` |
| Firmware/host ownership and storage guidance | `docs/Firmware-Autonomy-and-Storage.md`, rewritten against the current tree |

## Intentionally not copied

| Historical material | Exact reason |
|---|---|
| `.patch`, `.bundle`, recovery README and repository-check allowance from #592 | These are lossless transport wrappers. Their source identities and decisions are recorded here; copying binary/history containers into the implementation PR would duplicate Git data. |
| Conflict markers and combined files in `a2e62f38` | Eleven files contain literal unresolved `<<<<<<<`, `=======`, and `>>>>>>>` sections. Current code is reconciled feature-by-feature instead of importing a non-compiling snapshot. |
| Old monolithic `MacroAction` generator and duplicate queue implementation | The current generated living protocol plus portable `MacroRing` is the tested successor and also serves VirtualBoard and Go. Keeping both would recreate contract drift. |
| Historical macro lifecycle numeric ordering | The current C++/Go/VirtualBoard contract consistently uses its living state values. Importing the abandoned ordering would break the active wire contract. |
| Historical settings banks at EEPROM `0` and `32` and schema-migration chain | The current map owns `0..31` for audio/boot opcodes and `32..72` for the one 41-byte settings record. The old bank overlaps active data and violates the alpha one-contract policy. |
| Historical full cooperative settings writer | A faithful port exceeded the fixed identity boundary by 122 bytes; a compact current-layout version still overlapped it by 82 bytes. The unchanged default image builds at 32,240/32,384 bytes with 132 bytes free. It is not safe to land without first reclaiming measured flash. |
| Historical cooperative RF-store rewrite | It changes command acknowledgement/durability semantics and depends on the same broader persistence service contract. It is deferred until it can be integrated and measured as one current-layout transaction rather than copied partially. |
| Historical generated WebUI bundles and obsolete UI snapshots | Current source-driven WebUI/effect surfaces have advanced substantially; generated hashes are outputs, not source, and importing them would regress current assets. |
| Private task/session provenance added to the old requirements synchronizer | Requirements remain in GitHub issues and this public ledger; machine/task identifiers are not runtime product requirements. |

## Verification boundary

The reconciliation does not claim new physical-board acceptance. Default AVR
compilation, VirtualBoard/native tests, Go tests and PR checks are the software
gates. Loaded seat motion, RF, strip output and timing still require the
explicit physical acceptance recorded by issues #44, #390 and #554.
