import type { ITheme, Terminal as XTermTerminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef, useState } from "preact/hooks";
import type { RayanLampClient } from "../api";
import type {
  DeviceTerminalStatus,
  DeviceTerminalState
} from "../contracts";
import {
  activeClientKind,
  capabilities,
  config,
  resolvedWebTheme,
  webTheme
} from "../store";
import {
  TerminalBridge,
  terminalKeyboardInput,
  type TerminalTransportPhase
} from "../terminal-client";
import { Button, Card } from "../components/controls";
import { Icon } from "../components/icon";
import { BidiLtr, formatNumber, t } from "../i18n";
import { developerTools } from "../build-edition";

const disconnectedStatus: DeviceTerminalStatus = {
  state: "disconnected",
  host: "",
  port: 23,
  session: 0,
  sequence: 0
};

export function DeviceTerminal({ api }: { api: RayanLampClient }) {
  const direct =
    activeClientKind.value === "direct" ||
    capabilities.value.transport_direct === true ||
    api.kind === "direct";
  const available =
    !direct &&
    api.kind === "relay" &&
    capabilities.value.device_terminal === true;

  return available ? (
    <CoordinatorTerminal api={api} />
  ) : (
    <TerminalUnavailable direct={direct} />
  );
}

function CoordinatorTerminal({ api }: { api: RayanLampClient }) {
  const host = useRef<HTMLDivElement>(null);
  const terminal = useRef<XTermTerminal | null>(null);
  const bridge = useRef<TerminalBridge | null>(null);
  const inputEnabled = useRef(false);
  const [status, setStatus] =
    useState<DeviceTerminalStatus>(disconnectedStatus);
  const [transport, setTransport] =
    useState<TerminalTransportPhase>("opening");
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    let resizeObserver: ResizeObserver | null = null;
    let disposeInput: (() => void) | null = null;
    let disposeWebGL: (() => void) | null = null;
    let fit: (() => void) | null = null;

    void Promise.all([
      import("@xterm/xterm"),
      import("@xterm/addon-fit")
    ])
      .then(([xtermModule, fitModule]) => {
        if (!active || !host.current) return;
        const xterm = new xtermModule.Terminal({
          allowProposedApi: false,
          allowTransparency: true,
          convertEol: false,
          cursorBlink: config.value?.ui.animations !== false,
          cursorStyle: "bar",
          disableStdin: true,
          drawBoldTextInBrightColors: true,
          fontFamily:
            '"Cascadia Mono", "Cascadia Code", Consolas, "Courier New", monospace',
          fontSize: 14,
          fontWeight: "400",
          fontWeightBold: 650,
          letterSpacing: 0,
          lineHeight: 1.16,
          logLevel: developerTools ? "info" : "warn",
          minimumContrastRatio: 4.5,
          rightClickSelectsWord: true,
          screenReaderMode: true,
          scrollback: 5_000,
          scrollOnUserInput: true,
          theme: terminalThemeFor(
            webTheme.value,
            resolvedWebTheme.value === "light"
          )
        });
        const fitAddon = new fitModule.FitAddon();
        xterm.loadAddon(fitAddon);
        xterm.open(host.current);
        host.current.dataset.renderer = "canvas";
        void import("@xterm/addon-webgl")
          .then(({ WebglAddon }) => {
            if (!active || !host.current?.isConnected) return;
            const webgl = new WebglAddon();
            let disposed = false;
            const dispose = () => {
              if (disposed) return;
              disposed = true;
              webgl.dispose();
              if (host.current) host.current.dataset.renderer = "canvas";
            };
            webgl.onContextLoss(dispose);
            try {
              xterm.loadAddon(webgl);
              host.current.dataset.renderer = "webgl";
              disposeWebGL = dispose;
            } catch {
              dispose();
            }
          })
          .catch(() => {
            if (host.current) host.current.dataset.renderer = "canvas";
          });
        xterm.textarea?.setAttribute(
          "aria-label",
          t("Rayan Lamp device terminal input")
        );
        terminal.current = xterm;
        fit = () => {
          if (!active || !host.current?.isConnected) return;
          try {
            fitAddon.fit();
          } catch {
            // A hidden or mid-layout host can briefly have no measurable size.
          }
        };
        window.requestAnimationFrame(fit);
        if (typeof ResizeObserver === "function") {
          resizeObserver = new ResizeObserver(fit);
          resizeObserver.observe(host.current);
        } else {
          window.addEventListener("resize", fit);
        }
        const inputSubscription = xterm.onData((data) => {
          if (inputEnabled.current) {
            bridge.current?.sendInput(terminalKeyboardInput(data));
          }
        });
        disposeInput = () => inputSubscription.dispose();

        const terminalBridge = new TerminalBridge({
          onTransportChange: (phase) => {
            if (!active) return;
            setTransport(phase);
            if (phase === "ready") setError("");
          },
          onStatus: (next) => {
            if (!active) return;
            setStatus(next);
            setError(next.last_error ?? "");
          },
          onOutput: (data) => {
            if (active) xterm.write(data);
          },
          onReset: () => {
            if (!active) return;
            const restoreFocus = document.activeElement === xterm.textarea;
            // xterm's screen-reader live region is intentionally separate
            // from its screen buffer. Blur clears that announcement buffer so
            // a session purge does not leave the old transcript in the DOM.
            xterm.blur();
            xterm.reset();
            xterm.clear();
            if (restoreFocus) xterm.focus();
          },
          onError: (_code, message) => {
            if (active) setError(message);
          }
        });
        bridge.current = terminalBridge;
        terminalBridge.open();
      })
      .catch((loadError) => {
        if (!active) return;
        setTransport("closed");
        setError(
          loadError instanceof Error
            ? loadError.message
            : t("The terminal renderer could not be loaded.")
        );
      });

    return () => {
      active = false;
      resizeObserver?.disconnect();
      if (fit) window.removeEventListener("resize", fit);
      disposeInput?.();
      disposeWebGL?.();
      bridge.current?.dispose();
      bridge.current = null;
      terminal.current?.dispose();
      terminal.current = null;
    };
  }, []);

  const resolvedLight = resolvedWebTheme.value === "light";
  useEffect(() => {
    if (terminal.current) {
      terminal.current.options.theme = terminalThemeFor(
        webTheme.value,
        resolvedLight
      );
    }
  }, [resolvedLight, webTheme.value]);

  const canType = terminalAcceptsInput(status, transport);
  const animationsEnabled = config.value?.ui.animations !== false;
  inputEnabled.current = canType;
  useEffect(() => {
    if (terminal.current) {
      terminal.current.options.disableStdin = !canType;
    }
  }, [canType]);
  useEffect(() => {
    if (terminal.current) {
      terminal.current.options.cursorBlink = animationsEnabled;
    }
  }, [animationsEnabled]);

  const reconnectTransport = () => {
    setError("");
    setTransport("opening");
    void api
      .establishSession(true)
      .then(() => bridge.current?.open())
      .catch((sessionError) => {
        setTransport("closed");
        setError(
          sessionError instanceof Error
            ? sessionError.message
            : t("The coordinator session could not be renewed.")
        );
      });
  };
  const connectDevice = () => {
    setError("");
    bridge.current?.connect();
  };
  const disconnectDevice = () => {
    setError("");
    bridge.current?.disconnect();
  };
  const clearTerminal = () => {
    const current = terminal.current;
    if (!current) return;
    current.blur();
    current.clear();
    current.focus();
  };
  const shownPort =
    status.port || config.value?.lamp.telnet_port || disconnectedStatus.port;

  return (
    <div class="view terminal-view">
      <header class="view-heading">
        <div>
          <span class="eyebrow">{t("Coordinator-secured TCP bridge")}</span>
          <h1>{t("Device terminal")}</h1>
          <p>
            {t("An ANSI terminal connected to the lamp through the local Go coordinator. Output exists only in this page’s memory.")}
          </p>
        </div>
        <TerminalPhase status={status.state} transport={transport} />
      </header>

      {status.read_only && (
        <div class="terminal-warning" role="status">
          <Icon name="warning" />
          <div>
            <strong>{t("Firmware terminal is read-only")}</strong>
            <span>
              {t("This firmware reports that its interactive shell is assigned to Serial. Network output remains available, but keyboard input is intentionally disabled.")}
            </span>
          </div>
        </div>
      )}

      {error && (
        <div class="error-banner terminal-error" role="alert">
          <Icon name="warning" />
          <div>
            <strong>{t("Terminal needs attention")}</strong>
            <span>{t(error)}</span>
          </div>
        </div>
      )}

      <Card class="terminal-card">
        <header class="terminal-toolbar">
          <div class="terminal-target">
            <span class="terminal-target-icon">
              <Icon name="terminal" />
            </span>
            <span>
              <small>{status.host ? <BidiLtr>{status.host}</BidiLtr> : t("Configured lamp")}</small>
              <strong><BidiLtr>TCP {shownPort}</BidiLtr></strong>
            </span>
          </div>
          <div class="terminal-actions">
            {transport === "closed" ? (
              <Button
                kind="primary"
                icon={<Icon name="reconnect" />}
                onClick={reconnectTransport}
              >
                {t("Reconnect coordinator")}
              </Button>
            ) : status.state === "disconnected" ? (
              <Button
                kind="primary"
                icon={<Icon name="power" />}
                disabled={transport !== "ready"}
                busy={transport === "opening"}
                onClick={connectDevice}
              >
                {transport === "opening" ? t("Preparing") : t("Connect")}
              </Button>
            ) : (
              <Button
                kind="danger"
                icon={<Icon name="close" />}
                busy={status.state === "connecting"}
                disabled={transport !== "ready"}
                onClick={disconnectDevice}
              >
                {t("Disconnect")}
              </Button>
            )}
            <Button
              kind="quiet"
              icon={<Icon name="refresh" />}
              onClick={clearTerminal}
            >
              {t("Clear")}
            </Button>
          </div>
        </header>

        <div
          class={`terminal-frame${canType ? " accepts-input" : ""}`}
          role="region"
          aria-label={t("Rayan Lamp terminal output")}
          onClick={() => terminal.current?.focus()}
        >
          <div ref={host} class="terminal-host" />
          {status.state !== "connected" && (
            <div class="terminal-idle" aria-hidden="true">
              <Icon name="terminal" size={28} />
              <span>
                {status.state === "connecting"
                  ? t("Opening the device terminal…")
                  : t("Connect when you need the device console")}
              </span>
            </div>
          )}
        </div>

        <footer class="terminal-footer">
          <span>
            <Icon name={canType ? "keyboard" : "info"} size={15} />
            {canType
              ? t("Send one short command at a time; firmware lines are limited to 64 ASCII bytes.")
              : status.read_only
                ? t("Output-only session")
                : t("No input is sent while disconnected.")}
          </span>
          <span>{t("Session {session} · Sequence {sequence}", {
            session: status.session ? formatNumber(status.session) : "—",
            sequence: formatNumber(status.sequence)
          })}</span>
        </footer>
      </Card>

      <div class="security-note terminal-security-note">
        <Icon name="warning" />
        <span>
          {t("Device-shell commands can change firmware settings or restart the lamp. The coordinator fixes the destination to the configured lamp and terminal port; this browser never opens raw TCP itself.")}
        </span>
      </div>
    </div>
  );
}

export function TerminalUnavailable({ direct }: { direct: boolean }) {
  return (
    <div class="view terminal-view">
      <header class="view-heading">
        <div>
          <span class="eyebrow">{t("Device console")}</span>
          <h1>{t("Device terminal")}</h1>
          <p>
            {t("The interactive terminal is intentionally isolated behind the Go coordinator.")}
          </p>
        </div>
      </header>
      <Card class="terminal-unavailable">
        <span class="terminal-unavailable-icon">
          <Icon name="terminal" size={38} />
        </span>
        <span class="eyebrow">{t("Coordinator required")}</span>
        <h2>{t("Raw TCP is not available in direct browser mode")}</h2>
        <p>
          {t("Browsers cannot connect directly to the lamp’s Telnet port. Choose Automatic or Go coordinator to use the authenticated same-origin terminal bridge.")}
        </p>
        {direct && (
          <p class="terminal-static-note">
            {t("For the frontend-only ZIP, serve it behind the same origin that forwards")}{" "}
            <code dir="ltr">/app/session</code>{" "}
            {t("and the versionless API paths to a running RayanLamp coordinator.")}
          </p>
        )}
      </Card>
    </div>
  );
}

function TerminalPhase({
  status,
  transport
}: {
  status: DeviceTerminalState;
  transport: TerminalTransportPhase;
}) {
  const label =
    transport === "opening"
      ? t("Preparing bridge")
      : transport === "closed"
        ? t("Coordinator unavailable")
        : status === "connected"
          ? t("Terminal connected")
          : status === "connecting"
            ? t("Connecting to device")
            : t("Terminal disconnected");
  return (
    <span
      class={`terminal-phase terminal-phase-${transport === "ready" ? status : transport}`}
      role="status"
      aria-live="polite"
    >
      <span aria-hidden="true" />
      {label}
    </span>
  );
}

export function terminalThemeFor(
  theme: "system" | "light" | "dark",
  systemPrefersLight: boolean
): ITheme {
  const light = theme === "light" || (theme === "system" && systemPrefersLight);
  return light
    ? {
        background: "rgba(245, 250, 253, 0.94)",
        foreground: "#183044",
        cursor: "#006f92",
        cursorAccent: "#f5fafd",
        selectionBackground: "rgba(0, 132, 174, 0.22)",
        selectionForeground: "#102534",
        black: "#183044",
        red: "#b4233c",
        green: "#14754d",
        yellow: "#946400",
        blue: "#006f92",
        magenta: "#7c45a3",
        cyan: "#007f88",
        white: "#dce9ef",
        brightBlack: "#607786",
        brightRed: "#d53950",
        brightGreen: "#16885a",
        brightYellow: "#ad7600",
        brightBlue: "#008bb7",
        brightMagenta: "#9558ba",
        brightCyan: "#0099a2",
        brightWhite: "#ffffff"
      }
    : {
        background: "rgba(4, 13, 22, 0.9)",
        foreground: "#dcebf3",
        cursor: "#67dcff",
        cursorAccent: "#06111c",
        selectionBackground: "rgba(82, 205, 243, 0.24)",
        selectionForeground: "#f4fbff",
        black: "#07111c",
        red: "#ff6f83",
        green: "#60d89b",
        yellow: "#f1c75b",
        blue: "#67b7ff",
        magenta: "#c99bff",
        cyan: "#67dcff",
        white: "#dcebf3",
        brightBlack: "#728794",
        brightRed: "#ff91a1",
        brightGreen: "#81e7b3",
        brightYellow: "#ffe08a",
        brightBlue: "#8dcbff",
        brightMagenta: "#ddb9ff",
        brightCyan: "#91eaff",
        brightWhite: "#ffffff"
      };
}

export function terminalAcceptsInput(
  status: DeviceTerminalStatus,
  transport: TerminalTransportPhase
): boolean {
  return (
    transport === "ready" &&
    status.state === "connected" &&
    status.read_only !== true
  );
}
