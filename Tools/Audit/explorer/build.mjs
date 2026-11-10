// Scalar offline bundling adapted from atomicdeploy/patris-export PR #239,
// ce9ac11e3dc6d0ebdde7cfbc1319be86c6470f7c, scripts/docs/api-docs-lib.cjs.
// Copyright (c) 2025 Atomic Deploy. MIT licensed; see THIRD_PARTY_NOTICES.md.
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { loadProjectEnv } from "../../Build/env.mjs";
loadProjectEnv();
const here = dirname(fileURLToPath(import.meta.url));
const destination = resolve(here, "../../Controller/api/assets/scalar.js");
const source = readFileSync(
  resolve(
    here,
    "node_modules/@scalar/api-reference/dist/browser/standalone.js",
  ),
  "utf8",
);
const bootstrap = `
;(() => {
  const content=JSON.parse(document.getElementById('pccontroller-openapi').textContent);
  // Set only an origin, never a URL containing credentials or query secrets.
  if (location.protocol==='http:' || location.protocol==='https:') content.servers=[{url:location.origin}];
  for (const path of Object.values(content.paths)) for (const operation of Object.values(path)) {
    for (const sample of operation['x-codeSamples'] || []) sample.source=sample.source.replaceAll('<HOST_ORIGIN>', content.servers[0].url);
  }
  window.Scalar.createApiReference('#app', {
    content,
    agent: { disabled: true, hideAddApi: true },
    mcp: { disabled: true },
    defaultHttpClient: { targetKey: 'js', clientKey: 'fetch' },
    hideClientButton: false, hideDownloadButton: false, hideModels: false,
    hideTestRequestButton: true,
    layout: 'modern', orderRequiredPropertiesFirst: true, persistAuth: false,
    proxyUrl: '', showDeveloperTools: 'never', showOperationId: false,
    telemetry: false, theme: 'kepler', withDefaultFonts: false,
    customCss: 'html .light-mode { --scalar-color-accent: #7855c6 !important; --scalar-background-1: #f9f8fc !important; --scalar-background-2: #ffffff !important; --scalar-background-3: #f0eafa !important; } html .dark-mode { --scalar-color-accent: #bc9df6 !important; --scalar-background-1: #131117 !important; --scalar-background-2: #1b1820 !important; --scalar-background-3: #27222e !important; --scalar-sidebar-background-1: #1b1820 !important; }'
  });
})();
`;
const output = source + "\n" + bootstrap;
if (process.argv.includes("--check")) {
  if (
    readFileSync(destination, "utf8").replaceAll("\r\n", "\n") !==
    output.replaceAll("\r\n", "\n")
  )
    throw new Error(
      "Scalar explorer bundle is stale; run npm --prefix Tools/Audit/explorer run build",
    );
} else {
  mkdirSync(dirname(destination), { recursive: true });
  writeFileSync(destination, output);
}
