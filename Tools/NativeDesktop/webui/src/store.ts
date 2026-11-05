import { batch, computed, signal } from "@preact/signals";
import type {
  AppConfig,
  Capabilities,
  ConnectionPhase,
  DeepPartial,
  Revision,
  Snapshot,
  UIDiagnosticsSnapshot,
  VersionInfo
} from "./contracts";
import { emptySnapshot, isAppConfig, isSnapshot } from "./contracts";
import type { ClientKind, PCControllerDesktopClient } from "./api";
import {
  applyWebTheme,
  type WebTheme
} from "./theme";
import {
  browserConsole,
  browserErrorMetadata
} from "./browser-console";
import {
  applyWebLocale,
  localePreference,
  resolvedLocale,
  t,
  type LocalePreference
} from "./i18n";

export { resolvedWebTheme, webTheme } from "./theme";
export type { WebTheme } from "./theme";
export { localePreference, resolvedLocale } from "./i18n";
export type { LocalePreference } from "./i18n";

export type RouteName =
  | "dashboard"
  | "settings"
  | "terminal"
  | "diagnostics"
  | "about";
export interface ToastMessage {
  id: number;
  tone: "info" | "success" | "error";
  title: string;
  message?: string;
}

export const appPhase = signal<ConnectionPhase>("booting");
export const snapshot = signal<Snapshot>(emptySnapshot);
export const config = signal<AppConfig | null>(null);
export const version = signal<VersionInfo | null>(null);
export const capabilities = signal<Capabilities>({});
export const diagnostics = signal<UIDiagnosticsSnapshot | null>(null);
export const revision = signal<Revision>(0);
export const route = signal<RouteName>("dashboard");
export const settingsSection = signal<keyof AppConfig>("device");
export const settingsSaving = signal(false);
export const settingsError = signal("");
export const eventConnected = signal(false);
export const controllerAvailable = signal(false);
export const activeClientKind = signal<ClientKind>("auto");
export const activeClientLabel = signal("Selecting connection");
export const commandPending = signal("");
export const toasts = signal<ToastMessage[]>([]);
let toastSerial = 0;

export const isOnline = computed(
  () =>
    controllerAvailable.value &&
    (snapshot.value.http_reachable ||
      snapshot.value.websocket_online)
);

export const stateLabel = computed(() => {
  if (!isOnline.value) return t("Device unavailable");
  switch (snapshot.value.state) {
    case "on":
      return t("Device is on");
    case "off":
      return t("Device is off");
    default:
      return t("Checking device");
  }
});

export function setWebTheme(theme: WebTheme): void {
  applyThemeLocally(theme);
  if (config.value?.ui.theme !== theme) {
    updateConfig({ ui: { theme } }, true);
  }
}

export function setWebLocale(locale: LocalePreference): void {
  applyLocaleLocally(locale);
  if ((config.value?.ui.locale ?? "auto") !== locale) {
    updateConfig({ ui: { locale } }, true);
  }
}

function applyThemeLocally(theme: WebTheme): void {
  applyWebTheme(theme);
}

function applyLocaleLocally(locale: unknown): void {
  applyWebLocale(locale);
}

function applyMotionLocally(enabled: boolean): void {
  document.documentElement.dataset.motion = enabled ? "on" : "off";
}

export function showToast(
  tone: ToastMessage["tone"],
  title: string,
  message?: string,
  duration = 4_500
): void {
  const item: ToastMessage = {
    id: ++toastSerial,
    tone,
    title,
    message
  };
  toasts.value = [...toasts.value.slice(-3), item];
  window.setTimeout(() => dismissToast(item.id), duration);
}

export function dismissToast(id: number): void {
  toasts.value = toasts.value.filter((item) => item.id !== id);
}

export function applyBootstrap(value: unknown): void {
  if (!value || typeof value !== "object") {
    throw new Error("The controller returned an invalid bootstrap document.");
  }
  const candidate = value as Record<string, unknown>;
  const nextSnapshot = candidate.snapshot ?? candidate.status;
  if (!isSnapshot(nextSnapshot)) {
    throw new Error("The controller bootstrap did not contain a valid device snapshot.");
  }
  const nextConfig = candidate.config;
  if (!isAppConfig(nextConfig)) {
    throw new Error("The controller bootstrap did not contain a valid configuration.");
  }
  const nextVersion =
    candidate.version && typeof candidate.version === "object"
      ? (candidate.version as VersionInfo)
      : null;

  batch(() => {
    snapshot.value = nextSnapshot;
    config.value = nextConfig;
    applyThemeLocally(nextConfig.ui.theme);
    applyLocaleLocally(nextConfig.ui.locale ?? "auto");
    applyMotionLocally(nextConfig.ui.animations);
    if (nextVersion) version.value = nextVersion;
    revision.value =
      typeof candidate.revision === "string" ||
      typeof candidate.revision === "number"
        ? candidate.revision
        : revision.value;
    capabilities.value =
      candidate.capabilities && typeof candidate.capabilities === "object"
        ? (candidate.capabilities as Capabilities)
        : {};
    diagnostics.value =
      candidate.diagnostics && typeof candidate.diagnostics === "object"
        ? (candidate.diagnostics as UIDiagnosticsSnapshot)
        : diagnostics.value;
    controllerAvailable.value = true;
    appPhase.value = derivePhase(nextSnapshot);
  });
  browserConsole.event(
    nextSnapshot.state === "unknown" ? "info" : "success",
    "device.state",
    "Initial device state synchronized",
    {
      source: nextSnapshot.last_source || "bootstrap",
      state: nextSnapshot.state
    },
    {
      dedupeKey: `bootstrap:${nextSnapshot.state}:${nextSnapshot.last_source}`,
      dedupeWindowMs: 2_000
    }
  );
}

export function beginControllerConnection(): void {
  batch(() => {
    eventConnected.value = false;
    controllerAvailable.value = false;
    appPhase.value = "booting";
  });
}

export function applySnapshot(next: Snapshot): void {
  const previousState = snapshot.value.state;
  batch(() => {
    snapshot.value = next;
    controllerAvailable.value = true;
    appPhase.value = derivePhase(next);
  });
  if (next.state !== previousState) {
    browserConsole.event(
      next.state === "unknown" ? "warn" : "success",
      "device.state",
      `Device changed from ${previousState} to ${next.state}`,
      {
        source: next.last_source || "state-update",
        state: next.state
      }
    );
  }
}

export function markControllerAvailable(): void {
  batch(() => {
    controllerAvailable.value = true;
    appPhase.value = derivePhase(snapshot.value);
  });
}

export function markControllerUnavailable(
  reason = "The PCControllerDesktop controller is unavailable."
): void {
  batch(() => {
    controllerAvailable.value = false;
    eventConnected.value = false;
    snapshot.value = {
      ...snapshot.value,
      http_reachable: false,
      websocket_online: false,
      reconnect_attempt: Math.max(1, snapshot.value.reconnect_attempt),
      last_error: reason
    };
    appPhase.value = "offline";
  });
}

export function derivePhase(value: Snapshot): ConnectionPhase {
  if (value.websocket_online && value.http_reachable) return "online";
  if (value.websocket_online || value.http_reachable) return "degraded";
  if (value.reconnect_attempt > 0) return "connecting";
  return "offline";
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

let apiClient: PCControllerDesktopClient | null = null;
let pendingPatch: DeepPartial<AppConfig> = {};
let patchTimer = 0;
let patchInFlight: Promise<void> | null = null;
let persistenceGeneration = 0;

export function configureSettingsPersistence(
  api: PCControllerDesktopClient | null
): void {
  if (apiClient === api) return;
  persistenceGeneration++;
  window.clearTimeout(patchTimer);
  pendingPatch = {};
  settingsSaving.value = false;
  settingsError.value = "";
  apiClient = api;
}

export function updateConfig(
  patch: DeepPartial<AppConfig>,
  immediate = false
): void {
  if (!config.value) return;
  config.value = mergeRecord(config.value, patch);
  const nextTheme = patch.ui?.theme;
  if (
    nextTheme === "system" ||
    nextTheme === "light" ||
    nextTheme === "dark"
  ) {
    applyThemeLocally(nextTheme);
  }
  const nextLocale = patch.ui?.locale;
  if (
    nextLocale === "auto" ||
    nextLocale === "en" ||
    nextLocale === "fa"
  ) {
    applyLocaleLocally(nextLocale);
  }
  if (typeof patch.ui?.animations === "boolean") {
    applyMotionLocally(patch.ui.animations);
  }
  pendingPatch = mergeRecord(pendingPatch, patch);
  settingsError.value = "";
  window.clearTimeout(patchTimer);
  patchTimer = window.setTimeout(
    flushConfigPatch,
    immediate ? 0 : 500
  );
}

export async function flushConfigPatch(): Promise<void> {
  window.clearTimeout(patchTimer);
  while (apiClient) {
    if (patchInFlight) {
      await patchInFlight;
      if (settingsError.value) return;
      continue;
    }
    if (Object.keys(pendingPatch).length === 0) return;

    const patch = pendingPatch;
    pendingPatch = {};
    const client = apiClient;
    const generation = persistenceGeneration;
    const sections = Object.keys(patch).sort().join(",");
    const saveStarted = performance.now();
    const request = (async () => {
      settingsSaving.value = true;
      try {
        const result = await client.patchConfig(patch, revision.value);
        if (
          generation !== persistenceGeneration ||
          apiClient !== client
        ) {
          return;
        }
        const stillPending = pendingPatch;
        batch(() => {
          config.value = mergeRecord(result.config, stillPending);
          applyLocaleLocally(config.value.ui.locale ?? localePreference.value);
          applyMotionLocally(config.value.ui.animations);
          revision.value = result.revision;
          settingsError.value = "";
        });
        browserConsole.event(
          "success",
          "settings.saved",
          "Settings synchronized",
          {
            source: client.kind,
            action: sections,
            elapsed_ms: Math.round(performance.now() - saveStarted)
          }
        );
      } catch (error) {
        if (
          generation !== persistenceGeneration ||
          apiClient !== client
        ) {
          return;
        }
        pendingPatch = mergeRecord(patch, pendingPatch);
        const message =
          error instanceof Error ? error.message : t("Unable to save settings.");
        settingsError.value = message;
        browserConsole.event(
          "error",
          "settings.error",
          "Settings could not be synchronized",
          {
            source: client.kind,
            action: sections,
            elapsed_ms: Math.round(performance.now() - saveStarted),
            ...browserErrorMetadata(error)
          }
        );
        showToast("error", t("Settings were not saved"), t(message));
        try {
          const result = await client.getConfig();
          config.value = mergeRecord(result.config, pendingPatch);
          applyLocaleLocally(config.value.ui.locale ?? localePreference.value);
          applyMotionLocally(config.value.ui.animations);
          revision.value = result.revision;
        } catch {
          // Preserve the optimistic draft until the controller reconnects.
        }
      } finally {
        if (
          generation === persistenceGeneration &&
          apiClient === client
        ) {
          settingsSaving.value = false;
        }
      }
    })();
    patchInFlight = request;
    try {
      await request;
    } finally {
      if (patchInFlight === request) patchInFlight = null;
    }
    if (settingsError.value) return;
  }
}

export function acceptRemoteConfig(
  next: AppConfig,
  nextRevision: Revision
): void {
  config.value = mergeRecord(next, pendingPatch);
  revision.value = nextRevision;
  applyThemeLocally(config.value.ui.theme);
  applyLocaleLocally(config.value.ui.locale ?? "auto");
  applyMotionLocally(config.value.ui.animations);
}

export function pendingSettingsPatch(): DeepPartial<AppConfig> {
  return pendingPatch;
}
