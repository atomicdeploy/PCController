# CI/CD and Releases

PCController's GitHub automation builds the actual AVR, host, and simulator
deliverables. Ordinary CI never opens a serial port or programmer, and release
creation is gated on every build and test job succeeding.

## Pull-request and main-branch checks

| Workflow | Validation | Published Actions artifact |
|---|---|---|
| Repository Health | source/license/docs audit, Node build-tool tests, workflow `actionlint`, whitespace | none |
| Firmware - AVR | pinned MiniCore 3.1.2 and rc-switch 2.6.4, real ATmega328P compile, Intel HEX validation | application, EEPROM, merged Urboot image, manifest, dependency inventory, archive, checksum |
| Host - Cross-platform | Go tests and vet, executable identity, C ABI library, licenses and native packaging | Linux x64/ARM64, Windows x64, macOS Intel/ARM64 archives and checksums |
| Virtual Board - Cross-platform | native CMake build and CTest | Linux x64/ARM64, Windows x64, macOS Intel/ARM64 archives and checksums |

Artifacts use the commit SHA in the Actions artifact name, retain exact target
names in the archive filename, fail the job if expected files are absent, and
are retained for 14 days. Each archive has a matching `.sha256` sidecar. Job
summaries record identities, sizes, validation state, and hashes.

## Release behavior

The Release workflow reuses all three build workflows instead of trusting an
older run. It downloads the newly produced packages, generates
`SHA256SUMS.txt`, creates GitHub build-provenance attestations for every archive,
and only then creates or updates the GitHub release.

There are two supported entry points:

- a manual dispatch can create a draft release for a new tag name and target
  branch/commit; draft and prerelease default to `true`;
- pushing a `v*` tag builds the same package set and publishes or updates the
  corresponding release. A hyphenated SemVer tag is marked as a prerelease.

The first project release is `v0.1.0-alpha.1`. It is deliberately a draft
prerelease: firmware installation and physical-device acceptance remain
separate from successful compilation and packaging.

## Dependency automation

Dependabot checks GitHub Actions daily and Go modules weekly. Minor and patch
updates are grouped to reduce noise; major updates remain isolated for review.
The AVR build pins its only linked external Arduino library and the MiniCore
platform so a registry update cannot silently change release bytes.

Before merging dependency updates, require the complete workflow set to pass.
Arduino platform/library upgrades are intentional source changes: update the
pinned versions in the firmware workflow, rebuild, and record the resulting
flash/SRAM impact.

## Local parity checks

On Windows, the highest-value pre-push checks are:

```bat
build.cmd --all --clean --no-upx --version v0.1.0-alpha.1
cmake -S Tools\VirtualBoard -B .build\virtual-board -DBUILD_TESTING=ON
cmake --build .build\virtual-board --config Release --parallel
ctest --test-dir .build\virtual-board -C Release --output-on-failure
```

On Linux or macOS, use `./build.sh` with the same arguments. No command above
programs hardware.
