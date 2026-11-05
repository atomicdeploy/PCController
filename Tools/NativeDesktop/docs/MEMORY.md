# PCControllerDesktop memory profile

This document records the July 2026 RAM investigation and the invariants that
future rendering, animation, and server changes must preserve.

## Reproducible profile

The GUI workload was the 176×176 compact window using acrylic, layered
foregrounds, animations, tray-menu prewarming, and the automatic DirectX
backend. Samples were taken from a clean process after 30 seconds on Windows 11
Enterprise build 26100, an Intel i5-13400F, and an NVIDIA GTX 1080. Working set
is resident physical memory; private memory is committed memory owned only by
the process. Driver and DirectComposition allocations can affect the latter
without appearing in Go heap profiles.

| Workload | Working set | Private memory | Threads | Handles | CPU at 30 s |
| --- | ---: | ---: | ---: | ---: | ---: |
| Original automatic GUI | 166.63 MiB | 197.89 MiB | 40 | 665 | 9.56 s |
| Optimized automatic GUI | 83.30 MiB | 123.86 MiB | 37 | 556 | 2.77 s |
| Optimized compatibility GUI | 59.71 MiB | 70.85 MiB | 22 | 384 | 2.41 s |
| Automatic GUI while hidden | 69.57 MiB | 87.48 MiB | 23 | 448 | idle |
| Headless `serve` coordinator | 27.01 MiB | 56.06 MiB | 16 | 199 | 0.05 s |

The optimized automatic path reduced the matched working set by 83.33 MiB
(50.0%), private memory by 74.03 MiB (37.4%), and startup/idle CPU by 71.0%.
The compatibility backend kept the same UI features while reducing private
memory by a further 53.01 MiB in this profile. Its 59.71 MiB working set is
64.2% below the original GUI. GPU drivers and Windows memory pressure make
individual GUI runs vary by several MiB, so regressions should be judged from
repeated clean-process samples rather than one Task Manager reading.

Animation preparation is deliberately asynchronous. In the automatic sample,
the one-second transient was 146.59 MiB working set / 192.12 MiB private; it
fell to 81.62 / 126.59 MiB at three seconds and stabilized thereafter. A GC
trace confirmed that the final delayed release reduced live Go heap from a
61 MiB animation-preparation cycle to 6 MiB. This is real reclamation rather
than cosmetic working-set trimming.

## Root causes and fixes

- The 30-frame 512×512 APNG retained exactly 30 MiB of composited source
  pixels, even when animation was disabled. Decode also copied the 5.4 MiB
  embedded file. Animation is now loaded only when enabled, streams from the
  executable, builds the exact active-size native cache, then evicts the raw
  frames without changing frame order, timing, pixels, or resize behavior.
- Lanczos scaling expanded whole source and intermediate images into
  32-byte-per-pixel floating-point buffers. It now converts concrete
  NRGBA/RGBA sources without per-pixel heap allocations and keeps only the
  horizontal rows required by the current vertical filter window. Pixel-parity
  tests compare it byte-for-byte with the original full-buffer algorithm.
- Acrylic alpha recovery painted over black and white, then allocated multiple
  full-window readback/result images every frame. Size-and-DPI-keyed GDI and Go
  surfaces are now reused. Hidden windows release those CPU surfaces and the
  DirectX/DirectComposition presenter; showing the window recreates them.
- Walk's bitmap readback acquired a screen DC without releasing it. Every
  acquisition now has a tested `ReleaseDC`, and the hot path reads into a
  caller-owned pixel buffer.
- Custom WAV data was an unbounded in-memory history. Only the selected sound
  remains resident; content-addressed files remain authoritative and can be
  reselected exactly.
- UI-only settings changes created throwaway device clients, replaced HTTP
  transports retained idle connections, and diagnostics subscribers queued
  stale full snapshots. These are now lifecycle-bounded and latest-only.
- WebSocket/terminal queues are bounded, diagnostics JSON is not serialized
  with no listeners, and embedded Web files stream with cached metadata instead
  of being copied and rehashed per request.
- The unified executable necessarily imports the TUI before launch-mode
  routing. Its former one-use Glamour diagnostics renderer therefore eagerly
  initialized Chroma's lexer/style registries and Goldmark in GUI and headless
  modes too. A Windows/amd64 no-test init trace measured 4,570,336 bytes across
  65,447 initialization allocations for that family (3,953,568 Chroma,
  150,632 Glamour, and 466,136 Goldmark); the current graph measures zero for
  those packages because they are absent. This is allocation volume during
  package initialization, not a claim of equal retained heap or Task Manager
  working-set savings. A purpose-built Lip Gloss JSON/text renderer preserves
  sanitization, color, wrapping, and light/dark behavior without that eager
  stack.
- After a large decoded animation arena becomes unreachable, one delayed,
  coalesced `debug.FreeOSMemory` pass returns it to Windows outside `WM_PAINT`.
  It is never run per frame or per event.

## User-selectable lower-memory paths

In **Settings → Appearance → Rendering backend**, choose
**Compatibility renderer (lower RAM)**. It retains the acrylic/layered UI,
animations, controls, hit testing, and other application features; the tradeoff
is GDI/`UpdateLayeredWindow` presentation instead of GPU
DirectComposition. **Automatic** remains the default for hardware acceleration.

Hiding the main window now releases its DirectX presenter and reusable render
surfaces while the tray, hotkeys, IPC/API, automation, notifications, device
connection, and Web UI continue running. The resources are recreated when the
window is shown.

For a machine that needs only the browser dashboard/coordinator,
`PCControllerDesktop.exe serve --no-open` avoids the native GUI and tray entirely. Turning
off animations also avoids the APNG/native-frame cache, but that is an explicit
visual-feature tradeoff rather than a lossless memory mode.

The canonical build edition is currently `develop`. A `production` build keeps
all runtime features while omitting native symbols/DWARF and browser source
maps and minifying the embedded SPA. This primarily reduces executable and
debug-payload footprint; it is not a substitute for the measured runtime
rendering choices above.

Do not add `SetProcessWorkingSetSize` or periodic forced GC. Working-set
trimming only makes Task Manager look smaller by discarding useful pages and
causes faults/jank on the next interaction; per-frame GC has the same problem.
