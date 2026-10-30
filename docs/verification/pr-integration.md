# Strip, RF and programming source integration

## Retained checkpoints

The source integration branch retains these exact PR heads as ancestors, not
just copied patches or a provenance-only declaration:

| PR | Head | Retained behavior |
| --- | --- | --- |
| #693 | `d9d0f8c91164757f2fb5d3f0dfecdbad824b95b3` | AVR strip operand constraints, linked waveform gate and late-frame pacing |
| #694 | `0b52df0caf5bc19122be44984c30aed233829f02` | Urclock-only adapter `nometadata`, without relaxing readback |
| #697 | `a48acbdf79ac0e409c19db2a78b78846bbb8fb27` | Verified-write restoration recovery and configured programmer backend propagation |
| #700 | `cdbeac91061d0a4404235723fda729fb432ac6f2` | Deferred RF decoding, bounded buffer ownership and AVR profile size tests |
| #701 | `95d618d3a13e6e78606a14c1c6f7f4360b2a10d3` | Prepared serial reservation, scoped scheduling, strip authoring and pixel diagnostics |

The integration also retains main `8b0ea74a347f656640cfda244582802ac0d09517`:
`Project/ProtocolContract.json` remains the opcode owner, native A5/1 and Pakar
A5/0 admission is mandatory, and published opcode assignments remain append-only.
No legacy adapter or unknown-operation ACK fallback is introduced.

## Paired playback contract

`controller.media.playback.update` accepts `prepare_play` only for loaded, paused
media with a prepared plan revision. Starting a nonempty plan requires the
acknowledged epoch and `play_ready` serial reservation. Missing readiness fails
closed. Keep the existing 50 ms execution and 250 ms freshness/arming bounds.
Emergency stop retains priority admission while ordinary traffic is deferred;
intentional deferral is neither a successful observation nor a lost-board event.
This is the source counterpart of Pealayer's prepared-playback integration, not
evidence that either currently installed application has been replaced.

## Hardware-free verification

At integration checkpoint `085c209d38e88461398133ce3dc07a50bf3323cd`:

- The canonical Windows stable-path runner passed all tests in the public facade,
  controller command, host, control, link, native, playbackpriority, programmer
  and UDP packages. Its selected-suite receipt is
  `passed-selected-44b906be093a6770.json`; it is not the full-suite cache.
- `node Tools/Firmware/verify-rf-deferred.mjs` passed.
- Build-tooling and firmware source tests passed: 137 tests, no skips.
- Protocol regeneration, repository health and `git diff --check` passed.

Applicable final-head Actions checks are still required before normal merge.
No aggregate, skipped job, historical CI run or prior deployed build substitutes
for those checks.

## Explicit remaining acceptance

This integration does not flash a board, replace an installed app, change a
setting or actuate an output. The historical readback, streaming and chair-only
receipts in the existing checkpoint documents retain their original scope.
Final-image physical RF/strip tests, Android installation and design acceptance,
native visual acceptance, broader effects and full-film endurance remain separate
gates. The upstream AVRDUDE 8.3 vector-padding interpretation and matching
adapter/configuration packaging must not be resolved by loosening verification.
Track remaining work in #696, #695, #562 and the paired Pealayer #80 as applicable.
