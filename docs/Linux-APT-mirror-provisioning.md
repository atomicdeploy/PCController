# Linux Ubuntu mirror provisioning

PCController owns Ubuntu mirror installation and recurring health decisions. Do
not concatenate several raw `URIs:` values or deploy a separate shell health
script: both approaches make APT download the same indexes more than once and
split the safety policy across implementations.

## Read-only review and installation

Mirror management is independent of the Arduino/firmware toolchain. This is the
minimal rollout path for an Ubuntu server that only needs APT resilience:

```sh
# Probe signed metadata and inventory active, commented, disabled and backup
# source files. This is read-only; --dry-run is optional because it is default.
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
Architectures: HOST-DEBIAN-ARCH
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
```

The stable runtime and configuration are:

- `/opt/pccontroller/bin/controller`
- `/etc/pccontroller/apt-mirrors.json`
- `/etc/pccontroller/apt-mirror-proxy.env` (mode `0600`)
- `pccontroller-apt-mirror-health.service`
- `pccontroller-apt-mirror-health.timer`

An existing PCController-style `mirror+file` source is adopted. Active raw
Ubuntu source lines are backed up and disabled only after a verified generated
list exists. Third-party repositories are preserved byte-for-byte. Mixed or
unknown `mirror+file` topologies fail closed. Commented, disabled, distribution
upgrade and backup files are reported as inventory and are never activated.
The legacy `apt-mirror-health.timer`, when present, is quiesced before adoption
so it cannot race the Go refresh; its prior enabled/active state is restored if
installation rolls back.

## Trust and routing policy

For every candidate/suite pair Controller:

1. fetches `InRelease` with a bounded timeout;
2. verifies its signature with `/usr/bin/gpgv` and the Ubuntu archive keyring;
3. verifies Ubuntu origin, label, exact suite/codename, architecture,
   components, publication time and signed `Valid-Until` when present;
4. fetches every configured `component/binary-ARCH/Release`; and
5. verifies each file's size and SHA-256 against the signed `InRelease`.

That final check rejects a mirror which serves plausible signed metadata while
omitting the binary index topology, including the observed broken security
endpoint shape.

Routes are generated with these APT mirror priorities:

- `10`: domestic and within eight hours of a reachable official reference;
- `20`: first-run domestic bootstrap while official references are cut off,
  provided signed publication age is within the strict suite limit;
- `850`/`900`: unconditional official fallbacks; and
- `950`: signed but stale domestic or bounded transient last-good rescue.

Ubuntu does not consistently publish `Valid-Until`. A signed value, when
present, may only shorten validity. Otherwise Controller derives a conservative
deadline from the signed `Date` (48 hours for moving pockets and 180 days for
the immutable release pocket). Explicitly expired signed metadata is unsafe;
age-stale but otherwise signed/hash-valid metadata remains rescue-only.

Domestic candidates marked `bypass_proxy` connect directly. Other candidates
use Go's proxy environment handling, including `NO_PROXY`. Provisioning copies
only proxy variables into the root-readable timer environment file and never
prints their values.

## Recurring refresh and rollback

The timer runs:

```sh
/opt/pccontroller/bin/controller toolchain mirror-refresh \
  --config /etc/pccontroller/apt-mirrors.json \
  --apply --json
```

Run the same command without `--apply` for a read-only health report. Apply
holds a non-blocking process lock before reading last-good state, probes with an
overall four-minute deadline, and atomically replaces state and mirror output.
Cancellation and corrupt state preserve the prior output.

Before installation, exact file snapshots and a SHA-256 manifest are written
under `/var/backups/pccontroller-apt-mirrors-*`. Any file or systemd activation
failure restores the snapshots and the previous timer state. Backups are kept
for operator-reviewed recovery.

## Candidate override

`--mirror-candidates FILE` accepts a strict JSON array. Paths and timing policy
remain Controller-owned; unknown fields, credentials in URLs, duplicate IDs or
URIs, and missing official suite coverage are rejected. Domestic priority must
be omitted because health derives it. Official roles use reviewed priority 850
or 900.

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
