import { defineConfig, type ProxyOptions } from "vite";
import preact from "@preact/preset-vite";
import { VitePWA } from "vite-plugin-pwa";
import { readFile } from "node:fs/promises";
import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";
import path from "node:path";
import designTokens from "../internal/design/tokens.json";
import { rayanLampManifest } from "./manifest";
import {
  coordinatorExactProxyPattern,
  coordinatorFamilyProxyPattern
} from "./src/coordinator-routes";

const virtualDesignCSS = "virtual:rayanlamp-design.css";
const resolvedDesignCSS = `\0${virtualDesignCSS}`;

function cssTokenName(path: string[]): string {
  return `--rl-${path.join("-").replaceAll("_", "-")}`;
}

function designTokenCSS(): string {
  const declarations: string[] = [];
  const visit = (value: unknown, path: string[]) => {
    if (value && typeof value === "object") {
      for (const [key, child] of Object.entries(value)) {
        visit(child, [...path, key]);
      }
      return;
    }
    let formatted = String(value);
    const leaf = path.at(-1) ?? "";
    if (typeof value === "number" && leaf.endsWith("_px")) {
      formatted = `${value}px`;
    } else if (typeof value === "number" && leaf.endsWith("_ms")) {
      formatted = `${value}ms`;
    }
    declarations.push(`  ${cssTokenName(path)}: ${formatted};`);
  };
  visit(designTokens, []);
  return `:root {\n${declarations.join("\n")}\n}\n`;
}

const configuredOutput = process.env.RAYANLAMP_WEB_OUT_DIR?.trim();
const staticExport = process.env.VITE_RAYANLAMP_STATIC_EXPORT === "true";
const canonicalDefaultEdition = readFileSync(
  fileURLToPath(new URL("../build-edition.txt", import.meta.url)),
  "utf8"
).trim();
const configuredEdition =
  process.env.VITE_RAYANLAMP_BUILD_EDITION?.trim().toLowerCase() ||
  (staticExport ? "production" : canonicalDefaultEdition);
if (configuredEdition !== "develop" && configuredEdition !== "production") {
  throw new Error(
    `VITE_RAYANLAMP_BUILD_EDITION must be develop or production; got ${JSON.stringify(configuredEdition)}`
  );
}
const buildEdition = configuredEdition as "develop" | "production";
const developerTools = buildEdition === "develop";
const sharedSFXDirectory = fileURLToPath(
  new URL("../assets/sfx/", import.meta.url)
);
const canonicalAppIcon = fileURLToPath(
  new URL("../assets/icons/app.png", import.meta.url)
);
const canonicalWebFont = fileURLToPath(
  new URL("../assets/fonts/Vazirmatn-Regular.ttf", import.meta.url)
);
const sharedSFXCues = ["on", "off", "message", "error"] as const;
const sharedSFXManifestEntries = sharedSFXCues.map((cue) => {
  const data = readFileSync(path.join(sharedSFXDirectory, `${cue}.wav`));
  return {
    url: `shared/sfx/${cue}.wav`,
    revision: createHash("sha256").update(data).digest("hex")
  };
});
const sharedAssetManifestEntries = [
  ...sharedSFXManifestEntries,
  {
    url: "shared/icons/app.png",
    revision: createHash("sha256")
      .update(readFileSync(canonicalAppIcon))
      .digest("hex")
  },
  {
    url: "shared/fonts/Vazirmatn-Regular.ttf",
    revision: createHash("sha256")
      .update(readFileSync(canonicalWebFont))
      .digest("hex")
  }
];

function coordinatorProxy(): ProxyOptions {
  return {
    target: "http://127.0.0.1:47831",
    changeOrigin: true,
    ws: true,
    configure(proxy) {
      const rewriteOrigin = (request: {
        setHeader(name: string, value: string): void;
      }) => {
        request.setHeader("Origin", "http://127.0.0.1:47831");
      };
      proxy.on("proxyReq", rewriteOrigin);
      proxy.on("proxyReqWs", rewriteOrigin);
    }
  };
}

export default defineConfig({
  // Relative asset URLs make the compiled dashboard valid both at the Go
  // manager's /app/ prefix and at any directory used by the static export.
  base: "./",
  plugins: [
    {
      name: "rayanlamp-design-tokens",
      resolveId(id) {
        return id === virtualDesignCSS ? resolvedDesignCSS : undefined;
      },
      load(id) {
        return id === resolvedDesignCSS ? designTokenCSS() : undefined;
      }
    },
    {
      name: "rayanlamp-build-edition",
      generateBundle() {
        this.emitFile({
          type: "asset",
          fileName: "build-edition.json",
          source: `${JSON.stringify({
            edition: buildEdition,
            developer_tools: developerTools
          })}\n`
        });
      }
    },
    {
      name: "rayanlamp-shared-assets",
      generateBundle() {
        if (!staticExport) return;
        this.emitFile({
          type: "asset",
          fileName: "shared/fonts/Vazirmatn-Regular.ttf",
          source: readFileSync(canonicalWebFont)
        });
      },
      configureServer(server) {
        server.middlewares.use(async (request, response, next) => {
          const pathname = new URL(
            request.url ?? "/",
            "http://127.0.0.1"
          ).pathname;
          if (pathname === "/shared/icons/app.png") {
            try {
              response.statusCode = 200;
              response.setHeader("Content-Type", "image/png");
              response.setHeader("Cache-Control", "no-store");
              response.end(await readFile(canonicalAppIcon));
            } catch (error) {
              next(error as Error);
            }
            return;
          }
          if (pathname === "/shared/fonts/Vazirmatn-Regular.ttf") {
            try {
              response.statusCode = 200;
              response.setHeader("Content-Type", "font/ttf");
              response.setHeader("Cache-Control", "no-store");
              response.end(await readFile(canonicalWebFont));
            } catch (error) {
              next(error as Error);
            }
            return;
          }
          const match = /^\/shared\/sfx\/(on|off|message|error)\.wav$/.exec(
            pathname
          );
          if (!match) {
            next();
            return;
          }
          try {
            const data = await readFile(
              path.join(sharedSFXDirectory, `${match[1]}.wav`)
            );
            response.statusCode = 200;
            response.setHeader("Content-Type", "audio/wav");
            response.setHeader("Cache-Control", "no-store");
            response.end(data);
          } catch (error) {
            next(error as Error);
          }
        });
      }
    },
    preact(),
    VitePWA({
      registerType: "prompt",
      injectRegister: false,
      strategies: "generateSW",
      filename: "sw.js",
      scope: "./",
      manifest: rayanLampManifest,
      workbox: {
        cleanupOutdatedCaches: true,
        clientsClaim: false,
        skipWaiting: false,
        navigateFallback: "index.html",
        globPatterns: ["**/*.{html,js,css,svg,png,wav,ttf,webmanifest}"],
        // vite-plugin-pwa adds the generated manifest and its declared icons
        // itself. Exclude those same physical files from Workbox's glob pass
        // so the precache contains one identity for each asset.
        globIgnores: [
          "manifest.webmanifest",
          "icons/app-192.png",
          "icons/app-maskable-512.png",
          "shared/fonts/Vazirmatn-Regular.ttf"
        ],
        additionalManifestEntries: sharedAssetManifestEntries,
        runtimeCaching: [
          {
            urlPattern: ({ url }) => {
              const exact = [
                "/status",
                "/reconnect",
                "/diagnostics",
                "/config",
                "/notifications",
                "/version",
                "/events",
                "/healthz",
                "/app/session"
              ];
              const prefixes = [
                "/lamp/",
                "/window/",
                "/terminal/",
                "/ui/",
                "/audio/"
              ];
              return (
                exact.includes(url.pathname) ||
                prefixes.some((prefix) => url.pathname.startsWith(prefix))
              );
            },
            handler: "NetworkOnly"
          }
        ]
      }
    })
  ],
  define: {
    "import.meta.env.VITE_RAYANLAMP_BUILD_EDITION":
      JSON.stringify(buildEdition),
    "import.meta.env.VITE_RAYANLAMP_DEVELOPER_TOOLS":
      JSON.stringify(developerTools ? "true" : "false")
  },
  build: {
    outDir: configuredOutput
      ? path.resolve(configuredOutput)
      : "../internal/webui/dist",
    emptyOutDir: true,
    sourcemap: developerTools,
    minify: developerTools ? false : "oxc",
    cssMinify: !developerTools,
    target: "es2022",
    cssCodeSplit: true,
    reportCompressedSize: true
  },
  server: {
    port: 41731,
    strictPort: true,
    proxy: {
      [coordinatorExactProxyPattern]: coordinatorProxy(),
      [coordinatorFamilyProxyPattern]: coordinatorProxy(),
      "/app/session": {
        target: "http://127.0.0.1:47831",
        changeOrigin: true,
        configure(proxy) {
          proxy.on("proxyReq", (request) => {
            request.setHeader("Origin", "http://127.0.0.1:47831");
          });
        }
      }
    }
  }
});
