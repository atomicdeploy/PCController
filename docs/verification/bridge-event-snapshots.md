# Bridge event snapshot work

The source event loop expanded a complete client snapshot independently for
door-running presentation, segment scrolling and status LED state. Each snapshot
also clones/expands macro/effect/config catalogs. A private bounded application
profile identified this as real duplicate Go-side work; native Win32 wait frames
in that profile are **not** evidence of an OS busy loop or actual CPU percentage.

The bridge now shares one coherent snapshot across those three presentation
consumers, after keyboard and buzzer-route processing. It is never retained
across events or connection generations. Telemetry keyboard processing retains
its own earlier observation. Webhooks, text mappings, board mirror and event
forwarding keep their ordering and delivery semantics.

Notification filtering now observes a snapshot lazily, only for a confirmed
physical-door or configured door-running context. Ignored telemetry/debug/PWM
events and ordinary error toasts do not expand an unused catalog. Door context
is still freshly observed **after** action dispatch, not reused across a callback
that may have changed Running/connection state. No safety event is filtered out
and no notification preference, sound, command or polling rate is changed.

Focused hardware-free regressions assert one presentation observation, fresh
generation/door state on the next event, zero notification observations for
non-contextual events, real door observation and fresh Running-door suppression.
Ordinary public snapshots retain their full catalogs and library data.

Hosted CI and exact merged deployment are required. Then compare bounded process
CPU and private profiles with the same media, clients and event cadence. Source
work reduction is not a claim that all excessive CPU or board hangs are solved;
the owning CPU issue #149 and separate physical/writer investigation #672 remain
open until live causal and stability acceptance. Do not publish production
profiles, screenshots, device logs or private machine information.
