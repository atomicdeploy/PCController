/// <reference types="vite/client" />
/// <reference types="vite-plugin-pwa/client" />

declare module "virtual:pccontroller-desktop-design.css";

interface ImportMetaEnv {
  readonly VITE_PCCONTROLLER_DESKTOP_STATIC_EXPORT?: string;
  readonly VITE_PCCONTROLLER_DESKTOP_BUILD_EDITION?: "develop" | "production";
  readonly VITE_PCCONTROLLER_DESKTOP_DEVELOPER_TOOLS?: "true" | "false";
}
