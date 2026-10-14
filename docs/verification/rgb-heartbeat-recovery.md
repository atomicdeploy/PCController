# RGB ownership regression recovery

The Linux real-protocol mirror smoke on RF-guard PR #681 acknowledged earlier
PWM/relay/menu/buzzer operations but both status LEDs returned to ready green
instead of retaining the requested static RGB. This is not a name-collision
feature and is not dismissed as CI infrastructure failure.

The current production `ProgramState` handler again cleared
`HOST_STATUS_OVERRIDE` and cancelled the LED effect on each heartbeat. Merged
PR #181 explicitly removed those operations in its original firmware path.
That commit is still an ancestor of main, **but its behavior is not present**
in the current handler. An ancestry-only reconciliation is not content proof.
The old handler was already wrong in the parent of layout refactor #246, so
the move itself is not blamed as the first cause. Preserved historical merges
and the current functional source must be compared separately.

The feature-preserving repair restores independence between program liveness
and explicit status ownership. Static color and native effect phase survive
repeated Idle/Running heartbeats. Explicit status release and macro safety
termination retain their release paths; fault/hot/door/offline priority is not
weakened and no melody, peripheral or compile gate is removed.

The added regression sends actual UART frames through the production sketch
compiled in VirtualBoard, not a duplicate model. It advances deterministic time
through six heartbeats and asserts physical rendered-byte observations, animated
phase retention and explicit release. Existing LED-controller strict descriptor,
rollover and local-safety tests remain. The full-duplex real-protocol mirror
smoke must pass as well. No local build/test or production flash is performed
by this source pass; hosted exact-source verification precedes runtime-owner
deployment and separate physical LED acceptance. Owning issue #682.
