export type LampState = "on" | "off" | "unknown";
export type ConnectionPhase =
  | "booting"
  | "connecting"
  | "online"
  | "degraded"
  | "offline";

export interface Snapshot {
  state: LampState;
  http_reachable: boolean;
  websocket_online: boolean;
  device_url: string;
  last_event?: string;
  last_error?: string;
  pending_command?: string;
  last_source?: string;
  last_changed?: string;
  last_connected?: string;
  reconnect_attempt: number;
}

export interface LampConfig {
  base_url: string;
  request_timeout: string;
  websocket: boolean;
  reconnect_min: string;
  reconnect_max: string;
  http_poll: string;
  telnet_port: number;
}

export interface WindowBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export type WindowState = "normal" | "minimized" | "maximized";

export interface UIConfig {
  view_mode: "compact" | "dashboard";
  theme: "system" | "light" | "dark";
  locale?: "auto" | "en" | "fa";
  backdrop:
    | "auto"
    | "solid"
    | "transparent"
    | "blur"
    | "acrylic"
    | "mica"
    | "tabbed";
  render_backend: "auto" | "directx" | "legacy";
  rounded_corners: boolean;
  layered_foreground: boolean;
  title_bar_mode: "none" | "custom" | "native";
  border_mode: "auto" | "none";
  settings_tab: number;
  settings_bounds: WindowBounds;
  show_title_bar: boolean;
  always_on_top: boolean;
  close_to_tray: boolean;
  prewarm_tray_menu: boolean;
  start_minimized: boolean;
  window_visible: boolean;
  window_state: WindowState;
  show_taskbar: boolean;
  animations: boolean;
  magnetic_snap: boolean;
  magnetic_snap_distance: number;
  window_bounds: WindowBounds;
}

export interface NotificationsConfig {
  enabled: boolean;
  state_changes: boolean;
  initial_sync: boolean;
  custom_message: boolean;
  sound: boolean;
  sound_source: "bundled" | "custom";
  sound_file: string;
  sound_name: string;
  delivery: "auto" | "toast" | "balloon";
}

export type AudioCue = "on" | "off" | "message" | "error";

export interface HotkeysConfig {
  toggle: string;
  on: string;
  off: string;
  show: string;
}

export interface AutomationConfig {
  enabled: boolean;
  on_script: string;
  off_script: string;
  message_script: string;
  lock_script: string;
  unlock_script: string;
  suspend_script: string;
  resume_script: string;
  network_change_script: string;
  working_directory: string;
  timeout: string;
  capture_output: boolean;
}

export interface IntegrationConfig {
  start_with_windows: boolean;
  local_api_enabled: boolean;
  local_api_listen: string;
  local_api_token?: string;
  local_api_token_configured?: boolean;
}

export interface DiagnosticsConfig {
  safe_endpoints: string[];
  expert_enabled: boolean;
  expert_endpoints: string[];
}

export interface AppConfig {
  lamp: LampConfig;
  ui: UIConfig;
  notifications: NotificationsConfig;
  hotkeys: HotkeysConfig;
  automation: AutomationConfig;
  integration: IntegrationConfig;
  diagnostics: DiagnosticsConfig;
}

export interface VersionInfo {
  name: string;
  display_name: string;
  version: string;
  company: string;
  description: string;
  edition?: "develop" | "production";
  commit?: string;
  date?: string;
}

export interface Capabilities {
  notifications?: boolean;
  audio_sfx?: boolean;
  diagnostics?: boolean;
  window_control?: boolean;
  websocket_events?: boolean;
  device_terminal?: boolean;
  [name: string]: boolean | undefined;
}

export type DeviceTerminalState =
  | "disconnected"
  | "connecting"
  | "connected";

export interface DeviceTerminalStatus {
  state: DeviceTerminalState;
  host: string;
  port: number;
  session: number;
  sequence: number;
  oldest_sequence?: number;
  read_only?: boolean;
  connected_at?: string;
  last_activity?: string;
  last_error?: string;
}

export type Revision = number | string;

export interface UIBootstrap {
  snapshot: Snapshot;
  config: AppConfig;
  version: VersionInfo;
  revision: Revision;
  capabilities?: Capabilities;
  diagnostics?: UIDiagnosticsSnapshot;
}

export interface UIConfigResult {
  config: AppConfig;
  revision: Revision;
}

export type IntegrationRegistrationAction =
  | "integration.shell.register"
  | "integration.shell.unregister"
  | "integration.com.register"
  | "integration.com.unregister";

export interface IntegrationRegistrationStatus {
  executable: string;
  shell_registered: boolean;
  com_registered: boolean;
}

export interface SessionResult {
  csrf_token: string;
  expires_at: string;
}

export interface HotkeyCaptureLeaseResult {
  lease_id: string;
  expires_at: string;
  lease_ttl_ms: number;
}

export interface HotkeyCaptureReleaseResult {
  released: boolean;
}

export interface HotkeyProbeResult extends HotkeyCaptureLeaseResult {
  chord: string;
  available: boolean;
  message?: string;
}

export interface DiagnosticResult {
  endpoint: string;
  content: string;
  bytes: number;
}

export type DiagnosticValueKind =
  | "string"
  | "integer"
  | "number"
  | "boolean"
  | "bytes"
  | "percent"
  | "signal"
  | "duration"
  | "time"
  | "ip"
  | "mac"
  | "hash"
  | "state"
  | "redacted";

export interface DiagnosticField {
  key: string;
  label: string;
  value: string;
  kind: DiagnosticValueKind;
  unit?: string;
  integer?: number;
  number?: number;
  boolean?: boolean;
  time?: string;
  duration_seconds?: number;
  redacted?: boolean;
}

export interface DiagnosticDocument {
  endpoint: string;
  format: string;
  fetched_at: string;
  bytes: number;
  raw: string;
  fields: DiagnosticField[];
  warnings?: string[];
}

export interface DiagnosticEndpointResult {
  document: DiagnosticDocument;
  last_attempt: string;
  last_success?: string;
  error?: string;
  stale?: boolean;
}

export interface DiagnosticUpdateInfo {
  hash?: string;
  size_bytes?: number;
  free_bytes?: number;
  build?: string;
  date?: string;
}

export interface DiagnosticDescription {
  friendly_name?: string;
  model_name?: string;
  model_number?: string;
  manufacturer?: string;
  manufacturer_url?: string;
  serial_number?: string;
  udn?: string;
}

export interface DiagnosticDeviceInfo {
  device_name?: string;
  hostname?: string;
  firmware?: string;
  wifi_version?: string;
  esp_chip_id?: string;
  flash_chip_id?: string;
  firmware_hash?: string;
  firmware_corrupted?: boolean;
  protocol_revision?: string;
  protocol_match?: boolean;
  station_mac?: string;
  access_point_mac?: string;
  client_ip?: string;
  flash_size_bytes?: number;
  flash_real_size_bytes?: number;
  program_usage_percent?: number;
  settings_hash?: string;
  settings_status?: string;
  heap_free_bytes?: number;
  build?: string;
  build_time?: string;
  uptime_seconds?: number;
  reported_state?: string;
  rssi_dbm?: number;
  signal_quality?: string;
  wifi_connected?: boolean;
  ssid?: string;
  wifi_ip?: string;
  access_point_status?: string;
  station_status?: string;
  station_dhcp?: boolean;
  station_netmask?: string;
  station_gateway?: string;
  station_dns?: string;
  access_point_ssid?: string;
  access_point_ip?: string;
  access_point_netmask?: string;
  country?: string;
  update?: DiagnosticUpdateInfo;
  description?: DiagnosticDescription;
}

export interface UIDiagnosticsSnapshot {
  device_url: string;
  device: DiagnosticDeviceInfo;
  endpoints: Record<string, DiagnosticEndpointResult>;
  refreshing: boolean;
  last_attempt?: string;
  updated_at?: string;
}

export interface UIDiagnosticsRefreshResult {
  diagnostics: UIDiagnosticsSnapshot;
  warning?: string;
}

export interface APIErrorBody {
  code: string;
  message: string;
}

export interface APIEnvelope<T> {
  ok: boolean;
  result?: T;
  error?: APIErrorBody;
}

export interface ServerEvent {
  type?: "snapshot" | "config" | "diagnostics" | "message" | "hello";
  revision?: Revision;
  sequence?: number;
  snapshot?: Snapshot;
  config?: AppConfig;
  diagnostics?: UIDiagnosticsSnapshot;
  title?: string;
  message?: string;
  result?: unknown;
}

export type DeepPartial<T> = {
  [P in keyof T]?: T[P] extends Array<infer U>
    ? U[]
    : T[P] extends object
      ? DeepPartial<T[P]>
      : T[P];
};

export const emptySnapshot: Snapshot = {
  state: "unknown",
  http_reachable: false,
  websocket_online: false,
  device_url: "",
  reconnect_attempt: 0
};

export function isSnapshot(value: unknown): value is Snapshot {
  if (!value || typeof value !== "object") return false;
  const candidate = value as Partial<Snapshot>;
  return (
    (candidate.state === "on" ||
      candidate.state === "off" ||
      candidate.state === "unknown") &&
    typeof candidate.http_reachable === "boolean" &&
    typeof candidate.websocket_online === "boolean" &&
    typeof candidate.device_url === "string"
  );
}

export function isAppConfig(value: unknown): value is AppConfig {
  if (!value || typeof value !== "object") return false;
  const candidate = value as Partial<AppConfig>;
  return Boolean(
    candidate.lamp &&
      candidate.ui &&
      candidate.notifications &&
      candidate.hotkeys &&
      candidate.automation &&
      candidate.integration &&
      candidate.diagnostics
  );
}
