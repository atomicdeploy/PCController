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
    rpcSchemaID: "https://nimbus-link.local/schemas/jsonrpc-v1.json",
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
  assert.match(first.stdout, /^API reference is current: 107 RPC methods, 38 REST paths, digest [a-f0-9]{12}\.\n$/u);
});
