#!/usr/bin/env node

import { createHash } from "node:crypto";
import {
  appendFileSync,
  closeSync,
  constants,
  fstatSync,
  openSync,
  readFileSync,
  readdirSync,
} from "node:fs";
import { basename, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const TARGETS = Object.freeze([
  Object.freeze({ id: "Linux-x64", runner: "ubuntu-24.04 · x64" }),
  Object.freeze({ id: "Linux-ARM64", runner: "ubuntu-24.04-arm · ARM64" }),
  Object.freeze({ id: "Windows-x64", runner: "windows-2025 · x64" }),
  Object.freeze({ id: "macOS-Intel", runner: "macos-15-intel · x64" }),
  Object.freeze({ id: "macOS-Apple-Silicon", runner: "macos-15 · ARM64" }),
]);

const CODEBASES = Object.freeze({
  host: Object.freeze({
    title: "Host",
    icon: "🖥️",
    artifactPrefix: "PCController-Host",
    jobPatterns: [
      /(?:^|[^\p{L}\p{N}])host(?:$|[^\p{L}\p{N}])/iu,
      /(?:^|[^\p{L}\p{N}])controller(?:$|[^\p{L}\p{N}])/iu,
    ],
    sharedValidation: [
      "`go test ./...` and `go vet ./...`",
      "Native executable, C ABI shared-library build, and ABI smoke test",
      "Version/source identity manifest and SHA-256 package sidecar",
    ],
  }),
  "virtual-board": Object.freeze({
    title: "Virtual Board",
    icon: "🧪",
    artifactPrefix: "PCController-VirtualBoard",
    jobPatterns: [
      /(?:^|[^\p{L}\p{N}])virtual(?:[\s-]+)?board(?:$|[^\p{L}\p{N}])/iu,
    ],
    sharedValidation: [
      "Native Release-mode CMake build",
      "CTest protocol, hardware-model, UART, and smoke suite",
      "SHA-256 package sidecar",
    ],
  }),
});

const HARD_FAILURES = new Set([
  "action_required",
  "failure",
  "stale",
  "startup_failure",
  "timed_out",
]);

const escapeTable = (value) => String(value ?? "").replaceAll("|", "\\|");
const code = (value) => `\`${String(value ?? "").replaceAll("`", "\\`")}\``;
const normalize = (value) => String(value ?? "").trim().toLowerCase();
const titleCase = (value) =>
  String(value ?? "unknown")
    .replaceAll("_", " ")
    .replace(/\b\w/gu, (character) => character.toUpperCase());
const markdownLink = (label, url) =>
  url ? `[${escapeTable(label)}](${url})` : escapeTable(label);
const escapeRegExp = (value) =>
  String(value).replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");

const formatBytes = (value) => {
  const size = Number(value || 0);
  if (!Number.isFinite(size) || size < 0) return "unknown size";
  if (size < 1024) return `${Math.round(size).toLocaleString("en-US")} B`;
  if (size < 1024 ** 2) return `${(size / 1024).toFixed(1)} KiB`;
  return `${(size / 1024 ** 2).toFixed(2)} MiB`;
};

const collectFiles = (directory) => {
  const files = [];
  const visit = (path) => {
    for (const entry of readdirSync(path, { withFileTypes: true })) {
      const child = join(path, entry.name);
      if (entry.isDirectory()) visit(child);
      else if (entry.isFile()) files.push(child);
    }
  };
  visit(directory);
  return files;
};

const packageFailure = (label, severity = "warning", details = {}) => ({
  valid: false,
  label,
  severity,
  ...details,
});

const inspectArchive = (archivePath) => {
  const archiveName = basename(archivePath);
  let archiveBytes;
  let sha256;
  try {
    const archive = readFileSync(archivePath);
    archiveBytes = archive.byteLength;
    sha256 = createHash("sha256").update(archive).digest("hex");
  } catch (error) {
    return packageFailure("Archive unreadable", "failure", {
      archiveName,
      detail: error instanceof Error ? error.message : String(error),
    });
  }

  const checksumPath = `${archivePath}.sha256`;
  let checksumText;
  let checksumDescriptor;
  try {
    checksumDescriptor = openSync(
      checksumPath,
      constants.O_RDONLY | (constants.O_NONBLOCK ?? 0),
    );
  } catch (error) {
    if (error && typeof error === "object" && error.code === "ENOENT") {
      return packageFailure("SHA-256 sidecar missing", "warning", {
        archiveName,
        archiveBytes,
        sha256,
      });
    }
    return packageFailure("SHA-256 sidecar unreadable", "failure", {
      archiveName,
      archiveBytes,
      sha256,
      detail: error instanceof Error ? error.message : String(error),
    });
  }

  try {
    if (!fstatSync(checksumDescriptor).isFile()) {
      return packageFailure("SHA-256 sidecar is not a file", "failure", {
        archiveName,
        archiveBytes,
        sha256,
      });
    }
    checksumText = readFileSync(checksumDescriptor, "utf8").replace(/^\uFEFF/u, "");
  } catch (error) {
    return packageFailure("SHA-256 sidecar unreadable", "failure", {
      archiveName,
      archiveBytes,
      sha256,
      detail: error instanceof Error ? error.message : String(error),
    });
  } finally {
    closeSync(checksumDescriptor);
  }
  const lines = checksumText.split(/\r?\n/u).filter((line) => line.trim());
  const checksum = lines.length === 1
    ? /^([a-f0-9]{64})[ \t]+\*?(.+?)\s*$/iu.exec(lines[0])
    : null;
  if (!checksum) {
    return packageFailure("SHA-256 sidecar invalid", "failure", {
      archiveName,
      archiveBytes,
      sha256,
    });
  }
  if (checksum[2] !== archiveName) {
    return packageFailure("SHA-256 sidecar filename mismatch", "failure", {
      archiveName,
      archiveBytes,
      sha256,
    });
  }
  if (checksum[1].toLowerCase() !== sha256) {
    return packageFailure("SHA-256 mismatch", "failure", {
      archiveName,
      archiveBytes,
      sha256,
    });
  }

  return {
    valid: true,
    label: "Verified",
    severity: "success",
    archiveName,
    archiveBytes,
    sha256,
  };
};

export function inspectPackageDirectory(codebase, packagesDirectory) {
  const definition = CODEBASES[codebase];
  if (!definition) {
    throw new Error(`Unknown codebase ${JSON.stringify(codebase)}; expected host or virtual-board`);
  }

  const directory = resolve(packagesDirectory);
  let files;
  try {
    files = collectFiles(directory);
  } catch {
    files = null;
  }

  return Object.fromEntries(
    TARGETS.map((target) => {
      const expectedName = `${definition.artifactPrefix}-<version>-${target.id}.tar.gz`;
      if (!files) {
        return [
          target.id,
          packageFailure("Packages directory unavailable", "warning", { expectedName }),
        ];
      }
      const pattern = new RegExp(
        `^${escapeRegExp(definition.artifactPrefix)}-(.+)-${escapeRegExp(target.id)}\\.tar\\.gz$`,
        "iu",
      );
      const matches = files
        .filter((path) => pattern.test(basename(path)))
        .toSorted((left, right) => left.localeCompare(right, "en"));
      if (matches.length === 0) {
        return [target.id, packageFailure("Archive missing", "warning", { expectedName })];
      }
      if (matches.length > 1) {
        return [
          target.id,
          packageFailure("Multiple archives found", "failure", { expectedName }),
        ];
      }
      return [target.id, inspectArchive(matches[0])];
    }),
  );
}

const flattenCollection = (document, key) => {
  if (Array.isArray(document)) {
    return document.flatMap((page) => flattenCollection(page, key));
  }
  if (Array.isArray(document?.[key])) return document[key];
  return [];
};

export function parseRunData(jobsDocument, artifactsDocument) {
  return {
    jobs: flattenCollection(jobsDocument, "jobs"),
    artifacts: flattenCollection(artifactsDocument, "artifacts"),
  };
}

const componentMatches = (jobName, definition) => {
  const name = String(jobName ?? "");
  return definition.jobPatterns.some((pattern) => pattern.test(name));
};

const targetMatches = (value, target) => normalize(value).includes(normalize(target.id));

const latest = (entries) =>
  entries.toSorted((left, right) => {
    const attemptDifference = Number(right.run_attempt || 0) - Number(left.run_attempt || 0);
    return attemptDifference || Number(right.id || 0) - Number(left.id || 0);
  })[0];

const findJob = (jobs, definition, target) =>
  latest(
    jobs.filter(
      (job) => componentMatches(job.name, definition) && targetMatches(job.name, target),
    ),
  );

const findArtifact = (artifacts, definition, target) => {
  const expected = normalize(`${definition.artifactPrefix}-${target.id}`);
  return latest(
    artifacts.filter((artifact) => normalize(artifact.name) === expected),
  );
};

const jobState = (job) => {
  if (!job) return { label: "❓ Missing job", severity: "warning", passed: false };

  const conclusion = normalize(job.conclusion);
  const status = normalize(job.status);
  const knownConclusions = {
    success: ["✅ Passed", "success"],
    failure: ["❌ Failed", "failure"],
    cancelled: ["⛔ Cancelled", "warning"],
    skipped: ["⏭️ Skipped", "warning"],
    timed_out: ["⏱️ Timed out", "failure"],
    startup_failure: ["❌ Start failed", "failure"],
    action_required: ["⚠️ Action required", "failure"],
    stale: ["⚠️ Stale", "failure"],
    neutral: ["⚪ Neutral", "warning"],
  };

  if (knownConclusions[conclusion]) {
    const [label, severity] = knownConclusions[conclusion];
    return { label, severity, passed: conclusion === "success" };
  }
  if (["in_progress", "pending", "queued", "requested", "waiting"].includes(status)) {
    return {
      label: `⏳ ${titleCase(status)}`,
      severity: "active",
      passed: false,
    };
  }
  if (conclusion) {
    return {
      label: `❔ ${titleCase(conclusion)}`,
      severity: HARD_FAILURES.has(conclusion) ? "failure" : "warning",
      passed: false,
    };
  }
  return { label: "❔ Unknown", severity: "warning", passed: false };
};

const artifactState = (artifact) => {
  if (!artifact) return { label: "⚠️ Missing", available: false };
  if (artifact.expired) return { label: "⚠️ Expired", available: false };
  return { label: "available", available: true };
};

const artifactUrl = (artifact, context) => {
  if (!artifact) return "";
  const runId = artifact.workflow_run?.id || context.runId;
  if (context.repository && runId && artifact.id) {
    return `${context.serverUrl}/${context.repository}/actions/runs/${runId}/artifacts/${artifact.id}`;
  }
  return artifact.archive_download_url || artifact.url || "";
};

const renderArtifact = (artifact, state, context, expectedName) => {
  if (!state.available) {
    return `${state.label}<br>${code(artifact?.name || expectedName)}`;
  }
  const name = markdownLink(artifact.name, artifactUrl(artifact, context));
  const digest = String(artifact.digest || "").replace(/^sha256:/iu, "");
  const integrity = digest ? ` · ${code(`sha256:${digest.slice(0, 12)}…`)}` : "";
  return `${name}<br>${formatBytes(artifact.size_in_bytes)}${integrity}`;
};

const renderPackageCells = (evidence) => {
  const marker = evidence.valid
    ? "✅"
    : evidence.severity === "failure"
      ? "❌"
      : "⚠️";
  const archive = evidence.archiveName
    ? code(evidence.archiveName)
    : `${marker} ${escapeTable(evidence.label)}<br>${code(evidence.expectedName || "archive unavailable")}`;
  const size = Number.isFinite(evidence.archiveBytes)
    ? formatBytes(evidence.archiveBytes)
    : "—";
  const checksum = evidence.sha256
    ? `${marker} ${code(evidence.sha256)}${evidence.valid ? "" : `<br>${escapeTable(evidence.label)}`}`
    : "—";
  return { archive, size, checksum };
};

const buildContext = (options = {}) => {
  const repository = options.repository ?? process.env.GITHUB_REPOSITORY ?? "atomicdeploy/PCController";
  const serverUrl = options.serverUrl ?? process.env.GITHUB_SERVER_URL ?? "https://github.com";
  const runId = options.runId ?? process.env.GITHUB_RUN_ID ?? "";
  const sha = options.sha ?? process.env.GITHUB_SHA ?? "";
  const repositoryUrl = `${serverUrl}/${repository}`;
  return {
    repository,
    serverUrl,
    runId,
    sha,
    repositoryUrl,
    runUrl: runId ? `${repositoryUrl}/actions/runs/${runId}` : repositoryUrl,
  };
};

export function buildCodebaseSummary(codebase, jobsDocument, artifactsDocument, options = {}) {
  const definition = CODEBASES[codebase];
  if (!definition) {
    throw new Error(`Unknown codebase ${JSON.stringify(codebase)}; expected host or virtual-board`);
  }

  const { jobs, artifacts } = parseRunData(jobsDocument, artifactsDocument);
  const context = buildContext(options);
  const packageMode = options.packageEvidence !== undefined;
  const records = TARGETS.map((target) => {
    const job = findJob(jobs, definition, target);
    const artifact = findArtifact(artifacts, definition, target);
    const currentArtifactState = artifactState(artifact);
    return {
      target,
      job,
      artifact,
      jobState: jobState(job),
      artifactState: currentArtifactState,
      packageState: packageMode
        ? options.packageEvidence?.[target.id] || packageFailure("Archive evidence missing")
        : null,
    };
  });

  const passed = records.filter((record) => record.jobState.passed).length;
  const available = records.filter((record) => record.artifactState.available).length;
  const verified = packageMode
    ? records.filter((record) => record.packageState.valid).length
    : 0;
  const packageRequiredAndInvalid = (record) =>
    packageMode && record.artifactState.available && !record.packageState.valid;
  const hasFailure = records.some(
    (record) =>
      record.jobState.severity === "failure" ||
      (packageRequiredAndInvalid(record) && record.packageState.severity === "failure"),
  );
  const hasActive = records.some((record) => record.jobState.severity === "active");
  const hasWarning = records.some(
    (record) =>
      record.jobState.severity === "warning" ||
      !record.artifactState.available ||
      packageRequiredAndInvalid(record),
  );
  const headingIcon = hasFailure ? "❌" : hasActive ? "⏳" : hasWarning ? "⚠️" : "✅";
  const attention = records
    .filter(
      (record) =>
        !record.jobState.passed ||
        !record.artifactState.available ||
        packageRequiredAndInvalid(record),
    )
    .map((record) => {
      const problems = [];
      if (!record.jobState.passed) problems.push(record.jobState.label.replace(/^[^\p{L}\p{N}]+/u, ""));
      if (!record.artifactState.available) problems.push(record.artifactState.label.replace(/^[^\p{L}\p{N}]+/u, ""));
      if (packageRequiredAndInvalid(record)) problems.push(record.packageState.label);
      return `${record.target.id}: ${problems.join("; ")}`;
    });

  const rows = records
    .map((record) => {
      const jobResult = markdownLink(record.jobState.label, record.job?.html_url || "");
      const packageResult = renderArtifact(
        record.artifact,
        record.artifactState,
        context,
        `${definition.artifactPrefix}-${record.target.id}`,
      );
      if (!packageMode) {
        return `| **${record.target.id}** | ${record.target.runner} | ${jobResult} | ${packageResult} |`;
      }
      const packageCells = renderPackageCells(record.packageState);
      return `| **${record.target.id}** | ${record.target.runner} | ${jobResult} | ${packageResult} | ${packageCells.archive} | ${packageCells.size} | ${packageCells.checksum} |`;
    })
    .join("\n");
  const validations = definition.sharedValidation.map((item) => `- ${item}`).join("\n");
  const warning = attention.length
    ? `\n> [!WARNING]\n> ${escapeTable(attention.join(" · "))}\n`
    : "";
  const source = context.sha
    ? ` · [source ${code(context.sha.slice(0, 12))}](${context.repositoryUrl}/commit/${context.sha})`
    : "";
  const packageCount = packageMode
    ? ` · ${verified}/${TARGETS.length} archives verified`
    : "";
  const packageHeaders = packageMode
    ? " | Archive | Archive size | Archive SHA-256"
    : "";
  const packageSeparators = packageMode ? "|---|---:|---" : "";
  const incompleteTargets = records
    .filter(
      (record) =>
        !record.jobState.passed ||
        !record.artifactState.available ||
        packageRequiredAndInvalid(record),
    )
    .map((record) => record.target.id);

  const markdown = `# ${headingIcon} ${definition.icon} ${definition.title} builds

${passed}/${TARGETS.length} targets passed · ${available}/${TARGETS.length} artifacts available${packageCount} · [workflow run](${context.runUrl})
${warning}
## 🧪 Validation on successful targets

${validations}

## 🌐 Platform differences

| Target | Native runner | Build | Actions artifact${packageHeaders} |
|---|---|---|---${packageSeparators}|
${rows}

---

[Run](${context.runUrl})${source}
`;

  return {
    markdown,
    complete: incompleteTargets.length === 0,
    incompleteTargets,
    records,
  };
}

export function renderCodebaseSummary(codebase, jobsDocument, artifactsDocument, options = {}) {
  return buildCodebaseSummary(codebase, jobsDocument, artifactsDocument, options).markdown;
}

export function writeSummary(markdown, summaryPath = process.env.GITHUB_STEP_SUMMARY) {
  const text = `${markdown.trim()}\n`;
  if (summaryPath) appendFileSync(summaryPath, text, "utf8");
  else process.stdout.write(text);
}

export function main(arguments_ = process.argv.slice(2)) {
  const [codebase, jobsPath, artifactsPath, packagesDirectory] = arguments_;
  if (!codebase || !jobsPath || !artifactsPath) {
    throw new Error(
      "Usage: codebase-summary.mjs <host|virtual-board> <jobs.json> <artifacts.json> [packages-directory]",
    );
  }
  const jobsDocument = JSON.parse(readFileSync(resolve(jobsPath), "utf8"));
  const artifactsDocument = JSON.parse(readFileSync(resolve(artifactsPath), "utf8"));
  const packageEvidence = packagesDirectory
    ? inspectPackageDirectory(codebase, packagesDirectory)
    : undefined;
  const result = buildCodebaseSummary(codebase, jobsDocument, artifactsDocument, {
    ...(packageEvidence ? { packageEvidence } : {}),
  });
  writeSummary(result.markdown);
  if (!result.complete) {
    throw new Error(
      `Incomplete ${codebase} summary: ${result.incompleteTargets.join(", ")}`,
    );
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main();
  } catch (error) {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  }
}
