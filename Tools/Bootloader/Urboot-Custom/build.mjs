#!/usr/bin/env node
// Reproduce the pinned MiniCore reference image, then build current Urboot-Custom.

import { spawnSync } from "node:child_process";
import {
  copyFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { createHash } from "node:crypto";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, "..", "..", "..");
const manifest = JSON.parse(readFileSync(join(here, "source-manifest.json"), "utf8"));
const productMetadata = JSON.parse(readFileSync(join(repo, "Tools", "Controller", "web", "package.json"), "utf8"));
const managedConfigDirectory = String(productMetadata.productConfigDirectory ?? "").trim();
if (!managedConfigDirectory || managedConfigDirectory === "." || managedConfigDirectory === ".." ||
    /[<>:"/\\|?*\x00-\x1f]/.test(managedConfigDirectory)) {
  throw new Error("web/package.json.productConfigDirectory must be a safe, non-empty directory name");
}
const out = join(repo, ".build", "bootloader", "urboot-custom");
const stockUpstream = join(out, `stock-${manifest.stockFixture.upstream.tag}`);
const activeUpstream = join(out, `active-${manifest.activeUpstream.tag}`);
const patched = join(out, `patched-${manifest.activeUpstream.tag}`);
const bootstrap = process.argv.includes("--bootstrap");
const exe = process.platform === "win32" ? ".exe" : "";
const flashBytes = 32768;
const flashWords = flashBytes / 2;
const customStart = Number.parseInt(manifest.customProfile.startAddress, 16);
const customAllocatedBytes = manifest.customProfile.allocatedBytes;
const applicationMaximumBytes = manifest.customProfile.applicationMaximumBytes;
const flashPageBytes = 128;

if (sha256(join(here, "LICENSE.upstream")) !== manifest.stockFixture.upstream.licenseSha256 ||
    sha256(join(here, "LICENSE.upstream")) !== manifest.activeUpstream.licenseSha256) {
  throw new Error("Vendored upstream GPL-3.0 license hash mismatch");
}

function fail(message) {
  throw new Error(message);
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: options.cwd ?? repo,
    encoding: "utf8",
    windowsHide: true,
  });
  if (result.status !== 0) {
    fail(`${command} ${args.join(" ")}\n${result.stdout ?? ""}${result.stderr ?? ""}`);
  }
  return result.stdout ?? "";
}

function sha256(path) {
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

function findToolchain() {
  const candidates = [];
  if (process.env.AVR_GCC_ROOT) candidates.push(process.env.AVR_GCC_ROOT);
  if (process.env.LOCALAPPDATA) {
    candidates.push(join(process.env.LOCALAPPDATA, "Arduino15", "packages", "arduino", "tools", "avr-gcc", "7.3.0-atmel3.6.1-arduino7"));
    candidates.push(join(process.env.LOCALAPPDATA, managedConfigDirectory, "tools", "toolchain", "data", "packages", "arduino", "tools", "avr-gcc", "7.3.0-atmel3.6.1-arduino7"));
  }
  if (process.env.HOME) {
    candidates.push(join(process.env.HOME, ".arduino15", "packages", "arduino", "tools", "avr-gcc", "7.3.0-atmel3.6.1-arduino7"));
  }
  for (const candidate of candidates) {
    const bin = candidate.endsWith("bin") ? candidate : join(candidate, "bin");
    if (existsSync(join(bin, `avr-gcc${exe}`))) return bin;
  }
  return null;
}

function bootstrapToolchain() {
  if (!bootstrap) return;
  const cli = process.platform === "win32" ? "arduino-cli.exe" : "arduino-cli";
  const index = manifest.stockFixture.toolchain.packageIndex;
  run(cli, ["core", "update-index", "--additional-urls", index]);
  run(cli, [
    "core", "install", manifest.stockFixture.toolchain.bootstrapCore,
    "--additional-urls", index,
  ]);
}

function fetchPinnedSource(source, destinationRoot) {
  mkdirSync(destinationRoot, { recursive: true });
  for (const [name, expected] of Object.entries(source.files)) {
    const destination = join(destinationRoot, name);
    if (!existsSync(destination) || sha256(destination) !== expected) {
      const url = `https://raw.githubusercontent.com/stefanrueger/urboot/${source.commit}/src/${name}`;
      let result = spawnSync(process.platform === "win32" ? "curl.exe" : "curl", [
        "-L", "--fail", "--silent", "--show-error", url, "-o", destination,
      ], { encoding: "utf8", windowsHide: true });
      if (result.status !== 0) {
        // Some development proxies cannot tunnel raw.githubusercontent.com.
        result = spawnSync(process.platform === "win32" ? "curl.exe" : "curl", [
          "--noproxy", "*", "-L", "--fail", "--silent", "--show-error", url, "-o", destination,
        ], { encoding: "utf8", windowsHide: true });
      }
      if (result.status !== 0) fail(`Unable to fetch ${url}: ${result.stderr ?? "curl failed"}`);
    }
    if (sha256(destination) !== expected) fail(`Pinned-source hash mismatch for ${source.tag}: ${name}`);
  }
}

mkdirSync(out, { recursive: true });
bootstrapToolchain();
const toolBin = findToolchain();
if (!toolBin) {
  fail("Pinned Arduino AVR GCC 7.3.0 toolchain not found. Run build.cmd --bootstrap or set AVR_GCC_ROOT.");
}

const tool = (name) => join(toolBin, `${name}${exe}`);
const gccVersion = run(tool("avr-gcc"), ["--version"]).split(/\r?\n/)[0];
const objcopyVersion = run(tool("avr-objcopy"), ["--version"]).split(/\r?\n/)[0];
if (!gccVersion.includes("7.3.0")) fail(`Wrong avr-gcc: ${gccVersion}`);
if (!objcopyVersion.includes("2.26.20160125")) fail(`Wrong avr-objcopy: ${objcopyVersion}`);

fetchPinnedSource(manifest.stockFixture.upstream, stockUpstream);
fetchPinnedSource(manifest.activeUpstream, activeUpstream);

const common = [
  "-DFRILLS=6", "-D_urboot_AVAILABLE=0", "-g", "-Wundef", "-Wall", "-Os",
  "-fno-split-wide-types", "-mrelax", "-mmcu=atmega328p", "-DF_CPU=16000000L",
  "-Wno-clobbered", "-DWDTO=1S", "-DAUTOBAUD=1", "-DDUAL=0", "-DEEPROM=1",
  "-DVBL=1", "-DCHIP_ERASE=1", "-DUARTNUM=0", "-DTX=AtmelPD1", "-DRX=AtmelPD0",
  "-DPGMWRITEPAGE=1", "-DPROTECTRESET=1", "-Wl,--relax", "-nostartfiles", "-nostdlib",
];

function build(name, sourceDir, start, rjmp, extraFlags, extraSources = []) {
  const elf = join(out, `${name}.elf`);
  const hex = join(out, `${name}.hex`);
  const bin = join(out, `${name}.bin`);
  const map = join(out, `${name}.map`);
  const lst = join(out, `${name}.lst`);
  const objectDirectory = join(out, "objects", name);
  rmSync(objectDirectory, { recursive: true, force: true });
  mkdirSync(objectDirectory, { recursive: true });
  const inputs = [join(sourceDir, "urboot.c"), ...extraSources];
  const objects = inputs.map((input, index) => {
    const object = join(objectDirectory, `input-${index}.o`);
    run(tool("avr-gcc"), [
      `-DSTART=0x${start.toString(16)}UL`, `-DRJMPWP=0x${rjmp.toString(16)}`,
      ...common, ...extraFlags, "-c", "-o", object, input,
    ]);
    return object;
  });
  run(tool("avr-gcc"), [
    `-DSTART=0x${start.toString(16)}UL`, `-DRJMPWP=0x${rjmp.toString(16)}`,
    `-Wl,--section-start=.text=0x${start.toString(16)}`,
    "-Wl,--section-start=.version=0x7ffa", ...common, ...extraFlags,
    `-Wl,-Map=${map}`, "-o", elf, ...objects,
  ]);
  run(tool("avr-objcopy"), ["-j", ".text", "-j", ".data", "-j", ".version", "-O", "ihex", elf, hex]);
  // MiniCore's reference artifacts use CRLF; canonicalize for identical hashes on Windows/Linux.
  writeFileSync(hex, readFileSync(hex, "ascii").replace(/\r?\n/g, "\r\n"), "ascii");
  run(tool("avr-objcopy"), ["-j", ".text", "-j", ".data", "-j", ".version", "-O", "binary", elf, bin]);
  writeFileSync(lst, run(tool("avr-objdump"), ["-h", "-S", "-d", elf]));
  rmSync(objectDirectory, { recursive: true, force: true });
  return { name, elf, hex, bin, map, lst };
}

const noLed = build("stock-no-led", stockUpstream, 0x7e80, 0xcfca, ["-DBLINK=0"]);
const led = build("stock-led-b5", stockUpstream, 0x7e80, 0xcfcd, ["-DLED=AtmelPB5", "-DBLINK=1"]);

for (const [artifact, reference] of [[noLed, manifest.stockFixture.references["no-led"]], [led, manifest.stockFixture.references["led+b5"]]]) {
  if (sha256(artifact.hex) !== reference.hexSha256) fail(`${artifact.name} textual HEX differs from MiniCore 3.1.2`);
  if (sha256(artifact.bin) !== reference.binarySha256) fail(`${artifact.name} decoded bytes differ from MiniCore 3.1.2`);
}

rmSync(patched, { recursive: true, force: true });
mkdirSync(patched, { recursive: true });
for (const name of Object.keys(manifest.activeUpstream.files)) copyFileSync(join(activeUpstream, name), join(patched, name));
const patchPath = resolve(here, manifest.customProfile.inputs.patch.path);
if (sha256(patchPath) !== manifest.customProfile.inputs.patch.sha256) {
  fail("Urboot-Custom generic patch hash mismatch");
}
const patchDirectory = `.build/bootloader/urboot-custom/patched-${manifest.activeUpstream.tag}`;
run("git", ["apply", "--check", `--directory=${patchDirectory}`, patchPath], { cwd: repo });
run("git", ["apply", `--directory=${patchDirectory}`, patchPath], { cwd: repo });

const selectedCustomFlags = ["-DBLINK=0", "-DURBOOT_PROGRESS_ENABLED=1", "-DURBOOT_PROGRESS_TM1637=1",
  "-DURBOOT_PROGRESS_WRITE_EVENT=0x73", "-DURBOOT_PROGRESS_READ_EVENT=0x50"];
const selectedBackendPath = resolve(here, manifest.customProfile.inputs.backend.path);
if (sha256(selectedBackendPath) !== manifest.customProfile.inputs.backend.sha256) {
  fail("Urboot-Custom selected backend hash mismatch");
}
const selectedCustomSources = [selectedBackendPath];

// A disabled generic hook must compile byte-for-byte like pristine upstream.
// This proves that rebasing the patch does not alter ordinary Urboot profiles.
const hookOffUpstream = build("hook-off-upstream", activeUpstream, customStart, 0xc000,
  ["-DBLINK=0"]);
const hookOffPatched = build("hook-off-patched", patched, customStart, 0xc000,
  ["-DBLINK=0"]);
const hookOffUpstreamHexSha256 = sha256(hookOffUpstream.hex);
const hookOffPatchedHexSha256 = sha256(hookOffPatched.hex);
const hookOffUpstreamBinarySha256 = sha256(hookOffUpstream.bin);
const hookOffPatchedBinarySha256 = sha256(hookOffPatched.bin);
if (hookOffUpstreamHexSha256 !== hookOffPatchedHexSha256 ||
    hookOffUpstreamBinarySha256 !== hookOffPatchedBinarySha256) {
  fail("The disabled progress-hook patch changes the upstream image");
}
for (const name of [hookOffUpstream.name, hookOffPatched.name]) {
  for (const suffix of ["elf", "hex", "bin", "map", "lst"])
    rmSync(join(out, `${name}.${suffix}`), { force: true });
}

function encodeRjmp(fromByteAddress, toByteAddress) {
  const delta = (toByteAddress >>> 1) - ((fromByteAddress >>> 1) + 1);
  if (delta < -2048 || delta > 2047) fail("RJMP target is out of range");
  return 0xc000 | (delta & 0x0fff);
}

// Resolve the exported application-call trampoline from the active source; a
// source update is never accepted with a stale hard-coded RJMP operand.
const customProbe = build("urboot-atmega328p-custom-probe", patched, customStart, 0xc000,
  selectedCustomFlags, selectedCustomSources);
const probeNM = run(tool("avr-nm"), ["-n", customProbe.elf]);
const probePgmMatch = probeNM.match(/^([0-9a-fA-F]+)\s+\w\s+pgm_write_page$/m);
if (!probePgmMatch) fail("Active Urboot no longer exports pgm_write_page");
const pgmWritePageAddress = Number.parseInt(probePgmMatch[1], 16);
const customRjmp = encodeRjmp(0x7ffc, pgmWritePageAddress);
const custom = build("urboot-atmega328p-custom", patched, customStart, customRjmp,
  selectedCustomFlags, selectedCustomSources);
for (const suffix of ["elf", "hex", "bin", "map", "lst"])
  rmSync(join(out, `urboot-atmega328p-custom-probe.${suffix}`), { force: true });

function parseHex(path) {
  let upper = 0;
  const bytes = new Map();
  for (const line of readFileSync(path, "ascii").trim().split(/\r?\n/)) {
    if (!/^:[0-9A-Fa-f]+$/.test(line)) fail(`Malformed Intel HEX record in ${path}`);
    const count = Number.parseInt(line.slice(1, 3), 16);
    const address = Number.parseInt(line.slice(3, 7), 16);
    const type = Number.parseInt(line.slice(7, 9), 16);
    const record = Buffer.from(line.slice(1), "hex");
    if (record.length !== count + 5 ||
        record.reduce((sum, value) => (sum + value) & 0xff, 0) !== 0) {
      fail(`Invalid Intel HEX length/checksum in ${path}`);
    }
    if (type === 2) upper = Number.parseInt(line.slice(9, 13), 16) << 4;
    if (type === 4) upper = Number.parseInt(line.slice(9, 13), 16) << 16;
    if (type === 0) {
      for (let i = 0; i < count; i++) {
        const absolute = upper + address + i;
        const value = Number.parseInt(line.slice(9 + i * 2, 11 + i * 2), 16);
        if (bytes.has(absolute) && bytes.get(absolute) !== value) {
          fail(`Conflicting Intel HEX data at 0x${absolute.toString(16)} in ${path}`);
        }
        bytes.set(absolute, value);
      }
    }
  }
  return bytes;
}

function hexRecord(type, address, data = []) {
  const values = [data.length, (address >>> 8) & 0xff, address & 0xff, type, ...data];
  const checksum = (-values.reduce((sum, value) => sum + value, 0)) & 0xff;
  return `:${[...values, checksum].map((value) => value.toString(16).padStart(2, "0").toUpperCase()).join("")}`;
}

function writeHex(path, memory) {
  const addresses = [...memory.keys()].sort((a, b) => a - b);
  const lines = [];
  let index = 0;
  let currentUpper = 0;
  while (index < addresses.length) {
    const start = addresses[index];
    const upper = start >>> 16;
    if (upper !== currentUpper) {
      currentUpper = upper;
      lines.push(hexRecord(4, 0, [(upper >>> 8) & 0xff, upper & 0xff]));
    }
    const data = [memory.get(start)];
    index++;
    while (index < addresses.length && data.length < 16 &&
           addresses[index] === start + data.length &&
           (addresses[index] >>> 16) === upper) {
      data.push(memory.get(addresses[index++]));
    }
    lines.push(hexRecord(0, start & 0xffff, data));
  }
  lines.push(hexRecord(1, 0));
  writeFileSync(path, `${lines.join("\r\n")}\r\n`, "ascii");
}

function writeFlashBinary(path, memory) {
  const binary = Buffer.alloc(flashBytes, 0xff);
  for (const [address, value] of memory) {
    if (address < 0 || address >= flashBytes) fail(`Flash address 0x${address.toString(16)} is out of range`);
    binary[address] = value;
  }
  writeFileSync(path, binary);
}

function readWord(memory, address) {
  const low = memory.get(address);
  const high = memory.get(address + 1);
  if (low === undefined || high === undefined) fail(`Missing AVR word at 0x${address.toString(16)}`);
  return low | (high << 8);
}

function decodeJump(memory, address) {
  const opcode = readWord(memory, address);
  if ((opcode & 0xf000) === 0xc000) {
    let delta = opcode & 0x0fff;
    if (delta & 0x0800) delta -= 0x1000;
    const targetWord = (((address >>> 1) + 1 + delta) % flashWords + flashWords) % flashWords;
    return { kind: "rjmp", width: 2, opcode, target: targetWord << 1 };
  }
  // ATmega328P has a 14-bit program counter, so all valid absolute JMP targets
  // use the compact 0x940c first word followed by the target word.
  if (opcode === 0x940c) {
    return { kind: "jmp", width: 4, opcode, target: readWord(memory, address + 2) << 1 };
  }
  fail(`Unsupported reset/vector opcode 0x${opcode.toString(16)} at 0x${address.toString(16)}`);
}

function encodeWrappedRjmp(fromByteAddress, toByteAddress) {
  let delta = (toByteAddress >>> 1) - ((fromByteAddress >>> 1) + 1);
  while (delta > 2047) delta -= flashWords;
  while (delta < -2048) delta += flashWords;
  if (delta < -2048 || delta > 2047) fail("Wrapped RJMP target is out of range");
  return 0xc000 | (delta & 0x0fff);
}

function writeWord(memory, address, word) {
  memory.set(address, word & 0xff);
  memory.set(address + 1, (word >>> 8) & 0xff);
}

function writeAbsoluteJump(memory, address, targetByteAddress) {
  if ((targetByteAddress & 1) !== 0 || targetByteAddress < 0 || targetByteAddress >= flashBytes) {
    fail(`Invalid ATmega328P absolute jump target 0x${targetByteAddress.toString(16)}`);
  }
  writeWord(memory, address, 0x940c);
  writeWord(memory, address + 2, targetByteAddress >>> 1);
}

const bytes = parseHex(custom.hex);
const addresses = [...bytes.keys()].sort((a, b) => a - b);
const lowest = addresses[0];
const highest = addresses.at(-1);
const metadata = Array.from({ length: 6 }, (_, i) => bytes.get(0x7ffa + i));
if (lowest !== customStart || highest !== flashBytes - 1) fail(`Custom address range is ${lowest.toString(16)}..${highest.toString(16)}`);
if (bytes.size > customAllocatedBytes || readFileSync(custom.bin).length !== customAllocatedBytes) {
  fail(`Custom image exceeds its ${customAllocatedBytes}-byte allocation: ${bytes.size} meaningful bytes`);
}
if (metadata.some((value) => value === undefined)) fail("Urboot metadata is incomplete");
const customHexSha256 = sha256(custom.hex);
const customBinarySha256 = sha256(custom.bin);
const metadataHex = Buffer.from(metadata).toString("hex");
if (bytes.size !== manifest.customProfile.expected.meaningfulBytes ||
    customHexSha256 !== manifest.customProfile.expected.hexSha256 ||
    customBinarySha256 !== manifest.customProfile.expected.binarySha256 ||
    metadataHex !== manifest.customProfile.expected.metadataHex) {
  fail("Urboot-Custom image size/hash/metadata differs from its reviewed profile");
}

const nm = run(tool("avr-nm"), ["-n", custom.elf]);
const pgmMatch = nm.match(/^([0-9a-fA-F]+)\s+\w\s+pgm_write_page$/m);
if (!pgmMatch || Number.parseInt(pgmMatch[1], 16) !== pgmWritePageAddress) fail("pgm_write_page moved between probe and final link");
const progressMatch = nm.match(/^([0-9a-fA-F]+)\s+\w\s+urboot_progress$/m);
if (!progressMatch) fail("Selected Urboot-Custom backend is not linked");
const progressAddress = Number.parseInt(progressMatch[1], 16);
const customListing = readFileSync(custom.lst, "utf8");
const progressCalls = [...customListing.matchAll(
  /^\s*([0-9a-fA-F]+):[^\r\n]*\b(rcall|call)\b[^\r\n]*;\s*0x([0-9a-fA-F]+)\s*<urboot_progress>/gm,
)];
const progressCallCount = progressCalls.length;
if (progressCallCount !== 1) fail(`Expected one generic progress-hook call, found ${progressCallCount}`);
const progressCallSite = Number.parseInt(progressCalls[0][1], 16);
const progressCallKind = progressCalls[0][2];
const progressCallTarget = Number.parseInt(progressCalls[0][3], 16);
const progressCallDistanceBytes = progressCallTarget - (progressCallSite + 2);
if (progressCallKind !== "rcall" || progressCallTarget !== progressAddress ||
    (progressCallDistanceBytes & 1) !== 0 ||
    progressCallDistanceBytes < -4096 || progressCallDistanceBytes > 4094) {
  fail("Progress backend call is not a valid in-range AVR RCALL to urboot_progress");
}

// Urboot's application ABI preserves reset flags in fixed register r2 after
// clearing MCUSR. Assert the actual linked instructions, not just source text:
// r2 may be captured and tested, but must not be overwritten before hand-off.
const instructionLines = customListing.split(/\r?\n/).filter((line) =>
  /^\s*[0-9a-fA-F]+:\s+(?:[0-9a-fA-F]{2}\s+)+/.test(line));
const resetCaptureLine = instructionLines.find((line) => /\bin\s+r2,\s*0x34\b/.test(line));
const resetClearLine = instructionLines.find((line) => /\bout\s+0x34,\s*r1\b/.test(line));
const externalResetTestLine = instructionLines.find((line) => /\bsbrs\s+r2,\s*1\b/.test(line));
const r2InstructionLines = instructionLines.filter((line) => /\br2\b/.test(line));
const instructionAddress = (line) => Number.parseInt(line.trimStart().match(/^([0-9a-fA-F]+):/)?.[1] ?? "", 16);
const resetCaptureAddress = instructionAddress(resetCaptureLine ?? "");
const resetClearAddress = instructionAddress(resetClearLine ?? "");
const externalResetTestAddress = instructionAddress(externalResetTestLine ?? "");
if (!resetCaptureLine || !resetClearLine || !externalResetTestLine ||
    r2InstructionLines.length !== 2 ||
    !(resetCaptureAddress < resetClearAddress && resetClearAddress < externalResetTestAddress)) {
  fail("Linked Urboot image no longer preserves MCUSR in r2 for application hand-off");
}
const encodedRjmp = metadata[2] | (metadata[3] << 8);
let delta = encodedRjmp & 0x0fff;
if (delta & 0x0800) delta -= 0x1000;
const decodedTarget = ((0x7ffc >>> 1) + 1 + delta) << 1;
if (encodedRjmp !== customRjmp || decodedTarget !== pgmWritePageAddress) fail("RJMPWP trampoline does not resolve to pgm_write_page");

function sizeVariant(id, overrideFlags, lostCapability) {
  const sources = [selectedBackendPath];
  const probe = build(`matrix-${id}-probe`, patched, customStart, 0xc000,
    [...selectedCustomFlags, ...overrideFlags], sources);
  const symbols = run(tool("avr-nm"), ["-n", probe.elf]);
  const match = symbols.match(/^([0-9a-fA-F]+)\s+\w\s+pgm_write_page$/m);
  const rjmp = match ? encodeRjmp(0x7ffc, Number.parseInt(match[1], 16)) : 0x9508;
  const final = build(`matrix-${id}`, patched, customStart, rjmp,
    [...selectedCustomFlags, ...overrideFlags], sources);
  for (const suffix of ["elf", "hex", "bin", "map", "lst"]) rmSync(join(out, `matrix-${id}-probe.${suffix}`), { force: true });
  const profileBytes = parseHex(final.hex).size;
  if (profileBytes > customAllocatedBytes) fail(`Size profile ${id} exceeds ${customAllocatedBytes} bytes`);
  const requiredPages = Math.ceil(profileBytes / flashPageBytes);
  return { id, meaningfulBytes: profileBytes, bootloaderBytesGained: bytes.size - profileBytes,
    requiredPages, applicationBytesGained: customAllocatedBytes - requiredPages * flashPageBytes,
    rjmpwpOpcode: `0x${rjmp.toString(16)}`, lostCapability };
}

const profileSizeMatrix = [
  sizeVariant("no-chip-erase", ["-DCHIP_ERASE=0"],
    "No STK_CHIP_ERASE command; the uploader cannot request bootloader-managed whole-application erase."),
  sizeVariant("no-eeprom", ["-DEEPROM=0"],
    "No EEPROM read/write commands; EEPROM settings cannot be backed up or restored through Urboot."),
  sizeVariant("no-update-check", ["-DUPDATE_FL=0"],
    "No compare-before-write shortcut; requested flash pages are rewritten even when already identical."),
  sizeVariant("no-app-page-writer", ["-DPGMWRITEPAGE=0"],
    "No exported pgm_write_page application entry; serial flash programming remains available."),
  sizeVariant("fixed-115200", ["-DAUTOBAUD=0", "-DBAUD_RATE=115200L"],
    "No receive-edge autobaud measurement; bootloader requires the fixed 115200 baud/16 MHz timing."),
  sizeVariant("no-reset-vector-protection", ["-DPROTECTRESET=0"],
    "Flash writes no longer force page-zero reset back to Urboot; an unsafe page-zero write can strand the bootloader."),
];

function measureLegacyLedConflict() {
  const sources = [selectedBackendPath];
  const flags = [...selectedCustomFlags, "-DLED=AtmelPB5", "-DBLINK=1"];
  const probe = build("matrix-legacy-led-conflict-probe", patched, 0x7d80, 0xc000, flags, sources);
  const match = run(tool("avr-nm"), ["-n", probe.elf]).match(/^([0-9a-fA-F]+)\s+\w\s+pgm_write_page$/m);
  if (!match) fail("Legacy LED measurement lost pgm_write_page");
  const rjmp = encodeRjmp(0x7ffc, Number.parseInt(match[1], 16));
  const final = build("matrix-legacy-led-conflict", patched, 0x7d80, rjmp, flags, sources);
  for (const suffix of ["elf", "hex", "bin", "map", "lst"]) rmSync(join(out, `matrix-legacy-led-conflict-probe.${suffix}`), { force: true });
  const meaningfulBytes = parseHex(final.hex).size;
  return {
    meaningfulBytes,
    bytesOverSelectedAllocation: meaningfulBytes - customAllocatedBytes,
    requiredPages: 5,
    applicationMaximumBytes: 32128,
    note: "Compile-only: legacy PB5 blink also electrically conflicts with the TM1637 clock, so it is not selectable.",
  };
}

const legacyLedConflict = measureLegacyLedConflict();

let applicationBytes = null;
let mergedImage = null;
const firmwareManifestPath = join(repo, ".build", "firmware", "firmware-manifest.json");
if (existsSync(firmwareManifestPath)) {
  const firmwareManifest = JSON.parse(readFileSync(firmwareManifestPath, "utf8"));
  const applicationArtifact = firmwareManifest.artifacts?.find((item) =>
    item.role === "application" && /PCController\.ino\.hex$/i.test(item.path ?? ""));
  if (!applicationArtifact) fail("Current firmware manifest has no canonical application HEX");
  const applicationHexPath = resolve(repo, applicationArtifact.path);
  if (!existsSync(applicationHexPath)) fail(`Current application HEX is missing: ${applicationHexPath}`);
  if (sha256(applicationHexPath) !== applicationArtifact.sha256) {
    fail("Current application HEX does not match its firmware manifest hash");
  }

  const application = parseHex(applicationHexPath);
  const applicationAddresses = [...application.keys()].sort((a, b) => a - b);
  const applicationLowest = applicationAddresses[0];
  const applicationHighest = applicationAddresses.at(-1);
  applicationBytes = application.size;
  if (applicationBytes !== applicationArtifact.dataBytes) {
    fail(`Application manifest says ${applicationArtifact.dataBytes} bytes, decoded HEX has ${applicationBytes}`);
  }
  if (applicationLowest !== 0 || applicationHighest >= customStart) {
    fail(`Application range 0x${applicationLowest.toString(16)}..0x${applicationHighest.toString(16)} overlaps Urboot-Custom`);
  }
  if (applicationBytes > applicationMaximumBytes) {
    fail(`Application ${applicationBytes} exceeds custom bootloader ceiling ${applicationMaximumBytes}`);
  }

  const applicationElfPath = applicationHexPath.replace(/\.hex$/i, ".elf");
  if (!existsSync(applicationElfPath)) fail("Current application ELF is required to validate the reserved vector");
  const applicationSymbols = run(tool("avr-nm"), ["-n", applicationElfPath]);
  const badInterruptMatch = applicationSymbols.match(/^([0-9a-fA-F]+)\s+\w\s+__bad_interrupt$/m);
  if (!badInterruptMatch) fail("Current application has no __bad_interrupt symbol for vector safety validation");
  const badInterruptAddress = Number.parseInt(badInterruptMatch[1], 16);

  const bootloaderPages = metadata[0];
  const vblVectorNumber = metadata[1];
  const expectedBootloaderPages = customAllocatedBytes / flashPageBytes;
  if (!Number.isInteger(expectedBootloaderPages) || bootloaderPages !== expectedBootloaderPages) {
    fail(`Urboot metadata reports ${bootloaderPages} pages, expected ${expectedBootloaderPages}`);
  }
  if (vblVectorNumber < 1 || vblVectorNumber > 25) {
    fail(`Urboot metadata reports invalid ATmega328P vector ${vblVectorNumber}`);
  }
  const vblVectorAddress = vblVectorNumber * 4;
  const applicationReset = decodeJump(application, 0);
  const reservedVector = decodeJump(application, vblVectorAddress);
  if (applicationReset.target <= 0 || applicationReset.target >= customStart) {
    fail(`Application reset target 0x${applicationReset.target.toString(16)} is invalid`);
  }
  if (reservedVector.target !== badInterruptAddress) {
    fail(`Vector ${vblVectorNumber} is in use (targets 0x${reservedVector.target.toString(16)}, not __bad_interrupt)`);
  }
  for (let offset = 0; offset < 4; offset++) {
    if (!application.has(vblVectorAddress + offset)) fail(`Reserved vector ${vblVectorNumber} is incomplete`);
  }

  const patchedApplication = new Map(application);
  const resetToBootloaderOpcode = encodeWrappedRjmp(0, customStart);
  writeWord(patchedApplication, 0, resetToBootloaderOpcode);
  writeWord(patchedApplication, 2, 0x0000);
  writeAbsoluteJump(patchedApplication, vblVectorAddress, applicationReset.target);
  const patchedReset = decodeJump(patchedApplication, 0);
  const patchedApplicationStart = decodeJump(patchedApplication, vblVectorAddress);
  if (patchedReset.target !== customStart || patchedApplicationStart.target !== applicationReset.target) {
    fail("Merged vector patch does not preserve both bootloader and application entry targets");
  }

  const merged = new Map(patchedApplication);
  // Encode the entire selected boot region, including the two reviewed erased
  // bytes, so ISP validators and programmers never inherit stale boot bytes.
  for (let address = customStart; address < flashBytes; address++) {
    if (merged.has(address)) fail(`Application overlaps bootloader at 0x${address.toString(16)}`);
    merged.set(address, bytes.get(address) ?? 0xff);
  }
  const mergedHexPath = join(out, "urboot-atmega328p-custom-merged.hex");
  const mergedBinPath = join(out, "urboot-atmega328p-custom-merged.bin");
  writeHex(mergedHexPath, merged);
  writeFlashBinary(mergedBinPath, merged);
  const roundTrip = parseHex(mergedHexPath);
  if (roundTrip.size !== merged.size ||
      [...merged].some(([address, value]) => roundTrip.get(address) !== value)) {
    fail("Merged Intel HEX failed its round-trip validation");
  }
  const mergedBinary = readFileSync(mergedBinPath);
  if (mergedBinary.length !== flashBytes ||
      [...merged].some(([address, value]) => mergedBinary[address] !== value)) {
    fail("Merged full-flash binary failed its byte validation");
  }

  mergedImage = {
    structurallyReadyForIsp: true,
    hexPath: mergedHexPath.slice(repo.length + 1).replaceAll("\\", "/"),
    binaryPath: mergedBinPath.slice(repo.length + 1).replaceAll("\\", "/"),
    hexSha256: sha256(mergedHexPath),
    binarySha256: sha256(mergedBinPath),
    hexDataBytes: merged.size,
    applicationMeaningfulBytes: applicationBytes,
    bootRegionBytes: customAllocatedBytes,
    bootRegionMeaningfulBytes: bytes.size,
    normalizedFlashBytes: mergedBinary.length,
    erasedGapFill: "0xff",
    application: {
      sourcePath: applicationArtifact.path.replaceAll("\\", "/"),
      sourceHexSha256: applicationArtifact.sha256,
      meaningfulBytes: applicationBytes,
      lowestAddress: `0x${applicationLowest.toString(16)}`,
      highestAddress: `0x${applicationHighest.toString(16)}`,
      freeBeforeBootloader: applicationMaximumBytes - applicationBytes,
      originalResetKind: applicationReset.kind,
      originalResetOpcode: `0x${applicationReset.opcode.toString(16)}`,
      originalResetTarget: `0x${applicationReset.target.toString(16)}`,
    },
    vectorBootloading: {
      vectorNumber: vblVectorNumber,
      vectorAddress: `0x${vblVectorAddress.toString(16)}`,
      originalReservedVectorKind: reservedVector.kind,
      originalReservedVectorTarget: `0x${reservedVector.target.toString(16)}`,
      badInterruptAddress: `0x${badInterruptAddress.toString(16)}`,
      resetToBootloaderOpcode: `0x${resetToBootloaderOpcode.toString(16)}`,
      resetTarget: `0x${patchedReset.target.toString(16)}`,
      applicationTrampolineOpcode: "0x940c",
      applicationTrampolineTarget: `0x${patchedApplicationStart.target.toString(16)}`,
    },
    expectedProfile: {
      lowFuse: "0xf7",
      highFuse: "0xd7",
      extendedFuse: "0xfd",
      lockByte: "0xff",
      eepromPreservedByChipErase: true,
      hardwareBootResetDisabled: true,
    },
    installationGate: [
      "Capture signature, flash, EEPROM, fuses, and lock byte read-only through ISP.",
      "Compare the live fuse/lock state with the intended profile; do not write fuses implicitly.",
      "Write and byte-verify the merged flash image while preserving the EEPROM backup.",
      "Prove external-reset bootloader entry, UART/Urclock read/write progress, and application return.",
    ],
  };
}

const result = {
  format: "urboot-custom-build/v3",
  stockFixture: manifest.stockFixture,
  activeUpstream: manifest.activeUpstream,
  toolchain: { gcc: gccVersion, objcopy: objcopyVersion },
  patchValidation: {
    appliesCleanly: true,
    hookDisabledByteIdentical: true,
    upstreamHexSha256: hookOffUpstreamHexSha256,
    patchedHexSha256: hookOffPatchedHexSha256,
    upstreamBinarySha256: hookOffUpstreamBinarySha256,
    patchedBinarySha256: hookOffPatchedBinarySha256,
  },
  stockReproduction: {
    "no-led": { hexSha256: sha256(noLed.hex), binarySha256: sha256(noLed.bin), exactMiniCoreMatch: true },
    "led+b5": { hexSha256: sha256(led.hex), binarySha256: sha256(led.bin), exactMiniCoreMatch: true },
  },
  custom: {
    backend: "tm1637",
    hexSha256: customHexSha256,
    binarySha256: customBinarySha256,
    elfSha256: sha256(custom.elf),
    meaningfulBytes: bytes.size,
    allocatedBytes: readFileSync(custom.bin).length,
    lowestAddress: `0x${lowest.toString(16)}`,
    highestAddress: `0x${highest.toString(16)}`,
    metadataHex,
    pgmWritePageAddress: `0x${pgmWritePageAddress.toString(16)}`,
    progressBackendAddress: `0x${progressAddress.toString(16)}`,
    progressHookCallCount: progressCallCount,
    progressHookCallSite: `0x${progressCallSite.toString(16)}`,
    progressHookCallKind: progressCallKind,
    progressHookCallDistanceBytes: progressCallDistanceBytes,
    progressHookCallWithinRange: true,
    rjmpwpOpcode: `0x${customRjmp.toString(16)}`,
    rjmpwpDecodedTarget: `0x${decodedTarget.toString(16)}`,
    applicationMaximumBytes,
    currentApplicationBytes: applicationBytes,
    currentApplicationFreeBytes: applicationBytes === null ? null : applicationMaximumBytes - applicationBytes,
    retainedFeatureCode: "weU-jPrac",
  },
  resetCauseHandoff: {
    hardwareRegister: "MCUSR",
    hardwareIoAddress: "0x34",
    preservedRegister: "r2",
    captureInstructionAddress: `0x${resetCaptureAddress.toString(16)}`,
    clearInstructionAddress: `0x${resetClearAddress.toString(16)}`,
    externalResetTestInstructionAddress: `0x${externalResetTestAddress.toString(16)}`,
    observedR2InstructionCount: r2InstructionLines.length,
    preservedUntilApplicationHandoff: true,
  },
  profileSizeMatrix,
  legacyLedConflict,
  mergedImage,
};
writeFileSync(join(out, "build-manifest.json"), `${JSON.stringify(result, null, 2)}\n`);
console.log(`Stock MiniCore images: exact textual HEX and decoded-byte match`);
console.log(`Urboot-Custom ${manifest.activeUpstream.tag} (${result.custom.backend} backend): ${bytes.size}/${customAllocatedBytes} meaningful bytes, SHA256 ${result.custom.hexSha256}`);
console.log(`Application ceiling: ${applicationMaximumBytes} bytes; current application: ${applicationBytes ?? "not built"}`);
if (mergedImage) console.log(`ISP merged image: structurally ready, SHA256 ${mergedImage.hexSha256}`);
console.log(`Artifacts: ${out}`);
