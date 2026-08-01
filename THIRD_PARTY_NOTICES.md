# Third-party notices

This file records third-party code and build/runtime dependencies known at the
time of publication. It does not replace the license text shipped by each
upstream project, and the repository's `MIT OR BSD-2-Clause` choice does not
relicense any item listed here.

## Source incorporated in this repository

### Adafruit NeoPixel AVR timing loop

`Project/AddressableLeds.cpp` contains an adapted 20-cycle AVR WS281x sender
from [Adafruit_NeoPixel](https://github.com/adafruit/Adafruit_NeoPixel), written
by Phil "Paint Your Dragon" Burgess for Adafruit Industries with community
contributions.

- License: `LGPL-3.0-or-later`
- Local license text: `LICENSES/LGPL-3.0-or-later.txt`
- Upstream license: <https://github.com/adafruit/Adafruit_NeoPixel/blob/master/COPYING>

The file carries its own SPDX identifier and is not covered by the project's
dual-license grant.

### OneWire and Dallas 1-Wire algorithms

`LocalLib/DallasTemperatureBus.cpp` contains a compact implementation derived
from the ROM-search and CRC algorithms published by the
[OneWire](https://github.com/PaulStoffregen/OneWire) project and Dallas
Semiconductor sample code. OneWire credits Jim Studt, Paul Stoffregen, and
many community contributors.

- License: `MIT`
- Local license text and retained upstream notices:
  `LICENSES/OneWire-MIT.txt`
- Upstream source: <https://github.com/PaulStoffregen/OneWire/blob/master/OneWire.cpp>

The file carries its own SPDX identifier and is distributed under the MIT
terms required by that incorporated code, rather than the project's general
dual-license choice.

### Vazirmatn Persian web font

`Tools/Controller/web/src/assets/vazirmatn.woff2` is the unmodified
Vazirmatn 33.0.3 web font used by the embedded RTL/LTR control center. It is
copyright 2015 The Vazirmatn Project Authors.

- License: `OFL-1.1`
- Local license text: `LICENSES/Vazirmatn-OFL-1.1.txt`
- Upstream project: <https://github.com/rastikerdar/vazirmatn>
- Vendored font SHA-256:
  `4E3FA217D38FDAFC1FEA4414CEB58CA5E662CF0AB5FA735A8C8C20E8B42CAD92`

## AVR build dependencies (not vendored)

The firmware resolves these through Arduino CLI/MiniCore rather than copying
their sources into this repository:

| Component | Tested version | License relevant to linked/library code |
|---|---:|---|
| [rc-switch](https://github.com/sui77/rc-switch) | 2.6.4 | LGPL-2.1-or-later |
| [MiniCore](https://github.com/MCUdude/MiniCore) | 3.1.2 | Mixed upstream terms; Arduino core/libraries are predominantly LGPL-2.1-or-later |
| Arduino AVR core / Wire / EEPROM | MiniCore-bundled | LGPL-2.1-or-later |
| Urboot/urclock, AVRDUDE, Arduino CLI, AVR-GCC | externally installed tools | Their respective upstream licenses; not redistributed here |

Generated `.hex`, `.eep`, `.elf`, merged bootloader images, listings, and
toolchain caches are deliberately excluded from Git. Anyone distributing a
firmware binary must also comply with the licenses of the exact core,
libraries, and bootloader used to build that binary.

## Go host dependencies (not vendored)

Exact module versions and checksums are authoritative in
`Tools/Controller/go.mod` and `Tools/Controller/go.sum`. The packaging script
copies every resolved module's `LICENSE`, `COPYING`, and `NOTICE` files beside
the generated host binary; that generated directory is excluded from Git.

The resolved module graph was audited in these license groups:

- MIT: `github.com/MakeNowJust/heredoc`,
  `github.com/aymanbagabas/go-osc52/v2`,
  `github.com/aymanbagabas/go-udiff`,
  `github.com/charmbracelet/bubbles`,
  `github.com/charmbracelet/bubbletea`,
  `github.com/charmbracelet/colorprofile`,
  `github.com/charmbracelet/harmonica`,
  `github.com/charmbracelet/lipgloss`,
  `github.com/charmbracelet/x/ansi`,
  `github.com/charmbracelet/x/cellbuf`,
  `github.com/charmbracelet/x/exp/golden`,
  `github.com/charmbracelet/x/term`,
  `github.com/clipperhouse/displaywidth`,
  `github.com/clipperhouse/stringish`,
  `github.com/clipperhouse/uax29/v2`,
  `github.com/dustin/go-humanize`,
  `github.com/erikgeiser/coninput`,
  `github.com/grandcat/zeroconf`,
  `github.com/lucasb-eyer/go-colorful`,
  `github.com/mattn/go-isatty`,
  `github.com/mattn/go-localereader`,
  `github.com/mattn/go-runewidth`, `github.com/muesli/ansi`,
  `github.com/muesli/cancelreader`, `github.com/muesli/termenv`,
  `github.com/pelletier/go-toml/v2`, `github.com/rivo/uniseg`,
  `github.com/sahilm/fuzzy`, and
  `github.com/xo/terminfo`.
- BSD-3-Clause: `github.com/atotto/clipboard`,
  `github.com/bits-and-blooms/bitset`, `github.com/fsnotify/fsnotify`,
  `github.com/miekg/dns`, `go.bug.st/serial`, `golang.org/x/crypto`,
  `golang.org/x/exp`, `golang.org/x/mod`, `golang.org/x/net`,
  `golang.org/x/sys`, `golang.org/x/text`, and `golang.org/x/tools`.
- ISC: `github.com/coder/websocket`.
- Apache-2.0: `github.com/kylelemons/godebug`.
- Mixed MIT and Apache-2.0: `gopkg.in/yaml.v3`. Its listed
  libyaml-derived files retain MIT terms; the remaining files use
  Apache-2.0 and ship Canonical's `NOTICE`.

These classifications were checked against the license files in the exact
locally resolved module versions: zeroconf 1.0.0 and go-toml/v2 2.4.3 state
MIT; miekg/dns 1.1.27, the resolved `x/net`, and the resolved `x/crypto` use
the Go BSD-3-Clause text; yaml.v3 3.0.1 explicitly divides files between MIT
and Apache-2.0 and includes a `NOTICE` file. This audit should be repeated
after any `go mod tidy` or dependency-version change.

The Go standard library is distributed under its upstream BSD-style license.

## Embedded web application dependencies

The production web bundle compiled into the Go executable contains code from
React and React DOM (MIT), Motion (MIT), and Lucide React (ISC). Exact runtime
and development versions are locked by `Tools/Controller/web/package-lock.json`.
Vite and its React plugin (MIT), Vitest (MIT), TypeScript (Apache-2.0), and the
React type declarations (MIT) are build/test dependencies and are not served
at runtime. Their upstream license files remain authoritative and must be
included beside redistributed release artifacts by the packaging pipeline.

## Build/runtime tools (not vendored)

Node.js, CMake, Ninja, GCC/MinGW-w64, Go, `go-winres`, UPX, Git, and GitHub CLI
are discovered from the user's environment. They are not included in this
source tree and retain their own licenses.

The project-owned Node build presentation resolves Chalk (MIT) and
`cli-table3` (MIT), plus their transitive dependencies, from the exact
`Tools/Build/package-lock.json`. They are build-time dependencies rather than
host-runtime code and are not embedded in `controller.exe`. Their upstream
license files are installed with `npm ci` and remain authoritative.

## Behavioral references with no copied source

`Tools/Controller/docs/Upstream-Source-Audit.md` records the local and network projects that
were inspected for behavior or workflow ideas. It also records their known
license status. Their source was not copied into PCController and they are not
relicensed here.
