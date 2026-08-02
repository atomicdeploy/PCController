<div align="center">
  <a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a>
</div>

# CI/CD and releases

PCController builds firmware, the native Controller, and the Virtual Board as
separate deliverables. The flagship workflow gathers their packages into one
run without flattening product identity or creating duplicate compatibility
artifacts.

> [!IMPORTANT]
> The project is in active pre-release development and has no stable release.
> CI artifacts are unsigned engineering builds. Provenance confirms source and
> workflow origin; it is not physical-device acceptance or code signing.

## Workflow map

| Workflow | Purpose | Hardware policy |
|---|---|---|
| `Build` | Compile and validate all three deliverables across supported targets | Never opens serial, USB, or a programmer |
| `Firmware · AVR` | Real MiniCore firmware compile, Intel HEX checks, memory evidence, dependency inventory, package | Build-only |
| `Controller host` | WebUI build/tests, Go tests/vet, native host and C ABI packages | Build-only |
| `Virtual Board` | Native simulator build and behavior/protocol tests | Hardware-free by design |
| `Release` | Repackage a selected source revision, hash, attest, and publish release assets | Build-only |
| `Deploy AVR` | Explicit operator-authorized deployment on a labeled self-hosted runner | May access only the selected attached target |
| `Repository health` | Documentation, metadata, scripts, locks, and policy checks | Hardware-free |

## Artifact catalog

Actions publishes one friendly artifact for each deliverable/target:

| Deliverable | Artifact name | Contents |
|---|---|---|
| AVR firmware | `PCController-Firmware-ATmega328P` | Application HEX, full-flash recovery HEX, validated 1 KiB safe-default EEPROM, manifests, dependency inventory, archive, checksum |
| Native host | `PCController-Controller-<platform>` | Versioned package with native executable/library, the exact same-run firmware/EEPROM defaults, metadata, notices, and checksum |
| Virtual Board | `PCController-VirtualBoard-<platform>` | Versioned simulator package, metadata, notices, and checksum |

Supported host labels are Linux x64, Linux ARM64, Windows x64, macOS Intel,
and macOS Apple Silicon. Release archives add the selected version to their
product-named root directory.

The firmware job publishes its artifact name as a reusable-workflow output.
Every host target first cleans generated output, then downloads that exact
artifact, verifies the application and complete EEPROM ranges/hashes, embeds
them, and requires both independent enabled flags in `host-manifest.json`.
This ordering prevents host cleanup from silently deleting the firmware input.

## Download an engineering build

1. Open the current [Build workflow](https://github.com/atomicdeploy/PCController/actions/workflows/build.yml).
2. Select a successful run for the exact commit you want.
3. Download the firmware, Controller, or Virtual Board artifact matching the
   target.
4. Keep the archive beside its `.sha256` sidecar.
5. Verify before extracting.

Linux:

```bash
sha256sum -c PCController-Controller-<version>-Linux-x64.tar.gz.sha256
tar -xzf PCController-Controller-<version>-Linux-x64.tar.gz
```

macOS:

```bash
shasum -a 256 -c PCController-Controller-<version>-macOS-Apple-Silicon.tar.gz.sha256
tar -xzf PCController-Controller-<version>-macOS-Apple-Silicon.tar.gz
```

Windows PowerShell:

```powershell
$archive = "PCController-Controller-<version>-Windows-x64.tar.gz"
$expected = (Get-Content ".\$archive.sha256" -Raw).Split()[0]
$actual = (Get-FileHash ".\$archive" -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected.ToLowerInvariant()) { throw "SHA-256 mismatch" }
tar.exe -xzf $archive
```

## Verify provenance

Release workflow assets may carry GitHub artifact attestations. Verification
requires GitHub CLI:

```console
gh attestation verify <archive> --repo atomicdeploy/PCController
```

Check all three identities before deployment:

- repository and source commit;
- release/version metadata inside the package;
- SHA-256 in the sidecar and package manifest.

An attestation does not replace a signature from a trusted platform publisher,
and it does not authorize hardware programming.

## Release policy

A release tag must be explicit SemVer. A tag below `v1.0.0`, or a tag that
contains a SemVer prerelease suffix, is published as a GitHub prerelease.
PCController must not be described as stable until all of the following are
true:

- the exact release source passes the complete automated matrix;
- packaged Windows, Linux, and macOS hosts pass launch and identity checks;
- the embedded WebUI passes desktop/mobile, RTL/LTR, theme, keyboard, event,
  and HTTP-serving acceptance from the packaged executable;
- a labeled board passes connection, telemetry, peripheral, reconnect,
  backup/program/verify/restore, and safe-loaded-output checks;
- known hardware-dependent limitations are documented in release notes;
- platform signing status is stated accurately.

## Local release-equivalent build

Build the firmware and host with an explicit version without touching hardware:

```console
build.cmd --all --clean --no-upx --version <semver>
```

```bash
./build.sh --all --clean --no-upx --version <semver>
```

Build and test the Virtual Board with the CMake commands in its
[maintained guide](../Tools/VirtualBoard/README.md); the flagship CI workflow
runs that matrix independently on all five targets.

The local build writes generated output only to canonical project locations:

- `.build/firmware/`
- `Tools/Controller/bin/`
- `Tools/VirtualBoard/.build/` for the standalone Virtual Board helper

Do not treat loose executables or files outside those locations as current
artifacts.

## Hardware deployment boundary

Ordinary CI and source watching do not program a board. Physical deployment
requires an explicit port/device selector and operator-selected method. The
Controller owns backup, quiet-output preparation, programmer execution,
readback verification, reconnect, and board-settings restoration.

See [Toolchain and Safe Programming](Toolchain-and-Safe-Programming.md) for the
complete transaction and recovery rules.

<p align="center"><a href="../README.md">← Return to the PCController main page</a></p>
