import { useEffect, useRef, useState } from "preact/hooks";
import { APIError, type PCControllerDesktopClient } from "../api";
import { audioImportAccept } from "../audio";
import type {
  AppConfig,
  DeepPartial,
  HotkeyCaptureLeaseResult,
  IntegrationRegistrationAction,
  IntegrationRegistrationStatus
} from "../contracts";
import {
  capabilities,
  config,
  acceptRemoteConfig,
  flushConfigPatch,
  revision,
  settingsError,
  settingsSaving,
  settingsSection,
  setWebLocale,
  showToast,
  updateConfig
} from "../store";
import {
  Button,
  Card,
  FieldGroup,
  NumberField,
  SelectField,
  TextAreaField,
  TextField,
  Toggle
} from "../components/controls";
import {
  HotkeyRecorder,
  type HotkeyConflict
} from "../components/hotkey-recorder";
import { Icon, type IconName } from "../components/icon";
import { Modal } from "../components/modal";
import { BidiLtr, t } from "../i18n";

type SettingsKey = keyof AppConfig;

const sections: Array<{
  key: SettingsKey;
  label: string;
  description: string;
  icon: IconName;
}> = [
  { key: "device", label: "Device", description: "Address and transport", icon: "device" },
  { key: "ui", label: "Appearance", description: "Native window and material", icon: "settings" },
  { key: "notifications", label: "Notifications", description: "Toasts, balloons, and sound", icon: "bell" },
  { key: "hotkeys", label: "Hotkeys", description: "System-wide shortcuts", icon: "keyboard" },
  { key: "automation", label: "Automation", description: "State-change scripts", icon: "refresh" },
  { key: "integration", label: "Integration", description: "Startup and local API", icon: "network" },
  { key: "diagnostics", label: "Diagnostics", description: "Endpoint allowlists", icon: "diagnostics" }
];

export function Settings({ api }: { api: PCControllerDesktopClient }) {
  const current = config.value;
  const [disableAPIOpen, setDisableAPIOpen] = useState(false);
  const [toastOpen, setToastOpen] = useState(false);
  const [toastMessage, setToastMessage] = useState(() => t("PCController notifications are ready."));
  const [testingToast, setTestingToast] = useState(false);
  const direct = capabilities.value.transport_direct === true;
  const visualNotifications =
    capabilities.value.notifications !== false && !direct;
  const audioAvailable = capabilities.value.audio_sfx !== false;
  const visibleSections = sections.filter((item) => {
    switch (item.key) {
      case "ui":
        return true;
      case "notifications":
        return visualNotifications || audioAvailable;
      case "hotkeys":
        return capabilities.value.native_shell !== false && !direct;
      case "automation":
        return capabilities.value.automation !== false && !direct;
      case "integration":
        return !direct;
      case "diagnostics":
        return capabilities.value.diagnostics !== false;
      default:
        return true;
    }
  });

  if (!current) {
    return <div class="view"><div class="loading-card">{t("Loading settings…")}</div></div>;
  }

  const section = visibleSections.some(
    (item) => item.key === settingsSection.value
  )
    ? settingsSection.value
    : visibleSections[0]?.key ?? "device";
  const patch = (value: DeepPartial<AppConfig>, immediate = false) =>
    updateConfig(value, immediate);

  return (
    <div class="view settings-view">
      <header class="view-heading">
        <div>
          <span class="eyebrow">{t("Configuration")}</span>
          <h1>{t("Settings")}</h1>
          <p>
            {direct
              ? t("Direct connection preferences are stored in this browser and synchronized across tabs.")
              : t("Changes apply automatically and reconcile with the native app.")}
          </p>
        </div>
        <div
          class={`save-indicator${settingsError.value ? " has-error" : ""}`}
          role="status"
          aria-live="polite"
        >
          {settingsSaving.value ? (
            <>
              <span class="button-spinner" aria-hidden="true" /> {t("Saving…")}
            </>
          ) : settingsError.value ? (
            <>
              <Icon name="warning" size={16} /> {t("Needs attention")}
            </>
          ) : (
            <>
              <Icon name="check" size={16} /> {t("Up to date")}
            </>
          )}
        </div>
      </header>

      {settingsError.value && (
        <div class="error-banner" role="alert">
          <Icon name="warning" />
          <div>
            <strong>{t("Latest change was not accepted.")}</strong>
            <span>{t(settingsError.value)}</span>
          </div>
          <Button kind="quiet" onClick={() => void flushConfigPatch()}>
            {t("Retry")}
          </Button>
        </div>
      )}

      <div class="settings-layout">
        <nav class="settings-nav" aria-label={t("Settings categories")}>
          {visibleSections.map((item) => (
            <button
              type="button"
              key={item.key}
              class={section === item.key ? "is-current" : ""}
              aria-current={section === item.key ? "page" : undefined}
              onClick={() => {
                settingsSection.value = item.key;
              }}
            >
              <Icon name={item.icon} />
              <span>
                <strong>{t(item.label)}</strong>
                <small>{t(item.description)}</small>
              </span>
              <Icon name="chevron" size={16} class="settings-chevron" />
            </button>
          ))}
        </nav>

        <Card class="settings-panel">
          {section === "device" && (
            <DeviceSettings value={current} patch={patch} direct={direct} />
          )}
          {section === "ui" && (
            <AppearanceSettings
              value={current}
              patch={patch}
              native={capabilities.value.native_shell !== false && !direct}
            />
          )}
          {section === "notifications" && (
            <NotificationSettings
              api={api}
              value={current}
              patch={patch}
              direct={direct}
              visualAvailable={visualNotifications}
              audioAvailable={audioAvailable}
              onTest={() => setToastOpen(true)}
            />
          )}
          {section === "hotkeys" && (
            <HotkeySettings
              api={api}
              captureAvailable={capabilities.value.hotkey_capture === true}
              probeAvailable={capabilities.value.hotkey_probe === true}
              value={current}
              patch={patch}
            />
          )}
          {section === "automation" && <AutomationSettings value={current} patch={patch} />}
          {section === "integration" && (
            <IntegrationSettings
              api={api}
              value={current}
              patch={patch}
              onDisableAPI={() => setDisableAPIOpen(true)}
            />
          )}
          {section === "diagnostics" && (
            <DiagnosticsSettings value={current} patch={patch} />
          )}
        </Card>
      </div>

      <Modal
        open={disableAPIOpen}
        title={t("Turn off the local API?")}
        description={t("The bundled web dashboard uses this same private loopback listener. This page will disconnect immediately.")}
        onClose={() => setDisableAPIOpen(false)}
      >
        <div class="modal-actions">
          <Button kind="quiet" onClick={() => setDisableAPIOpen(false)}>
            {t("Keep enabled")}
          </Button>
          <Button
            kind="danger"
            onClick={() => {
              setDisableAPIOpen(false);
              patch({ integration: { local_api_enabled: false } }, true);
            }}
          >
            {t("Disable and disconnect")}
          </Button>
        </div>
      </Modal>

      <Modal
        open={toastOpen}
        title={t("Test a notification")}
        description={t("Visual delivery and sound remain independent and follow the settings above.")}
        onClose={() => setToastOpen(false)}
      >
        <TextAreaField
          label={t("Message")}
          value={toastMessage}
          rows={3}
          onInput={setToastMessage}
        />
        <div class="modal-actions">
          <Button kind="quiet" onClick={() => setToastOpen(false)}>
            {t("Cancel")}
          </Button>
          <Button
            kind="primary"
            busy={testingToast}
            disabled={!toastMessage.trim() || testingToast}
            onClick={async () => {
              setTestingToast(true);
              try {
                await api.notify(t("PCController test"), toastMessage.trim());
                showToast("success", t("Test notification sent"));
                setToastOpen(false);
              } catch (error) {
                showToast(
                  "error",
                  t("Notification test failed"),
                  error instanceof Error ? error.message : undefined
                );
              } finally {
                setTestingToast(false);
              }
            }}
          >
            {t("Send test")}
          </Button>
        </div>
      </Modal>
    </div>
  );
}

type SettingsProps = {
  value: AppConfig;
  patch: (value: DeepPartial<AppConfig>, immediate?: boolean) => void;
};

function PanelHeading({
  title,
  description
}: {
  title: string;
  description: string;
}) {
  return (
    <header class="panel-heading">
      <h2>{t(title)}</h2>
      <p>{t(description)}</p>
    </header>
  );
}

export function DeviceSettings({
  value,
  patch,
  direct
}: SettingsProps & { direct: boolean }) {
  const device = value.device;
  return (
    <>
      <PanelHeading
        title="Device connection"
        description={
          direct
            ? "This browser talks to the device over CORS-enabled HTTP and its firmware WebSocket."
            : "The native controller owns discovery, HTTP commands, and the resilient event socket."
        }
      />
      <FieldGroup title="Device">
        <TextField
          label="Device address"
          type="url"
          value={device.base_url}
          placeholder="http://pccontroller-desktop.local"
          description="HTTP or HTTPS origin without a path, query, or embedded credentials."
          onInput={(base_url) => patch({ device: { base_url } })}
        />
        <TextField
          label="Request timeout"
          dir="ltr"
          value={device.request_timeout}
          description={
            direct
              ? "Duration from 100ms through 2m."
              : "Go duration from 100ms through 5m."
          }
          onInput={(request_timeout) => patch({ device: { request_timeout } })}
        />
        <NumberField
          label="Device terminal port"
          value={device.telnet_port}
          min={1}
          max={65_535}
          description={
            direct
              ? "Retained for configuration compatibility; direct browsers cannot open this raw TCP port."
              : "Audited Telnet-style device console port. The default firmware uses TCP 23."
          }
          onInput={(telnet_port) =>
            patch({ device: { telnet_port } }, true)
          }
        />
      </FieldGroup>
      <FieldGroup title="Live updates">
        <Toggle
          label="Listen for WebSocket events"
          description="Connect to /ws, request one state, then passively receive updates."
          checked={device.websocket}
          onChange={(websocket) => patch({ device: { websocket } }, true)}
        />
        <TextField
          label="Reconnect minimum"
          dir="ltr"
          value={device.reconnect_min}
          onInput={(reconnect_min) => patch({ device: { reconnect_min } })}
        />
        <TextField
          label="Reconnect maximum"
          dir="ltr"
          value={device.reconnect_max}
          onInput={(reconnect_max) => patch({ device: { reconnect_max } })}
        />
        <TextField
          label="HTTP polling interval"
          dir="ltr"
          value={device.http_poll}
          description="Use 0s to disable fallback polling."
          onInput={(http_poll) => patch({ device: { http_poll } })}
        />
      </FieldGroup>
    </>
  );
}

export function AppearanceSettings({
  value,
  patch,
  native = true
}: SettingsProps & { native?: boolean }) {
  const ui = value.ui;
  const uiPatch = (next: Partial<typeof ui>, immediate = true) =>
    patch({ ui: next }, immediate);
  return (
    <>
      <PanelHeading
        title={native ? "Native app appearance" : "Web interface appearance"}
        description={
          native
            ? "Language and theme are shared with this dashboard; the remaining controls affect the Windows window."
            : "Language, direction, typography, theme, and motion apply to this browser immediately."
        }
      />
      <FieldGroup title={native ? "Layout and material" : "Dashboard appearance"}>
        <div class="two-column-fields">
          {native && (
            <SelectField
              label="Window layout"
              value={ui.view_mode}
              options={[
                { value: "compact", label: "Compact device" },
                { value: "dashboard", label: "Larger dashboard" }
              ]}
              onChange={(view_mode) => uiPatch({ view_mode })}
            />
          )}
          <SelectField
            label="Color theme"
            value={ui.theme}
            options={[
              { value: "system", label: "Use Windows setting" },
              { value: "light", label: "Light" },
              { value: "dark", label: "Dark" }
            ]}
            onChange={(theme) => uiPatch({ theme })}
          />
          <SelectField
            label="Interface language"
            description="Changes direction, typography, digits, and reader-facing dates immediately."
            value={ui.locale ?? "auto"}
            options={[
              { value: "auto", label: "Follow system language" },
              { value: "en", label: "English (US)" },
              { value: "fa", label: "فارسی" }
            ]}
            onChange={(locale) => setWebLocale(locale)}
          />
          {native && (
            <>
              <SelectField
                label="Backdrop"
            value={ui.backdrop}
            options={[
              { value: "auto", label: "Automatic" },
              { value: "solid", label: "Solid (no effect)" },
              { value: "transparent", label: "Transparent" },
              { value: "blur", label: "Blur behind" },
              { value: "acrylic", label: "Acrylic" },
              { value: "mica", label: "Mica" },
              { value: "tabbed", label: "Tabbed" }
            ]}
            onChange={(backdrop) =>
              uiPatch({
                backdrop,
                ...(backdrop === "transparent" &&
                ui.title_bar_mode === "native"
                  ? {
                      title_bar_mode: "custom" as const,
                      show_title_bar: true
                    }
                  : {})
              })
            }
              />
              <SelectField
                label="Rendering backend"
            description="Automatic uses DirectX 11 hardware composition when supported. Choose Compatibility for lower RAM while retaining the same UI features."
            value={ui.render_backend}
            options={[
              {
                value: "auto",
                label: "Automatic (DirectX when supported)"
              },
              { value: "directx", label: "DirectX 11 hardware" },
              {
                value: "legacy",
                label: "Compatibility renderer (lower RAM)"
              }
            ]}
            onChange={(render_backend) => uiPatch({ render_backend })}
              />
              <SelectField
                label="Title bar"
            value={ui.title_bar_mode}
            options={[
              { value: "none", label: "None" },
              { value: "custom", label: "Custom controls" },
              { value: "native", label: "Native Windows title bar" }
            ]}
            onChange={(title_bar_mode) =>
              uiPatch({
                title_bar_mode,
                show_title_bar: title_bar_mode === "custom",
                ...(title_bar_mode === "native" &&
                ui.backdrop === "transparent"
                  ? { backdrop: "solid" as const }
                  : {}),
                ...(title_bar_mode === "native" &&
                ui.border_mode === "none"
                  ? { border_mode: "auto" as const }
                  : {})
              })
            }
              />
              <SelectField
                label="Window border"
            description="Automatic keeps edge resizing. None removes the outer frame."
            value={ui.border_mode}
            options={[
              { value: "auto", label: "Automatic (resizable)" },
              { value: "none", label: "None (frameless)" }
            ]}
            onChange={(border_mode) =>
              uiPatch({
                border_mode,
                ...(border_mode === "none" &&
                ui.title_bar_mode === "native"
                  ? {
                      title_bar_mode: "custom" as const,
                      show_title_bar: true
                    }
                  : {})
              })
            }
              />
            </>
          )}
        </div>
        {native && (
          <>
            <Toggle
              label="Rounded corners"
              checked={ui.rounded_corners}
              onChange={(rounded_corners) => uiPatch({ rounded_corners })}
            />
            <Toggle
              label="Layered Fluent foreground"
              description="Sharp SVG artwork, alpha-composited controls, reveal lighting, and motion."
              checked={ui.layered_foreground}
              onChange={(layered_foreground) => uiPatch({ layered_foreground })}
            />
          </>
        )}
        <Toggle
          label="Animations"
          checked={ui.animations}
          onChange={(animations) => uiPatch({ animations })}
        />
      </FieldGroup>
      {native && (
        <>
          <FieldGroup title="Window behavior">
        <Toggle
          label="Always on top"
          checked={ui.always_on_top}
          onChange={(always_on_top) => uiPatch({ always_on_top })}
        />
        <Toggle
          label="Close to notification area"
          checked={ui.close_to_tray}
          onChange={(close_to_tray) => uiPatch({ close_to_tray })}
        />
        <Toggle
          label="Preload and reuse the notification-area menu"
          description="Prepares the menu invisibly and reuses its window between openings to avoid first-frame flashes. Disable to recreate it each time."
          checked={ui.prewarm_tray_menu}
          onChange={(prewarm_tray_menu) => uiPatch({ prewarm_tray_menu })}
        />
        <Toggle
          label="Start minimized"
          checked={ui.start_minimized}
          onChange={(start_minimized) => uiPatch({ start_minimized })}
        />
        <Toggle
          label="Show taskbar button"
          checked={ui.show_taskbar}
          onChange={(show_taskbar) => uiPatch({ show_taskbar })}
        />
        <Toggle
          label="Magnetic snap"
          description="Hold Ctrl while dragging to bypass snapping; release Ctrl to resume it immediately."
          checked={ui.magnetic_snap}
          onChange={(magnetic_snap) => uiPatch({ magnetic_snap })}
        />
        <NumberField
          label="Snap distance"
          description="Device-independent pixels; 1 through 128."
          value={ui.magnetic_snap_distance}
          min={1}
          max={128}
          onInput={(magnetic_snap_distance) =>
            uiPatch({ magnetic_snap_distance }, false)
          }
        />
          </FieldGroup>
          <FieldGroup
            title="Stored window bounds"
            description="Use -1 for X or Y to request default centering."
          >
        <div class="four-column-fields">
          {(["x", "y", "width", "height"] as const).map((key) => (
            <NumberField
              key={key}
              label={key.toUpperCase()}
              value={ui.window_bounds[key]}
              onInput={(next) =>
                uiPatch({
                  window_bounds: { ...ui.window_bounds, [key]: next }
                }, false)
              }
            />
          ))}
        </div>
          </FieldGroup>
        </>
      )}
    </>
  );
}

export function NotificationSettings({
  api,
  value,
  patch,
  direct,
  visualAvailable,
  audioAvailable,
  onTest
}: SettingsProps & {
  api: PCControllerDesktopClient;
  direct: boolean;
  visualAvailable: boolean;
  audioAvailable: boolean;
  onTest: () => void;
}) {
  const item = value.notifications;
  const fileInput = useRef<HTMLInputElement>(null);
  const [importing, setImporting] = useState(false);
  const [testingAudio, setTestingAudio] = useState(false);
  const update = (next: Partial<typeof item>) =>
    patch({ notifications: next }, true);

  const chooseFile = () => fileInput.current?.click();
  const importFile = async (file?: File) => {
    if (!file) return;
    setImporting(true);
    try {
      const result = await api.importAudio(file, revision.value);
      acceptRemoteConfig(result.config, result.revision);
      showToast(
        "success",
        t("Custom sound imported"),
        result.config.notifications.sound_name || file.name
      );
    } catch (error) {
      showToast(
        "error",
        t("Audio import failed"),
        error instanceof Error ? error.message : undefined
      );
    } finally {
      setImporting(false);
      if (fileInput.current) fileInput.current.value = "";
    }
  };

  const testAudio = async () => {
    setTestingAudio(true);
    try {
      await api.playAudio("message", 1);
      showToast(
        "success",
        direct ? t("Audio played in this browser") : t("Audio test sent to PCController")
      );
    } catch (error) {
      showToast(
        "error",
        t("Audio test failed"),
        error instanceof Error ? error.message : undefined
      );
    } finally {
      setTestingAudio(false);
    }
  };

  return (
    <>
      <PanelHeading
        title="Notifications and sound"
        description={
          visualAvailable
            ? "Visual delivery and audible cues are independent."
            : "Audible cues remain available without native visual notifications."
        }
      />
      {visualAvailable && (
        <FieldGroup title="Visual delivery">
          <Toggle
            label="Enable notifications"
            checked={item.enabled}
            onChange={(enabled) => update({ enabled })}
          />
          <SelectField
            label="Visual delivery"
            value={item.delivery}
            options={[
              { value: "auto", label: "Toast, then balloon fallback" },
              { value: "toast", label: "Modern toast only" },
              { value: "balloon", label: "Legacy tray balloon" }
            ]}
            onChange={(delivery) => update({ delivery })}
          />
          <Toggle
            label="Confirmed state changes"
            checked={item.state_changes}
            disabled={!item.enabled}
            onChange={(state_changes) => update({ state_changes })}
          />
          <Toggle
            label="Firmware messages"
            checked={item.custom_message}
            disabled={!item.enabled}
            onChange={(custom_message) => update({ custom_message })}
          />
          <div class="inline-action">
            <div>
              <strong>{t("Verify delivery")}</strong>
              <span>{t("Send a local test without changing any device state.")}</span>
            </div>
            <Button icon={<Icon name="bell" />} onClick={onTest}>
              Test notification
            </Button>
          </div>
        </FieldGroup>
      )}

      {(visualAvailable || audioAvailable) && (
        <FieldGroup title="Event timing">
          <Toggle
            label="Initial synchronization"
            description="Include the first known device state after connecting."
            checked={item.initial_sync}
            onChange={(initial_sync) => update({ initial_sync })}
          />
        </FieldGroup>
      )}

      {audioAvailable && (
        <FieldGroup
          title="Audio effects"
          description={
            direct
              ? "Direct mode plays in the elected browser tab only, so synchronized tabs never echo."
              : "The coordinator owns playback, including when this dashboard is opened remotely."
          }
        >
          <Toggle
            label="Audible cues"
            description="State and message sounds work even when visual notifications are disabled."
            checked={item.sound}
            onChange={(sound) => update({ sound })}
          />
          <SelectField
            label="Sound source"
            value={item.sound_source}
            options={[
              { value: "bundled", label: "Bundled PCController effects" },
              { value: "custom", label: "Custom sound" }
            ]}
            onChange={(sound_source) => {
              if (sound_source === "custom" && !item.sound_file) {
                chooseFile();
                return;
              }
              update({ sound_source });
            }}
          />
          <input
            ref={fileInput}
            class="audio-file-input"
            type="file"
            accept={audioImportAccept}
            aria-label={t("Choose custom sound")}
            onChange={(event) =>
              void importFile(event.currentTarget.files?.[0])
            }
          />
          <div class="audio-file-card">
            <span class="audio-file-icon" aria-hidden="true">
              <Icon name="sound" />
            </span>
            <span class="audio-file-copy">
              <strong>
                {item.sound_file
                  ? item.sound_name || t("Custom sound")
                  : t("No custom sound imported")}
              </strong>
              <small>
                {item.sound_file
                  ? `SHA-256 ${shortAssetID(item.sound_file)}`
                  : t("WAV, MP3, FLAC · 10 sec · 4 MiB")}
              </small>
            </span>
            <Button
              kind="quiet"
              busy={importing}
              disabled={importing}
              icon={<Icon name="upload" />}
              onClick={chooseFile}
            >
              {item.sound_file ? "Replace" : "Import"}
            </Button>
          </div>
          <div class="inline-action">
            <div>
              <strong>{t("Verify audio")}</strong>
              <span>
                {direct
                  ? t("Play the selected cue in this browser.")
                  : t("Ask the coordinator to play the selected cue once.")}
              </span>
            </div>
            <Button
              busy={testingAudio}
              disabled={testingAudio}
              icon={<Icon name="sound" />}
              onClick={() => void testAudio()}
            >
              Test audio
            </Button>
          </div>
        </FieldGroup>
      )}
    </>
  );
}

function shortAssetID(value: string): string {
  if (value.length <= 18) return value;
  return `${value.slice(0, 10)}…${value.slice(-8)}`;
}

type HotkeyBindingKey = keyof AppConfig["hotkeys"];

function useHotkeyCaptureSession(api?: PCControllerDesktopClient) {
  const [active, setActive] = useState<HotkeyBindingKey | null>(null);
  const [errors, setErrors] = useState<
    Partial<Record<HotkeyBindingKey, string>>
  >({});
  const activeRef = useRef<HotkeyBindingKey | null>(null);
  const leaseRef = useRef<HotkeyCaptureLeaseResult | null>(null);
  const acquireRef = useRef<
    Promise<HotkeyCaptureLeaseResult | null> | null
  >(null);
  const heartbeatRef = useRef(0);
  const mountedRef = useRef(true);

  const updateOwner = (key: HotkeyBindingKey | null) => {
    activeRef.current = key;
    if (mountedRef.current) setActive(key);
  };

  const updateError = (key: HotkeyBindingKey, message: string) => {
    if (!mountedRef.current) return;
    setErrors((current) => {
      if (current[key] === message) return current;
      const next = { ...current };
      if (message) {
        next[key] = message;
      } else {
        delete next[key];
      }
      return next;
    });
  };

  const clearHeartbeat = () => {
    window.clearTimeout(heartbeatRef.current);
    heartbeatRef.current = 0;
  };

  const loseCapture = (key: HotkeyBindingKey, message: string) => {
    clearHeartbeat();
    leaseRef.current = null;
    if (activeRef.current === key) updateOwner(null);
    updateError(key, message);
  };

  const armHeartbeat = (lease: HotkeyCaptureLeaseResult) => {
    clearHeartbeat();
    if (!api || activeRef.current === null) return;
    const ttl = Math.max(1_000, lease.lease_ttl_ms);
    const delay = Math.max(500, Math.floor(ttl * 0.45));
    heartbeatRef.current = window.setTimeout(async () => {
      if (
        activeRef.current === null ||
        leaseRef.current?.lease_id !== lease.lease_id
      ) {
        return;
      }
      const owner = activeRef.current;
      try {
        const renewed = await api.renewHotkeyCapture(lease.lease_id);
        if (
          activeRef.current !== owner ||
          leaseRef.current?.lease_id !== lease.lease_id
        ) {
          return;
        }
        leaseRef.current = renewed;
        armHeartbeat(renewed);
      } catch {
        if (
          activeRef.current === owner &&
          leaseRef.current?.lease_id === lease.lease_id
        ) {
          loseCapture(
            owner,
            "Shortcut capture paused because its lease could not be renewed. Select Record to try again."
          );
        }
      }
    }, delay);
  };

  const acceptLease = (lease: HotkeyCaptureLeaseResult) => {
    leaseRef.current = lease;
    armHeartbeat(lease);
  };

  const ensureLease = async (): Promise<HotkeyCaptureLeaseResult | null> => {
    if (!api) return null;
    if (leaseRef.current) return leaseRef.current;
    if (acquireRef.current) return acquireRef.current;

    const request = api.beginHotkeyCapture();
    const pending = (async () => {
      const lease = await request;
      if (activeRef.current === null) {
        try {
          await api.releaseHotkeyCapture(lease.lease_id);
        } catch {
          // The bounded server TTL remains the final cleanup guarantee.
        }
        return null;
      }
      acceptLease(lease);
      return lease;
    })();
    acquireRef.current = pending;
    try {
      return await pending;
    } finally {
      if (acquireRef.current === pending) acquireRef.current = null;
    }
  };

  const begin = (
    key: HotkeyBindingKey
  ):
    | { active: boolean; message?: string }
    | Promise<{ active: boolean; message?: string }> => {
    updateOwner(key);
    updateError(key, "");
    if (!api) return { active: true };

    return (async () => {
      try {
        const lease = await ensureLease();
        return { active: activeRef.current === key && lease !== null };
      } catch (error) {
        const message = hotkeyCaptureErrorMessage(
          error,
          "PCController could not prepare shortcut recording."
        );
        if (activeRef.current === key) {
          updateOwner(null);
          updateError(key, message);
          return { active: false, message };
        }
        return { active: false };
      }
    })();
  };

  const end = async (
    key: HotkeyBindingKey,
    reason: "cancel" | "complete"
  ): Promise<string | void> => {
    if (activeRef.current !== key) return;
    updateOwner(null);
    clearHeartbeat();
    const lease = leaseRef.current;
    leaseRef.current = null;
    if (!api || !lease) return;

    try {
      await api.releaseHotkeyCapture(lease.lease_id);
    } catch (error) {
      if (
        error instanceof APIError &&
        (error.code === "lease_not_found" ||
          error.status === 404 ||
          error.status === 503)
      ) {
        return;
      }
      const message =
        reason === "complete"
          ? "The shortcut was applied, but Windows reported a shortcut restore warning."
          : "Recording ended, but Windows reported a shortcut restore warning.";
      updateError(key, message);
      return message;
    }
  };

  const probe = async (
    key: HotkeyBindingKey,
    chord: string
  ): Promise<{ available: boolean; message?: string }> => {
    if (!api) return { available: true };
    if (activeRef.current !== key) {
      throw new Error("This recorder no longer owns the capture session.");
    }

    let lease: HotkeyCaptureLeaseResult | null;
    try {
      lease = await ensureLease();
      if (!lease || activeRef.current !== key) {
        throw new Error("The shortcut capture session ended.");
      }
      const result = await api.probeHotkey(lease.lease_id, chord);
      if (activeRef.current !== key) {
        throw new Error("The shortcut capture session ended.");
      }
      acceptLease(result);
      return {
        available: result.available,
        message: result.message
      };
    } catch (error) {
      const message = hotkeyCaptureErrorMessage(
        error,
        "PCController could not verify that shortcut. Select Record to try again."
      );
      if (activeRef.current === key) loseCapture(key, message);
      throw new Error(message);
    }
  };

  useEffect(() => {
    mountedRef.current = true;
    const dispose = (keepalive: boolean, updateState: boolean) => {
      clearHeartbeat();
      const lease = leaseRef.current;
      leaseRef.current = null;
      activeRef.current = null;
      if (updateState && mountedRef.current) setActive(null);
      if (api && lease) {
        void api
          .releaseHotkeyCapture(lease.lease_id, keepalive)
          .catch(() => {
            // Page teardown cannot present recovery UI; the TTL is bounded.
          });
      }
    };
    const pageHide = () => dispose(true, true);
    window.addEventListener("pagehide", pageHide);
    return () => {
      window.removeEventListener("pagehide", pageHide);
      mountedRef.current = false;
      dispose(false, false);
    };
  }, [api]);

  return { active, errors, begin, end, probe };
}

export function HotkeySettings({
  api,
  captureAvailable = false,
  probeAvailable = false,
  value,
  patch
}: SettingsProps & {
  api?: PCControllerDesktopClient;
  captureAvailable?: boolean;
  probeAvailable?: boolean;
}) {
  const item = value.hotkeys;
  const captureAPI = api && captureAvailable ? api : undefined;
  const capture = useHotkeyCaptureSession(captureAPI);
  const bindings = [
    { key: "toggle", label: "Toggle device" },
    { key: "on", label: "Turn on" },
    { key: "off", label: "Turn off" },
    { key: "show", label: "Show window" }
  ] as const;

  const update = async (key: keyof typeof item, chord: string) => {
    patch({ hotkeys: { [key]: chord } }, true);
    if (!api) return;
    await flushConfigPatch();
    if (settingsError.value) {
      throw new Error(settingsError.value);
    }
  };

  return (
    <>
      <PanelHeading
        title="Global hotkeys"
        description="Use modifier+key chords. Leave a field blank to disable it."
      />
      <FieldGroup
        title="Bindings"
        description="Record a chord directly. Completed shortcuts are normalized and synchronized immediately."
      >
        <div class="hotkey-browser-note">
          <Icon name="keyboard" size={17} />
          <span>
            {captureAPI
              ? probeAvailable
                ? t("Shortcuts are checked with Windows before they are applied. Windows-key and other system-reserved combinations may still be handled before a browser sees them.")
                : t("This controller can safely pause global shortcuts while you record, but cannot preflight availability. Windows-key and system-reserved combinations are best recorded in native Settings.")
              : t("Browser-only recording cannot preflight Windows-wide availability. Windows-key and system-reserved combinations are best recorded in native Settings.")}
          </span>
        </div>
        <div class="hotkey-grid">
          {bindings.map((binding) => {
            const conflicts: HotkeyConflict[] = bindings
              .filter((candidate) => candidate.key !== binding.key)
              .map((candidate) => ({
                label: candidate.label,
                value: item[candidate.key]
              }));
            return (
              <HotkeyRecorder
                key={binding.key}
                label={binding.label}
                value={item[binding.key]}
                conflicts={conflicts}
                captureActive={capture.active === binding.key}
                captureError={capture.errors[binding.key]}
                onCaptureStart={() => capture.begin(binding.key)}
                onCaptureStop={(reason) =>
                  capture.end(binding.key, reason)
                }
                onProbe={
                  captureAPI && probeAvailable
                    ? (chord) => capture.probe(binding.key, chord)
                    : undefined
                }
                onCommit={(chord) => update(binding.key, chord)}
              />
            );
          })}
        </div>
      </FieldGroup>
    </>
  );
}

function hotkeyCaptureErrorMessage(
  error: unknown,
  fallback: string
): string {
  if (error instanceof APIError) {
    if (error.code === "lease_not_found") {
      return "The shortcut capture session expired. Select Record to try again.";
    }
    return error.message || fallback;
  }
  return error instanceof Error && error.message
    ? error.message
    : fallback;
}

function AutomationSettings({ value, patch }: SettingsProps) {
  const item = value.automation;
  const update = (next: Partial<typeof item>, immediate = false) =>
    patch({ automation: next }, immediate);
  return (
    <>
      <PanelHeading
        title="Event automation"
        description="Run reviewed local scripts after device events and Windows session, power, or network changes."
      />
      <div class="security-note">
        <Icon name="warning" />
        <p>
          {t("Scripts run with your Windows account and are not sandboxed. Use trusted absolute paths and never evaluate firmware message text as code.")}
        </p>
      </div>
      <FieldGroup title="Runner">
        <Toggle
          label="Enable automation"
          checked={item.enabled}
          onChange={(enabled) => update({ enabled }, true)}
        />
        <TextField
          label="On script"
          dir="ltr"
          value={item.on_script}
          placeholder="C:\\Automation\\device-on.ps1"
          onInput={(on_script) => update({ on_script })}
        />
        <TextField
          label="Off script"
          dir="ltr"
          value={item.off_script}
          placeholder="C:\\Automation\\device-off.ps1"
          onInput={(off_script) => update({ off_script })}
        />
        <TextField
          label="Message script"
          dir="ltr"
          value={item.message_script}
          placeholder="C:\\Automation\\device-message.ps1"
          onInput={(message_script) => update({ message_script })}
        />
        <TextField
          label="Windows lock script"
          dir="ltr"
          value={item.lock_script}
          placeholder="C:\\Automation\\session-lock.ps1"
          onInput={(lock_script) => update({ lock_script })}
        />
        <TextField
          label="Windows unlock script"
          dir="ltr"
          value={item.unlock_script}
          placeholder="C:\\Automation\\session-unlock.ps1"
          onInput={(unlock_script) => update({ unlock_script })}
        />
        <TextField
          label="System suspend script"
          dir="ltr"
          value={item.suspend_script}
          placeholder="C:\\Automation\\system-suspend.ps1"
          onInput={(suspend_script) => update({ suspend_script })}
        />
        <TextField
          label="System resume script"
          dir="ltr"
          value={item.resume_script}
          placeholder="C:\\Automation\\system-resume.ps1"
          onInput={(resume_script) => update({ resume_script })}
        />
        <TextField
          label="Network-change script"
          dir="ltr"
          value={item.network_change_script}
          description="Runs after Windows reports an IP-interface change."
          placeholder="C:\\Automation\\network-change.ps1"
          onInput={(network_change_script) => update({ network_change_script })}
        />
        <TextField
          label="Working directory"
          dir="ltr"
          value={item.working_directory}
          onInput={(working_directory) => update({ working_directory })}
        />
        <TextField
          label="Execution timeout"
          dir="ltr"
          value={item.timeout}
          description="From 1s through 10m."
          onInput={(timeout) => update({ timeout })}
        />
        <Toggle
          label="Capture script output"
          description="Bounded stdout/stderr is written to the application log."
          checked={item.capture_output}
          onChange={(capture_output) => update({ capture_output }, true)}
        />
      </FieldGroup>
    </>
  );
}

type RegistrationTarget = "shell" | "com";

export function IntegrationSettings({
  api,
  value,
  patch,
  onDisableAPI
}: SettingsProps & { api: PCControllerDesktopClient; onDisableAPI: () => void }) {
  const item = value.integration;
  const [listenDraft, setListenDraft] = useState(item.local_api_listen);
  const [moveOpen, setMoveOpen] = useState(false);
  const [moving, setMoving] = useState(false);
  const [listenError, setListenError] = useState("");
  const [registration, setRegistration] =
    useState<IntegrationRegistrationStatus | null>(null);
  const [registrationLoading, setRegistrationLoading] = useState(true);
  const [registrationError, setRegistrationError] = useState("");
  const [registrationNotice, setRegistrationNotice] = useState<{
    kind: "warning" | "error";
    message: string;
  } | null>(null);
  const [registrationAction, setRegistrationAction] =
    useState<IntegrationRegistrationAction | null>(null);
  const [unregisterTarget, setUnregisterTarget] =
    useState<RegistrationTarget | null>(null);
  const update = (next: Partial<typeof item>, immediate = false) =>
    patch({ integration: next }, immediate);
  useEffect(() => {
    if (!moveOpen) setListenDraft(item.local_api_listen);
  }, [item.local_api_listen, moveOpen]);
  const refreshRegistration = async (announceFailure = false) => {
    try {
      const next = await api.integrationRegistrationStatus();
      setRegistration(next);
      setRegistrationError("");
    } catch (error) {
      setRegistration(null);
      const message = errorMessage(
        error,
        t("The current executable's registration status could not be read.")
      );
      setRegistrationError(message);
      if (announceFailure) {
        showToast("error", t("Registration status unavailable"), t(message));
      }
    } finally {
      setRegistrationLoading(false);
    }
  };
  useEffect(() => {
    let active = true;
    setRegistrationLoading(true);
    setRegistration(null);
    setRegistrationError("");
    void api.integrationRegistrationStatus().then(
      (next) => {
        if (!active) return;
        setRegistration(next);
        setRegistrationLoading(false);
      },
      (error) => {
        if (!active) return;
        setRegistrationError(
          errorMessage(
            error,
            t("The current executable's registration status could not be read.")
          )
        );
        setRegistrationLoading(false);
      }
    );
    return () => {
      active = false;
    };
  }, [api]);
  const changed = listenDraft.trim() !== item.local_api_listen;
  const validate = () => validateListenAddress(listenDraft);
  const runRegistrationAction = async (
    method: IntegrationRegistrationAction
  ) => {
    setRegistrationAction(method);
    setRegistrationNotice(null);
    try {
      await api.updateIntegrationRegistration(method);
      showToast(
        "success",
        registrationSuccessMessage(method)
      );
    } catch (error) {
      const message = errorMessage(error, t("Windows rejected the registration change."));
      const partial = error instanceof APIError && error.code === "partial";
      setRegistrationNotice({
        kind: partial ? "warning" : "error",
        message
      });
      showToast(
        partial ? "info" : "error",
        partial
          ? t("Shell registration partially completed")
          : t("Registration change failed"),
        t(message)
      );
    } finally {
      // Always ask the current executable for the authoritative result. This
      // matters when URI registration succeeded but Windows rejected Jump
      // List creation and the mutation returned the coded "partial" error.
      await refreshRegistration();
      setRegistrationAction(null);
    }
  };
  const busy = registrationAction !== null;
  const executable = registration?.executable ?? "";
  const targetCopy =
    unregisterTarget === "shell"
      ? {
          name: t("Windows shell integration"),
          method: "integration.shell.unregister" as const,
          description:
            t("This removes the pccontroller-desktop URI handler and Jump List tasks owned by this executable.")
        }
      : {
          name: t("COM automation"),
          method: "integration.com.unregister" as const,
          description:
            t("This removes the current-user COM registration owned by this executable.")
        };
  return (
    <>
      <PanelHeading
        title="Windows and local integrations"
        description="The authenticated API is restricted to the current machine."
      />
      <FieldGroup title="Windows">
        <Toggle
          label="Start with Windows"
          checked={item.start_with_windows}
          onChange={(start_with_windows) => update({ start_with_windows }, true)}
        />
      </FieldGroup>
      <FieldGroup
        title="Current executable registration"
        description="Portable mode stays unregistered by default. These one-time actions affect only the executable shown below; they are not saved as settings or replayed at startup."
      >
        <div class="registration-executable">
          <span>{t("Current executable")}</span>
          {registrationLoading && !executable ? (
            <span class="registration-loading">
              <span class="button-spinner" aria-hidden="true" />
              {t("Reading registration…")}
            </span>
          ) : executable ? (
            <code dir="ltr" title={executable}>{executable}</code>
          ) : (
            <span>{t("Not available")}</span>
          )}
        </div>

        {registrationError && (
          <div class="registration-notice is-error" role="alert">
            <Icon name="warning" />
            <div>
              <strong>{t("Registration status unavailable")}</strong>
              <span>{t(registrationError)}</span>
            </div>
            <Button
              kind="quiet"
              disabled={busy}
              onClick={() => void refreshRegistration(true)}
            >
              Refresh
            </Button>
          </div>
        )}

        {registrationNotice && (
          <div
            class={`registration-notice is-${registrationNotice.kind}`}
            role="alert"
          >
            <Icon name="warning" />
            <div>
              <strong>
                {registrationNotice.kind === "warning"
                  ? t("Partially completed")
                  : t("Registration was not changed")}
              </strong>
              <span>{t(registrationNotice.message)}</span>
            </div>
          </div>
        )}

        <div class="registration-grid">
          <article class="registration-card">
            <div class="registration-card-heading">
              <span class="registration-card-icon">
                <Icon name="window" />
              </span>
              <div>
                <strong>{t("Windows shell")}</strong>
                <span>
                  <BidiLtr>pccontroller-desktop://</BidiLtr>{" "}
                  {t("links and useful Jump List commands")}
                </span>
              </div>
              <RegistrationState
                loading={registrationLoading}
                registered={registration?.shell_registered}
              />
            </div>
            <div class="registration-card-actions">
              {registration?.shell_registered ? (
                <Button
                  kind="danger"
                  disabled={busy}
                  onClick={() => setUnregisterTarget("shell")}
                >
                  Unregister shell
                </Button>
              ) : (
                <Button
                  kind="secondary"
                  busy={registrationAction === "integration.shell.register"}
                  disabled={busy || registrationLoading || !registration}
                  onClick={() =>
                    void runRegistrationAction("integration.shell.register")
                  }
                >
                  Register shell
                </Button>
              )}
            </div>
          </article>

          <article class="registration-card">
            <div class="registration-card-heading">
              <span class="registration-card-icon">
                <Icon name="settings" />
              </span>
              <div>
                <strong>{t("COM automation")}</strong>
                <span>
                  {t("Opt-in control for VBA, AutoHotkey, and other COM clients")}
                </span>
              </div>
              <RegistrationState
                loading={registrationLoading}
                registered={registration?.com_registered}
              />
            </div>
            <div class="registration-card-actions">
              {registration?.com_registered ? (
                <Button
                  kind="danger"
                  disabled={busy}
                  onClick={() => setUnregisterTarget("com")}
                >
                  Unregister COM
                </Button>
              ) : (
                <Button
                  kind="secondary"
                  busy={registrationAction === "integration.com.register"}
                  disabled={busy || registrationLoading || !registration}
                  onClick={() =>
                    void runRegistrationAction("integration.com.register")
                  }
                >
                  Register COM
                </Button>
              )}
            </div>
          </article>
        </div>
        <p class="field-description">
          {t("If this portable executable is moved, register its new path again. Unregistration only removes entries still owned by this exact executable.")}
        </p>
      </FieldGroup>
      <FieldGroup title="Loopback API">
        <Toggle
          label="Enable local API and web dashboard"
          checked={item.local_api_enabled}
          onChange={(local_api_enabled) => {
            if (!local_api_enabled) onDisableAPI();
            else update({ local_api_enabled }, true);
          }}
        />
        <TextField
          label="Listen address"
          dir="ltr"
          value={listenDraft}
          description="Changing this address restarts the listener and may move this dashboard."
          onInput={(next) => {
            setListenDraft(next);
            setListenError("");
          }}
        />
        {listenError && <p class="validation-message" role="alert">{listenError}</p>}
        {changed && (
          <div class="inline-action">
            <div>
              <strong>{t("Dashboard address changed")}</strong>
              <span>{t("Validate, apply, and continue at the new loopback origin.")}</span>
            </div>
            <Button
              onClick={() => {
                const error = validate();
                if (error) setListenError(error);
                else setMoveOpen(true);
              }}
            >
              Apply and move dashboard
            </Button>
          </div>
        )}
        <div class="secret-status">
          <span class="secret-icon"><Icon name="check" /></span>
          <span>
            <strong>{t("API secret protected")}</strong>
            <small>
              {t("The web session never receives or stores the long-lived bearer token.")}
            </small>
          </span>
        </div>
      </FieldGroup>
      <Modal
        open={unregisterTarget !== null}
        title={t("Unregister {name}?", { name: targetCopy.name })}
        description={`${targetCopy.description} ${t("PCController settings and device data are not removed.")}`}
        onClose={() => !busy && setUnregisterTarget(null)}
      >
        {executable && (
          <p class="registration-confirm-path">
            {t("Current executable:")} <code dir="ltr">{executable}</code>
          </p>
        )}
        <div class="modal-actions">
          <Button
            kind="quiet"
            disabled={busy}
            onClick={() => setUnregisterTarget(null)}
          >
            {t("Keep registered")}
          </Button>
          <Button
            kind="danger"
            busy={registrationAction === targetCopy.method}
            disabled={busy}
            onClick={() => {
              const method = targetCopy.method;
              setUnregisterTarget(null);
              void runRegistrationAction(method);
            }}
          >
            {t("Unregister")}
          </Button>
        </div>
      </Modal>
      <Modal
        open={moveOpen}
        title={t("Move the local dashboard?")}
        description={
          <>
            {t("PCController will restart its loopback listener at")}{" "}
            <BidiLtr>{listenDraft.trim()}</BidiLtr>.{" "}
            {t("This tab will continue at the new address.")}
          </>
        }
        onClose={() => !moving && setMoveOpen(false)}
      >
        <div class="modal-actions">
          <Button kind="quiet" disabled={moving} onClick={() => setMoveOpen(false)}>
            {t("Cancel")}
          </Button>
          <Button
            kind="primary"
            busy={moving}
            disabled={moving}
            onClick={async () => {
              const nextListen = listenDraft.trim();
              const error = validateListenAddress(nextListen);
              if (error) {
                setListenError(error);
                setMoveOpen(false);
                return;
              }
              setMoving(true);
              try {
                await flushConfigPatch();
                const result = await api.patchConfig(
                  { integration: { local_api_listen: nextListen } },
                  revision.value
                );
                acceptRemoteConfig(result.config, result.revision);
              } catch (error) {
                if (!isListenerMoveDisconnect(error)) {
                  const message =
                    error instanceof Error
                      ? error.message
                      : t("The listener address was not accepted.");
                  setMoving(false);
                  setListenError(message);
                  showToast("error", t("Listener was not moved"), t(message));
                  return;
                }
                // A successful rebind can close the old listener before its
                // response reaches the browser. Only transport loss/timeouts
                // take this recovery path; rejected patches remain in place.
                showToast("info", t("Listener is moving"));
              }
              const destination = new URL(
                "/app/",
                `http://${nextListen}`
              );
              window.location.assign(destination.href);
            }}
          >
            {t("Apply and continue")}
          </Button>
        </div>
      </Modal>
    </>
  );
}

function RegistrationState({
  loading,
  registered
}: {
  loading: boolean;
  registered?: boolean;
}) {
  return (
    <span
      class={`registration-state${
        loading
          ? " is-loading"
          : registered
            ? " is-registered"
            : " is-unregistered"
      }`}
      role="status"
    >
      <span aria-hidden="true" />
      {loading ? t("Checking") : registered ? t("Registered") : t("Not registered")}
    </span>
  );
}

function registrationSuccessMessage(
  method: IntegrationRegistrationAction
): string {
  switch (method) {
    case "integration.shell.register":
      return t("Windows shell integration registered");
    case "integration.shell.unregister":
      return t("Windows shell integration unregistered");
    case "integration.com.register":
      return t("COM automation registered");
    case "integration.com.unregister":
      return t("COM automation unregistered");
  }
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

function isListenerMoveDisconnect(error: unknown): boolean {
  return (
    error instanceof TypeError ||
    (error instanceof APIError && error.status === 408)
  );
}

function DiagnosticsSettings({ value, patch }: SettingsProps) {
  const item = value.diagnostics;
  void patch;
  return (
    <>
      <PanelHeading
        title="Diagnostic endpoint policy"
        description="Only GET endpoints listed here can be queried through the app."
      />
      <FieldGroup title="Audited endpoints">
        <div class="endpoint-policy-list">
          {auditedEndpoints.map((endpoint) => (
            <code key={endpoint}>{endpoint}</code>
          ))}
        </div>
        <p class="field-description">
          {t("The firmware audit fixes this allowlist. Unrelated project and internal debug routes are intentionally unavailable.")}
        </p>
        {item.safe_endpoints.some(
          (endpoint) => !new Set<string>(auditedEndpoints).has(endpoint)
        ) && (
          <div class="security-note">
            <Icon name="warning" />
            <p>
              {t("This configuration contains legacy endpoint entries. The web dashboard never presents or requests them.")}
            </p>
          </div>
        )}
      </FieldGroup>
    </>
  );
}

const auditedEndpoints = [
  "/info",
  "/update/info",
  "/wifi/signal",
  "/wifi/status",
  "/wifi/country",
  "/description.xml"
] as const;

function validateListenAddress(value: string): string {
  const trimmed = value.trim();
  if (!trimmed) return t("Enter a loopback host and numeric port.");
  let parsed: URL;
  try {
    parsed = new URL(`http://${trimmed}`);
  } catch {
    return t("Use an address such as 127.0.0.1:47831.");
  }
  if (
    parsed.username ||
    parsed.password ||
    parsed.pathname !== "/" ||
    parsed.search ||
    parsed.hash ||
    !parsed.port
  ) {
    return t("Use only a loopback host and explicit numeric port.");
  }
  const port = Number(parsed.port);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    return t("Port must be between 1 and 65535.");
  }
  const host = parsed.hostname.toLowerCase().replace(/^\[|\]$/g, "");
  if (
    host !== "localhost" &&
    host !== "::1" &&
    host !== "0:0:0:0:0:0:0:1" &&
    !host.startsWith("127.")
  ) {
    return t("The listener must remain on localhost or a loopback IP.");
  }
  return "";
}
