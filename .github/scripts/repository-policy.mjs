import { closeSync, fstatSync, openSync, readSync } from "node:fs";

const normalized = (path) => String(path).replaceAll("\\", "/");

const generatedPathPatterns = [
  /(^|\/)\.git(?:\/|$)/iu,
  /(^|\/)\.cache(?:\/|$)/iu,
  /(^|\/)\.build(?:\/|$)/iu,
  /(^|\/)\.ci(?:\/|$)/iu,
  /(^|\/)node_modules(?:\/|$)/iu,
  /^Tools\/Controller\/bin(?:\/|$)/iu,
  /^Tools\/Controller\/\.bin-previous-[^/]+(?:\/|$)/iu,
  /^Tools\/Controller\/internal\/webui\/dist(?:\/|$)/iu,
  /^Tools\/Controller\/web\/(?:coverage|dist)(?:\/|$)/iu,
  /^Tools\/VirtualBoard\/(?:build|\.build)(?:\/|$)/iu,
];

const policyDefinitionPaths = new Set([
  ".github/scripts/repository-policy.mjs",
  ".github/scripts/repository-policy.test.mjs",
]);

// Current-product links are permitted only in maintained public entry points.
const repositoryIdentityPaths = new Set([
  "README.md",
  "docs/Alpha-Delivery-Ledger.md",
  "docs/CI-CD-and-Releases.md",
  "docs/Project-Checklist.md",
  "docs/Requirements-Backlog.md",
]);

// External repository URLs are limited to reviewed dependency provenance.
const thirdPartyProvenancePaths = new Set([
  "THIRD_PARTY_NOTICES.md",
  "REUSE.toml",
  "Tools/Build/package-lock.json",
  "Tools/Bootloader/Urboot-Custom/source-manifest.json",
  "Tools/Controller/internal/programmer/toolchain.go",
  "Tools/Controller/toolchain-lock.json",
  "Tools/Controller/web/package-lock.json",
  "Tools/Controller/web/third-party-notices/victory-vendor.txt",
  "Tools/Dependencies/pr-plan.mjs",
  "Tools/Dependencies/resolved-tools-lock.json",
  ".github/scripts/step-summary.mjs",
]);

const isThirdPartyProvenance = (path) =>
  thirdPartyProvenancePaths.has(path) || path.startsWith("LICENSES/");

const privatePathRules = [
  /[A-Za-z]:[\\/]Users[\\/][^\\/\s"'<>]+[\\/]/giu,
  /(?:^|[\s"'(])~[\\/]Desktop[\\/]/gimu,
  /\/(?:Users|home)\/[^/\s"'<>]+\/Desktop\//giu,
  /%(?:tmp|temp)%[\\/]/giu,
];

const externalNames = [
  ["ASA", "0002E"].join(""),
  ["motor", "encoder", "hmi"].join("_"),
  ["motor", "encoder", "hmi"].join(" "),
  ["HMI", "Controller"].join(" "),
  ["Next", "ion"].join(""),
  "Ardush",
  "ps_shell",
];
const externalNamePattern = new RegExp(
  externalNames.map((value) => value.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&")).join("|"),
  "giu",
);
const externalProjectPathPattern = /(?:\.\.[\\/])+(?:Puzzles|Timer)(?:[\\/]|\b)|\b(?:Puzzles|Timer) project\b/giu;
const staleOriginPattern = /\b(?:copied|ported|migrated|borrowed|inspired) from\b|\b(?:project lineage|origin project)\b/giu;
const repositoryUrlPattern = /https?:\/\/(?:www\.)?github\.com\/([A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)/giu;

function lineNumber(text, index) {
  let line = 1;
  for (let offset = 0; offset < index; offset += 1) {
    if (text.charCodeAt(offset) === 10) line += 1;
  }
  return line;
}

function addMatches(findings, text, pattern, kind) {
  pattern.lastIndex = 0;
  for (const match of text.matchAll(pattern)) {
    findings.push({ kind, line: lineNumber(text, match.index), match: match[0] });
  }
}

export function isGeneratedOrBinaryPath(path) {
  const value = normalized(path);
  return generatedPathPatterns.some((pattern) => pattern.test(value));
}

// Read a bounded text candidate from one opened descriptor. Returning the
// content (instead of reopening by path after a sample) keeps the size,
// binary check, and bytes passed to policy scanners on the same file object.
export function readOrdinaryTextFile(path) {
  if (isGeneratedOrBinaryPath(path)) return null;

  const descriptor = openSync(path, "r");
  try {
    const size = fstatSync(descriptor).size;
    if (size > 2 * 1024 * 1024) return null;
    const content = Buffer.allocUnsafe(size);
    let offset = 0;
    while (offset < size) {
      const bytesRead = readSync(descriptor, content, offset, size - offset, offset);
      if (bytesRead === 0) break;
      offset += bytesRead;
    }
    const complete = content.subarray(0, offset);
    if (complete.includes(0)) return null;
    return complete.toString("utf8");
  } finally {
    closeSync(descriptor);
  }
}

export function isOrdinaryTextFile(path) {
  return readOrdinaryTextFile(path) !== null;
}

export function privacyFindings(path, text, { repository = "" } = {}) {
  const relativePath = normalized(path);
  if (policyDefinitionPaths.has(relativePath)) return [];
  const findings = [];
  for (const pattern of privatePathRules) addMatches(findings, text, pattern, "private path");
  addMatches(findings, text, externalNamePattern, "external project name");
  addMatches(findings, text, externalProjectPathPattern, "external project path");
  if (!isThirdPartyProvenance(relativePath)) {
    addMatches(findings, text, staleOriginPattern, "stale origin language");
  }

  repositoryUrlPattern.lastIndex = 0;
  for (const match of text.matchAll(repositoryUrlPattern)) {
    const found = match[1].replace(/\.git$/iu, "");
    const ownRepository = repository && found.toLowerCase() === repository.toLowerCase();
    if (
      (ownRepository && repositoryIdentityPaths.has(relativePath)) ||
      isThirdPartyProvenance(relativePath)
    ) {
      continue;
    }
    findings.push({
      kind: ownRepository ? "repository identity outside allowlist" : "unreviewed repository reference",
      line: lineNumber(text, match.index),
      match: match[0],
    });
  }
  return findings;
}

export function actionPinFindings(path, text) {
  if (!normalized(path).startsWith(".github/workflows/")) return [];
  const findings = [];
  const lines = text.split(/\r?\n/u);
  for (let index = 0; index < lines.length; index += 1) {
    const match = lines[index].match(/^\s*(?:-\s*)?uses:\s*([^\s#]+)\s*(?:#\s*(v\d+))?\s*$/u);
    if (!match || match[1].startsWith("./")) continue;
    const separator = match[1].lastIndexOf("@");
    const revision = separator >= 0 ? match[1].slice(separator + 1) : "";
    if (!/^[0-9a-f]{40}$/u.test(revision) || !match[2]) {
      findings.push({
        kind: "floating GitHub Action reference",
        line: index + 1,
        match: lines[index].trim(),
      });
    }
  }
  return findings;
}

// Strip rendered Markdown tags without a replace-and-rescan sanitizer. Angle
// punctuation is immaterial to the resulting GitHub slug, so unmatched angle
// brackets are discarded as well.
function stripHeadingTags(value) {
  let result = "";
  for (let index = 0; index < value.length; index += 1) {
    if (value[index] === "<") {
      const closing = value.indexOf(">", index + 1);
      if (closing >= 0) index = closing;
      continue;
    }
    if (value[index] !== ">") result += value[index];
  }
  return result;
}

function markdownHeadingLabel(value) {
  const markdownLabel = value
    .replace(/!\[([^\]]*)\]\([^)]*\)/gu, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/gu, "$1")
    .replace(/[`*_~]/gu, "");
  return stripHeadingTags(markdownLabel)
    .replace(/&(?:amp|lt|gt|quot|#39);/gu, (entity) => ({
      "&amp;": "&",
      // Angle punctuation is removed by githubHeadingSlug, so omitting it here
      // preserves the slug without recreating markup after tag stripping.
      "&lt;": "",
      "&gt;": "",
      "&quot;": '"',
      "&#39;": "'",
    })[entity])
    .trim();
}

function githubHeadingSlug(value) {
  return markdownHeadingLabel(value)
    .normalize("NFKC")
    .toLocaleLowerCase("en-US")
    .replace(/[^\p{Letter}\p{Number}\p{Mark}\s_-]/gu, "")
    .replace(/\s/gu, "-");
}

// markdownAnchors models the stable heading IDs used by GitHub Markdown. It
// deliberately ignores fenced examples so a sample heading cannot satisfy a
// real documentation link, and applies GitHub's numeric duplicate suffixes.
export function markdownAnchors(text) {
  const anchors = new Set();
  const counts = new Map();
  let fence = "";
  for (const line of String(text).split(/\r?\n/gu)) {
    const fenceMatch = line.match(/^\s{0,3}(`{3,}|~{3,})/u);
    if (fenceMatch) {
      const marker = fenceMatch[1][0];
      if (fence === "") fence = marker;
      else if (fence === marker) fence = "";
      continue;
    }
    if (fence !== "") continue;

    for (const match of line.matchAll(/<a\s+(?:id|name)=["']([^"']+)["'][^>]*>/giu)) {
      anchors.add(match[1]);
    }
    const heading = line.match(/^\s{0,3}#{1,6}[\t ]+(.+?)(?:[\t ]+#+)?[\t ]*$/u);
    if (!heading) continue;
    const base = githubHeadingSlug(heading[1]);
    if (base === "") continue;
    const count = counts.get(base) || 0;
    counts.set(base, count + 1);
    anchors.add(count === 0 ? base : `${base}-${count}`);
  }
  return anchors;
}
