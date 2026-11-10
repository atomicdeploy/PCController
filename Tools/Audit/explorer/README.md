# Offline PCController API explorer

The Scalar browser setup is adapted from `atomicdeploy/patris-export` PR #239,
commit `ce9ac11e3dc6d0ebdde7cfbc1319be86c6470f7c`, particularly
`scripts/docs/api-docs-lib.cjs::buildScalarAssets`. Patris uses Redocly for
validation and **Scalar 1.63.0** for the rendered reference. Its local/offline,
modern Kepler layout, generated clients, search, schema browsing, no agent,
no telemetry, no credential persistence and disabled request console are
preserved. PCController supplies its own branding, contracts and RPC/event pages.
See the notices shipped in `Tools/Controller/api/THIRD_PARTY_NOTICES.md`.

```console
npm ci --ignore-scripts --prefix Tools/Audit/explorer
npm run build --prefix Tools/Audit/explorer
node Tools/Audit/generate-api-reference.mjs
node Tools/Audit/generate-api-reference.mjs --check
npm run check --prefix Tools/Audit/explorer
node --test Tools/Audit/generate-api-reference.test.mjs
```

Open `/api/docs/` on the existing host listener, or open
`Tools/Controller/api/reference.html` directly. All runtime assets and contracts
are local and embedded in the host executable. The Web UI Preferences links to
the currently selected controller origin, including standalone Web UI targets.
No CDN, AI service, live-device query, mutation console or proxy is required.

The existing generator remains the contract owner and checks dispatched RPC
method coverage and registered HTTP route families. The small Go AST helper
extracts literal request JSON fields and types without importing or executing
the controller. It is compiled only to a stable product-owned path on Windows.
Extracted fields are **not** assertions of semantic requiredness, ranges or
cross-field admission; the explicit JSON Schemas remain authoritative for those.
Request-field templates must not be advertised as validated actuation requests.

Changes to either contracts or embedded assets invalidate host-package and
stable Go-test identities. The committed bundle is compared with the pinned
npm browser asset in CI; no build-time date, private host identity, token or
live customer data belongs in generated documentation.
