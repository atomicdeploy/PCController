import assert from "node:assert/strict";
import {
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { usageColor, usageProgress } from "./usage-progress.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const script = resolve(root, ".github", "scripts", "step-summary.mjs");
const commonEnvironment = {
  ...process.env,
  GITHUB_REPOSITORY: "atomicdeploy/PCController",
  GITHUB_SERVER_URL: "https://github.com",
  GITHUB_RUN_ID: "123",
  GITHUB_SHA: "0123456789abcdef0123456789abcdef01234567",
  GITHUB_WORKFLOW: "Build",
};

test("usage bars are green through 50, orange above 50, and red above 80", () => {
  assert.equal(usageColor(0), "28a745");
  assert.equal(usageColor(50), "28a745");
  assert.equal(usageColor(50.01), "fd7e14");
  assert.equal(usageColor(80), "fd7e14");
  assert.equal(usageColor(80.01), "dc3545");
  assert.equal(usageColor(100), "dc3545");

  assert.match(
    usageProgress(99.52, "Application flash"),
    /progress\/99\?dangerColor=dc3545&warningColor=dc3545&successColor=dc3545/u,
  );
});

function runSummary(directory, arguments_) {
  const summary = resolve(directory, "summary.md");
  const result = spawnSync(process.execPath, [script, ...arguments_], {
    cwd: root,
    encoding: "utf8",
    env: { ...commonEnvironment, GITHUB_STEP_SUMMARY: summary },
  });
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  return readFileSync(summary, "utf8");
}

test("native summaries print target-correct checksum commands", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-summary-"));
  try {
    const archive = resolve(directory, "PCController-Host-test-Windows-x64.tar.gz");
    const manifest = resolve(directory, "controller-manifest.json");
    writeFileSync(archive, "controller archive", "utf8");
    writeFileSync(
      manifest,
      JSON.stringify({
        identity: { version: "test", sourceSHA256: "abc" },
        target: { platform: "windows", architecture: "amd64" },
        validation: { tests: "passed", vet: "passed", sharedLibrary: "passed" },
        artifacts: [],
      }),
      "utf8",
    );
    const windows = runSummary(directory, [
      "host",
      manifest,
      archive,
      "Windows-x64",
    ]);
    assert.match(windows, /~~~powershell[\s\S]*Get-FileHash/u);
    assert.doesNotMatch(windows, /sha256sum -c/u);

    const binary = resolve(directory, "virtual_board");
    const simulatorArchive = resolve(
      directory,
      "PCController-VirtualBoard-test-macOS-Apple-Silicon.tar.gz",
    );
    writeFileSync(binary, "native simulator", "utf8");
    writeFileSync(simulatorArchive, "simulator archive", "utf8");
    const mac = runSummary(directory, [
      "simulator",
      "macOS-Apple-Silicon",
      binary,
      simulatorArchive,
    ]);
    assert.match(mac, /shasum -a 256 -c/u);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("catalog presents AVR separately with live artifact links", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-catalog-"));
  try {
    const targets = [
      "Linux-x64",
      "Linux-ARM64",
      "Windows-x64",
      "macOS-Intel",
      "macOS-Apple-Silicon",
    ];
    writeFileSync(
      resolve(directory, "PCController-Firmware-test-AVR-ATmega328P.tar.gz"),
      "firmware",
      "utf8",
    );
    for (const product of ["Controller", "VirtualBoard"]) {
      for (const target of targets) {
        writeFileSync(
          resolve(directory, `PCController-${product}-test-${target}.tar.gz`),
          `${product}-${target}`,
          "utf8",
        );
      }
    }
    writeFileSync(
      resolve(directory, "firmware-manifest.json"),
      JSON.stringify({
        artifacts: [
          {
            role: "application",
            dataBytes: 32228,
            capacityBytes: 32384,
            freeBytes: 156,
            usagePercent: 99.52,
          },
        ],
        stackBudget: {
          estimatedPeakSramBytes: 1764,
          sramCapacityBytes: 2048,
          estimatedFreeSramBytes: 284,
        },
      }),
      "utf8",
    );
    writeFileSync(
      resolve(directory, "artifact-index.json"),
      JSON.stringify({
        artifacts: [
          { id: 42, name: "PCController-Firmware-ATmega328P" },
          ...targets.flatMap((target, index) => [
            { id: 100 + index, name: `PCController-Host-${target}` },
            { id: 200 + index, name: `PCController-VirtualBoard-${target}` },
          ]),
        ],
      }),
      "utf8",
    );
    const catalog = runSummary(directory, ["catalog", directory]);
    assert.match(catalog, /## ⚡ AVR ATmega328P target/u);
    assert.match(catalog, /32,228 \/ 32,384 B/u);
    assert.match(catalog, /`PCController-Firmware-ATmega328P` \(`firmware` alias\)/u);
    assert.match(
      catalog,
      /actions\/runs\/123\/artifacts\/42/u,
    );
    assert.doesNotMatch(catalog, /AVR Firmware \| ✅ ATmega328P/u);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
