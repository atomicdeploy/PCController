import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState
} from "preact/hooks";
import { registerSW } from "virtual:pwa-register";
import type { RayanLampClient } from "./api";
import type { Snapshot } from "./contracts";
import { createRayanLampClient } from "./client-factory";
import {
  readConnectionPreferences,
  saveConnectionPreferences,
  subscribeConnectionPreferences,
  type ConnectionPreferences
} from "./connection-preferences";
import { parseDurationMilliseconds } from "./direct-protocol";
import { startWebFaviconController } from "./favicon";
import { RealtimeSync } from "./sync";
import {
  activeClientKind,
  activeClientLabel,
  appPhase,
  applyBootstrap,
  applySnapshot,
  beginControllerConnection,
  capabilities,
  configureSettingsPersistence,
  eventConnected,
  route,
  setWebTheme,
  showToast,
  webTheme,
  type RouteName
} from "./store";
import { Icon, type IconName } from "./components/icon";
import { Button } from "./components/controls";
import { ConnectionDialog } from "./components/connection-dialog";
import { Modal } from "./components/modal";
import { ToastRegion } from "./components/toasts";
import { Dashboard } from "./views/dashboard";
import { Settings } from "./views/settings";
import { DeviceTerminal } from "./views/terminal";
import { Diagnostics } from "./views/diagnostics";
import { About } from "./views/about";
import { startWebThemeController } from "./theme";
import {
  browserConsole,
  browserErrorMetadata
} from "./browser-console";
import { browserAudio } from "./audio";
import {
  startWebLocaleController,
  t
} from "./i18n";

const navigation: Array<{
  route: RouteName;
  label: string;
  icon: IconName;
}> = [
  { route: "dashboard", label: "Dashboard", icon: "lamp" },
  { route: "settings", label: "Settings", icon: "settings" },
  { route: "terminal", label: "Terminal", icon: "terminal" },
  { route: "diagnostics", label: "Diagnostics", icon: "diagnostics" },
  { route: "about", label: "About", icon: "about" }
];

export function App({ api: suppliedAPI }: { api?: RayanLampClient } = {}) {
  const [preferences, setPreferences] = useState<ConnectionPreferences>(
    readConnectionPreferences
  );
  const api = useMemo(
    () => suppliedAPI ?? createRayanLampClient(preferences),
    // Preference announcements also act as a transport lifecycle generation.
    // Direct-mode timing/WebSocket edits intentionally announce the same URL
    // so the socket and fallback poll are recreated immediately.
    [suppliedAPI, preferences]
  );
  const [fatalError, setFatalError] = useState("");
  const [retry, setRetry] = useState(0);
  const [connectionOpen, setConnectionOpen] = useState(false);
  const [updateReady, setUpdateReady] = useState(false);
  const [applyUpdate, setApplyUpdate] =
    useState<((reloadPage?: boolean) => Promise<void>) | null>(null);
  const syncRef = useRef<RealtimeSync | null>(null);
  const acceptControllerSnapshot = useCallback((next: Snapshot) => {
    const sync = syncRef.current;
    if (sync) {
      sync.acceptSnapshot(next);
      return;
    }
    applySnapshot(next);
  }, []);

  useEffect(() => {
    const stopThemeController = startWebThemeController();
    const stopLocaleController = startWebLocaleController();
    const stopFaviconController = startWebFaviconController();
    const stopAudioUnlock = browserAudio.armUserGestureUnlock();
    route.value = routeFromHash(window.location.hash);
    const onHashChange = () => {
      route.value = routeFromHash(window.location.hash);
    };
    window.addEventListener("hashchange", onHashChange);
    return () => {
      stopThemeController();
      stopLocaleController();
      stopFaviconController();
      stopAudioUnlock();
      window.removeEventListener("hashchange", onHashChange);
    };
  }, []);

  useEffect(() => {
    if (suppliedAPI) return;
    return subscribeConnectionPreferences(setPreferences);
  }, [suppliedAPI]);

  useEffect(() => {
    let active = true;
    let sync: RealtimeSync | null = null;
    let poll = 0;
    configureSettingsPersistence(api);
    activeClientKind.value = api.kind;
    activeClientLabel.value = api.displayName;
    beginControllerConnection();
    setFatalError("");
    const transportStarted = performance.now();

    void (async () => {
      try {
        const bootstrap = await api.bootstrap();
        if (!active) return;
        activeClientKind.value = api.kind;
        activeClientLabel.value =
          api.kind === "relay" &&
          bootstrap.capabilities?.native_shell === false
            ? "Headless Go coordinator"
            : api.displayName;
        applyBootstrap(bootstrap);
        browserConsole.event(
          "success",
          "transport.ready",
          `${activeClientLabel.value} is ready`,
          {
            source: api.kind,
            action: "bootstrap",
            elapsed_ms: Math.round(performance.now() - transportStarted)
          }
        );
        sync = new RealtimeSync(api, {
          connectEvents:
            bootstrap.capabilities?.websocket_events !== false
        });
        syncRef.current = sync;
        sync.start();
        const configuredPoll = parseDurationMilliseconds(
          bootstrap.config.lamp.http_poll
        );
        if (configuredPoll !== 0) {
          poll = window.setInterval(async () => {
            if (!active || eventConnected.value) return;
            try {
              sync?.acceptSnapshot(await api.status());
            } catch {
              // WebSocket reconnection and the next poll retain responsibility.
            }
          }, Math.max(1_000, configuredPoll ?? 5_000));
        }
      } catch (error) {
        if (!active) return;
        appPhase.value = "offline";
        browserConsole.event(
          "error",
          "transport.error",
          "The selected controller transport could not start",
          {
            source: api.kind,
            action: "bootstrap",
            elapsed_ms: Math.round(performance.now() - transportStarted),
            ...browserErrorMetadata(error)
          }
        );
        setFatalError(
          error instanceof Error
            ? error.message
            : "The local Rayan Lamp controller is unavailable."
        );
      }
    })();

    return () => {
      active = false;
      window.clearInterval(poll);
      sync?.stop();
      if (syncRef.current === sync) {
        syncRef.current = null;
      }
      configureSettingsPersistence(null);
    };
  }, [api, retry]);

  useEffect(() => {
    if (
      !window.isSecureContext ||
      !("serviceWorker" in navigator)
    ) {
      return;
    }
    const update = registerSW({
      immediate: true,
      onNeedRefresh() {
        setApplyUpdate(() => update);
        setUpdateReady(true);
      },
      onOfflineReady() {
        showToast(
          "success",
          t("Offline fallback ready"),
          t("The app shell is cached; controls reconnect through the selected local transport.")
        );
      }
    });
  }, []);

  const view = useMemo(() => {
    switch (route.value) {
      case "settings":
        return <Settings api={api} />;
      case "terminal":
        return <DeviceTerminal api={api} />;
      case "diagnostics":
        return <Diagnostics api={api} />;
      case "about":
        return <About />;
      default:
        return (
          <Dashboard
            api={api}
            onSnapshot={acceptControllerSnapshot}
          />
        );
    }
  }, [acceptControllerSnapshot, api, route.value]);
  const realtimeDisabled = capabilities.value.websocket_events === false;
  const connectionIndicatorActive =
    eventConnected.value ||
    (realtimeDisabled &&
      (appPhase.value === "online" || appPhase.value === "degraded"));

  return (
    <div class="app-shell">
      <a class="skip-link" href="#main-content">
        {t("Skip to content")}
      </a>
      <aside class="app-rail">
        <button
          type="button"
          class="brand"
          aria-label={t("Open dashboard")}
          onClick={() => navigate("dashboard")}
        >
          <span class="brand-mark"><Icon name="lamp" size={28} /></span>
          <span class="brand-copy">
            <strong>{t("Rayan Lamp")}</strong>
            <small>{t("Local controller")}</small>
          </span>
        </button>
        <nav aria-label={t("Primary navigation")}>
          {navigation.map((item) => (
            <NavButton {...item} key={item.route} />
          ))}
        </nav>
        <div class="rail-footer">
          <button
            type="button"
            class="rail-transport"
            title={t("Connection: {name}", { name: t(activeClientLabel.value) })}
            onClick={() => setConnectionOpen(true)}
          >
            <Icon
              name={activeClientKind.value === "direct" ? "lamp" : "network"}
              size={15}
            />
            <span>{t(activeClientLabel.value)}</span>
          </button>
          <span
            class={`rail-connection ${connectionIndicatorActive ? "is-connected" : ""}`}
            title={
              realtimeDisabled
                ? t("WebSocket events disabled; HTTP polling is active")
                : eventConnected.value
                ? t("Realtime events connected")
                : t("Realtime events reconnecting")
            }
          >
            <span aria-hidden="true" />
            <span>
              {realtimeDisabled
                ? t("HTTP polling")
                : eventConnected.value
                  ? t("Events connected")
                  : t("Reconnecting")}
            </span>
          </span>
        </div>
      </aside>

      <div class="app-stage">
        <header class="top-bar">
          <div class="mobile-brand">
            <span class="brand-mark"><Icon name="lamp" size={22} /></span>
            <strong>{t("Rayan Lamp")}</strong>
          </div>
          <div class="top-bar-actions">
            <button
              type="button"
              class="transport-button"
              onClick={() => setConnectionOpen(true)}
              title={t("Choose how this dashboard connects")}
            >
              <span class={`transport-glyph transport-${activeClientKind.value}`}>
                <Icon
                  name={activeClientKind.value === "direct" ? "lamp" : "network"}
                  size={17}
                />
              </span>
              <span>
                <small>{t("Connection")}</small>
                <strong>{t(activeClientLabel.value)}</strong>
              </span>
              <Icon name="chevron" size={14} />
            </button>
            <div class="theme-switcher" role="group" aria-label={t("Color theme")}>
              {(["system", "light", "dark"] as const).map((theme) => (
                <button
                  type="button"
                  key={theme}
                  class={webTheme.value === theme ? "is-selected" : ""}
                  aria-pressed={webTheme.value === theme}
                  title={t("{theme} theme", { theme: themeLabel(theme) })}
                  onClick={() => setWebTheme(theme)}
                >
                  <Icon
                    name={
                      theme === "system"
                        ? "system"
                        : theme === "light"
                          ? "sun"
                          : "moon"
                    }
                    size={17}
                  />
                  <span>{themeLabel(theme)}</span>
                </button>
              ))}
            </div>
          </div>
        </header>

        <main id="main-content" tabIndex={-1}>
          {fatalError ? (
            <ConnectionError
              message={fatalError}
              onRetry={() => setRetry((value) => value + 1)}
              onConfigure={() => setConnectionOpen(true)}
            />
          ) : appPhase.value === "booting" ? (
            <LoadingView />
          ) : (
            view
          )}
        </main>
      </div>

      <nav class="mobile-nav" aria-label={t("Primary navigation")}>
        {navigation.map((item) => (
          <NavButton {...item} key={item.route} />
        ))}
      </nav>

      <ToastRegion />
      <ConnectionDialog
        open={connectionOpen}
        preferences={preferences}
        activeKind={activeClientKind.value}
        activeLabel={activeClientLabel.value}
        onClose={() => setConnectionOpen(false)}
        onApply={saveConnectionPreferences}
      />
      <Modal
        open={updateReady}
        title={t("Dashboard update ready")}
        description={t("A new bundled web interface is available from the running RayanLamp executable.")}
        onClose={() => setUpdateReady(false)}
      >
        <div class="modal-actions">
          <Button kind="quiet" onClick={() => setUpdateReady(false)}>
            {t("Later")}
          </Button>
          <Button
            kind="primary"
            onClick={() => {
              setUpdateReady(false);
              void applyUpdate?.(true);
            }}
          >
            {t("Reload dashboard")}
          </Button>
        </div>
      </Modal>
    </div>
  );
}

function NavButton({
  route: target,
  label,
  icon
}: {
  route: RouteName;
  label: string;
  icon: IconName;
}) {
  const current = route.value === target;
  return (
    <button
      type="button"
      class={`nav-button${current ? " is-current" : ""}`}
      aria-current={current ? "page" : undefined}
      onClick={() => navigate(target)}
    >
      <Icon name={icon} />
      <span>{t(label)}</span>
    </button>
  );
}

function navigate(target: RouteName): void {
  const hash = target === "dashboard" ? "#/" : `#/${target}`;
  if (window.location.hash !== hash) {
    window.location.hash = hash;
  }
  route.value = target;
  window.scrollTo({ top: 0, behavior: "smooth" });
  window.setTimeout(() => document.querySelector<HTMLElement>("#main-content")?.focus(), 0);
}

function routeFromHash(hash: string): RouteName {
  const segment = hash
    .replace(/^#\/?/, "")
    .split("/")[0]
    .toLowerCase();
  return navigation.some((item) => item.route === segment)
    ? (segment as RouteName)
    : "dashboard";
}

function LoadingView() {
  return (
    <div class="loading-view" role="status">
      <div class="loading-orbit" aria-hidden="true">
        <Icon name="lamp" size={36} />
      </div>
      <h1>{t("Connecting to Rayan Lamp")}</h1>
      <p>{t("Selecting a transport and synchronizing confirmed lamp state…")}</p>
    </div>
  );
}

function ConnectionError({
  message,
  onRetry,
  onConfigure
}: {
  message: string;
  onRetry: () => void;
  onConfigure: () => void;
}) {
  return (
    <div class="connection-error" role="alert">
      <span class="error-illustration"><Icon name="warning" size={42} /></span>
      <span class="eyebrow">{t("Selected connection unavailable")}</span>
      <h1>{t("The dashboard could not reach its controller")}</h1>
      <p>{t(message)}</p>
      <p class="error-help">
        {t("Coordinator mode needs the RayanLamp Go service. Direct mode needs this page served over HTTP and browser access to the lamp origin.")}
      </p>
      <div class="connection-error-actions">
        <Button kind="quiet" icon={<Icon name="network" />} onClick={onConfigure}>
          {t("Connection settings")}
        </Button>
        <Button kind="primary" icon={<Icon name="reconnect" />} onClick={onRetry}>
          {t("Try again")}
        </Button>
      </div>
    </div>
  );
}

function themeLabel(value: "system" | "light" | "dark"): string {
  return t(
    value === "system" ? "System" : value === "light" ? "Light" : "Dark"
  );
}
