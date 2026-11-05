import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { unpackFirmwareArtifact } from "./unpack-firmware-artifact.mjs";

const required = ["firmware-manifest.json", "PCController.ino.hex", "safe-default-eeprom.hex", "PCController.ino.with_bootloader.hex"];
function fixture(t, omitted = "") {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-raw-firmware-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const root = "PCController-Firmware-test-AVR-ATmega328P";
  const source = join(directory, "source");
  const download = join(directory, "download");
  const destination = join(directory, "unpacked");
  mkdirSync(join(source, root, "nested"), { recursive: true });
  mkdirSync(download);
  for (const name of [...required, "declared-dependencies.json", "README.md"]) {
    if (name !== omitted) writeFileSync(join(source, root, name), `fixture ${name}\n`);
  }
  writeFileSync(join(source, root, "nested", "unwanted.hex"), "not an input");
  const archive = join(download, `${root}.tar.gz`);
  execFileSync("tar", ["-czf", archive, "-C", source, root], { windowsHide: true });
  const digest = createHash("sha256").update(readFileSync(archive)).digest("hex");
  writeFileSync(`${archive}.sha256`, `${digest}  ${root}.tar.gz\n`);
  return { download, destination, archive };
}

test("raw firmware handoff verifies and extracts only top-level firmware evidence", t => {
  const { download, destination } = fixture(t);
  assert.equal(unpackFirmwareArtifact(download, destination), 5);
  assert.deepEqual(readdirSync(destination).sort(), [...required, "declared-dependencies.json"].sort());
  for (const name of required) assert.equal(readFileSync(join(destination, name), "utf8"), `fixture ${name}\n`);
});

test("firmware handoff rejects checksum corruption before writing", t => {
  const { download, destination, archive } = fixture(t);
  writeFileSync(archive, "tampered");
  assert.throws(() => unpackFirmwareArtifact(download, destination), /checksum mismatch/u);
  assert.throws(() => readdirSync(destination), { code: "ENOENT" });
});

test("firmware handoff rejects absent required images and ambiguous versions", t => {
  const { download, destination, archive } = fixture(t, "safe-default-eeprom.hex");
  assert.throws(() => unpackFirmwareArtifact(download, destination), /missing safe-default-eeprom.hex/u);
  copyFileSync(archive, join(download, "PCController-Firmware-other-AVR-ATmega328P.tar.gz"));
  assert.throws(() => unpackFirmwareArtifact(download, destination), /found 2/u);
});
