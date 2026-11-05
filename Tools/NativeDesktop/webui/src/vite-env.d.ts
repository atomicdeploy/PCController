/// <reference types="vite/client" />
/// <reference types="vite-plugin-pwa/client" />

declare module "virtual:rayanlamp-design.css";

interface ImportMetaEnv {
  readonly VITE_RAYANLAMP_STATIC_EXPORT?: string;
  readonly VITE_RAYANLAMP_BUILD_EDITION?: "develop" | "production";
  readonly VITE_RAYANLAMP_DEVELOPER_TOOLS?: "true" | "false";
}
