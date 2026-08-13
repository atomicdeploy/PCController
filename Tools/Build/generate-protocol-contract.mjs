#!/usr/bin/env node
// Generates the checked-in AVR and Go views of the native wire registry.
// The JSON source is deliberately simple enough to audit without a generator.

import { readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { loadProjectEnv } from "./env.mjs";

loadProjectEnv();

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
export const sourcePath = resolve(scriptDirectory, "../../Project/ProtocolContract.json");
export const cppOutputPath = resolve(scriptDirectory, "../../Project/ProtocolContract.h");
export const goOutputPath = resolve(
  scriptDirectory,
  "../Controller/internal/native/protocol_contract_generated.go",
);

const identifier = /^[A-Za-z][A-Za-z0-9]*$/;
const wireName = /^[A-Z][A-Z0-9_]*$/;
const requiredOpcodes = new Map([
  ["HostMenuDirectory", 0x42],
  ["HostMenuContent", 0x43],
  ["HostMenuStateGet", 0x44],
]);
const requiredErrors = new Map([
  ["NoError", 0],
  ["BadEnvelope", 1],
  ["Unsupported", 2],
  ["BadPayload", 3],
  ["HardwareUnavailable", 4],
  ["Busy", 5],
  ["Unsafe", 6],
]);

function requireInteger(value, label, minimum = 0, maximum = 0xFF) {
  if (!Number.isInteger(value) || value < minimum || value > maximum) {
    throw new Error(`${label} must be an integer in ${minimum}..${maximum}`);
  }
}

function requireIdentifier(value, label) {
  if (typeof value !== "string" || !identifier.test(value)) {
    throw new Error(`${label} must be an identifier`);
  }
}

function requireWireName(value, label) {
  if (typeof value !== "string" || !wireName.test(value)) {
    throw new Error(`${label} must be an upper-case wire name`);
  }
}

function validateRegistry(entries, label, fields) {
  if (!Array.isArray(entries) || entries.length === 0) {
    throw new Error(`${label} must be a non-empty array`);
  }
  const values = new Set();
  const cpp = new Set();
  const go = new Set();
  let previous = -1;
  for (const [index, entry] of entries.entries()) {
    if (entry === null || typeof entry !== "object") {
      throw new Error(`${label}[${index}] must be an object`);
    }
    requireInteger(entry.value, `${label}[${index}].value`);
    requireIdentifier(entry.cpp, `${label}[${index}].cpp`);
    requireIdentifier(entry.go, `${label}[${index}].go`);
    if (fields.includes("wireName")) {
      requireWireName(entry.wireName, `${label}[${index}].wireName`);
    }
    if (values.has(entry.value) || cpp.has(entry.cpp) || go.has(entry.go)) {
      throw new Error(`${label}[${index}] duplicates a value or exported name`);
    }
    if (entry.value <= previous) {
      throw new Error(`${label} must be strictly ordered by value`);
    }
    values.add(entry.value);
    cpp.add(entry.cpp);
    go.add(entry.go);
    previous = entry.value;
  }
  return { values, cpp, go };
}

// validateContract rejects ambiguous registries before either public target is
// rendered. The exact legacy extensions are guarded here instead of relying on
// a reviewer to notice a hexadecimal value changing in a generated diff.
export function validateContract(contract) {
  if (contract === null || typeof contract !== "object" || contract.schema !== 1) {
    throw new Error("protocol contract schema must be 1");
  }
  const envelope = contract.envelope;
  if (envelope === null || typeof envelope !== "object") {
    throw new Error("protocol contract requires an envelope object");
  }
  requireInteger(envelope.magic, "envelope.magic");
  requireInteger(envelope.revision, "envelope.revision");
  requireInteger(envelope.maximumPayload, "envelope.maximumPayload", 1, 247);
  requireInteger(envelope.rawFrameOverhead, "envelope.rawFrameOverhead", 6, 16);
  requireInteger(
    envelope.maximumEncodedFrameOverhead,
    "envelope.maximumEncodedFrameOverhead",
    2,
    16,
  );

  const opcodeRegistry = validateRegistry(contract.opcodes, "opcodes", ["wireName"]);
  const errorRegistry = validateRegistry(contract.errors, "errors", []);
  const reserved = contract.reservedOpcodes ?? [];
  if (!Array.isArray(reserved)) {
    throw new Error("reservedOpcodes must be an array");
  }
  const reservedValues = new Set();
  for (const [index, entry] of reserved.entries()) {
    if (entry === null || typeof entry !== "object") {
      throw new Error(`reservedOpcodes[${index}] must be an object`);
    }
    requireInteger(entry.value, `reservedOpcodes[${index}].value`);
    if (typeof entry.note !== "string" || entry.note.trim() === "") {
      throw new Error(`reservedOpcodes[${index}].note must be descriptive`);
    }
    if (opcodeRegistry.values.has(entry.value) || reservedValues.has(entry.value)) {
      throw new Error(`reserved opcode 0x${formatHex(entry.value)} conflicts`);
    }
    reservedValues.add(entry.value);
  }
  for (const [name, value] of requiredOpcodes) {
    const entry = contract.opcodes.find((candidate) => candidate.cpp === name);
    if (!entry || entry.value !== value) {
      throw new Error(`${name} must remain 0x${formatHex(value)}`);
    }
  }
  for (const [name, value] of requiredErrors) {
    const entry = contract.errors.find((candidate) => candidate.cpp === name);
    if (!entry || entry.value !== value) {
      throw new Error(`${name} must remain ${value}`);
    }
  }
  if (envelope.magic !== 0xA5 || envelope.revision !== 1 || envelope.maximumPayload !== 48) {
    throw new Error("the published native envelope must remain A5/revision-1/48-byte payload");
  }
  return contract;
}

function formatHex(value) {
  return value.toString(16).toUpperCase().padStart(2, "0");
}

function generatedBanner(source) {
  return [
    "// Code generated by Tools/Build/generate-protocol-contract.mjs; DO NOT EDIT.",
    `// Source: ${source}`,
    "",
  ];
}

function cppEnum(entries, label) {
  const lines = [`enum ${label} : uint8_t {`];
  for (const entry of entries) {
    lines.push(`  ${entry.cpp} = 0x${formatHex(entry.value)},`);
  }
  lines.push("};", "");
  return lines;
}

function goConstBlock(entries, valueFor) {
  const width = Math.max(...entries.map((entry) => entry.name.length));
  return [
    "const (",
    ...entries.map(
      (entry) => `\t${entry.name.padEnd(width)}${entry.type ? ` ${entry.type}` : ""} = ${valueFor(entry)}`,
    ),
    ")",
  ];
}

// renderCpp keeps the established ControllerProtocol::* names and C++11-only
// surface. AVR receives no allocation, clock, or transport dependency here.
export function renderCpp(contract) {
  validateContract(contract);
  const { envelope, opcodes, reservedOpcodes, errors } = contract;
  const reservations = new Map(reservedOpcodes.map((entry) => [entry.value, entry.note]));
  const lines = [
    "#pragma once",
    "",
    ...generatedBanner("Project/ProtocolContract.json"),
    "// Target-neutral native wire contract. Keep numeric values stable;",
    "// capabilities advertise optional support rather than renumbering operations.",
    "",
    "#include <stdint.h>",
    "",
    "namespace ControllerProtocol {",
    "namespace WireContract {",
    "",
    `constexpr uint8_t Magic = 0x${formatHex(envelope.magic)};`,
    `constexpr uint8_t EnvelopeRevision = ${envelope.revision};`,
    `constexpr uint8_t MaximumPayload = ${envelope.maximumPayload};`,
    `constexpr uint8_t RawFrameOverhead = ${envelope.rawFrameOverhead};`,
    "constexpr uint8_t MaximumRawFrame = MaximumPayload + RawFrameOverhead;",
    `constexpr uint8_t MaximumEncodedFrame = MaximumRawFrame + ${envelope.maximumEncodedFrameOverhead};`,
    "",
    "// Opcode is the stable native request, response, and event registry.",
    "enum Opcode : uint8_t {",
  ];
  for (const entry of opcodes) {
    lines.push(`  ${entry.cpp} = 0x${formatHex(entry.value)},`);
    const nextReservation = reservations.get(entry.value + 1);
    if (nextReservation) {
      lines.push(`  // 0x${formatHex(entry.value + 1)} is reserved: ${nextReservation}`);
    }
  }
  lines.push("};", "", "// Error is the compact failure code returned by ErrorResponse.");
  lines.push(...cppEnum(errors, "Error"));
  lines.push(
    "static_assert(MaximumRawFrame < 254,",
    "              \"native COBS contract requires one bounded code block\");",
    "static_assert(HostMenuDirectory == 0x42 && HostMenuContent == 0x43 &&",
    "                  HostMenuStateGet == 0x44 && StatusLedChanged == 0x9E,",
    "              \"native opcode values are part of the wire contract\");",
    "",
    "} // namespace WireContract",
    "",
    "// Preserve the established ControllerProtocol::* source API without",
    "// duplicating the generated constants in AVR storage.",
    "using namespace WireContract;",
    "",
    "} // namespace ControllerProtocol",
    "",
  );
  return lines.join("\n");
}

// renderGo preserves the existing public native.Op* and Error* spellings so
// callers retain source compatibility while values gain one checked-in owner.
export function renderGo(contract) {
  validateContract(contract);
  const { envelope, opcodes, errors } = contract;
  const lines = [
    ...generatedBanner("Project/ProtocolContract.json"),
    "package native",
    "",
    ...goConstBlock(
      [
        { name: "Magic", type: "byte", value: `0x${formatHex(envelope.magic)}` },
        { name: "EnvelopeRevision", type: "byte", value: `${envelope.revision}` },
      ],
      (entry) => entry.value,
    ),
    "",
    ...goConstBlock(
      [
        { name: "MaxPayload", value: `${envelope.maximumPayload}` },
        { name: "RawFrameOverhead", value: `${envelope.rawFrameOverhead}` },
        { name: "MaxRawFrame", value: "MaxPayload + RawFrameOverhead" },
        { name: "MaxEncodedFrame", value: `MaxRawFrame + ${envelope.maximumEncodedFrameOverhead}` },
      ],
      (entry) => entry.value,
    ),
    "",
    ...goConstBlock(
      opcodes.map((entry) => ({
        name: entry.go,
        type: "byte",
        value: `0x${formatHex(entry.value)}`,
      })),
      (entry) => entry.value,
    ),
    "",
    ...goConstBlock(
      errors.map((entry) => ({ name: entry.go, type: "byte", value: `${entry.value}` })),
      (entry) => entry.value,
    ),
    "",
  ];
  return lines.join("\n");
}

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
  const contract = validateContract(await readContract());
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
