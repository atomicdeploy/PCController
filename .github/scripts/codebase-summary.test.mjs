import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  TARGETS,
  parseRunData,
  renderCodebaseSummary,
} from "./codebase-summary.mjs";

const context = {
  repository: "atomicdeploy/PCController",
  serverUrl: "https://github.com",
  runId: "12345",
  sha: "0123456789abcdef0123456789abcdef01234567",
};

const job = (id, codebase, target, conclusion = "success") => ({
  id,
  name: `Build / ${codebase} · ${target}`,
  status: "completed",
  conclusion,
  html_url: `https://github.com/atomicdeploy/PCController/actions/runs/12345/job/${id}`,
});

const artifact = (id, prefix, target, extra = {}) => ({
  id,
  name: `${prefix}-${target}`,
  size_in_bytes: 1536,
  digest: `sha256:${String(id).padStart(64, "a")}`,
  expired: false,
  workflow_run: { id: 12345 },
  ...extra,
});

const writePackage = (
  directory,
  prefix,
  target,
  { sidecar = "valid", version = "1.2.3-alpha.1" } = {},
) => {
  const archiveName = `${prefix}-${version}-${target}.tar.gz`;
  const archivePath = resolve(directory, archiveName);
  const content = Buffer.from(`native package for ${target}\n`, "utf8");
  const sha256 = createHash("sha256").update(content).digest("hex");
  writeFileSync(archivePath, content);
  if (sidecar === "directory") {
    mkdirSync(`${archivePath}.sha256`);
  } else if (sidecar !== "missing") {
    const recorded = sidecar === "mismatch" ? "0".repeat(64) : sha256;
    const recordedName = sidecar === "wrong-name" ? `wrong-${archiveName}` : archiveName;
    const checksum = sidecar === "malformed"
      ? "not a checksum\n"
      : `${recorded}  ${recordedName}\n`;
    writeFileSync(`${archivePath}.sha256`, checksum, "utf8");
  }
  return { archiveName, archivePath, bytes: content.length, sha256 };
};

const writeRunDocuments = (directory, codebase, prefix, overrides = {}) => {
  const jobsPath = resolve(directory, "jobs.json");
  const artifactsPath = resolve(directory, "artifacts.json");
  writeFileSync(
    jobsPath,
    JSON.stringify({
      jobs: TARGETS.map((target, index) =>
        job(
          index + 1,
          codebase,
          target.id,
          overrides.conclusions?.[target.id] || "success",
        ),
      ),
    }),
    "utf8",
  );
  writeFileSync(
    artifactsPath,
    JSON.stringify({
      artifacts: TARGETS
        .filter((target) => !overrides.missingArtifacts?.includes(target.id))
        .map((target, index) => artifact(index + 301, prefix, target.id)),
    }),
    "utf8",
  );
  return { jobsPath, artifactsPath };
};

const runCli = (directory, codebase, jobsPath, artifactsPath, packagesDirectory) => {
  const summaryPath = resolve(directory, "summary.md");
  const script = resolve(".github", "scripts", "codebase-summary.mjs");
  const arguments_ = [script, codebase, jobsPath, artifactsPath];
  if (packagesDirectory) arguments_.push(packagesDirectory);
  const result = spawnSync(process.execPath, arguments_, {
    encoding: "utf8",
    env: {
      ...process.env,
      GITHUB_REPOSITORY: context.repository,
      GITHUB_SERVER_URL: context.serverUrl,
      GITHUB_RUN_ID: context.runId,
      GITHUB_SHA: context.sha,
      GITHUB_STEP_SUMMARY: summaryPath,
    },
  });
  return { result, summaryPath };
};

test("run data parser accepts gh api documents and paginated --slurp output", () => {
  const parsed = parseRunData(
    [{ jobs: [{ id: 1 }] }, { jobs: [{ id: 2 }] }],
    [{ artifacts: [{ id: 3 }] }, { artifacts: [{ id: 4 }] }],
  );
  assert.deepEqual(parsed.jobs.map(({ id }) => id), [1, 2]);
  assert.deepEqual(parsed.artifacts.map(({ id }) => id), [3, 4]);
});

test("Host summary prints shared gates once and keeps every incomplete target visible", () => {
  const jobs = {
    jobs: [
      job(1, "🖥️ Host", "Linux-x64"),
      job(2, "🖥️ Host", "Linux-ARM64", "failure"),
      job(3, "🖥️ Host", "Windows-x64", "cancelled"),
      job(4, "🖥️ Host", "macOS-Intel", "skipped"),
      job(5, "✨ PCController / 🧪 Virtual Board", "macOS-Apple-Silicon"),
    ],
  };
  const artifacts = {
    artifacts: [
      artifact(101, "PCController-Host", "Linux-x64"),
      artifact(103, "PCController-Host", "Windows-x64", { expired: true }),
    ],
  };

  const summary = renderCodebaseSummary("host", jobs, artifacts, context);

  assert.match(summary, /^# ❌ 🖥️ Host builds/mu);
  assert.equal(summary.match(/`go test \.\/\.\.\.`/gu)?.length, 1);
  assert.equal(summary.match(/`go vet \.\/\.\.\.`/gu)?.length, 1);
  assert.match(summary, /## 🧪 Validation on successful targets/u);
  assert.doesNotMatch(summary, /## 🧪 Shared validation/u);
  assert.doesNotMatch(summary, /Win32 icon/u);
  assert.match(summary, /\| \*\*Linux-x64\*\* \| ubuntu-24\.04 · x64 \| \[✅ Passed\]/u);
  assert.match(summary, /\| \*\*Linux-ARM64\*\* \| ubuntu-24\.04-arm · ARM64 \| \[❌ Failed\]/u);
  assert.match(summary, /\| \*\*Windows-x64\*\* \| windows-2025 · x64 \| \[⛔ Cancelled\]/u);
  assert.match(summary, /\| \*\*macOS-Intel\*\* \| macos-15-intel · x64 \| \[⏭️ Skipped\]/u);
  assert.match(
    summary,
    /\| \*\*macOS-Apple-Silicon\*\* \| macos-15 · ARM64 \| ❓ Missing job \| ⚠️ Missing<br>`PCController-Host-macOS-Apple-Silicon` \|/u,
  );
  assert.match(summary, /PCController-Host-Linux-x64/u);
  assert.match(summary, /actions\/runs\/12345\/artifacts\/101/u);
  assert.match(summary, /Windows-x64: Cancelled; Expired/u);
  assert.doesNotMatch(summary, /PCController-(?:Controller)/u);
});

test("Virtual Board summary uses one five-platform table and reports a missing artifact", () => {
  const jobs = {
    jobs: TARGETS.map((target, index) =>
      job(index + 1, "🧪 Virtual Board", target.id),
    ),
  };
  const artifacts = {
    artifacts: TARGETS.slice(0, 4).map((target, index) =>
      artifact(index + 201, "PCController-VirtualBoard", target.id),
    ),
  };

  const summary = renderCodebaseSummary("virtual-board", jobs, artifacts, context);

  assert.match(summary, /^# ⚠️ 🧪 Virtual Board builds/mu);
  assert.match(summary, /5\/5 targets passed · 4\/5 artifacts available/u);
  assert.equal(summary.match(/CTest protocol/gu)?.length, 1);
  assert.equal(summary.match(/^\| \*\*/gmu)?.length, 5);
  assert.match(summary, /PCController-VirtualBoard-macOS-Intel/u);
  assert.match(summary, /PCController-VirtualBoard-macOS-Apple-Silicon/u);
  assert.match(summary, /macOS-Apple-Silicon: Missing/u);
});

test("CLI appends the aggregate summary to GITHUB_STEP_SUMMARY", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-codebase-summary-"));
  try {
    const jobsPath = resolve(directory, "jobs.json");
    const artifactsPath = resolve(directory, "artifacts.json");
    const summaryPath = resolve(directory, "summary.md");
    const script = resolve(".github", "scripts", "codebase-summary.mjs");
    writeFileSync(
      jobsPath,
      JSON.stringify({
        jobs: TARGETS.map((target, index) => job(index + 1, "🖥️ Controller", target.id)),
      }),
      "utf8",
    );
    writeFileSync(
      artifactsPath,
      JSON.stringify({
        artifacts: TARGETS.map((target, index) =>
          artifact(index + 301, "PCController-Host", target.id),
        ),
      }),
      "utf8",
    );

    const result = spawnSync(process.execPath, [script, "host", jobsPath, artifactsPath], {
      encoding: "utf8",
      env: {
        ...process.env,
        GITHUB_REPOSITORY: context.repository,
        GITHUB_SERVER_URL: context.serverUrl,
        GITHUB_RUN_ID: context.runId,
        GITHUB_SHA: context.sha,
        GITHUB_STEP_SUMMARY: summaryPath,
      },
    });

    assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
    assert.equal(result.stdout, "");
    const summary = readFileSync(summaryPath, "utf8");
    assert.match(summary, /^# ✅ 🖥️ Host builds/mu);
    assert.match(summary, /5\/5 targets passed · 5\/5 artifacts available/u);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("four-argument CLI verifies packages and prints full archive evidence", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-package-summary-"));
  try {
    const { jobsPath, artifactsPath } = writeRunDocuments(
      directory,
      "🧪 Virtual Board",
      "PCController-VirtualBoard",
    );
    const packages = TARGETS.map((target) =>
      writePackage(directory, "PCController-VirtualBoard", target.id),
    );
    const { result, summaryPath } = runCli(
      directory,
      "virtual-board",
      jobsPath,
      artifactsPath,
      directory,
    );

    assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
    const summary = readFileSync(summaryPath, "utf8");
    assert.match(summary, /5\/5 archives verified/u);
    assert.match(summary, /\| Archive \| Archive size \| Archive SHA-256 \|/u);
    assert.match(summary, new RegExp(packages[0].archiveName, "u"));
    assert.match(summary, new RegExp(packages[0].sha256, "u"));
    assert.match(summary, new RegExp(`${packages[0].bytes} B`, "u"));
    assert.equal(summary.match(new RegExp(packages[0].sha256, "gu"))?.length, 1);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("API-only CLI writes the summary before failing an incomplete matrix", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-api-failure-"));
  try {
    const { jobsPath, artifactsPath } = writeRunDocuments(
      directory,
      "🖥️ Host",
      "PCController-Host",
      {
        conclusions: { "Linux-ARM64": "failure" },
        missingArtifacts: ["macOS-Intel"],
      },
    );
    const { result, summaryPath } = runCli(
      directory,
      "host",
      jobsPath,
      artifactsPath,
    );

    assert.equal(result.status, 1);
    assert.match(result.stderr, /Incomplete host summary/u);
    const summary = readFileSync(summaryPath, "utf8");
    assert.match(summary, /^# ❌ 🖥️ Host builds/mu);
    assert.match(summary, /Linux-ARM64: Failed/u);
    assert.match(summary, /macOS-Intel: Missing/u);
    assert.doesNotMatch(summary, /\| Archive \| Archive size/u);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("package CLI writes checksum failures before exiting nonzero", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-package-failure-"));
  try {
    const { jobsPath, artifactsPath } = writeRunDocuments(
      directory,
      "🖥️ Host",
      "PCController-Host",
    );
    const packages = new Map();
    for (const target of TARGETS) {
      if (target.id === "Linux-ARM64") continue;
      const sidecar = target.id === "Windows-x64"
        ? "mismatch"
        : target.id === "macOS-Intel"
          ? "missing"
          : target.id === "macOS-Apple-Silicon"
            ? "directory"
          : "valid";
      packages.set(
        target.id,
        writePackage(directory, "PCController-Host", target.id, { sidecar }),
      );
    }
    const { result, summaryPath } = runCli(
      directory,
      "host",
      jobsPath,
      artifactsPath,
      directory,
    );

    assert.equal(result.status, 1);
    assert.match(result.stderr, /Incomplete host summary/u);
    const summary = readFileSync(summaryPath, "utf8");
    assert.match(summary, /^# ❌ 🖥️ Host builds/mu);
    assert.match(summary, /Linux-ARM64: Archive missing/u);
    assert.match(summary, /Windows-x64: SHA-256 mismatch/u);
    assert.match(summary, /macOS-Intel: SHA-256 sidecar missing/u);
    assert.match(summary, /macOS-Apple-Silicon: SHA-256 sidecar is not a file/u);
    assert.match(summary, new RegExp(packages.get("Windows-x64").sha256, "u"));
    assert.match(
      summary,
      /⚠️ Archive missing<br>`PCController-Host-<version>-Linux-ARM64\.tar\.gz`/u,
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
