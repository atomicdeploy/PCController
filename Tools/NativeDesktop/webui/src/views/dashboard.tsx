import { useState } from "preact/hooks";
import type { PCControllerDesktopClient } from "../api";
import type { Snapshot } from "../contracts";
import {
  appPhase,
  applySnapshot,
  capabilities,
  commandPending,
  diagnostics,
  isOnline,
  showToast,
  snapshot,
  stateLabel
} from "../store";
import { Button, Card } from "../components/controls";
import { Icon } from "../components/icon";
import { DeviceVisual } from "../components/device-visual";
import { Modal } from "../components/modal";
import {
  browserConsole,
  browserErrorMetadata
} from "../browser-console";
import { webHaptics } from "../haptics";
import {
  BidiLtr,
  formatDateTime,
  formatNumber,
  t
} from "../i18n";

export function Dashboard({
  api,
  onSnapshot = applySnapshot
}: {
  api: PCControllerDesktopClient;
  onSnapshot?: (next: Snapshot) => void;
}) {
  const [reconnectOpen, setReconnectOpen] = useState(false);
  const current = snapshot.value;
  const device = diagnostics.value?.device;
  const displayName =
    device?.device_name ||
    device?.description?.friendly_name ||
    t("PCController");
  const busy = Boolean(commandPending.value || current.pending_command);

  const run = async (
    command:
      | "on"
      | "off"
      | "toggle"
      | "refresh"
      | "reconnect"
      | "window/show"
      | "window/hide",
    success?: string
  ) => {
    if (commandPending.value) return;
    commandPending.value = command;
    webHaptics.trigger("command.start");
    const commandStarted = performance.now();
    browserConsole.event(
      "info",
      "device.command",
      `Sending ${command.replace("/", " ")} command`,
      {
        source: api.kind,
        action: command
      }
    );
    try {
      await api.command(command);
      const next = await api.status();
      onSnapshot(next);
      browserConsole.event(
        "success",
        "device.command",
        `${command.replace("/", " ")} command completed`,
        {
          source: api.kind,
          action: command,
          state: next.state,
          elapsed_ms: Math.round(performance.now() - commandStarted)
        }
      );
      webHaptics.trigger("command.success");
      if (success) showToast("success", success);
    } catch (error) {
      browserConsole.event(
        "error",
        "device.command",
        `${command.replace("/", " ")} command failed`,
        {
          source: api.kind,
          action: command,
          elapsed_ms: Math.round(performance.now() - commandStarted),
          ...browserErrorMetadata(error)
        }
      );
      webHaptics.trigger("command.error");
      showToast(
        "error",
        t("Command failed"),
        error instanceof Error ? t(error.message) : t("The device did not respond.")
      );
    } finally {
      commandPending.value = "";
    }
  };

  return (
    <div class="view dashboard-view">
      <header class="view-heading">
        <div>
          <span class="eyebrow">{t("Office lighting")}</span>
          <h1>{displayName}</h1>
          <p>{t("Local, private control with a resilient live connection.")}</p>
        </div>
        <span class={`phase-pill phase-${appPhase.value}`}>
          <span class="phase-dot" aria-hidden="true" />
          {phaseLabel(appPhase.value)}
        </span>
      </header>

      <div class="dashboard-grid">
        <Card class="device-hero">
          <div class="device-hero-copy">
            <span class="eyebrow">
              {device?.hostname || current.device_url
                ? <BidiLtr>{device?.hostname || current.device_url}</BidiLtr>
                : t("Configured device")}
            </span>
            <h2>{stateLabel.value}</h2>
            <p>{stateDetail(current.last_error, current.pending_command)}</p>
          </div>
          <button
            type="button"
            class="device-toggle"
            aria-label={
              current.state === "on" ? t("Turn device off") : t("Turn device on")
            }
            disabled={!isOnline.value || busy}
            onClick={() => void run("toggle")}
          >
            <DeviceVisual
              state={current.state}
              offline={!isOnline.value}
              busy={busy}
            />
          </button>
          <div class="hero-actions" aria-label={t("Device controls")}>
            <Button
              kind="primary"
              icon={<Icon name="power" />}
              disabled={!isOnline.value || busy}
              busy={commandPending.value === "toggle"}
              onClick={() => void run("toggle")}
            >
              {t("Toggle device")}
            </Button>
            <Button
              disabled={!isOnline.value || busy || current.state === "on"}
              busy={commandPending.value === "on"}
              onClick={() => void run("on")}
            >
              {t("Turn on")}
            </Button>
            <Button
              disabled={!isOnline.value || busy || current.state === "off"}
              busy={commandPending.value === "off"}
              onClick={() => void run("off")}
            >
              {t("Turn off")}
            </Button>
          </div>
        </Card>

        <div class="dashboard-side">
          <Card class="status-card" aria-labelledby="connection-heading">
            <div class="card-heading-row">
              <div>
                <span class="eyebrow">{t("Connection")}</span>
                <h2 id="connection-heading">{t("Device channels")}</h2>
              </div>
              <Icon name="network" size={24} />
            </div>
            <dl class="status-list">
              <StatusRow
                label={t("HTTP control")}
                active={current.http_reachable}
                value={current.http_reachable ? t("Available") : t("Unavailable")}
              />
              <StatusRow
                label={t("WebSocket events")}
                active={current.websocket_online}
                value={current.websocket_online ? t("Connected") : t("Disconnected")}
              />
              <div>
                <dt>{t("Last source")}</dt>
                <dd>
                  {current.last_source
                    ? <BidiLtr>{current.last_source}</BidiLtr>
                    : t("Waiting for an event")}
                </dd>
              </div>
              <div>
                <dt>{t("Last change")}</dt>
                <dd>{formatTime(current.last_changed)}</dd>
              </div>
              {device?.firmware && (
                <div>
                  <dt>{t("Firmware")}</dt>
                  <dd><BidiLtr>{device.firmware}</BidiLtr></dd>
                </div>
              )}
              {(device?.signal_quality || device?.rssi_dbm !== undefined) && (
                <div>
                  <dt>{t("Wi-Fi signal")}</dt>
                  <dd>
                    {device.signal_quality ||
                      `${formatNumber(device.rssi_dbm ?? 0)} dBm`}
                  </dd>
                </div>
              )}
            </dl>
            <div class="card-actions">
              <Button
                kind="quiet"
                icon={<Icon name="refresh" />}
                busy={commandPending.value === "refresh"}
                disabled={busy}
                onClick={() => void run("refresh", t("Device state refreshed"))}
              >
                {t("Refresh")}
              </Button>
              <Button
                kind="quiet"
                icon={<Icon name="reconnect" />}
                disabled={busy}
                onClick={() => setReconnectOpen(true)}
              >
                {t("Reconnect")}
              </Button>
            </div>
          </Card>

          {capabilities.value.window_control !== false && (
            <Card class="quick-card" aria-labelledby="window-heading">
              <div class="card-heading-row">
                <div>
                  <span class="eyebrow">{t("Desktop app")}</span>
                  <h2 id="window-heading">{t("Native window")}</h2>
                </div>
                <Icon name="window" size={24} />
              </div>
              <p>
                {t("Bring the compact controller forward or tuck it back into the notification area.")}
              </p>
              <div class="card-actions">
                <Button
                  onClick={() =>
                    void run("window/show", t("Native window opened"))
                  }
                >
                  {t("Show")}
                </Button>
                <Button
                  kind="quiet"
                  onClick={() =>
                    void run("window/hide", t("Native window hidden"))
                  }
                >
                  {t("Hide")}
                </Button>
              </div>
            </Card>
          )}
        </div>
      </div>

      <Modal
        open={reconnectOpen}
        title={t("Reconnect to PCController?")}
        description={t("The current HTTP and WebSocket transports will restart. The cached .local address is retained unless the connection fails.")}
        onClose={() => setReconnectOpen(false)}
      >
        <div class="modal-actions">
          <Button kind="quiet" onClick={() => setReconnectOpen(false)}>
            {t("Keep connection")}
          </Button>
          <Button
            kind="primary"
            icon={<Icon name="reconnect" />}
            onClick={() => {
              setReconnectOpen(false);
              void run("reconnect", t("Reconnection started"));
            }}
          >
            {t("Reconnect")}
          </Button>
        </div>
      </Modal>
    </div>
  );
}

function StatusRow({
  label,
  active,
  value
}: {
  label: string;
  active: boolean;
  value: string;
}) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>
        <span class={`mini-status ${active ? "is-active" : "is-inactive"}`}>
          <span aria-hidden="true" />
          {value}
        </span>
      </dd>
    </div>
  );
}

function phaseLabel(phase: string): string {
  switch (phase) {
    case "online":
      return t("Connected");
    case "degraded":
      return t("Partially connected");
    case "connecting":
      return t("Reconnecting");
    case "offline":
      return t("Unavailable");
    default:
      return t("Starting");
  }
}

function stateDetail(lastError?: string, pending?: string): string {
  if (pending) return t("Applying {command}…", { command: pending.replace("device.", "") });
  if (lastError) return t(lastError);
  return t("Press the device or use an explicit action below.");
}

function formatTime(value?: string): string {
  if (!value) return t("Not reported yet");
  const parsed = new Date(value);
  if (Number.isNaN(parsed.valueOf())) return value;
  return formatDateTime(parsed, {
    dateStyle: "medium",
    timeStyle: "short"
  });
}
