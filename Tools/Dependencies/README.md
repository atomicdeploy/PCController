<div align="center"><a href="../../README.md"><img src="../../docs/assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Reproducible Dependency Maintenance

PCController follows a **latest stable, exactly locked** dependency policy.
Policy files describe what may be selected; lock files record exactly what was
selected, including immutable versions, source identities, URLs, and hashes.
This keeps routine updates current without making a rebuild depend on whatever
a registry happens to return later.

The dependency updater is hardware-free. It does not enumerate or open COM18,
reset a board, start the TUI, write EEPROM, or invoke a programmer.

## Policy and lock layers

| Area | Selection policy | Reproducible evidence |
| --- | --- | --- |
| Firmware CLI, MiniCore, firmware libraries, Urboot, Go | Stable channel at or above the declared minimum in `Tools/Controller/toolchain-profile.json` | Exact `Tools/Controller/toolchain-lock.json`, including archive/source hashes and the Urboot commit/tree |
| Node.js, UPX, go-winres, native Windows GCC, and GitHub Actions | Stable/LTS policy in `Tools/Dependencies/dependency-policy.json` | Exact `Tools/Dependencies/resolved-tools-lock.json`, including the compiler package/target/archive integrity and every immutable Action revision with its workflow consumers; revisions are maintained by the updater and Dependabot |
| Go modules | Latest compatible source requirements | `Tools/Controller/go.mod` and `go.sum` |
| Build-output npm packages | Compatible ranges, Node.js 22.12 or newer | `Tools/Build/package-lock.json` |
| Web npm packages | Compatible ranges | `Tools/Controller/web/package-lock.json` |

These are the only dependency policy/lock sources. CI does not maintain a
second abbreviated pin file. `Tools/Dependencies/export-lock.mjs` is a
network-free read-only adapter that validates this canonical lock and exports
the exact Linux build inputs plus package-metadata product identity to reusable
workflows; it never resolves or writes a dependency.

The policy is intentionally latest-first. `toolchain bootstrap` consumes the
resolved stable lock by default; `--locked` is an explicit rollback/recovery
choice, not the normal update policy. Lock writers compare substantive fields
and preserve the existing file byte-for-byte when only a resolution timestamp
would change.

The currently resolved firmware set is CLI 1.5.1, MiniCore 3.1.2, Urboot
`u8.0.1`, Go 1.26.5, and these libraries:

| Library | Resolved version |
| --- | ---: |
| Adafruit PWM Servo Driver Library | 3.0.3 |
| Adafruit INA219 | 1.2.3 |
| rc-switch | 2.6.4 |
| TM1637TinyDisplay | 1.12.2 |
| DallasTemperature | 4.0.6 |
| OneWire | 2.3.8 |

The host-tool lock currently resolves Node.js LTS 24.18.1 (`Krypton`), UPX
5.2.0, and go-winres 0.3.3. Treat these numbers as a lock snapshot, not as
hard-coded recommendations: the updater resolves a newer compatible stable
release when one becomes available and records its exact identity.

## Stable selection and canaries

Stable releases are the only automatic update candidates. Prerelease CLI builds
and Urboot `main` are observation-only canaries. Their identities appear in the
report so an upstream break can be investigated early, but they are never
written into the stable lock, installed, or proposed silently.

When a stable Urboot tag changes, validation must fetch the immutable source,
verify every source hash, reapply the Urboot-Custom unified diff, reproduce the
historical stock fixture, and rebuild the 512-byte custom image. A failed rebase
blocks the candidate rather than dropping the customization.

Every third-party Action is referenced by a 40-character commit revision with
a readable major-version comment. The updater rejects floating tags, records
the complete Action inventory in the host-tool lock, and leaves discovery of
new compatible revisions to Dependabot.

## Commands

From the repository root on Windows:

```cmd
update-dependencies.cmd --check
update-dependencies.cmd --check --require-current
update-dependencies.cmd --apply
update-dependencies.cmd --apply --validate
```

The equivalent POSIX commands are:

```sh
./update-dependencies.sh --check
./update-dependencies.sh --check --require-current
./update-dependencies.sh --apply
./update-dependencies.sh --apply --validate
```

- `--check` resolves registries and prints changes without modifying tracked
  files.
- `--require-current` makes check mode fail when a stable lock or compatible
  source dependency is stale; it is suitable for a no-device CI gate.
- `--apply` refreshes exact locks plus compatible Go/npm source requirements.
- `--validate` runs the complete hardware-free candidate gate after applying.
- `--report FILE` writes the structured JSON result used by CI.
- `--no-direct-retry` enforces proxy-only network access.

For a firmware-only policy check, the isolated resolver avoids importing the
serial, IPC, TUI, and integration packages:

```cmd
cd Tools\Controller
go run .\cmd\toolchain-resolver check --include-canary --require-current
```

That command also performs no device I/O.

## Proxy inheritance

The updater and its child tools inherit the caller's environment, including
upper- or lower-case `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `FTP_PROXY`, and
`NO_PROXY`, plus `ARDUINO_NETWORK_PROXY` for the current firmware dependency
backend. Reports list only the names of detected variables; values and
credentials are neither printed nor persisted.

On Windows, validation wrappers ending in `.cmd` or `.bat` are launched through
the inherited `ComSpec` with AutoRun and delayed expansion disabled and an
explicitly quoted command line. Native executables remain
direct child processes, and POSIX never uses a shell. This avoids Node's
`spawnSync *.cmd EINVAL` behavior without `shell: true` and without changing
the caller's proxy or local-network bypass environment.

The configured network route is attempted first. By default, a failed registry
or download request is retried once in a child environment with proxy variables
removed. This temporary retry does not mutate the parent process or machine
configuration. Use `--no-direct-retry` when policy prohibits a direct fallback.
GitHub requests use `GITHUB_TOKEN`/`GH_TOKEN` when supplied; on a workstation,
the updater can borrow the authenticated `gh` credential for its child request
without printing or persisting that credential. Scheduled CI supplies its
ephemeral repository token through the same path.

## Candidate validation

`--apply --validate` is designed to reject a dependency PR unless all affected
areas still work together:

- candidate-lock-compatible Windows compiler selection or provisioning before
  build-system tests;
- regeneration of the runtime Go policy after candidate lock/hash changes;
- exact firmware toolchain bootstrap and firmware compile;
- firmware size at or below the 32,256-byte Urboot-Custom application ceiling;
- active Urboot source hashes, clean diff application, exact `u8.0` stock
  fixture hashes, and a custom image within 512 bytes;
- stable-path Go tests, vetting, host build, Win32 resources, and UPX packaging;
- canonical product-identity generation checked before host compilation;
- clean npm installs, syntax checks, tests, type checking, and the web production
  build;
- VirtualBoard configure, compile, and CTest execution;
- generated firmware, bootloader, and host manifests.

The bounded build-system test output is captured into a failed structured
report, so its assertion and stack trace reach the blocked-update issue and
artifact instead of being available only in the transient Actions log.

The candidate report also contains npm audit severity totals. A deterministic
PR plan turns the report into release-note links, explicit license/security/size
review statements, memory headroom, compressed host size, and reviewer
checkboxes. Non-npm security and license changes remain deliberately visible
review items rather than being presented as automatically proven safe.

UPX and go-winres are provisioned into managed build locations from the exact
resolved release identities. No developer-specific absolute path is stored in
the policy, lock, or build output.

## Scheduled update workflow

`.github/workflows/update-dependencies.yml` runs the same resolver on a
schedule and on manual dispatch. A normal run applies the stable candidate and
performs the full validation **before** creating or refreshing its dependency
pull request. A check-only dispatch reports drift without changing locks.

This is the repository's only scheduled dependency updater. The firmware and
protected-deploy workflows consume the canonical lock through the read-only
export adapter, so reporting and reproducible installation do not introduce a
second update engine. Firmware CI installs all six locked libraries and records
the complete canonical lock with its package.

The workflow always uploads its structured report and generated evidence. If a
candidate fails, it creates or updates one actionable `dependency-blocked`
issue with the run, commit, error, and artifact location; it does not open a
known-broken PR. The issue is closed after a later candidate passes. Dependabot
separately maintains stable GitHub Actions, Go modules, and the two npm lock
domains.

Before opening a dependency PR, the workflow generates
`.build/dependencies/dependency-pr-plan.json` and `dependency-pr.md` from the
same report that passed validation. This makes the PR description repeatable
and prevents a hand-maintained body from drifting away from the tested
candidate.

Focused tests cover stable version selection, timestamp-free idempotence,
exact lock replay, Action pin inventory, deterministic PR planning, partial
network failure, case-insensitive proxy inheritance, and the single bounded
direct fallback. Full candidate validation covers the integrated build matrix.

The workflow definition, local resolver checks, and source-level tests have
been validated in this workspace. A real scheduled/manual GitHub Actions run
has **not yet been observed**, so hosted-run success, artifact publication, PR
creation, and blocked-issue lifecycle remain CI acceptance items rather than
claimed live results.

## Review rules

When reviewing an automated dependency change:

1. inspect both policy-to-lock changes and source lockfiles;
2. confirm prerelease/canary observations were not selected;
3. check the Urboot-Custom patch and size evidence when Urboot or AVR tools
   changed;
4. verify the firmware ceiling, host resources/UPX result, web build, and
   VirtualBoard tests in the uploaded report;
5. merge only the already validated stable candidate.
