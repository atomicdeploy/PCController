# Live value push contract (2026-10-13)

The shared AVR/native firmware observes the complete 16-channel PWM readback at a bounded 50 ms cadence and sends changed values with the existing PwmValuesResponse opcode and unsolicited sequence zero. It compares against the last sent values so a quiet final change is retained. This adds no output write. AVR SRAM/physical flash acceptance remains open.

Go caches complete PWM observations, calibration, monotonic revision, timestamp and connection generation. Serial observations, CLI writes and native readbacks use the same publisher. RGB throttling keeps one trailing observation per owner; WebSocket/Socket.IO presentation keeps one pending frame per continuous domain. Segments, buzzer and lifecycle edges stay immediate. New subscriptions receive the current cached snapshot, and PWM changes receive controller.pwm.values with all 16 calibrated channels. Low-rate authority changes carry a cached snapshot; macro steps do not amplify full snapshots.

Known consumers audited/updated:

| Consumer | Current treatment |
| --- | --- |
| Unified Android Flutter | Buffers pre-ACK notifications; validates selected host/generation/revision and complete calibrated PWM; fences older reads/pending writes. Healthy revised pushes cause no PWM HTTP read. Legacy/incomplete events and lost WS use bounded fallback. |
| PCController Web | Buffers pre-ACK notifications, merges complete PWM revisions, protects current edits, stops periodic PWM reads while WS is healthy. |
| Remote Go TUI | Buffers pre-ACK values, consumes snapshots/PWM, retains separate event domains when coalescing and fences generations. |
| Generic Go RPC SDK | Opaque notification transport accepts new methods; public snapshot decoding preserves unavailable settings without bypassing validation of available settings. |
| Native hostbridge, mirrors, UDP, Socket.IO | Shared Go runtime publication; WS and Socket.IO use the same event-stream worker. No independent client polling implementation added. |
| Pealayer native/Web | Separate coordinated change consumes complete calibrated PWM/snapshot facts, fences owners, invalidates hardware cache and releases Web drafts when actual newer observations arrive. |
| Pealayer typed relay | Request/response route remains bounded fallback; no invented controller subscription on player WS and no recursive proxy. |
| Legacy Pakar/ESP mobile transport | Different protocol/business domain, unchanged. |

Validation: selected stable-path Go tests in five packages include two real loopback WS clients and reconnect baseline, per-domain quiet final values, RGB timer ownership and TUI coalescing. Offline SDK snapshot decoder regression passes. PC Web 30 focused tests and TypeScript pass. Flutter 16 focused push/event/PWM/recovery/front-panel scroll checks and analyzer pass. Pealayer complete-push ownership/calibration Rust test, Web draft checks and TypeScript pass. Shared native firmware objects compiled, UART unit and memory-only native stdio/TCP conformance passed. The running dev-host binary is owned elsewhere and was preserved; candidate linked to a project-owned build-programs path.

These are source/loopback/RAM checks. Production Go/Pealayer replacement, AVR physical flash, hardware/audio/UI acceptance and the phone installation storage gate remain separate. Android candidate is 0.1.13+14; installed identity and device-specific delivery are recorded after the build.
