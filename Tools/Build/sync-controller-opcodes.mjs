// SPDX-License-Identifier: MIT
// Explicit source sync only. No board, toolchain, process or deployment actions.
import assert from "node:assert/strict";
import {readFile, writeFile, mkdir} from "node:fs/promises";
import {resolve, dirname} from "node:path";
import {fileURLToPath} from "node:url";
import {execFileSync} from "node:child_process";
import {createHash} from "node:crypto";
import {loadProjectEnv} from "./env.mjs";
loadProjectEnv();
const root = fileURLToPath(new URL("../../", import.meta.url));
const args = process.argv.slice(2);
assert(args.length === 2 && ["--pakar-root", "--pealayer-root"].includes(args[0]), "Use --pakar-root PATH or --pealayer-root PATH");
const pakar = args[0] === "--pakar-root", target = resolve(args[1]);
assert.notEqual(target.toLowerCase(), resolve(root).toLowerCase());
await readFile(resolve(target, pakar ? "libraries/BoardCore/src/Protocol.h" : "src/four_d/protocol.rs"));
const sources = ["Project/ProtocolContract.json", ...["registry.mjs", "published.json", "consumer.mjs"].map(n => "interfaces/controller-wire/" + n)];
execFileSync("git", ["diff", "--exit-code", "HEAD", "--", ...sources], {cwd: root, stdio: "pipe"});
const commit = execFileSync("git", ["rev-parse", "HEAD"], {cwd: root, encoding: "utf8"}).trim();
const previousPath = resolve(target, "interfaces/controller-wire/source-lock.json");
let previous; try { previous = JSON.parse(await readFile(previousPath, "utf8")); } catch (e) { if (e.code !== "ENOENT") throw e; }
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
const files = {}, writes = [];
for (const source of sources) {
  const destination = source === sources[0] ? (pakar ? "docs/pccontroller/ProtocolContract.json" : "interfaces/controller-wire/ProtocolContract.json") : source;
  const path = resolve(target, destination), bytes = await readFile(resolve(root, source));
  let existing; try { existing = await readFile(path); } catch (e) { if (e.code !== "ENOENT") throw e; }
  if (previous && existing && hash(existing) !== previous.files[source] && hash(existing) !== hash(bytes))
    throw Error("Unsynchronized local registry edits must be reviewed first: " + destination);
  files[source] = hash(bytes);
  if (!existing || !existing.equals(bytes)) writes.push([path, bytes]);
}
// Validate every destination before any write. Only these known source files
// and their provenance lock are copied; generated views belong to the consumer.
for (const [path, bytes] of writes) { await mkdir(dirname(path), {recursive:true}); await writeFile(path, bytes); }
await mkdir(dirname(previousPath), {recursive:true});
await writeFile(previousPath, JSON.stringify({owner:"atomicdeploy/PCController", commit, files}, null, 2) + "\n");
console.log("Synced canonical opcode sources from " + commit + "; run the selected consumer generator.");
