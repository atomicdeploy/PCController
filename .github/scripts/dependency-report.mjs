#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { gunzipSync } from "node:zlib";
import { fileURLToPath, pathToFileURL } from "node:url";
import { PRODUCT_METADATA } from "../../Tools/Build/product-metadata.mjs";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const REPOSITORY_ROOT = path.resolve(SCRIPT_DIR, "../..");
const DEFAULT_CONFIG = path.join(REPOSITORY_ROOT, ".github", "dependencies.json");
const PRODUCT_NAME = PRODUCT_METADATA.productName;
const PRODUCT_AGENT = PRODUCT_NAME.replace(/[^0-9A-Za-z._-]+/gu, "-") || "Controller";

export const CURATED_DEPENDENCY_DOCUMENTS = Object.freeze([
  "README.md",
  "THIRD_PARTY_NOTICES.md",
  "docs/CI-CD-and-Releases.md",
  "docs/Getting-Started.md",
  "docs/Getting-Started-and-Operations.md",
  "docs/Hardware.md",
  "docs/Hardware-Initialization-and-Tuning.md",
  "docs/Front-Panel-and-Menus.md",
  "docs/Project-Checklist.md",
]);

const OFFICIAL_SOURCES = Object.freeze({
  arduinoCliApi: "https://api.github.com/repos/arduino/arduino-cli/releases/latest",
  arduinoCliReleases: "https://github.com/arduino/arduino-cli/releases",
  miniCoreIndex: "https://mcudude.github.io/MiniCore/package_MCUdude_MiniCore_index.json",
  miniCoreReleases: "https://github.com/MCUdude/MiniCore/releases",
  arduinoLibraryIndex: "https://downloads.arduino.cc/libraries/library_index.json.gz",
  rcSwitchReleases: "https://github.com/sui77/rc-switch/releases",
});

const VERSION_PATTERN = /^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/;
const SHA256_PATTERN = /^[a-f0-9]{64}$/;
const execFileAsync = promisify(execFile);

function invariant(condition, message, errors) {
  if (!condition) errors.push(message);
}

export function validateConfig(config) {
  const errors = [];
  invariant(config && typeof config === "object" && !Array.isArray(config), "configuration must be an object", errors);
  if (!config || typeof config !== "object" || Array.isArray(config)) return errors;

  invariant(config.schemaVersion === 1, "schemaVersion must be 1", errors);
  invariant(VERSION_PATTERN.test(config.arduinoCli?.version ?? ""), "arduinoCli.version must be semantic version text", errors);
  invariant(config.arduinoCli?.repository === "arduino/arduino-cli", "arduinoCli.repository must be the official arduino/arduino-cli repository", errors);
  invariant(
    config.arduinoCli?.linux64?.asset === `arduino-cli_${config.arduinoCli?.version}_Linux_64bit.tar.gz`,
    "arduinoCli.linux64.asset must match the pinned version",
    errors,
  );
  invariant(SHA256_PATTERN.test(config.arduinoCli?.linux64?.sha256 ?? ""), "arduinoCli.linux64.sha256 must be a lowercase SHA-256 digest", errors);

  invariant(VERSION_PATTERN.test(config.miniCore?.version ?? ""), "miniCore.version must be semantic version text", errors);
  invariant(config.miniCore?.package === "MiniCore:avr", "miniCore.package must be MiniCore:avr", errors);
  invariant(config.miniCore?.packageIndex === OFFICIAL_SOURCES.miniCoreIndex, "miniCore.packageIndex must be the official MiniCore package index", errors);

  invariant(VERSION_PATTERN.test(config.libraries?.["rc-switch"] ?? ""), "libraries.rc-switch must be semantic version text", errors);
  return errors;
}

export function loadConfig(configPath = DEFAULT_CONFIG) {
  const absolutePath = path.resolve(configPath);
  let config;
  try {
    config = JSON.parse(fs.readFileSync(absolutePath, "utf8"));
  } catch (error) {
    throw new Error(`Unable to read dependency configuration at ${absolutePath}: ${error.message}`);
  }
  const errors = validateConfig(config);
  if (errors.length > 0) throw new Error(`Invalid dependency configuration:\n- ${errors.join("\n- ")}`);
  return { config, path: absolutePath };
}

export function currentOutputs(config) {
  const version = config.arduinoCli.version;
  const asset = config.arduinoCli.linux64.asset;
  return {
    arduino_cli_version: version,
    arduino_cli_asset: asset,
    arduino_cli_download_url: `https://github.com/${config.arduinoCli.repository}/releases/download/v${version}/${asset}`,
    arduino_cli_linux_64_sha256: config.arduinoCli.linux64.sha256,
    minicore_version: config.miniCore.version,
    minicore_package: config.miniCore.package,
    minicore_index_url: config.miniCore.packageIndex,
    rc_switch_version: config.libraries["rc-switch"],
  };
}

function writeOutputs(values, outputPath) {
  const body = `${Object.entries(values).map(([key, value]) => `${key}=${String(value)}`).join("\n")}\n`;
  if (outputPath) fs.appendFileSync(outputPath, body, "utf8");
  else process.stdout.write(body);
}

function parseVersion(version) {
  const match = /^(\d+)\.(\d+)\.(\d+)(?:-([^+]+))?/.exec(version);
  if (!match) throw new Error(`Unsupported version: ${version}`);
  return { numbers: match.slice(1, 4).map(Number), prerelease: match[4] ?? null };
}

export function compareVersions(left, right) {
  const a = parseVersion(left);
  const b = parseVersion(right);
  for (let index = 0; index < 3; index += 1) {
    if (a.numbers[index] !== b.numbers[index]) return a.numbers[index] > b.numbers[index] ? 1 : -1;
  }
  if (a.prerelease === b.prerelease) return 0;
  if (a.prerelease === null) return 1;
  if (b.prerelease === null) return -1;
  return a.prerelease.localeCompare(b.prerelease, undefined, { numeric: true });
}

function latestStable(versions) {
  const candidates = [...new Set(versions.filter((version) => VERSION_PATTERN.test(version) && !version.includes("-")))];
  if (candidates.length === 0) throw new Error("Official index did not contain a stable semantic version");
  return candidates.sort(compareVersions).at(-1);
}

async function fetchBuffer(url, accept = "application/json") {
  const headers = {
    Accept: accept,
    "User-Agent": `${PRODUCT_AGENT}-dependency-radar/1.0`,
  };
  if (url.startsWith("https://api.github.com/") && process.env.GITHUB_TOKEN) {
    headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
    headers["X-GitHub-Api-Version"] = "2022-11-28";
  }

  let response;
  try {
    response = await fetch(url, { headers, redirect: "follow", signal: AbortSignal.timeout(90_000) });
  } catch (error) {
    if (url.startsWith("https://downloads.arduino.cc/")) {
      const executable = process.platform === "win32" ? "curl.exe" : "curl";
      const { stdout } = await execFileAsync(
        executable,
        ["--fail", "--silent", "--show-error", "--location", url],
        { encoding: null, maxBuffer: 80 * 1024 * 1024, timeout: 90_000 },
      );
      return Buffer.from(stdout);
    }
    throw error;
  }
  if (!response.ok) {
    // Cloudflare occasionally challenges Node's HTTP fingerprint even while the
    // same official Arduino CDN object remains available to curl and the CLI.
    // This narrowly scoped fallback keeps the source authoritative.
    if (response.status === 403 && url.startsWith("https://downloads.arduino.cc/")) {
      const executable = process.platform === "win32" ? "curl.exe" : "curl";
      const { stdout } = await execFileAsync(
        executable,
        ["--fail", "--silent", "--show-error", "--location", url],
        { encoding: null, maxBuffer: 80 * 1024 * 1024, timeout: 90_000 },
      );
      return Buffer.from(stdout);
    }
    throw new Error(`${response.status} ${response.statusText} from ${url}`);
  }
  return Buffer.from(await response.arrayBuffer());
}

async function fetchJson(url) {
  return JSON.parse((await fetchBuffer(url)).toString("utf8"));
}

function checksumForAsset(checksums, assetName) {
  for (const line of checksums.split(/\r?\n/)) {
    const simple = /^([a-fA-F0-9]{64})\s+\*?(.+)$/.exec(line.trim());
    if (simple && simple[2].trim() === assetName) return simple[1].toLowerCase();
    const openssl = /^SHA256 \((.+)\) = ([a-fA-F0-9]{64})$/.exec(line.trim());
    if (openssl && openssl[1] === assetName) return openssl[2].toLowerCase();
  }
  throw new Error(`Official checksum list does not contain ${assetName}`);
}

async function latestArduinoCli(config) {
  const release = await fetchJson(OFFICIAL_SOURCES.arduinoCliApi);
  const version = String(release.tag_name ?? "").replace(/^v/, "");
  if (!VERSION_PATTERN.test(version) || version.includes("-")) throw new Error(`Unexpected latest Arduino CLI tag: ${release.tag_name}`);
  const asset = `arduino-cli_${version}_Linux_64bit.tar.gz`;
  const checksumAsset = (release.assets ?? []).find((candidate) => /checksums?\.txt$/i.test(candidate.name));
  if (!checksumAsset?.browser_download_url) throw new Error(`Arduino CLI ${version} does not publish a checksum text asset`);
  const checksums = (await fetchBuffer(checksumAsset.browser_download_url, "text/plain")).toString("utf8");
  return {
    version,
    asset,
    sha256: checksumForAsset(checksums, asset),
    source: release.html_url ?? OFFICIAL_SOURCES.arduinoCliReleases,
    repository: config.arduinoCli.repository,
  };
}

async function latestMiniCore(config) {
  const index = await fetchJson(config.miniCore.packageIndex);
  const platforms = (index.packages ?? []).flatMap((entry) => entry.platforms ?? []);
  const matching = platforms.filter((platform) => platform.architecture === "avr" && /minicore/i.test(platform.name ?? ""));
  return {
    version: latestStable(matching.map((platform) => String(platform.version))),
    source: OFFICIAL_SOURCES.miniCoreReleases,
  };
}

async function latestRcSwitch() {
  const encoded = await fetchBuffer(OFFICIAL_SOURCES.arduinoLibraryIndex, "application/gzip, application/json");
  const decoded = encoded[0] === 0x1f && encoded[1] === 0x8b ? gunzipSync(encoded) : encoded;
  const index = JSON.parse(decoded.toString("utf8"));
  const matching = (index.libraries ?? []).filter((library) => String(library.name).toLowerCase() === "rc-switch");
  return {
    version: latestStable(matching.map((library) => String(library.version))),
    source: OFFICIAL_SOURCES.rcSwitchReleases,
  };
}

function statusFor(current, latest) {
  const comparison = compareVersions(latest, current);
  if (comparison > 0) return "update";
  if (comparison < 0) return "ahead";
  return "current";
}

function dependencyItem(key, label, current, latestResult) {
  if (latestResult.status === "rejected") {
    return {
      key,
      label,
      current,
      latest: null,
      status: "error",
      updateAvailable: false,
      source: null,
      error: latestResult.reason?.message ?? String(latestResult.reason),
    };
  }
  const latest = latestResult.value;
  const status = statusFor(current, latest.version);
  return {
    key,
    label,
    current,
    latest: latest.version,
    status,
    updateAvailable: status === "update",
    source: latest.source,
    candidate: latest,
  };
}

export async function auditDependencies(config) {
  const [arduinoCli, miniCore, rcSwitch] = await Promise.allSettled([
    latestArduinoCli(config),
    latestMiniCore(config),
    latestRcSwitch(),
  ]);
  const dependencies = [
    dependencyItem("arduinoCli", "Arduino CLI", config.arduinoCli.version, arduinoCli),
    dependencyItem("miniCore", "MiniCore AVR core", config.miniCore.version, miniCore),
    dependencyItem("rc-switch", "rc-switch library", config.libraries["rc-switch"], rcSwitch),
  ];
  return {
    schemaVersion: 1,
    generatedAt: new Date().toISOString(),
    complete: dependencies.every((item) => item.status !== "error"),
    updatesAvailable: dependencies.some((item) => item.updateAvailable),
    applied: false,
    dependencies,
    officialSources: OFFICIAL_SOURCES,
  };
}

const CANDIDATE_EVIDENCE_FIELDS = Object.freeze([
  "version",
  "source",
  "asset",
  "sha256",
  "repository",
]);

function safeCandidateEvidence(candidate) {
  if (!candidate || typeof candidate !== "object" || Array.isArray(candidate)) return null;
  const evidence = {};
  for (const field of CANDIDATE_EVIDENCE_FIELDS) {
    if (typeof candidate[field] === "string" && candidate[field].length > 0) {
      evidence[field] = candidate[field];
    }
  }
  return Object.keys(evidence).length > 0 ? evidence : null;
}

export function serializableReport(report) {
  return {
    ...report,
    dependencies: report.dependencies.map(({ candidate, ...item }) => {
      const evidence = safeCandidateEvidence(candidate);
      return evidence ? { ...item, candidate: evidence } : item;
    }),
  };
}

function escapeRegularExpression(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
}

function replaceAdjacentDependencyVersion(contents, namePattern, current, next) {
  if (!current || !next || current === next) return contents;
  const name = `(?:${namePattern}|\\[${namePattern}\\]\\([^)\\r\\n]+\\)|\`${namePattern}\`)`;
  const separator = "(?:\\s+(?:version\\s*:?\\s+)?|\\s*(?:\\||@|=|:|_)\\s*)";
  const expression = new RegExp(
    `(${name})(${separator})(\`?)(v?)${escapeRegularExpression(current)}\\3(?![0-9A-Za-z.+-])`,
    "giu",
  );
  return contents.replace(
    expression,
    (_match, dependency, between, codeDelimiter, prefix) =>
      `${dependency}${between}${codeDelimiter}${prefix}${next}${codeDelimiter}`,
  );
}

export function synchronizeDependencyMentions(contents, currentConfig, nextConfig) {
  let synchronized = String(contents);
  synchronized = replaceAdjacentDependencyVersion(
    synchronized,
    String.raw`(?:Arduino[ -]CLI|arduino-cli)`,
    currentConfig.arduinoCli?.version,
    nextConfig.arduinoCli?.version,
  );
  synchronized = replaceAdjacentDependencyVersion(
    synchronized,
    String.raw`MiniCore(?:(?:/MiniCore)?:avr)?`,
    currentConfig.miniCore?.version,
    nextConfig.miniCore?.version,
  );
  synchronized = replaceAdjacentDependencyVersion(
    synchronized,
    String.raw`rc-switch`,
    currentConfig.libraries?.["rc-switch"],
    nextConfig.libraries?.["rc-switch"],
  );
  return synchronized;
}

export function synchronizeDependencyDocumentation(
  currentConfig,
  nextConfig,
  repositoryRoot = REPOSITORY_ROOT,
) {
  const changed = [];
  for (const relativePath of CURATED_DEPENDENCY_DOCUMENTS) {
    const documentPath = path.join(repositoryRoot, relativePath);
    if (!fs.existsSync(documentPath) || !fs.statSync(documentPath).isFile()) continue;
    const before = fs.readFileSync(documentPath, "utf8");
    const after = synchronizeDependencyMentions(before, currentConfig, nextConfig);
    if (after === before) continue;
    fs.writeFileSync(documentPath, after, "utf8");
    changed.push(relativePath);
  }
  return changed;
}

function documentationRootForConfig(configPath) {
  const absolutePath = path.resolve(configPath);
  const configDirectory = path.dirname(absolutePath);
  if (
    path.basename(absolutePath) !== "dependencies.json" ||
    path.basename(configDirectory) !== ".github"
  ) {
    return null;
  }
  return path.dirname(configDirectory);
}

export function applyReport(config, report) {
  if (!report.complete) throw new Error("Refusing to apply dependency updates because at least one official source failed");
  const updated = structuredClone(config);
  for (const item of report.dependencies.filter((dependency) => dependency.updateAvailable)) {
    if (item.key === "arduinoCli") {
      updated.arduinoCli.version = item.candidate.version;
      updated.arduinoCli.linux64.asset = item.candidate.asset;
      updated.arduinoCli.linux64.sha256 = item.candidate.sha256;
    } else if (item.key === "miniCore") {
      updated.miniCore.version = item.candidate.version;
    } else if (item.key === "rc-switch") {
      updated.libraries["rc-switch"] = item.candidate.version;
    }
  }
  const errors = validateConfig(updated);
  if (errors.length > 0) throw new Error(`Generated dependency configuration is invalid:\n- ${errors.join("\n- ")}`);
  report.applied = report.updatesAvailable;
  return updated;
}

function md(value) {
  return String(value).replaceAll("|", "\\|").replaceAll("\n", " ");
}

function runLink() {
  const repository = process.env.GITHUB_REPOSITORY;
  const runId = process.env.GITHUB_RUN_ID;
  if (!repository || !runId) return null;
  return `https://github.com/${repository}/actions/runs/${runId}`;
}

function statusPresentation(status) {
  return {
    current: "✅ Current",
    update: "🆕 Update found",
    ahead: "🧭 Pin is ahead",
    error: "⚠️ Source unavailable",
  }[status];
}

export function renderSummary(report, config, { applied = report.applied } = {}) {
  const healthy = report.dependencies.filter((item) => item.status === "current" || item.status === "ahead").length;
  const slots = 12;
  const filled = Math.round((healthy / report.dependencies.length) * slots);
  const meter = `${"█".repeat(filled)}${"░".repeat(slots - filled)}`;
  const run = runLink();
  const headline = !report.complete
    ? "⚠️ Official-source verification is incomplete"
    : report.updatesAvailable
      ? applied
        ? "🚀 Verified updates were applied to the canonical pin file"
        : "🆕 Verified updates are ready for an automated pull request"
      : "✅ Every AVR build dependency is current";

  const lines = [
    `# 🔭 ${PRODUCT_NAME} Dependency Radar`,
    "",
    `> ${headline}`,
    "",
    `**Supply-chain health:** \`${meter}\` ${healthy}/${report.dependencies.length} verified current`,
    "",
    `| Component | ${applied ? "Previous pin" : "Pinned"} | Official latest | Signal | Source |`,
    "|:--|:--:|:--:|:--|:--|",
    ...report.dependencies.map((item) => {
      const latest = item.latest ?? "unavailable";
      const source = item.source ? `[official](${item.source})` : "see diagnostics";
      return `| ${md(item.label)} | \`${md(item.current)}\` | \`${md(latest)}\` | ${statusPresentation(item.status)} | ${source} |`;
    }),
    "",
  ];

  const errors = report.dependencies.filter((item) => item.error);
  if (errors.length > 0) {
    lines.push("<details><summary><strong>⚠️ Source diagnostics</strong></summary>", "");
    for (const item of errors) lines.push(`- **${md(item.label)}:** ${md(item.error)}`);
    lines.push("", "</details>", "");
  }

  if (applied && report.documentationUpdated?.length > 0) {
    lines.push(
      `**Synchronized current-version documentation:** ${report.documentationUpdated.map((file) => `\`${md(file)}\``).join(", ")}`,
      "",
    );
  }

  lines.push(
    "<details><summary><strong>🧰 Reproducible firmware inputs</strong></summary>",
    "",
    "| Input | Canonical value |",
    "|:--|:--|",
    `| Arduino CLI Linux x64 asset | \`${md(config.arduinoCli.linux64.asset)}\` |`,
    `| Arduino CLI SHA-256 | \`${md(config.arduinoCli.linux64.sha256)}\` |`,
    `| MiniCore package | \`${md(config.miniCore.package)}@${md(config.miniCore.version)}\` |`,
    `| MiniCore index | ${config.miniCore.packageIndex} |`,
    `| Arduino library | \`rc-switch@${md(config.libraries["rc-switch"])}\` |`,
    "",
    "The firmware workflow consumes these values through `dependency-report.mjs export`; there is one pin source and no silent `latest` install.",
    "",
    "</details>",
    "",
    "<details><summary><strong>🔐 Verification policy</strong></summary>",
    "",
    `- Arduino CLI releases and checksums: [official GitHub release](${OFFICIAL_SOURCES.arduinoCliReleases})`,
    `- MiniCore versions: [official board-manager index](${OFFICIAL_SOURCES.miniCoreIndex})`,
    `- Arduino libraries: [official Arduino library index](${OFFICIAL_SOURCES.arduinoLibraryIndex})`,
    "- Update application is all-or-nothing: any unreachable or malformed official source blocks mutation.",
    "- A verified scheduled update or manual **apply** run may open a review-only pull request; neither path merges or releases it.",
    "",
    "</details>",
    "",
    "---",
    `Generated ${report.generatedAt}${run ? ` · [Open workflow run](${run})` : ""} · Canonical pins: \`.github/dependencies.json\``,
    "",
  );
  return lines.join("\n");
}

function writeFileEnsuringParent(filePath, contents) {
  const absolutePath = path.resolve(filePath);
  fs.mkdirSync(path.dirname(absolutePath), { recursive: true });
  fs.writeFileSync(absolutePath, contents, "utf8");
  return absolutePath;
}

function parseArguments(argv) {
  const command = argv[0] ?? "help";
  const options = { apply: command === "apply" };
  for (let index = 1; index < argv.length; index += 1) {
    const token = argv[index];
    if (token === "--apply") options.apply = true;
    else if (["--config", "--output", "--report", "--markdown", "--summary"].includes(token)) {
      if (!argv[index + 1]) throw new Error(`${token} requires a path`);
      options[token.slice(2)] = argv[index + 1];
      index += 1;
    } else throw new Error(`Unknown argument: ${token}`);
  }
  return { command, options };
}

function usage() {
  return [
    "Usage:",
    "  dependency-report.mjs validate [--config FILE]",
    "  dependency-report.mjs export [--config FILE] [--output GITHUB_OUTPUT]",
    "  dependency-report.mjs check [--config FILE] [--report JSON] [--markdown MD] [--summary FILE]",
    "  dependency-report.mjs apply [--config FILE] [--report JSON] [--markdown MD] [--summary FILE]",
  ].join("\n");
}

async function main(argv) {
  const { command, options } = parseArguments(argv);
  if (command === "help" || command === "--help" || command === "-h") {
    console.log(usage());
    return;
  }

  const loaded = loadConfig(options.config ?? DEFAULT_CONFIG);
  if (command === "validate") {
    console.log(`Dependency configuration is valid: ${loaded.path}`);
    return;
  }
  if (command === "export") {
    writeOutputs(currentOutputs(loaded.config), options.output ?? process.env.GITHUB_OUTPUT);
    return;
  }
  if (command !== "check" && command !== "apply") throw new Error(`Unknown command: ${command}\n\n${usage()}`);

  const report = await auditDependencies(loaded.config);
  let effectiveConfig = loaded.config;
  if (options.apply) {
    effectiveConfig = applyReport(loaded.config, report);
    if (report.applied) {
      const documentationRoot = documentationRootForConfig(loaded.path);
      report.documentationUpdated = documentationRoot
        ? synchronizeDependencyDocumentation(loaded.config, effectiveConfig, documentationRoot)
        : [];
      fs.writeFileSync(loaded.path, `${JSON.stringify(effectiveConfig, null, 2)}\n`, "utf8");
    }
  }

  const safeReport = serializableReport(report);
  const summary = renderSummary(report, effectiveConfig);
  const reportPath = options.report ? writeFileEnsuringParent(options.report, `${JSON.stringify(safeReport, null, 2)}\n`) : null;
  if (options.markdown) writeFileEnsuringParent(options.markdown, `${summary}\n`);
  const summaryPath = options.summary ?? process.env.GITHUB_STEP_SUMMARY;
  if (summaryPath) fs.appendFileSync(summaryPath, `${summary}\n`, "utf8");

  const changed = report.dependencies.filter((item) => item.updateAvailable).map((item) => item.key);
  writeOutputs(
    {
      audit_complete: report.complete,
      updates_available: report.updatesAvailable,
      changed_dependencies: changed.join(","),
      config_changed: report.applied,
      documentation_updated: (report.documentationUpdated ?? []).join(","),
      report_path: reportPath ?? "",
    },
    process.env.GITHUB_OUTPUT,
  );

  console.log(`${report.complete ? "Verified" : "Incomplete"} dependency audit: ${changed.length} update(s) found${report.applied ? " and applied" : ""}.`);
  if (!report.complete) process.exitCode = 1;
}

const invokedDirectly = process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href;
if (invokedDirectly) {
  main(process.argv.slice(2)).catch((error) => {
    console.error(`dependency-report: ${error.message}`);
    process.exitCode = 1;
  });
}
