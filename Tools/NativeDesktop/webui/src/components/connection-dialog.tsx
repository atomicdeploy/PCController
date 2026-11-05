import { useEffect, useState } from "preact/hooks";
import type { ClientKind } from "../api";
import type {
  ConnectionMode,
  ConnectionPreferences
} from "../connection-preferences";
import { isStaticExport } from "../connection-preferences";
import { normalizeLampBaseURL } from "../direct-protocol";
import { Button, SelectField, TextField } from "./controls";
import { Icon } from "./icon";
import { Modal } from "./modal";
import { t } from "../i18n";

export function ConnectionDialog({
  open,
  preferences,
  activeKind,
  activeLabel,
  onClose,
  onApply
}: {
  open: boolean;
  preferences: ConnectionPreferences;
  activeKind: ClientKind;
  activeLabel: string;
  onClose: () => void;
  onApply: (value: ConnectionPreferences) => void;
}) {
  const [mode, setMode] = useState<ConnectionMode>(preferences.mode);
  const [lampURL, setLampURL] = useState(preferences.lampURL);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    setMode(preferences.mode);
    setLampURL(preferences.lampURL);
    setError("");
  }, [open, preferences.mode, preferences.lampURL]);

  const apply = () => {
    try {
      onApply({
        mode,
        lampURL: normalizeLampBaseURL(lampURL)
      });
      onClose();
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : t("Enter a valid lamp address.")
      );
    }
  };

  return (
    <Modal
      open={open}
      title={t("Connection mode")}
      description={t("Choose who owns lamp communication. This preference is shared with other dashboard tabs.")}
      onClose={onClose}
    >
      <div class="connection-dialog">
        <div class="active-transport" role="status">
          <span class={`transport-glyph transport-${activeKind}`}>
            <Icon name={activeKind === "direct" ? "lamp" : "network"} />
          </span>
          <span>
            <small>{t("Active now")}</small>
            <strong>{t(activeLabel)}</strong>
          </span>
        </div>

        <SelectField
          label={t("Preferred transport")}
          value={mode}
          options={[
            {
              value: "auto",
              label: t("Automatic — coordinator, then direct")
            },
            {
              value: "relay",
              label: t("Go coordinator")
            },
            {
              value: "direct",
              label: t("Direct to lamp")
            }
          ]}
          onChange={(value) => setMode(value as ConnectionMode)}
        />

        <div class="transport-explainer">
          <div class={mode === "relay" ? "is-selected" : ""}>
            <Icon name="network" />
            <span>
              <strong>{t("Go coordinator")}</strong>
              <small>
                {t("Uses the same-origin RayanLamp manager for resilient discovery, settings, diagnostics, automation, and native integration.")}
              </small>
            </span>
          </div>
          <div class={mode === "direct" ? "is-selected" : ""}>
            <Icon name="lamp" />
            <span>
              <strong>{t("Direct browser connection")}</strong>
              <small>
                {t("Uses firmware HTTP and WebSocket endpoints. Native-only features are intentionally hidden.")}
              </small>
            </span>
          </div>
        </div>

        {(mode === "direct" || mode === "auto") && (
          <TextField
            label={t("Lamp address")}
            type="url"
            value={lampURL}
            placeholder="http://rayanlamp.local"
            description={t("HTTP or HTTPS origin only; no path, query, fragment, or credentials.")}
            onInput={setLampURL}
          />
        )}

        {isStaticExport() && mode === "relay" && (
          <p class="connection-note">
            <Icon name="info" size={16} />
            {t("The static edition cannot borrow the manager’s private browser session cross-origin. Open the dashboard served by RayanLamp.exe to use coordinator mode.")}
          </p>
        )}
        {(mode === "direct" || mode === "auto") && (
          <p class="connection-note">
            <Icon name="info" size={16} />
            {t("Serve the ZIP over HTTP. An HTTPS page cannot control the current HTTP/WS lamp because browsers block mixed content.")}
          </p>
        )}
        {error && (
          <p class="field-error" role="alert">
            {t(error)}
          </p>
        )}
      </div>
      <div class="modal-actions">
        <Button kind="quiet" onClick={onClose}>
          {t("Cancel")}
        </Button>
        <Button kind="primary" onClick={apply}>
          {t("Apply and reconnect")}
        </Button>
      </div>
    </Modal>
  );
}
