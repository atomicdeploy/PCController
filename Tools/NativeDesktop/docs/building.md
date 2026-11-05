# Building, packaging, and installation

## Supported targets

The module declares Go 1.25.8. Go 1.21 and later support Windows 10 / Windows
Server 2016 and newer; that is RayanLamp's operating-system baseline. See the
official [Go minimum requirements](https://go.dev/wiki/MinimumRequirements).

The release scripts accept:

- `amd64` (default and CI release target);
- `386`;
- `arm64`.

The Windows release path runs tests and vet with the host Windows architecture.
The Linux cross-build path runs every suite supported by Linux, vets the
selected Windows target, and cross-compiles every Windows test package without
trying to execute Windows binaries on Linux. Each target receives a matching
COFF resource object. CGO is forced to zero for validation and compilation.
The adaptive Windows renderer dynamically loads the operating system's D3D11,
DXGI, and DirectComposition entry points at runtime. No Windows SDK import
library, DirectX redistributable, CGO bridge, or C/C++ compiler is part of the
ordinary executable build; an unavailable graphics stack simply selects the
retained GDI/`UpdateLayeredWindow` renderer.

The bundled browser client requires Node.js 20.19+ within the 20.x line, or
22.12+, and npm 10+. CI uses Node.js 24. Exact frontend dependencies are locked by `webui/package-lock.json`;
the generated `internal/webui/dist` is committed so an offline Go-only source
build can explicitly reuse the reviewed distribution. Its xterm terminal
loads the WebGL addon opportunistically and keeps the ordinary canvas renderer
as the no-WebGL and context-loss fallback.

## Build editions

`build-edition.txt` is the single canonical default and currently contains
`develop`. The PowerShell, Linux, Make, and direct Vite build paths all read
that same file. Later, changing it to `production` changes the default
everywhere; an explicit command-line edition always wins.

The editions contain the same application features:

| Edition | Native Go payload | Embedded Web payload | Intended use |
| --- | --- | --- | --- |
| `develop` | Optimized code with symbols and DWARF retained | Readable chunks and source maps | Current development, debugging, and crash investigation |
| `production` | `-s -w`; symbols and DWARF removed | Oxc-minified chunks and no source maps | Final distribution and smallest normal executable |

Neither edition uses `-N -l`, so the development build remains representative
for performance testing. Production does not remove GUI/tray, CLI, TUI,
headless serving, diagnostics and raw endpoint inspection, IPC/API/COM,
automation, terminal, notifications, audio, self-installation, or Windows
integration. `runtime/debug` also remains because its delayed
`FreeOSMemory` call is a product memory optimization, not a developer
endpoint. No pprof, trace, or expvar server is linked into either unified
executable.

Every build embeds `build-edition.json` in the SPA. The PE verifier rejects a
native/Web edition mismatch, a development executable without symbols/source
maps, or a production executable that retains native debug sections. Build
tags `rayanlamp_develop` and `rayanlamp_production` are mutually exclusive.

Explicit selection:

```powershell
.\tools\build.ps1 -Edition develop
.\tools\build.ps1 -Edition production
.\tools\package.ps1 -Edition production -Force
```

```bash
make windows-cross-develop FORCE=1
make windows-cross-production FORCE=1
bash ./tools/build-linux.sh --edition production --force
```

## Release build

```powershell
.\tools\build.ps1
```

The script:

1. Reads the canonical semantic version from
   `internal/buildinfo/buildinfo.go`.
2. Rejects a requested version that differs from that source constant.
3. Validates strict filename-safe SemVer and takes an exclusive release-build
   lock.
4. Sets `GOOS=windows` and enforces `CGO_ENABLED=0`.
5. Runs `npm ci`, TypeScript type checking, frontend unit tests, and the
   selected development or production Vite/PWA build.
6. Runs tagged `go vet ./...` and `go test -count=1 ./...`.
7. Creates temporary version JSON and manifest copies for the selected
   version/build number.
8. Runs pinned
   [`goversioninfo` v1.7.0](https://github.com/josephspurrier/goversioninfo)
   for the unified executable.
9. Uses `assets/RayanLamp.ico` once as the PE's standard application-icon
   resource alongside the Per-Monitor-V2/UTF-8/as-invoker/detached-console
   manifest and Explorer file properties. The ICO is not embedded again in
   Go's runtime asset filesystem.
10. Embeds the compiled SPA/PWA, shared design tokens, manifest, service
    worker, CSS, JavaScript, required Web icon variants, and the canonical
    native/Web SFX and artwork bundle into the same executable.
11. Builds exactly one console-subsystem `RayanLamp.exe`; the same PE contains
    the native GUI, CLI, and TUI modes.
12. Optionally compresses amd64/386 with UPX and immediately verifies the
    packed image.
13. Uses `-trimpath`, disables VCS auto-stamping, applies `-s -w` only for
    production, injects the commit and UTC build timestamp, writes only
    `dist\RayanLamp.exe`, and reports its SHA-256 to the build log.
14. Removes only the temporary `.syso` object created by that locked build,
    even when it fails.

The console/CUI subsystem is intentional: an invoking shell waits for the real
process and exit code. On Windows 11 24H2, Windows Server 2025, and later
releases, the manifest's detached console-allocation policy prevents automatic
allocation outside an existing console session while preserving CUI shell-wait
semantics. On older supported releases, GUI startup detaches only a console the
process owns exclusively and retains inherited/shared consoles; those systems
can still show a brief pre-main flash.

Release output:

```text
dist\RayanLamp.exe
```

Useful options:

```powershell
.\tools\build.ps1 -Architecture arm64
.\tools\build.ps1 -Edition production
.\tools\build.ps1 -BuildNumber 42 -Commit abc123 -BuildDate 2026-07-23T08:00:00Z
.\tools\build.ps1 -Compress
.\tools\build.ps1 -UPXPath C:\tools\upx\upx.exe
.\tools\build.ps1 -Force
```

## Interactive TUI smoke test

After building on Windows, exercise the packaged executable through an actual
Windows pseudoconsole:

```powershell
go run ./tools/tui-smoke -exe .\dist\RayanLamp.exe -verbose
```

The smoke tool creates an isolated temporary configuration, sends real terminal
key bytes through ConPTY, visits the dashboard, command center, application
settings, device settings, serving/integration, diagnostics, and device
terminal, navigates selectable menus, resizes through 80×24, 40×12, 23×7, and
120×40, and then verifies a bounded exit plus balanced alternate-screen and
cursor controls. It uses the pure-Go `x/sys/windows` ConPTY API and does not
require CGO. Add `-transcript .\work\tui-smoke.vt` only when a raw VT audit
record is useful; transcripts are not written by default.

## Cross-build Windows from Linux

No Wine, MinGW, Windows SDK, or C compiler is needed. Install the Go version
declared by `go.mod`, Node.js 20.19 or newer, npm 10 or newer, GNU Make, and the
ordinary Linux utilities `bash`, `awk`, `sed`, `grep`, `date`, `flock`,
`mktemp`, and `sha256sum`. Then run:

```bash
make windows-cross FORCE=1
make windows-cross-production FORCE=1
```

This produces the same single `dist/RayanLamp.exe`, including the rebuilt
SPA/PWA, multi-resolution icon, Windows version properties, application
manifest, and runtime CLI/TUI support. The cross-build uses the pinned
`goversioninfo` Go tool natively on Linux to generate the correct COFF object,
then links the application with `GOOS=windows` and `CGO_ENABLED=0`. DirectX
support does not alter that cross-build: the resulting pure-Go executable
resolves the Windows-owned D3D11, DXGI, and DirectComposition APIs only when it
runs on the target machine.

All release architectures are available:

```bash
make windows-cross ARCH=386 FORCE=1
make windows-cross ARCH=arm64 FORCE=1
make windows-cross-production ARCH=amd64 FORCE=1
bash ./tools/build-linux.sh --architecture amd64 --edition production --build-number 42 --force
```

Optional UPX compression is enabled with `COMPRESS=1` or `--compress`. It is
used only when `upx` is on `PATH`, is immediately checked with `upx -t`, and is
intentionally skipped for ARM64. `--upx /trusted/path/to/upx` selects an
explicit binary. Compression remains optional and the build succeeds
uncompressed when an implicit `upx` lookup finds nothing.

Before publishing, the Linux script uses the standard-library PE inspector in
`tools/verify-pe` to confirm:

- the selected amd64, 386, or ARM64 machine type;
- the Windows console/CUI subsystem;
- icon-image and icon-group resources;
- version information and the exact four-part file version;
- the application manifest identity, version, and detached-console policy;
- markers for the embedded SPA/PWA distribution;
- the selected Go build tag, native symbol/DWARF policy, and matching Web
  edition marker.

You can repeat that inspection independently:

```bash
make verify-windows ARCH=amd64 BUILD_NUMBER=0
```

The Linux host cannot execute the resulting Windows application directly.
Windows-specific behavior remains covered by the Windows CI job; the Linux
build additionally cross-compiles every Windows test binary so target-only
compile regressions fail before linking the release.

`--skip-tests`, `--skip-vet`, and `--skip-web-build` support a previously
validated local iteration. `--skip-web-build` fails unless the complete
checked-in web distribution is present. The script refuses unexpected files
in `dist`, holds an exclusive build lock, generates resources through unique
temporary paths, and atomically publishes only `RayanLamp.exe`.

Build messages use emojis plus capability-aware VT-100 SGR sequences. Set
`NO_COLOR` to suppress escape codes in redirected logs, or
`FORCE_COLOR=1` or `CLICOLOR_FORCE=1` to keep color in a CI service that
supports ANSI output. Set `NO_EMOJI=1` for the stable ASCII labels, or use
`RAYANLAMP_EMOJI=always|never` to explicitly choose emoji policy for redirected
logs. The same controls apply to the PowerShell release path; its human
statuses use the information stream, preserve native child-process output,
and leave the final success-stream object as the artifact path.

`-SkipTests` and `-SkipVet` exist for a developer's already-validated local
iteration; they should not be used for published artifacts. The script builds
through unique temporary paths and only replaces the exact
`dist\RayanLamp.exe` when `-Force` is explicit.

`-SkipWebBuild` is an offline source-build escape hatch. It is accepted only
when the complete checked-in `internal\webui\dist` already exists; it never
creates a placeholder dashboard. Published artifacts should always rebuild
the client from its lockfile.

For frontend-only iteration:

```powershell
.\tools\build-web.ps1
cd webui
npm run dev
```

To produce the frontend-only, direct-first static artifact:

```powershell
.\tools\export-web.ps1
```

This performs a separate Vite build in a validated temporary directory with
`VITE_RAYANLAMP_STATIC_EXPORT=true`, then creates
`dist\RayanLamp-Web-Static.zip`. It does not replace or modify the reviewed
assets embedded in `RayanLamp.exe`. ZIP entries are sorted, use a fixed
1980-01-01 timestamp and portable 0644 mode, exclude source maps and symlinks,
and include an embedded deployment/security README. The archive is written to
a synchronized temporary sibling and atomically replaces only that exact
output after validation. Repeating the export from identical sources produces
identical bytes.

The Web build does not copy the four WAVs or the ordinary 512-pixel app icon
into Vite's generated directory. The Go Web host serves those entries straight
from the canonical runtime bundle, and the static exporter inserts each one
exactly once under `shared/sfx/` and `shared/icons/app.png`. The separate
maskable, 192-pixel, and Apple-touch files are purpose/size-specific artwork,
not duplicate copies of that ordinary 512-pixel icon.

Run `package.ps1` before `export-web.ps1` when producing both deliverables.
The release packager intentionally insists that `dist` contain only
`RayanLamp.exe`; move or remove the optional static ZIP before invoking the
single-executable packager again. `export-web.ps1 -SkipTests` is available only
for an already-validated local iteration.

The development server uses `/app/` and proxies REST, session, and WebSocket
traffic to `127.0.0.1:47831`, rewriting the development proxy Origin to the
exact protected backend origin. Production never enables cross-origin access.
Vite generates source maps only for the `develop` edition; production and the
static ZIP contain none. The service worker precaches only the
fingerprinted shell, canonical SFX, and art; `/app/session` and every
versionless coordinator API route are network-only.

`-Compress` searches `PATH` for UPX and warns, then safely leaves the executable
uncompressed, when it is unavailable. `-UPXPath` selects an explicit trusted
UPX binary and also enables compression. Packing uses `--best --lzma` followed
by UPX's integrity test, and is intentionally limited to amd64/386; ARM64 is
always left untouched. UPX keeps the artifact self-contained, but may increase
startup CPU, private memory, and page faults, reduce antivirus reputation,
complicate crash forensics, and
invalidates any existing Authenticode signature. Compress before signing and
prefer the ordinary uncompressed build where size is not important.

## Single-file release and hash

```powershell
.\tools\package.ps1 -Force
```

`package.ps1` is a release-facing alias for the same validated build. It
follows `build-edition.txt` unless `-Edition` is supplied, and emits only:

```text
dist\RayanLamp.exe
```

No README, installer, uninstaller, DLL, runtime, hash sidecar, or ZIP is placed
beside it. The command prints the executable's SHA-256 to the build log and
fails closed unless `dist` contains that one file and nothing else. Recompute
the hash when needed:

```powershell
Get-FileHash .\dist\RayanLamp.exe -Algorithm SHA256
```

Hashes detect corruption; they are not signatures. Published releases should
add Authenticode signing in a protected release job if publisher identity and
tamper resistance are required.

## Optional native SDK

The external DLL is a separate, opt-in artifact and never changes the
single-file portable release. It is always built with the production tag and
stripped symbols because it contains neither the embedded Web UI nor the
application's development payload; use a separate SDK-symbol workflow if
native DLL debugging is needed:

```powershell
.\tools\build-sdk.ps1 -Architecture all -Force
```

Linux cross-builds use `make windows-sdk-cross ARCH=amd64` or `ARCH=386`.
See [Native and Go SDK](sdk.md) for the ABI, attach-only lifecycle, validated
MinGW requirements, artifacts, and host examples.

## Per-user installation

The single executable has an explicit per-user self-installer:

```powershell
.\dist\RayanLamp.exe /install /with-startup /with-shell /with-com /launch
.\dist\RayanLamp.exe /install-status /json
```

`/install` alone installs only the file. `/with-startup`, `/with-shell`, and
`/with-com` independently opt in to persistent Windows integration; `/launch`
starts the installed copy after commit. `/install /force` performs an in-place
update.

The self-installer:

- accepts exactly the unified `RayanLamp.exe`;
- verifies the staged copy against the source SHA-256;
- stages into a unique sibling directory;
- installs exactly to `%LOCALAPPDATA%\Programs\RayanLamp`;
- requires `/force` before replacing an existing installation;
- asks the existing controller to quit, swaps directories, and rolls back if
  the new installation fails;
- optionally creates only the current user's Run value;
- optionally registers the current executable's `rayan-lamp://` URI handler
  and native Jump List tasks;
- optionally creates ownership-checked per-user COM registration for the
  installed executable;
- never requires elevation and never writes HKLM.

It also rejects reparse-point, incomplete, or non-single-EXE installation
directories instead of recursively replacing unexpected content. File swaps
and rollback directories stay on the same LocalAppData volume. The current
primary instance is asked to exit before the destructive boundary.

The repository helper remains useful in release automation and accepts the
equivalent PowerShell switches:

```powershell
.\tools\install.ps1 `
  -SourcePath .\dist\RayanLamp.exe `
  -StartWithWindows -RegisterShellIntegration -RegisterCOM -Launch
```

The installer invokes requested registration operations noninteractively and
checks their exit codes. Manual `/register-shell`, `/unregister-shell`,
`/register-com`, and `/unregister-com` commands retain their confirmation
dialogs unless `/quiet` is supplied. The installer does not silently opt users
into shell integration, COM, or startup.

`-StartWithWindows` refuses to overwrite an existing `RayanLamp` Run value
whose exact quoted command points elsewhere. If a later install phase fails,
rollback removes only the value newly written by that attempt and rechecks its
command first; a value whose ownership changed concurrently is preserved.
Normal GUI/TUI startup performs only a read-only ownership probe and never
repairs or removes the Run value implicitly.

Windows policy can reject Jump List publication while still allowing the URI
handler. That useful partial result exits with code `3`; the installer warns
and keeps the URI integration. Other registration failures trigger normal
installer rollback.

## Uninstall

The installed or portable unified executable can remove the fixed per-user
installation:

```powershell
RayanLamp.exe /uninstall
```

Non-quiet use requires a native confirmation. `/quiet` is available for
explicit automation. The uninstaller removes exactly
`%LOCALAPPDATA%\Programs\RayanLamp`. It removes the Run value, URI/Jump List
integration, legacy owned shell metadata, and COM keys only when they point to
that exact executable; registrations that point elsewhere are preserved with
a warning. It then starts a detached copy of the same Go executable from the
Windows temporary directory, waits for the uninstaller to exit, revalidates
the exact one-EXE directory, removes it, and unlinks the helper with native
Windows file-disposition semantics. This cleanup path invokes no PowerShell,
does not request elevation, and never schedules reboot-time deletion.
Configuration and logs remain by default.

To also remove the exact `%APPDATA%\RayanLamp` data directory:

```powershell
RayanLamp.exe /uninstall /remove-user-data
```

That flag permanently removes the API token, settings, content-addressed
custom SFX, automation log, and toast cache. Back up the configuration first
if needed.

The source-tree `.\tools\uninstall.ps1` helper remains available as a recovery
and release-automation entry point.

## Shared assets and regeneration

`assets/embed.go` is the single reviewed runtime bundle for the native UI,
sound manager, embedded Web host, and static Web exporter. Keep source masters
on disk without broad wildcard embedding. In particular,
`assets/RayanLamp.ico` is a PE build input, animation PNG frames are design
sources, and unused raster/ICO variants must not silently become a second
runtime copy.

The artwork is generated procedurally by `tools/generate-assets.ps1`; no stock
icons or third-party image packs are used. It requires ImageMagick 7's
`magick` command:

```powershell
.\tools\generate-assets.ps1 -FrameCount 24
```

It regenerates:

- `assets/animation/lamp_000.png` through the final frame;
- `assets/lamp-transition.apng`;
- `assets/lamp-transition.svg`;
- `assets/lamp-off.svg` and `assets/lamp-on.svg`;
- the app, state, connecting, and error PNGs;
- tray-menu PNGs;
- the multi-resolution `assets/RayanLamp.ico`.

The script uses an eased color/glow/scale transition, applies a longer final
APNG delay, and builds the ICO at 256, 128, 64, 48, 40, 32, 24, 20, and 16
pixels.

The four dependency-free bundled effects are deterministically synthesized as
mono 44.1-kHz, 16-bit integer PCM WAV:

```powershell
go run ./tools/generate-sfx -out .\assets\sfx
```

This host-neutral generator writes `on.wav`, `off.wav`, `message.wav`, and
`error.wav`, skips identical files, and prints each SHA-256. It uses no audio
encoder, Windows API, CGO, or external media tool, so Linux and Windows
developers reproduce identical bytes.

Review regenerated binary diffs before committing them. Run:

```powershell
go test ./assets ./internal/animation ./internal/sfx ./internal/sfxgen ./internal/traymenu
```

Those tests decode the embedded APNG and state/menu artwork and verify that the
steady-state SVG renderer retains every authored light ray. They also validate
the exact icon bundle boundary, PCM catalog structure, and deterministic SFX
hashes. The runtime embeds the APNG, steady-state SVGs, only the required
state/dialog ICO packs and PNGs, and one WAV catalog. Individual animation
frames and the product PE ICO remain available as source/build inputs without
being embedded a second time.

## CI

`.github/workflows/build.yml` runs the same PowerShell release script on the
current GitHub-hosted Windows runner. A Linux matrix also exercises the native
cross-build for amd64, 386, and ARM64, including the PE/resource verifier. Each
job uploads only its architecture's `RayanLamp.exe`. Every Linux matrix entry
runs the frontend checks, host-supported Go tests, selected-target vet,
cross-compilation of every Windows test package, selected-edition link, and
structural inspection so 386/ARM64 regressions cannot hide behind the amd64
job.
