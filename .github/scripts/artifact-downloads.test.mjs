import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { findDistributionArtifact, findDistributionChecksum, distributionDownloadLabel, distributionSuffix } from "./artifact-downloads.mjs";

test("versioned raw archives resolve every target without selecting checksum artifacts", () => {
  for (const product of ["Host", "VirtualBoard"]) {
    for (const target of ["Windows-x64", "Linux-x64", "Linux-ARM64", "macOS-Intel", "macOS-Apple-Silicon"]) {
      const archive = { id: 1, name: `PCController-${product}-1.2.3-alpha.1-${target}${distributionSuffix(target)}` };
      const checksum = { id: 2, name: `${archive.name}.sha256` };
      assert.equal(findDistributionArtifact([checksum, archive], `PCController-${product}-${target}`), archive);
      assert.equal(findDistributionChecksum([checksum, archive], archive), checksum);
      assert.equal(distributionDownloadLabel(archive), target.startsWith("Windows") ? "Download ZIP" : "Download .tar.gz");
      assert.equal(findDistributionArtifact([checksum], `PCController-${product}-${target}`), undefined);
      assert.equal(findDistributionChecksum([{ ...checksum, expired: true }], archive), undefined);
    }
  }
  const firmware = { id: 3, name: "PCController-Firmware-1.2.3-AVR-ATmega328P.tar.gz" };
  assert.equal(findDistributionArtifact([firmware], "PCController-Firmware-ATmega328P"), firmware);
});

test("historical ZIP artifacts retain truthful labels and newest matching identity wins", () => {
  const legacy = { id: 10, name: "PCController-Host-Windows-x64" };
  const raw = { id: 11, name: "PCController-Host-test-Windows-x64.tar.gz" };
  assert.equal(distributionDownloadLabel(legacy), "Download ZIP");
  assert.equal(findDistributionArtifact([legacy], legacy.name), legacy);
  assert.equal(findDistributionArtifact([legacy, raw], legacy.name), raw);
  assert.equal(findDistributionArtifact([legacy, raw], "PCController-Host-Linux-x64"), undefined);
});

test("distribution workflows upload exactly one raw package and one raw checksum", () => {
  for (const workflow of ["host", "virtual-board", "firmware"]) {
    const source = readFileSync(new URL(`../workflows/${workflow}.yml`, import.meta.url), "utf8");
    for (const output of ["archive", "checksum"]) {
      assert.match(source, new RegExp(`path: \\$\\{\\{ steps\\.package\\.outputs\\.${output} \\}\\}\\r?\\n\\s+archive: false`, "u"));
    }
    assert.doesNotMatch(source, /\.build\/actions-artifact/u);
  }
  const firmware = readFileSync(new URL("../workflows/firmware.yml", import.meta.url), "utf8");
  assert.match(firmware, /artifact_name: \$\{\{ steps\.package\.outputs\.archive-name \}\}/u);
  for (const workflow of ["host", "release", "build"]) {
    const source = readFileSync(new URL(`../workflows/${workflow}.yml`, import.meta.url), "utf8");
    assert.match(source, /node \.github\/scripts\/unpack-firmware-artifact\.mjs/u);
  }
});
