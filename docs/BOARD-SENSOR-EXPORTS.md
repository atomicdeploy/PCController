# Exporting PCB measurements to system monitors

This is the **outbound** PCB sensor bridge. It is separate from
[host sensor diagnostics](HOST-SENSORS.md), which imports the computer's readings.

PCController remains the sole USB-serial owner. Exporters read its cached board
status; they do not open a second serial connection, probe sensors, change
firmware, or send output commands. This is a telemetry bridge, **not** emulation
of a DS2490 USB/1-Wire adapter or an operating-system USB device identity.

## Published data

`GET /api/sensors/board` returns supply/bus voltage (V), signed current (A),
signed power (W), and the lighting/audio DS18B20 temperatures (°C). Missing or
invalid measurements have JSON `null`, never a fabricated zero. Connection
generation, original board-status receipt time and expiry accompany the data.
The timestamp is host receipt of a status frame, not a per-sensor ADC timestamp.

`GET /api/sensors/metrics` exposes the same values in Prometheus text format.
Disconnected, programming, invalid and five-second-old values are omitted, with
per-sensor availability gauges set to zero. Real zero and negative readings are
preserved. Both endpoints use existing HTTP authentication and read policy,
disable HTTP caching, and perform no board request on a scrape.

## Windows

No additional software is needed to read the JSON endpoint in a browser or
PowerShell. For history, graphs and alerts, install
[Prometheus for Windows](https://prometheus.io/download/) and optionally
[Grafana for Windows](https://grafana.com/docs/grafana/latest/setup-grafana/installation/windows/).
Start Prometheus with the supplied
[configuration](../Tools/Integrations/Prometheus/prometheus.yml), adjusting the
listener port (often 8787; a GUI-owned host can use 8788).

```powershell
prometheus.exe --config.file=prometheus.yml --web.listen-address=127.0.0.1:9090
```

Example queries: `pccontroller_board_temperature_led_celsius`,
`pccontroller_board_supply_volts`, `pccontroller_board_current_amperes`, and
`pccontroller_board_power_watts`. Grafana's Prometheus data source uses
`http://127.0.0.1:9090`. Gate alerts with the corresponding `_available` gauge.

Installing HWiNFO or Libre Hardware Monitor alone does **not** add support for
this arbitrary USB-serial PCB. Their motherboard/device drivers are not generic
HTTP-sensor importers. Libre Hardware Monitor in `HOST-SENSORS.md` is the opposite
direction and is not required for this export. No Windows kernel driver or
automatic integration into those applications is claimed here.

## Linux: real hwmon and lm-sensors

An optional GPL-2.0-only kernel facade in `Tools/Integrations/hwmon` registers
standard hwmon attributes using the kernel API. It exposes `in0_input`,
`in1_input`, `curr1_input`, `power1_input`, `temp1_input` and `temp2_input`, with
labels and temperature fault attributes. Units are mV, mA, µW and m°C as required
by the [hwmon ABI](https://docs.kernel.org/hwmon/sysfs-interface.html).

Install your distribution's `lm-sensors`, build tools and **matching running
kernel headers**. Review the module and build/load it explicitly:

```sh
cd Tools/Integrations # In a source checkout; use integrations in a host package.
make -C hwmon
sudo insmod hwmon/pccontroller_hwmon.ko
sudo /path/to/controller sensors hwmon --url http://127.0.0.1:8787
# In another terminal:
sensors
sensors -j
```

The feeder is part of the same host executable; it attaches by read-only HTTP
to the existing host, never owns the board. It only accepts literal loopback
HTTP origins, refuses redirects/proxies, bounds responses and deadlines, and
can use `PCCONTROLLER_SENSOR_TOKEN` if the host requires a header credential.
The character device defaults to root-only mode 0600. A reviewed systemd
service template is supplied; installation/enabling is deliberately explicit.
Secure Boot systems may require locally signing the module according to their
distribution's policy. Nothing disables Secure Boot or installs a driver
automatically.

One open feeder owns one hwmon instance. Closing it removes that instance.
The kernel uses boottime expiry, so suspend or a stalled/killed feeder cannot
keep a stale sample healthy. Repeated HTTP polls preserve remaining freshness
rather than renewing old data. Expired/missing inputs return `ENODATA` and
temperatures report a fault; they are never substituted with zero. Do not use
this userspace-mediated facade for firmware thermal protection or fan control.

Stop the feeder before unloading with `sudo rmmod pccontroller_hwmon`.

## Acceptance boundaries

Fixtures verify units, signed/zero readings, invalid/absent/stale/disconnected
data, expiry and HTTP policy. CI compiles the module against Linux headers;
that is not a kernel-load or electrical test. Matching-kernel load, actual
`sensors` enumeration and meter/temperature comparison remain runtime gates
until an authorized Linux host and connected PCB are available.
