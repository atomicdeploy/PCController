import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  actionPinFindings,
  isGeneratedOrBinaryPath,
  isOrdinaryTextFile,
  markdownAnchors,
  privacyFindings,
  readOrdinaryTextFile,
} from "./repository-policy.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

test("reusable hosts preserve and assert the exact firmware-job defaults", () => {
  const host = readFileSync(resolve(root, ".github/workflows/host.yml"), "utf8");
  const clean = host.indexOf("Clean generated host output before accepting firmware");
  const download = host.indexOf("Download the exact validated firmware defaults");
  const build = host.indexOf("Build and validate complete Unix package");
  const assertDefaults = host.indexOf("assert-host-defaults.mjs");
  assert.ok(clean >= 0 && clean < download && download < build && build < assertDefaults);
  assert.doesNotMatch(host, /--host-only --clean/u);

  for (const workflow of ["build.yml", "release.yml"]) {
    const source = readFileSync(resolve(root, ".github/workflows", workflow), "utf8");
    assert.match(source, /needs:\s*(?:\[preflight, firmware\]|firmware)/u);
    assert.match(
      source,
      /firmware_artifact_name:\s*\$\{\{ needs\.firmware\.outputs\.artifact_name \}\}/u,
    );
  }
  const firmware = readFileSync(resolve(root, ".github/workflows/firmware.yml"), "utf8");
  assert.match(firmware, /safe 1 KiB default EEPROM image/u);
  assert.match(firmware, /assert-firmware-defaults\.mjs/u);
});

test("workflow actions require immutable commits plus readable version comments", () => {
  assert.equal(
    actionPinFindings(
      ".github/workflows/build.yml",
      "- uses: actions/checkout@" + "a".repeat(40) + " # v7\n",
    ).length,
    0,
  );
  assert.equal(
    actionPinFindings(
      ".github/workflows/build.yml",
      "- uses: actions/checkout@v7\n",
    )[0].kind,
    "floating GitHub Action reference",
  );
});

test("privacy policy scans private paths, stale origins, and named external projects", () => {
  const findings = privacyFindings(
    "docs/bad.md",
    "C:\\Users\\Example\\Desktop\\work\\file.txt\nCopied from " +
      ["ASA", "0002E"].join("") + ".\n",
    { repository: "owner/current" },
  );
  assert.deepEqual(
    findings.map((item) => item.kind),
    ["private path", "external project name", "stale origin language"],
  );
  assert.deepEqual(findings.map((item) => item.line), [1, 2, 2]);
});

test("repository URLs have narrow product and provenance allowlists", () => {
  assert.deepEqual(
    privacyFindings(
      "README.md",
      "https://github.com/owner/current",
      { repository: "owner/current" },
    ),
    [],
  );
  assert.deepEqual(
    privacyFindings(
      "THIRD_PARTY_NOTICES.md",
      "https://github.com/vendor/dependency",
      { repository: "owner/current" },
    ),
    [],
  );
  assert.deepEqual(
    privacyFindings(
      "Tools/Dependencies/pr-plan.mjs",
      "https://github.com/vendor/dependency/releases",
      { repository: "owner/current" },
    ),
    [],
  );
  assert.equal(
    privacyFindings(
      "Project/Feature.cpp",
      "https://github.com/other/private-project",
      { repository: "owner/current" },
    )[0].kind,
    "unreviewed repository reference",
  );
});

test("generated and ignored-equivalent paths are not text-scan candidates", () => {
  for (const path of [
    ".cache/private/turns.jsonl",
    ".build/firmware/image.hex",
    "Tools/Controller/internal/webui/dist/assets/app.js",
    "Tools/Controller/.bin-previous-42/controller.exe",
  ]) {
    assert.equal(isGeneratedOrBinaryPath(path), true, path);
  }
  assert.equal(isGeneratedOrBinaryPath("Project/Controller.cpp"), false);
});

test("ordinary text detection reads one opened file and rejects binary or oversized input", () => {
  const directory = mkdtempSync(join(tmpdir(), "repository-policy-text-"));
  try {
    const textPath = join(directory, "plain.txt");
    const binaryPath = join(directory, "binary.dat");
    const oversizedPath = join(directory, "oversized.txt");
    writeFileSync(textPath, "plain text\n", "utf8");
    writeFileSync(binaryPath, Buffer.from([0x70, 0x00, 0x71]));
    writeFileSync(oversizedPath, Buffer.alloc(2 * 1024 * 1024 + 1, 0x61));

    assert.equal(isOrdinaryTextFile(textPath), true);
    assert.equal(readOrdinaryTextFile(textPath), "plain text\n");
    assert.equal(isOrdinaryTextFile(binaryPath), false);
    assert.equal(readOrdinaryTextFile(binaryPath), null);
    assert.equal(isOrdinaryTextFile(oversizedPath), false);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("Markdown heading anchors ignore examples and disambiguate duplicates", () => {
  const anchors = markdownAnchors(`
# Getting Started
## API, RPC & WebSocket
## Repeated heading
## Repeated heading
<a id="explicit-anchor"></a>
\`\`\`markdown
## Example only
\`\`\`
### **Styled** [section](https://example.invalid)
`);
  assert.deepEqual([...anchors], [
    "getting-started",
    "api-rpc--websocket",
    "repeated-heading",
    "repeated-heading-1",
    "explicit-anchor",
    "styled-section",
  ]);
  assert.equal(anchors.has("example-only"), false);
});

test("Markdown heading anchors cannot recreate markup while simplifying labels", () => {
  const anchors = markdownAnchors(`
# <kbd>Control</kbd>
# [<scr](https://example.invalid)[ipt>](https://example.invalid) Visible
# &lt;script&gt;Literal&lt;/script&gt;
`);
  assert.deepEqual([...anchors], ["control", "visible", "scriptliteralscript"]);
});
