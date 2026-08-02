#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { existsSync, writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

function invariant(condition, message) {
  if (!condition) throw new Error(message);
}

function addressFromLine(line, name) {
  invariant(line, `AVR symbol table is missing ${name}`);
  const match = line.match(/^([0-9a-f]+)\s/iu);
  invariant(match, `cannot parse ${name} address from AVR symbol table`);
  return Number.parseInt(match[1], 16);
}

function symbolAddress(table, name) {
  return addressFromLine(
    table.split(/\r?\n/u).find((candidate) => candidate.endsWith(` ${name}`)),
    name,
  );
}

function captureBlock(disassembly) {
  const lines = disassembly.split(/\r?\n/u);
  const start = lines.findIndex((line) =>
    /^[0-9a-f]+\s+<.*captureResetCause\(\)>:$/iu.test(line.trim()),
  );
  invariant(start >= 0, "AVR disassembly is missing captureResetCause()")
  const body = [];
  for (let index = start + 1; index < lines.length; index += 1) {
    if (/^[0-9a-f]+\s+<.*>:\s*$/iu.test(lines[index].trim())) break;
    if (lines[index].trim()) body.push(lines[index]);
  }
  invariant(body.length > 0, "captureResetCause() has no AVR instructions");
  return { heading: lines[start], body };
}

export function verifyResetCaptureDisassembly(symbolTable, disassembly) {
  const capture = addressFromLine(
    symbolTable.split(/\r?\n/u).find((line) => line.includes("captureResetCause()")),
    "captureResetCause()",
  );
  const constructorsEnd = symbolAddress(symbolTable, "__ctors_end");
  const copyData = symbolAddress(symbolTable, "__do_copy_data");
  invariant(
    constructorsEnd < capture && capture < copyData,
    `captureResetCause() is not in the early-init window: 0x${constructorsEnd.toString(16)} < 0x${capture.toString(16)} < 0x${copyData.toString(16)}`,
  );

  const block = captureBlock(disassembly);
  const body = block.body.join("\n");
  const required = [
    [/\bin\s+r24,\s*0x34\b/iu, "read MCUSR into r24"],
    [/\b(?:cpse|tst)\s+r24\b/iu, "test the direct MCUSR value"],
    [/\bmov\s+r24,\s*r2\b/iu, "consume Urboot reset flags from r2"],
    [/\bsts\s+0x[0-9a-f]+,\s*r24\b/iu, "persist the selected reset cause"],
    [/\bout\s+0x34,\s*r1\b/iu, "clear MCUSR"],
  ];
  const positions = required.map(([pattern, description]) => {
    const index = body.search(pattern);
    invariant(index >= 0, `captureResetCause() does not ${description}`);
    return index;
  });
  invariant(
    positions.every((position, index) => index === 0 || position > positions[index - 1]),
    "captureResetCause() reset-cause instructions are not in the required order",
  );
  invariant(!/\bret(?:i)?\b/iu.test(body), "naked early-init reset capture must fall through without RET/RETI");
  return {
    capture,
    constructorsEnd,
    copyData,
    report: [
      "PCController AVR reset-capture disassembly proof",
      `early-init window: 0x${constructorsEnd.toString(16)} < capture 0x${capture.toString(16)} < data-init 0x${copyData.toString(16)}`,
      "verified: MCUSR direct read, zero fallback from Urboot r2, persisted cause, MCUSR clear, naked fall-through",
      "",
      block.heading,
      ...block.body,
      "",
    ].join("\n"),
  };
}

function run(file, arguments_) {
  const result = spawnSync(file, arguments_, { encoding: "utf8", windowsHide: true });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${file} ${arguments_.join(" ")} failed: ${result.stderr.trim()}`);
  return result.stdout;
}

function main(arguments_) {
  const [elf, objdump, reportPath] = arguments_;
  if (!elf || !objdump) {
    process.stderr.write("Usage: assert-avr-reset-capture.mjs FIRMWARE.elf AVR_OBJDUMP [REPORT.txt]\n");
    return 2;
  }
  invariant(existsSync(elf), `AVR ELF does not exist: ${elf}`);
  invariant(existsSync(objdump), `avr-objdump does not exist: ${objdump}`);
  const proof = verifyResetCaptureDisassembly(
    run(objdump, ["-t", "-C", elf]),
    run(objdump, ["-d", "-C", elf]),
  );
  if (reportPath) writeFileSync(reportPath, proof.report, "utf8");
  process.stdout.write(`Verified Urboot r2 reset-cause consumption at AVR address 0x${proof.capture.toString(16)}.\n`);
  return 0;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    process.exitCode = main(process.argv.slice(2));
  } catch (error) {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  }
}
