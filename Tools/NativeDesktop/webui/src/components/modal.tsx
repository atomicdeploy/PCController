import { useEffect, useId, useRef } from "preact/hooks";
import type { ComponentChildren } from "preact";
import { Icon } from "./icon";
import { t } from "../i18n";

export function Modal({
  open,
  title,
  description,
  children,
  onClose
}: {
  open: boolean;
  title: string;
  description?: ComponentChildren;
  children: ComponentChildren;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const identity = useId();
  const titleID = `${identity}-title`;
  const descriptionID = `${identity}-description`;

  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (open && !element.open) element.showModal();
    if (!open && element.open) element.close();
  }, [open]);

  return (
    <dialog
      ref={dialog}
      class="modal"
      aria-labelledby={titleID}
      aria-describedby={description ? descriptionID : undefined}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClose={onClose}
      onClick={(event) => {
        if (event.target === dialog.current) onClose();
      }}
    >
      <div class="modal-surface">
        <header class="modal-header">
          <div>
            <h2 id={titleID}>{title}</h2>
            {description && <p id={descriptionID}>{description}</p>}
          </div>
          <button class="icon-button" type="button" onClick={onClose} aria-label={t("Close dialog")}>
            <Icon name="close" />
          </button>
        </header>
        <div class="modal-content">{children}</div>
      </div>
    </dialog>
  );
}
