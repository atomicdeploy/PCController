# One RF button per MOSFET

A confirmed assignment can bind one complete code/bits/protocol waveform to
one selected MOSFET channel, using the existing persisted RF binding API:

| Gesture | Board-command action |
| --- | --- |
| `tap` | `pwm toggle CHANNEL` |
| `hold` | `pwm roll CHANNEL` |
| `repeat` | `pwm roll CHANNEL` |
| `hold-end` | `pwm remember CHANNEL` |

Configure that channel's `on_behavior` to `restore`. Tap is emitted once at
short release, without the double-click delay; a held release never emits Tap.
RF release is inferred after 250 ms of silence; hold begins after 600 ms.
Timed firmware uses the MCU clock, not delayed host/UI arrival times, for
continuity, hold duration and silence. A bounded receive-pump cache observes RF
before presentation dispatch; buffered receptions cannot split a continuous
hold into false taps. Release requires a later MCU clock observation proving
silence, with a read-only status heartbeat when necessary. A failed heartbeat
or lost/replaced session never becomes a toggle. Untimed firmware retains the
legacy host-clock inference. This is inferred release, not a physical button-up
signal; physical acceptance is still required.
Existing Click/Double and Down/Up events remain available independently. Do not
assign both Tap and Click/Down toggles to the same waveform. Learning receptions
never schedule delayed taps that could execute after learning is stopped.

Roll increases the coordinator-calibrated logical percentage by the channel's
configured step. It reaches 100%, then rolls to a low nonzero output and repeats
while packets continue. The low level is 1% or at least one 12-bit quantum,
whichever is representable under the current curve. It does not toggle Off
before dimming. Only fresh active-hold roll events execute; an owner-pinned
freshness guard is checked after serial admission and writer queues, immediately
before PWM_SET. Release prevents a queued mutation, while a write already sent
can finish its normal acknowledgement and authoritative readback deadline.
Obsolete release-timer callbacks recheck the most recent packet before releasing.
Use a 1 ms binding cooldown for hold/repeat if the ordinary 250 ms automation
cooldown is not desired; the established RF repeat cadence still bounds writes.

Confirmed nonzero duty is saved under the selected configured board profile's
`pwm_recall` map at explicit toggle/Off or `pwm remember`, not every telemetry
frame or hold repeat. Historical persisted duty is a fallback only when the
current connection has no newer confirmed memory. Exact duty survives curve
edits; it is displayed through the current curve. Zero does not erase recall,
and failed/unconfirmed I/O never persists a new value or retries a mutation.
Profile clones deeply copy this map. The shared PWM value reply distinguishes
`remembered_persisted` from current-generation recall.

Profile identity follows the existing configured USB serial/instance/port
identity contract; adapter paths are not advertised as stable provisioned board
IDs. Memory does not cross distinct profiles. An operator must confirm the
profile and physical channel when a board is replaced on the same adapter path.
Without known memory, restore retains the established full-intensity fallback.
Absolute `controller.pwm.set percent:100` still means exactly full intensity.

This is host-owned functionality; PCController must run. It does not pretend to
fit autonomous recall/ramping into the existing space-constrained AVR image.
Learned unmapped EEPROM records and original RF/motion mappings are preserved.
Four internal gesture rules implement one physical button, not four required
remote buttons. Each waveform/channel pairing needs operator confirmation and
physical tap/hold/release/restore acceptance before it is marked operational.
