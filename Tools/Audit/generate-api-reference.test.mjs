import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { createSchemaResolver } from "./explorer/schema-resolver.mjs";
import { extractRequestShapes } from './explorer/request-shapes.mjs';

import {
  extractGoCaseMethods,
  productIdentity,
  validateMethodCatalog,
} from "./generate-api-reference.mjs";

const directory = dirname(fileURLToPath(import.meta.url));
const root = resolve(directory, "..", "..");
const script = join(directory, "generate-api-reference.mjs");
const outputDirectory = join(root, "Tools", "Controller", "api");

test('all HTTP operations have purpose, response meaning, use cases and described parameters',()=>{
  const spec=JSON.parse(readFileSync(join(outputDirectory,'openapi.json'),'utf8'));
  const {expand}=createSchemaResolver(spec);
  for(const [path,operations] of Object.entries(spec.paths)) for(const [method,operation] of Object.entries(operations)) {
    assert.notEqual(operation.summary,operation.description,`${method} ${path}`);
    assert.ok(operation.summary.length<110,`Concise title: ${method} ${path}`);
    assert.match(operation.description,/Use case:/u);
    assert.ok(operation['x-use-case']);
    for(const parameter of operation.parameters??[]) assert.ok(parameter.description,`${path} ${parameter.name}`);
    for(const [,name] of path.matchAll(/\{([^}]+)\}/gu)) assert.ok(operation.parameters?.some(p=>p.in==='path'&&p.name===name&&p.required),`${path} path.${name}`);
    for(const response of Object.values(operation.responses)) if(!response.$ref) {
      assert.ok(response.description);
      assert.notEqual(response.description,'Accepted response');
    }
    const body=expand(operation.requestBody?.content?.['application/json']?.schema);
    for(const [field,schema] of Object.entries(body?.properties??{})) assert.ok(schema.description,`${method} ${path} request.${field}`);
  }
  for(const tag of spec.tags) {assert.ok(tag.description?.length>50,tag.name);assert.ok(tag['x-summary']);}
  assert.match(spec.info.description,/DS18B20/u);
  assert.match(spec.info.description,/Idempotency key/u);
  assert.match(spec.info.description,/RF.*learning/su);
  const bundle=readFileSync(join(outputDirectory,'assets/scalar.js'),'utf8');
  assert.match(bundle,/showOperationId: false/u);
});

test('discovery and programming documentation distinguish inventory, staging, acceptance and file transfer',()=>{
  const spec=JSON.parse(readFileSync(join(outputDirectory,'openapi.json'),'utf8'));
  const workflow=spec.paths['/api/discovery/github/workflow'].post;
  assert.equal(workflow.summary,'Find GitHub Actions build artifacts');
  assert.match(workflow.description,/does not run a workflow/u);
  assert.match(workflow.requestBody.content['application/json'].schema.properties.workflow.description,/not the YAML filename/u);
  assert.equal(workflow.responses['200'].content['application/json'].schema.properties.candidates.items.properties.archive_sha256.description.includes('outer archive'),true);
  assert.equal(spec.paths['/api/discovery/stage'].post.responses['200'],undefined);
  assert.ok(spec.paths['/api/discovery/stage'].post.responses['202']);
  assert.ok(spec.paths['/api/artifacts/upload'].post.responses['201']);
  assert.ok(spec.paths['/api/artifacts/upload'].post.requestBody.content['application/octet-stream']);
  assert.ok(spec.paths['/api/board/firmware/download'].get.responses['206']);
  assert.equal(spec.paths['/api/board/firmware/download'].head.responses['200'].content,undefined);
  assert.equal(spec.paths['/api/diagnostics/execution-trace'].post.requestBody,undefined);
  assert.match(spec.paths['/api/diagnostics/execution-trace'].post.responses['200'].description,/not a gzip CPU profile/u);
  assert.ok(spec.paths['/api/integrations/device/{path}'].get.responses['404']);
  assert.equal(spec.paths['/api/integrations/device/{path}'].get.responses['200'],undefined);
  assert.equal(spec.paths['/api/webhooks/inbound'].get['x-idempotency'],'non-idempotent');
  assert.ok(spec.paths['/api/pwm/channel'].put);
  assert.equal(spec.paths['/api/pwm/channel'].get['x-required-capability'],'read');
  const rpc=JSON.parse(readFileSync(join(outputDirectory,'jsonrpc.schema.json'),'utf8'))['x-methods'];
  assert.match(rpc['controller.discovery.github.workflow'].request_fields.properties.repository.description,/owner\/repository/u);
  assert.equal(rpc['controller.discovery.github.workflow'].result_schema.properties.candidates.type,'array');
  assert.equal(rpc['controller.artifact.upload'].request_fields.properties.data.contentEncoding,'base64');
  assert.equal(rpc['controller.artifact.upload'].request_fields.properties.file,undefined);
  assert.match(rpc['controller.artifact.upload'].description,/base64-encoded/u);
  assert.match(rpc['controller.osd.show'].description,/concurrent stack/u);
});

test('RPC purpose text is distinct from its title and never a mechanical surface label',()=>{
  const spec=JSON.parse(readFileSync(join(outputDirectory,'jsonrpc.schema.json'),'utf8'));
  const normalize=s=>s.replace(/[.]/gu,'').trim().toLowerCase();
  for(const [name,method] of Object.entries(spec['x-methods'])) {
    assert.ok(method.title,name);assert.ok(method.description,name);
    assert.notEqual(normalize(method.title),normalize(method.description),name);
    assert.doesNotMatch(method.description,/^(Read|Invoke) the .* (surface|operation)\.$/u,name);
  }
});

test('Go schema extraction separates same-named package types and preserves JSON byte/time encoding',()=>{
  const shapes=extractRequestShapes(root,[{Name:'alpha.go',Text:'package alpha; type Manifest struct { Data []byte `json:"data"`; Count int `json:"count"` }'},{Name:'beta.go',Text:'package beta; import "time"; type Manifest struct { At time.Time `json:"at"`; Nested alpha.Manifest `json:"nested"` }'}]);
  assert.equal(shapes.$types['alpha.Manifest'].properties.data.type,'string');
  assert.equal(shapes.$types['alpha.Manifest'].properties.data.contentEncoding,'base64');
  assert.equal(shapes.$types['beta.Manifest'].properties.at.format,'date-time');
  assert.equal(shapes.$types['beta.Manifest'].properties.nested.properties.count.type,'integer');
  assert.equal(shapes.$types['alpha.Manifest'].required,undefined);
});

test('request extraction does not confuse shadowed command/application params',()=>{
  const shapes=extractRequestShapes(root,[{Name:'dispatch.go',Text:'package ipcjson; func dispatch(method string, raw []byte) { switch method {case "controller.example": var params struct { Command string `json:"command"` }; decodeParams(raw, &params); if true { var params struct { Kind string `json:"kind"` }; decodeParams(raw, &params) } } }'}]);
  assert.deepEqual(Object.keys(shapes['controller.example'].properties),['command']);
  const rpc=JSON.parse(readFileSync(join(outputDirectory,'jsonrpc.schema.json'),'utf8'));
  assert.deepEqual(Object.keys(rpc['x-methods']['controller.command.execute'].request_fields.properties),['command']);
});

test("extracts controller methods from one-line and wrapped Go case clauses", () => {
  const dispatched = extractGoCaseMethods([
    `switch method {
case "controller.linear":
	return handleLinear()
case "controller.first",
	"controller.second",
	"controller.third":
	return handleWrapped()
case "unrelated.method":
	return nil
}
const example = "controller.not-a-case"`,
    "switch method {\r\n\tcase \"controller.windows\",\r\n\t\t\"controller.multiline\":\r\n\t\treturn nil\r\n}",
  ]);

  assert.deepEqual([...dispatched].sort(), [
    "controller.first",
    "controller.linear",
    "controller.multiline",
    "controller.second",
    "controller.third",
    "controller.windows",
  ]);
});

test("rejects a catalog-only method that no dispatch case recognizes", () => {
  assert.throws(() => validateMethodCatalog({
    catalogued: new Set(["controller.status", "controller.stale"]),
    dispatched: new Set(["controller.status"]),
  }), /catalog contains methods absent from dispatch sources: controller\.stale/u);
});

test("rejects a dispatched method missing from the catalog", () => {
  assert.throws(() => validateMethodCatalog({
    catalogued: new Set(["controller.status"]),
    dispatched: new Set(["controller.status", "controller.undocumented"]),
  }), /catalog is missing dispatched methods: controller\.undocumented/u);
});

test("derives public API titles and schema ID from product metadata", () => {
  assert.deepEqual(productIdentity({
    name: "@example/internal-package",
    productName: "Nimbus Console",
    productProtocol: "Nimbus-Link",
  }), {
    productName: "Nimbus Console",
    productProtocol: "nimbus-link",
    httpTitle: "Nimbus Console HTTP API",
		rpcSchemaID: "https://nimbus-link.local/schemas/jsonrpc.json",
    rpcTitle: "Nimbus Console JSON-RPC 2.0",
    eventTitle: "Nimbus Console event and WebSocket API",
    referenceTitle: "Nimbus Console API Reference",
    referenceHeading: "Nimbus Console API",
  });
  assert.throws(
    () => productIdentity({ productName: "Nimbus", productProtocol: "not a URI scheme" }),
    /URI-safe protocol name/u,
  );

  const metadata = JSON.parse(readFileSync(join(root, "Tools", "Controller", "web", "package.json"), "utf8"));
  const expected = productIdentity(metadata);
  const openapi = JSON.parse(readFileSync(join(outputDirectory, "openapi.json"), "utf8"));
  const asyncapi = JSON.parse(readFileSync(join(outputDirectory, "asyncapi.json"), "utf8"));
  const rpcSchema = JSON.parse(readFileSync(join(outputDirectory, "jsonrpc.schema.json"), "utf8"));
  const reference = readFileSync(join(outputDirectory, "reference.html"), "utf8");

  assert.equal(openapi.info.title, expected.httpTitle);
  assert.equal(asyncapi.info.title, expected.eventTitle);
  assert.equal(rpcSchema.$id, expected.rpcSchemaID);
  assert.equal(rpcSchema.title, expected.rpcTitle);
  assert.equal(reference.includes(`<title>${expected.referenceTitle}</title>`), true);
  assert.equal(reference.includes(`${expected.referenceHeading}</h1>`), true);
  assert.equal(openapi.components.securitySchemes.tokenHeader.name, "X-PCController-Token");
  assert.equal(openapi.paths["/api/session/ticket"].post.responses["409"].description.includes("disabled"), true);
  assert.equal(openapi.paths["/api/auth/server-proof"].get.security.length, 0);
  assert.equal(openapi.paths["/api/auth/server-proof"].get.parameters[0].name, "X-PCController-Nonce");
  assert.equal(openapi.components.schemas.SessionTicket.properties.ticket.writeOnly, true);
  assert.equal(openapi.components.schemas.OpcodeRequest.properties.opcode.maximum, 255);
  assert.equal(openapi.components.schemas.AppInstanceReport.properties.lease_seconds.maximum, 300);
  assert.equal(openapi.paths["/api/app/instances"].delete.parameters[0].required, true);
  assert.equal(openapi.paths["/api/app/navigate"].post.requestBody.content["application/json"].schema.$ref, "#/components/schemas/AppNavigation");
  assert.equal(asyncapi.components.securitySchemes.browserTicket.name, "Sec-WebSocket-Protocol");
  assert.equal(JSON.stringify(asyncapi).includes("access_token"), false);
});

test("publishes routing pins, logical PWM conversion and non-retryable output contracts", () => {
  const schema = JSON.parse(readFileSync(join(outputDirectory, "jsonrpc.schema.json"), "utf8"));
  for (const method of ["controller.channel.set", "controller.output.set"]) {
    const contract = schema["x-methods"][method];
    assert.equal(contract.capability, "board_commands");
    assert.equal(contract.idempotency, "non-idempotent");
    const request = schema.$defs[contract.params_schema.$ref.split("/").at(-1)];
    for (const pin of ["expected_host_instance_id", "expected_connection_generation", "board_identity", "expected_revision"]) assert.ok(request.required.includes(pin));
    assert.equal(request.additionalProperties, false);
    assert.equal(request.properties.percent_milli.maximum, 100000);
    assert.equal(request.properties.value.maximum, 4095);
    assert.ok(request.oneOf.some(branch => branch.required.includes("expected_pwm_configuration_revision")));
  }
  assert.equal(schema["x-methods"]["controller.channel_routing.update"].capability, "host_configuration");
  assert.equal(schema.$defs.ChannelRouting.properties.channels.maxItems, 15);
  assert.equal(schema.$defs.PinnedOutputResult.properties.outcome.const, "confirmed");
  assert.ok(schema.$defs.PinnedOutputResult.required.includes("observed_value"));
  assert.deepEqual(schema.$defs.PinnedOutputErrorData.properties.outcome.enum, ["rejected", "unknown"]);
  assert.equal(schema.$defs.PhysicalOutputRequest.properties.target.enum.includes("relay.1"), false);
  assert.equal(schema.$defs.PhysicalOutputRequest.properties.target.enum.includes("pwm.11"), false);
});

test("generates typed app action requests and per-target outcomes", () => {
	const openapi = JSON.parse(readFileSync(join(outputDirectory, "openapi.json"), "utf8"));
	const rpcSchema = JSON.parse(readFileSync(join(outputDirectory, "jsonrpc.schema.json"), "utf8"));

	assert.equal(openapi.components.schemas.AppActionRequest.properties.operation_id.pattern, "^[A-Za-z0-9._:-]{1,180}$");
	assert.equal(openapi.components.schemas.AppActionRequest.properties.timeout_ms.maximum, 30000);
	assert.equal(openapi.components.schemas.AppAction.properties.metadata.properties.operation_delivery_id.pattern, "^[A-Za-z0-9._:-]{1,180}$");
	assert.equal(openapi.components.schemas.AppAction.properties.metadata.properties.operation_expires_at.format, "date-time");
	assert.equal(openapi.components.schemas.ActionAck.required.includes("delivery_id"), true);
	assert.equal(openapi.components.schemas.ActionAck.properties.delivery_id.pattern, "^[A-Za-z0-9._:-]{1,180}$");
	assert.equal(openapi.components.schemas.ActionAck.properties.state.enum.includes("timeout"), false);
	assert.equal(openapi.components.schemas.ActionTargetOutcome.properties.state.enum.includes("timeout"), true);
	assert.equal(openapi.components.schemas.ActionOperation.properties.state.enum.includes("partial"), true);
	assert.equal(
		openapi.paths["/api/app/action"].post.requestBody.content["application/json"].schema.$ref,
		"#/components/schemas/AppActionRequest",
	);
	assert.equal(
		openapi.paths["/api/app/action"].post.responses["202"].content["application/json"].schema.$ref,
		"#/components/schemas/ActionOperationEnvelope",
	);
	assert.equal(openapi.components.schemas.ActionOperationEnvelope.required.includes("operation"), true);
	assert.equal(openapi.components.schemas.AppActionSubmitEnvelope, undefined);
	assert.equal(
		openapi.paths["/api/app/action/ack"].post.requestBody.content["application/json"].schema.$ref,
		"#/components/schemas/ActionAck",
	);
	assert.equal(
		openapi.paths["/api/app/action/ack"].post.responses["200"].content["application/json"].schema.$ref,
		"#/components/schemas/ActionOperationEnvelope",
	);
	assert.deepEqual(openapi.paths["/api/app/action/outcome"].get.parameters.map(({ name, required }) => ({ name, required })), [
		{ name: "operation_id", required: true },
	]);
	assert.equal(
		openapi.paths["/api/app/action/outcome"].get.responses["200"].content["application/json"].schema.$ref,
		"#/components/schemas/ActionOperationEnvelope",
	);
	assert.equal(rpcSchema.$defs.AppActionRequest.properties.timeout_ms.maximum, 30000);
	assert.equal(rpcSchema.$defs.ActionAck.required.includes("delivery_id"), true);
	assert.equal(rpcSchema.$defs.ActionOutcomeRequest.required.includes("operation_id"), true);
	const actionRequestRule = rpcSchema.$defs.request.allOf.find((rule) =>
		rule.if?.properties?.method?.const === "controller.app.action");
	assert.equal(actionRequestRule.then.properties.params.$ref, "#/$defs/AppActionRequest");
	assert.equal(actionRequestRule.then.required.includes("params"), true);
	assert.equal(rpcSchema["x-methods"]["controller.app.action"].params_schema.$ref, "#/$defs/AppActionRequest");
	assert.equal(rpcSchema["x-methods"]["controller.app.action"].result_schema.$ref, "#/$defs/ActionOperationEnvelope");
	assert.equal(rpcSchema["x-methods"]["controller.app.action.ack"].params_schema.$ref, "#/$defs/ActionAck");
	assert.equal(rpcSchema["x-methods"]["controller.app.action.outcome"].result_schema.$ref, "#/$defs/ActionOperationEnvelope");
});

test("--check is read-only and stable across repeated runs", () => {
  const run = () => spawnSync(process.execPath, [script, "--check"], {
    cwd: root,
    encoding: "utf8",
    windowsHide: true,
  });
  const first = run();
  const second = run();

  assert.equal(first.status, 0, first.stderr);
  assert.equal(second.status, 0, second.stderr);
  assert.equal(second.stdout, first.stdout);
  const rpcSchema = JSON.parse(readFileSync(join(outputDirectory, "jsonrpc.schema.json"), "utf8"));
  const openapi = JSON.parse(readFileSync(join(outputDirectory, "openapi.json"), "utf8"));
  const methodCount = Object.keys(rpcSchema["x-methods"]).length;
  const pathCount = Object.keys(openapi.paths).length;
  assert.match(
    first.stdout,
    new RegExp(`^API reference is current: ${methodCount} RPC methods, ${pathCount} REST paths, digest [a-f0-9]{12}\\.\\n$`, "u"),
  );
});

test('explorer includes complete protocol catalogs and source-derived JSON field names', () => {
  const rpc = JSON.parse(readFileSync(join(outputDirectory,'jsonrpc.schema.json'),'utf8'));
  const openapi = JSON.parse(readFileSync(join(outputDirectory,'openapi.json'),'utf8'));
  const page = readFileSync(join(outputDirectory,'rpc.html'),'utf8');
  assert.equal(rpc['x-methods']['controller.osd.show'].params_schema.$ref,'#/$defs/OSDRequest');
  assert.equal(rpc.$defs.OSDAction.properties.label.maxLength,64);
  assert.equal(rpc.$defs.OSDRequest.properties.actions.maxItems,5);
	assert.equal(rpc.$defs.OSDRequest.properties.behavior.default,'replace');
	assert.ok(rpc.$defs.OSDRequest.properties.background.enum.includes('acrylic'));
	assert.equal(rpc.$defs.OSDStatus.properties.items.maxItems,6);
	assert.equal(rpc.$defs.OSDRequest.properties.sound.default,'none');
	assert.ok(rpc.$defs.OSDRequest.properties.sound.enum.includes('warning'));
	assert.equal(rpc.$defs.OSDRequest.properties.flash_taskbar.default,false);
	assert.ok(rpc.$defs.OSDStatus.properties.attention.properties.taskbar.enum.includes('no-window'));
	assert.deepEqual(rpc.$defs.OSDStatus.properties.text_renderer.enum, ['directwrite', 'gdi']);
  assert.equal(openapi.paths['/api/osd'].post.requestBody.content['application/json'].schema.$ref,'#/components/schemas/OSDRequest');
  assert.ok(openapi.paths['/api/osd'].delete);
  for(const method of Object.keys(rpc['x-methods'])) assert.ok(page.includes(method),method);
  const fields = rpc['x-methods']['controller.pwm.set'].request_fields.properties;
  for(const name of ['channel','percent','raw_value','expected_host_instance_id']) assert.ok(fields[name],name);
  assert.equal(fields.raw_value.type,'integer');
  assert.equal(fields.percent.type,'number');
  assert.equal(rpc['x-methods']['controller.pwm.set'].request_fields.required,undefined,'Go field presence must not invent semantic requiredness');
  assert.equal(rpc['x-methods']['controller.pwm.set'].request_template.percent,50);
  assert.equal(rpc['x-methods']['controller.pwm.set'].request_template.raw_value,undefined,'PWM template must not combine mutually exclusive selectors');
  assert.ok(openapi.paths['/api/sensors/board'].get.responses['200'].content['application/json'].schema.$ref);
  assert.ok(openapi.paths['/api/sensors/metrics'].get.responses['200'].content['text/plain']);
  assert.deepEqual(openapi.paths['/api/sensors/board'].get.tags,['sensors']);
  const bootstrap=readFileSync(join(outputDirectory,'assets/scalar.js'),'utf8');
  for(const safety of ['hideTestRequestButton: true','persistAuth: false','telemetry: false']) assert.ok(bootstrap.includes(safety),safety);
});

test('WebSocket explorer exposes connection guides, resolved RPC contracts and every emitted notification', () => {
  const events = JSON.parse(readFileSync(join(outputDirectory, 'asyncapi.json'), 'utf8'));
  const rpc = JSON.parse(readFileSync(join(outputDirectory, 'jsonrpc.schema.json'), 'utf8'));
  const page = readFileSync(join(outputDirectory, 'protocols.html'), 'utf8');
  const client = readFileSync(join(outputDirectory, 'assets/catalog.js'), 'utf8');
  for (const name of ['reference.html', 'rpc.html', 'protocols.html']) {
    const html = readFileSync(join(outputDirectory, name), 'utf8');
    assert.ok(html.includes('WebSocket &amp; events'), name);
    assert.ok(html.includes('href="asyncapi.json"'), name);
  }
  assert.ok(page.includes('id="rpc-contract"'), 'external RPC references must resolve offline');
  assert.ok(client.includes('x-socket-io-events'), 'Socket.IO event metadata must use the published extension key');
  const guides = events['x-connection-guides'];
  assert.deepEqual(guides.map(guide => guide.id), ['websocket', 'socketio']);
  const quickstart = guides[0].sections.find(section => section.code).code;
  for (const method of quickstart.matchAll(/method: '(controller\.[a-z.]+)'/gu)) {
    assert.ok(rpc['x-methods'][method[1]], method[1]);
  }
  assert.ok(quickstart.includes('config.websocket_path'));
  assert.ok(quickstart.includes("'wss:'"));
  assert.ok(!quickstart.includes('auth_token'));
  const subscription = rpc.$defs.Subscription;
  assert.equal(rpc['x-methods']['controller.subscribe'].params_schema.$ref, '#/$defs/Subscription');
  const source = readFileSync(join(root, 'Tools/Controller/internal/ipcjson/ipc.go'), 'utf8');
  const fields = source.match(/type wsSubscription struct \{([\s\S]*?)\n\}/u)[1];
  for (const match of fields.matchAll(/json:"([^",]+)/gu)) assert.ok(subscription.properties[match[1]], match[1]);
  const notifications = new Set(Object.values(events.components.messages).map(message => message.payload?.properties?.method?.const).filter(Boolean));
  for (const match of source.matchAll(/wsNotification\{[\s\S]*?Method:\s*"(controller\.[a-z.]+)"/gu)) {
    assert.ok(notifications.has(match[1]), `undocumented notification ${match[1]}`);
    assert.ok(rpc.$defs.notification.properties.method.enum.includes(match[1]), match[1]);
    assert.ok(events['x-socket-io-events'].outgoing.includes(match[1]), match[1]);
  }
  assert.ok(guides[1].sections.some(section => section.code?.startsWith('42["subscribe"')));
});

test('offline schema resolver expands nested response branches and preserves cycle edges', () => {
  const events = JSON.parse(readFileSync(join(outputDirectory, 'asyncapi.json'), 'utf8'));
  const rpc = JSON.parse(readFileSync(join(outputDirectory, 'jsonrpc.schema.json'), 'utf8'));
  const { expand } = createSchemaResolver(events, rpc);
  const response = expand(events.components.messages.JSONRPCResponse.payload);
  assert.equal(response.oneOf[0].type, 'object');
  assert.ok(response.oneOf[0].required.includes('result'));
  assert.equal(response.oneOf[1].properties.error.properties.code.type, 'integer');
  assert.equal(response.oneOf[0].properties.id.oneOf[0].type, 'string');
  assert.ok(!JSON.stringify(response).includes('"$ref"'), 'all finite response branches must resolve');
  const finite = expand(events.components.messages.JSONRPCRequest.payload);
  assert.ok(!JSON.stringify(finite).includes('"$ref"'), 'request allOf/parameter schemas must also resolve');
  const graph = { $defs: { node: { type: 'object', properties: { child: { $ref: '#/$defs/node' } } }, 'a/b~c': { type: 'string' }, forbidden: false } };
  const resolver = createSchemaResolver(graph);
  assert.equal(resolver.expand({ $ref: '#/$defs/node' }).properties.child.$ref, '#/$defs/node');
  assert.equal(resolver.expand({ $ref: '#/$defs/a~1b~0c' }).type, 'string');
  assert.equal(resolver.expand({ $ref: '#/$defs/forbidden' }), false);
  assert.deepEqual(resolver.expand({ $ref: 'https://untrusted.invalid/schema' }), { $ref: 'https://untrusted.invalid/schema' });
  assert.equal(resolver.expand({ $ref: '#/$defs/a~1b~0c', description: 'local override' }).description, 'local override');
});
