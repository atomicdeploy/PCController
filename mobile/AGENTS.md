# Flutter controller implementation rules

Read the parent AGENTS.md and ../docs/CONTEXTUAL-UI.md in full before changing this app. The current basic design is explicitly rejected, and all unified-controller acceptance rules in the parent apply to this directory.

- One integrated, activity-oriented PCController/Pealayer workspace; no separate product tabs. Offer compact expert controls, responsive phone/tablet composition, meaningful headers/cards/dialogs, coherent semantic colors, icons, charts and pills.
- A peer has one connection action reflecting its actual lifecycle. Omit unavailable-only cards, irrelevant actions, dormant access-token fields and filler narration. Unknown is not zero. Keep useful warnings, safety restrictions and failed/uncertain operation receipts.
- Appearance/branding preferences must update the actual Android activity/task presentation and selectable launcher aliases. Preserve one or both launcher entries for the same unified app and include PakarController palettes without replacing existing schemes.
- Discovery is DNS-SD/mDNS, SSDP and MNDP with verified product/endpoint metadata. Automatic connection respects saved intent and the live selected peer; it must not seize board authority, actuate hardware or replay an uncertain command.
- Peer APK transfers/upgrades share IPC/RPC and must be bounded, integrity-checked, package/signature-checked and mediated by Android installation consent. Do not remove existing protected local automation security to hide an irrelevant remote-peer token UI.
- Preserve dirty work and active owner file scopes. Do not claim design or physical acceptance from compilation, fixture rendering or APK delivery. Keep screenshots honest and separate controlled fixtures from actual device/network state.
