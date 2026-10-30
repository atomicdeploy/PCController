// SPDX-License-Identifier: MIT
// Exact, pinned consumer of the PCController-owned registry. Never downloads code
// to execute it; optional upstream verification compares immutable source bytes.
import assert from "node:assert/strict";
import {readFile, writeFile} from "node:fs/promises";
import {createHash} from "node:crypto";
import {fileURLToPath} from "node:url";
import {validatePublishedBindings, renderCpp, renderPakarCpp, renderRust, renderDart} from "./registry.mjs";

const root = new URL("../../", import.meta.url);
const args = process.argv.slice(2);
assert(args.every(arg => ["--pakar", "--pealayer", "--check", "--upstream"].includes(arg)), "Unknown opcode generator argument");
assert.equal(args.includes("--pakar") !== args.includes("--pealayer"), true, "Select exactly one consumer");
const pakar = args.includes("--pakar");
const snapshot = pakar ? "docs/pccontroller/ProtocolContract.json" : "interfaces/controller-wire/ProtocolContract.json";
const read = path => readFile(new URL(path, root), "utf8");
const digest = bytes => createHash("sha256").update(bytes).digest("hex");
const lock = JSON.parse(await read("interfaces/controller-wire/source-lock.json"));
assert.equal(lock.owner, "atomicdeploy/PCController");
assert.match(lock.commit, /^[0-9a-f]{40}$/);
const sources = [
  ["Project/ProtocolContract.json", snapshot],
  ...["registry.mjs", "published.json", "consumer.mjs"].map(name => ["interfaces/controller-wire/" + name, "interfaces/controller-wire/" + name])
];
assert.deepEqual(Object.keys(lock.files).sort(), sources.map(([source]) => source).sort(), "Unrecognized registry source");
for (const [source, destination] of sources) {
  const bytes = await readFile(new URL(destination, root));
  assert.equal(digest(bytes), lock.files[source], "Pinned registry source changed: " + destination);
  if (args.includes("--upstream")) {
    const response = await fetch("https://raw.githubusercontent.com/atomicdeploy/PCController/" + lock.commit + "/" + source,
      {signal: AbortSignal.timeout(15000), redirect: "error"});
    assert(response.ok, "Cannot verify canonical opcode source: HTTP" + response.status);
    assert.equal(digest(new Uint8Array(await response.arrayBuffer())), lock.files[source], "Canonical opcode owner differs: " + source);
  }
}
const contract = validatePublishedBindings(JSON.parse(await read(snapshot)), JSON.parse(await read("interfaces/controller-wire/published.json")));
const outputs = pakar ? [
  ["libraries/BoardCore/src/ProtocolOpcodes.h", renderPakarCpp(contract)],
  ["libraries/BoardCore/src/pccontroller/ProtocolContract.h", renderCpp(contract)],
  ["desktop/src/protocol_opcodes.rs", renderRust(contract)],
  ["mobile/lib/protocol_opcodes.dart", renderDart(contract)]
] : [["src/four_d/protocol_opcodes.rs", renderRust(contract)]];
for (const [path, expected] of outputs) {
  let actual = ""; try { actual = await read(path); } catch (error) { if (error.code !== "ENOENT") throw error; }
  if (actual.replaceAll("\r\n", "\n") === expected) continue;
  assert(!args.includes("--check"), "Generated opcodes are stale: " + path);
  await writeFile(new URL(path, root), expected);
}
console.log("Canonical opcode ownership, published allocations and all selected consumer views agree.");
