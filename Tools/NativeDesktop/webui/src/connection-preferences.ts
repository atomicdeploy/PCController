import { normalizeDeviceBaseURL } from "./direct-protocol";

export type ConnectionMode = "auto" | "relay" | "direct";

export interface ConnectionPreferences {
  mode: ConnectionMode;
  deviceURL: string;
}

const storageKey = "pccontroller-desktop.web.connection.v1";
const channelName = "pccontroller-desktop-web-connection-v1";
const localEventName = "pccontroller-desktop:connection-preferences";

export function isStaticExport(): boolean {
  return import.meta.env.VITE_PCCONTROLLER_DESKTOP_STATIC_EXPORT === "true";
}

export function defaultConnectionPreferences(): ConnectionPreferences {
  return {
    mode: isStaticExport() ? "direct" : "auto",
    deviceURL: "http://pccontroller-desktop.local"
  };
}

export function readConnectionPreferences(): ConnectionPreferences {
  const fallback = defaultConnectionPreferences();
  try {
    const raw = localStorage.getItem(storageKey);
    if (!raw) return fallback;
    const value = JSON.parse(raw) as Partial<ConnectionPreferences>;
    return {
      mode: isConnectionMode(value.mode) ? value.mode : fallback.mode,
      deviceURL:
        typeof value.deviceURL === "string" && value.deviceURL.trim()
          ? normalizeDeviceBaseURL(value.deviceURL)
          : fallback.deviceURL
    };
  } catch {
    return fallback;
  }
}

export function saveConnectionPreferences(
  value: ConnectionPreferences
): void {
  const normalized = {
    mode: value.mode,
    deviceURL: normalizeDeviceBaseURL(value.deviceURL)
  } satisfies ConnectionPreferences;
  try {
    localStorage.setItem(storageKey, JSON.stringify(normalized));
  } catch {
    // The current tab can still apply the preference without persistence.
  }
  window.dispatchEvent(
    new CustomEvent<ConnectionPreferences>(localEventName, {
      detail: normalized
    })
  );
  if (typeof BroadcastChannel === "function") {
    const channel = new BroadcastChannel(channelName);
    channel.postMessage(normalized);
    channel.close();
  }
}

export function subscribeConnectionPreferences(
  callback: (value: ConnectionPreferences) => void
): () => void {
  const onLocal = (event: Event) => {
    const detail = (event as CustomEvent<ConnectionPreferences>).detail;
    if (validPreferences(detail)) callback(detail);
  };
  window.addEventListener(localEventName, onLocal);

  const channel =
    typeof BroadcastChannel === "function"
      ? new BroadcastChannel(channelName)
      : null;
  const onBroadcast = (event: MessageEvent<unknown>) => {
    if (!validPreferences(event.data)) return;
    let value: ConnectionPreferences;
    try {
      value = {
        mode: event.data.mode,
        deviceURL: normalizeDeviceBaseURL(event.data.deviceURL)
      };
    } catch {
      return;
    }
    try {
      localStorage.setItem(storageKey, JSON.stringify(value));
    } catch {
      // Keep the synchronized in-memory preference.
    }
    callback(value);
  };
  channel?.addEventListener("message", onBroadcast);

  return () => {
    window.removeEventListener(localEventName, onLocal);
    channel?.removeEventListener("message", onBroadcast);
    channel?.close();
  };
}

function isConnectionMode(value: unknown): value is ConnectionMode {
  return value === "auto" || value === "relay" || value === "direct";
}

function validPreferences(value: unknown): value is ConnectionPreferences {
  if (!value || typeof value !== "object") return false;
  const candidate = value as Partial<ConnectionPreferences>;
  return (
    isConnectionMode(candidate.mode) &&
    typeof candidate.deviceURL === "string" &&
    candidate.deviceURL.trim().length > 0
  );
}
