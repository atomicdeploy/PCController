import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  applyReport,
  compareVersions,
  currentOutputs,
  loadConfig,
  pullRequestTitle,
  renderSummary,
  serializableReport,
  synchronizeDependencyDocumentation,
  synchronizeDependencyMentions,
  validateConfig,
  validateFirmwareDependencyInventory,
} from "./dependency-report.mjs";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const SCRIPT = path.join(SCRIPT_DIR, "dependency-report.mjs");
const CONFIG = path.resolve(SCRIPT_DIR, "../dependencies.json");

test("canonical dependency configuration validates without network access", () => {
  const { config } = loadConfig(CONFIG);
  assert.deepEqual(validateConfig(config), []);
  assert.equal(config.arduinoCli.version, "1.5.1");
  assert.equal(config.arduinoCli.linux64.sha256, "28a8e119c498a25607821c36cb2dc49e8463941b261a0d99091baa7bc692dd2b");
  assert.equal(config.miniCore.version, "3.1.2");
  assert.equal(config.libraries["rc-switch"].version, "2.6.4");
  assert.equal(config.miniCore.archive.sha256, "2aa8523aa24eb20f8277d90480286399c67a59b05164ac3bad81716a0c6490b3");
  assert.equal(config.libraries["rc-switch"].archive.sha256, "ed53920db3a38debe53e43b3bf8849ec3583aa419ef1f421a45662bcf056e014");
  assert.deepEqual(validateFirmwareDependencyInventory(config, path.resolve(SCRIPT_DIR, "../..")), []);
});

test("validation rejects an untrusted board-manager index and mismatched asset", () => {
  const { config } = loadConfig(CONFIG);
  const invalid = structuredClone(config);
  invalid.miniCore.packageIndex = "https://example.invalid/package.json";
  invalid.arduinoCli.linux64.asset = "latest.tar.gz";
  invalid.libraries["rc-switch"].archive.url = "https://example.invalid/rc-switch.zip";
  invalid.libraries["rc-switch"].repository = "example/rc-switch";
  invalid.libraries["bad$(name)"] = structuredClone(invalid.libraries["rc-switch"]);
  const errors = validateConfig(invalid);
  assert.ok(errors.some((error) => error.includes("official MiniCore")));
  assert.ok(errors.some((error) => error.includes("must match the pinned version")));
  assert.ok(errors.some((error) => error.includes("official Arduino library CDN")));
  assert.ok(errors.some((error) => error.includes("invalid Arduino Library Manager name")));
  assert.ok(errors.some((error) => error.includes("code-reviewed release-source allowlist")));
});

test("outbound release-note sources are code allowlisted rather than manifest-derived", () => {
  const source = fs.readFileSync(SCRIPT, "utf8");
  assert.match(source, /case "rc-switch":\s*return "sui77\/rc-switch"/u);
  assert.match(source, /repos\/arduino\/arduino-cli\/releases\?per_page/u);
  assert.match(source, /releaseEvidence\("MCUdude\/MiniCore", version\)/u);
  assert.doesNotMatch(source, /repos\/\$\{config\./u);
  assert.doesNotMatch(source, /releaseEvidence\(config\./u);
});

test("export exposes every build pin through stable GitHub output names", () => {
  const { config } = loadConfig(CONFIG);
  assert.deepEqual(currentOutputs(config), {
    arduino_cli_version: "1.5.1",
    arduino_cli_asset: "arduino-cli_1.5.1_Linux_64bit.tar.gz",
    arduino_cli_download_url: "https://github.com/arduino/arduino-cli/releases/download/v1.5.1/arduino-cli_1.5.1_Linux_64bit.tar.gz",
    arduino_cli_linux_64_sha256: "28a8e119c498a25607821c36cb2dc49e8463941b261a0d99091baa7bc692dd2b",
    minicore_version: "3.1.2",
    minicore_package: "MiniCore:avr",
    minicore_index_url: "https://mcudude.github.io/MiniCore/package_MCUdude_MiniCore_index.json",
    arduino_libraries_json: '["rc-switch@2.6.4"]',
  });
});

test("CLI export writes GitHub output format without performing a network request", () => {
  const temporaryDirectory = fs.mkdtempSync(path.join(os.tmpdir(), "pccontroller-deps-"));
  const output = path.join(temporaryDirectory, "github-output.txt");
  try {
    const result = spawnSync(process.execPath, [SCRIPT, "export", "--config", CONFIG, "--output", output], { encoding: "utf8" });
    assert.equal(result.status, 0, result.stderr);
    const contents = fs.readFileSync(output, "utf8");
    assert.match(contents, /^arduino_cli_version=1\.5\.1$/m);
    assert.match(contents, /^minicore_version=3\.1\.2$/m);
    assert.match(contents, /^arduino_libraries_json=\["rc-switch@2\.6\.4"\]$/m);
  } finally {
    fs.rmSync(temporaryDirectory, { recursive: true, force: true });
  }
});

test("semantic versions are compared numerically", () => {
  assert.equal(compareVersions("1.10.0", "1.9.9"), 1);
  assert.equal(compareVersions("3.1.2", "3.1.2"), 0);
  assert.equal(compareVersions("2.0.0-beta.1", "2.0.0"), -1);
});

test("dependency proposals use bounded component-aware titles", () => {
  assert.equal(
    pullRequestTitle([
      { label: "Arduino CLI" },
      { label: "MiniCore AVR core" },
      { label: "rc-switch library" },
      { label: "Future sensor library" },
    ]),
    "⬆️ AVR supply chain · Arduino CLI + MiniCore + rc-switch + 1 more",
  );
});

test("verified candidates update only the canonical pins and render the showcase", () => {
  const { config } = loadConfig(CONFIG);
  const report = {
    schemaVersion: 2,
    generatedAt: "2026-08-02T00:00:00.000Z",
    complete: true,
    updatesAvailable: true,
    applied: false,
    officialSources: {},
    dependencies: [
      {
        key: "arduinoCli",
        label: "Arduino CLI",
        current: "1.5.1",
        latest: "1.6.0",
        status: "update",
        updateAvailable: true,
        source: "https://github.com/arduino/arduino-cli/releases/tag/v1.6.0",
        candidate: {
          version: "1.6.0",
          asset: "arduino-cli_1.6.0_Linux_64bit.tar.gz",
          sha256: "a".repeat(64),
        },
      },
      {
        key: "miniCore",
        label: "MiniCore AVR core",
        current: "3.1.2",
        latest: "3.1.2",
        status: "current",
        updateAvailable: false,
        source: "https://github.com/MCUdude/MiniCore/releases",
      },
      {
        key: "library:rc-switch",
        label: "rc-switch library",
        current: "2.6.4",
        latest: "2.6.4",
        status: "current",
        updateAvailable: false,
        source: "https://github.com/sui77/rc-switch/releases",
      },
    ],
  };

  const updated = applyReport(config, report);
  assert.equal(updated.arduinoCli.version, "1.6.0");
  assert.equal(updated.arduinoCli.linux64.sha256, "a".repeat(64));
  assert.equal(config.arduinoCli.version, "1.5.1", "source object must not be mutated");
  const summary = renderSummary(report, updated);
  assert.match(summary, /# 🔭 PCController Dependency Radar/);
  assert.match(summary, /Reproducible firmware inputs/);
  assert.match(summary, /all-or-nothing/);
});

test("serialized reports retain only safe verified candidate evidence", () => {
  const candidate = {
    version: "1.6.0",
    source: "https://github.com/arduino/arduino-cli/releases/tag/v1.6.0",
    asset: "arduino-cli_1.6.0_Linux_64bit.tar.gz",
    sha256: "b".repeat(64),
    repository: "arduino/arduino-cli",
    archiveUrl: "https://github.com/arduino/arduino-cli/releases/download/v1.6.0/arduino-cli_1.6.0_Linux_64bit.tar.gz",
    archiveSha256: "b".repeat(64),
    releaseNotesUrl: "https://github.com/arduino/arduino-cli/releases/tag/v1.6.0",
    releaseBody: "must-not-be-copied",
    responseHeaders: { authorization: "must-not-leak" },
    token: "must-not-leak",
  };
  const report = {
    schemaVersion: 2,
    dependencies: [
      {
        key: "arduinoCli",
        current: "1.5.1",
        latest: "1.6.0",
        candidate,
      },
      { key: "miniCore", current: "3.1.2", latest: null, candidate: null },
    ],
  };

  const serialized = serializableReport(report);
  assert.deepEqual(serialized.dependencies[0].candidate, {
    version: "1.6.0",
    source: "https://github.com/arduino/arduino-cli/releases/tag/v1.6.0",
    asset: "arduino-cli_1.6.0_Linux_64bit.tar.gz",
    sha256: "b".repeat(64),
    repository: "arduino/arduino-cli",
    archiveUrl: "https://github.com/arduino/arduino-cli/releases/download/v1.6.0/arduino-cli_1.6.0_Linux_64bit.tar.gz",
    archiveSha256: "b".repeat(64),
    releaseNotesUrl: "https://github.com/arduino/arduino-cli/releases/tag/v1.6.0",
  });
  assert.equal("token" in serialized.dependencies[0].candidate, false);
  assert.equal("responseHeaders" in serialized.dependencies[0].candidate, false);
  assert.equal("releaseBody" in serialized.dependencies[0].candidate, false);
  assert.equal("candidate" in serialized.dependencies[1], false);
  assert.equal(candidate.token, "must-not-leak", "source report must not be mutated");
});

test("documentation synchronization updates only explicit dependency-version forms", () => {
  const current = {
    arduinoCli: { version: "1.5.1" },
    miniCore: { version: "3.1.2" },
    libraries: { "rc-switch": { version: "2.6.4" } },
  };
  const next = {
    arduinoCli: { version: "1.6.0" },
    miniCore: { version: "3.2.0" },
    libraries: { "rc-switch": { version: "2.7.0" } },
  };
  const fixture = [
    "Arduino CLI 1.5.1 is the current tool.",
    "Use `arduino-cli@1.5.1` or arduino-cli_1.5.1_Linux_64bit.tar.gz.",
    "MiniCore 3.1.2 and MiniCore:avr@3.1.2 are pinned.",
    "| [MiniCore](https://example.test/MiniCore) | `3.1.2` |",
    "rc-switch 2.6.4 and `rc-switch` version: 2.6.4 are linked.",
    "| [rc-switch](https://example.test/rc-switch) | 2.6.4 |",
    "Historical measurement 3.1.2 and MightyCore 3.1.2 stay unchanged.",
    "Unrelated device firmware 2.6.4 and rc-switch 2.6.40 stay unchanged.",
    "",
  ].join("\n");

  const synchronized = synchronizeDependencyMentions(fixture, current, next);
  assert.match(synchronized, /Arduino CLI 1\.6\.0/u);
  assert.match(synchronized, /`arduino-cli@1\.6\.0`/u);
  assert.match(synchronized, /arduino-cli_1\.6\.0_Linux_64bit/u);
  assert.match(synchronized, /MiniCore 3\.2\.0 and MiniCore:avr@3\.2\.0/u);
  assert.match(synchronized, /\[MiniCore\][^\n]+\| `3\.2\.0` \|/u);
  assert.match(synchronized, /rc-switch 2\.7\.0 and `rc-switch` version: 2\.7\.0/u);
  assert.match(synchronized, /\[rc-switch\][^\n]+\| 2\.7\.0 \|/u);
  assert.match(synchronized, /Historical measurement 3\.1\.2 and MightyCore 3\.1\.2 stay unchanged/u);
  assert.match(synchronized, /Unrelated device firmware 2\.6\.4 and rc-switch 2\.6\.40 stay unchanged/u);
});

test("documentation synchronization writes only existing curated files", () => {
  const repository = fs.mkdtempSync(path.join(os.tmpdir(), "pccontroller-doc-sync-"));
  const docs = path.join(repository, "docs");
  fs.mkdirSync(docs, { recursive: true });
  fs.writeFileSync(
    path.join(repository, "README.md"),
    "MiniCore 3.1.2\nTRAILING-SENTINEL\n",
    "utf8",
  );
  fs.writeFileSync(
    path.join(docs, "Project-Checklist.md"),
    "Current rc-switch 2.6.4; unrelated 2.6.4.\n",
    "utf8",
  );
  fs.writeFileSync(
    path.join(docs, "Historical-Record.md"),
    "Historical MiniCore 3.1.2\n",
    "utf8",
  );
  const current = {
    arduinoCli: { version: "1.5.1" },
    miniCore: { version: "3.1.2" },
    libraries: { "rc-switch": { version: "2.6.4" } },
  };
  const next = {
    arduinoCli: { version: "1.6.0" },
    miniCore: { version: "3.20.0" },
    libraries: { "rc-switch": { version: "3.0" } },
  };

  try {
    const changed = synchronizeDependencyDocumentation(current, next, repository);
    assert.deepEqual(changed, ["README.md", "docs/Project-Checklist.md"]);
    assert.equal(
      fs.readFileSync(path.join(repository, "README.md"), "utf8"),
      "MiniCore 3.20.0\nTRAILING-SENTINEL\n",
    );
    assert.equal(
      fs.readFileSync(path.join(docs, "Project-Checklist.md"), "utf8"),
      "Current rc-switch 3.0; unrelated 2.6.4.\n",
    );
    assert.equal(
      fs.readFileSync(path.join(docs, "Historical-Record.md"), "utf8"),
      "Historical MiniCore 3.1.2\n",
    );
  } finally {
    fs.rmSync(repository, { recursive: true, force: true });
  }
});

test("verified core and library candidates preserve archive provenance and bounded release context", () => {
  const { config } = loadConfig(CONFIG);
  const report = {
    schemaVersion: 2,
    generatedAt: "2026-08-02T00:00:00.000Z",
    complete: true,
    updatesAvailable: true,
    applied: false,
    officialSources: {},
    dependencies: [
      {
        key: "miniCore",
        label: "MiniCore AVR core",
        current: "3.1.2",
        latest: "3.2.0",
        status: "update",
        updateAvailable: true,
        source: "https://mcudude.github.io/MiniCore/package_MCUdude_MiniCore_index.json",
        candidate: {
          version: "3.2.0",
          archiveUrl: "https://MCUdude.github.io/MiniCore/MiniCore-3.2.0.tar.bz2",
          archiveSha256: "c".repeat(64),
          releaseNotesUrl: "https://github.com/MCUdude/MiniCore/releases/tag/v3.2.0",
          releaseTitle: "MiniCore 3.2.0",
        },
      },
      {
        key: "library:rc-switch",
        label: "rc-switch library",
        current: "2.6.4",
        latest: "2.7.0",
        status: "update",
        updateAvailable: true,
        source: "https://downloads.arduino.cc/libraries/library_index.json.gz",
        candidate: {
          version: "2.7.0",
          archiveUrl: "https://downloads.arduino.cc/libraries/github.com/sui77/rc_switch-2.7.0.zip",
          archiveSha256: "d".repeat(64),
          releaseNotesUrl: "https://github.com/sui77/rc-switch/releases/tag/v2.7.0",
          releaseTitle: "Release notes with\nnewlines",
        },
      },
    ],
  };

  const updated = applyReport(config, report);
  assert.equal(updated.miniCore.version, "3.2.0");
  assert.equal(updated.miniCore.archive.sha256, "c".repeat(64));
  assert.equal(updated.libraries["rc-switch"].version, "2.7.0");
  assert.equal(updated.libraries["rc-switch"].archive.sha256, "d".repeat(64));
  assert.deepEqual(validateConfig(updated), []);

  const summary = renderSummary(report, updated);
  assert.match(summary, /## 🧭 Upgrade review/u);
  assert.match(summary, /MiniCore-3\.2\.0\.tar\.bz2/u);
  assert.match(summary, /releases\/tag\/v2\.7\.0/u);
  assert.match(summary, /Release notes with newlines/u);
  assert.match(summary, /Release bodies are not copied/u);
});

test("firmware dependency inventory rejects undeclared and stale third-party headers", () => {
  const repository = fs.mkdtempSync(path.join(os.tmpdir(), "pccontroller-inventory-"));
  const { config } = loadConfig(CONFIG);
  try {
    fs.writeFileSync(path.join(repository, "PCController.ino"), "#include <Arduino.h>\n#include <RCSwitch.h>\n", "utf8");
    assert.deepEqual(validateFirmwareDependencyInventory(config, repository), []);

    fs.appendFileSync(path.join(repository, "PCController.ino"), "#include <MysteryDevice.h>\n", "utf8");
    assert.deepEqual(
      validateFirmwareDependencyInventory(config, repository),
      ["firmware include <MysteryDevice.h> is not owned by a pinned library"],
    );

    fs.writeFileSync(path.join(repository, "PCController.ino"), "#include <Arduino.h>\n", "utf8");
    assert.deepEqual(
      validateFirmwareDependencyInventory(config, repository),
      ["libraries.rc-switch.includes declares unused firmware header RCSwitch.h"],
    );
  } finally {
    fs.rmSync(repository, { recursive: true, force: true });
  }
});

test("dependency workflow keeps daily review-only updates behind a real AVR preflight", () => {
  const workflow = fs.readFileSync(path.resolve(SCRIPT_DIR, "../workflows/dependencies.yml"), "utf8");
  assert.match(workflow, /cron:\s*"23 3 \* \* \*"/u);
  assert.match(workflow, /options:\s*\n\s*- check\s*\n\s*- apply/u);
  assert.match(workflow, /node \.github\/scripts\/dependency-report\.mjs apply/u);
  assert.match(workflow, /\.\/build\.sh\s+--firmware-only\s+--clean/u);
  assert.match(workflow, /\.\/firmware\.sh check/u);
  assert.match(workflow, /dependencies-\$\{GITHUB_RUN_ID\}-\$\{GITHUB_RUN_ATTEMPT\}/u);
  assert.match(workflow, /PR_TITLE: \$\{\{ steps\.refresh\.outputs\.pull_request_title \}\}/u);
  assert.match(workflow, /--title "\$PR_TITLE"/u);
  assert.doesNotMatch(workflow, /GH_PAT/u);
});
