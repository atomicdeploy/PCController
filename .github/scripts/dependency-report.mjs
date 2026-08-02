#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { gunzipSync } from "node:zlib";
import { fileURLToPath, pathToFileURL } from "node:url";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const REPOSITORY_ROOT = path.resolve(SCRIPT_DIR, "../..");
const DEFAULT_CONFIG = path.join(REPOSITORY_ROOT, ".github", "dependencies.json");

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
});

const VERSION_PATTERN = /^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/;
const SHA256_PATTERN = /^[a-f0-9]{64}$/;
const GITHUB_REPOSITORY_PATTERN = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;
const FIRMWARE_SOURCE_DIRECTORIES = Object.freeze(["LocalLib", "Project"]);
const FIRMWARE_SOURCE_FILES = Object.freeze(["PCController.ino", "PCControllerLocalLib.cpp", "PCControllerProject.cpp"]);
const FRAMEWORK_OR_SYSTEM_INCLUDES = new Set([
  "Arduino.h",
  "EEPROM.h",
  "SPI.h",
  "SoftwareSerial.h",
  "Wire.h",
  "limits.h",
  "math.h",
  "stddef.h",
  "stdint.h",
  "string.h",
]);
const execFileAsync = promisify(execFile);

function invariant(condition, message, errors) {
  if (!condition) errors.push(message);
}

export function validateConfig(config) {
  const errors = [];
  invariant(config && typeof config === "object" && !Array.isArray(config), "configuration must be an object", errors);
  if (!config || typeof config !== "object" || Array.isArray(config)) return errors;

  invariant(config.schemaVersion === 2, "schemaVersion must be 2", errors);
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
  invariant(config.miniCore?.repository === "MCUdude/MiniCore", "miniCore.repository must be the official MCUdude/MiniCore repository", errors);
  invariant(
    config.miniCore?.archive?.url === `https://MCUdude.github.io/MiniCore/MiniCore-${config.miniCore?.version}.tar.bz2`,
    "miniCore.archive.url must match the pinned version and official MiniCore host",
    errors,
  );
  invariant(SHA256_PATTERN.test(config.miniCore?.archive?.sha256 ?? ""), "miniCore.archive.sha256 must be a lowercase SHA-256 digest", errors);

  invariant(config.libraries && typeof config.libraries === "object" && !Array.isArray(config.libraries), "libraries must be an object", errors);
  const libraries = config.libraries && typeof config.libraries === "object" && !Array.isArray(config.libraries)
    ? Object.entries(config.libraries)
    : [];
  invariant(libraries.length > 0, "libraries must declare every third-party firmware library", errors);
  const claimedIncludes = new Set();
  for (const [name, library] of libraries) {
    invariant(/^[A-Za-z0-9][A-Za-z0-9 ._+-]{0,79}$/u.test(name), `libraries contains an invalid Arduino Library Manager name: ${name}`, errors);
    invariant(VERSION_PATTERN.test(library?.version ?? ""), `libraries.${name}.version must be semantic version text`, errors);
    invariant(GITHUB_REPOSITORY_PATTERN.test(library?.repository ?? ""), `libraries.${name}.repository must be an owner/repository pair`, errors);
    invariant(Array.isArray(library?.includes) && library.includes.length > 0, `libraries.${name}.includes must list its firmware headers`, errors);
    for (const include of Array.isArray(library?.includes) ? library.includes : []) {
      invariant(typeof include === "string" && /^[A-Za-z0-9_.-]+\.h(?:pp)?$/u.test(include), `libraries.${name}.includes contains an invalid header`, errors);
      invariant(!claimedIncludes.has(include), `firmware header ${include} is claimed by more than one library`, errors);
      claimedIncludes.add(include);
    }
    invariant(
      typeof library?.archive?.url === "string" && library.archive.url.startsWith("https://downloads.arduino.cc/libraries/"),
      `libraries.${name}.archive.url must use the official Arduino library CDN`,
      errors,
    );
    invariant(SHA256_PATTERN.test(library?.archive?.sha256 ?? ""), `libraries.${name}.archive.sha256 must be a lowercase SHA-256 digest`, errors);
  }
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

function collectSourceFiles(directory) {
  if (!fs.existsSync(directory)) return [];
  const files = [];
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const absolutePath = path.join(directory, entry.name);
    if (entry.isDirectory()) files.push(...collectSourceFiles(absolutePath));
    else if (entry.isFile() && /\.(?:c|cc|cpp|h|hpp|ino)$/iu.test(entry.name)) files.push(absolutePath);
  }
  return files;
}

export function validateFirmwareDependencyInventory(config, repositoryRoot = REPOSITORY_ROOT) {
  const sourceFiles = [
    ...FIRMWARE_SOURCE_FILES.map((file) => path.join(repositoryRoot, file)).filter((file) => fs.existsSync(file)),
    ...FIRMWARE_SOURCE_DIRECTORIES.flatMap((directory) => collectSourceFiles(path.join(repositoryRoot, directory))),
  ];
  if (sourceFiles.length === 0) return ["firmware dependency inventory found no source files"];

  const externalIncludes = new Set();
  for (const sourceFile of sourceFiles) {
    const source = fs.readFileSync(sourceFile, "utf8");
    for (const match of source.matchAll(/^\s*#\s*include\s*<([^>]+)>/gmu)) {
      const include = match[1].trim();
      if (include.startsWith("avr/") || include.startsWith("util/") || FRAMEWORK_OR_SYSTEM_INCLUDES.has(include)) continue;
      externalIncludes.add(include);
    }
  }

  const declaredIncludes = new Map();
  for (const [libraryName, library] of Object.entries(config.libraries ?? {})) {
    for (const include of library.includes ?? []) declaredIncludes.set(include, libraryName);
  }

  const errors = [];
  for (const include of [...externalIncludes].sort()) {
    if (!declaredIncludes.has(include)) errors.push(`firmware include <${include}> is not owned by a pinned library`);
  }
  for (const [include, libraryName] of [...declaredIncludes.entries()].sort()) {
    if (!externalIncludes.has(include)) errors.push(`libraries.${libraryName}.includes declares unused firmware header ${include}`);
  }
  return errors;
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
    arduino_libraries_json: JSON.stringify(
      Object.entries(config.libraries).map(([name, library]) => `${name}@${library.version}`),
    ),
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

function trustedDependencyHost(url) {
  try {
    return new Set([
      "api.github.com",
      "downloads.arduino.cc",
      "github.com",
      "mcudude.github.io",
    ]).has(new URL(url).hostname.toLowerCase());
  } catch {
    return false;
  }
}

async function fetchWithCurl(url, accept) {
  if (!trustedDependencyHost(url)) throw new Error(`Refusing curl fallback for untrusted dependency source: ${url}`);
  const executable = process.platform === "win32" ? "curl.exe" : "curl";
  const { stdout } = await execFileAsync(
    executable,
    [
      "--fail",
      "--silent",
      "--show-error",
      "--location",
      "--retry",
      "3",
      "--header",
      `Accept: ${accept}`,
      "--header",
      "User-Agent: PCController-dependency-radar/2.0",
      url,
    ],
    { encoding: null, maxBuffer: 80 * 1024 * 1024, timeout: 90_000 },
  );
  return Buffer.from(stdout);
}

async function fetchBuffer(url, accept = "application/json") {
  const headers = {
    Accept: accept,
    "User-Agent": "PCController-dependency-radar/1.0",
  };
  if (url.startsWith("https://api.github.com/") && process.env.GITHUB_TOKEN) {
    headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
    headers["X-GitHub-Api-Version"] = "2022-11-28";
  }

  let response;
  try {
    response = await fetch(url, { headers, redirect: "follow", signal: AbortSignal.timeout(90_000) });
  } catch (error) {
    if (trustedDependencyHost(url)) return fetchWithCurl(url, accept);
    throw error;
  }
  if (!response.ok) {
    // Official CDNs and GitHub occasionally challenge Node's HTTP fingerprint
    // while remaining available to curl and the Arduino CLI. The fallback is
    // host-allowlisted and still fails closed on HTTP or checksum errors.
    if (trustedDependencyHost(url)) return fetchWithCurl(url, accept);
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

function checksumFromIndex(checksum, label) {
  const match = /^SHA-256:([a-fA-F0-9]{64})$/u.exec(String(checksum ?? ""));
  if (!match) throw new Error(`${label} does not publish a valid SHA-256 checksum`);
  return match[1].toLowerCase();
}

function normalizedGithubRepository(url) {
  const match = /^https:\/\/github\.com\/([^/]+\/[^/#]+?)(?:\.git)?\/?$/iu.exec(String(url ?? ""));
  return match?.[1] ?? null;
}

function releaseListUrl(repository) {
  return `https://github.com/${repository}/releases`;
}

async function releaseEvidence(repository, version) {
  const fallback = { releaseNotesUrl: releaseListUrl(repository) };
  try {
    const releases = await fetchJson(`https://api.github.com/repos/${repository}/releases?per_page=100`);
    const release = (Array.isArray(releases) ? releases : []).find(
      (candidate) => String(candidate.tag_name ?? "").replace(/^v/iu, "") === version,
    );
    if (!release?.html_url) return fallback;
    return {
      releaseNotesUrl: release.html_url,
      releaseTitle: String(release.name ?? release.tag_name ?? version).replace(/\s+/gu, " ").trim().slice(0, 160),
      releasedAt: release.published_at ?? release.created_at ?? null,
    };
  } catch {
    // Release notes are useful review context, but the signed official package
    // indexes remain the authority. Their temporary absence must not disguise
    // a real core or library update or weaken all-or-nothing index validation.
    return fallback;
  }
}

function verifiedArchive(entry, label, allowedUrl) {
  const archiveUrl = String(entry?.url ?? "");
  if (!allowedUrl(archiveUrl)) throw new Error(`${label} archive URL is outside the trusted package host`);
  return {
    archiveUrl,
    archiveSha256: checksumFromIndex(entry?.checksum, label),
    archiveSize: String(entry?.size ?? ""),
  };
}

function verifyPinnedArchive(pinned, official, label) {
  if (pinned?.url !== official.archiveUrl) throw new Error(`${label} pinned archive URL no longer matches the official index`);
  if (pinned?.sha256 !== official.archiveSha256) throw new Error(`${label} pinned SHA-256 no longer matches the official index`);
}

async function arduinoCliArchiveEvidence(release, version) {
  const asset = `arduino-cli_${version}_Linux_64bit.tar.gz`;
  const archiveAsset = (release.assets ?? []).find((candidate) => candidate.name === asset);
  if (!archiveAsset?.browser_download_url) throw new Error(`Arduino CLI ${version} does not publish ${asset}`);
  const checksumAsset = (release.assets ?? []).find((candidate) => /checksums?\.txt$/i.test(candidate.name));
  if (!checksumAsset?.browser_download_url) throw new Error(`Arduino CLI ${version} does not publish a checksum text asset`);
  const checksums = (await fetchBuffer(checksumAsset.browser_download_url, "text/plain")).toString("utf8");
  const sha256 = checksumForAsset(checksums, asset);
  return {
    asset,
    sha256,
    archiveUrl: archiveAsset.browser_download_url,
    archiveSha256: sha256,
    archiveSize: String(archiveAsset.size ?? ""),
  };
}

async function latestArduinoCli(config) {
  const release = await fetchJson(OFFICIAL_SOURCES.arduinoCliApi);
  const version = String(release.tag_name ?? "").replace(/^v/, "");
  if (!VERSION_PATTERN.test(version) || version.includes("-")) throw new Error(`Unexpected latest Arduino CLI tag: ${release.tag_name}`);
  const latestArchive = await arduinoCliArchiveEvidence(release, version);
  const pinnedArchive = version === config.arduinoCli.version
    ? latestArchive
    : await arduinoCliArchiveEvidence(
      await fetchJson(`https://api.github.com/repos/${config.arduinoCli.repository}/releases/tags/v${config.arduinoCli.version}`),
      config.arduinoCli.version,
    );
  if (config.arduinoCli.linux64.asset !== pinnedArchive.asset) throw new Error(`Pinned Arduino CLI ${config.arduinoCli.version} asset no longer matches the official release`);
  if (config.arduinoCli.linux64.sha256 !== pinnedArchive.sha256) throw new Error(`Pinned Arduino CLI ${config.arduinoCli.version} SHA-256 no longer matches the official release`);
  return {
    version,
    asset: latestArchive.asset,
    sha256: latestArchive.sha256,
    source: release.html_url ?? OFFICIAL_SOURCES.arduinoCliReleases,
    repository: config.arduinoCli.repository,
    archiveUrl: latestArchive.archiveUrl,
    archiveSha256: latestArchive.archiveSha256,
    archiveSize: latestArchive.archiveSize,
    releaseNotesUrl: release.html_url ?? OFFICIAL_SOURCES.arduinoCliReleases,
    releaseTitle: String(release.name ?? release.tag_name ?? version).replace(/\s+/gu, " ").trim().slice(0, 160),
    releasedAt: release.published_at ?? release.created_at ?? null,
  };
}

async function latestMiniCore(config) {
  // The config value is validated against this constant before any report is
  // generated. Fetch the allowlisted source directly so repository data can
  // never redirect the dependency radar to a different network location.
  const index = await fetchJson(OFFICIAL_SOURCES.miniCoreIndex);
  const platforms = (index.packages ?? []).flatMap((entry) => entry.platforms ?? []);
  const matching = platforms.filter((platform) => platform.architecture === "avr" && /minicore/i.test(platform.name ?? ""));
  const version = latestStable(matching.map((platform) => String(platform.version)));
  const latest = matching.find((platform) => String(platform.version) === version);
  const current = matching.find((platform) => String(platform.version) === config.miniCore.version);
  if (!latest) throw new Error(`Official MiniCore index is missing metadata for ${version}`);
  if (!current) throw new Error(`Pinned MiniCore ${config.miniCore.version} is absent from the official index`);
  const currentArchive = verifiedArchive(
    current,
    `MiniCore ${config.miniCore.version}`,
    (url) => url === `https://MCUdude.github.io/MiniCore/MiniCore-${config.miniCore.version}.tar.bz2`,
  );
  verifyPinnedArchive(config.miniCore.archive, currentArchive, `MiniCore ${config.miniCore.version}`);
  const archive = verifiedArchive(
    latest,
    `MiniCore ${version}`,
    (url) => url === `https://MCUdude.github.io/MiniCore/MiniCore-${version}.tar.bz2`,
  );
  const release = await releaseEvidence(config.miniCore.repository, version);
  return {
    version,
    source: OFFICIAL_SOURCES.miniCoreIndex,
    repository: config.miniCore.repository,
    ...archive,
    ...release,
  };
}

async function loadArduinoLibraryIndex() {
  const encoded = await fetchBuffer(OFFICIAL_SOURCES.arduinoLibraryIndex, "application/gzip, application/json");
  const decoded = encoded[0] === 0x1f && encoded[1] === 0x8b ? gunzipSync(encoded) : encoded;
  return JSON.parse(decoded.toString("utf8"));
}

async function latestArduinoLibrary(name, config, indexPromise) {
  const index = await indexPromise;
  const matching = (index.libraries ?? []).filter((library) => String(library.name).toLowerCase() === name.toLowerCase());
  const version = latestStable(matching.map((library) => String(library.version)));
  const latest = matching.find((library) => String(library.version) === version);
  const current = matching.find((library) => String(library.version) === config.version);
  if (!latest) throw new Error(`Arduino library index is missing metadata for ${name} ${version}`);
  if (!current) throw new Error(`Pinned ${name} ${config.version} is absent from the Arduino library index`);

  for (const [entry, entryVersion] of [[current, config.version], [latest, version]]) {
    const repository = normalizedGithubRepository(entry.repository ?? entry.website);
    if (repository !== config.repository) throw new Error(`${name} ${entryVersion} repository does not match ${config.repository}`);
    const provided = new Set(entry.providesIncludes ?? []);
    for (const include of config.includes) {
      if (!provided.has(include)) throw new Error(`${name} ${entryVersion} no longer provides ${include}`);
    }
  }

  const currentArchive = verifiedArchive(
    current,
    `${name} ${config.version}`,
    (url) => url.startsWith("https://downloads.arduino.cc/libraries/"),
  );
  verifyPinnedArchive(config.archive, currentArchive, `${name} ${config.version}`);
  const archive = verifiedArchive(
    latest,
    `${name} ${version}`,
    (url) => url.startsWith("https://downloads.arduino.cc/libraries/"),
  );
  const release = await releaseEvidence(config.repository, version);
  return {
    version,
    source: OFFICIAL_SOURCES.arduinoLibraryIndex,
    repository: config.repository,
    ...archive,
    ...release,
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
  const libraries = Object.entries(config.libraries);
  const libraryIndex = loadArduinoLibraryIndex();
  const results = await Promise.allSettled([
    latestArduinoCli(config),
    latestMiniCore(config),
    ...libraries.map(([name, library]) => latestArduinoLibrary(name, library, libraryIndex)),
  ]);
  const dependencies = [
    dependencyItem("arduinoCli", "Arduino CLI", config.arduinoCli.version, results[0]),
    dependencyItem("miniCore", "MiniCore AVR core", config.miniCore.version, results[1]),
    ...libraries.map(([name, library], index) =>
      dependencyItem(`library:${name}`, `${name} library`, library.version, results[index + 2])),
  ];
  return {
    schemaVersion: 2,
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
  "archiveUrl",
  "archiveSha256",
  "archiveSize",
  "releaseNotesUrl",
  "releaseTitle",
  "releasedAt",
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
  for (const [name, currentLibrary] of Object.entries(currentConfig.libraries ?? {})) {
    synchronized = replaceAdjacentDependencyVersion(
      synchronized,
      escapeRegularExpression(name),
      currentLibrary?.version,
      nextConfig.libraries?.[name]?.version,
    );
  }
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
    let descriptor;
    try {
      descriptor = fs.openSync(documentPath, "r+");
    } catch (error) {
      if (error && typeof error === "object" && ["ENOENT", "ENOTDIR"].includes(error.code)) {
        continue;
      }
      throw error;
    }
    try {
      if (!fs.fstatSync(descriptor).isFile()) continue;
      const before = fs.readFileSync(descriptor, "utf8");
      const after = synchronizeDependencyMentions(before, currentConfig, nextConfig);
      if (after === before) continue;
      const bytes = Buffer.from(after, "utf8");
      let written = 0;
      while (written < bytes.length) {
        const count = fs.writeSync(
          descriptor,
          bytes,
          written,
          bytes.length - written,
          written,
        );
        if (count <= 0) throw new Error(`Could not update ${relativePath}`);
        written += count;
      }
      fs.ftruncateSync(descriptor, bytes.length);
      fs.fsyncSync(descriptor);
      changed.push(relativePath);
    } finally {
      fs.closeSync(descriptor);
    }
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
      updated.miniCore.archive.url = item.candidate.archiveUrl;
      updated.miniCore.archive.sha256 = item.candidate.archiveSha256;
    } else if (item.key.startsWith("library:")) {
      const libraryName = item.key.slice("library:".length);
      if (!updated.libraries[libraryName]) throw new Error(`Update report contains unknown Arduino library: ${libraryName}`);
      updated.libraries[libraryName].version = item.candidate.version;
      updated.libraries[libraryName].archive.url = item.candidate.archiveUrl;
      updated.libraries[libraryName].archive.sha256 = item.candidate.archiveSha256;
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
    "# 🔭 PCController Dependency Radar",
    "",
    `> ${headline}`,
    "",
    `**Supply-chain health:** \`${meter}\` ${healthy}/${report.dependencies.length} verified current`,
    "",
    `| Component | ${applied ? "Previous pin" : "Pinned"} | Official latest | Signal | Index | Release notes |`,
    "|:--|:--:|:--:|:--|:--|:--|",
    ...report.dependencies.map((item) => {
      const latest = item.latest ?? "unavailable";
      const source = item.source ? `[official](${item.source})` : "see diagnostics";
      const notes = item.candidate?.releaseNotesUrl ? `[review](${item.candidate.releaseNotesUrl})` : "unavailable";
      return `| ${md(item.label)} | \`${md(item.current)}\` | \`${md(latest)}\` | ${statusPresentation(item.status)} | ${source} | ${notes} |`;
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

  const updates = report.dependencies.filter((item) => item.updateAvailable && item.candidate);
  if (updates.length > 0) {
    lines.push(
      "## 🧭 Upgrade review",
      "",
      "| Component | Version change | Verified archive | SHA-256 | Release context |",
      "|:--|:--:|:--|:--|:--|",
      ...updates.map((item) => {
        const candidate = item.candidate;
        const archive = candidate.archiveUrl ? `[download](${candidate.archiveUrl})` : "see official release";
        const digest = candidate.archiveSha256 ?? candidate.sha256 ?? "not published";
        const release = candidate.releaseNotesUrl
          ? `[${md(candidate.releaseTitle ?? "release notes")}](${candidate.releaseNotesUrl})`
          : "unavailable";
        return `| ${md(item.label)} | \`${md(item.current)}\` → \`${md(item.latest)}\` | ${archive} | \`${md(digest)}\` | ${release} |`;
      }),
      "",
      "Release bodies are not copied into the pull request; exact upstream links keep the proposal bounded and prevent third-party Markdown from becoming trusted automation output.",
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
    `| MiniCore archive SHA-256 | \`${md(config.miniCore.archive.sha256)}\` |`,
    ...Object.entries(config.libraries).map(
      ([name, library]) => `| Arduino library | \`${md(name)}@${md(library.version)}\` · \`${md(library.archive.sha256)}\` |`,
    ),
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
    "- Every pinned archive and SHA-256 must still match its official release or index; library repositories and provided headers are reconciled too.",
    "- The firmware include inventory fails if a third-party header is undeclared or a declared Arduino library is no longer used.",
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

export function pullRequestTitle(changed) {
  const labels = changed.map((item) => item.label.replace(/\s+(?:AVR core|library)$/u, ""));
  if (labels.length === 0) return "⬆️ AVR supply chain · Verified dependency refresh";
  const visible = labels.slice(0, 3).join(" + ");
  const remainder = labels.length > 3 ? ` + ${labels.length - 3} more` : "";
  return `⬆️ AVR supply chain · ${visible}${remainder}`;
}

async function main(argv) {
  const { command, options } = parseArguments(argv);
  if (command === "help" || command === "--help" || command === "-h") {
    console.log(usage());
    return;
  }

  const loaded = loadConfig(options.config ?? DEFAULT_CONFIG);
  if (path.resolve(loaded.path) === path.resolve(DEFAULT_CONFIG)) {
    const inventoryErrors = validateFirmwareDependencyInventory(loaded.config, REPOSITORY_ROOT);
    if (inventoryErrors.length > 0) throw new Error(`Invalid firmware dependency inventory:\n- ${inventoryErrors.join("\n- ")}`);
  }
  if (command === "validate") {
    console.log(`Dependency configuration and firmware inventory are valid: ${loaded.path}`);
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

  const changedItems = report.dependencies.filter((item) => item.updateAvailable);
  const changed = changedItems.map((item) => item.key);
  writeOutputs(
    {
      audit_complete: report.complete,
      updates_available: report.updatesAvailable,
      changed_dependencies: changed.join(","),
      config_changed: report.applied,
      documentation_updated: (report.documentationUpdated ?? []).join(","),
      report_path: reportPath ?? "",
      pull_request_title: pullRequestTitle(changedItems),
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
