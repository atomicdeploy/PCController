import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { dirname, isAbsolute, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { loadProjectEnv } from "../../Tools/Build/env.mjs";

loadProjectEnv();

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

const sha256 = (path) =>
  createHash("sha256").update(readFileSync(path)).digest("hex");

function checkedArtifact(manifest, role, root) {
  const matches = (manifest.artifacts || []).filter((item) => item.role === role);
  if (matches.length !== 1) {
    throw new Error(`firmware manifest must contain exactly one ${role} artifact`);
  }
  const artifact = matches[0];
  const path = isAbsolute(artifact.path) ? artifact.path : resolve(root, artifact.path);
  if (!existsSync(path)) throw new Error(`${role} artifact is missing: ${path}`);
  if (sha256(path) !== String(artifact.sha256 || "").toLowerCase()) {
    throw new Error(`${role} artifact SHA-256 differs from firmware-manifest.json`);
  }
  return artifact;
}

export function assertFirmwareDefaults(manifest, root = repositoryRoot) {
  if (manifest?.format !== "pccontroller-avr-firmware-manifest/v1") {
    throw new Error("unexpected firmware manifest format");
  }
  const application = checkedArtifact(manifest, "application", root);
  if (Number(application.dataBytes) <= 0) {
    throw new Error("default application image is empty");
  }
  const eeprom = checkedArtifact(manifest, "default-eeprom", root);
  if (
    Number(eeprom.capacityBytes) !== 1024 ||
    Number(eeprom.dataBytes) !== 1024 ||
    Number(eeprom.startAddress) !== 0 ||
    Number(eeprom.endAddress) !== 1023
  ) {
    throw new Error("safe default EEPROM must cover exactly bytes 0..1023");
  }
  return { application, eeprom };
}

const isMain = process.argv[1] &&
  resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url));
if (isMain) {
  const path = resolve(process.argv[2] || "");
  if (!process.argv[2]) {
    process.stderr.write("Usage: assert-firmware-defaults.mjs MANIFEST\n");
    process.exitCode = 2;
  } else {
    try {
      const result = assertFirmwareDefaults(JSON.parse(readFileSync(path, "utf8")));
      process.stdout.write(
        `Validated firmware defaults: ${result.application.sha256} + EEPROM ${result.eeprom.sha256}\n`,
      );
    } catch (error) {
      process.stderr.write(`${error.message}\n`);
      process.exitCode = 1;
    }
  }
}
