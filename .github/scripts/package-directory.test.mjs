import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  mkdtempSync,
  readFileSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const script = resolve(root, ".github", "scripts", "package-directory.mjs");
const version = "9.8.7-test";

function packageFirmware() {
  const result = spawnSync(
    process.execPath,
    [
      script,
      ".github/actionlint.yaml",
      "PCController-Firmware",
      "AVR-ATmega328P",
    ],
    {
      cwd: root,
      encoding: "utf8",
      env: {
        ...process.env,
        PCCONTROLLER_VERSION: version,
        SOURCE_DATE_EPOCH: "1700000000",
        GITHUB_REPOSITORY: "example-owner/example-project",
        GITHUB_SHA: "0123456789abcdef0123456789abcdef01234567",
      },
    },
  );
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  const archive = resolve(
    root,
    ".build",
    "release",
    `PCController-Firmware-${version}-AVR-ATmega328P.tar.gz`,
  );
  return {
    archive,
    sha256: createHash("sha256").update(readFileSync(archive)).digest("hex"),
  };
}

function packageHost() {
  const result = spawnSync(
    process.execPath,
    [script, ".github/actionlint.yaml", "PCController-Host", "Linux-x64"],
    {
      cwd: root,
      encoding: "utf8",
      env: {
        ...process.env,
        PCCONTROLLER_VERSION: version,
        SOURCE_DATE_EPOCH: "1700000000",
        GITHUB_REPOSITORY: "atomicdeploy/PCController",
        GITHUB_SHA: "0123456789abcdef0123456789abcdef01234567",
      },
    },
  );
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  return resolve(
    root,
    ".build",
    "release",
    `PCController-Host-${version}-Linux-x64.tar.gz`,
  );
}

test("package is reproducible and contains a self-contained target guide", () => {
  const first = packageFirmware();
  const second = packageFirmware();
  assert.equal(second.sha256, first.sha256);

  const extraction = mkdtempSync(join(tmpdir(), "pccontroller-package-"));
  try {
    const extracted = spawnSync(
      "tar",
      ["-xzf", second.archive, "-C", extraction],
      { encoding: "utf8" },
    );
    assert.equal(extracted.status, 0, extracted.stderr);
    const packageRoot = resolve(
      extraction,
      `PCController-Firmware-${version}-AVR-ATmega328P`,
    );
    const readme = readFileSync(resolve(packageRoot, "README.md"), "utf8");
    assert.match(readme, /PCController\.ino\.hex/u);
    assert.match(readme, /PCController\.ino\.with_bootloader\.hex/u);
    assert.match(readme, /safe-default-eeprom\.hex/u);
    assert.doesNotMatch(readme, /\]\((?:docs|Tools)\//u);

    const manifest = JSON.parse(
      readFileSync(resolve(packageRoot, "PACKAGE-MANIFEST.json"), "utf8"),
    );
    assert.equal(manifest.sourceCommit, "0123456789abcdef0123456789abcdef01234567");
    assert.equal(manifest.sourceRepository, "example-owner/example-project");
    assert.equal(manifest.packageRoot, `PCController-Firmware-${version}-AVR-ATmega328P`);
    assert.equal("workflowRun" in manifest, false);
  } finally {
    rmSync(extraction, { recursive: true, force: true });
  }
});

test("Host package uses Host distribution naming and keeps the controller executable", () => {
  const archive = packageHost();
  const extraction = mkdtempSync(join(tmpdir(), "pccontroller-host-package-"));
  try {
    const extracted = spawnSync("tar", ["-xzf", archive, "-C", extraction], {
      encoding: "utf8",
    });
    assert.equal(extracted.status, 0, extracted.stderr);
    const packageRoot = resolve(
      extraction,
      `PCController-Host-${version}-Linux-x64`,
    );
    const readme = readFileSync(resolve(packageRoot, "README.md"), "utf8");
    assert.match(readme, /^# PCController-Host /u);
    assert.match(readme, /## Run the Host/u);
    assert.match(readme, /\.\/controller --help/u);
    const manifest = JSON.parse(
      readFileSync(resolve(packageRoot, "PACKAGE-MANIFEST.json"), "utf8"),
    );
    assert.equal(manifest.product, "PCController-Host");
    assert.equal(manifest.packageRoot, `PCController-Host-${version}-Linux-x64`);
  } finally {
    rmSync(extraction, { recursive: true, force: true });
  }
});
