# Strip integration timing checkpoint

The synchronized restore failure on host ci.1653 was distinct from serial framing corruption. A five-second execution trace showed the timeline worker starting its request at +173.45 ms, the serial reader delivering the ACK at +189.335 ms, and the worker resuming at +234.281 ms. Delivery took about 15.9 ms; the runnable worker then waited 44.95 ms. A GC sweep-termination stop-the-world interval occupied 15.59 ms of that wait. These are host observations, not hardware arrival timestamps.

The allocation triggering that collection came from hostbridge event presentation -> Client.Snapshot -> effect groups -> Store.Current -> deep configuration clone. Door, segment and status-light observers do not need the effect library. BoardSnapshot now supplies their cached live state without catalog providers, macro library copies or reset-device enumeration. The facade regression also caught an indirect catalog expansion through output-status capability checks; those checks now prefer the same board-only observation while preserving the existing fallback for other commanders. Full public query snapshots still contain the catalogs. Regression coverage checks provider exclusion, retained state/timestamps, public catalog preservation and shared fresh presentation observations.

This removes demonstrated unnecessary allocation; it does not prove every prior timing failure had the same cause or guarantee a Windows scheduling deadline. The 50 ms limit remains unchanged. The next gate is hosted CI, exact-artifact host update, and a coordinated unprofiled synchronized replay with cleanup and counter checks. Do not count the profiled failure as playback acceptance.

Standalone evidence remains separate: a three-pixel 180-second stream and 150 seat Up requests over 15 seconds had no new framing/CRC/reset errors. Identical seat commands coalesce, so this does not establish 150 relay cycles. Police, White Thunder, Converging, Fade and Pulse demos are available; visual, physical and audible acceptance remains with the user. Android installation and native modal visual verification are still open.

Private traces, machine configuration and captures are retained locally, not published.

## Post-update result

Host ci.1654 passed hosted builds and was installed with independently verified live identity. Its unprofiled short replay still failed restore at 63.5 ms before observed playback. One post-fix trace failed at 79.2 ms: the worker waited runnable about 23.2 ms after a write syscall and 43.7 ms after ACK delivery. The relevant stop-the-world intervals were only about 0.50 ms and 0.07 ms, so those pauses alone do not explain the delays.

The new trace exposed further avoidable allocation: retained timeline trimming allocated a new whole-history slice, and door-warning notification context still expanded the full catalog. The follow-up reuses history storage under its existing lock and clears discarded references; public readers remain detached. Notification context now uses BoardSnapshot. Catalog construction also avoids building defaults only to overwrite them with configured values. Regression coverage checks zero allocations during pruning, retained ordering and cleared references, plus existing history and notification behavior. These reductions remain performance fixes, not proof of synchronized acceptance. No timer, priority or GC-disable workaround is applied.

The trace also shows ACK delivery at +225.376 ms followed immediately by the reader entering another Win32 blocking wait at +225.394 ms, while a different Go processor remained idle until +253.318 ms. The waiter ran at +269.071 ms. This supports a local run-queue handoff problem rather than claiming GC explains the whole delay. The Windows reader now cooperatively yields once after a decoded batch delivers a pending response, before its next blocking read. It does not sleep, modify process priorities, disable GC or change deadlines. Live comparison remains required.

## Reply-handoff candidate result

ci.1655 was built, installed and live-identity verified. An unprofiled restore still timed out at 99.8 ms; its unmatched ACK reached the read loop after the fault, with zero observer delay. Nearby board timestamps suggest generation preceded host receipt by roughly 97 ms; this is a clock-alignment inference, not a hardware capture. A subsequent traced short replay did restore and enter Playing. Its configure and frame-stage acknowledgements resumed the waiter within microseconds after delivery, confirming the new handoff path operates. However the stage took 38.2 ms end-to-end, and publishing evidence before show consumed the remaining deadline; show was refused at 50.9 ms late.

That trace exposed roughly 400 KB event-log allocation at publication: the independent 512-event bounded log still reallocated its retained slice after every append. The follow-up reuses and clears bounded event, activity and RF-log storage under eventMu, preserving order and retention. Regression coverage checks backing storage reuse, newest/oldest order and cleared discarded references. This does not resolve the separate variable read-delivery delay by itself; synchronized acceptance remains open. Native exact-zero seek settlement is tracked separately in Pealayer issue 219.

## Shorter Windows read interval experiment

ci.1656 was verified and installed. Its short unprofiled replay restored and entered Playing, but show expired during a 29.4 ms write after earlier frame operations consumed the shared cue budget. Cleanup retained Stop/Stop, relays off, framing/CRC zero and the same reset count. No full run was claimed.

The next diagnostic changes only the Windows idle serial read interval from 100 ms to 10 ms, keeping the persistent frame decoder and the original response/cue deadlines. This tests whether shorter blocking-read residency reduces driver/scheduler latency; it is a hypothesis, not a confirmed driver diagnosis. A fragmented ACK regression inserts empty reads between bytes to ensure timeout polling cannot discard frame state. No priority, GC or physical-output policy changes accompany it.
