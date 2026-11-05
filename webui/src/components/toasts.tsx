import { dismissToast, toasts } from "../store";
import { Icon } from "./icon";
import { t } from "../i18n";

export function ToastRegion() {
  return (
    <div class="toast-region" aria-live="polite" aria-atomic="false">
      {toasts.value.map((toast) => (
        <div class={`app-toast toast-${toast.tone}`} role="status" key={toast.id}>
          <span class="toast-icon">
            <Icon
              name={
                toast.tone === "error"
                  ? "warning"
                  : toast.tone === "success"
                    ? "check"
                    : "info"
              }
            />
          </span>
          <span class="toast-copy">
            <strong>{toast.title}</strong>
            {toast.message && <span>{toast.message}</span>}
          </span>
          <button
            type="button"
            class="icon-button"
            aria-label={t("Dismiss {title}", { title: toast.title })}
            onClick={() => dismissToast(toast.id)}
          >
            <Icon name="close" size={16} />
          </button>
        </div>
      ))}
    </div>
  );
}
