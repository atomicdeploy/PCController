import {
  APIError,
  type DeviceCommand,
  type PCControllerDesktopClient
} from "./api";
import {
  browserAudio,
  type BrowserAudioPlayback
} from "./audio";
import type {
  AppConfig,
  AudioCue,
  DeepPartial,
  DiagnosticDocument,
  DiagnosticEndpointResult,
  DiagnosticResult,
  HotkeyCaptureLeaseResult,
  HotkeyCaptureReleaseResult,
  HotkeyProbeResult,
  IntegrationRegistrationAction,
  IntegrationRegistrationStatus,
  Revision,
  ServerEvent,
  Snapshot,
  UIBootstrap,
  UIConfigResult,
  UIDiagnosticsRefreshResult,
  UIDiagnosticsSnapshot
} from "./contracts";
import { buildEdition, developerTools } from "./build-edition";
import {
  DIRECT_DIAGNOSTIC_ENDPOINTS,
  diagnosticDeviceFromDocuments,
  directDiagnosticURL,
  directDeviceCommandURL,
  directProjectionDiagnosticURLs,
  directWebSocketURL,
  isDirectDiagnosticEndpoint,
  normalizeDeviceBaseURL,
  parseDiagnosticDocument,
  parseDirectEvent,
  parseDirectProjectionDiagnostic,
  parseDurationMilliseconds,
  parseOutputStatePayload,
  type DirectDiagnosticDocuments,
  type DirectDiagnosticEndpoint
} from "./direct-protocol";
import {
  readConnectionPreferences,
  saveConnectionPreferences
} from "./connection-preferences";

const directConfigStorageKey = "pccontroller-desktop.web.direct.config.v1";
const maxControlResponseBytes = 64 << 10;
const maxDiagnosticResponseBytes = 1 << 20;

export interface DirectDeviceAPIOptions {
  fetch?: typeof fetch;
  pageURL?: string;
  now?: () => Date;
  audio?: BrowserAudioPlayback;
}

/**
 * Browser-only client for firmware that exposes the audited PCController HTTP
 * and WebSocket surface. It deliberately never sends credentials or custom
 * request headers, keeping cross-origin device reads in the CORS-simple path.
 */
export class DirectDeviceAPI implements PCControllerDesktopClient {
  readonly kind = "direct" as const;
  readonly displayName = "Direct to device";

  private baseURL: string;
  private config: AppConfig;
  private revision: number;
  private current: Snapshot;
  private diagnosticsSnapshot: UIDiagnosticsSnapshot;
  private projectionDocuments: DirectDiagnosticDocuments = {};
  private readonly fetcher: typeof fetch;
  private readonly pageURL?: string;
  private readonly clock: () => Date;
  private readonly audio: BrowserAudioPlayback;
  private socket: WebSocket | null = null;

  constructor(rawBaseURL: string, options: DirectDeviceAPIOptions = {}) {
    this.baseURL = normalizeDeviceBaseURL(rawBaseURL);
    this.fetcher = options.fetch ?? fetch.bind(globalThis);
    this.pageURL = options.pageURL;
    this.clock = options.now ?? (() => new Date());
    this.audio = options.audio ?? browserAudio;
    this.config = loadDirectConfig(this.baseURL);
    this.revision = loadDirectRevision();
    this.current = offlineSnapshot(this.baseURL);
    this.diagnosticsSnapshot = emptyDiagnostics(this.baseURL);
  }

  get syncKey(): string {
    return `direct:${this.baseURL}`;
  }

  async establishSession(_force = false): Promise<string> {
    // Firmware endpoints have no browser session or CSRF exchange.
    return "";
  }

  async bootstrap(): Promise<UIBootstrap> {
    await this.repairMissingCustomAudioSelection();
    try {
      await this.status();
    } catch {
      // Direct mode must still render its controls and actionable connection
      // error when the device is currently sleeping or unreachable.
    }

    try {
      await this.refreshUIDiagnostics();
    } catch {
      // Individual endpoint errors are retained in diagnosticsSnapshot. A
      // diagnostics failure must never prevent the controller from opening.
    }

    return {
      snapshot: clone(this.current),
      config: clone(this.config),
      version: {
        name: "PCControllerDesktopWeb",
        display_name: "PCController Web",
        version: "1.0.0",
        company: "AtomicDeploy",
        description: "Portable browser controller connected directly to the device",
        edition: buildEdition
      },
      revision: this.revision,
      capabilities: {
        device_control: true,
        realtime_events: true,
        websocket_events: this.config.device.websocket,
        transport_direct: true,
        transport_relay: false,
        native_shell: false,
        window_control: false,
        configuration: true,
        diagnostics: true,
        notifications: false,
        audio_sfx: true,
        global_hotkeys: false,
        hotkey_capture: false,
        hotkey_probe: false,
        developer_tools: developerTools,
        automation: false,
        device_terminal: false
      },
      diagnostics: clone(this.diagnosticsSnapshot)
    };
  }

  async getConfig(): Promise<UIConfigResult> {
    this.refreshStoredConfig();
    return {
      config: clone(this.config),
      revision: this.revision
    };
  }

  async patchConfig(
    patch: DeepPartial<AppConfig>,
    revision?: Revision
  ): Promise<UIConfigResult> {
    this.refreshStoredConfig();
    if (
      revision !== undefined &&
      revision !== "" &&
      String(revision) !== String(this.revision)
    ) {
      throw new APIError(
        412,
        "stale_revision",
        "These browser settings changed in another tab. Reload and try again."
      );
    }

    const candidate = mergeRecord(this.config, patch);
    validateDirectConfig(candidate);
    const oldBaseURL = this.baseURL;
    this.baseURL = normalizeDeviceBaseURL(candidate.device.base_url);
    candidate.device.base_url = this.baseURL;
    candidate.diagnostics.safe_endpoints = uniqueAuditedEndpoints(
      candidate.diagnostics.safe_endpoints
    );
    candidate.diagnostics.expert_enabled = false;
    candidate.diagnostics.expert_endpoints = [];

    this.config = clone(candidate);
    this.revision++;
    this.current = {
      ...this.current,
      device_url: this.baseURL
    };
    if (oldBaseURL !== this.baseURL) {
      this.current = offlineSnapshot(this.baseURL);
      this.diagnosticsSnapshot = emptyDiagnostics(this.baseURL);
      this.projectionDocuments = {};
    }
    persistDirectConfig(this.config, this.revision);

    if (patch.device !== undefined) {
      const preferences = readConnectionPreferences();
      saveConnectionPreferences({
        ...preferences,
        deviceURL: this.baseURL
      });
    }
    this.broadcastConfig();

    return {
      config: clone(this.config),
      revision: this.revision
    };
  }

  async beginHotkeyCapture(): Promise<HotkeyCaptureLeaseResult> {
    throw nativeOnly("Global hotkey recording");
  }

  async renewHotkeyCapture(
    _leaseID: string
  ): Promise<HotkeyCaptureLeaseResult> {
    throw nativeOnly("Global hotkey recording");
  }

  async releaseHotkeyCapture(
    _leaseID: string,
    _keepalive = false
  ): Promise<HotkeyCaptureReleaseResult> {
    throw nativeOnly("Global hotkey recording");
  }

  async probeHotkey(
    _leaseID: string,
    _chord: string
  ): Promise<HotkeyProbeResult> {
    throw nativeOnly("Global hotkey probing");
  }

  async integrationRegistrationStatus(): Promise<IntegrationRegistrationStatus> {
    throw nativeOnly("Windows executable registration");
  }

  async updateIntegrationRegistration(
    _method: IntegrationRegistrationAction
  ): Promise<IntegrationRegistrationStatus> {
    throw nativeOnly("Windows executable registration");
  }

  async status(): Promise<Snapshot> {
    try {
      const payload = await this.requestText(
        directDeviceCommandURL(this.baseURL, "get"),
        maxControlResponseBytes
      );
      const state = parseOutputStatePayload(payload);
      if (state === "unknown") {
        throw new APIError(
          502,
          "invalid_device_state",
          "The device returned a response whose state was not recognized."
        );
      }
      this.applyHTTPState(state, "http");
      return clone(this.current);
    } catch (error) {
      this.applyHTTPFailure(error);
      throw error;
    }
  }

  async command(command: DeviceCommand): Promise<unknown> {
    if (command === "window/show" || command === "window/hide") {
      throw nativeOnly("Native window control");
    }
    if (command === "refresh") return this.status();
    if (command === "reconnect") {
      this.socket?.close(4000, "manual reconnect");
      return clone(this.current);
    }

    let resolved: "on" | "off";
    if (command === "toggle") {
      if (
        this.current.state === "unknown" ||
        (!this.current.http_reachable && !this.current.websocket_online)
      ) {
        await this.status();
      }
      if (this.current.state === "unknown") {
        throw new APIError(
          409,
          "state_unknown",
          "The device state is unknown, so a safe toggle cannot be chosen."
        );
      }
      resolved = this.current.state === "on" ? "off" : "on";
    } else {
      resolved = command;
    }

    this.current = { ...this.current, pending_command: resolved };
    try {
      const payload = await this.requestText(
        directDeviceCommandURL(this.baseURL, resolved),
        maxControlResponseBytes
      );
      const reported = parseOutputStatePayload(payload);
      this.applyHTTPState(
        reported === "unknown" ? resolved : reported,
        `http:${resolved}`
      );
      return clone(this.current);
    } catch (error) {
      this.applyHTTPFailure(error);
      throw error;
    }
  }

  async diagnostics(endpoint: string): Promise<DiagnosticResult> {
    if (!isDirectDiagnosticEndpoint(endpoint)) {
      throw new APIError(
        400,
        "endpoint_not_allowed",
        "Only audited public device diagnostic endpoints may be requested."
      );
    }
    const content = await this.requestText(
      directDiagnosticURL(this.baseURL, endpoint),
      maxDiagnosticResponseBytes
    );
    return {
      endpoint,
      content,
      bytes: utf8Bytes(content)
    };
  }

  async uiDiagnostics(): Promise<UIDiagnosticsSnapshot> {
    return clone(this.diagnosticsSnapshot);
  }

  async refreshUIDiagnostics(
    endpoints?: string[]
  ): Promise<UIDiagnosticsRefreshResult> {
    const requested = normalizeDiagnosticSelection(endpoints);
    const attemptedAt = this.clock().toISOString();
    this.diagnosticsSnapshot = {
      ...this.diagnosticsSnapshot,
      refreshing: true,
      last_attempt: attemptedAt
    };

    const refreshProjectionSources = diagnosticSelectionIsFull(requested);
    const [outcomes, projectionOutcomes] = await Promise.all([
      Promise.all(
        requested.map(async (endpoint) => {
          try {
            const content = await this.requestText(
              directDiagnosticURL(this.baseURL, endpoint),
              maxDiagnosticResponseBytes
            );
            const fetchedAt = this.clock();
            const document = parseDiagnosticDocument(
              endpoint,
              content,
              fetchedAt
            );
            return {
              endpoint,
              result: {
                document,
                last_attempt: fetchedAt.toISOString(),
                last_success: fetchedAt.toISOString()
              } satisfies DiagnosticEndpointResult
            };
          } catch (error) {
            const previous = this.diagnosticsSnapshot.endpoints[endpoint];
            const document =
              previous?.document ??
              emptyDiagnosticDocument(endpoint, this.clock());
            return {
              endpoint,
              result: {
                document,
                last_attempt: this.clock().toISOString(),
                ...(previous?.last_success
                  ? { last_success: previous.last_success }
                  : {}),
                error: errorMessage(error),
                stale: Boolean(previous?.last_success)
              } satisfies DiagnosticEndpointResult
            };
          }
        })
      ),
      refreshProjectionSources
        ? Promise.all(
            directProjectionDiagnosticURLs(this.baseURL).map(
              async ({ endpoint, url }) => {
                try {
                  const content = await this.requestText(
                    url,
                    maxDiagnosticResponseBytes
                  );
                  return {
                    endpoint,
                    document: parseDirectProjectionDiagnostic(
                      endpoint,
                      content,
                      this.clock()
                    )
                  };
                } catch {
                  return { endpoint };
                }
              }
            )
          )
        : Promise.resolve([])
    ]);

    const nextEndpoints = { ...this.diagnosticsSnapshot.endpoints };
    let successes = 0;
    for (const outcome of outcomes) {
      nextEndpoints[outcome.endpoint] = outcome.result;
      if (!("error" in outcome.result)) successes++;
    }
    let projectionFailures = 0;
    for (const outcome of projectionOutcomes) {
      if (outcome.document) {
        // Only the parser's redacted document survives this request. The
        // plaintext firmware response is never added to application state.
        this.projectionDocuments[outcome.endpoint] = outcome.document;
      } else {
        projectionFailures++;
      }
    }

    const documents: DirectDiagnosticDocuments = {
      ...this.projectionDocuments
    };
    for (const endpoint of DIRECT_DIAGNOSTIC_ENDPOINTS) {
      const result = nextEndpoints[endpoint];
      if (result?.last_success) documents[endpoint] = result.document;
    }
    const finishedAt = this.clock().toISOString();
    this.diagnosticsSnapshot = {
      device_url: this.baseURL,
      device: diagnosticDeviceFromDocuments(documents),
      endpoints: nextEndpoints,
      refreshing: false,
      last_attempt: attemptedAt,
      ...(successes > 0
        ? { updated_at: finishedAt }
        : this.diagnosticsSnapshot.updated_at
          ? { updated_at: this.diagnosticsSnapshot.updated_at }
          : {})
    };
    this.broadcastDiagnostics();

    const failures = outcomes.length - successes + projectionFailures;
    const sourceCount = outcomes.length + projectionOutcomes.length;
    return {
      diagnostics: clone(this.diagnosticsSnapshot),
      ...(failures > 0
        ? {
            warning: `${failures} of ${sourceCount} diagnostic sources could not be refreshed.`
          }
        : {})
    };
  }

  async notify(_title: string, _message: string): Promise<unknown> {
    throw nativeOnly("Native toast and balloon delivery");
  }

  async playAudio(
    cue: AudioCue = "message",
    count = 1
  ): Promise<unknown> {
    this.refreshStoredConfig();
    await this.repairMissingCustomAudioSelection();
    await this.audio.play(cue, this.config.notifications, count);
    return { cue, count, played: true };
  }

  async importAudio(
    file: File,
    revision?: Revision
  ): Promise<UIConfigResult> {
    const asset = await this.audio.importCustomWAV(file);
    return this.patchConfig(
      {
        notifications: {
          sound: true,
          sound_source: "custom",
          sound_file: asset.id,
          sound_name: asset.name
        }
      },
      revision
    );
  }

  realtimeURL(): string {
    return directWebSocketURL(this.baseURL);
  }

  realtimeOpened(socket: WebSocket): ServerEvent[] {
    this.socket = socket;
    this.current = {
      ...this.current,
      websocket_online: true,
      reconnect_attempt: 0,
      last_connected: this.clock().toISOString(),
      last_error: undefined
    };
    socket.send("get");
    return [{ type: "snapshot", snapshot: clone(this.current) }];
  }

  realtimeClosed(): ServerEvent[] {
    this.socket = null;
    this.current = {
      ...this.current,
      websocket_online: false,
      reconnect_attempt: this.current.reconnect_attempt + 1
    };
    return [{ type: "snapshot", snapshot: clone(this.current) }];
  }

  decodeRealtimeMessage(raw: unknown): ServerEvent[] {
    const event = parseDirectEvent(raw);
    if (!event) return [];
    const now = this.clock().toISOString();
    this.current = {
      ...this.current,
      websocket_online: true,
      reconnect_attempt: 0,
      last_event: event.raw,
      last_source: "websocket"
    };

    if (event.type === "snapshot") {
      const changed = this.current.state !== event.state;
      this.current = {
        ...this.current,
        state: event.state,
        ...(changed ? { last_changed: now } : {})
      };
      return [{ type: "snapshot", snapshot: clone(this.current) }];
    }
    if (event.type === "message") {
      return [
        {
          type: "message",
          title: event.title,
          message: event.message,
          result: { kind: "message", icon: event.icon }
        }
      ];
    }
    return [
      {
        type: "message",
        title: "PCController",
        message:
          event.count === 1
            ? "The device requested an audible cue."
            : `The device requested ${event.count} audible cues.`,
        result: { kind: "beep", count: event.count }
      }
    ];
  }

  private applyHTTPState(
    state: Exclude<Snapshot["state"], "unknown">,
    source: string
  ): void {
    const now = this.clock().toISOString();
    const changed = this.current.state !== state;
    this.current = {
      ...this.current,
      state,
      http_reachable: true,
      device_url: this.baseURL,
      last_event: state.toUpperCase(),
      last_source: source,
      last_error: undefined,
      pending_command: undefined,
      ...(changed ? { last_changed: now } : {})
    };
  }

  private applyHTTPFailure(error: unknown): void {
    this.current = {
      ...this.current,
      http_reachable: false,
      device_url: this.baseURL,
      pending_command: undefined,
      last_error: errorMessage(error)
    };
  }

  private async requestText(url: string, maximumBytes: number): Promise<string> {
    const pageError = directPageAccessError(
      this.pageURL ?? window.location.href,
      this.baseURL
    );
    if (pageError) throw pageError;

    const controller = new AbortController();
    const timeout =
      parseDurationMilliseconds(this.config.device.request_timeout) ?? 5_000;
    const timer = window.setTimeout(() => controller.abort(), timeout);
    try {
      const response = await this.fetcher(url, {
        method: "GET",
        mode: "cors",
        credentials: "omit",
        cache: "no-store",
        redirect: "error",
        signal: controller.signal
      });
      if (!response.ok) {
        throw new APIError(
          response.status,
          `device_http_${response.status}`,
          `The device returned HTTP ${response.status}.`
        );
      }
      const declared = Number(response.headers.get("Content-Length") ?? "0");
      if (Number.isFinite(declared) && declared > maximumBytes) {
        throw new APIError(
          413,
          "response_too_large",
          "The device response exceeded the browser safety limit."
        );
      }
      const content = await response.text();
      if (utf8Bytes(content) > maximumBytes) {
        throw new APIError(
          413,
          "response_too_large",
          "The device response exceeded the browser safety limit."
        );
      }
      return content;
    } catch (error) {
      if (isAbortError(error)) {
        throw new APIError(
          408,
          "device_timeout",
          `The device did not respond within ${this.config.device.request_timeout}.`
        );
      }
      if (error instanceof APIError) throw error;
      throw new APIError(
        0,
        "device_network_error",
        "The browser could not reach the device. Check its address, local-network access, and CORS support."
      );
    } finally {
      window.clearTimeout(timer);
    }
  }

  private broadcastConfig(): void {
    this.broadcast({
      kind: "config",
      config: clone(this.config),
      revision: this.revision
    });
  }

  private async repairMissingCustomAudioSelection(): Promise<void> {
    const notifications = this.config.notifications;
    if (notifications.sound_source !== "custom") return;
    let available = false;
    try {
      available =
        (await this.audio.describe(notifications.sound_file)) !== null;
    } catch {
      // IndexedDB can become unavailable after storage eviction or a browser
      // privacy-policy change. The bundled catalog remains fully local.
    }
    if (available) return;
    this.config = {
      ...this.config,
      notifications: {
        ...notifications,
        sound_source: "bundled",
        sound_file: "",
        sound_name: ""
      }
    };
    this.revision++;
    persistDirectConfig(this.config, this.revision);
    this.broadcastConfig();
  }

  private refreshStoredConfig(): void {
    const storedRevision = loadDirectRevision();
    if (storedRevision <= this.revision) return;
    this.config = loadDirectConfig(this.baseURL);
    this.revision = storedRevision;
  }

  private broadcastDiagnostics(): void {
    this.broadcast({
      kind: "diagnostics",
      diagnostics: clone(this.diagnosticsSnapshot)
    });
  }

  private broadcast(payload: Record<string, unknown>): void {
    if (typeof BroadcastChannel !== "function") return;
    const channel = new BroadcastChannel(syncChannelName(this.syncKey));
    channel.postMessage({
      protocol: 1,
      sender: `direct-client-${randomID()}`,
      connected: this.current.websocket_online,
      ...payload
    });
    channel.close();
  }
}

export function directPageAccessError(
  pageURL: string,
  deviceURL: string
): APIError | null {
  let page: URL;
  let device: URL;
  try {
    page = new URL(pageURL);
    device = new URL(normalizeDeviceBaseURL(deviceURL));
  } catch {
    return new APIError(
      400,
      "invalid_url",
      "The dashboard or device URL is invalid."
    );
  }
  if (page.protocol === "file:") {
    return new APIError(
      0,
      "file_origin_unsupported",
      "Serve the exported ZIP from a local HTTP server; browsers do not reliably allow device access from file:// pages."
    );
  }
  if (page.protocol === "https:" && device.protocol === "http:") {
    return new APIError(
      0,
      "mixed_content",
      "This HTTPS page cannot contact an HTTP device. Serve the dashboard over HTTP on the local network, or enable HTTPS on the device."
    );
  }
  return null;
}

function defaultDirectConfig(baseURL: string): AppConfig {
  return {
    device: {
      base_url: baseURL,
      request_timeout: "5s",
      websocket: true,
      reconnect_min: "1s",
      reconnect_max: "30s",
      http_poll: "30s",
      telnet_port: 23
    },
    ui: {
      view_mode: "compact",
      theme: "system",
      locale: "auto",
      backdrop: "acrylic",
      render_backend: "auto",
      rounded_corners: true,
      layered_foreground: true,
      title_bar_mode: "none",
      border_mode: "auto",
      settings_tab: 0,
      settings_bounds: { x: -1, y: -1, width: 780, height: 640 },
      show_title_bar: false,
      always_on_top: false,
      close_to_tray: true,
      prewarm_tray_menu: true,
      start_minimized: false,
      window_visible: true,
      window_state: "normal",
      show_taskbar: true,
      animations: true,
      magnetic_snap: false,
      magnetic_snap_distance: 16,
      window_bounds: { x: -1, y: -1, width: 176, height: 176 }
    },
    notifications: {
      enabled: false,
      state_changes: false,
      initial_sync: false,
      custom_message: false,
      sound: true,
      sound_source: "bundled",
      sound_file: "",
      sound_name: "",
      delivery: "auto"
    },
    hotkeys: { toggle: "", on: "", off: "", show: "" },
    automation: {
      enabled: false,
      on_script: "",
      off_script: "",
      message_script: "",
      lock_script: "",
      unlock_script: "",
      suspend_script: "",
      resume_script: "",
      network_change_script: "",
      working_directory: "",
      timeout: "15s",
      capture_output: false
    },
    integration: {
      start_with_windows: false,
      local_api_enabled: false,
      local_api_listen: ""
    },
    diagnostics: {
      safe_endpoints: [...DIRECT_DIAGNOSTIC_ENDPOINTS],
      expert_enabled: false,
      expert_endpoints: []
    }
  };
}

function loadDirectConfig(baseURL: string): AppConfig {
  const fallback = defaultDirectConfig(baseURL);
  try {
    const raw = localStorage.getItem(directConfigStorageKey);
    if (!raw) return fallback;
    const decoded = JSON.parse(raw) as DeepPartial<AppConfig>;
    const merged = mergeRecord(fallback, decoded);
    // The connection picker is authoritative; a stale settings document must
    // never silently switch the selected device.
    merged.device.base_url = baseURL;
    validateDirectConfig(merged);
    merged.diagnostics.safe_endpoints = uniqueAuditedEndpoints(
      merged.diagnostics.safe_endpoints
    );
    merged.diagnostics.expert_enabled = false;
    merged.diagnostics.expert_endpoints = [];
    return merged;
  } catch {
    return fallback;
  }
}

function loadDirectRevision(): number {
  try {
    const value = Number(
      localStorage.getItem(`${directConfigStorageKey}.revision`) ?? "1"
    );
    return Number.isSafeInteger(value) && value > 0 ? value : 1;
  } catch {
    return 1;
  }
}

function persistDirectConfig(config: AppConfig, revision: number): void {
  try {
    localStorage.setItem(directConfigStorageKey, JSON.stringify(config));
    localStorage.setItem(
      `${directConfigStorageKey}.revision`,
      String(revision)
    );
  } catch {
    // The active tab remains fully usable when private storage is unavailable.
  }
}

function validateDirectConfig(config: AppConfig): void {
  normalizeDeviceBaseURL(config.device.base_url);
  if (
    config.ui.theme !== "system" &&
    config.ui.theme !== "light" &&
    config.ui.theme !== "dark"
  ) {
    throw new APIError(
      400,
      "invalid_config",
      "Color theme must be system, light, or dark."
    );
  }
  if (
    config.ui.locale !== undefined &&
    config.ui.locale !== "auto" &&
    config.ui.locale !== "en" &&
    config.ui.locale !== "fa"
  ) {
    throw new APIError(
      400,
      "invalid_config",
      "Interface language must be auto, en, or fa."
    );
  }
  if (
    config.ui.window_state !== "normal" &&
    config.ui.window_state !== "minimized" &&
    config.ui.window_state !== "maximized"
  ) {
    throw new APIError(
      400,
      "invalid_config",
      "Window state must be normal, minimized, or maximized."
    );
  }
  if (
    config.notifications.sound_source !== "bundled" &&
    config.notifications.sound_source !== "custom"
  ) {
    throw new APIError(
      400,
      "invalid_config",
      "Sound source must be bundled or custom."
    );
  }
  if (
    config.notifications.sound_file !== "" &&
    !/^[a-f0-9]{64}$/.test(config.notifications.sound_file)
  ) {
    throw new APIError(
      400,
      "invalid_config",
      "Custom sound identity must be a lowercase SHA-256 value."
    );
  }
  if (
    config.notifications.sound_source === "custom" &&
    config.notifications.sound_file === ""
  ) {
    throw new APIError(
      400,
      "invalid_config",
      "Import a valid custom WAV before selecting the custom sound source."
    );
  }
  if (
    Array.from(config.notifications.sound_name).length > 255 ||
    /[\u0000-\u001f\u007f]/.test(config.notifications.sound_name)
  ) {
    throw new APIError(
      400,
      "invalid_config",
      "Custom sound name is invalid."
    );
  }
  if (
    !Number.isInteger(config.device.telnet_port) ||
    config.device.telnet_port < 1 ||
    config.device.telnet_port > 65_535
  ) {
    throw new APIError(
      400,
      "invalid_config",
      "Device terminal port must be an integer between 1 and 65535."
    );
  }
  const timeout = parseDurationMilliseconds(config.device.request_timeout);
  const reconnectMin = parseDurationMilliseconds(config.device.reconnect_min);
  const reconnectMax = parseDurationMilliseconds(config.device.reconnect_max);
  const poll = parseDurationMilliseconds(config.device.http_poll);
  if (timeout === null || timeout < 100 || timeout > 120_000) {
    throw new APIError(
      400,
      "invalid_config",
      "Device request timeout must be between 100ms and 2m."
    );
  }
  if (
    reconnectMin === null ||
    reconnectMax === null ||
    reconnectMin < 100 ||
    reconnectMax < reconnectMin
  ) {
    throw new APIError(
      400,
      "invalid_config",
      "Reconnect timing must start at 100ms or more, and maximum must not be smaller than minimum."
    );
  }
  if (poll === null || (poll !== 0 && poll < 1_000)) {
    throw new APIError(
      400,
      "invalid_config",
      "HTTP polling must be 0 to disable it, or at least 1s."
    );
  }
  uniqueAuditedEndpoints(config.diagnostics.safe_endpoints);
  if (config.diagnostics.expert_endpoints.length > 0) {
    throw new APIError(
      400,
      "endpoint_not_allowed",
      "Direct mode cannot enable unaudited expert endpoints."
    );
  }
}

function uniqueAuditedEndpoints(endpoints: string[]): string[] {
  const result: string[] = [];
  for (const endpoint of endpoints) {
    if (!isDirectDiagnosticEndpoint(endpoint)) {
      throw new APIError(
        400,
        "endpoint_not_allowed",
        `Direct diagnostics endpoint ${JSON.stringify(endpoint)} is not audited.`
      );
    }
    if (!result.includes(endpoint)) result.push(endpoint);
  }
  return result;
}

function normalizeDiagnosticSelection(
  endpoints?: string[]
): DirectDiagnosticEndpoint[] {
  const values =
    endpoints && endpoints.length > 0
      ? endpoints
      : [...DIRECT_DIAGNOSTIC_ENDPOINTS];
  return uniqueAuditedEndpoints(values) as DirectDiagnosticEndpoint[];
}

function diagnosticSelectionIsFull(
  endpoints: readonly DirectDiagnosticEndpoint[]
): boolean {
  return DIRECT_DIAGNOSTIC_ENDPOINTS.every((endpoint) =>
    endpoints.includes(endpoint)
  );
}

function emptyDiagnostics(baseURL: string): UIDiagnosticsSnapshot {
  return {
    device_url: baseURL,
    device: {},
    endpoints: {},
    refreshing: false
  };
}

function emptyDiagnosticDocument(
  endpoint: DirectDiagnosticEndpoint,
  fetchedAt: Date
): DiagnosticDocument {
  return parseDiagnosticDocument(endpoint, "", fetchedAt);
}

function offlineSnapshot(baseURL: string): Snapshot {
  return {
    state: "unknown",
    http_reachable: false,
    websocket_online: false,
    device_url: baseURL,
    reconnect_attempt: 0
  };
}

function nativeOnly(feature: string): APIError {
  return new APIError(
    501,
    "native_feature_unavailable",
    `${feature} is available when the dashboard connects through the Go coordinator.`
  );
}

function mergeRecord<T>(base: T, patch: DeepPartial<T>): T {
  if (
    !base ||
    !patch ||
    typeof base !== "object" ||
    typeof patch !== "object" ||
    Array.isArray(base) ||
    Array.isArray(patch)
  ) {
    return patch as T;
  }
  const output = { ...(base as Record<string, unknown>) };
  for (const [key, value] of Object.entries(
    patch as Record<string, unknown>
  )) {
    if (value === undefined) continue;
    const current = output[key];
    output[key] =
      current &&
      value &&
      typeof current === "object" &&
      typeof value === "object" &&
      !Array.isArray(current) &&
      !Array.isArray(value)
        ? mergeRecord(current, value)
        : value;
  }
  return output as T;
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "The device request failed.";
}

function isAbortError(error: unknown): boolean {
  return (
    error instanceof DOMException
      ? error.name === "AbortError"
      : Boolean(
          error &&
            typeof error === "object" &&
            "name" in error &&
            (error as { name?: unknown }).name === "AbortError"
        )
  );
}

function utf8Bytes(value: string): number {
  return new TextEncoder().encode(value).byteLength;
}

function syncChannelName(key: string): string {
  let hash = 0x811c9dc5;
  for (let index = 0; index < key.length; index++) {
    hash ^= key.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
  }
  return `pccontroller-desktop-web-v1-${(hash >>> 0).toString(16).padStart(8, "0")}`;
}

function randomID(): string {
  return globalThis.crypto?.randomUUID?.() ?? String(Math.random());
}
