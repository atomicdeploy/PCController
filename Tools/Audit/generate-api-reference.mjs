#!/usr/bin/env node

import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const outputDirectory = resolve(root, "Tools", "Controller", "api");
const checkOnly = process.argv.includes("--check");

const capabilityGroups = {
  events: [
    "controller.event.next", "controller.event.latest", "controller.subscribe", "controller.unsubscribe",
  ],
  programming: [
    "controller.artifact.fetch", "controller.artifact.upload", "controller.artifact.capture",
    "controller.update.firmware", "controller.restore.flash", "controller.update.eeprom",
    "controller.update.host", "controller.discovery.stage",
  ],
  connection_control: [
    "controller.connect", "controller.open", "controller.port.open", "controller.close", "controller.port.close",
  ],
  reset: ["controller.reset.lines", "controller.reset", "controller.port.reset"],
  shutdown: ["controller.quit", "controller.exit"],
  messages: ["controller.message.send"],
  host_configuration: [
    "controller.host_menu.configure", "controller.host_menu.config.set", "controller.ui.config.set",
    "controller.peripherals.set", "controller.hotkeys.set", "controller.os.configure",
    "controller.lcd.presentation.configure", "controller.app.page",
  ],
  virtual_keys: ["controller.os.key", "controller.virtual_key"],
  power_actions: ["controller.os.power"],
  bridge_calls: ["controller.bridge.call"],
  integrations: [
    "controller.device.status", "controller.device.action", "controller.device.inspect",
    "controller.integrations.local.get", "controller.integrations.local.set",
  ],
  read: [
    "controller.artifact.manifest", "controller.artifact.list", "controller.update.status",
    "controller.discovery.github.workflow", "controller.discovery.github.release",
    "controller.discovery.manifest", "controller.discovery.local_manifest", "controller.discovery.check",
    "controller.discovery.status", "controller.host_menu.config", "controller.host_menu.config.get",
    "controller.ui.config", "controller.ui.config.get", "controller.peripherals", "controller.peripherals.get",
    "controller.os.policy", "controller.os.facts.catalog", "controller.host.facts.catalog",
    "controller.hotkeys.get", "controller.bridge.list", "controller.ping", "controller.snapshot",
    "controller.session.snapshot", "controller.session.snapshot.last", "controller.status",
    "controller.front_panel", "controller.front-panel", "controller.command.catalog",
    "controller.program_state.get", "controller.program-state.get", "controller.temperatures",
    "controller.menu.list", "controller.menu.current", "controller.menu.layout.get",
    "controller.host_menu.state", "controller.rf.list", "controller.rf.presentation",
    "controller.rf.learn.status", "controller.history.status", "controller.history.timeline",
    "controller.lcd.presentation.status", "controller.ports", "controller.os.status",
    "controller.system.status", "controller.os.facts", "controller.host.facts",
    "controller.discovery.scan", "controller.pwm.values",
  ],
  board_commands: [
    "controller.program_state.set", "controller.program-state.set", "controller.menu.layout.set",
    "controller.host_menu.directory.replace", "controller.host_menu.content.push", "controller.menu.jump",
    "controller.menu.page", "controller.pwm.set", "controller.pwm.off", "controller.rf.learn.start",
    "controller.rf.learn.cancel", "controller.rf.map", "controller.rf.remove", "controller.rf.clear",
    "controller.rf.transmit", "controller.lcd.prompt", "controller.lcd.priority",
  ],
  dynamic: ["controller.command.execute", "controller.app.action"],
};

const methodOverrides = {
  "controller.ping": "Return service health and protocol versions.",
  "controller.snapshot": "Return the authoritative cached controller snapshot.",
  "controller.command.execute": "Run a shared command after semantic capability classification.",
  "controller.command.catalog": "Return the machine-readable shared command catalog.",
  "controller.event.next": "Long-poll the next retained event after an event ID.",
  "controller.rf.map": "Replace one learned RF mapping and return board readback.",
  "controller.rf.transmit": "Transmit one validated RF waveform request.",
  "controller.restore.flash": "Restore a captured flash backup through the guarded restore path.",
  "controller.subscribe": "Subscribe this WebSocket connection to events and/or status.",
  "controller.unsubscribe": "Remove this WebSocket connection's active subscriptions.",
};

const nonIdempotentMethods = new Set([
  "controller.reset.lines", "controller.reset", "controller.port.reset", "controller.command.execute",
  "controller.rf.learn.start", "controller.rf.transmit", "controller.lcd.prompt", "controller.lcd.priority",
  "controller.message.send", "controller.bridge.call", "controller.os.key", "controller.os.power",
  "controller.device.action", "controller.app.action", "controller.artifact.fetch",
  "controller.artifact.capture", "controller.update.firmware", "controller.restore.flash",
  "controller.update.eeprom", "controller.update.host", "controller.discovery.stage",
]);

const safeCapabilities = new Set(["read", "events"]);
const methods = [];
for (const [capability, names] of Object.entries(capabilityGroups)) {
  for (const name of names) {
    methods.push({
      name,
      capability,
      summary: methodOverrides[name] ?? name.replace(/^controller\./u, "").replaceAll(/[_.-]+/gu, " "),
      idempotency: safeCapabilities.has(capability)
        ? "safe"
        : nonIdempotentMethods.has(name)
          ? "non-idempotent"
          : "idempotent-with-authoritative-readback",
    });
  }
}
methods.sort((left, right) => left.name.localeCompare(right.name));
if (new Set(methods.map(({ name }) => name)).size !== methods.length) {
  throw new Error("JSON-RPC catalog contains a duplicate method name");
}

const routes = [
  { path: "/healthz", methods: ["get"], public: true, capability: "public", summary: "Service liveness and API identity" },
  { path: "/api/v1/ui-config", methods: ["get"], public: true, capability: "public", summary: "Non-secret browser bootstrap" },
  { path: "/api/v1/rpc", methods: ["post"], capability: "dynamic", summary: "JSON-RPC 2.0 request" },
  { path: "/api/v1/snapshot", methods: ["get"], capability: "read", summary: "Authoritative cached controller snapshot" },
  { path: "/api/v1/peripherals", methods: ["get"], capability: "read", summary: "Peripheral descriptors and host-owned names" },
  { path: "/api/v1/peripherals", methods: ["put"], capability: "host_configuration", summary: "Replace host-owned peripheral names" },
  { path: "/api/v1/pwm", methods: ["get"], capability: "read", summary: "Authoritative PWM state" },
  { path: "/api/v1/pwm", methods: ["put", "delete"], capability: "board_commands", summary: "Mutate PWM state with board readback" },
  { path: "/api/v1/commands", methods: ["get"], capability: "read", summary: "Shared command catalog" },
  { path: "/api/v1/program-state", methods: ["get"], capability: "read", summary: "Host-owned program state" },
  { path: "/api/v1/program-state", methods: ["put", "post"], capability: "board_commands", summary: "Update host-owned program state" },
  { path: "/api/v1/menu/catalog", methods: ["get"], capability: "read", summary: "Live board menu catalog" },
  { path: "/api/v1/menu/layout", methods: ["get"], capability: "read", summary: "Live board menu layout" },
  { path: "/api/v1/menu/layout", methods: ["put", "post"], capability: "board_commands", summary: "Persist a validated board menu layout" },
  { path: "/api/v1/host-menus", methods: ["get"], capability: "read", summary: "Host-presented menu directory" },
  { path: "/api/v1/host-menus", methods: ["put", "post"], capability: "host_configuration", summary: "Replace the host-presented menu directory" },
  { path: "/api/v1/os/status", methods: ["get"], capability: "read", summary: "Host status and policy" },
  { path: "/api/v1/os/facts", methods: ["get"], capability: "read", summary: "Bounded read-only host facts" },
  { path: "/api/v1/os/key", methods: ["post"], capability: "virtual_keys", summary: "Validated virtual-key action" },
  { path: "/api/v1/os/power", methods: ["post"], capability: "power_actions", summary: "Confirmed system or display action" },
  { path: "/api/v1/command", methods: ["post"], capability: "dynamic", summary: "Shared command dispatcher" },
  { path: "/api/v1/messages", methods: ["post"], capability: "messages", summary: "Typed source-tagged message" },
  { path: "/api/v1/bridges", methods: ["get"], capability: "read", summary: "Configured bridge state" },
  { path: "/api/v1/bridges/call", methods: ["post"], capability: "bridge_calls", summary: "Correlated bridge call" },
  { path: "/api/v1/artifacts/manifest", methods: ["get"], capability: "read", summary: "Artifact/default/current manifest" },
  { path: "/api/v1/artifacts", methods: ["get"], capability: "read", summary: "Content-addressed artifact catalog" },
  { path: "/api/v1/artifacts/upload", methods: ["post"], capability: "programming", summary: "Import a verified artifact" },
  { path: "/api/v1/artifacts/fetch", methods: ["post"], capability: "programming", summary: "Queue a bounded remote artifact fetch" },
  { path: "/api/v1/artifacts/capture", methods: ["post"], capability: "programming", summary: "Capture flash or EEPROM explicitly" },
  { path: "/api/v1/artifacts/{kind}/{sha256}", methods: ["get", "head"], capability: "read", summary: "Immutable ranged artifact download" },
  { path: "/api/v1/artifacts/current/{kind}", methods: ["get", "head"], capability: "read", summary: "Latest explicitly captured artifact" },
  { path: "/api/v1/updates/{kind}", methods: ["post"], capability: "programming", summary: "Queue an explicitly authorized update" },
  { path: "/api/v1/restores/flash", methods: ["post"], capability: "programming", summary: "Queue an explicitly authorized flash restore" },
  { path: "/api/v1/updates/status/{id}", methods: ["get"], capability: "read", summary: "Update operation progress" },
  { path: "/api/v1/discovery/github/workflow", methods: ["post"], capability: "read", summary: "Discover workflow artifacts" },
  { path: "/api/v1/discovery/github/release", methods: ["post"], capability: "read", summary: "Discover release assets" },
  { path: "/api/v1/discovery/manifest", methods: ["get", "post"], capability: "read", summary: "Serve or validate an update manifest" },
  { path: "/api/v1/discovery/check", methods: ["post"], capability: "read", summary: "Compare candidate and installed identities" },
  { path: "/api/v1/discovery/stage", methods: ["post"], capability: "programming", summary: "Stage a verified candidate without programming" },
  { path: "/api/v1/discovery/status/{id}", methods: ["get"], capability: "read", summary: "Discovery or staging progress" },
  { path: "/api/v1/webhooks/inbound", methods: ["post"], capability: "messages", summary: "Optional inbound typed webhook" },
  { path: "/api/v1/integrations/datahub/{path}", methods: ["get", "head", "post", "put", "patch", "delete"], capability: "integrations", summary: "Sanitized loopback data-service proxy" },
  { path: "/api/v1/integrations/device/{path}", methods: ["get", "head", "post", "put", "patch", "delete"], capability: "integrations", summary: "Fail-closed device route; use typed RPC" },
];

function operationFor(route, method) {
  const operation = {
    operationId: `${method}_${route.path.replaceAll(/[^a-zA-Z0-9]+/gu, "_").replaceAll(/^_|_$/gu, "")}`,
    summary: route.summary,
    tags: [route.path.split("/").filter(Boolean)[2] ?? "service"],
    "x-required-capability": route.capability,
    "x-idempotency": ["get", "head"].includes(method) ? "safe" : "see operation semantics and idempotency keys",
    responses: {
      "200": { description: "Accepted response", content: { "application/json": { schema: { type: "object", additionalProperties: true } } } },
      "400": { $ref: "#/components/responses/BadRequest" },
      "401": { $ref: "#/components/responses/Unauthorized" },
      "403": { $ref: "#/components/responses/Forbidden" },
      "405": { $ref: "#/components/responses/MethodNotAllowed" },
    },
  };
  if (route.public) operation.security = [];
  if (!["get", "head", "delete"].includes(method)) {
    operation.requestBody = {
      required: true,
      content: {
        "application/json": { schema: route.path === "/api/v1/rpc" ? { $ref: "#/components/schemas/JSONRPCRequest" } : { type: "object", additionalProperties: true } },
      },
    };
  }
  if (route.path.includes("{kind}")) {
    operation.parameters = [{ name: "kind", in: "path", required: true, schema: { type: "string", minLength: 1 } }];
  }
  if (route.path.includes("{sha256}")) {
    operation.parameters ??= [];
    operation.parameters.push({ name: "sha256", in: "path", required: true, schema: { type: "string", pattern: "^[a-fA-F0-9]{64}$" } });
  }
  if (route.path.includes("{id}")) {
    operation.parameters ??= [];
    operation.parameters.push({ name: "id", in: "path", required: true, schema: { type: "string", minLength: 1 } });
  }
  if (route.path.includes("{path}")) {
    operation.parameters ??= [];
    operation.parameters.push({ name: "path", in: "path", required: true, schema: { type: "string", minLength: 1 } });
  }
  return operation;
}

const openAPIPaths = {};
for (const route of routes) {
  openAPIPaths[route.path] ??= {};
  for (const method of route.methods) openAPIPaths[route.path][method] = operationFor(route, method);
}

const errorSchema = {
  type: "object",
  required: ["error"],
  properties: { error: { type: "string" }, correlation_id: { type: "string" } },
  additionalProperties: true,
};

const openapi = {
  openapi: "3.1.0",
  info: {
    title: "PCController HTTP API",
    version: "1",
    summary: "Versioned REST and JSON-RPC surface of the primary controller host.",
    description: "Loopback is the safe default. Remote requests require authentication and an explicit capability. The built-in listener does not terminate TLS.",
  },
  servers: [{ url: "http://127.0.0.1:8787", description: "Default loopback primary" }],
  security: [{ bearerAuth: [] }, { tokenHeader: [] }],
  paths: openAPIPaths,
  components: {
    securitySchemes: {
      bearerAuth: { type: "http", scheme: "bearer", description: "Host access credential. Never place durable credentials in URLs." },
      tokenHeader: { type: "apiKey", in: "header", name: "X-PCController-Token", description: "Header-only compatibility credential." },
    },
    schemas: {
      JSONRPCID: { oneOf: [{ type: "string" }, { type: "integer" }, { type: "null" }] },
      JSONRPCRequest: {
        type: "object", required: ["jsonrpc", "method"], additionalProperties: false,
        properties: {
          jsonrpc: { const: "2.0" }, id: { $ref: "#/components/schemas/JSONRPCID" },
          method: { type: "string", enum: methods.map(({ name }) => name) },
          params: { type: ["object", "array", "null"] },
          auth: { type: "string", writeOnly: true, description: "Raw NDJSON compatibility only; prefer authenticated transport setup." },
        },
        examples: [{ jsonrpc: "2.0", id: 1, method: "controller.status", params: {} }],
      },
      JSONRPCError: {
        type: "object", required: ["code", "message"], additionalProperties: false,
        properties: { code: { type: "integer", enum: [-32700, -32600, -32601, -32602, -32003, -32001, -32000] }, message: { type: "string" } },
        examples: [{ code: -32003, message: "remote capability board_commands is disabled" }],
      },
      Error: errorSchema,
    },
    responses: {
      BadRequest: { description: "Invalid request or parameters", content: { "application/json": { schema: errorSchema } } },
      Unauthorized: { description: "Authentication required", content: { "application/json": { schema: errorSchema } } },
      Forbidden: { description: "Capability or safety policy denied", content: { "application/json": { schema: errorSchema } } },
      MethodNotAllowed: { description: "Unsupported method", headers: { Allow: { schema: { type: "string" } } }, content: { "application/json": { schema: errorSchema } } },
    },
  },
  "x-body-limit-bytes": 1048576,
  "x-unsupported-transports": ["versionless /api paths", "built-in TLS termination", "Socket.IO long-polling", "Socket.IO namespaces", "Socket.IO rooms", "binary Socket.IO attachments"],
};

const rpcSchema = {
  $schema: "https://json-schema.org/draft/2020-12/schema",
  $id: "https://pccontroller.local/schemas/jsonrpc-v1.json",
  title: "PCController JSON-RPC 2.0",
  oneOf: [
    { $ref: "#/$defs/request" }, { $ref: "#/$defs/success" }, { $ref: "#/$defs/error" }, { $ref: "#/$defs/notification" },
  ],
  $defs: {
    id: { oneOf: [{ type: "string" }, { type: "integer" }, { type: "null" }] },
    request: {
      type: "object", required: ["jsonrpc", "method"], additionalProperties: false,
      properties: {
        jsonrpc: { const: "2.0" }, id: { $ref: "#/$defs/id" },
        method: { enum: methods.map(({ name }) => name) }, params: { type: ["object", "array", "null"] },
        auth: { type: "string", writeOnly: true },
      },
    },
    success: {
      type: "object", required: ["jsonrpc", "id", "result"], additionalProperties: false,
      properties: { jsonrpc: { const: "2.0" }, id: { $ref: "#/$defs/id" }, result: true },
    },
    error: {
      type: "object", required: ["jsonrpc", "id", "error"], additionalProperties: false,
      properties: {
        jsonrpc: { const: "2.0" }, id: { $ref: "#/$defs/id" },
        error: {
          type: "object", required: ["code", "message"], additionalProperties: false,
          properties: { code: { type: "integer" }, message: { type: "string" } },
        },
      },
    },
    notification: {
      type: "object", required: ["jsonrpc", "method", "params"], additionalProperties: false,
      properties: {
        jsonrpc: { const: "2.0" }, method: { enum: ["controller.event", "controller.status", "controller.error", "controller.data"] }, params: true,
      },
    },
  },
  "x-methods": Object.fromEntries(methods.map(({ name, ...metadata }) => [name, metadata])),
  "x-error-codes": {
    "-32700": "parse error", "-32600": "invalid request", "-32601": "method not found",
    "-32602": "invalid params", "-32001": "authentication required",
    "-32003": "remote capability denied", "-32000": "runtime or device error",
  },
};

const asyncapi = {
  asyncapi: "3.0.0",
  info: {
    title: "PCController event and WebSocket API", version: "1",
    description: "Authenticated full-duplex JSON-RPC, event, status, and Socket.IO-compatible messaging. WebSocket transport is required.",
  },
  servers: {
    loopback: {
      host: "127.0.0.1:8787", protocol: "ws", pathname: "/ipc",
      description: "Default loopback primary. Remote exposure requires explicit origin, authentication, and capability policy.",
      security: [{ $ref: "#/components/securitySchemes/session" }],
    },
  },
  channels: {
    rpc: {
      address: "/ipc",
      messages: {
        request: { $ref: "#/components/messages/JSONRPCRequest" }, response: { $ref: "#/components/messages/JSONRPCResponse" },
        event: { $ref: "#/components/messages/Event" }, status: { $ref: "#/components/messages/Status" }, error: { $ref: "#/components/messages/Error" },
      },
    },
    socketIO: {
      address: "/socket.io/?EIO=4&transport=websocket",
      description: "Bounded Engine.IO v4 / Socket.IO adapter. Long-polling, namespaces, rooms, binary attachments, and acknowledgement callbacks are intentionally unsupported.",
      messages: { packet: { $ref: "#/components/messages/SocketIOPacket" } },
    },
  },
  operations: {
    sendRPC: { action: "send", channel: { $ref: "#/channels/rpc" }, messages: [{ $ref: "#/channels/rpc/messages/request" }] },
    receiveRPC: { action: "receive", channel: { $ref: "#/channels/rpc" }, messages: [
      { $ref: "#/channels/rpc/messages/response" }, { $ref: "#/channels/rpc/messages/event" },
      { $ref: "#/channels/rpc/messages/status" }, { $ref: "#/channels/rpc/messages/error" },
    ] },
    exchangeSocketIO: { action: "send", channel: { $ref: "#/channels/socketIO" }, messages: [{ $ref: "#/channels/socketIO/messages/packet" }] },
  },
  components: {
    securitySchemes: {
      session: { type: "httpApiKey", in: "header", name: "Authorization", description: "Bearer credential or short-lived session authorization. Durable secrets must not appear in URLs." },
    },
    messages: {
      JSONRPCRequest: { payload: { $ref: "./jsonrpc.schema.json#/$defs/request" } },
      JSONRPCResponse: { payload: { oneOf: [{ $ref: "./jsonrpc.schema.json#/$defs/success" }, { $ref: "./jsonrpc.schema.json#/$defs/error" }] } },
      Event: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.event" }, params: { type: "object", additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.event", params: { id: 42, kind: "door", state: "open", source: "board" } }] } },
      Status: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.status" }, params: { type: "object", additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.status", params: { uptime_ms: 1234 } }] } },
      Error: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.error" }, params: { type: "object", additionalProperties: true } } } },
      SocketIOPacket: { payload: { type: "string", description: "Engine.IO v4 text packet carrying subscribe, unsubscribe, message, command, or rpc events." } },
    },
  },
  "x-socket-io-events": {
    incoming: ["subscribe", "unsubscribe", "message", "command", "rpc"],
    outgoing: ["subscribed", "unsubscribed", "message.accepted", "command.response", "rpc.response", "controller.event", "controller.status", "controller.error", "error"],
  },
};

const outputs = new Map([
  ["openapi.json", `${JSON.stringify(openapi, null, 2)}\n`],
  ["asyncapi.json", `${JSON.stringify(asyncapi, null, 2)}\n`],
  ["jsonrpc.schema.json", `${JSON.stringify(rpcSchema, null, 2)}\n`],
]);

const digest = createHash("sha256").update([...outputs.values()].join("\n")).digest("hex");
const escapeHTML = (value) => value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
const routeRows = routes.flatMap((route) => route.methods.map((method) => `<tr><td><code>${method.toUpperCase()}</code></td><td><code>${escapeHTML(route.path)}</code></td><td>${escapeHTML(route.summary)}</td><td><span>${escapeHTML(route.capability)}</span></td></tr>`)).join("");
const methodRows = methods.map((method) => `<tr><td><code>${method.name}</code></td><td>${escapeHTML(method.summary)}</td><td><span>${method.capability}</span></td><td>${method.idempotency}</td></tr>`).join("");
const reference = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark"><title>PCController API Reference</title>
<style>:root{font-family:Inter,Segoe UI,system-ui,sans-serif;color-scheme:light dark;--bg:#f6f7fb;--panel:#fff;--text:#172033;--muted:#647087;--line:#dfe3ec;--accent:#6d4aff}@media(prefers-color-scheme:dark){:root{--bg:#11131a;--panel:#191c25;--text:#edf0f7;--muted:#a8b0c2;--line:#303543;--accent:#a995ff}}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text)}main{width:min(1180px,calc(100% - 32px));margin:auto;padding:48px 0 80px}header{display:grid;gap:12px;margin-bottom:34px}h1{font-size:clamp(2rem,5vw,4rem);letter-spacing:-.05em;margin:0}p{color:var(--muted);max-width:76ch;line-height:1.65}.pills{display:flex;flex-wrap:wrap;gap:8px}.pills a,.pills span,td span{border:1px solid var(--line);border-radius:999px;padding:6px 10px;color:var(--text);text-decoration:none;background:color-mix(in srgb,var(--panel) 88%,var(--accent) 12%)}section{margin-top:34px;background:color-mix(in srgb,var(--panel) 92%,transparent);border:1px solid var(--line);border-radius:20px;overflow:hidden;box-shadow:0 18px 55px color-mix(in srgb,var(--text) 8%,transparent)}section>div{padding:22px 24px 6px}h2{margin:0;font-size:1.25rem}table{border-collapse:collapse;width:100%;font-size:.9rem}th,td{text-align:left;padding:13px 16px;border-top:1px solid var(--line);vertical-align:top}th{color:var(--muted);font-weight:600}code{font-family:Cascadia Code,ui-monospace,monospace;color:var(--accent);overflow-wrap:anywhere}@media(max-width:720px){main{width:min(100% - 20px,1180px);padding-top:26px}section{overflow:auto}table{min-width:760px}}</style></head>
<body><main><header><span>OFFLINE CONTRACT · API v1</span><h1>PCController API</h1><p>The primary host exposes one versioned, safety-gated surface across REST, JSON-RPC, WebSocket, and the bounded Socket.IO adapter. Loopback is the default; every remote operation requires authentication and an explicit capability.</p><div class="pills"><a href="openapi.json">OpenAPI 3.1</a><a href="asyncapi.json">AsyncAPI 3.0</a><a href="jsonrpc.schema.json">JSON-RPC schema</a><span>${methods.length} RPC methods</span><span>${routes.reduce((count, route) => count + route.methods.length, 0)} HTTP operations</span></div></header>
<section><div><h2>HTTP operations</h2><p>Versionless <code>/api/</code> routes are deliberately unsupported. JSON bodies are capped at 1 MiB.</p></div><table><thead><tr><th>Method</th><th>Path</th><th>Purpose</th><th>Capability</th></tr></thead><tbody>${routeRows}</tbody></table></section>
<section><div><h2>JSON-RPC methods</h2><p>Standard JSON-RPC errors are preserved; host extensions use -32001 for authentication, -32003 for capability denial, and -32000 for runtime or device failures.</p></div><table><thead><tr><th>Method</th><th>Purpose</th><th>Capability</th><th>Idempotency</th></tr></thead><tbody>${methodRows}</tbody></table></section>
<p>Contract digest <code>${digest}</code>. Generated by <code>Tools/Audit/generate-api-reference.mjs</code>.</p></main></body></html>\n`;
outputs.set("reference.html", reference);

const dispatcherFiles = [
  "Tools/Controller/internal/ipcjson/ipc.go",
  "Tools/Controller/internal/artifacts/service.go",
  "Tools/Controller/internal/releaseplane/service.go",
];
const dispatched = new Set();
for (const file of dispatcherFiles) {
  const source = readFileSync(resolve(root, file), "utf8");
  for (const match of source.matchAll(/case\s+((?:"controller\.[a-z0-9_.-]+"\s*,?\s*)+):/giu)) {
    for (const literal of match[1].matchAll(/"(controller\.[a-z0-9_.-]+)"/giu)) dispatched.add(literal[1]);
  }
}
const catalogued = new Set(methods.map(({ name }) => name));
const missingMethods = [...dispatched].filter((name) => !catalogued.has(name)).sort();
if (missingMethods.length > 0) throw new Error(`JSON-RPC catalog is missing dispatched methods: ${missingMethods.join(", ")}`);
for (const required of ["controller.subscribe", "controller.unsubscribe"]) {
  if (!catalogued.has(required)) throw new Error(`JSON-RPC catalog is missing WebSocket method ${required}`);
}

const routeSourceFiles = [
  "Tools/Controller/internal/ipcjson/ipc.go",
  "Tools/Controller/internal/ipcjson/artifacts_http.go",
  "Tools/Controller/internal/releaseplane/http.go",
];
const routeLiterals = new Set();
for (const file of routeSourceFiles) {
  const source = readFileSync(resolve(root, file), "utf8");
  for (const match of source.matchAll(/"(\/(?:healthz|api\/v1)[^"? ]*)"/gu)) routeLiterals.add(match[1]);
}
const documentedPaths = Object.keys(openAPIPaths);
const routeCovered = (literal) => {
  if (literal === "/api/v1" || literal === "/api/v1/") return true;
  return documentedPaths.some((documented) => {
    if (documented === literal) return true;
    const parameter = documented.indexOf("{");
    const prefix = parameter >= 0 ? documented.slice(0, parameter) : `${documented}/`;
    return literal.startsWith(prefix) || documented.startsWith(`${literal.replace(/\/$/u, "")}/`);
  });
};
const missingRoutes = [...routeLiterals].filter((literal) => !routeCovered(literal)).sort();
if (missingRoutes.length > 0) throw new Error(`OpenAPI catalog is missing source route families: ${missingRoutes.join(", ")}`);
for (const path of documentedPaths) {
  if (path !== "/healthz" && !path.startsWith("/api/v1/")) throw new Error(`OpenAPI contains a versionless route: ${path}`);
}

function normalize(content) {
  return content.replaceAll("\r\n", "\n");
}

if (checkOnly) {
  const stale = [];
  for (const [name, content] of outputs) {
    const path = resolve(outputDirectory, name);
    if (!existsSync(path) || normalize(readFileSync(path, "utf8")) !== normalize(content)) stale.push(relative(root, path).replaceAll("\\", "/"));
  }
  if (stale.length > 0) throw new Error(`generated API reference is missing or stale: ${stale.join(", ")}; run node Tools/Audit/generate-api-reference.mjs`);
  process.stdout.write(`API reference is current: ${methods.length} RPC methods, ${Object.keys(openAPIPaths).length} REST paths, digest ${digest.slice(0, 12)}.\n`);
} else {
  mkdirSync(outputDirectory, { recursive: true });
  for (const [name, content] of outputs) writeFileSync(resolve(outputDirectory, name), content, "utf8");
  process.stdout.write(`Generated ${outputs.size} API reference files with digest ${digest.slice(0, 12)}.\n`);
}
