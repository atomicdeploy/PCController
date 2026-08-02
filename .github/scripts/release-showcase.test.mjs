import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { buildReleasePresentation } from "./release-showcase.mjs";

const TAG = "v0.1.0-alpha.1";
const SOURCE_SHA = "0123456789abcdef0123456789abcdef01234567";
const TARGETS = [
  "Linux-x64",
  "Linux-ARM64",
  "Windows-x64",
  "macOS-Intel",
  "macOS-Apple-Silicon",
];
const hash = (content) => createHash("sha256").update(content).digest("hex");

function fixture({ includeBootloader = true, omitFromCombined = "" } = {}) {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-release-showcase-"));
  const files = new Map();
  const add = (name, content = `fixture:${name}\n`) => {
    const value = Buffer.from(content);
    writeFileSync(join(directory, name), value);
    files.set(name, value);
  };

  add(`PCController-Firmware-${TAG}-AVR-ATmega328P.tar.gz`);
  add(`PCController-${TAG}-ATmega328P-Application.hex`, ":00000001FF\n");
  if (includeBootloader) {
    add(
      `PCController-${TAG}-ATmega328P-Full-Flash-Urboot.hex`,
      ":00000001FF\n",
    );
  }
  for (const target of TARGETS) {
    add(`PCController-Controller-${TAG}-${target}.tar.gz`);
    add(`PCController-VirtualBoard-${TAG}-${target}.tar.gz`);
  }
  add(
    "firmware-manifest.json",
    `${JSON.stringify({
      format: "pccontroller-avr-firmware-manifest/v1",
      target: { fqbn: "MiniCore:avr:328:bootloader=uart0" },
      stackBudget: {
        estimatedPeakSramBytes: 1764,
        estimatedFreeSramBytes: 284,
        sramCapacityBytes: 2048,
      },
      artifacts: [
        {
          role: "application",
          sha256: hash(":00000001FF\n"),
          dataBytes: 32228,
          capacityBytes: 32256,
          freeBytes: 28,
          usagePercent: 99.91,
        },
        {
          role: "flash+bootloader",
          sha256: hash(":00000001FF\n"),
          dataBytes: 32738,
          capacityBytes: 32768,
          freeBytes: 30,
          usagePercent: 99.91,
        },
      ],
    }, null, 2)}\n`,
  );

  for (const [name, content] of [...files]) {
    if (!name.endsWith(".tar.gz")) continue;
    add(`${name}.sha256`, `${hash(content)}  ${name}\n`);
  }
  const checksummed = [...files]
    .filter(
      ([name]) => !name.endsWith(".sha256") && name !== omitFromCombined,
    )
    .sort(([left], [right]) => left.localeCompare(right, "en"));
  add(
    "SHA256SUMS.txt",
    `${checksummed.map(([name, content]) => `${hash(content)}  ${name}`).join("\n")}\n`,
  );
  return directory;
}

const environment = {
  GITHUB_REPOSITORY: "example-owner/example-project",
  GITHUB_SERVER_URL: "https://github.com",
  GITHUB_RUN_ID: "30718662898",
  GITHUB_RUN_ATTEMPT: "2",
};

test("generates a deterministic chooser, release body, and release manifest", () => {
  const directory = fixture();
  const summaryPath = `${directory}-step-summary.md`;
  const outputPath = `${directory}-github-output.txt`;
  const runnerEnvironment = {
    ...environment,
    GITHUB_STEP_SUMMARY: summaryPath,
    GITHUB_OUTPUT: outputPath,
  };
  try {
    const first = buildReleasePresentation({
      assetsDirectory: directory,
      tag: TAG,
      sourceSha: SOURCE_SHA,
      environment: runnerEnvironment,
    });
    const firstNotes = readFileSync(first.notesPath, "utf8");
    const firstManifest = readFileSync(first.manifestPath, "utf8");

    assert.match(firstNotes, /^# 🚀 PCController v0\.1\.0-alpha\.1/u);
    assert.match(firstNotes, /Linux x64/u);
    assert.match(firstNotes, /macOS Apple Silicon/u);
    assert.match(firstNotes, /Application only/u);
    assert.match(firstNotes, /Flash \+ bootloader/u);
    assert.match(firstNotes, /gh attestation verify/u);
    assert.match(firstNotes, /physical controller board/u);
    assert.doesNotMatch(firstNotes, /What's Changed/u);

    const parsed = JSON.parse(firstManifest);
    assert.equal(parsed.format, "pccontroller-release-manifest/v1");
    assert.equal(parsed.release.sourceSha, SOURCE_SHA);
    assert.equal(parsed.release.prerelease, true);
    assert.equal(parsed.chooser.platforms.length, 5);
    assert.equal(parsed.validation.length, 11);
    assert.equal(parsed.firmware.flash.freeBytes, 28);
    assert.deepEqual(parsed.firmware.peakSram, {
      usedBytes: 1764,
      capacityBytes: 2048,
      freeBytes: 284,
    });
    assert.equal(parsed.integrity.validatedSidecars.length, 11);
    assert.ok(parsed.assets.every((asset) => /^[0-9a-f]{64}$/u.test(asset.sha256)));
    assert.ok(!parsed.assets.some((asset) => asset.file === "RELEASE-NOTES.md"));
    assert.match(readFileSync(summaryPath, "utf8"), /geps\.dev\/progress\/99/u);
    assert.match(
      readFileSync(outputPath, "utf8"),
      /release_notes=.*RELEASE-NOTES\.md/u,
    );

    const second = buildReleasePresentation({
      assetsDirectory: directory,
      tag: TAG,
      sourceSha: SOURCE_SHA,
      environment: runnerEnvironment,
    });
    assert.equal(readFileSync(second.notesPath, "utf8"), firstNotes);
    assert.equal(readFileSync(second.manifestPath, "utf8"), firstManifest);
  } finally {
    rmSync(directory, { recursive: true, force: true });
    rmSync(summaryPath, { force: true });
    rmSync(outputPath, { force: true });
  }
});

test("rejects a checksum sidecar that does not match its archive", () => {
  const directory = fixture();
  try {
    const archive = `PCController-Controller-${TAG}-Linux-x64.tar.gz`;
    writeFileSync(join(directory, archive), "corrupted\n");
    assert.throws(
      () =>
        buildReleasePresentation({
          assetsDirectory: directory,
          tag: TAG,
          sourceSha: SOURCE_SHA,
          environment,
        }),
      /does not match/u,
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("rejects an incomplete direct firmware image set", () => {
  const directory = fixture({ includeBootloader: false });
  try {
    assert.throws(
      () =>
        buildReleasePresentation({
          assetsDirectory: directory,
          tag: TAG,
          sourceSha: SOURCE_SHA,
          environment,
        }),
      /direct AVR flash-with-bootloader image; found 0/u,
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("requires SHA256SUMS coverage for every primary release payload", () => {
  const omitted = `PCController-${TAG}-ATmega328P-Application.hex`;
  const directory = fixture({ omitFromCombined: omitted });
  try {
    assert.throws(
      () =>
        buildReleasePresentation({
          assetsDirectory: directory,
          tag: TAG,
          sourceSha: SOURCE_SHA,
          environment,
        }),
      new RegExp(`SHA256SUMS\\.txt is missing primary asset ${omitted}`),
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("requires a full Git commit hash", () => {
  const directory = fixture();
  try {
    assert.throws(
      () =>
        buildReleasePresentation({
          assetsDirectory: directory,
          tag: TAG,
          sourceSha: "deadbeef",
          environment,
        }),
      /full 40- or 64-character/u,
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("binds direct firmware images to the canonical firmware manifest", () => {
  const directory = fixture();
  const application = `PCController-${TAG}-ATmega328P-Application.hex`;
  try {
    const corrupted = ":020000040000FA\n:00000001FF\n";
    writeFileSync(join(directory, application), corrupted);
    const sumsPath = join(directory, "SHA256SUMS.txt");
    const sums = readFileSync(sumsPath, "utf8").replace(
      new RegExp(`^[0-9a-f]{64}  ${application}$`, "mu"),
      `${hash(corrupted)}  ${application}`,
    );
    writeFileSync(sumsPath, sums);
    assert.throws(
      () =>
        buildReleasePresentation({
          assetsDirectory: directory,
          tag: TAG,
          sourceSha: SOURCE_SHA,
          environment,
        }),
      /does not match the firmware manifest application SHA-256/u,
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("draft mode emits no broken tag downloads and honors explicit release flags", () => {
  const directory = fixture();
  try {
    const result = buildReleasePresentation({
      assetsDirectory: directory,
      tag: TAG,
      sourceSha: SOURCE_SHA,
      environment: {
        ...environment,
        PCCONTROLLER_RELEASE_DRAFT: "true",
        PCCONTROLLER_RELEASE_PRERELEASE: "false",
      },
    });
    assert.doesNotMatch(result.notes, /\/releases\/download\/v0\.1\.0-alpha\.1/u);
    assert.match(result.notes, /This is a GitHub draft/u);
    assert.equal(result.manifest.release.draft, true);
    assert.equal(result.manifest.release.prerelease, false);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
