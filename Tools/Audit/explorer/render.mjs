import { readFileSync } from "node:fs";
const html = (value) =>
  String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
const json = (value) =>
  JSON.stringify(value)
    .replaceAll("<", "\\u003c")
    .replaceAll(">", "\\u003e")
    .replaceAll("&", "\\u0026");
export function renderExplorer({
  product,
  openapi,
  rpcSchema,
  asyncapi,
  digest,
}) {
  const nav = `<nav aria-label="API protocols"><a href="reference.html">HTTP</a><a href="rpc.html">JSON-RPC</a><a href="protocols.html">Events &amp; streams</a><a href="openapi.json" download>OpenAPI ↗</a><a href="jsonrpc.schema.json" download>RPC schema ↗</a></nav>`;
  const page = (title, body) =>
    `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light dark"><title>${html(title)}</title><link rel="icon" href="assets/icon.svg"><link rel="stylesheet" href="assets/explorer.css"></head><body><header class="topbar"><a class="identity" href="reference.html"><img src="assets/icon.svg" alt="" width="32" height="32"><strong>${html(product.productName)}</strong><span>API explorer</span></a>${nav}</header>${body}<footer>Contract ${html(digest.slice(0, 12))} · <a href="THIRD_PARTY_NOTICES.md">Licenses</a></footer></body></html>\n`;
  const reference = page(
    product.referenceTitle,
    `<h1 class="sr-only">${html(product.referenceHeading)}</h1><div id="app"></div><script id="pccontroller-openapi" type="application/json">${json(openapi)}</script><script src="assets/scalar.js"></script>`,
  );
  const rpc = page(
    `${product.productName} JSON-RPC`,
    `<main class="catalog" data-mode="rpc"><aside><h1>JSON-RPC</h1><label class="search"><span>⌕</span><input id="search" type="search" placeholder="Search methods, capabilities…" aria-label="Search RPC methods"></label><div class="filters"><select id="capability" aria-label="Filter capability"><option value="">All capabilities</option></select><span id="count"></span></div><div id="operations" role="navigation" aria-label="RPC methods"></div></aside><article id="detail" tabindex="-1"></article></main><script id="catalog" type="application/json">${json(rpcSchema)}</script><script src="assets/catalog.js"></script>`,
  );
  const protocols = page(
    `${product.productName} events and streams`,
    `<main class="catalog" data-mode="streams"><aside><h1>Events &amp; streams</h1><label class="search"><span>⌕</span><input id="search" type="search" placeholder="Search channels, messages…" aria-label="Search stream messages"></label><div class="filters"><select id="capability" aria-label="Filter channel"><option value="">All channels</option></select><span id="count"></span></div><div id="operations" role="navigation" aria-label="Stream messages"></div></aside><article id="detail" tabindex="-1"></article></main><script id="catalog" type="application/json">${json(asyncapi)}</script><script src="assets/catalog.js"></script>`,
  );
  return new Map([
    ["reference.html", reference],
    ["rpc.html", rpc],
    ["protocols.html", protocols],
    [
      "assets/explorer.css",
      readFileSync(new URL("./explorer.css", import.meta.url), "utf8"),
    ],
    [
      "assets/catalog.js",
      readFileSync(new URL("./catalog.js", import.meta.url), "utf8"),
    ],
  ]);
}
