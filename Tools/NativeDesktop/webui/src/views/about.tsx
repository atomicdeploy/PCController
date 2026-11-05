import { Card } from "../components/controls";
import { Icon } from "../components/icon";
import {
  activeClientKind,
  activeClientLabel,
  capabilities,
  eventConnected,
  version
} from "../store";
import { BidiLtr, t } from "../i18n";

export function About() {
  const info = version.value;
  const realtimeDisabled = capabilities.value.websocket_events === false;
  return (
    <div class="view about-view">
      <header class="view-heading">
        <div>
          <span class="eyebrow">{t("Application")}</span>
          <h1>{t("About PCController")}</h1>
          <p>{t("A private, single-executable Windows controller by AtomicDeploy.")}</p>
        </div>
      </header>
      <div class="about-grid">
        <Card class="about-identity">
          <div class="about-mark" aria-hidden="true">
            <Icon name="device" size={42} />
          </div>
          <div>
            <span class="eyebrow">{info?.company || "AtomicDeploy"}</span>
            <h2>{info?.display_name || t("PCController")}</h2>
            <p>{t(info?.description || "Local controller")}</p>
          </div>
          <dl>
            <div><dt>{t("Version")}</dt><dd><BidiLtr>{info?.version || "1.0.0"}</BidiLtr></dd></div>
            {info?.edition && (
              <div>
                <dt>{t("Edition")}</dt>
                <dd>{t(info.edition === "production" ? "Production" : "Development")}</dd>
              </div>
            )}
            {info?.commit && <div><dt>{t("Commit")}</dt><dd><code dir="ltr">{info.commit}</code></dd></div>}
            {info?.date && <div><dt>{t("Built")}</dt><dd><BidiLtr>{info.date}</BidiLtr></dd></div>}
          </dl>
        </Card>
        <Card class="privacy-card">
          <span class="card-icon"><Icon name="network" /></span>
          <h2>{t("Local by design")}</h2>
          <p>
            {activeClientKind.value === "direct"
              ? t("Direct mode sends simple requests from this browser to the configured device. No cloud service or manager token is involved.")
              : t("The Go coordinator serves this dashboard on a loopback-only listener. Its temporary browser session never exposes the long-lived API bearer token.")}
          </p>
        </Card>
        <Card class="privacy-card">
          <span class="card-icon"><Icon name="check" /></span>
          <h2>{t("Shared state")}</h2>
          <p>
            {activeClientKind.value === "direct"
              ? t("Tabs share one elected firmware event socket and fan confirmed device state through BroadcastChannel.")
              : capabilities.value.native_shell === false
                ? t("The headless web host, CLI, terminal UI, IPC clients, and automation share one command dispatcher and confirmed device snapshot.")
                : t("Native UI, tray, CLI, terminal UI, automation, and this web client all reach the same command dispatcher and confirmed device snapshot.")}
          </p>
        </Card>
        <Card class="capability-card">
          <div class="card-heading-row">
            <h2>{t("Web session")}</h2>
            <span class={`mini-status ${eventConnected.value ? "is-active" : "is-inactive"}`}>
              <span aria-hidden="true" />
              {realtimeDisabled
                ? t("HTTP polling")
                : eventConnected.value
                  ? t("Events connected")
                  : t("Events reconnecting")}
            </span>
          </div>
          <p class="capability-transport">
            {t("Active transport:")} <strong>{t(activeClientLabel.value)}</strong>
          </p>
          <ul>
            {Object.entries(capabilities.value).map(([name, enabled]) => (
              <li key={name}>
                <Icon name={enabled ? "check" : "close"} size={16} />
                <span>{name.replaceAll("_", " ")}</span>
              </li>
            ))}
          </ul>
        </Card>
      </div>
    </div>
  );
}
