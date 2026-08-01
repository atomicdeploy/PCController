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
  renderSummary,
  serializableReport,
  synchronizeDependencyDocumentation,
  synchronizeDependencyMentions,
  validateConfig,
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
  assert.equal(config.libraries["rc-switch"], "2.6.4");
});

test("validation rejects an untrusted board-manager index and mismatched asset", () => {
  const { config } = loadConfig(CONFIG);
  const invalid = structuredClone(config);
  invalid.miniCore.packageIndex = "https://example.invalid/package.json";
  invalid.arduinoCli.linux64.asset = "latest.tar.gz";
  const errors = validateConfig(invalid);
  assert.ok(errors.some((error) => error.includes("official MiniCore")));
  assert.ok(errors.some((error) => error.includes("must match the pinned version")));
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
    rc_switch_version: "2.6.4",
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
    assert.match(contents, /^rc_switch_version=2\.6\.4$/m);
  } finally {
    fs.rmSync(temporaryDirectory, { recursive: true, force: true });
  }
});

test("semantic versions are compared numerically", () => {
  assert.equal(compareVersions("1.10.0", "1.9.9"), 1);
  assert.equal(compareVersions("3.1.2", "3.1.2"), 0);
  assert.equal(compareVersions("2.0.0-beta.1", "2.0.0"), -1);
});

test("verified candidates update only the canonical pins and render the showcase", () => {
  const { config } = loadConfig(CONFIG);
  const report = {
    schemaVersion: 1,
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
        key: "rc-switch",
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
    responseHeaders: { authorization: "must-not-leak" },
    token: "must-not-leak",
  };
  const report = {
    schemaVersion: 1,
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
  });
  assert.equal("token" in serialized.dependencies[0].candidate, false);
  assert.equal("responseHeaders" in serialized.dependencies[0].candidate, false);
  assert.equal("candidate" in serialized.dependencies[1], false);
  assert.equal(candidate.token, "must-not-leak", "source report must not be mutated");
});

test("documentation synchronization updates only explicit dependency-version forms", () => {
  const current = {
    arduinoCli: { version: "1.5.1" },
    miniCore: { version: "3.1.2" },
    libraries: { "rc-switch": "2.6.4" },
  };
  const next = {
    arduinoCli: { version: "1.6.0" },
    miniCore: { version: "3.2.0" },
    libraries: { "rc-switch": "2.7.0" },
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
  fs.writeFileSync(path.join(repository, "README.md"), "MiniCore 3.1.2\n", "utf8");
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
    libraries: { "rc-switch": "2.6.4" },
  };
  const next = {
    arduinoCli: { version: "1.6.0" },
    miniCore: { version: "3.2.0" },
    libraries: { "rc-switch": "2.7.0" },
  };

  try {
    const changed = synchronizeDependencyDocumentation(current, next, repository);
    assert.deepEqual(changed, ["README.md", "docs/Project-Checklist.md"]);
    assert.equal(fs.readFileSync(path.join(repository, "README.md"), "utf8"), "MiniCore 3.2.0\n");
    assert.equal(
      fs.readFileSync(path.join(docs, "Project-Checklist.md"), "utf8"),
      "Current rc-switch 2.7.0; unrelated 2.6.4.\n",
    );
    assert.equal(
      fs.readFileSync(path.join(docs, "Historical-Record.md"), "utf8"),
      "Historical MiniCore 3.1.2\n",
    );
  } finally {
    fs.rmSync(repository, { recursive: true, force: true });
  }
});
