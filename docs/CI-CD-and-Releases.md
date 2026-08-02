<div align="center">
  <a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a>
</div>

# CI/CD and releases

PCController builds firmware, the native Host, and the Virtual Board as
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
| `Firmware · AVR` | MiniCore compile, Intel HEX checks, memory evidence, dependency inventory, package | Build-only |
| `Host` | WebUI build/tests, Go tests/vet, native host and C ABI packages | Build-only |
| `Virtual Board` | Native simulator build and behavior/protocol tests | Hardware-free by design |
| `Release` | Repackage a selected source revision, hash, attest, and publish release assets | Build-only |
| `Deploy AVR` | Explicit operator-authorized deployment on a labeled self-hosted runner | May access only the selected attached target |
| `Repository health` | Documentation, metadata, scripts, locks, and policy checks | Hardware-free |
| `CodeQL` | Six-category repository-wide security scan with one stable gate | Hardware-free |
| `Update dependencies` | Resolve, lock, preflight, and propose compatible dependency updates | Hardware-free |

## Artifact catalog

Actions publishes one friendly artifact for each deliverable/target:

| Deliverable | Artifact name | Contents |
|---|---|---|
| AVR firmware | `PCController-Firmware-ATmega328P` | Application HEX, full-flash recovery HEX, validated 1 KiB safe-default EEPROM, manifests, dependency inventory, archive, checksum |
| Native host | `PCController-Host-<platform>` | Versioned package with native executable/library, the exact same-run firmware/EEPROM defaults, metadata, notices, and checksum |
| Virtual Board | `PCController-VirtualBoard-<platform>` | Versioned simulator package, metadata, notices, and checksum |

Supported host labels are Linux x64, Linux ARM64, Windows x64, macOS Intel,
and macOS Apple Silicon. Release archives add the selected version to their
product-named root directory.

The firmware job publishes its artifact name as a reusable-workflow output.
Every host target first cleans generated output, then downloads that exact
artifact, verifies the application and complete EEPROM ranges/hashes, embeds
them, and requires both independent enabled flags in `host-manifest.json`.
This ordering prevents host cleanup from silently deleting the firmware input.

Artifact retention is deliberate: pull-request packages remain available for
14 days, while `main`, tag, and manual-run packages remain available for 90
days. Published release assets remain attached to their release until a
maintainer explicitly removes them.

## Download an engineering build

1. Open the current [Build workflow](https://github.com/atomicdeploy/PCController/actions/workflows/build.yml).
2. Select a successful run for the exact commit you want.
3. Download the firmware, Host, or Virtual Board artifact matching the
   target.
4. Keep the archive beside its `.sha256` sidecar.
5. Verify before extracting.

Linux:

```bash
archive=PCController-Host-<version>-Linux-x64.tar.gz
sha256sum -c "${archive}.sha256"
tar -xzf "$archive"
```

macOS:

```bash
archive=PCController-Host-<version>-macOS-Apple-Silicon.tar.gz
shasum -a 256 -c "${archive}.sha256"
tar -xzf "$archive"
```

Windows PowerShell:

```powershell
$archive = "PCController-Host-<version>-Windows-x64.tar.gz"
$expected = (Get-Content ".\$archive.sha256" -Raw).Split()[0]
$actual = (Get-FileHash ".\$archive" -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected.ToLowerInvariant()) { throw "SHA-256 mismatch" }
tar.exe -xzf $archive
```

## Release integrity and draft repair

A release never relabels packages from an older run. Tag-triggered releases
build the commit named by that tag; a manual run builds the immutable
`github.sha` selected in the **Run workflow** dialog. That same source identity
is passed into every reusable checkout, recorded in every manifest, and used
as the release target.

Release waits for the complete firmware, Host, and Virtual Board target set,
then stages only artifacts produced by that invocation. Missing or colliding
assets fail the run. The workflow emits deterministic checksums, a release
manifest, direct application and full-flash recovery images, release notes,
and build-provenance attestations before it creates or updates the release.

Manual releases default to draft and prerelease. Re-running a matching draft
repairs it in place: case-folded filename collisions are removed, current
assets replace their exact predecessors, stale draft assets are pruned, and
release notes are replaced rather than appended. The repair step never deletes
pre-existing assets from an already published release.

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

Each codebase writes one summary. Host and Virtual Board show shared checks
once, then list only platform-specific differences in a target table. Failed,
cancelled, skipped, and missing targets remain visible. Firmware reports its
flash and SRAM budgets, dependencies, build configuration, and stack headroom.
Usage meters are green through 50%, orange above 50%, and red above 80%.

## Dependency automation

The daily `Update dependencies` workflow owns Arduino CLI, MiniCore, all
declared firmware libraries, Urboot-Custom, Go and Node toolchains, Go modules,
npm packages, GitHub Actions, UPX, go-winres, and the native Windows compiler.
It resolves compatible stable releases, records exact source and integrity data
in reviewed locks, checks release/license/security/size impact, and compiles the
AVR firmware before opening a uniquely named pull request. It never merges or
releases its own proposal, and duplicate or failed proposals remain explicit.

Release discovery is not allowed to follow caller-supplied repository names.
Firmware, bootloader, and native-tool release APIs are rooted in reviewed
profile or policy entries, with hashes and official index metadata verified
before use. Adding a new release source therefore requires an explicit policy
or manifest change in review; generated proposal text cannot expand the
outbound-source allowlist.

Dependabot independently covers GitHub Actions daily, the Go Host module
weekly, and the Build, Firmware, and WebUI npm roots weekly. Minor and patch
updates are grouped by ecosystem; security updates have separate groups; major
updates remain isolated for review. Repository Health inventories all module
roots and rejects gaps between the repository and Dependabot configuration.

## Code scanning

CodeQL runs `security-extended` queries across GitHub Actions, JavaScript and
TypeScript tooling, Go on Linux and Windows, and C/C++ across AVR, Linux, and
Windows builds. Platform-specific categories remain distinct, while the stable
`🛡️ CodeQL · Entire repository` job is the protected-branch gate. Checkouts do
not persist credentials, workflows receive no repository secrets, and the scan
uploads SARIF only.

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
runs the checked-in `release` preset independently on all five targets.

The local build writes generated output only to canonical project locations:

- `.build/firmware/`
- `Tools/Controller/bin/`
- `Tools/VirtualBoard/.build/` for the standalone Virtual Board helper

Do not treat loose executables or files outside those locations as current
artifacts.

## Hardware deployment boundary

Ordinary CI and source watching do not program a board. Physical deployment
requires an explicit port/device selector and operator-selected method. The
Host owns backup, quiet-output preparation, programmer execution,
readback verification, reconnect, and board-settings restoration.

The deploy job always runs trusted controller code from protected `main`; the
release-selected SHA remains firmware provenance only. A protected environment,
repository opt-in, labeled self-hosted runner, exact device/method selection,
checksum verification, and explicit confirmation must all pass before the
hardware job can start. A workflow definition is never presented as evidence
that a physical upload occurred.

Bundle preparation may verify either a published release or a draft. Because
GitHub restricts draft visibility, `contents: write` is scoped only to the
download-and-verify preparation job; the hardware job retains a read-only
token. Preparation also runs through the secret-free `avr-release-read`
environment, whose deployment policy accepts only protected `main`.

The release target must equal the manifest source SHA and be an ancestor of
the protected deployment checkout. The self-hosted runner builds the guarded
programmer from protected `main` with persisted checkout credentials disabled,
so deployment evidence records firmware provenance and the independently
trusted deployment-controller identity separately.

See [Toolchain and Safe Programming](Toolchain-and-Safe-Programming.md) for the
complete transaction and recovery rules.

<p align="center"><a href="../README.md">← Return to the PCController main page</a></p>
