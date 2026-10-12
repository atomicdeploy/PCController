# Native app provenance

- Donor: atomicdeploy/PakarController, commit
  `6dfc3e72ca0264242325f76b7fba04dec1f38fe2`.
- Copied only its 68 Git-tracked `mobile/` paths, not caches, SDKs, generated
  build outputs, local properties, private configuration or credentials.
- Original donor checkout is untouched. Its main entry is retained here as
  `lib/pakar_reference.dart`; package imports/channel namespace are mechanically
  renamed, while new PCController/Pealayer adapters live in `lib/unified/`.
- Reused deterministic seven-segment painter, Android SAF one-use destinations,
  bounds/cancellation behavior, responsive Flutter scaffold, locked packages,
  Vazirmatn and Phosphor licensed assets and original regression coverage.
- PCController starting source: `634a41f655e4b3f8148483e2f7ff6991938187ae`.
  Current JSON-RPC, artifact, event-patch and app-action contracts were read from
  `Tools/Controller/web/src` and `internal/ipcjson`. Segment/RGB event patch
  behavior follows `web/src/status-led-event.ts`, not a second firmware model.
- Pealayer source: `00687b16a70217963794bc2d867160f3fbcd5e65`.
  Native RPC names/parameters and existing typed bridge route were read from
  `src/platform/interop.rs`, `src/server/mod.rs` and `src/peer.rs`.
  No Pealayer authored source was changed or running installation replaced.
- Public PCController icon SHA-256:
  `f3ab0659028944986e90759973f06e634b57b4ac8964fa332659fc44c3c66f35`.
- Public Pealayer icon SHA-256:
  `082cd912ba2a649f4f2c975b570efa6063e1c96a8051e0b81cdd1895826ab9ca`.

MIT notices are preserved as PAKAR-LICENSE and PEALAYER-LICENSE. Font notices
remain under assets/fonts. No private Soulayer/user artworks were copied.

The donor's reuse consultation returned mobile-mirroring parity gaps and a
stale tracked Pealayer inventory for mobile-firmware. This fork is **not** a
claim of a certified exhaustive donor reuse audit, audio/mirroring acceptance
or full interface parity. Its new transport and native UI require their own
tests and current live-host evidence; original donor build gates were neither
changed nor bypassed.
