# Changelog

- Added explicit unified-EXE `/install`, `/install-status`, and `/uninstall`
  lifecycle commands. Portable launch remains write-free; installation uses a
  verified same-volume staged swap, integrations are opt-in, and uninstall
  performs ownership-safe native cleanup without PowerShell.

All notable changes to RayanLamp are documented here. The project follows
[Semantic Versioning](https://semver.org/).

## [Unreleased]

- Fixed the thick white captionless frame exposed by the DirectX host's
  ordinary non-client paint path, and stale white GDI backing when switching
  from native to custom chrome. Hardware composition remains enabled.
- Fixed Settings first-open collapse after hidden localization/layout and
  Walk's first-show minimum-size pass. Settings now remembers its own logical
  size and screen position, including Cancel and ownerless opens, with
  monitor-work-area recovery and taskbar coordinate conversion.
- Added screenshot-backed native lifecycle tests, a Common Controls manifest
  test runner, and safeguards for chrome/network message-ID collisions and
  zero-valued window styles with stale Win32 last-error state.
- Added one canonical `develop`/`production` build-edition switch shared by
  PowerShell, Linux cross-builds, Make, CI, Go build tags, and the embedded
  Vite client. Development retains native symbols/DWARF, readable JavaScript,
  and source maps; production uses `-s -w`, Oxc minification, and no source
  maps without removing product features. PE verification now rejects
  mismatched native/Web editions and incorrect debug payloads. The optional
  native SDK remains an independent, always-stripped production artifact.
- Replaced the TUI diagnostics screen's one-use Glamour pipeline with a
  purpose-built, sanitized Lip Gloss JSON/text renderer. This preserves
  color, wrapping, light/dark palettes, and bounded output while preventing
  Chroma and Goldmark registries from initializing in every GUI/headless
  process before launch-mode routing.
- Added application-wide English/Persian localization with `ui.locale`
  (`auto`, `en`, `fa`), live language switching, native and Web RTL geometry,
  Persian digits/calendar presentation, bidi-safe technical values, localized
  toast actions, and complete TUI/Web/native dialog coverage. Integrated the
  YekanBakh variable font as one canonical embedded asset and tightened the SPA
  at 320/390-pixel phone, 768-pixel tablet, and desktop breakpoints.
- Reworked the terminal UI rendering and input loop after real Windows Terminal
  and ConPTY validation. Transparent Lip Gloss surfaces no longer inherit
  opaque black or white ANSI backgrounds, menus complete on one Enter,
  printable filter input cannot leak into global shortcuts, Escape unwinds
  filters and workspaces predictably, and Ctrl+C quits everywhere except the
  raw device terminal where it correctly sends ETX. Idle screens no longer
  spin or repaint continuously; loading, saving, and terminal operations keep
  their animation through a runtime-only scheduler. Added full Bubble Tea
  program traversal tests plus a pure-Go Windows ConPTY smoke tool covering
  every workspace, real key bytes, live resize, and terminal cleanup.
- Added adaptive native rendering with `ui.render_backend` values `auto`,
  `directx`, and `legacy`. Compatible systems use dynamically loaded,
  pure-Go D3D11, DXGI, and DirectComposition; unsupported initialization,
  presentation failures, and graphics-device loss fall back safely to the
  retained GDI/`UpdateLayeredWindow` path while preserving exact alpha-zero
  click-through. Rendering never snapshots or reads back the desktop, and the
  Web device terminal now prefers WebGL with an automatic canvas fallback.
- Reduced the matched compact-acrylic working set by 50% and startup/idle CPU
  by 71% without removing features. APNG source pixels are streamed,
  actively cached, and evicted; Lanczos and acrylic readback use bounded,
  reusable row/surface storage; hidden windows release their presentation
  resources; custom audio, diagnostics, HTTP clients, WebSocket queues, and
  embedded Web delivery now have explicit memory lifetimes. The compatibility
  renderer is labeled as the lower-RAM choice, with a reproducible memory
  profile and regression contract in `docs/MEMORY.md`.
- Added a shared sound-effects system with deterministic bundled
  on/off/message/error PCM WAVs and validated custom-WAV import. Native GUI,
  TUI, headless, coordinator Web UI, and direct/static Web playback now use the
  same semantic cue/configuration model; audio remains independent of toast
  delivery, and direct multi-tab playback is owned by only the elected tab.
  Custom coordinator assets are content-addressed by SHA-256, while direct Web
  mode uses the same scheme in browser storage.
- Consolidated runtime assets behind one canonical bundle. The embedded Web
  host and deterministic static exporter reuse the native WAV catalog and
  512-pixel app artwork instead of carrying copied Web variants; the product
  ICO remains a PE-resource build input rather than being embedded again in
  Go, and unused raster/ICO packs are excluded from the runtime image.
- Added `--audio --cue on|off|message|error --count 1..10`, matching
  `audio.play` IPC and `POST /audio/play` JSON parameters, plus authenticated
  Web endpoints for selected-cue streaming and custom-WAV import.
- Fixed the generic icon on Task Manager's collapsed/root RayanLamp process
  row. Walk window classes now load large and small icons from the executable's
  standard embedded application-icon resource, while retaining legacy-resource
  compatibility, and the PE release gate rejects binaries missing that icon.
- Added an optional, versioned native SDK DLL for amd64 and 386 plus a public
  attach-only Go client. The bounded C/UTF-16 ABI exposes generic dispatcher
  calls, common lamp commands, state, availability, explicit primary
  bootstrap, owned-buffer release, Windows version resources, verified exports,
  native smoke tests, and Windows/Linux MinGW build pipelines without changing
  the ordinary single-executable release.
- Added live taskbar integration: thumbnail-toolbar Toggle/On/Off/Reconnect
  controls, Explorer-restart recovery, DPI-aware embedded icons, descriptive
  thumbnail tooltips, and operation/connection/suspend progress states. Native
  DWM previews remain live and no screenshot/iconic-bitmap path is used.
- Added Windows lock, unlock, suspend, resume, and debounced IP-interface
  change handling. Recovery is health-gated, explicit reconnect invalidates the
  cached `.local` address, and each event has an optional bounded automation
  script exposed consistently in native, Web, and terminal settings.
- Added opt-in, per-user Windows shell integration: a strict bounded
  `rayan-lamp://` command allowlist, native unpackaged Jump List tasks, a
  stable process AppUserModelID, ownership-safe registration cleanup, and
  noninteractive installer support without changing portable-mode defaults.
- Fixed synchronous console ownership for the unified executable. The release
  is now a Windows console/CUI image, so interactive `cmd.exe` and PowerShell
  wait for CLI, TUI, and server completion and receive the real exit code.
  Windows 11 24H2 uses the detached-console manifest policy for flash-free
  Explorer GUI startup; older Windows versions conservatively release only a
  console exclusively owned by RayanLamp and never hide a console window.
- Unified CLI, TUI, headless-server, PE-verifier, PowerShell, and Bash terminal
  presentation around capability-aware VT/ANSI output with complete SGR
  resets, Windows console activation, `NO_COLOR`/`TERM`/`CLICOLOR`/forced-color
  controls, and tasteful emoji with explicit or automatic ASCII fallback.
  Redirected TUI launches now fail before emitting alternate-screen or cursor
  controls, and pipeline tests reject raw escape leakage and malformed resets.
- Added pure-Go Linux-hosted cross-build support for Windows amd64, 386, and
  ARM64 through `tools/build-linux.sh` and `make windows-cross`. The pipeline
  rebuilds the embedded PWA, generates architecture-correct icon,
  VERSIONINFO, and manifest resources without Wine or a C toolchain,
  cross-compiles Windows tests, structurally verifies the final PE, and runs
  as a three-architecture GitHub Actions matrix alongside the Windows build.
- Completed the installable mobile PWA shell with a portable Web Manifest,
  standalone/Apple metadata, exact 180 px touch artwork, dynamic viewport and
  display-cutout safe areas, accessible zoom, and globally suppressed mobile
  tap highlighting. Compatible foreground browsers now provide restrained
  vibration feedback for explicit command progress and normalized
  message/audio events; unsupported devices, hidden pages, reduced-motion
  preferences, bootstrap, polling, and raw payloads remain silent.
- Added a polished, theme-aware Web DevTools console with CSS-styled badges,
  explicit foreground/background colors, useful emoji, grouped metadata, and
  appropriately routed `console.debug`, `info`, `warn`, and `error` events.
  The TUI activity stream now shares the same event names and
  debug/info/success/warn/error vocabulary, while sensitive and high-volume
  data remains excluded.
- Moved the shared Show/Hide main-window command to the bottom of both
  application menus, immediately above Exit Rayan Lamp.
- Removed Settings live-drag stalls and mixed-DPI resize jumps with
  generation-tagged, latest-only Walk layouts, owner-monitor first-show
  geometry, monitor-bounded minimum tracking, and a scrollable high-DPI tab
  surface. Dropdown native content and rounded app chrome now compose
  offscreen and commit atomically, popup theming is cached, and fills are
  clipped to the exact Win32 region. Tab changes stage only the selected
  destination, preserve transparent checkbox/radio backgrounds, and perform
  one final live handoff. Shared tray/main menus now prewarm current-monitor
  plus 100–400% icon sizes in parallel, skip unchanged composition work, and
  use a shorter reveal plus immediate perceptual close.
- Vertically centered Web UI toast status icons, one- or two-line message
  content, and dismiss controls on the same visual axis.
- Made coordinator shutdown immediately observable in every Web UI tab. The
  event stream now closes first with a graceful Going Away signal, stale lamp
  controls become unavailable at once, and reconnect renews the browser
  session after an executable restart. Browser favicons now use crisp,
  cache-safe SVG artwork for connecting, lamp-on, lamp-off, and offline state.
- Removed the `/v1` prefix and API-visible protocol-version fields from the
  loopback HTTP/WebSocket surface. The coordinator now exposes one living,
  versionless protocol at `/status`, `/lamp/*`, `/ui/*`, `/events`, and the
  other documented root routes; legacy `/v1/...` aliases intentionally return
  `404 Not Found`.
- Unified native, web, PWA, and terminal appearance around the canonical
  design tokens and the shared system/light/dark preference. Open windows and
  cached menus now react live to Windows color, High Contrast, transparency,
  and reduced-motion changes; the web shell avoids first-paint theme flashes
  and keeps browser chrome and direct-mode tabs synchronized.
- Initial release engineering, per-user installer/uninstaller, CI workflow,
  firmware mock, and integration documentation.
- Added a full-screen Charm terminal UI using Bubble Tea, Lip Gloss, Bubbles,
  and Huh, with responsive views, keyboard/mouse operation, a
  read-only diagnostics overlay, attach-through-IPC/API modes, and an
  authoritative standalone mode.
- GUI, command client, terminal UI, standalone headless primary, and COM server
  now ship as exactly one pure-Go `RayanLamp.exe`, with explicit `cli` and
  `tui` routing plus optional verified amd64/386 UPX compression.
- Settings are now modeless and single-instance, keep the lamp window
  interactive, save asynchronously, restore previews transactionally, remember
  the selected page, and expose none/custom/native title bars plus distinct
  solid and fully transparent backdrop choices.
- Replaced free-form hotkey fields with coordinated native and web shortcut
  recorders featuring live keycaps, broad keyboard/media-key support,
  reserved-key and duplicate feedback, accessibility announcements, temporary
  global-hotkey suspension, and Windows availability preflight.
- Added distinct, DPI-aware Fluent icons to every native Settings tab, with
  animated state tinting and Windows 10 MDL2 fallback.
- Rebuilt application, state, and menu artwork as validated 16-frame ICO packs
  with exact 100–400% DPI sizes and optically simplified caption variants.
  Settings/Diagnostics title bars, tray/taskbar state icons, animated APNG
  frames, and in-window graphics now use alpha-aware high-quality scaling and
  exact-size caches instead of GDI or nearest-neighbor stretching.
- Fixed transparent, borderless windows retaining a full-HWND DWM shadow;
  transparent-to-material changes now commit immediately without a drag, and
  native Blur/Acrylic captions reapply their final theme and stable product
  icon after every frame or DPI transition.
- Fixed square DWM/Accent bleed behind the custom tray and main context-menu
  corners by restoring the final Windows 11 round compositor contour and
  aligning every app-owned popup mask to the same 8-DIP geometry.
- Smoothed popup reveal, hover, and dismissal plus Settings tab transitions
  with elapsed-time, coalesced frame scheduling; persistent GDI buffers and
  cached menu content remove per-frame resource churn, while tab navigation no
  longer reapplies the complete runtime configuration on the UI thread.
- Added an app-owned, double-buffered Settings page compositor that stages
  native child controls before an atomic crossfade/slide handoff. Dialog
  buttons now use cached Fluent/MDL2 semantic icons and deterministic
  mnemonics, while EDIT and COMBOBOX controls gain rounded token-driven
  frames, focus/hover motion, and polished chevrons without giving up native
  keyboard, IME, clipboard, type-ahead, or accessibility behavior. Live High
  Contrast changes now restore or reapply native-compatible chrome without
  reopening Settings.
- Added a polished Hide/Show main-window action to the shared tray/main menu
  and COM surface. Normal restore bounds, hidden visibility, and native
  normal/minimized/maximized state now survive clean exit and restart; command-
  only background launches do not overwrite the remembered session, removed
  monitors are recovered safely, and transient maximize/restore rectangles are
  never persisted.

## [1.0.0] - 2026-07-23

### Added

- Native Go Windows application and console companion with no CGO dependency.
- Lamp control over HTTP and continuous firmware events over WebSocket.
- Device-pixel SVG steady states, 512 px APNG-powered lamp transitions, and
  embedded original tray artwork.
- Notification-area controls, global hotkeys, always-on-top, startup, and
  magnetic window snapping.
- Resizable compact and dashboard surfaces with optional composited title
  controls, adaptive auto/Mica/tabbed/acrylic/blur/solid rendering, Fluent
  glass controls, and accessibility-aware fallbacks.
- Modern Windows toasts, legacy notification balloons, and audible cues.
- Live-preview configuration modal and safe asynchronous diagnostics viewer
  with a rounded native frame, composition-safe Fluent material, themed
  controls, and explicit readable dark/light tab rendering.
- Shared command dispatcher exposed through the CLI, current-user named pipe,
  WM_COPYDATA handoff, bearer-authenticated loopback HTTP API, and optional
  per-user COM Automation.
- Bounded event automation for PowerShell, command, and executable scripts.
- Cached `.local` resolution with invalidation on network and WebSocket
  failures, bounded exponential reconnect, and optional HTTP polling.
- Reproducible single-executable release scripts with Windows icon, manifest,
  version metadata, tests, vet, and logged SHA-256 hashes.

[Unreleased]: https://github.com/AtomicDeploy/RayanLamp/commits/main
[1.0.0]: https://github.com/AtomicDeploy/RayanLamp/blob/main/CHANGELOG.md
