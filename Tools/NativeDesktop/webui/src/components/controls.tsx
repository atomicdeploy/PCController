import type {
  ButtonHTMLAttributes,
  ComponentChildren,
  TargetedInputEvent
} from "preact";
import { t } from "../i18n";

export function Card({
  children,
  class: className = "",
  as: Tag = "section",
  ...props
}: {
  children: ComponentChildren;
  class?: string;
  as?: "section" | "article" | "div";
} & Record<string, unknown>) {
  return (
    <Tag class={`glass-card ${className}`} {...props}>
      {children}
    </Tag>
  );
}

export function FieldGroup({
  title,
  description,
  children
}: {
  title: string;
  description?: string;
  children: ComponentChildren;
}) {
  return (
    <fieldset class="field-group">
      <legend>{t(title)}</legend>
      {description && <p class="field-group-description">{t(description)}</p>}
      <div class="field-stack">{children}</div>
    </fieldset>
  );
}

export function Toggle({
  label,
  description,
  checked,
  disabled,
  onChange
}: {
  label: string;
  description?: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label class={`toggle-row${disabled ? " is-disabled" : ""}`}>
      <span class="field-copy">
        <span class="field-label">{t(label)}</span>
        {description && <span class="field-description">{t(description)}</span>}
      </span>
      <input
        class="toggle-input"
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.currentTarget.checked)}
      />
      <span class="toggle-track" aria-hidden="true">
        <span class="toggle-thumb" />
      </span>
    </label>
  );
}

export function TextField({
  label,
  description,
  value,
  placeholder,
  type = "text",
  readOnly,
  autoComplete,
  dir,
  onInput
}: {
  label: string;
  description?: string;
  value: string;
  placeholder?: string;
  type?: "text" | "password" | "url";
  readOnly?: boolean;
  autoComplete?: string;
  dir?: "ltr" | "rtl" | "auto";
  onInput?: (value: string) => void;
}) {
  const id = fieldID(label);
  const inputProps = {
    id,
    value,
    placeholder,
    readOnly,
    autocomplete: autoComplete,
    dir:
      dir ??
      (type === "url" || type === "password" ? ("ltr" as const) : undefined),
    onInput: (event: TargetedInputEvent<HTMLInputElement>) =>
      onInput?.(event.currentTarget.value)
  };
  return (
    <label class="input-field" for={id}>
      <span class="field-label">{t(label)}</span>
      {description && <span class="field-description">{t(description)}</span>}
      {type === "password" ? (
        <input type="password" {...inputProps} />
      ) : type === "url" ? (
        <input type="url" {...inputProps} />
      ) : (
        <input type="text" {...inputProps} />
      )}
    </label>
  );
}

export function NumberField({
  label,
  description,
  value,
  min,
  max,
  step = 1,
  onInput
}: {
  label: string;
  description?: string;
  value: number;
  min?: number;
  max?: number;
  step?: number;
  onInput: (value: number) => void;
}) {
  const id = fieldID(label);
  return (
    <label class="input-field" for={id}>
      <span class="field-label">{t(label)}</span>
      {description && <span class="field-description">{t(description)}</span>}
      <input
        id={id}
        type="number"
        value={String(value)}
        min={min}
        max={max}
        step={step}
        inputMode="decimal"
        onInput={(event) => {
          const next = event.currentTarget.valueAsNumber;
          if (Number.isFinite(next)) onInput(next);
        }}
      />
    </label>
  );
}

export function SelectField<T extends string>({
  label,
  description,
  value,
  options,
  onChange
}: {
  label: string;
  description?: string;
  value: T;
  options: ReadonlyArray<{ value: T; label: string }>;
  onChange: (value: T) => void;
}) {
  const id = fieldID(label);
  return (
    <label class="input-field" for={id}>
      <span class="field-label">{t(label)}</span>
      {description && <span class="field-description">{t(description)}</span>}
      <span class="select-wrap">
        <select
          id={id}
          value={value}
          onChange={(event) => onChange(event.currentTarget.value as T)}
        >
          {options.map((option) => (
            <option value={option.value} key={option.value}>
              {t(option.label)}
            </option>
          ))}
        </select>
      </span>
    </label>
  );
}

export function TextAreaField({
  label,
  description,
  value,
  rows = 4,
  spellcheck = false,
  onInput
}: {
  label: string;
  description?: string;
  value: string;
  rows?: number;
  spellcheck?: boolean;
  onInput: (value: string) => void;
}) {
  const id = fieldID(label);
  return (
    <label class="input-field" for={id}>
      <span class="field-label">{t(label)}</span>
      {description && <span class="field-description">{t(description)}</span>}
      <textarea
        id={id}
        value={value}
        rows={rows}
        spellcheck={spellcheck}
        onInput={(event) => onInput(event.currentTarget.value)}
      />
    </label>
  );
}

export function Button({
  children,
  kind = "secondary",
  busy,
  icon,
  class: className = "",
  ...props
}: {
  children: ComponentChildren;
  kind?: "primary" | "secondary" | "quiet" | "danger";
  busy?: boolean;
  icon?: ComponentChildren;
  class?: string;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      class={`button button-${kind}${busy ? " is-busy" : ""} ${className}`}
      aria-busy={busy || undefined}
      {...props}
    >
      {busy && <span class="button-spinner" aria-hidden="true" />}
      {!busy && icon}
      <span>{typeof children === "string" ? t(children) : children}</span>
    </button>
  );
}

let nextFieldID = 0;
function fieldID(label: string): string {
  const slug = label.toLowerCase().replace(/[^a-z0-9]+/g, "-");
  return `field-${slug}-${++nextFieldID}`;
}
