# Repository execution rules

- On Windows, never run `go test` directly. It executes test programs from
  changing temporary paths and can repeatedly trigger Windows Firewall prompts.
- Run Go verification through `build.cmd --host-only` or
  `node Tools/Build/go-tests.mjs`. That runner compiles and executes tests only
  from deterministic product-owned paths. On Windows that path is
  `%LOCALAPPDATA%\PCController\test-programs\go`, shared by every worktree.
- Use `go vet ./...` for the non-executing static-analysis pass. CI may use its
  native test runner on non-Windows hosts.
- For a focused pass, use `node Tools/Build/go-tests.mjs --package
  internal/artifacts --run TestPeer`. On Windows use the canonical output;
  do not choose per-task `--output` paths, override `GOTMPDIR`, or copy/rename
  test executables. Repeat `--package` as needed.
  Partial passes use separate caches and never satisfy the default full suite.
- This workstation also routes Go temporary output to
  `%LOCALAPPDATA%\PCController\go-noexec-temp`; files created there inherit a
  deny-execute ACL. Do not remove that guard to work around a direct-test
  failure—use the stable runner.
- Exercise real LAN discovery only with the named packaged
  `Tools/Controller/bin/controller.exe`; unit tests should use loopback or
  side-effect-free wire parsing.
- Treat `main` as the sole durable branch. Temporary branches must describe a
  cohesive topic with short semantic names (for example `fix/status-ownership`),
  never dates, ticket numbers, machine names, actor/agent names, or random
  suffixes. Finish, test, merge, and delete them promptly; do not delete any
  branch until its unique commits and dirty work are represented on GitHub and
  proven merged or deliberately superseded on the corresponding PR/issue.

## Digitalogic interface design

Before implementing or reviewing any user-facing interface, read and follow [Contextual UI and no narration](docs/CONTEXTUAL-UI.md). This standing ecosystem contract covers state-driven rendering, progressive disclosure, semantic actions, conditional affordances and stable layout. Apply it across related controls, preserving existing safety and authorization requirements.

## Unified controller application - mandatory acceptance

- The current basic Flutter design is rejected. Build one expert utility workspace, not separate PCController/Pealayer product tabs. Organize by useful activities and current capabilities; peers and branding are contextual identities, not duplicate navigation hierarchies.
- Use one contextual connection action per peer. Connect/cancel may be first-class while disconnected/connecting; Disconnect belongs in the peer More menu, per the latest direct Flutter acceptance request. Preserve explicit disconnect discovery opt-out. Hide dormant access-token inputs while the current products do not require authentication. Defer a complete multi-step authentication/authorization flow; do not introduce token placeholders or a partial login design.
- Omit unavailable-only cards and irrelevant controls. Never publish unknown measurements as zero. Remove filler narration such as "last values unavailable" and boilerplate explanations of ordinary behavior. Preserve concise, actionable failures, stale-state/safety indicators and outcome-unknown errors.
- Use compact, professionally composed phone/tablet layouts: aligned headers, meaningful cards, contextual dialogs, square accessible icon actions, semantic colors/pills, real live charts and efficient space. Rich styling must remain coherent, legible and useful; neither oversized generic buttons nor a plain basic navigation shell is accepted.
- Refresh from authoritative events, coalesce unchanged work and retain last good state. Do not add per-frame network probes, persistence, synthetic telemetry or automatic mutation retries.
- Preferences own appearance, density, branding, palettes (including the original PakarController schemes), activity/task icon and one or both launcher identities. Both launcher entries must open the same unified application. Check actual Android icon/alias behavior rather than claiming a Dart preference changed OS state.
- DNS-SD/mDNS, SSDP and MNDP must be implemented/coordinated across PCController, Pealayer and Flutter, with real service identity and endpoint verification. Auto-connect valid discovered peers according to saved user intent, but never treat a raw advertisement as proof, silently replace a live selected peer, take board authority or actuate outputs.
- Android peers must support bounded, verified app-package upload/download and upgrades through the existing IPC/RPC transport. Preserve package identity/signature checks, unknown-outcome handling and Android user-consent/install permissions; no silent install or arbitrary path/URL proxy.
- Source/build/device screenshots are separate from live transport, physical/audio acceptance and David's design acceptance. Keep rejected/incomplete gates open, preserve concurrent authoring work and record source provenance before deployment.
