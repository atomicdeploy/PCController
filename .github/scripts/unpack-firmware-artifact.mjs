#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { basename, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// Consume the raw Actions archive without extracting arbitrary tar paths or
// symlinks. Only named top-level firmware evidence is copied into the build.
export function unpackFirmwareArtifact(directory, destination) {
  const archives = readdirSync(directory).filter(name => /^PCController-Firmware-.+-AVR-ATmega328P\.tar\.gz$/u.test(name));
  if (archives.length !== 1) throw new Error(`Expected one ATmega328P firmware archive; found ${archives.length}`);
  const name = archives[0];
  const archive = resolve(directory, name);
  const checksum = readFileSync(`${archive}.sha256`, "utf8").trim();
  const digest = createHash("sha256").update(readFileSync(archive)).digest("hex");
  const match = /^([a-f0-9]{64})\s+\*?([^\r\n]+)$/iu.exec(checksum);
  if (!match || match[1].toLowerCase() !== digest || match[2] !== name) throw new Error("Firmware archive checksum mismatch");
  const options = { windowsHide: true, maxBuffer: 32 * 1024 * 1024 };
  const root = name.slice(0, -7);
  const entries = execFileSync("tar", ["-tzf", archive], { ...options, encoding: "utf8" }).trim().split(/\r?\n/u);
  if (new Set(entries).size !== entries.length) throw new Error("Duplicate firmware archive members");
  const selected = entries.filter(entry => {
    if (!entry.startsWith(`${root}/`)) return false;
    const leaf = entry.slice(root.length + 1);
    return /^[\w.-]+$/u.test(leaf) && (/\.(?:hex|eep)$/u.test(leaf) ||
      /^(?:firmware-manifest\.json|dependencies\.txt|[\w-]*dependencies\.json|resolved-[\w-]+\.json)$/u.test(leaf));
  });
  for (const required of ["firmware-manifest.json", "PCController.ino.hex", "safe-default-eeprom.hex", "PCController.ino.with_bootloader.hex"]) {
    if (!selected.includes(`${root}/${required}`)) throw new Error(`Firmware archive missing ${required}`);
  }
  mkdirSync(destination, { recursive: true });
  for (const entry of selected) {
    writeFileSync(resolve(destination, basename(entry)), execFileSync("tar", ["-xOzf", archive, entry], options));
  }
  return selected.length;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [directory, destination] = process.argv.slice(2);
  if (!directory || !destination) throw new Error("Usage: unpack-firmware-artifact.mjs DOWNLOAD_DIRECTORY DESTINATION");
  console.log(`Unpacked ${unpackFirmwareArtifact(resolve(directory), resolve(destination))} verified firmware files`);
}
