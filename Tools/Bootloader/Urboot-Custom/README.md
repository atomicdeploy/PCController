# Urboot-Custom

Urboot-Custom is PCController's patch-based Urboot variant. The name describes
the extension point, not its current output device: today the selected backend
reports page activity on TM1637, while a future build may select another
display, LED, or telemetry backend without renaming the bootloader or changing
the generic hook.

The image has been compiled and structurally validated. It has **not** been
installed on the physical board, and no ISP or COM18 operation was performed as
part of this work.

## Upstream and reproducibility model

The build deliberately keeps two upstream identities separate:

| Role | Upstream | Toolchain | Purpose |
| --- | --- | --- | --- |
| Active custom source | Urboot `u8.0.1`, commit `bd52751acaa5923163e938a6e35051c22317da68` | Resolved AVR toolchain | Source onto which the custom diff is applied |
| Stock hash fixture | Urboot `u8.0`, commit `b25d491a0b16eda79e8c5c10dd861d16396c12ae` | AVR GCC 7.3.0, Binutils 2.26.20160125, MiniCore 3.1.2 | Exact reproduction of the two historical MiniCore bootloader artifacts |

`source-manifest.json` pins every downloaded source file by SHA-256. The build
first compiles the stock fixture and requires both its textual Intel HEX and
decoded-byte hashes to match MiniCore exactly. Only then does it fetch the
active `u8.0.1` source, verify its hashes, apply
`patches/0001-optional-progress-backend-hook.patch`, and build Urboot-Custom.

The old MiniCore hashes cannot be expected from `u8.0.1`: upstream source,
compiler, assembler, linker, and link layout all participate in the generated
bytes. Keeping the exact `u8.0`/GCC 7.3 combination as a fixture answers whether
the historical build is reproducible without forcing the active custom image
to remain on an older Urboot release.

The customization is stored as a unified diff rather than a modified upstream
source tree. That makes a future upgrade reviewable: update the immutable
upstream identity and file hashes, rebase the diff, then rerun every hash,
metadata, size, and address assertion. A patch that no longer applies fails the
build instead of silently producing a partly customized bootloader.

## Generic hook and current backend

The diff adds only the generic build switch and callback contract:

- `URBOOT_PROGRESS_ENABLED`
- `urboot_progress(event, addressHigh)`
- backend-neutral read and write event values

TM1637-specific AVR instructions remain in `backends/tm1637_progress.S`. The
current profile uses D13/PB5 as clock and D11/PB3 as data. A different backend
can implement the same callback without changing the Urboot-Custom name or
putting peripheral-specific logic into the upstream diff.

The current backend shows `P` for a program/write page and `r` for a read page.
The other three digits add their middle bars when the address high byte crosses
`0x20`, `0x40`, and `0x60`, representing 25%, 50%, and 75% of the 32 KiB flash
address space. These are address-derived milestones, not elapsed-time or host
packet-progress estimates.

The hook runs after the page operation and before its final `STK_OK`, adding
about 0.35 ms per page. It changes no protocol byte, command handler, UART
register, or metadata format. Flash and EEPROM operations invoke the hook;
EEPROM addresses remain below the flash-oriented quarter thresholds.

To fit the renderer, the backend assumes that the application previously sent
the TM1637 data-mode (`0x40`) and brightness commands. A DTR/external MCU reset
normally preserves the separately powered display controller's state, so later
UART/Urclock programming can show progress. A cold or simultaneously
power-cycled display may remain blank until the application initializes it.
Visibility and electrical timing still require an on-board test.

## Reproducible build

Run either wrapper from the repository root:

```cmd
Tools\Bootloader\Urboot-Custom\build.cmd
```

```sh
./Tools/Bootloader/Urboot-Custom/build.sh
```

If the historical compiler fixture is absent, add `--bootstrap`. Bootstrap
installs the isolated MiniCore 3.1.2 dependency needed to obtain
`avr-gcc@7.3.0-atmel3.6.1-arduino7`; it does not downgrade an unrelated global
core. `AVR_GCC_ROOT` may instead identify that exact toolchain.

Downloads inherit the current proxy environment. The configured route is tried
first; where enabled, a failed source fetch is retried directly in a child
process without altering the parent or machine environment. Proxy credentials
are never written into manifests or console output.

Generated files are under `.build/bootloader/urboot-custom/`:

- `urboot-atmega328p-custom.hex`, `.elf`, `.bin`, `.map`, and `.lst`
- `stock-no-led.*` and `stock-led-b5.*` fixture evidence
- `build-manifest.json`, including source identities, hashes, addresses, sizes,
  feature matrix, and the current application-ceiling assertion
- compile-only alternatives named `matrix-*`

The last verified build produced:

| Artifact/check | Verified result |
| --- | --- |
| Stock `no-led` HEX SHA-256 | `b2aba91e0bd5a7ef64df3471684cc69c4942cfd587c64e7d884c08e78969354e` |
| Stock `no-led` binary SHA-256 | `28d3566779663909146b00d45e38df24f04fbcf33763d806d11578ff55c94d7c` |
| Stock `led+b5` HEX SHA-256 | `a1f557128760c597d12822faa072eb8712562fd49150cc03807dcdd40fa3a192` |
| Stock `led+b5` binary SHA-256 | `35debc1341130cad85b566c364ae2639b4dc228b30cfa2f96b4cf99e2bccd650` |
| Custom HEX SHA-256 | `27a053dcf384818a4b18b806a1eb0f4020ebce1051d422afee8017dd48c615e0` |
| Normalized 512-byte custom binary SHA-256 | `8e826f33e61bb87ce738deee1bf8045c2b6e14ae86892bab8e6dc6e676d6f8db` |
| Custom meaningful/allocation bytes | 510 / 512 |
| Application ceiling | 32,256 bytes |
| Application recorded by that build | 32,240 bytes, leaving 16 bytes |

The application figure is evidence from that build, not permanent headroom.
Regenerate the normal firmware manifest and rerun this build immediately before
installing the bootloader.

## Structural assertions

The build fails unless all of these remain true:

- the active source hashes match and the unified diff applies cleanly;
- both `u8.0` MiniCore fixtures match their reference textual HEX and normalized
  binary hashes exactly;
- the custom image occupies at most 512 bytes from `0x7E00` through `0x7FFF`;
- the top metadata remains `04 19 95 CF E7 40`;
- the exported `pgm_write_page` target is derived from the active ELF and the
  generated `RJMPWP` decodes back to that address, rather than relying on a
  stale hard-coded jump;
- the current application is no larger than 32,256 bytes.

The retained Urboot feature code is `weU-jPrac`: EEPROM read/write, level-1
compare-before-write, vector bootloading, bootloader self-protection,
reset-vector protection, reset flags, autobaud, chip erase, and exported
application `pgm_write_page` remain enabled. The old single-PB5 receive blink is
replaced by the selected progress backend.

## Optional feature/size matrix

No capability below is disabled in the selected image. These measured variants
are explicit options if a future backend needs more bytes:

| Optional removal | Image bytes | Bytes gained | Exact loss |
| --- | ---: | ---: | --- |
| Chip erase | 482 | 28 | No `STK_CHIP_ERASE`; the uploader cannot request bootloader-managed whole-application erase. |
| EEPROM | 454 | 56 | No EEPROM read/write through Urboot, so bootloader-path settings backup/restore is lost. |
| Update check | 484 | 26 | No compare-before-write shortcut; requested pages are rewritten even when already identical. |
| Application page writer | 500 | 10 | No exported `pgm_write_page` for application self-programming; serial flash upload remains. |
| Autobaud | 494 | 16 | Fixed 115200 baud at 16 MHz; receive-edge baud measurement/tolerance is lost. |
| Reset-vector protection | 496 | 14 | Page-zero writes are no longer forced back to Urboot and could strand the bootloader. Bootloader-region protection remains. |

Keeping the legacy PB5 blink as well as the TM1637 backend was also measured.
It needs 520 meaningful bytes (five pages), costs another 128 application bytes,
and electrically contends with the TM1637 clock. It is therefore not selectable.

## Installation and ISP requirement

**A USBasp or another ISP is required for the first installation.** The running
bootloader protects its own region, while this image begins one flash page
lower, so ordinary UART/Urclock application programming cannot replace it.
After installation, normal application uploads can return to UART/Urclock;
future bootloader replacement still requires ISP.

Do not write the bootloader-only HEX with a generic chip-erase command. A safe
installation must:

1. connect ISP and back up flash, EEPROM, fuses, and lock state;
2. rebuild the application against the 32,256-byte ceiling;
3. create and verify a merged application plus bootloader image with the
   vector-25 trampoline and reset vector prepared correctly;
4. preserve EEPROM according to EESAVE, write and verify the image, and keep the
   backup until boot and UART programming are proven.

The first ISP write cannot show its own TM1637 progress because the CPU is held
under ISP control while D13/SCK and D11/MOSI are programmer signals. Progress
begins on subsequent UART/Urclock reads and writes.

## Licensing

Urboot and this derivative diff/backend are GPL-3.0-only; the repository's
MIT/BSD licensing does not override that. The exact upstream license is kept in
`LICENSE.upstream`, and upstream source is fetched by immutable identity rather
than copied into this repository.
