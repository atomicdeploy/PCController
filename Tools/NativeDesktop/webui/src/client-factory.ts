import {
  APIError,
  PCControllerDesktopAPI,
  type DeviceCommand,
  type PCControllerDesktopClient
} from "./api";
import type {
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
  ServerEvent,
  Snapshot,
  UIBootstrap,
  UIConfigResult,
  UIDiagnosticsRefreshResult,
  UIDiagnosticsSnapshot
} from "./contracts";
import type { ConnectionPreferences } from "./connection-preferences";
import { DirectDeviceAPI } from "./direct-api";
import {
  browserConsole,
  browserErrorMetadata
} from "./browser-console";

/**
 * Selects the same-origin Go coordinator when it is available, otherwise the
 * browser talks to the configured device directly. Fallback is intentionally
 * confined to "auto"; explicit relay/direct choices never change transport.
 */
export class AutoPCControllerDesktopClient implements PCControllerDesktopClient {
  private active: PCControllerDesktopClient | null = null;

  constructor(
    private readonly relay: PCControllerDesktopClient,
    private readonly direct: PCControllerDesktopClient
  ) {}

  get kind(): PCControllerDesktopClient["kind"] {
    return this.active?.kind ?? "auto";
  }

  get displayName(): string {
    return this.active?.displayName ?? "Automatic connection";
  }

  get syncKey(): string {
    return this.active?.syncKey ?? `auto:${this.direct.syncKey}`;
  }

  async bootstrap(): Promise<UIBootstrap> {
    if (this.active) return this.active.bootstrap();
    let relayError: unknown;
    try {
      const value = await this.relay.bootstrap();
      this.active = this.relay;
      return value;
    } catch (error) {
      relayError = error;
      browserConsole.event(
        "warn",
        "transport.error",
        "Coordinator unavailable; trying the direct device transport",
        {
          source: "relay",
          action: "fallback-to-direct",
          ...browserErrorMetadata(error)
        }
      );
    }

    try {
      const value = await this.direct.bootstrap();
      this.active = this.direct;
      return value;
    } catch (directError) {
      throw new APIError(
        0,
        "automatic_connection_failed",
        `Neither connection mode is available. Go coordinator: ${messageOf(
          relayError
        )} Direct device: ${messageOf(directError)}`
      );
    }
  }

  establishSession(force = false): Promise<string> {
    return this.active?.establishSession(force) ?? Promise.resolve("");
  }

  getConfig(): Promise<UIConfigResult> {
    return this.requireActive().getConfig();
  }

  patchConfig(
    patch: DeepPartial<AppConfig>,
    revision?: Revision
  ): Promise<UIConfigResult> {
    return this.requireActive().patchConfig(patch, revision);
  }

  beginHotkeyCapture(): Promise<HotkeyCaptureLeaseResult> {
    return this.requireActive().beginHotkeyCapture();
  }

  renewHotkeyCapture(
    leaseID: string
  ): Promise<HotkeyCaptureLeaseResult> {
    return this.requireActive().renewHotkeyCapture(leaseID);
  }

  releaseHotkeyCapture(
    leaseID: string,
    keepalive = false
  ): Promise<HotkeyCaptureReleaseResult> {
    return this.requireActive().releaseHotkeyCapture(leaseID, keepalive);
  }

  probeHotkey(
    leaseID: string,
    chord: string
  ): Promise<HotkeyProbeResult> {
    return this.requireActive().probeHotkey(leaseID, chord);
  }

  integrationRegistrationStatus(): Promise<IntegrationRegistrationStatus> {
    return this.requireActive().integrationRegistrationStatus();
  }

  updateIntegrationRegistration(
    method: IntegrationRegistrationAction
  ): Promise<IntegrationRegistrationStatus> {
    return this.requireActive().updateIntegrationRegistration(method);
  }

  status(): Promise<Snapshot> {
    return this.requireActive().status();
  }

  command(command: DeviceCommand): Promise<unknown> {
    return this.requireActive().command(command);
  }

  diagnostics(endpoint: string): Promise<DiagnosticResult> {
    return this.requireActive().diagnostics(endpoint);
  }

  uiDiagnostics(): Promise<UIDiagnosticsSnapshot> {
    return this.requireActive().uiDiagnostics();
  }

  refreshUIDiagnostics(
    endpoints?: string[]
  ): Promise<UIDiagnosticsRefreshResult> {
    return this.requireActive().refreshUIDiagnostics(endpoints);
  }

  notify(title: string, message: string): Promise<unknown> {
    return this.requireActive().notify(title, message);
  }

  playAudio(cue: AudioCue = "message", count = 1): Promise<unknown> {
    return this.requireActive().playAudio(cue, count);
  }

  importAudio(
    file: File,
    revision?: Revision
  ): Promise<UIConfigResult> {
    return this.requireActive().importAudio(file, revision);
  }

  realtimeURL(): string {
    return this.requireActive().realtimeURL();
  }

  realtimeOpened(socket: WebSocket): ServerEvent[] {
    return this.requireActive().realtimeOpened(socket);
  }

  realtimeClosed(): ServerEvent[] {
    return this.requireActive().realtimeClosed();
  }

  decodeRealtimeMessage(raw: unknown): ServerEvent[] {
    return this.requireActive().decodeRealtimeMessage(raw);
  }

  private requireActive(): PCControllerDesktopClient {
    if (!this.active) {
      throw new APIError(
        503,
        "connection_not_selected",
        "The automatic connection is still being selected."
      );
    }
    return this.active;
  }
}

export function createPCControllerDesktopClient(
  preferences: ConnectionPreferences
): PCControllerDesktopClient {
  if (preferences.mode === "relay") return new PCControllerDesktopAPI();
  if (preferences.mode === "direct") {
    return new DirectDeviceAPI(preferences.deviceURL);
  }
  return new AutoPCControllerDesktopClient(
    new PCControllerDesktopAPI(),
    new DirectDeviceAPI(preferences.deviceURL)
  );
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : "unavailable.";
}
