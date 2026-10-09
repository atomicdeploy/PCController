import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const hashPattern = /^[0-9a-f]{64}$/u;

export function assertHostDefaults(manifest) {
  if (manifest?.format !== "pccontroller-host-package-manifest/v1") {
    throw new Error("unexpected host manifest format");
  }
  const defaults = manifest.validation?.embeddedDefaults;
  if (
    defaults?.enabled !== true ||
    defaults?.firmwareEnabled !== true ||
    defaults?.eepromEnabled !== true
  ) {
    throw new Error("host manifest does not report both embedded defaults enabled");
  }
  if (!hashPattern.test(String(defaults.firmwareSHA256 || ""))) {
    throw new Error("host manifest has no valid embedded firmware SHA-256");
  }
  if (!hashPattern.test(String(defaults.eepromSHA256 || ""))) {
    throw new Error("host manifest has no valid embedded EEPROM SHA-256");
  }
  if (Number(defaults.eepromDataBytes) !== 1024) {
    throw new Error("host manifest embedded EEPROM is not exactly 1,024 data bytes");
  }
  return defaults;
}

const isMain = process.argv[1] &&
  resolve(process.argv[1]) === resolve(fileURLToPath(import.meta.url));
if (isMain) {
  const path = resolve(process.argv[2] || "");
  if (!process.argv[2]) {
    process.stderr.write("Usage: assert-host-defaults.mjs MANIFEST\n");
    process.exitCode = 2;
  } else {
    try {
      const result = assertHostDefaults(JSON.parse(readFileSync(path, "utf8")));
      process.stdout.write(
        `Validated embedded host defaults: ${result.firmwareSHA256} + EEPROM ${result.eepromSHA256}\n`,
      );
    } catch (error) {
      process.stderr.write(`${error.message}\n`);
      process.exitCode = 1;
    }
  }
}
