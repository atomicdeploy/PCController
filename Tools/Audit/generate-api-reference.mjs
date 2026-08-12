#!/usr/bin/env node

import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

/** Resolve the public API identity from the shared product manifest. */
export function productIdentity(metadata) {
  const productName = String(metadata.productName || metadata.name || "Controller").trim();
  const productProtocol = String(metadata.productProtocol || "controller").trim().toLowerCase();
  if (!productName || !/^[a-z][a-z0-9+.-]*$/u.test(productProtocol)) {
    throw new Error("product metadata requires a display name and URI-safe protocol name");
  }
  return {
    productName,
    productProtocol,
    httpTitle: `${productName} HTTP API`,
		rpcSchemaID: `https://${productProtocol}.local/schemas/jsonrpc.json`,
    rpcTitle: `${productName} JSON-RPC 2.0`,
    eventTitle: `${productName} event and WebSocket API`,
    referenceTitle: `${productName} API Reference`,
    referenceHeading: `${productName} API`,
  };
}

/** Extract literal controller methods from one-line or wrapped Go case clauses. */
export function extractGoCaseMethods(sources) {
  const dispatched = new Set();
  for (const source of sources) {
    const lines = source.split(/\r?\n/u);
    for (let index = 0; index < lines.length; index += 1) {
      const trimmed = lines[index].trimStart();
      if (!trimmed.startsWith("case ")) continue;
      let clause = trimmed.slice(5);
      while (!clause.includes(":") && index + 1 < lines.length) {
        index += 1;
        clause += ` ${lines[index].trim()}`;
      }
      const colon = clause.indexOf(":");
      if (colon < 0) continue;
      for (const literal of clause.slice(0, colon).matchAll(/"(controller\.[a-z0-9_.-]+)"/giu)) {
        dispatched.add(literal[1]);
      }
    }
  }
  return dispatched;
}

/** Require exact agreement between the documented and dispatched RPC methods. */
export function validateMethodCatalog({ catalogued, dispatched, required = [] }) {
  const missing = [...dispatched].filter((name) => !catalogued.has(name)).sort();
  if (missing.length > 0) {
    throw new Error(`JSON-RPC catalog is missing dispatched methods: ${missing.join(", ")}`);
  }
  const stale = [...catalogued].filter((name) => !dispatched.has(name)).sort();
  if (stale.length > 0) {
    throw new Error(`JSON-RPC catalog contains methods absent from dispatch sources: ${stale.join(", ")}`);
  }
  for (const method of required) {
    if (!catalogued.has(method)) throw new Error(`JSON-RPC catalog is missing WebSocket method ${method}`);
  }
}

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const outputDirectory = resolve(root, "Tools", "Controller", "api");
const checkOnly = process.argv.includes("--check");
const productMetadata = JSON.parse(readFileSync(resolve(root, "Tools", "Controller", "web", "package.json"), "utf8"));
const product = productIdentity(productMetadata);

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
  messages: ["controller.message.send", "controller.message.delivery", "controller.message.action"],
  host_configuration: [
    "controller.host_menu.configure", "controller.host_menu.config.set", "controller.ui.config.set",
    "controller.peripherals.set", "controller.hotkeys.set", "controller.os.configure",
    "controller.lcd.presentation.configure", "controller.app.page", "controller.app.navigate",
    "controller.app.instance.report", "controller.app.instance.remove",
    "controller.macro.create", "controller.macro.update", "controller.macro.delete",
    "controller.macro.record.start", "controller.macro.record.stop",
  ],
  virtual_keys: ["controller.os.key", "controller.virtual_key"],
  power_actions: ["controller.os.power"],
  bridge_calls: ["controller.bridge.call"],
  integrations: [
    "controller.device.status", "controller.device.action", "controller.device.inspect",
    "controller.integrations.local.get", "controller.integrations.local.set",
    "controller.webhooks.replay", "controller.webhooks.clear",
  ],
  read: [
    "controller.artifact.manifest", "controller.artifact.list", "controller.update.status",
    "controller.discovery.github.workflow", "controller.discovery.github.release",
    "controller.discovery.manifest", "controller.discovery.local_manifest", "controller.discovery.check",
    "controller.discovery.status", "controller.host_menu.config", "controller.host_menu.config.get",
    "controller.ui.config", "controller.ui.config.get", "controller.peripherals", "controller.peripherals.get",
    "controller.os.policy", "controller.os.facts.catalog", "controller.host.facts.catalog",
    "controller.hotkeys.get", "controller.bridge.list", "controller.webhooks.status",
    "controller.webhooks.pending", "controller.webhooks.dead", "controller.ping", "controller.snapshot",
    "controller.session.snapshot", "controller.session.snapshot.last", "controller.status",
    "controller.front_panel", "controller.front-panel", "controller.command.catalog",
    "controller.program_state.get", "controller.program-state.get", "controller.temperatures",
    "controller.menu.list", "controller.menu.current", "controller.menu.layout.get",
    "controller.host_menu.state", "controller.rf.list", "controller.rf.presentation",
    "controller.rf.learn.status", "controller.history.status", "controller.history.timeline",
    "controller.lcd.presentation.status", "controller.ports", "controller.os.status",
    "controller.system.status", "controller.os.facts", "controller.host.facts",
    "controller.discovery.scan", "controller.pwm.values",
    "controller.app.instances", "controller.app.instance.get", "controller.app.bridge",
    "controller.macro.snapshot", "controller.macro.list", "controller.macro.status",
  ],
  board_commands: [
    "controller.program_state.set", "controller.program-state.set", "controller.menu.layout.set",
    "controller.host_menu.directory.replace", "controller.host_menu.content.push", "controller.menu.jump",
    "controller.menu.page", "controller.pwm.set", "controller.pwm.off", "controller.rf.learn.start",
    "controller.rf.learn.cancel", "controller.rf.map", "controller.rf.remove", "controller.rf.clear",
		"controller.rf.transmit", "controller.lcd.prompt", "controller.lcd.priority",
		"controller.display.send", "controller.opcode.send", "controller.opcode.exchange",
		"controller.opcode.request",
    "controller.macro.board_record.start", "controller.macro.board_record.stop",
    "controller.macro.board_record.clear", "controller.macro.play", "controller.macro.cancel",
  ],
  dynamic: ["controller.command.execute", "controller.app.action"],
};

const methodOverrides = {
	"controller.ping": "Return service health and protocol identity.",
  "controller.snapshot": "Return the authoritative cached controller snapshot.",
  "controller.command.execute": "Run a shared command after semantic capability classification.",
  "controller.command.catalog": "Return the machine-readable shared command catalog.",
  "controller.event.next": "Long-poll the next retained event after an event ID, optionally selecting activity, state, telemetry, or debug.",
  "controller.rf.map": "Replace one learned RF mapping and return board readback.",
  "controller.rf.transmit": "Transmit one validated RF waveform request.",
  "controller.restore.flash": "Restore a captured flash backup through the guarded restore path.",
  "controller.webhooks.status": "Return bounded outbound queue and dead-letter counters.",
  "controller.webhooks.pending": "List bounded non-secret pending outbound deliveries.",
  "controller.webhooks.dead": "List bounded non-secret dead-letter deliveries.",
  "controller.webhooks.replay": "Replay explicitly selected dead-letter deliveries.",
  "controller.webhooks.clear": "Clear explicitly selected dead-letter deliveries.",
	"controller.subscribe": "Subscribe this WebSocket connection to activity events, changed state, explicit debug, raw opcodes, and/or status.",
	"controller.opcode.exchange": "Exchange an opaque versionless UART opcode and caller-selected response opcode.",
	"controller.opcode.request": "Alias for an opaque versionless UART opcode exchange.",
	"controller.opcode.send": "Send an opaque versionless UART opcode; ACK is expected by default.",
	"controller.app.navigate": "Navigate all, one surface, or one exact live application instance.",
	"controller.app.action": "Route a validated page, title, OSC progress, raw OSC, port, command, or lifecycle action to live application instances.",
  "controller.app.instances": "List live application instances and their bounded non-secret state.",
  "controller.app.bridge": "Return the original coordinator bridge instance and its bounded process self-information.",
  "controller.app.instance.get": "Read one live application instance by ID.",
  "controller.app.instance.report": "Create or refresh one leased application-instance report.",
  "controller.app.instance.remove": "Remove one application instance from the live registry.",
  "controller.macro.snapshot": "Return the macro library, active recording, board capture, and playback state.",
  "controller.macro.list": "List named and categorized macros through the shared library.",
  "controller.macro.status": "Return current macro recording, board-capture, and playback progress.",
  "controller.macro.create": "Create a validated named macro with ordinary opcode steps.",
  "controller.macro.update": "Rename, recategorize, recolor, or replace a validated macro.",
  "controller.macro.delete": "Delete one macro from the shared persistent library.",
  "controller.macro.record.start": "Start host-connected recording from timestamped board and host action evidence.",
  "controller.macro.record.stop": "Save or discard the active host-connected recording.",
  "controller.macro.board_record.start": "Start the board-owned capture ring and host-connected continuation stream.",
  "controller.macro.board_record.stop": "Stop board-owned capture and import the retained timed records.",
  "controller.macro.board_record.clear": "Acknowledge and clear an exact retained board-capture identity.",
  "controller.macro.play": "Play one named macro through the ordinary opcode path with live timing evidence.",
  "controller.macro.cancel": "Cancel active playback and apply the macro output safe-stop policy.",
  "controller.message.send": "Publish one bounded typed message; synchronous delivery waits for requested presentation outcomes.",
  "controller.message.delivery": "Acknowledge presentation of one retained targeted message from an explicit surface.",
  "controller.message.action": "Run a retained message action only after an explicit surface gesture and publish its correlated outcome.",
  "controller.unsubscribe": "Remove this WebSocket connection's active subscriptions.",
};

const nonIdempotentMethods = new Set([
  "controller.reset.lines", "controller.reset", "controller.port.reset", "controller.command.execute",
  "controller.rf.learn.start", "controller.rf.transmit", "controller.lcd.prompt", "controller.lcd.priority",
  "controller.message.send", "controller.message.delivery", "controller.message.action",
  "controller.bridge.call", "controller.os.key", "controller.os.power",
	"controller.device.action", "controller.app.action", "controller.artifact.fetch",
	"controller.display.send", "controller.opcode.send", "controller.opcode.exchange",
	"controller.opcode.request",
  "controller.macro.create", "controller.macro.update", "controller.macro.delete",
  "controller.macro.record.start", "controller.macro.record.stop",
  "controller.macro.board_record.start", "controller.macro.board_record.stop",
  "controller.macro.board_record.clear", "controller.macro.play", "controller.macro.cancel",
  "controller.app.page", "controller.app.navigate", "controller.app.instance.report",
  "controller.app.instance.remove",
  "controller.artifact.capture", "controller.update.firmware", "controller.restore.flash",
  "controller.update.eeprom", "controller.update.host", "controller.discovery.stage",
  "controller.webhooks.replay",
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
  { path: "/api/ui-config", methods: ["get"], public: true, capability: "public", summary: "Non-secret browser bootstrap" },
  { path: "/api/session/ticket", methods: ["post"], capability: "session", summary: "Exchange a header credential for a short-lived one-use browser WebSocket ticket" },
  { path: "/api/rpc", methods: ["post"], capability: "dynamic", summary: "JSON-RPC 2.0 request" },
  { path: "/api/snapshot", methods: ["get"], capability: "read", summary: "Authoritative cached controller snapshot" },
  { path: "/api/peripherals", methods: ["get"], capability: "read", summary: "Ordered peripheral presentation descriptors" },
  { path: "/api/peripherals", methods: ["put"], capability: "host_configuration", summary: "Replace peripheral name, description, and order metadata" },
  { path: "/api/pwm", methods: ["get"], capability: "read", summary: "Authoritative PWM state" },
  { path: "/api/pwm", methods: ["put", "delete"], capability: "board_commands", summary: "Mutate PWM state with board readback" },
  { path: "/api/commands", methods: ["get"], capability: "read", summary: "Shared command catalog" },
  { path: "/api/program-state", methods: ["get"], capability: "read", summary: "Host-owned program state" },
  { path: "/api/program-state", methods: ["put", "post"], capability: "board_commands", summary: "Update host-owned program state" },
  { path: "/api/menu/catalog", methods: ["get"], capability: "read", summary: "Live board menu catalog" },
  { path: "/api/menu/layout", methods: ["get"], capability: "read", summary: "Live board menu layout" },
  { path: "/api/menu/layout", methods: ["put", "post"], capability: "board_commands", summary: "Persist a validated board menu layout" },
  { path: "/api/host-menus", methods: ["get"], capability: "read", summary: "Host-presented menu directory" },
  { path: "/api/host-menus", methods: ["put", "post"], capability: "host_configuration", summary: "Replace the host-presented menu directory" },
  { path: "/api/os/status", methods: ["get"], capability: "read", summary: "Host status and policy" },
  { path: "/api/os/facts", methods: ["get"], capability: "read", summary: "Bounded read-only host facts" },
  { path: "/api/os/key", methods: ["post"], capability: "virtual_keys", summary: "Validated virtual-key action" },
  { path: "/api/os/power", methods: ["post"], capability: "power_actions", summary: "Confirmed system or display action" },
  { path: "/api/command", methods: ["post"], capability: "dynamic", summary: "Shared command dispatcher" },
	{ path: "/api/messages", methods: ["post"], capability: "messages", summary: "Typed source-tagged message" },
	{ path: "/api/macros", methods: ["get"], capability: "read", summary: "Macro library, recording, and playback snapshot" },
	{ path: "/api/macros", methods: ["post", "put", "patch", "delete"], capability: "host_configuration", summary: "Create, rename, categorize, recolor, or delete a named macro" },
	{ path: "/api/macros/recording", methods: ["post", "delete"], capability: "host_configuration", summary: "Start or save/discard host-connected exact-delta recording" },
	{ path: "/api/macros/board-recording", methods: ["post", "delete"], capability: "board_commands", summary: "Start or stop the board capture ring and continuation stream" },
	{ path: "/api/macros/board-recording/clear", methods: ["post"], capability: "board_commands", summary: "Acknowledge and clear one retained board capture" },
	{ path: "/api/macros/playback", methods: ["post", "delete"], capability: "board_commands", summary: "Play or cancel a named macro through ordinary opcodes" },
	{ path: "/api/display", methods: ["post"], capability: "board_commands", summary: "Present arbitrary seven-segment or LCD text with scroll and repeat timing" },
	{ path: "/api/opcode", methods: ["post"], capability: "board_commands", summary: "Exchange an opaque versionless UART opcode" },
  { path: "/api/app/bridge", methods: ["get"], capability: "read", summary: "Original coordinator bridge instance and process identity" },
  { path: "/api/app/instances", methods: ["get"], capability: "read", summary: "List or query live application instances" },
  { path: "/api/app/instances", methods: ["post", "delete"], capability: "host_configuration", summary: "Report or remove one live application instance" },
	{ path: "/api/app/action", methods: ["post"], capability: "host_configuration", summary: "Route a validated action to all, one surface, or one live application instance" },
  { path: "/api/app/navigate", methods: ["post"], capability: "host_configuration", summary: "Navigate all, one surface, or one exact application instance" },
  { path: "/api/bridges", methods: ["get"], capability: "read", summary: "Configured bridge state" },
  { path: "/api/bridges/call", methods: ["post"], capability: "bridge_calls", summary: "Correlated bridge call" },
  { path: "/api/artifacts/manifest", methods: ["get"], capability: "read", summary: "Artifact/default/current manifest" },
  { path: "/api/artifacts", methods: ["get"], capability: "read", summary: "Content-addressed artifact catalog" },
  { path: "/api/artifacts/upload", methods: ["post"], capability: "programming", summary: "Import a verified artifact" },
  { path: "/api/artifacts/fetch", methods: ["post"], capability: "programming", summary: "Queue a bounded remote artifact fetch" },
  { path: "/api/artifacts/capture", methods: ["post"], capability: "programming", summary: "Capture flash or EEPROM explicitly" },
  { path: "/api/artifacts/{kind}/{sha256}", methods: ["get", "head"], capability: "read", summary: "Immutable ranged artifact download" },
  { path: "/api/artifacts/current/{kind}", methods: ["get", "head"], capability: "read", summary: "Latest explicitly captured artifact" },
  { path: "/api/updates/{kind}", methods: ["post"], capability: "programming", summary: "Queue an explicitly authorized update" },
  { path: "/api/restores/flash", methods: ["post"], capability: "programming", summary: "Queue an explicitly authorized flash restore" },
  { path: "/api/updates/status/{id}", methods: ["get"], capability: "read", summary: "Update operation progress" },
  { path: "/api/discovery/github/workflow", methods: ["post"], capability: "read", summary: "Discover workflow artifacts" },
  { path: "/api/discovery/github/release", methods: ["post"], capability: "read", summary: "Discover release assets" },
  { path: "/api/discovery/manifest", methods: ["get", "post"], capability: "read", summary: "Serve or validate an update manifest" },
  { path: "/api/discovery/check", methods: ["post"], capability: "read", summary: "Compare candidate and installed identities" },
  { path: "/api/discovery/stage", methods: ["post"], capability: "programming", summary: "Stage a verified candidate without programming" },
  { path: "/api/discovery/status/{id}", methods: ["get"], capability: "read", summary: "Discovery or staging progress" },
  { path: "/api/webhooks/inbound", methods: ["post"], capability: "messages", summary: "Optional inbound typed webhook" },
  { path: "/api/webhooks/outbound/status", methods: ["get"], capability: "read", summary: "Outbound delivery queue and dead-letter status" },
  { path: "/api/webhooks/outbound/pending", methods: ["get"], capability: "read", summary: "Bounded non-secret pending delivery list" },
  { path: "/api/webhooks/outbound/dead", methods: ["get"], capability: "read", summary: "Bounded non-secret dead-letter list" },
  { path: "/api/webhooks/outbound/replay", methods: ["post"], capability: "integrations", summary: "Replay explicitly selected dead-letter deliveries" },
  { path: "/api/webhooks/outbound/clear", methods: ["post"], capability: "integrations", summary: "Clear explicitly selected dead-letter deliveries" },
  { path: "/api/integrations/datahub/{path}", methods: ["get", "head", "post", "put", "patch", "delete"], capability: "integrations", summary: "Sanitized loopback data-service proxy" },
  { path: "/api/integrations/device/{path}", methods: ["get", "head", "post", "put", "patch", "delete"], capability: "integrations", summary: "Fail-closed device route; use typed RPC" },
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
	if (route.path === "/api/session/ticket") {
    delete operation.responses["200"];
    operation.responses["201"] = {
      description: "One-use Origin-bound browser session ticket",
      content: { "application/json": { schema: { $ref: "#/components/schemas/SessionTicket" } } },
    };
	}
	if (route.path === "/api/opcode") {
		operation.responses["200"] = {
			description: "Opaque UART response frame",
			content: { "application/json": { schema: { $ref: "#/components/schemas/OpcodeFrame" } } },
		};
	}
  if (route.path === "/api/app/instances" && method === "get") {
    operation.responses["200"] = {
      description: "One exact instance when id is supplied, otherwise all live instances",
      content: { "application/json": { schema: { oneOf: [
        { $ref: "#/components/schemas/AppInstance" },
        { type: "array", items: { $ref: "#/components/schemas/AppInstance" } },
      ] } } },
    };
  }
  if (route.path === "/api/app/bridge" && method === "get") {
    operation.responses["200"] = {
      description: "Original coordinator bridge instance with bounded process self-information",
      content: { "application/json": { schema: { $ref: "#/components/schemas/AppInstance" } } },
    };
  }
  if (route.path === "/api/app/instances" && method === "post") {
    operation.responses["200"] = {
      description: "Normalized live instance report",
      content: { "application/json": { schema: { $ref: "#/components/schemas/AppInstance" } } },
    };
  }
  if (!["get", "head", "delete"].includes(method)) {
    operation.requestBody = {
      required: true,
      content: {
		"application/json": { schema: route.path === "/api/rpc"
			? { $ref: "#/components/schemas/JSONRPCRequest" }
			: route.path === "/api/session/ticket"
				? { $ref: "#/components/schemas/SessionTicketRequest" }
				: route.path === "/api/opcode"
					? { $ref: "#/components/schemas/OpcodeRequest" }
					: route.path === "/api/app/instances"
						? { $ref: "#/components/schemas/AppInstanceReport" }
						: route.path === "/api/app/navigate"
							? { $ref: "#/components/schemas/AppNavigation" }
				: { type: "object", additionalProperties: true } },
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
  if (route.path === "/api/app/instances" && ["get", "delete"].includes(method)) {
    operation.parameters ??= [];
    operation.parameters.push({
      name: "id", in: "query", required: method === "delete",
      schema: { type: "string", pattern: "^[A-Za-z0-9._:-]{1,180}$" },
    });
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
    title: product.httpTitle,
		version: "unversioned",
		summary: "Unversioned living REST and JSON-RPC surface of the primary controller host.",
    description: "Loopback is the default. This alpha build deliberately disables authentication and authorization until issue #148; remote exposure still requires an explicit bind and Origin allow-list. The built-in listener does not terminate TLS.",
  },
  servers: [{ url: "http://127.0.0.1:8787", description: "Default loopback primary" }],
  security: [],
	"x-authentication-state": "disabled-until-issue-148",
  paths: openAPIPaths,
  components: {
    securitySchemes: {
      bearerAuth: { type: "http", scheme: "bearer", description: "Durable host access credential for HTTP and non-browser WebSocket clients. Never place it in a URL." },
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
      SessionTicketRequest: {
        type: "object", required: ["transport"], additionalProperties: false,
        properties: { transport: { type: "string", enum: ["websocket", "socket_io"] } },
      },
		SessionTicket: {
        type: "object", required: ["ticket", "protocol", "expires_at", "expires_in_ms", "principal"], additionalProperties: false,
        properties: {
          ticket: { type: "string", pattern: "^[a-f0-9]{64}$", writeOnly: true, description: "One-use ticket carried in Sec-WebSocket-Protocol, never in the URL." },
			protocol: { type: "string", const: "pccontroller" },
          expires_at: { type: "string", format: "date-time" },
		  expires_in_ms: { type: "integer", const: 15000 },
			principal: { type: "string" },
		},
		},
		OpcodeRequest: {
			type: "object", required: ["opcode"], additionalProperties: false,
			properties: {
				opcode: { type: "integer", minimum: 1, maximum: 255 },
				expect_opcode: { type: "integer", minimum: 1, maximum: 255, default: 128 },
				payload: { type: "string", contentEncoding: "base64", maxLength: 64 },
				payload_hex: { type: "string", pattern: "^(?:0x)?(?:[0-9a-fA-F][0-9a-fA-F][ _:-]?){0,48}$" },
			},
			not: { required: ["payload", "payload_hex"] },
		},
		OpcodeFrame: {
			type: "object", required: ["opcode", "name", "sequence"], additionalProperties: false,
			properties: {
				opcode: { type: "integer", minimum: 1, maximum: 255 },
				name: { type: "string" }, sequence: { type: "integer", minimum: 0, maximum: 255 },
				payload: { type: "string", contentEncoding: "base64" },
				payload_hex: { type: "string", pattern: "^[0-9A-F]*$" },
			},
		},
		AppInstanceReport: {
			type: "object", required: ["id", "surface"], additionalProperties: false,
			properties: {
				id: { type: "string", pattern: "^[A-Za-z0-9._:-]{1,180}$" },
				surface: { type: "string", pattern: "^[A-Za-z0-9._-]{1,64}$" },
				page: { type: "string", maxLength: 96 },
				state: { type: "string", enum: ["active", "hidden", "background", "leaving"] },
				lease_seconds: { type: "integer", minimum: 0, maximum: 300, default: 45 },
				values: { type: "object", maxProperties: 32, additionalProperties: { type: "string", maxLength: 1024 } },
				self: { $ref: "#/components/schemas/InstanceSelf" },
			},
		},
		InstanceSelf: {
			type: "object", required: ["kind"], additionalProperties: false,
			properties: {
				kind: { type: "string", pattern: "^[A-Za-z0-9._-]{1,64}$" },
				pid: { type: "integer", minimum: 0 },
				parent_pid: { type: "integer", minimum: 0 },
				image_path: { type: "string", maxLength: 4096 },
				working_directory: { type: "string", maxLength: 4096 },
				started_at: { type: "string", format: "date-time" },
				vars: { type: "object", maxProperties: 32, additionalProperties: { type: "string", maxLength: 1024 } },
			},
		},
		AppInstance: {
			allOf: [
				{ $ref: "#/components/schemas/AppInstanceReport" },
				{ type: "object", required: ["registered_at", "updated_at"], properties: {
					registered_at: { type: "string", format: "date-time" },
					updated_at: { type: "string", format: "date-time" },
					expires_at: { type: "string", format: "date-time" },
				} },
			],
		},
		AppNavigation: {
			type: "object", required: ["page"], additionalProperties: false,
			properties: {
				page: { type: "string", pattern: "^[A-Za-z0-9._/-]{1,96}$" },
				target: { type: "string", pattern: "^(?:\\*|[A-Za-z0-9._:-]{1,180})$", default: "*" },
			},
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
	"x-unsupported-transports": ["versioned /api/v* paths", "built-in TLS termination", "Socket.IO long-polling", "Socket.IO namespaces", "Socket.IO rooms", "binary Socket.IO attachments"],
};

const rpcSchema = {
  $schema: "https://json-schema.org/draft/2020-12/schema",
  $id: product.rpcSchemaID,
  title: product.rpcTitle,
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
			jsonrpc: { const: "2.0" }, method: { enum: ["controller.event", "controller.state", "controller.debug", "controller.opcode", "controller.status", "controller.error", "controller.data"] }, params: true,
      },
    },
  },
  "x-methods": Object.fromEntries(methods.map(({ name, ...metadata }) => [name, metadata])),
  "x-error-codes": {
    "-32700": "parse error", "-32600": "invalid request", "-32601": "method not found",
    "-32602": "invalid params", "-32001": "reserved for deferred authentication",
    "-32003": "reserved for deferred authorization", "-32000": "runtime or device error",
  },
};

const asyncapi = {
  asyncapi: "3.0.0",
  info: {
		title: product.eventTitle, version: "unversioned",
    description: "Full-duplex JSON-RPC, event, status, and Socket.IO-compatible messaging. Authentication and authorization are disabled in this alpha build until issue #148. WebSocket transport is required.",
  },
  servers: {
    loopback: {
      host: "127.0.0.1:8787", protocol: "ws", pathname: "/ipc",
      description: "Default loopback primary. Alpha remote exposure requires an explicit bind and allowed Origin; authentication and authorization are deferred to issue #148.",
	  security: [],
    },
  },
  channels: {
    rpc: {
      address: "/ipc",
      messages: {
		request: { $ref: "#/components/messages/JSONRPCRequest" }, response: { $ref: "#/components/messages/JSONRPCResponse" },
		event: { $ref: "#/components/messages/Event" }, state: { $ref: "#/components/messages/State" }, debug: { $ref: "#/components/messages/Debug" }, opcode: { $ref: "#/components/messages/Opcode" }, status: { $ref: "#/components/messages/Status" }, error: { $ref: "#/components/messages/Error" },
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
		{ $ref: "#/channels/rpc/messages/response" }, { $ref: "#/channels/rpc/messages/event" }, { $ref: "#/channels/rpc/messages/state" }, { $ref: "#/channels/rpc/messages/debug" }, { $ref: "#/channels/rpc/messages/opcode" },
      { $ref: "#/channels/rpc/messages/status" }, { $ref: "#/channels/rpc/messages/error" },
    ] },
    exchangeSocketIO: { action: "send", channel: { $ref: "#/channels/socketIO" }, messages: [{ $ref: "#/channels/socketIO/messages/packet" }] },
  },
  components: {
    securitySchemes: {
	  durableHeader: { type: "httpApiKey", in: "header", name: "Authorization", description: "Bearer credential for non-browser WebSocket and Socket.IO clients." },
	  browserTicket: { type: "httpApiKey", in: "header", name: "Sec-WebSocket-Protocol", description: "Browser clients first POST /api/session/ticket with a header credential, then offer pccontroller and pccontroller.ticket.<ticket>. The one-use ticket is Origin, peer, transport, and expiry bound; no credential appears in the URL." },
    },
    messages: {
      JSONRPCRequest: { payload: { $ref: "./jsonrpc.schema.json#/$defs/request" } },
      JSONRPCResponse: { payload: { oneOf: [{ $ref: "./jsonrpc.schema.json#/$defs/success" }, { $ref: "./jsonrpc.schema.json#/$defs/error" }] } },
		Event: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.event" }, params: { type: "object", additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.event", params: { id: 42, kind: "door", state: "open", source: "board" } }] } },
		State: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.state" }, params: { type: "object", additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.state", params: { id: 43, kind: "status_led.changed", stream: "state", source: "board" } }] } },
		Debug: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.debug" }, params: { type: "object", additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.debug", params: { id: 44, kind: "rx", stream: "debug", source: "board" } }] } },
		Opcode: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.opcode" }, params: { type: "object", required: ["id", "opcode"], additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.opcode", params: { id: 43, kind: "rx", opcode: 225, payload: "qrs=" } }] } },
      Status: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.status" }, params: { type: "object", additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.status", params: { uptime_ms: 1234 } }] } },
      Error: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.error" }, params: { type: "object", additionalProperties: true } } } },
      SocketIOPacket: { payload: { type: "string", description: "Engine.IO v4 text packet carrying subscribe, unsubscribe, message, command, or rpc events." } },
    },
  },
  "x-socket-io-events": {
    incoming: ["subscribe", "unsubscribe", "message", "command", "rpc"],
		outgoing: ["subscribed", "unsubscribed", "message.accepted", "command.response", "rpc.response", "controller.event", "controller.state", "controller.debug", "controller.opcode", "controller.status", "controller.error", "error"],
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
<meta name="color-scheme" content="light dark"><title>${escapeHTML(product.referenceTitle)}</title>
<style>:root{font-family:Inter,Segoe UI,system-ui,sans-serif;color-scheme:light dark;--bg:#f6f7fb;--panel:#fff;--text:#172033;--muted:#647087;--line:#dfe3ec;--accent:#6d4aff}@media(prefers-color-scheme:dark){:root{--bg:#11131a;--panel:#191c25;--text:#edf0f7;--muted:#a8b0c2;--line:#303543;--accent:#a995ff}}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text)}main{width:min(1180px,calc(100% - 32px));margin:auto;padding:48px 0 80px}header{display:grid;gap:12px;margin-bottom:34px}h1{font-size:clamp(2rem,5vw,4rem);letter-spacing:-.05em;margin:0}p{color:var(--muted);max-width:76ch;line-height:1.65}.pills{display:flex;flex-wrap:wrap;gap:8px}.pills a,.pills span,td span{border:1px solid var(--line);border-radius:999px;padding:6px 10px;color:var(--text);text-decoration:none;background:color-mix(in srgb,var(--panel) 88%,var(--accent) 12%)}section{margin-top:34px;background:color-mix(in srgb,var(--panel) 92%,transparent);border:1px solid var(--line);border-radius:20px;overflow:hidden;box-shadow:0 18px 55px color-mix(in srgb,var(--text) 8%,transparent)}section>div{padding:22px 24px 6px}h2{margin:0;font-size:1.25rem}table{border-collapse:collapse;width:100%;font-size:.9rem}th,td{text-align:left;padding:13px 16px;border-top:1px solid var(--line);vertical-align:top}th{color:var(--muted);font-weight:600}code{font-family:Cascadia Code,ui-monospace,monospace;color:var(--accent);overflow-wrap:anywhere}@media(max-width:720px){main{width:min(100% - 20px,1180px);padding-top:26px}section{overflow:auto}table{min-width:760px}}</style></head>
<body><main><header><span>OFFLINE CONTRACT · LIVING API</span><h1>${escapeHTML(product.referenceHeading)}</h1><p>The primary host exposes one unversioned living surface across REST, JSON-RPC, WebSocket, and the bounded Socket.IO adapter. In this alpha build authentication and authorization are deliberately disabled until issue #148; remote exposure still requires an explicit listener and Origin allow-list.</p><div class="pills"><a href="openapi.json">OpenAPI 3.1</a><a href="asyncapi.json">AsyncAPI 3.0</a><a href="jsonrpc.schema.json">JSON-RPC schema</a><span>${methods.length} RPC methods</span><span>${routes.reduce((count, route) => count + route.methods.length, 0)} HTTP operations</span></div></header>
<section><div><h2>HTTP operations</h2><p>Canonical routes live directly under <code>/api/</code>; versioned aliases are rejected. JSON bodies are capped at 1 MiB.</p></div><table><thead><tr><th>Method</th><th>Path</th><th>Purpose</th><th>Capability</th></tr></thead><tbody>${routeRows}</tbody></table></section>
<section><div><h2>JSON-RPC methods</h2><p>Standard JSON-RPC errors are preserved. Codes -32001 and -32003 are reserved for the deferred authentication design; -32000 reports runtime or device failures.</p></div><table><thead><tr><th>Method</th><th>Purpose</th><th>Capability</th><th>Idempotency</th></tr></thead><tbody>${methodRows}</tbody></table></section>
<p>Contract digest <code>${digest}</code>. Generated by <code>Tools/Audit/generate-api-reference.mjs</code>.</p></main></body></html>\n`;
outputs.set("reference.html", reference);

const dispatcherFiles = [
  "Tools/Controller/internal/ipcjson/ipc.go",
  "Tools/Controller/internal/artifacts/service.go",
  "Tools/Controller/internal/releaseplane/service.go",
];
const dispatcherSources = [];
for (const file of dispatcherFiles) {
  dispatcherSources.push(readFileSync(resolve(root, file), "utf8"));
}
const dispatched = extractGoCaseMethods(dispatcherSources);
const catalogued = new Set(methods.map(({ name }) => name));
validateMethodCatalog({
  catalogued,
  dispatched,
  required: ["controller.subscribe", "controller.unsubscribe"],
});

const routeSourceFiles = [
  "Tools/Controller/internal/ipcjson/ipc.go",
  "Tools/Controller/internal/ipcjson/artifacts_http.go",
  "Tools/Controller/internal/releaseplane/http.go",
];
const routeLiterals = new Set();
for (const file of routeSourceFiles) {
  const source = readFileSync(resolve(root, file), "utf8");
	for (const match of source.matchAll(/"(\/(?:healthz|api)[^"? ]*)"/gu)) routeLiterals.add(match[1]);
}
const documentedPaths = Object.keys(openAPIPaths);
const routeCovered = (literal) => {
  if (literal === "/api" || literal === "/api/") return true;
  // Explicit legacy-prefix rejection handlers are guards, not API routes.
  if (/^\/api\/v[0-9]+\/?$/u.test(literal)) return true;
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
	if (path !== "/healthz" && !path.startsWith("/api/")) throw new Error(`OpenAPI route is outside the living /api surface: ${path}`);
}

function normalize(content) {
  return content.replaceAll("\r\n", "\n");
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
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
}
