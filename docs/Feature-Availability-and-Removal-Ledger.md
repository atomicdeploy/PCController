<div align="center"><a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Feature availability and removal ledger

This ledger is a required review gate for constrained firmware profiles. A
feature may be off in one profile, but it must never disappear silently. Every
change that removes, relocates, or disables behavior must state the owner, the
observable loss, the measured reason, the recovery path, and the capability or
build flag that tells clients the truth.

## Removal policy

1. Do not remove or default-disable user-visible behavior without recording it
   here and in its GitHub issue before delivery.
2. Preserve the exact data or implementation when a feature moves between MCU,
   EEPROM, and host. Add a regression test for the preserved representation.
3. Measure flash and SRAM before and after a constrained-profile change; “save
   space” is not sufficient evidence by itself.
4. Advertise only behavior that is compiled and callable. A build flag that
   selects no implementation is forbidden.
5. Verify the production profile, not only a larger diagnostic profile.
6. Alpha storage may be replaced directly, but alpha status does not authorize
   silent feature loss. Schema cleanup and feature removal are separate changes.
7. A moved feature remains restorable: either its engine stays compiled, its
   exact host definition remains in the catalog, or a tracked build/EEPROM path
   can put it back.

## Audio ownership

`TonePlayer` is the cooperative Timer1 playback engine. The cue controller
selects *when* and *what* to play. Host melodies use the same acknowledged
`BUZZER` opcode one note at a time; moving a sequence to the host does not
replace the MCU tone engine.

| Feedback | Current owner and availability | Exact definition / recovery |
|---|---|---|
| Front-panel key feedback | MCU, autonomous | Existing ordinary 2,000 Hz menu beep; obeys Silent |
| Door opened / closed | MCU, autonomous | EEPROM-tail cue record, with immutable fallback 1,700/1,100 Hz for 45 ms |
| Relay or motion energized / released | MCU, autonomous | EEPROM-tail cue record, with immutable fallback 1,900/1,250 Hz for 35 ms; motion is intentionally distinct from the ordinary key beep |
| Direct beep | MCU engine, commanded by any host surface or a macro | `beep [frequency_hz] [duration_ms]`; zero/zero stops playback |
| `finish` | Host named melody | E5 659 Hz/100 ms, G5 784 Hz/100 ms, A5 880 Hz/250 ms |
| `lost` | Host named melody | G4 392, E4 330, C4 262, G3 196 Hz; 100 ms each |
| `incorrect-beep` | Host named melody | Three 2,000 Hz/100 ms notes, each followed by 100 ms |
| `error-beep` | Host named melody | Five 2,000 Hz/10 ms notes, each followed by 10 ms |
| `fault-beep` | Host named melody | 1,000 Hz/250 ms, 500 Hz/500 ms, then 5 s gap |
| `success-cue` | Host named melody | C6 1,047 Hz/70 ms, 30 ms gap, E6 1,319 Hz/110 ms |
| `error-cue` | Host named melody | E4 330 Hz/90 ms, 50 ms gap, C4 262 Hz/160 ms |
| Welcome / programming-ready | Host named melody | 1,032 Hz/70 ms, 60 ms gap; 2,010 Hz/70 ms, 60 ms gap; 2,400 Hz/120 ms, 150 ms gap |
| Save, discard, RF-learning result, hot warning, motion-exit success | Not autonomously audible in the constrained image | Exact `success-cue`/`error-cue` primitives are preserved on host; assign event policy where the host owns lifecycle or include selected cues in the tracked EEPROM executor |

The 13-byte audio-cue record occupies EEPROM `1011..1023`: four packed
`{frequencyLE16,duration8}` entries followed by CRC-8. Blank, corrupt, or torn
data uses the immutable historical fallback, so a factory-erased EEPROM does
not remove essential door/motion feedback. The generated default EEPROM writes
the same values explicitly.

The remaining `967..1010` 44-byte gap is deliberately not claimed by an
unimplemented feature. A future bounded startup/event-opcode record may place
selected melodies or peripheral setup there after the user chooses which
sequences belong offline. It needs an opcode allow-list, CRC/atomic publication,
execution limits, silent/programming policy, host editors, and recovery tests.

## Other constrained-profile switches

| Area | Current state | Reason and recovery path |
|---|---|---|
| Native status RGB engine | Enabled | Descriptor compositor remains MCU-owned; host sends state descriptors rather than compatibility-rendered frames |
| EEPROM status-effect profiles | Disabled | Compact safety and host descriptor paths remain; enable only with a measured, callable profile implementation |
| Native I2C LCD renderer | Disabled | Current production LCD presentation is host-owned; generic I2C remains available. The old native flag is compile-rejected until lifecycle/render call sites exist |
| Local PCA/PWM editor pages | Disabled | Direct PWM, illumination automation, persistence, opcodes, and host controls remain; retired local editors duplicated richer host surfaces |
| BT Audio LED detection | Disabled | Explicit profile choice; raw active-high hardware validation remains tracked before re-enabling |
| Async segment events | Enabled | Changed-only segment state is board-pushed |
| General async presentation events | Disabled | Rendered RGB/buzzer frames are not advertised; re-enable only after exact flash measurement |
| Scheduled segment messages | Disabled | No capability is advertised; immediate display remains |
| Local extended settings editor | Disabled | Host owns extended fields; compact `bEEP` retains immediate mute/unmute |
| Task scheduler | Disabled | The prior global task object had no registered tasks; enable on a larger profile only with a real task set |

This file describes source behavior. A feature is not physically accepted until
the exact artifact is uploaded/read back and its observable behavior is tested.
