// SPDX-License-Identifier: MIT
// Portable source owner: PCController interfaces/controller-wire/registry.mjs.
// Adapted from its actual Tools/Build/generate-protocol-contract.mjs.
// No IO, toolchain or transport dependencies. Consumers vendor exact pinned bytes.

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
// rendered. The exact physical extensions are guarded here instead of relying on
// a reviewer to notice a hexadecimal value changing in a generated diff.
export function validateContract(contract) {
  if (contract === null || typeof contract !== "object") {
    throw new Error("protocol contract must be an object");
  }
  const envelope = contract.envelope;
  if (envelope === null || typeof envelope !== "object") {
    throw new Error("protocol contract requires an envelope object");
  }
  requireInteger(envelope.magic, "envelope.magic");
  requireInteger(envelope.reserved, "envelope.reserved");
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
  if (envelope.magic !== 0xA5 || envelope.reserved !== 1 || envelope.maximumPayload !== 48) {
    throw new Error("the published native envelope must remain A5/reserved-1/48-byte payload");
  }
  const peer = contract.pakar;
  if (!peer || !peer.envelope || peer.envelope.magic !== envelope.magic ||
      peer.envelope.reserved !== 0 || peer.envelope.maximumPayload !== 48 ||
      peer.envelope.rawFrameOverhead !== 6 || peer.envelope.maximumEncodedFrameOverhead !== 2)
    throw new Error("Pakar requires its fixed A5/reserved-0/48-byte envelope; no dialect fallback");
  validateRegistry(peer.opcodes, "pakar.opcodes", ["wireName"]);
  validateRegistry(peer.errors, "pakar.errors", []);
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
    `constexpr uint8_t ReservedEnvelopeByte = ${envelope.reserved};`,
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
        { name: "ReservedEnvelopeByte", type: "byte", value: `${envelope.reserved}` },
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
    "var opcodeNames = map[byte]string{",
    ...opcodes.map(entry => `\t${entry.go}: ${" ".repeat(Math.max(...opcodes.map(row => row.go.length)) - entry.go.length)}"${entry.wireName}",`),
    "}",
    "",
  ];
  return lines.join("\n");
}


// Published assignments are an independent compatibility fixture, not another
// editable output. Additions are allowed; removal, reuse and renumbering are not.
export function validatePublishedBindings(contract, published) {
  validateContract(contract);
  for (const [name, current] of [["controller", contract], ["pakar", contract.pakar]]) {
    const previous = published[name];
    if (!previous) throw Error("Missing published namespace " + name);
    for (const [key, value] of Object.entries(previous.envelope))
      if (current.envelope[key] !== value) throw Error(name + " published envelope changed: " + key);
    for (const type of ["opcodes", "errors"]) for (const entry of previous[type]) {
      const actual = current[type].find(row => row.cpp === entry.cpp);
      if (!actual || Object.entries(entry).some(([key, value]) => actual[key] !== value))
        throw Error(name + " published binding changed: " + entry.cpp);
    }
    for (const reserved of previous.reservedOpcodes) {
      if (current.opcodes.some(row => row.value === reserved.value) ||
          !current.reservedOpcodes.some(row => row.value === reserved.value))
        throw Error(name + " published reservation reused: " + reserved.value);
    }
  }
  return contract;
}
const upper = name => name.replace(/^k(?=[A-Z])/, "").replace(/([a-z0-9])([A-Z])/g, "$1_$2").toUpperCase();
const dartName = name => name.replace(/^k(?=[A-Z])/, "").replace(/^./, c => c.toLowerCase());
const banner = "// Generated from PCController Project/ProtocolContract.json; DO NOT EDIT.\n";
export function renderPakarCpp(contract) {
  validateContract(contract);
  const p = contract.pakar;
  return banner + "#pragma once\n#include <stdint.h>\n\nnamespace BoardCore { namespace Protocol {\n" +
    "constexpr uint8_t kMagic = 0xA5;\nconstexpr uint8_t kReserved = 0;\nconstexpr uint8_t kMaximumPayload = 48;\n" +
    "constexpr uint8_t kRawOverhead = 6;\nconstexpr uint8_t kMaximumRaw = kMaximumPayload + kRawOverhead;\nconstexpr uint8_t kMaximumEncoded = kMaximumRaw + 2;\n\n" +
    ["Opcode", "Error"].map((type, i) => "enum " + type + " : uint8_t {\n" +
      p[i ? "errors" : "opcodes"].map(r => "  " + r.cpp + " = 0x" + formatHex(r.value) + ",").join("\n") + "\n};\n").join("\n") +
    "\n} } // namespace BoardCore::Protocol\n";
}
export function renderRust(contract) {
  validateContract(contract);
  return banner + [["pakar", contract.pakar], ["controller", contract]].map(([name, ns]) =>
    "#[allow(dead_code)]\npub mod " + name + " {\n" +
    "    pub const MAGIC: u8 = 0x" + formatHex(ns.envelope.magic) + ";\n" +
    "    pub const RESERVED: u8 = " + ns.envelope.reserved + ";\n" +
    "    pub const MAXIMUM_PAYLOAD: usize = " + ns.envelope.maximumPayload + ";\n" +
    ns.opcodes.map(r => "    pub const " + upper(r.cpp) + ": u8 = 0x" + formatHex(r.value) + ";").join("\n") +
    "\n}\n").join("\n");
}
export function renderDart(contract) {
  validateContract(contract);
  return banner + [["Pakar", contract.pakar], ["Controller", contract]].map(([name, ns]) =>
    "abstract final class " + name + "Opcode {\n" +
    "  static const magic = " + ns.envelope.magic + ";\n" +
    "  static const reserved = " + ns.envelope.reserved + ";\n" +
    "  static const maximumPayload = " + ns.envelope.maximumPayload + ";\n" +
    ns.opcodes.map(r => "  static const " + dartName(r.cpp) + " = 0x" + formatHex(r.value) + ";").join("\n") +
    "\n}\n").join("\n");
}
