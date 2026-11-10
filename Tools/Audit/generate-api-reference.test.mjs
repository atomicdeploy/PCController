import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  extractGoCaseMethods,
  productIdentity,
  validateMethodCatalog,
} from "./generate-api-reference.mjs";

const directory = dirname(fileURLToPath(import.meta.url));
const root = resolve(directory, "..", "..");
const script = join(directory, "generate-api-reference.mjs");
const outputDirectory = join(root, "Tools", "Controller", "api");

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
