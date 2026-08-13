import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  cppOutputPath,
  goOutputPath,
  renderCpp,
  renderGo,
  sourcePath,
  syncProtocolContract,
  validateContract,
} from "./generate-protocol-contract.mjs";

async function loadContract() {
  return JSON.parse(await readFile(sourcePath, "utf8"));
}

test("generated protocol registry preserves the published native values", async () => {
  const contract = validateContract(await loadContract());
  const cpp = renderCpp(contract);
  const go = renderGo(contract);

  assert.match(cpp, /HostMenuDirectory = 0x42/);
  assert.match(cpp, /HostMenuContent = 0x43/);
  assert.match(cpp, /HostMenuStateGet = 0x44/);
  assert.match(cpp, /StatusLedChanged = 0x9E/);
  assert.match(cpp, /Unsafe = 0x06/);
  assert.match(go, /OpHostMenuDirectory\s+byte = 0x42/);
  assert.match(go, /OpHostMenuContent\s+byte = 0x43/);
  assert.match(go, /OpHostMenuStateGet\s+byte = 0x44/);
  assert.match(go, /ErrorUnsafe\s+byte = 6/);
});

test("checked-in protocol views are exactly reproducible", async () => {
  const contract = await loadContract();
  const [cpp, go] = await Promise.all([
    readFile(cppOutputPath, "utf8"),
    readFile(goOutputPath, "utf8"),
  ]);
  assert.equal(cpp, renderCpp(contract));
  assert.equal(go, renderGo(contract));
  await syncProtocolContract({ check: true });
});

test("registry validation rejects extension or error-number drift", async () => {
  const contract = await loadContract();
  const wrongExtension = structuredClone(contract);
  wrongExtension.opcodes.find((entry) => entry.cpp === "HostMenuContent").value = 0x46;
  assert.throws(() => validateContract(wrongExtension), /strictly ordered by value/);

  const wrongError = structuredClone(contract);
  wrongError.errors.find((entry) => entry.cpp === "Unsafe").value = 7;
  assert.throws(() => validateContract(wrongError), /Unsafe must remain 6/);
});
