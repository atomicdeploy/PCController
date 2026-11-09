# PC hardware sensor diagnostics

PCController exposes host-computer measurements separately from physical
controller-board telemetry. This read-only integration does not map CPU values
to board lighting/audio probes, change fan settings, run sensor detection,
install drivers, or participate in firmware safety decisions.

## Sources and setup

| Host | Source | Setup |
| --- | --- | --- |
| Linux | [lm-sensors](https://github.com/lm-sensors/lm-sensors), using the fixed `sensors -j` command | Install the distribution's `lm-sensors` package and arrange appropriate kernel sensor drivers separately. Existing labels and compute rules are honored. PCController never runs `sensors-detect` or `sensors -s`. |
| Windows | [Libre Hardware Monitor](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor), local read-only JSON HTTP API | Run the monitor and enable its web server bound to `127.0.0.1`, normally port 8085. Enable only the hardware categories needed there; its own hardware permissions/drivers remain its responsibility. |

Windows defaults to `http://127.0.0.1:8085/data.json`. A host administrator can
set `PCCONTROLLER_HOST_SENSORS_URL` before starting PCController to select another
local port (or literal IPv6 loopback). Only HTTP, literal loopback IPs and the
exact `/data.json` path are accepted. Credentials, query/fragment, remote hosts,
redirects and system proxies are rejected. Authenticated monitor servers are
not silently bypassed. No application is installed or started automatically.

The current upstream monitor has a [JSON publisher](https://github.com/LibreHardwareMonitor/LibreHardwareMonitor/blob/master/LibreHardwareMonitor.Windows.Forms/Utilities/HttpServer.cs),
not the older WMI publisher. Numeric raw values are used without rounding or
parsing localized display text. When the publisher provides formatted strings,
PCController obtains nullable numbers using its same-origin read-only
`/Sensor?action=Get&id=...` operation. This is one bounded, cached batch: at most
128 selected sensors, four requests in flight, within the original query
deadline. No Set/Reset request is sent. A failed batch is not reported as a
successful partial snapshot.

## Shared CLI and API

```console
controller.exe exec os facts sensors
controller.exe ipc call --method controller.os.facts --params '{"profile":"sensors"}'
```

The same read capability is exposed through existing JSON-RPC transports
(including WebSocket RPC), `controller.host.facts`, and
`GET /api/os/facts?profile=sensors`. The normal authentication and read policy
apply. `controller.os.facts.catalog` / `os facts list` advertise the profile.
There is no caller-supplied query, executable, URL, namespace or write action.
There is no new stream/subscription or GUI in this backend change.

Results retain the existing `profile`, `class`, `columns`, `rows`, `truncated`,
`source`, `collected_at` and `duration_ms` envelope. Each row has:

| Field | Meaning |
| --- | --- |
| `ID`, `Name`, `Hardware` | Source-qualified sensor identity, current label and hardware identity; labels are not the key. |
| `Type`, `Unit`, `Source` | Measurement kind, explicit native engineering unit and backend. |
| `Value`, `Available` | Finite numeric reading, or JSON null / false. A genuine zero remains available. |
| `Statistic` | `average` when an lm-sensors average is used instead of an input reading. |
| `ObservedMin`, `ObservedMax` | Optional historical extrema reported by Libre Hardware Monitor; never safety thresholds. |
| `LowLimit`, `HighLimit`, `CriticalLow`, `CriticalHigh`, `Alarm` | Optional lm-sensors thresholds/alarm, not observed extrema or board limits. |
| `Fault` | Optional lm-sensors fault. A reported fault invalidates the numeric reading instead of showing it as healthy. |

Temperature uses °C, voltage V, current A, power W, fans rpm, and supported other
types retain explicit units (Hz/MHz, %, J/mWh, B/s, etc.). Libre Data/SmallData
raw units vary across publishers, so these are explicitly marked `native`,
never guessed to be bytes. Unrecognized types fail clearly rather than receive
an invented unit. Unsupported devices cannot acquire measurements by adding an
API adapter; capabilities remain those of the actual source.

Snapshots are on-demand with the existing process-wide five-second cache and
serialized native-query slot, not per-frame/per-client monitoring processes.
`collected_at` is PCController's collection time, not a sensor's acquisition
timestamp or proof that a provider's cached measurement changed. Consumers must
retain that timestamp and actual source, and must not present an old response as
a fresh hardware event. Errors are briefly cached for one second. The existing
100–5000 ms API deadline, 512-byte cell limit and 64 KiB result limit apply.
Sensors have a deterministic 128-row ceiling with explicit truncation; the
other five profiles retain their smaller row limits.

## Verification and acceptance

Pure fixtures cover temperature/current/voltage/power/fan units, null/non-finite
and real zero readings, threshold versus extrema semantics, raw precision,
malformed/oversized/deep responses, duplicate IDs, deterministic truncation,
shared cache, loopback-only transport, no redirects/writes, numeric Get batches,
and failed/cancelled requests. Linux tests execute an isolated `sensors` fixture
with exact read-only arguments and verify deadline cancellation. CLI/RPC/REST
tests retain the existing read owner and catalog.

These checks do not establish real motherboard sensor availability. Live
Libre Hardware Monitor was not reachable on the development host's default
endpoint during this pass. No monitor/driver was installed, no board output was
actuated, and no production runtime was replaced. Actual Windows/Linux sensor
readback and production deployment remain separate gates until a configured
provider and an authorized deployment window are available. Native Desktop
presentation remains with its existing owner.
