#!/usr/bin/env node
// Reproduce the historical MiniCore image, then patch/build current Urboot-Custom.

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
const out = join(repo, ".build", "bootloader", "urboot-custom");
const stockUpstream = join(out, `stock-${manifest.stockFixture.upstream.tag}`);
const activeUpstream = join(out, `active-${manifest.activeUpstream.tag}`);
const patched = join(out, `patched-${manifest.activeUpstream.tag}`);
const bootstrap = process.argv.includes("--bootstrap");
const exe = process.platform === "win32" ? ".exe" : "";

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
    candidates.push(join(process.env.LOCALAPPDATA, "PCController", "tools", "toolchain", "data", "packages", "arduino", "tools", "avr-gcc", "7.3.0-atmel3.6.1-arduino7"));
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
  run(tool("avr-gcc"), [
    `-DSTART=0x${start.toString(16)}UL`, `-DRJMPWP=0x${rjmp.toString(16)}`,
    `-Wl,--section-start=.text=0x${start.toString(16)}`,
    "-Wl,--section-start=.version=0x7ffa", ...common, ...extraFlags,
    `-Wl,-Map=${map}`, "-o", elf, join(sourceDir, "urboot.c"), ...extraSources,
  ]);
  run(tool("avr-objcopy"), ["-j", ".text", "-j", ".data", "-j", ".version", "-O", "ihex", elf, hex]);
  // MiniCore's reference artifacts use CRLF; canonicalize for identical hashes on Windows/Linux.
  writeFileSync(hex, readFileSync(hex, "ascii").replace(/\r?\n/g, "\r\n"), "ascii");
  run(tool("avr-objcopy"), ["-j", ".text", "-j", ".data", "-j", ".version", "-O", "binary", elf, bin]);
  writeFileSync(lst, run(tool("avr-objdump"), ["-h", "-S", "-d", elf]));
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
const patchPath = join(here, "patches", "0001-optional-progress-backend-hook.patch");
const patchDirectory = `.build/bootloader/urboot-custom/patched-${manifest.activeUpstream.tag}`;
run("git", ["apply", "--check", `--directory=${patchDirectory}`, patchPath], { cwd: repo });
run("git", ["apply", `--directory=${patchDirectory}`, patchPath], { cwd: repo });

const selectedCustomFlags = ["-DBLINK=0", "-DURBOOT_PROGRESS_ENABLED=1", "-DURBOOT_PROGRESS_TM1637=1",
  "-DURBOOT_PROGRESS_WRITE_EVENT=0x73", "-DURBOOT_PROGRESS_READ_EVENT=0x50"];
const selectedCustomSources = [join(here, "backends", "tm1637_progress.S")];

function encodeRjmp(fromByteAddress, toByteAddress) {
  const delta = (toByteAddress >>> 1) - ((fromByteAddress >>> 1) + 1);
  if (delta < -2048 || delta > 2047) fail("RJMP target is out of range");
  return 0xc000 | (delta & 0x0fff);
}

// Resolve the exported application-call trampoline from the active source; a
// source update is never accepted with a stale hard-coded RJMP operand.
const customProbe = build("urboot-atmega328p-custom-probe", patched, 0x7e00, 0xc000,
  selectedCustomFlags, selectedCustomSources);
const probeNM = run(tool("avr-nm"), ["-n", customProbe.elf]);
const probePgmMatch = probeNM.match(/^([0-9a-fA-F]+)\s+\w\s+pgm_write_page$/m);
if (!probePgmMatch) fail("Active Urboot no longer exports pgm_write_page");
const pgmWritePageAddress = Number.parseInt(probePgmMatch[1], 16);
const customRjmp = encodeRjmp(0x7ffc, pgmWritePageAddress);
const custom = build("urboot-atmega328p-custom", patched, 0x7e00, customRjmp,
  selectedCustomFlags, selectedCustomSources);
for (const suffix of ["elf", "hex", "bin", "map", "lst"])
  rmSync(join(out, `urboot-atmega328p-custom-probe.${suffix}`), { force: true });

function parseHex(path) {
  let upper = 0;
  const bytes = new Map();
  for (const line of readFileSync(path, "ascii").trim().split(/\r?\n/)) {
    const count = Number.parseInt(line.slice(1, 3), 16);
    const address = Number.parseInt(line.slice(3, 7), 16);
    const type = Number.parseInt(line.slice(7, 9), 16);
    if (type === 4) upper = Number.parseInt(line.slice(9, 13), 16) << 16;
    if (type === 0) for (let i = 0; i < count; i++) bytes.set(upper + address + i, Number.parseInt(line.slice(9 + i * 2, 11 + i * 2), 16));
  }
  return bytes;
}

const bytes = parseHex(custom.hex);
const addresses = [...bytes.keys()].sort((a, b) => a - b);
const lowest = addresses[0];
const highest = addresses.at(-1);
const metadata = Array.from({ length: 6 }, (_, i) => bytes.get(0x7ffa + i));
if (lowest !== 0x7e00 || highest !== 0x7fff) fail(`Custom address range is ${lowest.toString(16)}..${highest.toString(16)}`);
if (bytes.size > 512 || readFileSync(custom.bin).length !== 512) fail(`Custom image exceeds its 512-byte allocation: ${bytes.size} meaningful bytes`);
if (metadata.some((value) => value === undefined)) fail("Urboot metadata is incomplete");

const nm = run(tool("avr-nm"), ["-n", custom.elf]);
const pgmMatch = nm.match(/^([0-9a-fA-F]+)\s+\w\s+pgm_write_page$/m);
if (!pgmMatch || Number.parseInt(pgmMatch[1], 16) !== pgmWritePageAddress) fail("pgm_write_page moved between probe and final link");
const encodedRjmp = metadata[2] | (metadata[3] << 8);
let delta = encodedRjmp & 0x0fff;
if (delta & 0x0800) delta -= 0x1000;
const decodedTarget = ((0x7ffc >>> 1) + 1 + delta) << 1;
if (encodedRjmp !== customRjmp || decodedTarget !== pgmWritePageAddress) fail("RJMPWP trampoline does not resolve to pgm_write_page");

function sizeVariant(id, overrideFlags, lostCapability) {
  const sources = [join(here, "backends", "tm1637_progress.S")];
  const probe = build(`matrix-${id}-probe`, patched, 0x7e00, 0xc000,
    [...selectedCustomFlags, ...overrideFlags], sources);
  const symbols = run(tool("avr-nm"), ["-n", probe.elf]);
  const match = symbols.match(/^([0-9a-fA-F]+)\s+\w\s+pgm_write_page$/m);
  const rjmp = match ? encodeRjmp(0x7ffc, Number.parseInt(match[1], 16)) : 0x9508;
  const final = build(`matrix-${id}`, patched, 0x7e00, rjmp,
    [...selectedCustomFlags, ...overrideFlags], sources);
  for (const suffix of ["elf", "hex", "bin", "map", "lst"]) rmSync(join(out, `matrix-${id}-probe.${suffix}`), { force: true });
  const profileBytes = parseHex(final.hex).size;
  if (profileBytes > 512) fail(`Size profile ${id} exceeds 512 bytes`);
  return { id, meaningfulBytes: profileBytes, bytesGained: bytes.size - profileBytes,
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
  const sources = [join(here, "backends", "tm1637_progress.S")];
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
    bytesOverFourPages: meaningfulBytes - 512,
    requiredPages: 5,
    applicationMaximumBytes: 32128,
    note: "Compile-only: legacy PB5 blink also electrically conflicts with the TM1637 clock, so it is not selectable.",
  };
}

const legacyLedConflict = measureLegacyLedConflict();

let applicationBytes = null;
const firmwareManifestPath = join(repo, ".build", "firmware", "firmware-manifest.json");
if (existsSync(firmwareManifestPath)) {
  const firmwareManifest = JSON.parse(readFileSync(firmwareManifestPath, "utf8"));
  applicationBytes = firmwareManifest.artifacts?.find((item) => item.role === "application")?.dataBytes ?? null;
  if (applicationBytes !== null && applicationBytes > 32256) fail(`Application ${applicationBytes} exceeds custom bootloader ceiling 32256`);
}

const result = {
  format: "pccontroller-urboot-custom-build/v2",
  stockFixture: manifest.stockFixture,
  activeUpstream: manifest.activeUpstream,
  toolchain: { gcc: gccVersion, objcopy: objcopyVersion },
  stockReproduction: {
    "no-led": { hexSha256: sha256(noLed.hex), binarySha256: sha256(noLed.bin), exactMiniCoreMatch: true },
    "led+b5": { hexSha256: sha256(led.hex), binarySha256: sha256(led.bin), exactMiniCoreMatch: true },
  },
  custom: {
    backend: "tm1637",
    hexSha256: sha256(custom.hex),
    binarySha256: sha256(custom.bin),
    elfSha256: sha256(custom.elf),
    meaningfulBytes: bytes.size,
    allocatedBytes: readFileSync(custom.bin).length,
    lowestAddress: `0x${lowest.toString(16)}`,
    highestAddress: `0x${highest.toString(16)}`,
    metadataHex: Buffer.from(metadata).toString("hex"),
    pgmWritePageAddress: `0x${pgmWritePageAddress.toString(16)}`,
    rjmpwpOpcode: `0x${customRjmp.toString(16)}`,
    rjmpwpDecodedTarget: `0x${decodedTarget.toString(16)}`,
    applicationMaximumBytes: 32256,
    currentApplicationBytes: applicationBytes,
    currentApplicationFreeBytes: applicationBytes === null ? null : 32256 - applicationBytes,
    retainedFeatureCode: "weU-jPrac",
  },
  profileSizeMatrix,
  legacyLedConflict,
};
writeFileSync(join(out, "build-manifest.json"), `${JSON.stringify(result, null, 2)}\n`);
console.log(`Stock MiniCore images: exact textual HEX and decoded-byte match`);
console.log(`Urboot-Custom ${manifest.activeUpstream.tag} (${result.custom.backend} backend): ${bytes.size}/512 meaningful bytes, SHA256 ${result.custom.hexSha256}`);
console.log(`Application ceiling: 32256 bytes; current application: ${applicationBytes ?? "not built"}`);
console.log(`Artifacts: ${out}`);
