# RF and lighting continuation checkpoint

The existing strip-serial-diagnostics branch at a48acbdf79ac0e409c19db2a78b78846bbb8fb27 remains the recovery source checkpoint (PR #697). This branch additionally preserves a previously uncommitted bench trace found during the RF setup pass. The opcode 0xE1 trace has not been validated or newly deployed in this pass. Do not merge or deploy it without reviewing its transport and timing effects. The original working copy is intentionally preserved unchanged.

Live checks: coordinator and Pealayer report a connected board. The four existing board seat mappings are left up/down and right up/down. A bounded first-button capture completed with no new signal; no existing mappings or learning preferences changed. New handset capture and physical motion confirmation remain pending.

The shared effect catalog contains police, white-thunder and converging-red. These entries are catalog evidence only. Next: capture one new handset key at a time, confirm its intended seat action, append matching board assignments without replacing the old handset, read back, then verify press/release and physical movement. After remote acceptance, test rainbow on the currently attached pixel count, verify the three effects individually, and resume MOSFET investigation (#672). Do not infer physical acceptance from software acknowledgements.

Validation for this preservation checkpoint: git diff --check only; no firmware build/upload or hardware stress test. Runtime configuration backups, screenshots and private hardware snapshots remain local and are excluded from Git.

## New handset configuration completed

Four distinct buttons were captured individually and appended to the board catalog: A left seat up, B left seat down, C right seat up, D right seat down. Each mapping was checked through a separate authoritative board read. Stable identities/actions of all original nine entries and all host bindings were compared and preserved. Learning is inactive after the final capture. No app binary or firmware was changed for this setup.

The board reports both seats stopped. Physical direction and release-to-stop confirmation remains the next human step; configuration readback does not establish movement acceptance. The subsequent phase remains the rainbow check, MOSFET investigation and individual checks of police, white-thunder and converging-red. Private RF identities, device settings and snapshots remain excluded from Git.

## Human remote acceptance and start of lighting checks

The human confirmed all four new remote directions and stopping after release. The board subsequently reported both seats stopped. This acceptance applies to the handset test, not unrelated seat stress or timing tests.

A three-pixel, 20-FPS rainbow was started through Pealayer hardware.strip.rainbow. The coordinator reports an active rainbow stream and Pealayer reflects it after status refresh. During observation the reset count, framing/CRC errors and PWM errors remained unchanged. Physical rainbow smoothness and current connected pixel count await human confirmation; no visual LED acceptance is inferred from ACKs.

A read-only command diagnostic found that strip effect list is rejected by the current coordinator, whereas effect inspect police succeeds. The hardware preview in Pealayer also uses the obsolete strip effect play namespace. A focused correction to effect play ID COUNT FPS is pushed at ToghrolTP/pealayer fix/strip-effect-dispatch, f5fb6a19db711adefa634ff11e0b48e6871c074e, PR #212. Hosted checks are pending; Cafe deployment has not happened. Build/deployment is blocked on the exact-source successful package and requires a fresh authoring-state check and the existing validated local MPV pairing. No local production Rust compilation or firmware upload was performed.

Next: obtain rainbow visual confirmation, run a bounded watched MOSFET test preserving unrelated outputs, then verify the three lighting effects individually. Complete PR #212 checks and the supported updater deployment before claiming the hardware-preview fix is delivered. The original MOSFET shutdown cause remains unproven (#672).

## MOSFET remote continuation

The user deferred LED/rainbow checks and confirmed a separate lighting handset. A and B match already stored lighting waveforms; the user physically confirmed the first lamp at about 20 percent and the chandelier lit. No duplicate bindings were added. B dimming across the range is not yet accepted.

C reception was confirmed against the third lamp's existing complete waveform. Its tap, hold, repeat and hold-end host rules were disabled. These four rules were enabled through controller.rf.binding.put with explicit previous_name, preserving names, gestures, targets, actions and cooldowns. Independent catalog readback confirmed all four flags and exact preservation of the other host bindings and all 13 board RF mappings. Enabling configuration did not command an output; the third lamp remains at zero awaiting the human press.

Next: ask the user to hold C for two seconds and confirm the third lamp physically lights, then verify D and the short-tap/hold behavior on the intended lamps. Keep the LED tests deferred. No app binary or firmware change is required for this configuration step. Full private RF/settings snapshots remain local.

## Lighting handset setup accepted

The final B hold test physically increased the chandelier brightness. Authoritative readback changed from about 1 percent to 46 percent, with remembered brightness matching. The human has now confirmed all four lighting keys and visible brightness adjustment for their intended lamps: A first lamp, B chandelier, C third lamp, D fourth lamp.

Final catalog comparison confirms all 13 on-board RF mappings unchanged and exactly the original host binding set. The only host rule changes are the four C enabled flags; names, waveforms, actions, cooldowns and unrelated bindings are preserved. All 16 lighting gesture rules are enabled, and learning is inactive. Configured behavior is short tap to toggle, hold/repeat to roll brightness, and hold-end to remember. User-selected output levels were left in place. No application binary, firmware or direct output override was introduced for this setup.

The remote setup scope is complete. Keep separate acceptance open for the original 100-percent/transient-shutdown issue (#672), full-range behavior and untested power-cycle recovery. LED/rainbow/effect testing remains deferred at the user's request. Pealayer #212 is separately preserved source, not an installed fix claim. Private current catalogs and runtime snapshots remain local; the GitHub documentation excludes those identities/settings. The pre-existing bench trace on this WIP branch remains unvalidated and is not approved for deployment.
