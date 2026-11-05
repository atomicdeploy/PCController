import type { ManifestOptions } from "vite-plugin-pwa";

// Keep every URL relative: the same production bundle is served below /app/
// by the native coordinator and from an arbitrary folder in the static ZIP.
export const controllerManifest = {
  id: "./",
  name: "PCController",
  short_name: "PCController",
  description: "A private local control surface for PCController.",
  start_url: "./",
  scope: "./",
  display: "standalone",
  orientation: "any",
  background_color: "#07111c",
  theme_color: "#0b1724",
  dir: "ltr",
  lang: "en",
  categories: ["utilities", "productivity"],
  prefer_related_applications: false,
  icons: [
    {
      src: "icons/app-192.png",
      sizes: "192x192",
      type: "image/png",
      purpose: "any"
    },
    {
      src: "shared/icons/app.png",
      sizes: "512x512",
      type: "image/png",
      purpose: "any"
    },
    {
      src: "icons/app-maskable-512.png",
      sizes: "512x512",
      type: "image/png",
      purpose: "maskable"
    }
  ]
} satisfies Partial<ManifestOptions>;
