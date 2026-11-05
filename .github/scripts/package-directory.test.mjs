import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  mkdirSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const script = resolve(root, ".github", "scripts", "package-directory.mjs");
const version = "9.8.7-test";

test("Windows Host and VirtualBoard are reproducible portable ZIPs with no inner archive", () => {
  const source = mkdtempSync(join(tmpdir(), "pccontroller-zip-source-"));
  const extraction = mkdtempSync(join(tmpdir(), "pccontroller-zip-extract-"));
  try {
    mkdirSync(join(source, "assets"));
    writeFileSync(join(source, "controller.exe"), "fixture executable");
    writeFileSync(join(source, "assets", "toast image.png"), "fixture artwork");
    for (const product of ["Host", "VirtualBoard"]) {
      const packageRoot = `PCController-${product}-${version}-Windows-x64-Portable`;
      const archive = resolve(root, ".build", "release", `${packageRoot}.zip`);
      const build = () => {
        const result = spawnSync(process.execPath, [script, source, `PCController-${product}`, "Windows-x64"], {
          cwd: root, encoding: "utf8", env: { ...process.env, PCCONTROLLER_VERSION: version,
            SOURCE_DATE_EPOCH: "1700000000", GITHUB_REPOSITORY: "atomicdeploy/PCController", GITHUB_SHA: "test-commit" },
        });
        assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
        return readFileSync(archive);
      };
      const first = build(), second = build();
      assert.equal(second.subarray(0, 4).toString("hex"), "504b0304");
      assert.deepEqual(second, first);
      assert.equal(readFileSync(`${archive}.sha256`, "utf8"), `${createHash("sha256").update(second).digest("hex")}  ${packageRoot}.zip\n`);
      const unpack = process.platform === "win32"
        ? spawnSync("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command", "Expand-Archive -LiteralPath $env:ZIP_INPUT -DestinationPath $env:ZIP_OUTPUT -Force"], {
            encoding: "utf8", windowsHide: true, env: { ...process.env, ZIP_INPUT: archive, ZIP_OUTPUT: extraction },
          })
        : spawnSync("unzip", ["-q", archive, "-d", extraction], { encoding: "utf8" });
      assert.equal(unpack.status, 0, unpack.stderr);
      const extracted = join(extraction, packageRoot);
      assert.ok(!readdirSync(extracted, { recursive: true }).some(name => /\.(?:zip|tar|tar\.gz)$/iu.test(name)), "nested distribution archive");
      assert.equal(readFileSync(join(extracted, "assets", "toast image.png"), "utf8"), "fixture artwork");
      assert.equal(readFileSync(join(extracted, "controller.exe"), "utf8"), "fixture executable");
      assert.match(readFileSync(join(extracted, "README.md"), "utf8"), /Extract All/u);
      const manifest = JSON.parse(readFileSync(join(extracted, "PACKAGE-MANIFEST.json"), "utf8"));
      assert.equal(manifest.target, "Windows-x64");
      assert.equal(manifest.packageRoot, packageRoot);
    }
  } finally {
    rmSync(source, { recursive: true, force: true });
    rmSync(extraction, { recursive: true, force: true });
  }
});

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
