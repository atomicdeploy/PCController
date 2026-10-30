#!/usr/bin/env node
// Generates the checked-in AVR and Go views of the native wire registry.
// The JSON source is deliberately simple enough to audit without a generator.

import { readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { loadProjectEnv } from "./env.mjs";
import {execFileSync} from "node:child_process";

loadProjectEnv();

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
export const sourcePath = resolve(scriptDirectory, "../../Project/ProtocolContract.json");
export const cppOutputPath = resolve(scriptDirectory, "../../Project/ProtocolContract.h");
export const goOutputPath = resolve(
  scriptDirectory,
  "../Controller/internal/native/protocol_contract_generated.go",
);

import {validateContract, renderCpp, renderGo, validatePublishedBindings} from "../../interfaces/controller-wire/registry.mjs";
export {validateContract, renderCpp, renderGo} from "../../interfaces/controller-wire/registry.mjs";

async function readContract() {
  const source = await readFile(sourcePath, "utf8");
  return JSON.parse(source);
}

async function checkOutput(path, expected) {
  let actual = "";
  try {
    actual = await readFile(path, "utf8");
  } catch (error) {
    if (error.code !== "ENOENT") {
      throw error;
    }
  }
  if (actual !== expected) {
    throw new Error(`generated contract is stale: ${path}`);
  }
}

// syncProtocolContract writes both checked-in target views, or verifies that a
// clean checkout contains exactly the deterministic generator output.
export async function syncProtocolContract({ check = false } = {}) {
  const contract = validatePublishedBindings(await readContract(), JSON.parse(await readFile(resolve(scriptDirectory, "../../interfaces/controller-wire/published.json"), "utf8")));
  // Check the protected base as well as the fixture. Updating a fixture and
  // regenerating both languages must not make a published renumbering legal.
  if (check && process.env.GITHUB_EVENT_PATH) {
    const event = JSON.parse(await readFile(process.env.GITHUB_EVENT_PATH, "utf8"));
    const base = event.pull_request?.base?.sha ?? event.before;
    if (base && /^[0-9a-f]{40}$/.test(base) && !/^0+$/.test(base)) {
      const root = resolve(scriptDirectory, "../..");
      let previous;
      try { previous = execFileSync("git", ["show", base + ":Project/ProtocolContract.json"], {cwd:root,encoding:"utf8",stdio:["ignore","pipe","pipe"]}); }
      catch {
        execFileSync("git", ["fetch", "--no-tags", "--depth=1", "origin", base], {cwd:root,stdio:"pipe"});
        previous = execFileSync("git", ["show", base + ":Project/ProtocolContract.json"], {cwd:root,encoding:"utf8"});
      }
      const published = JSON.parse(previous);
      validatePublishedBindings(contract, {controller:published, pakar:published.pakar ??
        {envelope:contract.pakar.envelope, opcodes:[], errors:[], reservedOpcodes:[]}});
    }
  }
  const cpp = renderCpp(contract);
  const go = renderGo(contract);
  if (check) {
    await Promise.all([checkOutput(cppOutputPath, cpp), checkOutput(goOutputPath, go)]);
    return;
  }
  await Promise.all([
    writeFile(cppOutputPath, cpp, "utf8"),
    writeFile(goOutputPath, go, "utf8"),
  ]);
}

async function main() {
  const args = new Set(process.argv.slice(2));
  for (const arg of args) {
    if (arg !== "--check") {
      throw new Error(`unknown argument: ${arg}`);
    }
  }
  await syncProtocolContract({ check: args.has("--check") });
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
