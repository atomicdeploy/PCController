import { signal } from "@preact/signals";

export type WebTheme = "system" | "light" | "dark";
export type ResolvedWebTheme = "light" | "dark";

export const webThemeStorageKey = "pccontroller-desktop.web.theme";

const lightThemeColor = "#eef4f8";
const darkThemeColor = "#0b1724";
const systemThemeQuery = "(prefers-color-scheme: light)";

export const webTheme = signal<WebTheme>(readStoredTheme());
export const resolvedWebTheme = signal<ResolvedWebTheme>(
  resolveWebTheme(webTheme.value, systemPrefersLight())
);

let activeMediaQuery: MediaQueryList | null = null;
let detachMediaListener: (() => void) | null = null;

export function readStoredTheme(): WebTheme {
  try {
    const value = localStorage.getItem(webThemeStorageKey);
    if (isWebTheme(value)) return value;
  } catch {
    // Storage can be disabled without making the controller unusable.
  }
  return "system";
}

export function applyWebTheme(
  theme: WebTheme,
  persist = true
): ResolvedWebTheme {
  webTheme.value = theme;
  if (persist) {
    try {
      localStorage.setItem(webThemeStorageKey, theme);
    } catch {
      // The current page still keeps the selected theme.
    }
  }
  return reconcileTheme();
}

export function startWebThemeController(): () => void {
  detachMediaListener?.();
  detachMediaListener = null;
  activeMediaQuery =
    typeof window.matchMedia === "function"
      ? window.matchMedia(systemThemeQuery)
      : null;

  const media = activeMediaQuery;
  let detachThisController: (() => void) | null = null;
  if (media) {
    const onChange = () => {
      if (webTheme.value === "system") reconcileTheme();
    };
    media.addEventListener("change", onChange);
    detachThisController = () =>
      media.removeEventListener("change", onChange);
    detachMediaListener = detachThisController;
  }
  reconcileTheme();

  let stopped = false;
  return () => {
    if (stopped) return;
    stopped = true;
    detachThisController?.();
    if (detachMediaListener === detachThisController) {
      detachMediaListener = null;
      activeMediaQuery = null;
    }
  };
}

export function resolveWebTheme(
  requested: WebTheme,
  prefersLight: boolean
): ResolvedWebTheme {
  if (requested === "light") return "light";
  if (requested === "dark") return "dark";
  return prefersLight ? "light" : "dark";
}

function reconcileTheme(): ResolvedWebTheme {
  const resolved = resolveWebTheme(
    webTheme.value,
    activeMediaQuery?.matches ?? systemPrefersLight()
  );
  resolvedWebTheme.value = resolved;

  const root = document.documentElement;
  root.dataset.theme = webTheme.value;
  root.dataset.resolvedTheme = resolved;
  root.style.colorScheme = resolved;

  const colorScheme = document.querySelector<HTMLMetaElement>(
    'meta[name="color-scheme"]'
  );
  if (colorScheme) colorScheme.content = resolved;

  const themeColor = document.querySelector<HTMLMetaElement>(
    'meta[name="theme-color"]'
  );
  if (themeColor) {
    themeColor.content =
      resolved === "light" ? lightThemeColor : darkThemeColor;
  }
  return resolved;
}

function systemPrefersLight(): boolean {
  return (
    typeof window.matchMedia === "function" &&
    window.matchMedia(systemThemeQuery).matches
  );
}

function isWebTheme(value: unknown): value is WebTheme {
  return value === "system" || value === "light" || value === "dark";
}
