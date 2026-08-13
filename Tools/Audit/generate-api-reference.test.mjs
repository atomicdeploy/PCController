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
  assert.equal(reference.includes(`<h1>${expected.referenceHeading}</h1>`), true);
  assert.equal(openapi.components.securitySchemes.tokenHeader.name, "X-PCController-Token");
  assert.equal(openapi.paths["/api/session/ticket"].post.responses["201"], undefined);
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
		"#/components/schemas/AppActionSubmitEnvelope",
	);
	assert.deepEqual(openapi.components.schemas.AppActionSubmitEnvelope.required, ["accepted"]);
	assert.equal(openapi.components.schemas.AppActionSubmitEnvelope.properties.operation.$ref, "#/components/schemas/ActionOperation");
	assert.equal(openapi.components.schemas.ActionOperationEnvelope.required.includes("operation"), true);
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
	assert.equal(rpcSchema["x-methods"]["controller.app.action"].result_schema.$ref, "#/$defs/AppActionSubmitEnvelope");
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
