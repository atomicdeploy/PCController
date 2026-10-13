# Contextual utility redesign audit

The 2026-10-13 rejection is durable in root and mobile AGENTS.md. The latest
explicit single Connect/Cancel/Disconnect toggle supersedes the earlier menu-only
Disconnect request. This document records source proof separately from delivery,
real transports, physical behavior and human design acceptance.

## Changes in 0.1.11+12

- One compact connection lifecycle toggle per peer; no competing connection verbs
  in More. Explicit disconnect still persists discovery opt-out and saved intent.
- Cancel playback exists only for authoritative running macro/effect IDs; Pealayer
  Stop effects requires its advertised hardware_details.macro_running. Empty
  live-only panel/board/effect cards and static empty-catalog narration disappear.
  Useful offline drafts and retained stale PWM edits remain, with owner guards.
- Relay state pills directly invoke the opposite typed action, using actual
  observed state and the same ownership, lock, busy and E-STOP checks. E-STOP is
  checked again after any asynchronous confirmation. Unknown never implies OFF.
- Hardware action rows have equal widths and fill their cards. Grids fill their
  available width and reduce column count for small groups. Semantic green/red
  ON/OFF colors, muted disabled actions, header menus, inline rename, handle-only
  reorder, independent header/button styles and Pastel/Vibrant stay intact.
- Channel folder assignment is a dropdown with No folder, compatible advertised
  folders and same-kind existing assignments. Current raw identity is retained.
  Channel editing now uses the shared responsive dialog header/Close/footer.
- A healthy acknowledged WebSocket subscription performs no periodic HTTP/PWM
  reads. Change events coalesce readback; explicit pull refresh is retained.
  Same-owner confirmed PWM stays current through a continuous fresh stream;
  failed/unavailable or replaced-owner reads cannot become a zero or an applied
  draft. Failed/stale WebSockets and request/response-only relay routes use bounded
  fallback reads. Reads join in-flight work, and commands never replay.
- The short tablet activity rail scrolls instead of overflowing. app.ping no
  longer advertises the obsolete hardcoded 0.1.7 version; native package.info
  remains the installed package identity source.

## Existing cross-project capabilities inspected

| Requirement | Source evidence | Current acceptance |
|---|---|---|
| Unified activities | workspace_chrome.dart Live/Channels/Panel/Studio/Devices/Firmware/Events; one selected board/player context | Phone/tablet fixtures; human acceptance open |
| Palettes/branding | preferences.dart Digitalogic/Pealayer/Pakar/Amber/custom, Pastel/Vibrant; setTask native task title/icon/color | Source inspected; actual OS switching open |
| One/both launchers | Android manifest aliases target one MainActivity; PreferencesChannel enables destination before disabling old aliases with DONT_KILL_APP | Source inspected; real launcher acceptance open |
| Android discovery | NsdManager DNS-SD/mDNS service browse/resolve, multicast lock/network callback; discovery.dart SSDP/MNDP parsing and read-only identity verification | Parser/source proof; real Android multicast open |
| PCController discovery | internal/discovery DNS-SD/mDNS, SSDP and MNDP; public directory product/ports; hostbridge advertises configured protocols | Cafe public directory read below; transport-by-transport live acceptance open |
| Pealayer discovery | server/discovery.rs browse/advertise/verify DNS-SD/mDNS, SSDP and MNDP | Cafe reports all four active below |
| Automatic connection | scanPeers requires verified unambiguous role, auto-connect preference, no current selected connection and no explicit disconnect opt-out | Existing saved-intent/owner guards retained |
| APK peer transfers | AppUpgrade bounded chunk IPC/RPC read/write, size/digest/package/signer/newer-version checks; PeerUpgrades foreground sharing and short-lived approved offer | Source inspected; two-device transfer/install consent and phone storage open |
| Authentication | Remote token inputs absent; future multi-step authentication deferred. Protected local HTTP/WS/TCP/Intent automation keeps required credentials and action permission | Existing local security preserved |
| egui hardware parity | Pealayer layout.rs 7375/7415/7575 observed indicators, direct relay toggle and card actions informed this change | Full native parity, hold-motion/shortcuts and physical acceptance still open |

Read-only Cafe checks through its existing SSH endpoint reached both native
services. PCController /upnp/public.json returned product PCController,
health.ok true and enabled discovery protocols dns-sd, ssdp, upnp,
ws-discovery, broadcast, netbios and mndp. This is a configured directory,
not proof that every multicast transport succeeded. Pealayer /api/discovery/peers
returned configured dns-sd/mdns/ssdp/mndp, all four active, and four peers.
No connection selection, metadata mutation, board output, playback or firmware
operation was sent by these checks. Earlier failed direct LAN probes are not
reinterpreted as successful Android discovery.

## Verification

Thirty focused checks passed: 17 appearance/recovery/front-panel/output checks,
five new contextual checks, one real loopback healthy-WS/event/fallback check,
six PWM ownership/freshness checks and one discovery parser check. One additional
opt-in renderer passed and produced phone/tablet hardware and folder-dialog PNGs
using actual app themes/fonts. Those are controlled source fixtures, not device
screenshots. Analyzer/package/device receipts are added after final delivery.
The application remains unaccepted until its live and human gates are met.

## Package/device receipt

Final analyzer and one ARM64 release build passed. Source checkpoint
0eef88bb5736b7010efa990fc65278c0531641de produced 0.1.11/code 12,
20,133,536 bytes, SHA-256
`0af60c9b04f34964f19a0f072cd87df13fb6decd3bf585789bb9647c2986ba37`.
The inherited Android development signer verified. One ordinary non-streamed
in-place tablet install succeeded under continuing delivery authorization and
the recent saved-drafts/update confirmation. Installed version/code/base hash
match. Active video foreground remained intact; the controller was not launched
over it. No physical relay/motor/beep/firmware or player action was sent.

The deduplicated phone remains 0.1.2/code 3, with 498,272 KiB available below
its previously verified 1 GiB installer threshold. No repeated install, data
removal, reserve bypass or unlock was attempted. Exact receipts are in
android-0.1.11-delivery.json. Live Android discovery, OS branding/launcher changes,
two-device upgrade/installation consent, full egui parity and physical/audio/human
acceptance remain open. The continuous goal remains active.
