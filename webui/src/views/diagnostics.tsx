import { useEffect, useMemo, useState } from "preact/hooks";
import type { RayanLampClient } from "../api";
import type {
  DiagnosticDeviceInfo,
  DiagnosticEndpointResult,
  DiagnosticField
} from "../contracts";
import { diagnostics, showToast } from "../store";
import { Button, Card, SelectField } from "../components/controls";
import { Icon } from "../components/icon";
import { Modal } from "../components/modal";
import {
  browserConsole,
  browserErrorMetadata
} from "../browser-console";
import {
  BidiLtr,
  formatDateTime,
  formatNumber,
  t
} from "../i18n";

const endpointAllowlist = [
  "/info",
  "/update/info",
  "/wifi/signal",
  "/wifi/status",
  "/wifi/country",
  "/description.xml"
] as const;

export function Diagnostics({ api }: { api: RayanLampClient }) {
  const [loading, setLoading] = useState(!diagnostics.value);
  const [rawEndpoint, setRawEndpoint] =
    useState<(typeof endpointAllowlist)[number]>("/info");
  const [raw, setRaw] = useState("");
  const [rawIsExact, setRawIsExact] = useState(false);
  const [rawConfirm, setRawConfirm] = useState(false);
  const [rawLoading, setRawLoading] = useState(false);
  const current = diagnostics.value;
  const cachedRaw =
    current?.endpoints[rawEndpoint]?.document.raw ?? "";

  useEffect(() => {
    if (diagnostics.value) return;
    let active = true;
    const loadStarted = performance.now();
    void api
      .uiDiagnostics()
      .then((result) => {
        if (active) {
          diagnostics.value = result;
          browserConsole.event(
            "success",
            "diagnostics.loaded",
            "Cached diagnostics loaded",
            {
              source: api.kind,
              count: Object.keys(result.endpoints).length,
              elapsed_ms: Math.round(performance.now() - loadStarted)
            }
          );
        }
      })
      .catch((error) => {
        if (active) {
          browserConsole.event(
            "error",
            "diagnostics.error",
            "Cached diagnostics could not be loaded",
            {
              source: api.kind,
              elapsed_ms: Math.round(performance.now() - loadStarted),
              ...browserErrorMetadata(error)
            }
          );
          showToast(
            "error",
            t("Diagnostics unavailable"),
            error instanceof Error ? error.message : undefined
          );
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [api]);

  const endpointResults = useMemo(
    () =>
      endpointAllowlist
        .map((endpoint) => [endpoint, current?.endpoints[endpoint]] as const)
        .filter((entry) => Boolean(entry[1])) as Array<
          readonly [
            (typeof endpointAllowlist)[number],
            DiagnosticEndpointResult
          ]
        >,
    [current]
  );

  const refresh = async () => {
    const refreshStarted = performance.now();
    try {
      diagnostics.value = {
        ...(diagnostics.value ?? emptyDiagnostics()),
        refreshing: true
      };
      // Omitting a selection asks each transport for its complete safe
      // refresh. The Go coordinator and direct client can then also update
      // their private, redacted Wi-Fi projection sources.
      const result = await api.refreshUIDiagnostics();
      diagnostics.value = result.diagnostics;
      browserConsole.event(
        result.warning ? "warn" : "success",
        "diagnostics.loaded",
        result.warning
          ? "Diagnostics refreshed with a warning"
          : "Diagnostics refreshed",
        {
          source: api.kind,
          action: result.warning ? "partial" : "complete",
          count: Object.keys(result.diagnostics.endpoints).length,
          elapsed_ms: Math.round(performance.now() - refreshStarted)
        }
      );
      if (result.warning) {
        showToast("info", t("Diagnostics refreshed with a warning"), t(result.warning));
      }
      showToast("success", t("Diagnostics refreshed"));
    } catch (error) {
      if (diagnostics.value) {
        diagnostics.value = { ...diagnostics.value, refreshing: false };
      }
      browserConsole.event(
        "error",
        "diagnostics.error",
        "Diagnostics refresh failed",
        {
          source: api.kind,
          action: "refresh",
          elapsed_ms: Math.round(performance.now() - refreshStarted),
          ...browserErrorMetadata(error)
        }
      );
      showToast(
        "error",
        t("Refresh failed"),
        error instanceof Error ? error.message : undefined
      );
    }
  };

  return (
    <div class="view diagnostics-view">
      <header class="view-heading">
        <div>
          <span class="eyebrow">{t("Read-only device data")}</span>
          <h1>{t("Diagnostics")}</h1>
          <p>
            {t("Parsed, redacted information from the firmware-audited endpoint set.")}
          </p>
        </div>
        <Button
          kind="primary"
          icon={<Icon name="refresh" />}
          busy={current?.refreshing || loading}
          disabled={current?.refreshing || loading}
          onClick={() => void refresh()}
        >
          {t("Refresh all")}
        </Button>
      </header>

      {loading && !current ? (
        <Card class="diagnostic-loading">
          <span class="button-spinner" aria-hidden="true" />
          {t("Reading cached device information…")}
        </Card>
      ) : current ? (
        <>
          <div class="diagnostic-summary-grid">
            <SummaryCard
              title="Device"
              icon="info"
              rows={deviceRows(current.device)}
            />
            <SummaryCard
              title="Network"
              icon="network"
              rows={networkRows(current.device)}
            />
            <SummaryCard
              title="Runtime"
              icon="diagnostics"
              rows={runtimeRows(current.device)}
            />
            <SummaryCard
              title="Firmware update"
              icon="refresh"
              rows={updateRows(current.device)}
            />
          </div>

          <Card class="endpoint-card">
            <div class="card-heading-row">
              <div>
                <span class="eyebrow">{t("Endpoint health")}</span>
                <h2>{t("Audited sources")}</h2>
              </div>
              <span class="updated-label">
                {t("Updated {date}", { date: formatDate(current.updated_at) })}
              </span>
            </div>
            <div class="endpoint-list">
              {endpointAllowlist.map((endpoint) => (
                <EndpointStatus
                  endpoint={endpoint}
                  result={current.endpoints[endpoint]}
                  key={endpoint}
                />
              ))}
            </div>
          </Card>

          {endpointResults.map(([endpoint, result]) =>
            result.document.fields.length > 0 ? (
              <Card class="field-card" key={endpoint}>
                <div class="card-heading-row">
                  <div>
                    <span class="eyebrow"><BidiLtr>{result.document.format}</BidiLtr></span>
                    <h2><BidiLtr>{endpoint}</BidiLtr></h2>
                  </div>
                  <span class="updated-label">
                    {formatBytes(result.document.bytes)}
                  </span>
                </div>
                <div class="diagnostic-fields">
                  {result.document.fields.map((field) => (
                    <DiagnosticValue field={field} key={field.key} />
                  ))}
                </div>
                {result.document.warnings?.map((warning) => (
                  <p class="diagnostic-warning" key={warning}>
                    <Icon name="warning" size={16} />
                    {t(warning)}
                  </p>
                ))}
              </Card>
            ) : null
          )}

          <Card class="advanced-card">
            <details>
              <summary>
                <span>
                  <Icon name="terminal" />
                  {t("Advanced raw response")}
                </span>
                <small>{t("Cached redacted output by default")}</small>
              </summary>
              <div class="advanced-content">
                <p>
                  {t("The cached response below is the backend's redacted SafeRaw projection. An explicit exact fetch is available with a sensitivity warning and is always rendered as plain text.")}
                </p>
                <div class="advanced-toolbar">
                  <SelectField
                    label="Endpoint"
                    value={rawEndpoint}
                    options={endpointAllowlist.map((endpoint) => ({
                      value: endpoint,
                      label: endpoint
                    }))}
                    onChange={(endpoint) => {
                      setRawEndpoint(endpoint);
                      setRaw("");
                      setRawIsExact(false);
                    }}
                  />
                  <Button
                    busy={rawLoading}
                    disabled={rawLoading}
                    kind="danger"
                    onClick={() => setRawConfirm(true)}
                  >
                    {t("Fetch exact raw")}
                  </Button>
                </div>
                {rawIsExact && (
                  <div class="security-note" role="status">
                    <Icon name="warning" />
                    <p>
                      {t("Exact device response: review before copying or sharing. It may contain sensitive identifiers or network data.")}
                    </p>
                  </div>
                )}
                <pre class="raw-output" dir="ltr" tabIndex={0}>
                  {raw ||
                    cachedRaw ||
                    t("No cached redacted response is available for this endpoint.")}
                </pre>
              </div>
            </details>
          </Card>
          <Modal
            open={rawConfirm}
            title={t("Fetch the exact device response?")}
            description={t("This bypasses the cached redacted view. The response may contain sensitive device identifiers or local network information.")}
            onClose={() => setRawConfirm(false)}
          >
            <div class="modal-actions">
              <Button kind="quiet" onClick={() => setRawConfirm(false)}>
                {t("Keep redacted view")}
              </Button>
              <Button
                kind="danger"
                onClick={async () => {
                  setRawConfirm(false);
                  setRawLoading(true);
                  const exactStarted = performance.now();
                  try {
                    const result = await api.diagnostics(rawEndpoint);
                    setRaw(result.content);
                    setRawIsExact(true);
                    browserConsole.event(
                      "success",
                      "diagnostics.loaded",
                      "Exact diagnostic response fetched",
                      {
                        source: api.kind,
                        endpoint: rawEndpoint,
                        bytes: result.bytes,
                        elapsed_ms: Math.round(
                          performance.now() - exactStarted
                        )
                      }
                    );
                  } catch (error) {
                    browserConsole.event(
                      "error",
                      "diagnostics.error",
                      "Exact diagnostic response failed",
                      {
                        source: api.kind,
                        endpoint: rawEndpoint,
                        elapsed_ms: Math.round(
                          performance.now() - exactStarted
                        ),
                        ...browserErrorMetadata(error)
                      }
                    );
                    setRaw(
                      error instanceof Error
                        ? error.message
                        : t("Unable to fetch endpoint.")
                    );
                    setRawIsExact(false);
                  } finally {
                    setRawLoading(false);
                  }
                }}
              >
                {t("Fetch exact response")}
              </Button>
            </div>
          </Modal>
        </>
      ) : (
        <Card class="empty-card">
          <Icon name="warning" size={32} />
          <h2>{t("No diagnostic snapshot")}</h2>
          <p>{t("The controller has not returned parsed device information yet.")}</p>
          <Button onClick={() => void refresh()}>{t("Try again")}</Button>
        </Card>
      )}
    </div>
  );
}

function SummaryCard({
  title,
  icon,
  rows
}: {
  title: string;
  icon: "info" | "network" | "diagnostics" | "refresh";
  rows: Array<[string, string]>;
}) {
  return (
    <Card class="summary-card">
      <div class="summary-title">
        <span><Icon name={icon} /></span>
        <h2>{t(title)}</h2>
      </div>
      <dl>
        {rows.map(([label, value]) => (
          <div key={label}>
            <dt>{t(label)}</dt>
            <dd>
              {value
                ? isTechnicalSummary(label)
                  ? <BidiLtr>{value}</BidiLtr>
                  : t(value)
                : t("Not reported")}
            </dd>
          </div>
        ))}
      </dl>
    </Card>
  );
}

function EndpointStatus({
  endpoint,
  result
}: {
  endpoint: string;
  result?: DiagnosticEndpointResult;
}) {
  const tone = result?.error ? "error" : result?.stale ? "stale" : result ? "ok" : "idle";
  return (
    <div class="endpoint-row">
      <span class={`endpoint-state endpoint-${tone}`} aria-hidden="true" />
      <code dir="ltr">{endpoint}</code>
      <span>
        {result?.error
          ? t(result.error)
          : result?.stale
            ? t("Cached result")
            : result
              ? t("Fetched {date}", { date: formatDate(result.last_success) })
              : t("Not fetched")}
      </span>
    </div>
  );
}

function DiagnosticValue({ field }: { field: DiagnosticField }) {
  return (
    <div class={field.redacted ? "is-redacted" : ""}>
      <span>{t(field.label)}</span>
      <strong>
        {field.redacted
          ? t("Protected")
          : diagnosticFieldIsTechnical(field)
            ? <BidiLtr>{field.value || "—"}</BidiLtr>
            : formatDiagnosticField(field)}
      </strong>
    </div>
  );
}

function deviceRows(device: DiagnosticDeviceInfo): Array<[string, string]> {
  return [
    ["Name", device.device_name || device.description?.friendly_name || ""],
    ["Hostname", device.hostname || ""],
    ["Model", device.description?.model_name || ""],
    ["Firmware", device.firmware || device.build || ""],
    ["State", device.reported_state || ""]
  ];
}

function networkRows(device: DiagnosticDeviceInfo): Array<[string, string]> {
  return [
    ["Wi-Fi", device.wifi_connected === undefined ? "" : device.wifi_connected ? t("Connected") : t("Disconnected")],
    ["SSID", device.ssid || ""],
    ["Address", device.wifi_ip || ""],
    ["Station status", device.station_status || ""],
    ["Access point", device.access_point_ssid || device.access_point_status || ""],
    ["AP address", device.access_point_ip || ""],
    ["Signal", device.signal_quality || (device.rssi_dbm === undefined ? "" : `${formatNumber(device.rssi_dbm)} dBm`)],
    ["Country", device.country || ""]
  ];
}

function runtimeRows(device: DiagnosticDeviceInfo): Array<[string, string]> {
  return [
    ["Uptime", device.uptime_seconds === undefined ? "" : formatDuration(device.uptime_seconds)],
    ["Free heap", device.heap_free_bytes === undefined ? "" : formatBytes(device.heap_free_bytes)],
    ["Program usage", device.program_usage_percent === undefined ? "" : `${formatNumber(device.program_usage_percent, { maximumFractionDigits: 1 })}%`],
    ["Protocol", device.protocol_revision || ""],
    ["Firmware integrity", device.firmware_corrupted === undefined ? "" : device.firmware_corrupted ? t("Attention required") : t("Verified")]
  ];
}

function updateRows(device: DiagnosticDeviceInfo): Array<[string, string]> {
  const update = device.update ?? {};
  return [
    ["Build", update.build || device.build || ""],
    ["Hash", update.hash || device.firmware_hash || ""],
    ["Image size", update.size_bytes === undefined ? "" : formatBytes(update.size_bytes)],
    ["Free space", update.free_bytes === undefined ? "" : formatBytes(update.free_bytes)],
    ["Date", formatDate(update.date)]
  ];
}

function formatDate(value?: string): string {
  if (!value) return t("Not reported");
  const parsed = new Date(value);
  if (Number.isNaN(parsed.valueOf())) return value;
  return formatDateTime(parsed, {
    dateStyle: "medium",
    timeStyle: "short"
  });
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value)) return t("Not reported");
  const units = ["B", "KB", "MB", "GB"];
  let amount = value;
  let index = 0;
  while (Math.abs(amount) >= 1024 && index < units.length - 1) {
    amount /= 1024;
    index++;
  }
  return `${formatNumber(amount, {
    minimumFractionDigits: 0,
    maximumFractionDigits: index === 0 ? 0 : 1
  })} ${units[index]}`;
}

function formatDuration(seconds: number): string {
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3_600);
  const minutes = Math.floor((seconds % 3_600) / 60);
  return [
    days ? t("{count}d", { count: formatNumber(days) }) : "",
    hours ? t("{count}h", { count: formatNumber(hours) }) : "",
    t("{count}m", { count: formatNumber(minutes) })
  ]
    .filter(Boolean)
    .join(" ");
}

function isTechnicalSummary(label: string): boolean {
  return [
    "Hostname",
    "Firmware",
    "SSID",
    "Address",
    "Station status",
    "Access point",
    "AP address",
    "Protocol",
    "Build",
    "Hash"
  ].includes(label);
}

function diagnosticFieldIsTechnical(field: DiagnosticField): boolean {
  return [
    "ip",
    "mac",
    "hash",
    "time"
  ].includes(field.kind);
}

function formatDiagnosticField(field: DiagnosticField): string {
  switch (field.kind) {
    case "integer":
      return field.integer === undefined
        ? localizeDiagnosticDigits(field.value)
        : formatNumber(field.integer);
    case "number":
      return field.number === undefined
        ? localizeDiagnosticDigits(field.value)
        : formatNumber(field.number, { maximumFractionDigits: 2 });
    case "bytes":
      return field.integer === undefined
        ? localizeDiagnosticDigits(field.value)
        : formatBytes(field.integer);
    case "percent":
      return field.number === undefined
        ? localizeDiagnosticDigits(field.value)
        : `${formatNumber(field.number, { maximumFractionDigits: 1 })}%`;
    case "signal":
      return field.number === undefined
        ? localizeDiagnosticDigits(field.value)
        : `${formatNumber(field.number, { maximumFractionDigits: 1 })} dBm`;
    case "duration":
      return field.duration_seconds === undefined
        ? localizeDiagnosticDigits(field.value)
        : formatDuration(field.duration_seconds);
    case "boolean":
      return field.boolean === undefined
        ? t(field.value || "—")
        : field.boolean
          ? t("Yes")
          : t("No");
    default:
      return t(field.value || "—");
  }
}

function localizeDiagnosticDigits(value: string): string {
  return value.replace(/\d+(?:\.\d+)?/g, (part) => {
    const number = Number(part);
    return Number.isFinite(number)
      ? formatNumber(number, { maximumFractionDigits: 2 })
      : part;
  });
}

function emptyDiagnostics() {
  return {
    device_url: "",
    device: {},
    endpoints: {},
    refreshing: false
  };
}
