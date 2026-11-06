import type { JSX } from "preact";

export type IconName =
  | "device"
  | "settings"
  | "diagnostics"
  | "about"
  | "power"
  | "refresh"
  | "reconnect"
  | "window"
  | "moon"
  | "sun"
  | "system"
  | "check"
  | "warning"
  | "close"
  | "menu"
  | "chevron"
  | "bell"
  | "sound"
  | "upload"
  | "keyboard"
  | "terminal"
  | "network"
  | "info";

const paths: Record<IconName, JSX.Element> = {
  device: (
    <>
      <path d="M12 2.75a6.75 6.75 0 0 0-4.2 12.03c.75.6 1.2 1.3 1.3 2.22h5.8c.1-.92.55-1.62 1.3-2.22A6.75 6.75 0 0 0 12 2.75Z" />
      <path d="M9.25 19h5.5M10 21.25h4" />
    </>
  ),
  settings: (
    <>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.86 2.86-.06-.06A1.7 1.7 0 0 0 15 19.4a1.7 1.7 0 0 0-1 .6 1.7 1.7 0 0 0-.4 1.1V21h-4v-.1A1.7 1.7 0 0 0 8.6 19.4a1.7 1.7 0 0 0-1.88.34l-.06.06-2.86-2.86.06-.06A1.7 1.7 0 0 0 4.2 15a1.7 1.7 0 0 0-.6-1 1.7 1.7 0 0 0-1.1-.4H2.4v-4h.1A1.7 1.7 0 0 0 4.2 8.6a1.7 1.7 0 0 0-.34-1.88l-.06-.06L6.66 3.8l.06.06A1.7 1.7 0 0 0 8.6 4.2a1.7 1.7 0 0 0 1-.6 1.7 1.7 0 0 0 .4-1.1v-.1h4v.1a1.7 1.7 0 0 0 1 1.7 1.7 1.7 0 0 0 1.88-.34l.06-.06 2.86 2.86-.06.06A1.7 1.7 0 0 0 19.4 8.6a1.7 1.7 0 0 0 .6 1 1.7 1.7 0 0 0 1.1.4h.1v4h-.1a1.7 1.7 0 0 0-1.7 1Z" />
    </>
  ),
  diagnostics: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 10v6M12 7h.01" />
    </>
  ),
  about: (
    <>
      <path d="M5 3.5h10.5L19 7v13.5H5z" />
      <path d="M15.5 3.5V7H19M8.5 11h7M8.5 14h7M8.5 17h4" />
    </>
  ),
  power: (
    <>
      <path d="M12 2.75v9" />
      <path d="M6.2 6.2a8 8 0 1 0 11.6 0" />
    </>
  ),
  refresh: (
    <>
      <path d="M20 11a8 8 0 0 0-14.9-4L3 10" />
      <path d="M3 4v6h6M4 13a8 8 0 0 0 14.9 4L21 14" />
      <path d="M21 20v-6h-6" />
    </>
  ),
  reconnect: (
    <>
      <path d="M19 8a8 8 0 0 0-13.5-2L3 9" />
      <path d="M3 4v5h5M5 16a8 8 0 0 0 13.5 2l2.5-3" />
      <path d="M21 20v-5h-5" />
    </>
  ),
  window: (
    <>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M3 8h18M6 6h.01M9 6h.01" />
    </>
  ),
  moon: <path d="M20 15.2A8.5 8.5 0 0 1 8.8 4a8.5 8.5 0 1 0 11.2 11.2Z" />,
  sun: (
    <>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.42 1.42M17.65 17.65l1.42 1.42M2 12h2M20 12h2M4.93 19.07l1.42-1.42M17.65 6.35l1.42-1.42" />
    </>
  ),
  system: (
    <>
      <rect x="3" y="4" width="18" height="14" rx="2" />
      <path d="M8 22h8M12 18v4" />
    </>
  ),
  check: <path d="m4 12 5 5L20 6" />,
  warning: (
    <>
      <path d="M10.3 3.7 2.4 18a2 2 0 0 0 1.75 3h15.7a2 2 0 0 0 1.75-3L13.7 3.7a2 2 0 0 0-3.4 0Z" />
      <path d="M12 9v4M12 17h.01" />
    </>
  ),
  close: <path d="m5 5 14 14M19 5 5 19" />,
  menu: <path d="M4 7h16M4 12h16M4 17h16" />,
  chevron: <path d="m9 5 7 7-7 7" />,
  bell: (
    <>
      <path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9Z" />
      <path d="M10 21h4" />
    </>
  ),
  sound: (
    <>
      <path d="M5 10v4h3l4 3V7l-4 3H5Z" />
      <path d="M15 9.2a4 4 0 0 1 0 5.6M17.8 6.4a8 8 0 0 1 0 11.2" />
    </>
  ),
  upload: (
    <>
      <path d="M12 16V4M7.5 8.5 12 4l4.5 4.5" />
      <path d="M4 15v4.5h16V15" />
    </>
  ),
  keyboard: (
    <>
      <rect x="2.5" y="5.5" width="19" height="13" rx="2.25" />
      <path d="M6 9h.01M9 9h.01M12 9h.01M15 9h.01M18 9h.01M6 12.5h.01M9 12.5h.01M12 12.5h.01M15 12.5h3M7.5 16h9" />
    </>
  ),
  terminal: (
    <>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="m7 9 3 3-3 3M12.5 16h4.5" />
    </>
  ),
  network: (
    <>
      <path d="M4.9 9a10 10 0 0 1 14.2 0M7.75 11.85a6 6 0 0 1 8.5 0M10.6 14.7a2 2 0 0 1 2.8 0" />
      <circle cx="12" cy="18" r=".75" />
    </>
  ),
  info: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v6M12 7h.01" />
    </>
  )
};

export function Icon({
  name,
  size = 20,
  label,
  class: className
}: {
  name: IconName;
  size?: number;
  label?: string;
  class?: string;
}) {
  return (
    <svg
      class={className}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.75"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden={label ? undefined : "true"}
      aria-label={label}
      role={label ? "img" : undefined}
    >
      {paths[name]}
    </svg>
  );
}
