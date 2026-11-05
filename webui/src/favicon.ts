import { effect } from "@preact/signals";
import type { LampState } from "./contracts";
import type { ConnectionPhase } from "./contracts";
import {
  appPhase,
  controllerAvailable,
  snapshot
} from "./store";
import faviconConnectingURL from "./assets/favicon-connecting.svg?url";
import faviconOfflineURL from "./assets/favicon-offline.svg?url";
import faviconOffURL from "./assets/favicon-off.svg?url";
import faviconOnURL from "./assets/favicon-on.svg?url";

export type FaviconState = "on" | "off" | "offline" | "connecting";

const faviconURLs: Record<FaviconState, string> = {
  on: faviconOnURL,
  off: faviconOffURL,
  offline: faviconOfflineURL,
  connecting: faviconConnectingURL
};

export function faviconURL(state: FaviconState): string {
  return faviconURLs[state];
}

export function faviconState(
  state: LampState,
  phase: ConnectionPhase,
  available: boolean
): FaviconState {
  if (phase === "booting" || phase === "connecting") return "connecting";
  if (!available || phase === "offline") return "offline";
  if (state === "unknown") return "connecting";
  return state;
}

export function startWebFaviconController(
  target: Document = document
): () => void {
  return effect(() => {
    updateFavicon(
      faviconState(
        snapshot.value.state,
        appPhase.value,
        controllerAvailable.value
      ),
      target
    );
  });
}

export function updateFavicon(
  state: FaviconState,
  target: Document = document
): void {
  const current = target.querySelector<HTMLLinkElement>(
    'link[data-rayanlamp-favicon]'
  );
  if (current?.dataset.lampState === state) return;

  const next = target.createElement("link");
  next.rel = "icon";
  next.type = "image/svg+xml";
  next.setAttribute("sizes", "any");
  next.dataset.rayanlampFavicon = "";
  next.dataset.lampState = state;
  next.href = new URL(faviconURL(state), target.baseURI).href;

  if (current) {
    current.replaceWith(next);
  } else {
    target.head.append(next);
  }
}
