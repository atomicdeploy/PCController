import type {
  APIEnvelope,
  AppConfig,
  AudioCue,
  DeepPartial,
  DiagnosticResult,
  HotkeyCaptureLeaseResult,
  HotkeyCaptureReleaseResult,
  HotkeyProbeResult,
  IntegrationRegistrationAction,
  IntegrationRegistrationStatus,
  Revision,
  SessionResult,
  Snapshot,
  ServerEvent,
  UIBootstrap,
  UIConfigResult,
  UIDiagnosticsRefreshResult,
  UIDiagnosticsSnapshot
} from "./contracts";

const defaultTimeout = 12_000;
const diagnosticsRefreshTimeout = 55_000;

export type ClientKind = "auto" | "relay" | "direct";
export type DeviceCommand =
  | "on"
  | "off"
  | "toggle"
  | "refresh"
  | "reconnect"
  | "window/show"
  | "window/hide";

// PCControllerDesktopClient is the UI-facing controller contract. The relay implements
// it with the Go manager's authenticated loopback API; the portable build also
// supplies a direct firmware implementation.
export interface PCControllerDesktopClient {
  readonly kind: ClientKind;
  readonly displayName: string;
  readonly syncKey: string;

  establishSession(force?: boolean): Promise<string>;
  bootstrap(): Promise<UIBootstrap>;
  getConfig(): Promise<UIConfigResult>;
  patchConfig(
    patch: DeepPartial<AppConfig>,
    revision?: string | number
  ): Promise<UIConfigResult>;
  beginHotkeyCapture(): Promise<HotkeyCaptureLeaseResult>;
  renewHotkeyCapture(leaseID: string): Promise<HotkeyCaptureLeaseResult>;
  releaseHotkeyCapture(
    leaseID: string,
    keepalive?: boolean
  ): Promise<HotkeyCaptureReleaseResult>;
  probeHotkey(leaseID: string, chord: string): Promise<HotkeyProbeResult>;
  integrationRegistrationStatus(): Promise<IntegrationRegistrationStatus>;
  updateIntegrationRegistration(
    method: IntegrationRegistrationAction
  ): Promise<IntegrationRegistrationStatus>;
  status(): Promise<Snapshot>;
  command(command: DeviceCommand): Promise<unknown>;
  diagnostics(endpoint: string): Promise<DiagnosticResult>;
  uiDiagnostics(): Promise<UIDiagnosticsSnapshot>;
  refreshUIDiagnostics(endpoints?: string[]): Promise<UIDiagnosticsRefreshResult>;
  notify(title: string, message: string): Promise<unknown>;
  playAudio(cue?: AudioCue, count?: number): Promise<unknown>;
  importAudio(
    file: File,
    revision?: Revision
  ): Promise<UIConfigResult>;

  realtimeURL(): string;
  realtimeOpened(socket: WebSocket): ServerEvent[];
  realtimeClosed(): ServerEvent[];
  decodeRealtimeMessage(raw: unknown): ServerEvent[];
}

export class APIError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "APIError";
    this.status = status;
    this.code = code;
  }
}

function isEnvelope<T>(value: unknown): value is APIEnvelope<T> {
  return Boolean(
    value &&
      typeof value === "object" &&
      typeof (value as APIEnvelope<T>).ok === "boolean"
  );
}

export function unwrapResponse<T>(value: unknown): T {
  if (!isEnvelope<T>(value)) return value as T;
  if (value.ok && value.result !== undefined) return value.result;
  const error = value.error;
  throw new APIError(
    500,
    error?.code ?? "invalid_response",
    error?.message ?? "The local API returned an incomplete response."
  );
}

export class PCControllerDesktopAPI implements PCControllerDesktopClient {
  readonly kind = "relay" as const;
  readonly displayName = "Go coordinator";
  private csrf = "";
  private sessionRequest: Promise<string> | null = null;

  get syncKey(): string {
    return `relay:${window.location.origin}`;
  }

  get csrfToken(): string {
    return this.csrf;
  }

  async establishSession(force = false): Promise<string> {
    if (force) this.csrf = "";
    if (this.csrf) return this.csrf;
    if (this.sessionRequest) return this.sessionRequest;
    this.sessionRequest = this.requestSession().finally(() => {
      this.sessionRequest = null;
    });
    return this.sessionRequest;
  }

  private async requestSession(): Promise<string> {
    const response = await fetch("/app/session", {
      method: "POST",
      credentials: "same-origin",
      headers: { Accept: "application/json" },
      cache: "no-store"
    });
    const value = await this.readJSON<SessionResult>(response);
    const result = unwrapResponse<SessionResult>(value);
    if (
      !result ||
      typeof result.csrf_token !== "string" ||
      !result.csrf_token
    ) {
      throw new APIError(
        response.status,
        "invalid_session",
        "The local controller did not provide a CSRF token."
      );
    }
    this.csrf = result.csrf_token;
    return this.csrf;
  }

  async bootstrap(): Promise<UIBootstrap> {
    await this.establishSession();
    return this.request<UIBootstrap>("/ui/bootstrap");
  }

  async getConfig(): Promise<UIConfigResult> {
    return this.request<UIConfigResult>("/ui/config");
  }

  async patchConfig(
    patch: DeepPartial<AppConfig>,
    revision?: string | number
  ): Promise<UIConfigResult> {
    const headers: Record<string, string> = {};
    if (revision !== undefined && revision !== "") {
      headers["If-Match"] = String(revision);
    }
    return this.request<UIConfigResult>("/ui/config", {
      method: "PATCH",
      headers,
      body: JSON.stringify(patch)
    });
  }

  async beginHotkeyCapture(): Promise<HotkeyCaptureLeaseResult> {
    return this.request<HotkeyCaptureLeaseResult>(
      "/ui/hotkeys/capture",
      {
        method: "POST",
        body: JSON.stringify({})
      }
    );
  }

  async renewHotkeyCapture(
    leaseID: string
  ): Promise<HotkeyCaptureLeaseResult> {
    return this.request<HotkeyCaptureLeaseResult>(
      "/ui/hotkeys/capture",
      {
        method: "POST",
        body: JSON.stringify({ lease_id: leaseID })
      }
    );
  }

  async releaseHotkeyCapture(
    leaseID: string,
    keepalive = false
  ): Promise<HotkeyCaptureReleaseResult> {
    return this.request<HotkeyCaptureReleaseResult>(
      "/ui/hotkeys/capture",
      {
        method: "DELETE",
        body: JSON.stringify({ lease_id: leaseID }),
        keepalive
      }
    );
  }

  async probeHotkey(
    leaseID: string,
    chord: string
  ): Promise<HotkeyProbeResult> {
    return this.request<HotkeyProbeResult>("/ui/hotkeys/probe", {
      method: "POST",
      body: JSON.stringify({ lease_id: leaseID, chord })
    });
  }

  async integrationRegistrationStatus(): Promise<IntegrationRegistrationStatus> {
    return this.request<IntegrationRegistrationStatus>(
      "/ui/integration/registration"
    );
  }

  async updateIntegrationRegistration(
    method: IntegrationRegistrationAction
  ): Promise<IntegrationRegistrationStatus> {
    return this.request<IntegrationRegistrationStatus>(
      "/ui/integration/registration",
      {
        method: "POST",
        body: JSON.stringify({ method })
      }
    );
  }

  async status(): Promise<Snapshot> {
    return this.request<Snapshot>("/status");
  }

  async command(command: DeviceCommand): Promise<unknown> {
    const path =
      command === "reconnect"
        ? "/reconnect"
        : command.startsWith("window/")
          ? `/${command}`
          : `/device/${command}`;
    return this.request(path, { method: "POST" });
  }

  async diagnostics(endpoint: string): Promise<DiagnosticResult> {
    const query = new URLSearchParams({ endpoint });
    return this.request<DiagnosticResult>(`/diagnostics?${query}`);
  }

  async uiDiagnostics(): Promise<UIDiagnosticsSnapshot> {
    return this.request<UIDiagnosticsSnapshot>("/ui/diagnostics");
  }

  async refreshUIDiagnostics(
    endpoints?: string[]
  ): Promise<UIDiagnosticsRefreshResult> {
    return this.request<UIDiagnosticsRefreshResult>("/ui/diagnostics/refresh", {
      method: "POST",
      body: JSON.stringify(
        endpoints && endpoints.length > 0 ? { endpoints } : {}
      )
    }, true, diagnosticsRefreshTimeout);
  }

  async notify(title: string, message: string): Promise<unknown> {
    return this.request("/notifications", {
      method: "POST",
      body: JSON.stringify({ title, message })
    });
  }

  async playAudio(
    cue: AudioCue = "message",
    count = 1
  ): Promise<unknown> {
    validateAudioRequest(cue, count);
    return this.request("/audio/play", {
      method: "POST",
      body: JSON.stringify({ cue, count })
    });
  }

  async importAudio(
    file: File,
    revision?: Revision
  ): Promise<UIConfigResult> {
    const headers: Record<string, string> = {
      "Content-Type": "audio/wav",
      "X-PCControllerDesktop-Filename": safeAudioFilenameHeader(file.name)
    };
    if (revision !== undefined && revision !== "") {
      headers["If-Match"] = String(revision);
    }
    return this.request<UIConfigResult>("/ui/audio/import", {
      method: "POST",
      headers,
      body: file
    });
  }

  realtimeURL(): string {
    const url = new URL("/events", window.location.href);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    return url.href;
  }

  realtimeOpened(_socket: WebSocket): ServerEvent[] {
    return [];
  }

  realtimeClosed(): ServerEvent[] {
    return [];
  }

  decodeRealtimeMessage(raw: unknown): ServerEvent[] {
    if (typeof raw !== "string") return [];
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      return [];
    }
    const envelope =
      parsed && typeof parsed === "object" && "result" in parsed
        ? (parsed as ServerEvent).result
        : parsed;
    return envelope && typeof envelope === "object"
      ? [envelope as ServerEvent]
      : [];
  }

  private async request<T>(
    path: string,
    init: RequestInit = {},
    retrySession = true,
    timeoutMs = defaultTimeout
  ): Promise<T> {
    const method = (init.method ?? "GET").toUpperCase();
    const mutating = method !== "GET" && method !== "HEAD";
    if (mutating) await this.establishSession();

    const controller = new AbortController();
    const timer = window.setTimeout(() => controller.abort(), timeoutMs);
    const headers = new Headers(init.headers);
    headers.set("Accept", "application/json");
    if (init.body !== undefined && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
    if (mutating) {
      headers.set("X-PCControllerDesktop-CSRF", this.csrf);
    }

    try {
      const response = await fetch(path, {
        ...init,
        method,
        headers,
        credentials: "same-origin",
        cache: "no-store",
        signal: controller.signal
      });
      if (
        response.status === 401 &&
        retrySession &&
        path !== "/app/session"
      ) {
        this.csrf = "";
        await this.establishSession(true);
        return this.request<T>(path, init, false, timeoutMs);
      }
      const value = await this.readJSON<T>(response);
      if (!response.ok) {
        const envelope = isEnvelope<T>(value)
          ? value
          : undefined;
        throw new APIError(
          response.status,
          envelope?.error?.code ?? `http_${response.status}`,
          envelope?.error?.message ??
            `The local controller returned HTTP ${response.status}.`
        );
      }
      return unwrapResponse<T>(value);
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") {
        throw new APIError(408, "timeout", "The local controller did not respond in time.");
      }
      throw error;
    } finally {
      window.clearTimeout(timer);
    }
  }

  private async readJSON<T>(response: Response): Promise<T | APIEnvelope<T>> {
    const type = response.headers.get("Content-Type") ?? "";
    if (!type.toLowerCase().includes("application/json")) {
      throw new APIError(
        response.status,
        "invalid_response",
        "The local controller returned a non-JSON response."
      );
    }
    try {
      return (await response.json()) as T | APIEnvelope<T>;
    } catch {
      throw new APIError(
        response.status,
        "invalid_response",
        "The local controller returned malformed JSON."
      );
    }
  }
}

function validateAudioRequest(cue: AudioCue, count: number): void {
  if (!["on", "off", "message", "error"].includes(cue)) {
    throw new APIError(400, "invalid_audio_cue", "Unknown audio cue.");
  }
  if (!Number.isInteger(count) || count < 1 || count > 10) {
    throw new APIError(
      400,
      "invalid_audio_count",
      "Audio cue count must be an integer between 1 and 10."
    );
  }
}

function safeAudioFilenameHeader(value: string): string {
  const leaf = value.split(/[\\/]/).at(-1)?.trim() ?? "";
  const ascii = leaf
    .replace(/[^\x20-\x7e]/g, "_")
    .replace(/[";]/g, "_")
    .slice(0, 128);
  return ascii || "Custom sound.wav";
}
