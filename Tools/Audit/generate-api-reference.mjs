#!/usr/bin/env node

import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { loadProjectEnv } from "../Build/env.mjs";
import { renderExplorer } from "./explorer/render.mjs";
import { extractRequestShapes } from "./explorer/request-shapes.mjs";
import { connectionGuides, subscriptionSchema } from "./explorer/websocket-contract.mjs";
import { groups, introduction, enrichOperation, explainFields, explainContract, rpcMeaning, rpcWireDocumentation } from './explorer/semantics.mjs';

loadProjectEnv();

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
// Generate against a committed dispatcher during concurrent UI development,
// without resetting another contributor's working tree or making a checkout.
const sourceRefIndex = process.argv.indexOf("--source-ref");
const sourceRef = sourceRefIndex < 0 ? null : process.argv[sourceRefIndex + 1];
if (sourceRefIndex >= 0 && (!sourceRef || !/^[a-zA-Z0-9_./-]+$/u.test(sourceRef))) {
  throw new Error("--source-ref requires an explicit Git revision");
}
const readContractSource = (file) => sourceRef
  ? execFileSync("git", ["show", `${sourceRef}:${file}`], { cwd: root, encoding: "utf8", maxBuffer: 8 << 20 })
  : readFileSync(resolve(root, file), "utf8");
const productMetadata = JSON.parse(readFileSync(resolve(root, "Tools", "Controller", "web", "package.json"), "utf8"));
const product = productIdentity(productMetadata);
const shapeDirectories = ["Tools/Controller/internal/ipcjson", "Tools/Controller/internal/artifacts", "Tools/Controller/internal/releaseplane", "Tools/Controller/internal/appconfig", "Tools/Controller/internal/native", "Tools/Controller/internal/hostui", "Tools/Controller/internal/control", "Tools/Controller/internal/hostos", "Tools/Controller/internal/mqttbridge"];
const shapeFiles = ["Tools/Controller/controller.go", ...shapeDirectories.flatMap(directory => readdirSync(resolve(root,directory)).filter(name=>name.endsWith('.go')&&!name.endsWith('_test.go')).map(name=>`${directory}/${name}`))];
if (sourceRef) {
  const existing = new Set(execFileSync('git',['ls-tree','-r','--name-only',sourceRef],{cwd:root,encoding:'utf8'}).trim().split(/\r?\n/u));
  for(let i=shapeFiles.length-1;i>=0;i--) if(!existing.has(shapeFiles[i])) shapeFiles.splice(i,1);
}
const requestFields = extractRequestShapes(root, shapeFiles.map(Name => ({ Name, Text: readContractSource(Name) })));

const capabilityGroups = {
  events: [
    "controller.event.next", "controller.event.latest", "controller.subscribe", "controller.unsubscribe",
		"controller.pwm.trace.start", "controller.pwm.trace.stop",
  ],
  programming: [
	"controller.firmware.build",
    "controller.artifact.fetch", "controller.artifact.upload", "controller.artifact.capture",
    "controller.board.firmware.upload", "controller.board.firmware.download",
    "controller.artifact.upload.begin", "controller.artifact.upload.chunk",
    "controller.artifact.upload.finish", "controller.artifact.upload.abort",
    "controller.update.firmware", "controller.restore.flash", "controller.update.eeprom",
    "controller.update.host", "controller.peer.update.host", "controller.discovery.stage",
    "controller.software.call",
  ],
  connection_control: [
    "controller.connect", "controller.open", "controller.port.open", "controller.close", "controller.port.close",
  ],
  reset: ["controller.reset.lines", "controller.reset", "controller.port.reset"],
  shutdown: ["controller.quit", "controller.exit"],
  messages: ["controller.message.send", "controller.osd.show", "controller.osd.dismiss"],
  host_configuration: [
    "controller.mqtt.config.get", "controller.mqtt.config.set", "controller.mqtt.status",
    "controller.udp.config.set", "controller.udp.subscribe", "controller.udp.unsubscribe",
		"controller.mirror.configure",
    "controller.host_menu.configure", "controller.host_menu.config.set", "controller.ui.config.set", "controller.ui.config.open",
		"controller.ui.preferences.set", "controller.status_led.policy.set", "controller.effects.library.set", "controller.automations.set", "controller.melodies.set",
    "controller.peripherals.set", "controller.board_profile.update", "controller.peripheral.presentation.update",
    "controller.peripheral.folder.update", "controller.channel_routing.update",
    "controller.hotkeys.set", "controller.os.configure",
    "controller.lcd.presentation.configure", "controller.app.page", "controller.app.navigate",
    "controller.app.launch", "controller.app.navigation.commit",
    "controller.app.instance.report", "controller.app.instance.remove", "controller.app.action.ack",
		"controller.network.peers.set", "controller.pwm.channel.configure",
		"controller.rf.binding.put", "controller.rf.binding.remove", "controller.rf.preferences.set",
  ],
  virtual_keys: ["controller.os.key", "controller.virtual_key"],
  power_actions: ["controller.os.power"],
  bridge_calls: ["controller.bridge.call", "controller.discovery.connect", "controller.udp.call"],
  integrations: [
		"controller.audio.play",
    "controller.device.status", "controller.device.action", "controller.device.inspect",
    "controller.integrations.local.get", "controller.integrations.local.set",
    "controller.webhooks.replay", "controller.webhooks.clear",
		"controller.discovery.config.set",
  ],
  read: [
    "controller.osd.status",
    "controller.udp.config.get", "controller.udp.status", "controller.udp.events.get",
		"controller.mirror.inspect", "controller.mirror.status",
		"controller.audio.cues",
    "controller.artifact.manifest", "controller.artifact.list", "controller.update.status",
    "controller.artifact.download.begin", "controller.artifact.download.chunk", "controller.artifact.download.close",
    "controller.software.targets", "controller.software.bundle.list", "controller.software.bundle.read",
    "controller.discovery.github.workflow", "controller.discovery.github.release",
    "controller.discovery.manifest", "controller.discovery.local_manifest", "controller.discovery.check",
    "controller.discovery.status", "controller.host_menu.config", "controller.host_menu.config.get",
    "controller.ui.config", "controller.ui.config.get", "controller.peripherals", "controller.peripherals.get",
		"controller.ui.preferences.get", "controller.status_led.policy.get", "controller.effects.library.get", "controller.automations.get", "controller.melodies.get",
    "controller.board_profile.get", "controller.channel_routing.get",
    "controller.os.policy", "controller.os.facts.catalog", "controller.host.facts.catalog",
    "controller.hotkeys.get", "controller.bridge.list", "controller.webhooks.status",
    "controller.webhooks.pending", "controller.webhooks.dead", "controller.ping", "controller.snapshot", "controller.estop.get",
    "controller.session.snapshot", "controller.session.snapshot.last", "controller.status",
    "controller.front_panel", "controller.front-panel", "controller.command.catalog", "controller.melodies.list",
    "controller.program_state.get", "controller.program-state.get", "controller.temperatures",
    "controller.menu.list", "controller.menu.current", "controller.menu.layout.get",
    "controller.host_menu.state", "controller.rf.list", "controller.rf.presentation",
    "controller.board_automation.list",
    "controller.rf.learn.status", "controller.history.status", "controller.history.timeline",
    "controller.lcd.presentation.status", "controller.ports", "controller.os.status",
    "controller.system.status", "controller.os.facts", "controller.host.facts",
    "controller.discovery.scan", "controller.discovery.config", "controller.discovery.config.get",
		"controller.integrations.status", "controller.pwm.values", "controller.illumination.get", "controller.port.owner", "controller.port.process",
    "controller.app.instances", "controller.app.instance.get", "controller.app.bridge",
		"controller.network.peers.get", "controller.app.action.outcome",
		"controller.media.playback.get", "controller.media.authority.get", "controller.media.timeline.get",
		"controller.rf.catalog", "controller.pwm.trace.get",
  ],
  board_commands: [
    "controller.buzzer.tone",
    "controller.channel.set", "controller.output.set",
		"controller.mirror.apply", "controller.mirror.resync",
    "controller.program_state.set", "controller.program-state.set", "controller.menu.layout.set",
    "controller.host_menu.directory.replace", "controller.host_menu.content.push", "controller.menu.jump",
	"controller.menu.page", "controller.pwm.set", "controller.pwm.off", "controller.status_led.set", "controller.status_led.release", "controller.illumination.set", "controller.illumination.override", "controller.rf.learn.start",
    "controller.rf.learn.cancel", "controller.rf.map", "controller.rf.remove", "controller.rf.clear",
    "controller.rf.transmit", "controller.rf.observed.add", "controller.board_automation.put",
    "controller.board_automation.remove", "controller.board_automation.clear",
    "controller.lcd.prompt", "controller.lcd.priority",
		"controller.display.send", "controller.opcode.send", "controller.opcode.exchange",
		"controller.opcode.request", "controller.action.invoke",
		"controller.estop.set", "controller.media.authority.change",
		"controller.motion.hold.start", "controller.motion.hold.renew", "controller.motion.hold.release",
		"controller.media.playback.update", "controller.media.timeline.prepare",
  ],
  dynamic: ["controller.command.execute", "controller.app.action"],
};

const methodOverrides = {
  "controller.osd.show": "Present a non-activating Windows desktop overlay with a bounded lifetime, semantic icon, transitions/marquee and up to five human-activated actions. behavior selects replacement or a bounded concurrent stack; actions never execute merely on receipt.",
  "controller.osd.status": "Read desktop OSD support, current presentation identity, expiry and last action/error without creating a window.",
  "controller.osd.dismiss": "Fade out the current OSD. Supply its id to prevent an obsolete dismissal from closing a replacement overlay.",
  "controller.pwm.set": "Set one physical PWM channel (0..15) using either calibrated percent (0..100) or raw_value (0..4095), never both. Supply current snapshot owner/revision fields; the host verifies admission and returns authoritative readback.",
  "controller.pwm.off": "Turn off selected PWM outputs through the current board owner and return authoritative state.",
  "controller.pwm.values": "Read authoritative PWM output registers through the current board owner without changing a channel. During prepared playback the host retains the cached observation and its original generation/revision/time rather than creating polling traffic.",
	"controller.effects.library.get": "Return the portable effect library and its canonical ETag.",
	"controller.effects.library.set": "Import or replace an effect library using mandatory if_match, canonical validation and serialized compare-and-swap; does not execute effects.",
	"controller.automations.get": "Return configured host event/RF rules and their canonical ETag.",
	"controller.automations.set": "Replace configured host event/RF rules with mandatory if_match, policy and RF collision validation; does not explicitly run a rule or enable OS permissions.",
	"controller.melodies.get": "Return stored melody overrides and their canonical ETag; melodies.list includes built-in fallback melodies.",
	"controller.melodies.set": "Replace stored melody overrides with mandatory if_match and full note validation; does not play audio.",
	"controller.ui.preferences.get": "Return full typed host UI preferences and their ETag; no credentials or transport configuration.",
	"controller.ui.preferences.set": "Replace only UI preferences with mandatory if_match ETag, serialized compare-and-swap, validation and no-op suppression.",
	"controller.status_led.policy.get": "Return the host-owned reactive status-lighting policy.",
	"controller.status_led.policy.set": "Replace the reactive status-lighting policy with mandatory canonical JSON if_match revision and serialized validation.",
	"controller.motion.hold.start": "Start an advertised seat hold with a fresh 128-bit token, board generation and short press deadline; existing door and authority gates apply.",
	"controller.motion.hold.renew": "Extend the owned 700 ms motion lease without retransmitting movement; reject expired owners and unsafe door status.",
	"controller.motion.hold.release": "Revoke a pending or active owned hold and request safety-priority STOP; stale tokens cannot stop newer intents.",
  "controller.os.facts": "Read one bounded host-facts profile, including PC hardware sensors from lm-sensors or Libre Hardware Monitor; never board telemetry or arbitrary queries.",
  "controller.host.facts": "Read one bounded host-facts profile, including PC hardware sensors from lm-sensors or Libre Hardware Monitor; never board telemetry or arbitrary queries.",
  "controller.os.facts.catalog": "List fixed read-only host-facts profiles, including sensors, with columns and row limits.",
  "controller.host.facts.catalog": "List fixed read-only host-facts profiles, including sensors, with columns and row limits.",
  "controller.channel_routing.get": "Return host-owned semantic bindings, eligible physical targets, board/session pins and PWM configuration revision.",
  "controller.channel_routing.update": "Atomically replace revision-fenced compatible bindings while affected outputs are observed idle; never actuate outputs or migrate numeric cues.",
  "controller.channel.set": "Resolve a stable semantic key and confirm one owner/revision/session-pinned physical output; never automatically retry unknown outcomes.",
  "controller.output.set": "Confirm one explicit physical output with owner/revision/session pins; PWM accepts raw value or curve-aware percent_milli with a PWM configuration revision. No batch atomicity or automatic retry.",
	"controller.pwm.trace.get": "Return bounded DEBUG-only PWM acknowledgements; no board read or output change.",
	"controller.pwm.trace.start": "Temporarily capture one channel 0..15 for explicit duration_ms 1..60000, off by default; no board operation.",
	"controller.pwm.trace.stop": "Stop temporary PWM acknowledgement capture and retain bounded receipts; no board operation.",
	"controller.audio.cues": "Return the receiving host's declared local sound-effect intent vocabulary.",
	"controller.audio.play": "Play one validated sound-effect intent on the receiving host while honoring its mute and volume policy.",
	"controller.ping": "Return service health and protocol identity.",
  "controller.ui.config": "Return host-owned UI settings, including status_interval_ms and measurement_freshness_ms.",
  "controller.ui.config.get": "Return host-owned UI settings, including status_interval_ms and measurement_freshness_ms.",
  "controller.ui.config.set": "Atomically persist host UI fields; status_interval_ms is 50..60000 and measurement_freshness_ms is refresh+100..120000.",
  "controller.ui.config.open": "Open the active configuration file or its containing folder on the host desktop; target is file or folder. Requires an available interactive desktop.",
  "controller.snapshot": "Return the authoritative cached controller snapshot.",
	"controller.board_profile.get": "Return the attached board identity, configured wiring mode, and opaque profile revision.",
	"controller.board_profile.update": "Bind the attached physical board identity to one explicit host-owned wiring profile.",
	"controller.peripheral.presentation.update": "Update one advertised peripheral name, icon, group, kind-local order, visibility, or lock using optional optimistic concurrency.",
	"controller.peripheral.folder.update": "Atomically create, rename, set the icon, delete or move members of a board-profile channel folder using required expected_revision; return the complete authoritative folder/control catalog.",
	"controller.pwm.channel.configure": "Persist a partial PWM channel configuration (output type, icon, curve, gamma, step_percent, on_behavior full|restore), preserving omitted fields, then publish the changed descriptor.",
	"controller.action.invoke": "Invoke one stable action ID advertised by the attached board profile and wait for the board acknowledgement.",
	"controller.estop.get": "Return the authoritative PCController-wide effect and motion interlock state.",
	"controller.estop.set": "Engage or release the PCController-wide effect and motion interlock; release never resumes cancelled work.",
	"controller.media.authority.get": "Return the registered playback authority owner, pending requests, exclusivity, and revision.",
	"controller.media.authority.change": "Request, accept, reject, release, lock, or unlock playback authority for a registered client.",
	"controller.media.playback.get": "Return the authoritative registered consumer playback clock and prepared-timeline state.",
	"controller.media.playback.update": "Publish one sequenced playback-clock update from the registered authority owner.",
	"controller.media.timeline.get": "Return the prepared media timeline, acknowledgement ledger, and sticky failure state.",
	"controller.media.timeline.prepare": "Validate and arm a revisioned host-executed hardware timeline for a registered paused consumer.",
  "controller.command.execute": "Run a shared command after semantic capability classification.",
	"controller.firmware.build": "Compile the canonical host project with reviewed feature names and publish correlated ordered program progress events.",
  "controller.command.catalog": "Return the machine-readable shared command catalog.",
  "controller.melodies.list": "Return the effective configured host melody catalog.",
  "controller.event.next": "Long-poll the next retained event after an event ID, optionally selecting activity, state, telemetry, or debug.",
  "controller.rf.map": "Replace one learned RF mapping and return board readback.",
  "controller.rf.transmit": "Transmit one validated RF waveform request.",
	"controller.rf.catalog": "Return learned RF records, live capture state, persisted bindings, and currently advertised action choices.",
	"controller.rf.binding.put": "Create a uniquely named validated RF binding, or explicitly edit with previous_name; reject name/stale-target conflicts without replacing unrelated assignments.",
	"controller.rf.binding.remove": "Remove one named host-owned RF application binding.",
	"controller.rf.preferences.set": "Save timed-learning, duration and continuous-capture preferences without entering learning.",
	"controller.rf.observed.add": "Save a waveform actually received from the current board, unassigned, with preserved records and verified readback.",
  "controller.board_automation.list": "Read one generation-consistent board automation snapshot.",
  "controller.board_automation.put": "Transactionally add or edit one board automation and verify readback.",
  "controller.board_automation.remove": "Remove one board automation and verify its absence.",
  "controller.board_automation.clear": "Clear the board automation table and verify the empty snapshot.",
  "controller.restore.flash": "Restore a captured flash backup through the guarded restore path.",
  "controller.artifact.upload.begin": "Begin a bounded correlated peer artifact transfer.",
  "controller.artifact.upload.chunk": "Append one ordered bounded chunk to a peer artifact transfer.",
  "controller.artifact.upload.finish": "Revalidate and publish a completed peer artifact transfer.",
  "controller.artifact.upload.abort": "Abort and remove an incomplete peer artifact transfer.",
  "controller.artifact.download.begin": "Pin a verified artifact for a bounded peer download without copying it.",
  "controller.artifact.download.chunk": "Read an exact bounded range from a pinned peer artifact download.",
  "controller.artifact.download.close": "Release a pinned peer artifact download handle.",
  "controller.software.targets": "List revision-fenced software update owners among registered application instances.",
  "controller.software.call": "Relay only allowlisted update operations to an explicit current software owner; require programming and bridge authorization.",
  "controller.software.bundle.list": "List verified software candidates from the adjacent confined bundle catalog without exposing local paths.",
  "controller.software.bundle.read": "Read a bounded verified software bundle range for transfer to an explicit peer.",
  "controller.peer.update.host": "Transfer a verified executable using a caller-generated idempotency_key, wait through the peer restart, and verify its exact active SHA-256 acknowledgement. Retry uncertain outcomes with the same key.",
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
	"controller.app.launch": "Ensure, launch, or focus a named TUI or WebUI surface without arbitrary process execution.",
	"controller.app.navigation.commit": "Commit one follower group's canonical page and return its correlated coordinator outcome.",
	"controller.app.action": "Freeze a validated action to exact live targets and return its correlated per-target delivery operation.",
	"controller.app.action.ack": "Acknowledge one exact app-action target as applied or rejected.",
	"controller.app.action.outcome": "Return one bounded app-action operation and its exact per-target outcomes.",
  "controller.app.instances": "List live application instances and their bounded non-secret state.",
  "controller.app.bridge": "Return the original coordinator bridge instance and its bounded process self-information.",
  "controller.app.instance.get": "Read one live application instance by ID.",
  "controller.app.instance.report": "Create or refresh one leased application-instance report.",
  "controller.app.instance.remove": "Remove one application instance from the live registry.",
  "controller.discovery.connect": "Verify an authenticated discovered host and return its health and snapshot.",
  "controller.discovery.config": "Return persistent network advertisement configuration.",
  "controller.discovery.config.get": "Return persistent network advertisement configuration.",
  "controller.discovery.config.set": "Persist and hot-apply network advertisement configuration.",
  "controller.integrations.status": "Return requested and effective buzzer routing and playback state.",
	"controller.board.firmware.upload": "Queue a guarded transfer of a staged immutable firmware artifact to the board.",
	"controller.board.firmware.download": "Queue a fresh verified flash-only readback from the board.",
	"controller.network.peers.get": "Return persistent peer topology with secret references and no plaintext credentials.",
	"controller.network.peers.set": "Replace and hot-apply peer topology; events, state, and status topics are accepted, and only optional secret references may carry compatibility credentials.",
	"controller.illumination.get": "Read persisted enclosure-light policy, live door-selected target, and exact applied MOSFET channel-12 PWM.",
	"controller.illumination.override": "Temporarily apply exact 0..4095 PWM to MOSFET channel 12 without changing or taking ownership from the enclosure-light policy, then return authoritative readback.",
	"controller.illumination.set": "Change only mode/on/off illumination fields, verify EEPROM durability, and return authoritative live state.",
  "controller.unsubscribe": "Remove this WebSocket connection's active subscriptions.",
};

const nonIdempotentMethods = new Set([
  "controller.buzzer.tone",
  "controller.osd.show",
  "controller.reset.lines", "controller.reset", "controller.port.reset", "controller.command.execute",
	"controller.audio.play",
	"controller.firmware.build",
  "controller.rf.learn.start", "controller.rf.transmit", "controller.lcd.prompt", "controller.lcd.priority",
  "controller.board_automation.put",
  "controller.message.send", "controller.bridge.call", "controller.os.key", "controller.os.power",
	"controller.device.action", "controller.app.action", "controller.artifact.fetch",
	"controller.display.send", "controller.opcode.send", "controller.opcode.exchange",
	"controller.opcode.request", "controller.action.invoke",
  "controller.app.page", "controller.app.navigate", "controller.app.navigation.commit", "controller.app.instance.report",
  "controller.app.instance.remove",
  "controller.artifact.capture", "controller.board.firmware.download",
  "controller.update.firmware", "controller.board.firmware.upload", "controller.restore.flash",
  "controller.artifact.upload.begin", "controller.artifact.upload.chunk",
  "controller.artifact.upload.finish", "controller.artifact.upload.abort",
  "controller.software.call",
  "controller.update.eeprom", "controller.update.host", "controller.peer.update.host",
  "controller.discovery.stage",
  "controller.webhooks.replay",
]);

const safeCapabilities = new Set(["read", "events"]);
// Hardware side effects must never be replayed merely because the requested
// absolute number happens to be unchanged.
nonIdempotentMethods.add("controller.channel.set");
nonIdempotentMethods.add("controller.output.set");
const methods = [];
function defaultMethodSummary(name, capability) {
  const parts = name.replace(/^controller\./u, '').split(/[_.-]+/u);
  const verbs = { get:'Read', set:'Set', list:'List', configure:'Configure', update:'Update', remove:'Remove', clear:'Clear', start:'Start', stop:'Stop', cancel:'Cancel', open:'Open', close:'Close', reset:'Reset', build:'Build' };
  const last = parts.at(-1);
  const verb = verbs[last];
  const words = (verb ? parts.slice(0,-1) : parts).map(word => ['pwm','rf','lcd','udp','mqtt','ui','os'].includes(word) ? word.toUpperCase() : word).join(' ');
  return verb ? `${verb} ${words || 'the controller session'}.` : `${safeCapabilities.has(capability) ? 'Read' : 'Invoke'} the ${words} ${safeCapabilities.has(capability) ? 'surface' : 'operation'}.`;
}
for (const [capability, names] of Object.entries(capabilityGroups)) {
  for (const name of names) {
    methods.push({
      name,
      capability,
      summary: methodOverrides[name] ?? defaultMethodSummary(name,capability),
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
  { path:"/api/osd", methods:["get"], capability:"read", summary:"Current Windows desktop OSD presentation and platform support" },
  { path:"/api/osd", methods:["post","delete"], capability:"messages", summary:"Show or dismiss a timed desktop OSD; actions run only on human activation" },
  { path: "/healthz", methods: ["get"], public: true, capability: "public", summary: "Service liveness and API identity" },
  { path: "/upnp/public.json", methods: ["get"], public: true, capability: "public", summary: "Bounded public host, board, endpoint, health, and telemetry directory" },
  { path: "/api/ui-config", methods: ["get"], public: true, capability: "public", summary: "Non-secret browser bootstrap" },
  { path: "/api/auth/server-proof", methods: ["get"], public: true, capability: "public", summary: "Dormant alpha compatibility endpoint; returns 409 while application authentication is disabled" },
  { path: "/api/session/ticket", methods: ["post"], capability: "session", summary: "Dormant alpha compatibility endpoint; returns 409 while application authentication is disabled" },
  { path: "/api/rpc", methods: ["post"], capability: "dynamic", summary: "JSON-RPC 2.0 request" },
  { path: "/api/snapshot", methods: ["get"], capability: "read", summary: "Authoritative cached controller snapshot" },
  { path: "/api/sensors/board", methods: ["get"], capability: "read", summary: "Export fresh PCB DS18B20 and INA219 measurements, units, availability and original receipt time; never host PC sensors" },
  { path: "/api/sensors/metrics", methods: ["get"], capability: "read", summary: "Prometheus PCB sensor gauges; unavailable readings are omitted, never zero-filled" },
  { path: "/api/docs/{asset}", methods: ["get", "head"], capability: "read", summary: "Offline API explorer and generated HTTP, RPC and event contracts" },
  { path: "/api/diagnostics/cpu-profile", methods: ["post"], capability: "local-only", binaryResponse: true, summary: "Explicit five-second application CPU profile; loopback callers only, one active capture, no board actions or request body" },
  { path: "/api/diagnostics/execution-trace", methods: ["post"], capability: "local-only", binaryResponse: true, summary: "Explicit five-second Go execution trace; loopback callers only, shared profile capture exclusion, no board actions or request body" },
  { path: "/api/estop", methods: ["get"], capability: "read", summary: "Authoritative effect and motion interlock state" },
  { path: "/api/estop", methods: ["put", "post"], capability: "board_commands", summary: "Engage or release the effect and motion interlock" },
  { path: "/api/peripherals", methods: ["get"], capability: "read", summary: "Peripheral descriptors and host-owned names" },
  { path: "/api/peripherals", methods: ["put"], capability: "host_configuration", summary: "Replace host-owned peripheral names" },
  { path: "/api/pwm", methods: ["get"], capability: "read", summary: "Authoritative PWM state" },
  { path: "/api/pwm", methods: ["put", "delete"], capability: "board_commands", summary: "Mutate PWM state with board readback" },
  { path: "/api/pwm/channel", methods: ["get", "put"], capability: "host_configuration", summary: "Read or configure host-owned PWM channel behavior" },
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
	{ path: "/api/display", methods: ["post"], capability: "board_commands", summary: "Present arbitrary seven-segment or LCD text with scroll and repeat timing" },
	{ path: "/api/opcode", methods: ["post"], capability: "board_commands", summary: "Exchange an opaque versionless UART opcode" },
  { path: "/api/app/bridge", methods: ["get"], capability: "read", summary: "Original coordinator bridge instance and process identity" },
  { path: "/api/app/instances", methods: ["get"], capability: "read", summary: "List or query live application instances" },
  { path: "/api/app/instances", methods: ["post", "delete"], capability: "host_configuration", summary: "Report or remove one live application instance" },
	{ path: "/api/app/action", methods: ["post"], capability: "host_configuration", summary: "Freeze and route a correlated action to exact live application instances" },
	{ path: "/api/app/action/ack", methods: ["post"], capability: "host_configuration", summary: "Acknowledge one exact app-action target outcome" },
	{ path: "/api/app/action/outcome", methods: ["get"], capability: "read", summary: "Read one bounded app-action operation outcome" },
  { path: "/api/app/navigate", methods: ["post"], capability: "host_configuration", summary: "Navigate all, one surface, or one exact application instance" },
  { path: "/api/app/launch", methods: ["post"], capability: "host_configuration", summary: "Ensure, launch, or focus a named TUI or WebUI surface" },
  { path: "/api/bridges", methods: ["get"], capability: "read", summary: "Configured bridge state" },
  { path: "/api/bridges/call", methods: ["post"], capability: "bridge_calls", summary: "Correlated bridge call" },
  { path: "/api/artifacts/manifest", methods: ["get"], capability: "read", summary: "Artifact/default/current manifest" },
  { path: "/api/artifacts", methods: ["get"], capability: "read", summary: "Content-addressed artifact catalog" },
  { path: "/api/artifacts/upload", methods: ["post"], capability: "programming", summary: "Import a verified artifact" },
  { path: "/api/artifacts/fetch", methods: ["post"], capability: "programming", summary: "Queue a bounded remote artifact fetch" },
  { path: "/api/artifacts/capture", methods: ["post"], capability: "programming", summary: "Capture flash or EEPROM explicitly" },
  { path: "/api/artifacts/{kind}/{sha256}", methods: ["get", "head"], capability: "read", summary: "Immutable ranged artifact download" },
  { path: "/api/artifacts/current/{kind}", methods: ["get", "head"], capability: "read", summary: "Latest explicitly captured artifact" },
  { path: "/api/board/firmware/upload", methods: ["post"], capability: "programming", summary: "Queue a guarded staged-firmware transfer to the board" },
  { path: "/api/board/firmware/download", methods: ["get", "head"], capability: "read", summary: "Download the latest verified board flash readback" },
  { path: "/api/board/firmware/download", methods: ["post"], capability: "programming", summary: "Queue a fresh verified board flash readback" },
  { path: "/api/updates/{kind}", methods: ["post"], capability: "programming", summary: "Queue an explicitly authorized update" },
  { path: "/api/restores/flash", methods: ["post"], capability: "programming", summary: "Queue an explicitly authorized flash restore" },
  { path: "/api/updates/status/{id}", methods: ["get"], capability: "read", summary: "Update operation progress" },
  { path: "/api/discovery/github/workflow", methods: ["post"], capability: "read", summary: "Discover workflow artifacts" },
  { path: "/api/discovery/github/release", methods: ["post"], capability: "read", summary: "Discover release assets" },
  { path: "/api/discovery/manifest", methods: ["get", "post"], capability: "read", summary: "Serve or validate an update manifest" },
  { path: "/api/discovery/check", methods: ["post"], capability: "read", summary: "Compare candidate and installed identities" },
  { path: "/api/discovery/stage", methods: ["post"], capability: "programming", summary: "Stage a verified candidate without programming" },
  { path: "/api/discovery/status/{id}", methods: ["get"], capability: "read", summary: "Discovery or staging progress" },
  { path: "/api/webhooks/inbound", methods: ["get", "post", "put", "patch", "delete", "options"], capability: "messages", summary: "Optional inbound typed webhook" },
  { path: "/api/webhooks/outbound/status", methods: ["get"], capability: "read", summary: "Outbound delivery queue and dead-letter status" },
  { path: "/api/webhooks/outbound/pending", methods: ["get"], capability: "read", summary: "Bounded non-secret pending delivery list" },
  { path: "/api/webhooks/outbound/dead", methods: ["get"], capability: "read", summary: "Bounded non-secret dead-letter list" },
  { path: "/api/webhooks/outbound/replay", methods: ["post"], capability: "integrations", summary: "Replay explicitly selected dead-letter deliveries" },
  { path: "/api/webhooks/outbound/clear", methods: ["post"], capability: "integrations", summary: "Clear explicitly selected dead-letter deliveries" },
  { path: "/api/integrations/status", methods: ["get"], capability: "read", summary: "Requested and effective buzzer routing and playback state" },
  { path: "/api/integrations/datahub/{path}", methods: ["get", "head", "post", "put", "patch", "delete"], capability: "integrations", summary: "Sanitized loopback data-service proxy" },
  { path: "/api/integrations/device/{path}", methods: ["get", "head", "post", "put", "patch", "delete"], capability: "integrations", summary: "Fail-closed device route; use typed RPC" },
];

const actionIdentifierSchema = { type: "string", pattern: "^[A-Za-z0-9._:-]{1,180}$" };
const actionSelectorSchema = { type: "string", pattern: "^(?:\\*|[A-Za-z0-9._:-]{1,180})$", default: "*" };
const actionKindSchema = {
	type: "string",
	oneOf: [
		{ enum: ["app.page", "app.title", "app.progress", "app.osc", "app.quit", "app.port.open", "app.port.close", "command"] },
		{
			maxLength: 64,
			pattern: "^(?!app\\.)(?!controller\\.)(?:[A-Za-z0-9_-]+\\.)+[A-Za-z0-9_-]+$",
		},
	],
};

function osdSchemas(prefix) {
  const ref=name=>({$ref:prefix+name});
  return {
    OSDEmpty:{type:'object',additionalProperties:false,properties:{}},
    OSDDismiss:{type:'object',additionalProperties:false,properties:{id:{type:'string',maxLength:64,description:'Exact active OSD identity; omission dismisses whichever overlay is current.'}}},
    OSDAction:{type:'object',required:['label'],anyOf:[{required:['id']},{required:['uri']}],additionalProperties:false,properties:{id:{type:'string',minLength:1,maxLength:64,pattern:'^[^\\s]+$',description:'Unique choice identifier; required for script-only buttons without a URI.'},label:{type:'string',minLength:1,maxLength:64},uri:{type:'string',minLength:1,maxLength:2048,description:'Optional pccontroller://, http:// or https:// URI. Activated only by an explicit button click; omission returns a script choice without launching anything.'}}},
    OSDRequest:{type:'object',required:['text'],additionalProperties:false,properties:{
      id:{type:'string',maxLength:64},title:{type:'string',maxLength:128},text:{type:'string',minLength:1,maxLength:2048,description:'Up to six explicit lines (LF or CRLF); each long line scrolls independently.',examples:['Desktop overlay preview\nSecond line']},
      icon:{enum:['app','info','success','warning','error','door','hot','temperature-cleared','motion','relay','radio'],default:'app'},
      severity:{enum:['info','success','warning','error']},duration_ms:{type:'integer',anyOf:[{const:0},{minimum:500,maximum:120000}],default:5000},
      position:{enum:['top-left','top-center','top-right','center','bottom-left','bottom-center','bottom-right'],default:'bottom-center'},theme:{enum:['system','dark','light'],default:'system'},reduce_motion:{type:'boolean',default:false},
      behavior:{enum:['replace','stack'],default:'replace'},background:{enum:['current','none','transparent','acrylic','blur'],default:'current'},
      sound:{enum:['none','select','success','warning','error','connect','disconnect'],default:'none',description:'Optional Windows system sound cue. Host mute/zero-volume policy applies; never uses the board buzzer or loads remote files.'},flash_taskbar:{type:'boolean',default:false,description:'Request three flashes of this process\'s visible PCController taskbar button without activating it; no-window when none exists.'},
      actions:{type:'array',maxItems:5,items:ref('OSDAction')},
    }},
    OSDStatus:{type:'object',required:['supported','active','accepted'],properties:{supported:{type:'boolean'},active:{type:'boolean'},accepted:{type:'integer',minimum:0},id:{type:'string'},shown_at:{type:'string',format:'date-time'},expires_at:{type:'string',format:'date-time'},last_action:{type:'string'},last_action_id:{type:'string'},dismissal:{enum:['expired','dismissed','action','replaced','closed','failed']},last_error:{type:'string'},active_count:{type:'integer',minimum:0,maximum:6},items:{type:'array',maxItems:6,items:ref('OSDStatus')},dpi:{type:'integer'},pixel_width:{type:'integer'},pixel_height:{type:'integer'},text_renderer:{enum:['directwrite','gdi'],description:'Effective text rasterizer used for this presentation; omitted before presentation or when unsupported.'},background:{enum:['current','transparent','acrylic','blur']},attention:{type:'object',properties:{sound:{enum:['requested','suppressed','failed']},taskbar:{enum:['requested','foreground','no-window','unavailable']},error:{type:'string'}}}}},
  };
}

function actionSchemas(refPrefix) {
	const ref = (name) => ({ $ref: `${refPrefix}${name}` });
	const actionProperties = {
		kind: actionKindSchema,
		value: { type: "string", maxLength: 4096, pattern: "^[^\\u0000\\r\\n]*$" },
		source: { type: "string" },
		target: actionSelectorSchema,
		operation_id: actionIdentifierSchema,
		metadata: {
			type: "object", maxProperties: 16,
			propertyNames: { pattern: "^[A-Za-z0-9._-]{1,64}$" },
			properties: {
				operation_delivery_id: actionIdentifierSchema,
				operation_expires_at: { type: "string", format: "date-time" },
			},
			additionalProperties: { type: "string", maxLength: 1024, pattern: "^[^\\u0000\\r\\n]*$" },
			description: "Bounded non-secret action metadata. Exact-target pushes include coordinator-owned operation_delivery_id and operation_expires_at values. Credential-shaped keys are rejected by the host.",
		},
		at: { type: "string", format: "date-time" },
	};
	return {
		...osdSchemas(refPrefix),
		AppAction: {
			type: "object", required: ["kind"], additionalProperties: false,
			properties: actionProperties,
		},
		AppActionRequest: {
			type: "object", required: ["kind"], additionalProperties: false,
			properties: {
				...actionProperties,
				timeout_ms: {
					type: "integer", minimum: 0, maximum: 30000, default: 5000,
					description: "Acknowledgement deadline in milliseconds; zero or omission selects the 5000 ms host default.",
				},
			},
		},
		ActionAck: {
			type: "object", required: ["operation_id", "delivery_id", "instance_id", "state"], additionalProperties: false,
			properties: {
				operation_id: actionIdentifierSchema,
				delivery_id: actionIdentifierSchema,
				instance_id: actionIdentifierSchema,
				state: { type: "string", enum: ["applied", "rejected"] },
				reason: { type: "string", maxLength: 256, pattern: "^[^\\u0000-\\u001f\\u007f]*$" },
			},
		},
		ActionOutcomeRequest: {
			type: "object", required: ["operation_id"], additionalProperties: false,
			properties: { operation_id: actionIdentifierSchema },
		},
		ActionTargetOutcome: {
			type: "object", required: ["instance_id", "surface", "state", "updated_at"], additionalProperties: false,
			properties: {
				instance_id: actionIdentifierSchema,
				surface: { type: "string", pattern: "^[A-Za-z0-9._-]{1,64}$" },
				state: { type: "string", enum: ["queued", "applied", "rejected", "timeout"] },
				reason: { type: "string", maxLength: 256 },
				updated_at: { type: "string", format: "date-time" },
			},
		},
		ActionOperation: {
			type: "object",
			required: ["operation_id", "kind", "selector", "state", "created_at", "expires_at", "targets"],
			additionalProperties: false,
			properties: {
				operation_id: actionIdentifierSchema,
				kind: actionKindSchema,
				source: { type: "string" },
				selector: actionSelectorSchema,
				state: { type: "string", enum: ["queued", "applied", "rejected", "timeout", "partial"] },
				reason: { type: "string", maxLength: 256 },
				created_at: { type: "string", format: "date-time" },
				expires_at: { type: "string", format: "date-time" },
				targets: { type: "array", items: ref("ActionTargetOutcome") },
			},
		},
		ActionOperationEnvelope: {
			type: "object", required: ["accepted", "operation"], additionalProperties: false,
			properties: {
				accepted: { type: "boolean" },
				operation: ref("ActionOperation"),
			},
		},
	};
}

const openAPIActionSchemas = actionSchemas("#/components/schemas/");
const rpcActionSchemas = actionSchemas("#/$defs/");
const rpcActionMethodContracts = {
  "controller.osd.show": { params:"OSDRequest", result:"OSDStatus" },
  "controller.osd.dismiss": { params:"OSDDismiss", result:"OSDStatus" },
  "controller.osd.status": { params:"OSDDismiss", result:"OSDStatus" },
	"controller.app.action": { params: "AppActionRequest", result: "ActionOperationEnvelope" },
	"controller.app.action.ack": { params: "ActionAck", result: "ActionOperationEnvelope" },
	"controller.app.action.outcome": { params: "ActionOutcomeRequest", result: "ActionOperationEnvelope" },
  "controller.channel_routing.get": { params: "ChannelRoutingQuery", result: "ChannelRoutingCatalog" },
  "controller.channel_routing.update": { params: "ChannelRoutingUpdate", result: "ChannelRoutingCatalog" },
  "controller.channel.set": { params: "SemanticOutputRequest", result: "SemanticOutputResult" },
  "controller.output.set": { params: "PhysicalOutputRequest", result: "PinnedOutputResult" },
};

function channelRoutingSchemas(prefix) {
  const ref = (name) => ({ $ref: `${prefix}${name}` });
  const text = { type: "string", minLength: 1 };
  const generation = { type: "integer", minimum: 1 };
  const raw = { type: "integer", minimum: 0, maximum: 4095 };
  const percent = { type: "integer", minimum: 0, maximum: 100000 };
  const targets = [...Array.from({length:4}, (_, n) => `relay.${n+5}`), ...Array.from({length:11}, (_, n) => `pwm.${n}`)];
  const key = {type:"string", maxLength:64, pattern:"^(?!(?:relay|pwm|seat|motion|display|sensor|strip)(?:\\.|$))(?!.*\\.\\.)(?!.*\\.$)[a-z][a-z0-9._-]*$"};
  const write = (semantic) => ({
    type:"object", additionalProperties:false,
    required:["expected_host_instance_id","expected_connection_generation","board_identity","expected_revision",semantic ? "key" : "target"],
    properties:{
      expected_host_instance_id:text, expected_connection_generation:generation, board_identity:text, expected_revision:text,
      [semantic ? "key" : "target"]:semantic ? key : {enum:targets},
      value:raw, percent_milli:percent, expected_pwm_configuration_revision:text,
    },
    oneOf:[{required:["value"],not:{required:["percent_milli"]}},
      {required:["percent_milli","expected_pwm_configuration_revision"],not:{required:["value"]}}],
    ...(!semantic ? {allOf:[{if:{properties:{target:{pattern:"^relay\\."}}},then:{required:["value"],properties:{value:{type:"integer",minimum:0,maximum:1}},not:{required:["percent_milli"]}}}]} : {}),
    description:"Semantic kind is validated against the current binding. Revisions are opaque. Unknown outcomes must never be automatically retried.",
  });
  return {
    ChannelRoutingQuery:{type:"object",additionalProperties:false},
    SemanticChannel:{type:"object",additionalProperties:false,required:["key","kind","target","name"],properties:{
      key,kind:{enum:["relay","pwm"]},target:{enum:targets},name:{...text,maxLength:64},icon:{type:"string",maxLength:64},
    }},
    ChannelRouting:{type:"object",additionalProperties:false,required:["enabled","channels"],properties:{enabled:{type:"boolean"},channels:{type:"array",maxItems:15,items:ref("SemanticChannel")}}},
    ChannelRoutingUpdate:{type:"object",additionalProperties:false,required:["board_identity","expected_revision","routing"],properties:{board_identity:text,expected_revision:text,routing:ref("ChannelRouting")}},
    ChannelRoutingCatalog:{type:"object",required:["host_instance_id","connection_generation","pwm_configuration_revision","board_profile","routing","targets"],properties:{
      host_instance_id:text,connection_generation:{type:"integer",minimum:0},pwm_configuration_revision:text,
      board_profile:{type:"object",additionalProperties:true},routing:ref("ChannelRouting"),targets:{type:"array",items:{type:"object",additionalProperties:true}},
    }},
    SemanticOutputRequest:write(true),PhysicalOutputRequest:write(false),
    PinnedOutputResult:{type:"object",required:["accepted","outcome","target","value","observed_value","host_instance_id","board_identity","revision","connection_generation"],properties:{
      accepted:{const:true},outcome:{const:"confirmed"},target:{enum:targets},value:raw,observed_value:raw,
      host_instance_id:text,board_identity:text,revision:text,connection_generation:generation,percent_milli:percent,pwm_configuration_revision:text,
    },description:"The host requires matching observed_value and value. Logical writes also echo percent_milli and pwm_configuration_revision. This is board-register readback, not electrical measurement."},
    SemanticOutputResult:{allOf:[ref("PinnedOutputResult"),{type:"object",required:["key"],properties:{key}}]},
    PinnedOutputErrorData:{type:"object",required:["outcome"],properties:{outcome:{enum:["rejected","unknown"]}},description:"rejected means pre-runtime admission failed; unknown means no safe claim of non-actuation. Missing response/data is unknown. Do not retry hardware automatically."},
  };
}

function operationFor(route, method) {
  const operation = {
    operationId: `${method}_${route.path.replaceAll(/[^a-zA-Z0-9]+/gu, "_").replaceAll(/^_|_$/gu, "")}`,
    summary: route.summary,
    description: route.summary,
    tags: [route.path.startsWith('/api/') ? route.path.split('/')[2] : 'service'],
    "x-required-capability": route.capability,
    "x-idempotency": ["get", "head"].includes(method) ? "safe" : "see operation semantics and idempotency keys",
    responses: {
      "200": route.binaryResponse
        ? { description: "Five-second gzip-encoded Go CPU profile", content: { "application/octet-stream": { schema: { type: "string", format: "binary" } } } }
        : { description: "Accepted response", content: { "application/json": { schema: { type: "object", additionalProperties: true } } } },
      "400": { $ref: "#/components/responses/BadRequest" },
      "401": { $ref: "#/components/responses/Unauthorized" },
      "403": { $ref: "#/components/responses/Forbidden" },
      "405": { $ref: "#/components/responses/MethodNotAllowed" },
    },
  };
	if(route.path==='/api/osd'){
		operation.responses['200'].content['application/json'].schema={$ref:'#/components/schemas/OSDStatus'};
		if(method==='get') operation.parameters=[{in:'query',name:'id',required:false,schema:{type:'string',maxLength:64},description:'Exact active or retained terminal notification identity; omission reads the current presentation.'}];
		if(method!=='get') operation.requestBody={required:true,content:{'application/json':{schema:{$ref:'#/components/schemas/'+(method==='post'?'OSDRequest':'OSDDismiss')}}}};
	}
	if (route.path === "/api/sensors/metrics") {
		operation.responses["200"] = { description: "Prometheus text exposition", content: { "text/plain": { schema: { type: "string" } } } };
	}
	if (route.path === '/api/sensors/board') operation.responses['200'] = { description:'Observed PCB measurements with null unavailable values; original host receipt time is retained.', content:{'application/json':{schema:{$ref:'#/components/schemas/BoardSensorSnapshot'}}} };
	if (route.path === '/api/docs/{asset}') operation.responses['200'] = { description:'An embedded API reference asset or contract.', content:{'text/html':{schema:{type:'string'}},'application/json':{schema:{type:'object',additionalProperties:true}},'text/javascript':{schema:{type:'string'}},'text/css':{schema:{type:'string'}}} };
  if (route.public) operation.security = [];
	if (route.path === "/api/session/ticket") {
    delete operation.responses["200"];
		operation.responses["409"] = { description: "Application authentication is disabled in the immediate alpha" };
	}
	if (route.path === "/api/auth/server-proof") {
		operation.parameters = [{ name: "X-PCController-Nonce", in: "header", required: true, schema: { type: "string", minLength: 22, maxLength: 86 }, description: "16..64 random bytes encoded as unpadded base64url" }];
		delete operation.responses["200"];
		operation.responses["409"] = { description: "Application authentication is disabled in the immediate alpha" };
	}
	if (route.path === "/api/opcode") {
		operation.responses["200"] = {
			description: "Opaque UART response frame",
			content: { "application/json": { schema: { $ref: "#/components/schemas/OpcodeFrame" } } },
		};
	}
	if (route.path === "/api/app/action" && method === "post") {
		delete operation.responses["200"];
		operation.responses["202"] = {
			description: "Action frozen to its exact live target set",
			content: { "application/json": { schema: { $ref: "#/components/schemas/ActionOperationEnvelope" } } },
		};
		operation.responses["409"] = {
			description: "The selector resolved only to rejected targets",
			content: { "application/json": { schema: { $ref: "#/components/schemas/ActionOperationEnvelope" } } },
		};
	}
	if (route.path === "/api/app/action/ack" && method === "post") {
		operation.responses["200"] = {
			description: "Updated correlated operation",
			content: { "application/json": { schema: { $ref: "#/components/schemas/ActionOperationEnvelope" } } },
		};
		operation.responses["409"] = {
			description: "Unknown, expired, foreign, offline, or conflicting acknowledgement",
			content: { "application/json": { schema: { $ref: "#/components/schemas/Error" } } },
		};
	}
	if (route.path === "/api/app/action/outcome" && method === "get") {
		operation.responses["200"] = {
			description: "Current bounded per-target operation outcome",
			content: { "application/json": { schema: { $ref: "#/components/schemas/ActionOperationEnvelope" } } },
		};
		operation.responses["404"] = {
			description: "Operation is unknown or expired",
			content: { "application/json": { schema: { $ref: "#/components/schemas/Error" } } },
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
  if (!["get", "head", "delete"].includes(method) && !operation.requestBody) {
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
						: route.path === "/api/app/action"
							? { $ref: "#/components/schemas/AppActionRequest" }
							: route.path === "/api/app/action/ack"
								? { $ref: "#/components/schemas/ActionAck" }
						: route.path === "/api/app/navigate"
							? { $ref: "#/components/schemas/AppNavigation" }
							: route.path === "/api/app/launch"
								? { $ref: "#/components/schemas/AppSurfaceLaunch" }
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
	if (route.path === "/api/app/action/outcome" && method === "get") {
		operation.parameters ??= [];
		operation.parameters.push({
			name: "operation_id", in: "query", required: true,
			schema: actionIdentifierSchema,
			description: "Correlated app-action operation identifier.",
		});
	}
  return enrichOperation(operation, route.path, method, requestFields);
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
    description: introduction,
  },
  servers: [{ url: "http://127.0.0.1:8787", description: "Default loopback primary" }],
  security: [],
  paths: openAPIPaths,
  tags: [...new Set(routes.map(route => route.path.startsWith('/api/') ? route.path.split('/')[2] : 'service'))].sort().map(name => {
    if (!groups[name]) throw new Error(`Missing API group description: ${name}`);
    return { name, 'x-displayName':groups[name][0], 'x-summary':groups[name][0], description:groups[name][1] };
  }),
  components: {
    securitySchemes: {
      bearerAuth: { type: "http", scheme: "bearer", description: "Durable host access credential for HTTP and non-browser WebSocket clients. Never place it in a URL." },
      tokenHeader: { type: "apiKey", in: "header", name: "X-PCController-Token", description: "Header-only compatibility credential." },
    },
    schemas: {
			...osdSchemas('#/components/schemas/'),
      BoardSensorReading: { type:'object',required:['id','label','kind','unit','value'],additionalProperties:false,properties:{
        id:{enum:['supply','bus','current','power','temperature_led','temperature_bt_audio']},label:{type:'string'},
        kind:{enum:['voltage','current','power','temperature']},unit:{enum:['V','A','W','C']},
        value:{type:['number','null'],description:'Valid fresh reading in the declared unit, or null. Zero and negative readings are not missing.'},
      }},
      BoardSensorSnapshot:{type:'object',required:['source','host_instance_id','connection_generation','connected','fresh','observed_at','expires_at','readings'],properties:{
        source:{const:'pccontroller-board-status'},host_instance_id:{type:'string'},connection_generation:{type:'integer',minimum:0},connected:{type:'boolean'},fresh:{type:'boolean'},
        observed_at:{type:'string',format:'date-time',description:'Host receipt time of the original board-status frame, not scrape or per-sensor ADC time.'},
        expires_at:{type:'string',format:'date-time',description:'Five seconds after original receipt. Expired values are null.'},
        readings:{type:'array',items:{$ref:'#/components/schemas/BoardSensorReading'}},
      }},
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
		ServerProof: {
			type: "object", required: ["format", "nonce", "audience", "instance_id", "proof"], additionalProperties: false,
			properties: {
				format: { type: "string", const: "pccontroller-server-proof" },
				nonce: { type: "string", description: "The caller-supplied unpadded base64url nonce." },
				audience: { type: "string", description: "The IP:port of the exact local listener that accepted the request." },
				instance_id: { type: "string", minLength: 1 },
				proof: { type: "string", description: "Unpadded base64url HMAC-SHA256 over format, nonce, audience, and instance identity." },
			},
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
				page: { type: "string", pattern: "^[A-Za-z0-9._-]{1,96}$" },
				target: { type: "string", pattern: "^(?:\\*|[A-Za-z0-9._:-]{1,180})$", default: "*" },
			},
		},
		AppSurfaceLaunch: {
			type: "object", required: ["surface"], additionalProperties: false,
			properties: {
				surface: { type: "string", enum: ["tui", "webui"] },
				mode: { type: "string", enum: ["ensure", "launch", "focus"], default: "ensure" },
				target: { type: "string", pattern: "^[A-Za-z0-9._:-]{1,180}$" },
				page: { type: "string", pattern: "^[A-Za-z0-9._/-]{1,96}$" },
				idempotency_key: { type: "string", pattern: "^[A-Za-z0-9._:-]{1,180}$" },
			},
			description: "Named product surface only. Executables, arguments, environment, shell text, and credentials are not accepted.",
		},
		...openAPIActionSchemas,
    ...channelRoutingSchemas("#/components/schemas/"),
      JSONRPCError: {
        type: "object", required: ["code", "message"], additionalProperties: false,
        properties: { code: { type: "integer", enum: [-32700, -32600, -32601, -32602, -32004, -32003, -32001, -32000] }, message: { type: "string" }, data: true },
        examples: [{ code: -32003, message: "remote capability board_commands is disabled" }],
      },
      Error: errorSchema,
    },
    responses: {
      BadRequest: { description: "Invalid request or parameters", content: { "application/json": { schema: errorSchema } } },
      Unauthorized: { description: "Reserved authentication rejection; application authentication is disabled in the current alpha. This is not a board-disconnection signal.", content: { "application/json": { schema: errorSchema } } },
      Forbidden: { description: "Capability or safety policy denied", content: { "application/json": { schema: errorSchema } } },
      MethodNotAllowed: { description: "The handler does not support this HTTP verb. When present, Allow lists accepted methods. Most handlers return plain text; structured policy handlers may return JSON.", headers: { Allow: { description:'HTTP methods accepted by this handler.',schema: { type: "string" } } }, content: { "text/plain":{schema:{type:'string'}}, "application/json": { schema: errorSchema } } },
    },
  },
  "x-body-limit-bytes": 1048576,
	"x-unsupported-transports": ["built-in TLS termination", "Socket.IO long-polling", "Socket.IO namespaces", "Socket.IO rooms", "binary Socket.IO attachments"],
};

const rpcSchema = {
  $schema: "https://json-schema.org/draft/2020-12/schema",
  $id: product.rpcSchemaID,
  title: product.rpcTitle,
  oneOf: [
    { $ref: "#/$defs/request" }, { $ref: "#/$defs/success" }, { $ref: "#/$defs/error" }, { $ref: "#/$defs/notification" },
  ],
	$defs: {
		...rpcActionSchemas,
    ...channelRoutingSchemas("#/$defs/"),
    id: { oneOf: [{ type: "string" }, { type: "integer" }, { type: "null" }] },
    request: {
      type: "object", required: ["jsonrpc", "method"], additionalProperties: false,
      properties: {
        jsonrpc: { const: "2.0" }, id: { $ref: "#/$defs/id" },
        method: { enum: methods.map(({ name }) => name) }, params: { type: ["object", "array", "null"] },
        auth: { type: "string", writeOnly: true },
      },
		allOf: Object.entries(rpcActionMethodContracts).map(([method, contract]) => ({
			if: { properties: { method: { const: method } }, required: ["method"] },
			then: { required: ["params"], properties: { params: { $ref: `#/$defs/${contract.params}` } } },
		})),
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
          properties: { code: { type: "integer" }, message: { type: "string" }, data: true },
        },
      },
    },
    notification: {
      type: "object", required: ["jsonrpc", "method", "params"], additionalProperties: false,
		properties: {
			jsonrpc: { const: "2.0" }, method: { enum: ["controller.event", "controller.state", "controller.debug", "controller.opcode", "controller.status", "controller.error", "controller.snapshot", "controller.pwm.values", "controller.data"] }, params: true,
      },
    },
  },
	"x-methods": Object.fromEntries(methods.map(({ name, ...metadata }) => {
		Object.assign(metadata, rpcMeaning(name, metadata.summary));
		if (requestFields[name] && !name.startsWith('controller.osd.')) metadata.request_fields = requestFields[name];
		Object.assign(metadata,rpcWireDocumentation(name,requestFields));
		if (name === 'controller.mqtt.config.set') {
			metadata.request_fields = {
				type:'object',required:['enabled'],additionalProperties:false,
				properties:{
					enabled:{type:'boolean',description:'Enable or stop the outbound MQTT broker session.'},
					broker:{type:'string',description:'mqtts://HOST[:PORT], or mqtt:// with a literal loopback IP. No credentials in the URL.'},
					topic:{type:'string',description:'Unique per-device prefix for command, event, status and availability.'},
					username:{type:'string',description:'Optional broker username.'},
					password_ref:{type:'string',description:'Optional os: or env: reference. Omit to clear; never send the password here.'},
					ca_file:{type:'string',description:'Optional local PEM bundle to add to system trust roots.'},
					telemetry_seconds:{type:'integer',minimum:0,maximum:3600,description:'Periodic status interval; zero uses the default of five seconds.'},
					allow_commands:{type:'boolean',description:'Explicitly grant broker command-topic writers the bounded board command set; defaults to false.'},
				},
			};
			metadata.request_template = {enabled:true,broker:'mqtts://broker.example:8883',topic:'pccontroller/cinema',telemetry_seconds:5,allow_commands:false};
		}
		if (name === 'controller.subscribe') {
			metadata.params_schema = { $ref: '#/$defs/Subscription' };
			metadata.request_template = subscriptionSchema.examples[0];
		}
		// Examples must not contain mutually exclusive selectors. This remains a
		// template: owner and revision placeholders must come from a fresh snapshot.
		if (name === 'controller.pwm.set') metadata.request_template = {
			channel: 0, percent: 50, expected_host_instance_id: '<snapshot host_instance_id>',
			expected_connection_generation: 0, board_identity: '<snapshot board identity>',
			expected_revision: '<snapshot revision>', expected_pwm_configuration_revision: '<snapshot PWM configuration revision>',
		};
		const contract = rpcActionMethodContracts[name];
		if (!contract) return [name, metadata];
		return [name, {
			...metadata,
			params_schema: { $ref: `#/$defs/${contract.params}` },
			result_schema: { $ref: `#/$defs/${contract.result}` },
		}];
	})),
  "x-error-codes": {
    "-32700": "parse error", "-32600": "invalid request", "-32601": "method not found",
    "-32602": "invalid params", "-32001": "authentication required",
    "-32003": "remote capability denied", "-32000": "runtime or device error",
    "-32004": "peer outcome uncertain; retry with the same idempotency key",
  },
};

rpcSchema.$defs.Subscription = subscriptionSchema;

const asyncapi = {
  asyncapi: "3.0.0",
  info: {
		title: product.eventTitle, version: "unversioned",
    description: "Immediate-alpha unauthenticated full-duplex JSON-RPC, event, status, and Socket.IO-compatible messaging. WebSocket transport is required; deferred security schemes are retained as non-active design metadata.",
  },
  servers: {
    loopback: {
      host: "127.0.0.1:8787", protocol: "ws", pathname: "/ipc",
      description: "Default loopback primary. Remote exposure requires an explicit listener and allowed origin; alpha authentication is disabled under #148.",
	  security: [],
    },
  },
  channels: {
    rpc: {
      address: "/ipc",
      description: "Raw JSON-RPC 2.0 over WebSocket text messages. Requests and notifications share the connection; live streams require controller.subscribe. The path is configurable via websocket_path in /api/ui-config.",
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
      JSONRPCRequest: { name: "JSON-RPC request", summary: "Client → host. One object per text message; use a unique id to correlate the reply.", payload: { $ref: "./jsonrpc.schema.json#/$defs/request" }, examples: [{ payload: { jsonrpc: "2.0", id: 1, method: "controller.subscribe", params: subscriptionSchema.examples[0] } }] },
      JSONRPCResponse: { name: "JSON-RPC response", summary: "Host → client. Matches the request id; result and error are alternatives. Notifications may arrive before this reply.", payload: { oneOf: [{ $ref: "./jsonrpc.schema.json#/$defs/success" }, { $ref: "./jsonrpc.schema.json#/$defs/error" }] }, examples: [{ payload: { jsonrpc: "2.0", id: 3, result: { subscribed: false } } }, { payload: { jsonrpc: "2.0", id: 4, error: { code: -32602, message: 'unknown subscription topic "invalid"' } } }] },
		Event: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.event" }, params: { type: "object", additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.event", params: { id: 42, kind: "door", state: "open", source: "board" } }] } },
		State: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.state" }, params: { type: "object", additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.state", params: { id: 43, kind: "status_led.changed", stream: "state", source: "board" } }] } },
		Debug: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.debug" }, params: { type: "object", additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.debug", params: { id: 44, kind: "rx", stream: "debug", source: "board" } }] } },
		Opcode: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.opcode" }, params: { type: "object", required: ["id", "opcode"], additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.opcode", params: { id: 43, kind: "rx", opcode: 225, payload: "qrs=" } }] } },
      Status: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.status" }, params: { type: "object", additionalProperties: true } }, examples: [{ jsonrpc: "2.0", method: "controller.status", params: { uptime_ms: 1234 } }] } },
      Error: { payload: { type: "object", required: ["jsonrpc", "method", "params"], properties: { jsonrpc: { const: "2.0" }, method: { const: "controller.error" }, params: { type: "object", additionalProperties: true } } } },
      SocketIOPacket: { payload: { type: "string", description: "Engine.IO v4 text packet carrying subscribe, unsubscribe, message, command, or rpc events." } },
    },
  },
  "x-connection-guides": connectionGuides,
  "x-socket-io-events": {
    incoming: ["subscribe", "unsubscribe", "message", "command", "rpc"],
		outgoing: ["subscribed", "unsubscribed", "message.accepted", "command.response", "rpc.response", "controller.event", "controller.state", "controller.debug", "controller.opcode", "controller.status", "controller.error", "error"],
  },
};

for (const [key, method, summary] of [
  ['snapshot', 'controller.snapshot', 'Complete snapshot sent initially for state subscriptions and after relevant activity/state changes.'],
  ['pwmValues', 'controller.pwm.values', 'Complete cached PWM values following a pwm.changed event on the state topic, when values are available.'],
]) {
  asyncapi.channels.rpc.messages[key] = { $ref: `#/components/messages/${key}` };
  asyncapi.operations.receiveRPC.messages.push({ $ref: `#/channels/rpc/messages/${key}` });
  asyncapi.components.messages[key] = { name: method, summary, payload: { type: 'object', required: ['jsonrpc', 'method', 'params'], properties: { jsonrpc: { const: '2.0' }, method: { const: method }, params: { type: 'object', additionalProperties: true } } } };
  asyncapi['x-socket-io-events'].outgoing.push(method);
}

for (const [name,schema] of Object.entries(openapi.components.schemas)) explainContract(name,schema);
for (const [name,schema] of Object.entries(rpcSchema.$defs)) explainContract(name,schema);
for (const metadata of Object.values(rpcSchema['x-methods'])) explainFields(metadata.request_fields);

const outputs = new Map([
  ["openapi.json", `${JSON.stringify(openapi, null, 2)}\n`],
  ["asyncapi.json", `${JSON.stringify(asyncapi, null, 2)}\n`],
  ["jsonrpc.schema.json", `${JSON.stringify(rpcSchema, null, 2)}\n`],
]);

const digest = createHash("sha256").update([...outputs.values()].join("\n")).digest("hex");
for (const [name, content] of renderExplorer({ product, openapi, rpcSchema, asyncapi, digest })) outputs.set(name, content);
outputs.set("assets/icon.svg", readFileSync(resolve(root, "Tools/Controller/web/public/favicon.svg"), "utf8"));

const dispatcherFiles = [
  "Tools/Controller/internal/ipcjson/ipc.go",
  "Tools/Controller/internal/artifacts/service.go",
  "Tools/Controller/internal/releaseplane/service.go",
];
const dispatcherSources = [];
for (const file of dispatcherFiles) {
  dispatcherSources.push(readContractSource(file));
}
const dispatched = extractGoCaseMethods(dispatcherSources);
const catalogued = new Set(methods.map(({ name }) => name));
validateMethodCatalog({
  catalogued,
  dispatched,
  required: ["controller.subscribe", "controller.unsubscribe"],
});

const routeSourceFiles = [
	"Tools/Controller/internal/ipcjson/osd.go",
  "Tools/Controller/internal/ipcjson/ipc.go",
	"Tools/Controller/internal/ipcjson/upnp.go",
	"Tools/Controller/internal/ipcjson/board_sensors.go",
	"Tools/Controller/internal/ipcjson/api_reference.go",
  "Tools/Controller/internal/ipcjson/artifacts_http.go",
  "Tools/Controller/internal/releaseplane/http.go",
];
const routeLiterals = new Set();
for (const file of routeSourceFiles) {
  const source = readContractSource(file);
	for (const match of source.matchAll(/"(\/(?:healthz|api[^"? ]*|upnp\/public\.json))"/gu)) routeLiterals.add(match[1]);
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
	if (path !== "/healthz" && path !== "/upnp/public.json" && !path.startsWith("/api/")) throw new Error(`OpenAPI route is outside the living /api surface: ${path}`);
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
    for (const [name, content] of outputs) {
      mkdirSync(dirname(resolve(outputDirectory, name)), { recursive: true });
      writeFileSync(resolve(outputDirectory, name), content, "utf8");
    }
    process.stdout.write(`Generated ${outputs.size} API reference files with digest ${digest.slice(0, 12)}.\n`);
  }
}
