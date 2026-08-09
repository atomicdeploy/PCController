# Linux Ubuntu mirror provisioning

PCController owns Ubuntu mirror installation and recurring health decisions. Do
not concatenate several raw `URIs:` values or deploy a separate shell health
script: both approaches make APT download the same indexes more than once and
split the safety policy across implementations.

## Read-only review and installation

Mirror management is independent of the Arduino/firmware toolchain. This is the
minimal rollout path for an Ubuntu server that only needs APT resilience:

```sh
# Recursively inventory real source definitions (including commented, disabled,
# backup and legacy mirror-list entries), then probe possible Ubuntu archive
# roots. This is read-only; --dry-run is optional because it is the default.
controller toolchain mirror-install --dry-run --json

# Explicit privileged adoption. This installs the verified current executable
# at the stable systemd path, takes rollback backups, and enables the timer.
controller toolchain mirror-install --apply --json
```

The full fresh-host command can opt into the same profile while it installs
PCController's native adapters and target-user-owned firmware toolchain:

```sh
controller toolchain provision-host \
  --target-user USER \
  --ubuntu-mirrors=domestic-first \
  --apply
```

Neither command mutates the host without `--apply`. Linux is the only supported
platform; other operating systems return an explicit unsupported-platform
error.

## Managed topology and paths

The installer creates one deb822 source:

```text
Types: deb
URIs: mirror+file:/etc/apt/mirrors/ubuntu-dynamic.list
Suites: RELEASE RELEASE-updates RELEASE-backports RELEASE-security
Components: main restricted universe multiverse
Architectures: DPKG-NATIVE-ARCH DPKG-FOREIGN-ARCH...
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
```

Controller discovers the native architecture with
`dpkg --print-architecture`, then appends every architecture reported by
`dpkg --print-foreign-architectures` without duplicates. Every discovered
architecture is rendered into the source and must pass the signed metadata and
per-component topology probes. Provisioning therefore preserves an existing
multiarch host instead of silently reducing it to the native architecture.

The stable runtime and configuration are:

- `/opt/pccontroller/bin/controller`
- `/opt/pccontroller/libexec/unattended-upgrade`
- `/etc/pccontroller/apt-mirrors.json`
- `/etc/pccontroller/apt-mirror-proxy.env` (mode `0600`)
- `/etc/systemd/system/apt-daily.service.d/50-pccontroller-proxy.conf`
- `/etc/systemd/system/apt-daily-upgrade.service.d/50-pccontroller-origin-cache.conf`
- `pccontroller-apt-mirror-health.service`
- `pccontroller-apt-mirror-health.timer`

An existing PCController-style `mirror+file` source is adopted. Active raw
Ubuntu source lines are backed up and disabled only after a verified generated
list exists. The canonical source is activated before legacy Ubuntu stanzas are
disabled, so a power loss between atomic writes can temporarily leave duplicate
Ubuntu topology but cannot leave the host with no Ubuntu source. Third-party
repositories are preserved byte-for-byte. Mixed or unknown `mirror+file`
topologies fail closed.

Controller never re-enables a historical source file literally. Instead, it
recursively reads bounded regular files without following symlinks, parses
one-line and deb822 definitions (including `Enabled: no`, commented stanzas,
backup suffixes, distribution-upgrade files and nested `disabled` directories),
and parses legacy `/etc/apt/mirrors/*.list` entries. Distinct HTTP(S) archive
roots are normalized and deduplicated, with a verified HTTPS spelling preferred
over the same HTTP backend. A historical root is merged into the one managed
mirror list only after the current Ubuntu archive keyring, release identity,
suite, architecture, component and signed index-topology checks below succeed
for at least one current-release pocket. Historical suite names are provenance;
they never override the currently detected Ubuntu codename. PPAs and other
third-party repositories cannot pass Ubuntu archive identity verification and
are never adopted.

The JSON report's `source_inventory` records path, line, exact active/commented/
disabled/backup/legacy-list status, credential-free URI, historical suites,
classification, candidate ID and per-suite verification. Rejected and
unreachable definitions cannot become routes. Unsafe inactive files remain
visible as credential-free `ignored` inventory; an unsafe active APT source
fails the install before mutation. `verified_discovered_candidates` lists only roots merged
into the persisted configuration; `inactive_source_inventory` remains the
backward-compatible path summary. The current and legacy mirror timers and
loaded services are quiesced before adoption so they
cannot race the Go refresh. Controller also stops (but never disables)
`apt-daily.timer` and `apt-daily-upgrade.timer`, verifies both package services
are idle, and non-blockingly retains the dpkg frontend, dpkg, APT lists and APT
archives POSIX record locks for the complete adoption and systemd activation.
An active package service or lock owner fails the operation before `Install`
can edit a source. Controller never stops or signals an APT/dpkg service or
package process. Every timer's prior active state is restored on success and on
rollback.

## Ubuntu 26.04 unattended-upgrades compatibility

Ubuntu 26.04's `python3-apt` repeatedly performs a linear source-index lookup
for every `Version.origins` access. A large multiarch topology can consequently
spend tens of minutes issuing failed `stat` calls before unattended-upgrades
downloads or invokes dpkg. This is the still-open Debian bug
[#1012752](https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=1012752); it is
independent of the number of backends inside a single `mirror+file` list.

PCController keeps unattended security upgrades enabled and installs a narrow
same-process wrapper for `apt-daily-upgrade.service`. The wrapper invokes the
unchanged distro `/usr/bin/unattended-upgrade` with `runpy`, so argv, signals,
locks, logging and the systemd process identity remain unchanged. It calls the
reviewed upstream `apt.package.Origin` constructor once per PackageFile and
caches only the resulting upstream-derived fields, including `trusted`, on the
current `apt.Cache` object. It does not synthesize or override trust.

The workaround is deliberately fail-closed for affected implementations. The
identical reviewed constructor used by Ubuntu 24.04.4 `apt_pkg` 2.8.3 and
Ubuntu 26.04 `apt_pkg` 3.2.0 is recognized by exact source (SHA-256
`3abb1ceff3af2e4f5b42f45c9e16754632c8bfd3db062b7e1f9041328d220f9f`), and the
distro program must remain root-owned and non-writable.
During installation, before any source adoption or systemd reload, the newly
written wrapper creates the live `apt.Cache`, compares original and cached
Origin state for every PackageFile, and proves one upstream call per file. Any
unknown constructor which still calls `find_index`, state mismatch, or inability
to scope the cache aborts the installation and restores the file snapshots. If
a future constructor has removed every `find_index` call, the wrapper reports
passthrough and runs the distro program completely unpatched. Security upgrades
are therefore neither disabled after an upstream fix nor silently run with an
unreviewed affected trust path.

## Trust and routing policy

For every candidate/suite pair Controller:

1. fetches `InRelease` with a bounded timeout;
2. verifies its signature with `/usr/bin/gpgv` and the Ubuntu archive keyring;
3. verifies Ubuntu origin, label, exact suite/codename, every discovered
   native/foreign architecture, all components, publication time and signed
   `Valid-Until` when present;
4. fetches every configured `component/binary-ARCH/Release` for every
   discovered architecture; and
5. verifies each file's size and SHA-256 against the signed `InRelease`.

That final check rejects a mirror which serves plausible signed metadata while
omitting the binary index topology, including the observed broken security
endpoint shape.

"Working" therefore means more than an HTTP response: a route must have a valid
Ubuntu signature and identity, serve the configured architecture/component
topology, and meet the per-suite freshness policy. Several equally preferred
domestic routes let APT's mirror transport select a backend per requested file;
this can spread concurrent package downloads and lets a missing object fall
through to another route, but it does not stripe one package file across hosts.

Routes are generated with these APT mirror priorities:

- `10`: verified domestic and within eight hours of the current or persisted
  official per-suite reference;
- `20`: censorship-safe domestic bootstrap when that suite has neither a
  reachable official source nor a persisted official reference;
- `850`/`900`: unconditional official fallbacks; and
- `950`: signed but stale domestic or bounded transient last-good rescue.

During that no-official/no-last-known-good cutoff case, the newest verified
domestic publication becomes a per-suite routing consensus for the current run
only. It is not persisted as an official freshness reference. Domestic mirrors
within eight hours of that consensus receive `20` for the immutable base suite
even when its signed `Date` is old. Moving pockets receive `20` only when their
signed publication is no more than 48 hours old; stale moving pockets and
domestic mirrors more than eight hours behind consensus remain rescue-only at
`950`. This lets an older immutable Ubuntu release bootstrap during official
censorship without treating stale updates or security metadata as current.

Ubuntu does not consistently publish `Valid-Until`. An explicitly expired
signed value is always unsafe. When it is absent, Controller derives a
suite-specific deadline from the signed `Date` (48 hours for moving pockets and
180 days for the immutable base) to bound transient last-good reuse. That
derived deadline does not override the routing-only immutable-base consensus
exception above. All identity, signature, hash and topology checks still apply.

Domestic candidates marked `bypass_proxy` connect directly during Go probes.
Controller also writes per-host `Acquire::http::Proxy` and
`Acquire::https::Proxy` rules with the value `DIRECT`, so later APT downloads
to those domestic hosts bypass a configured proxy as well. Other candidates use
Go's proxy environment handling, including `NO_PROXY`. Both `apt-daily` and
`apt-daily-upgrade` inherit the same captured environment through managed
systemd drop-ins, so unattended third-party indexes use the requested proxy
while the per-host `DIRECT` rules still keep domestic Ubuntu traffic local.
Provisioning copies only recognized proxy variables into the root-readable
environment file, emits both uppercase (Go) and lowercase (APT) spellings, and
never prints their values. If only `ALL_PROXY` is present, it is also supplied
as `HTTP_PROXY` and `HTTPS_PROXY` for clients that do not consume `ALL_PROXY`
directly.

## Recurring refresh and rollback

The timer runs:

```sh
/opt/pccontroller/bin/controller toolchain mirror-refresh \
  --config /etc/pccontroller/apt-mirrors.json \
  --apply --json
```

Run the same command without `--apply` for a read-only health report. Apply
holds a non-blocking process lock before reading last-good state, probes with an
overall four-minute deadline, and atomically replaces each state and mirror
output file. Cancellation and corrupt state preserve the prior output. If the
mirror-list write returns an error after state was written, Controller restores
the prior state. A sudden power loss between those two renames can leave new
state beside the prior valid mirror list; the next refresh reconciles them.

The persistent timer first becomes eligible two minutes after boot, then runs
two hours after the prior activation with up to two minutes of randomized
delay. This cadence limits repeated metadata traffic while still adapting to a
failed or recovered mirror.

Before installation, exact snapshots of every managed path (including the
unattended-upgrade shim and systemd drop-in) and every active
Ubuntu source that will be edited, plus a SHA-256 manifest, are written under
`/var/backups/pccontroller-apt-mirrors-*`. A file-write, refresh, or systemd
activation failure during the same install invocation restores those snapshots
and the previously observed current/legacy unit state.

That rollback is deliberately bounded. Once timer activation succeeds, the
in-process rollback is committed. Recurring refreshes do not create another
full backup directory, and a saved backup does not include APT package lists,
cache, dpkg state, third-party repositories, or inactive historical source
files that were only inventoried. It also does not persist the prior systemd
unit state for a later manual rollback. Backups are therefore retained for
operator-reviewed file recovery through `manifest.json`, followed by an
explicit `systemctl daemon-reload` and deliberate unit-state restoration; they
are not a general package or host transaction snapshot.

## Live validation after apply

`mirror-install --apply` proves source inventory, signed mirror health, file
installation and timer activation. It does not run an end-to-end APT update or
package transaction. A production rollout is not complete until the host also
passes live APT validation:

```sh
# Confirm the installed config and source retain native plus foreign arches.
dpkg --print-architecture
dpkg --print-foreign-architectures
sed -n '/"architectures"/,/]/p' /etc/pccontroller/apt-mirrors.json
grep '^Architectures:' /etc/apt/sources.list.d/ubuntu.sources

# Re-run health through the stable, root-owned executable without mutation.
/opt/pccontroller/bin/controller toolchain mirror-refresh --dry-run --json

# Verify the managed schedule and execute one refresh now.
systemctl is-enabled pccontroller-apt-mirror-health.timer
systemctl start pccontroller-apt-mirror-health.service
systemctl --no-pager --full status pccontroller-apt-mirror-health.service
systemctl list-timers pccontroller-apt-mirror-health.timer --no-pager

# Prove the distro constructor and every live PackageFile still match the
# reviewed cache contract, then inspect the service-scoped PATH selection.
/opt/pccontroller/libexec/unattended-upgrade --pccontroller-self-test
systemctl cat apt-daily-upgrade.service

# Exercise APT itself, then simulate package resolution without installing.
apt-get update
apt-get --simulate dist-upgrade
```

Review the refresh JSON and generated mirror list to confirm every suite keeps
an official `850`/`900` fallback and healthy domestic routes appear before it.
`apt-get update` must succeed for every configured architecture without
duplicate index downloads, signature warnings or missing component indexes.
The simulation must resolve normally and must not propose removing foreign
architecture support. Exercise forced domestic failure/official fallback only
in a controlled staging or maintenance test; do not edit the generated list or
disable live repositories merely to demonstrate fallback on a production host.

## Candidate override

`--mirror-candidates FILE` accepts a strict JSON array. Paths and timing policy
remain Controller-owned; unknown fields, credentials in URLs, duplicate IDs or
URIs, and missing official suite coverage are rejected. Domestic priority must
be omitted because health derives it. Official roles use reviewed priority 850
or 900. The override remains useful for a nonstandard archive root whose path
does not look like an Ubuntu archive and therefore cannot be safely inferred
from historical files. Automatic discovery never bypasses these validation
rules and never promotes a PPA or arbitrary third-party repository.

```json
[
  {
    "id": "domestic-example",
    "role": "domestic",
    "uri": "https://mirror.example/ubuntu/",
    "bypass_proxy": true
  },
  {
    "id": "official",
    "role": "official-both",
    "priority": 900,
    "uri": "http://archive.ubuntu.com/ubuntu/"
  }
]
```
