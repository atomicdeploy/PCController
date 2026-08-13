import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { assertFirmwareDefaults } from "./assert-firmware-defaults.mjs";
import { assertHostDefaults } from "./assert-host-defaults.mjs";

const hash = (value) => createHash("sha256").update(value).digest("hex");

test("firmware defaults require an exact application and 1 KiB EEPROM pair", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "firmware-defaults-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(join(root, "out"));
  await writeFile(join(root, "out", "application.hex"), "application");
  await writeFile(join(root, "out", "eeprom.hex"), "eeprom");
  const manifest = {
    format: "pccontroller-avr-firmware-manifest/v1",
    artifacts: [
      {
        role: "application", path: "out/application.hex", dataBytes: 12,
        sha256: hash("application"),
      },
      {
        role: "default-eeprom", path: "out/eeprom.hex", capacityBytes: 1024,
        dataBytes: 1024, startAddress: 0, endAddress: 1023, sha256: hash("eeprom"),
      },
    ],
  };
  assert.equal(assertFirmwareDefaults(manifest, root).eeprom.dataBytes, 1024);
	assert.equal(assertFirmwareDefaults({
		...manifest,
		format: "pccontroller-avr-firmware-manifest/v2",
		source: { compileFeatures: ["eeprom-menu-labels"] },
	}, root).application.dataBytes, 12);
	for (const [name, invalid, expected] of [
		["v1 features", { ...manifest, source: { compileFeatures: ["eeprom-menu-labels"] } }, /v1 cannot declare/u],
		["v2 empty", { ...manifest, format: "pccontroller-avr-firmware-manifest/v2" }, /v2 requires/u],
		["unknown", { ...manifest, format: "pccontroller-avr-firmware-manifest/v2", source: { compileFeatures: ["unknown"] } }, /unsupported firmware feature/u],
		["duplicate", { ...manifest, format: "pccontroller-avr-firmware-manifest/v2", source: { compileFeatures: ["eeprom-menu-labels", "eeprom-menu-labels"] } }, /unique and sorted/u],
		["unsorted", { ...manifest, format: "pccontroller-avr-firmware-manifest/v2", source: { compileFeatures: ["eeprom-menu-labels", "eeprom-boot-opcodes"] } }, /unique and sorted/u],
	]) {
		assert.throws(() => assertFirmwareDefaults(invalid, root), expected, name);
	}
  assert.throws(
    () => assertFirmwareDefaults({
      ...manifest,
      artifacts: manifest.artifacts.map((item) =>
        item.role === "default-eeprom" ? { ...item, dataBytes: 1023 } : item),
    }, root),
    /exactly bytes 0\.\.1023/u,
  );
});

test("host defaults report firmware and EEPROM independently enabled", () => {
  const manifest = {
    format: "pccontroller-host-package-manifest/v1",
    validation: {
      embeddedDefaults: {
        enabled: true,
        firmwareEnabled: true,
        eepromEnabled: true,
        firmwareSHA256: "a".repeat(64),
        eepromSHA256: "b".repeat(64),
        eepromDataBytes: 1024,
      },
    },
  };
  assert.equal(assertHostDefaults(manifest).eepromEnabled, true);
  assert.throws(
    () => assertHostDefaults({
      ...manifest,
      validation: { embeddedDefaults: { ...manifest.validation.embeddedDefaults, eepromEnabled: false } },
    }),
    /both embedded defaults enabled/u,
  );
});
