import { normalizeLampBaseURL } from "./direct-protocol";

export type ConnectionMode = "auto" | "relay" | "direct";

export interface ConnectionPreferences {
  mode: ConnectionMode;
  lampURL: string;
}

const storageKey = "rayanlamp.web.connection.v1";
const channelName = "rayanlamp-web-connection-v1";
const localEventName = "rayanlamp:connection-preferences";

export function isStaticExport(): boolean {
  return import.meta.env.VITE_RAYANLAMP_STATIC_EXPORT === "true";
}

export function defaultConnectionPreferences(): ConnectionPreferences {
  return {
    mode: isStaticExport() ? "direct" : "auto",
    lampURL: "http://rayanlamp.local"
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
      lampURL:
        typeof value.lampURL === "string" && value.lampURL.trim()
          ? normalizeLampBaseURL(value.lampURL)
          : fallback.lampURL
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
    lampURL: normalizeLampBaseURL(value.lampURL)
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
        lampURL: normalizeLampBaseURL(event.data.lampURL)
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
    typeof candidate.lampURL === "string" &&
    candidate.lampURL.trim().length > 0
  );
}
