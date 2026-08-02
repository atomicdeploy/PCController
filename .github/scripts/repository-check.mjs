import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, isAbsolute, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { resolveRepository } from "./repository-context.mjs";
import {
  actionPinFindings,
  isGeneratedOrBinaryPath,
  isOrdinaryTextFile,
  markdownAnchors,
  privacyFindings,
} from "./repository-policy.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const errors = [];

const requiredFiles = [
  "README.md",
  "LICENSE",
  "REUSE.toml",
  "THIRD_PARTY_NOTICES.md",
  "LICENSES/MIT.txt",
  "LICENSES/BSD-2-Clause.txt",
  "docs/README.md",
  "docs/Getting-Started-and-Operations.md",
  "docs/CI-CD-and-Releases.md",
  "docs/Project-Checklist.md",
  "Tools/Controller/README.md",
  "Tools/VirtualBoard/README.md",
  "Tools/VirtualBoard/CMakePresets.json",
  ".github/dependabot.yml",
  ".github/actionlint.yaml",
  ".github/workflows/build.yml",
  ".github/workflows/update-dependencies.yml",
  ".github/workflows/codeql.yml",
  ".github/workflows/deploy-avr.yml",
  ".github/workflows/firmware.yml",
  ".github/workflows/host.yml",
  ".github/workflows/release.yml",
  ".github/workflows/repository-health.yml",
  ".github/workflows/virtual-board.yml",
  ".github/scripts/package-directory.mjs",
  ".github/scripts/package-directory.test.mjs",
  ".github/scripts/assert-defaults.test.mjs",
  ".github/scripts/assert-firmware-defaults.mjs",
  ".github/scripts/assert-host-defaults.mjs",
  ".github/scripts/repository-context.mjs",
  ".github/scripts/repository-context.test.mjs",
  ".github/scripts/repository-policy.mjs",
  ".github/scripts/repository-policy.test.mjs",
  "Tools/Dependencies/export-lock.mjs",
  "Tools/Dependencies/export-lock.test.mjs",
  "Tools/Dependencies/dependency-policy.json",
  "Tools/Dependencies/resolved-tools-lock.json",
  "Tools/Controller/toolchain-profile.json",
  "Tools/Controller/toolchain-lock.json",
  "Tools/Controller/api/openapi.json",
  "Tools/Controller/api/asyncapi.json",
  "Tools/Controller/api/jsonrpc.schema.json",
  "Tools/Controller/api/reference.html",
  "Tools/Audit/generate-api-reference.mjs",
  ".github/scripts/codebase-summary.mjs",
  ".github/scripts/codebase-summary.test.mjs",
  ".github/scripts/release-showcase.mjs",
  ".github/scripts/security-config-check.mjs",
  ".github/scripts/security-config-check.test.mjs",
  ".github/scripts/step-summary.mjs",
  ".github/scripts/step-summary.test.mjs",
  ".github/scripts/usage-progress.mjs",
];

function report(message) {
  errors.push(message);
  process.stderr.write(`ERROR: ${message}\n`);
}

for (const relativePath of requiredFiles) {
  const absolutePath = resolve(root, relativePath);
  if (!existsSync(absolutePath) || !statSync(absolutePath).isFile()) {
    report(`required file is missing: ${relativePath}`);
    continue;
  }
  if (statSync(absolutePath).size === 0) {
    report(`required file is empty: ${relativePath}`);
  }
}

const license = readFileSync(resolve(root, "LICENSE"), "utf8");
if (!license.includes("SPDX-License-Identifier: MIT OR BSD-2-Clause")) {
  report("LICENSE does not declare the project dual-license expression");
}

const reuse = readFileSync(resolve(root, "REUSE.toml"), "utf8");
if (!reuse.includes('SPDX-License-Identifier = "MIT OR BSD-2-Clause"')) {
  report("REUSE.toml does not declare the aggregate dual license");
}

let trackedFiles = [];
let sourceFiles = [];
let filesCameFromGit = false;
try {
  trackedFiles = execFileSync("git", ["ls-files", "-z"], {
    cwd: root,
    encoding: "utf8",
  }).split("\0").filter(Boolean);
  filesCameFromGit = trackedFiles.length > 0;
  sourceFiles = execFileSync(
    "git",
    ["ls-files", "--cached", "--others", "--exclude-standard", "-z"],
    { cwd: root, encoding: "utf8" },
  ).split("\0").filter(Boolean);
} catch {
  // The initial local baseline may be checked before `git init`; CI always has Git.
}

if (sourceFiles.length === 0) {
  const ignoredDirectories = new Set([".git", ".build", ".ci", "bin", "build", "node_modules"]);
  const pending = [root];
  while (pending.length > 0) {
    const directory = pending.pop();
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (
        entry.isDirectory() &&
        (ignoredDirectories.has(entry.name) || (entry.name.startsWith(".") && entry.name !== ".github"))
      ) {
        continue;
      }
      const absolutePath = resolve(directory, entry.name);
      if (entry.isDirectory()) {
        pending.push(absolutePath);
      } else if (entry.isFile()) {
        sourceFiles.push(relative(root, absolutePath).replaceAll("\\", "/"));
      }
    }
  }
  trackedFiles = [...sourceFiles];
}

const forbiddenTrackedPaths = [
  /(^|\/)\.build\//i,
  /(^|\/)node_modules\//i,
  /^Tools\/Controller\/bin\//i,
  /^Tools\/Controller\/\.ci\//i,
  /^Tools\/VirtualBoard\/build\//i,
];
const forbiddenTrackedExtensions = /\.(dll|elf|exe|hex|o|obj|upx|zip)$/i;

for (const relativePath of trackedFiles) {
  const normalized = relativePath.replaceAll("\\", "/");
  if (filesCameFromGit && forbiddenTrackedPaths.some((pattern) => pattern.test(normalized))) {
    report(`generated path is tracked: ${normalized}`);
  }
  if (filesCameFromGit && forbiddenTrackedExtensions.test(normalized)) {
    report(`generated binary is tracked: ${normalized}`);
  }
  const absolutePath = resolve(root, relativePath);
  if (
    filesCameFromGit &&
    existsSync(absolutePath) &&
    statSync(absolutePath).size > 5 * 1024 * 1024
  ) {
    report(`tracked file exceeds 5 MiB: ${normalized}`);
  }
}

let repository = "";
try {
  repository = resolveRepository(process.env, { cwd: root });
} catch (error) {
  report(error.message);
}

try {
  execFileSync(process.execPath, ["Tools/Audit/generate-api-reference.mjs", "--check"], {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
} catch (error) {
  const detail = String(error.stderr || error.message || error).trim();
  report(`machine-readable API reference check failed: ${detail}`);
}

// These exact strings are positive/negative supply-chain policy fixtures. Keep
// the exception file- and value-specific so docs and all other source remain
// subject to the normal external-repository privacy gate.
const reviewedRepositoryFixtures = new Map([
  ["Tools/Dependencies/update.test.mjs", new Set([
    ["https://github", ".com/arduino/arduino-cli"].join(""),
    ["https://github", ".com/example/arduino-cli"].join(""),
  ])],
]);

for (const relativePath of sourceFiles) {
  const normalized = relativePath.replaceAll("\\", "/");
  const absolutePath = resolve(root, relativePath);
  if (
    !existsSync(absolutePath) ||
    isGeneratedOrBinaryPath(normalized) ||
    !isOrdinaryTextFile(absolutePath)
  ) {
    continue;
  }
  const content = readFileSync(absolutePath, "utf8");
  for (const finding of privacyFindings(normalized, content, { repository })) {
    if (
      finding.kind === "unreviewed repository reference" &&
      reviewedRepositoryFixtures.get(normalized)?.has(finding.match)
    ) {
      continue;
    }
    report(`${normalized}:${finding.line} contains ${finding.kind}: ${finding.match}`);
  }
  for (const finding of actionPinFindings(normalized, content)) {
    report(`${normalized}:${finding.line} contains ${finding.kind}: ${finding.match}`);
  }
}

function localLinkReference(rawTarget) {
  let target = rawTarget.trim();
  if (target.startsWith("<") && target.endsWith(">")) {
    target = target.slice(1, -1);
  } else {
    target = target.split(/\s+["']/u, 1)[0];
  }
  if (
    target.length === 0 ||
    /^[a-z][a-z0-9+.-]*:/iu.test(target) ||
    /^[A-Za-z]:[\\/]/u.test(target)
  ) {
    return null;
  }
  const hash = target.indexOf("#");
  const rawFragment = hash >= 0 ? target.slice(hash + 1) : "";
  target = (hash >= 0 ? target.slice(0, hash) : target).split("?", 1)[0];
  if (target && target.replaceAll("\\", "/").split("/").includes(".build")) {
    return null;
  }
  try {
    return {
      target: decodeURIComponent(target),
      fragment: decodeURIComponent(rawFragment),
    };
  } catch {
    return { target, fragment: rawFragment };
  }
}

const markdownAnchorCache = new Map();

function hasMarkdownAnchor(path, fragment) {
  let anchors = markdownAnchorCache.get(path);
  if (!anchors) {
    anchors = markdownAnchors(readFileSync(path, "utf8"));
    markdownAnchorCache.set(path, anchors);
  }
  return anchors.has(fragment);
}

for (const relativePath of trackedFiles.filter((path) => path.endsWith(".md"))) {
  const markdownPath = resolve(root, relativePath);
  // A local acceptance run may include intentionally deleted tracked docs
  // before the changes are committed. Git no longer lists them after merge,
  // so treat a missing worktree file as deleted rather than crashing the gate.
  if (!existsSync(markdownPath)) {
    continue;
  }
  const markdown = readFileSync(markdownPath, "utf8");
  const links = markdown.matchAll(/!?\[[^\]]*\]\(([^)]+)\)/gu);
  for (const match of links) {
    const reference = localLinkReference(match[1]);
    if (reference === null) {
      continue;
    }
    const targetPath = reference.target === ""
      ? markdownPath
      : reference.target.startsWith("/")
        ? resolve(root, reference.target.slice(1))
        : resolve(dirname(markdownPath), reference.target);
    if (!isAbsolute(targetPath) || !existsSync(targetPath)) {
      report(`${relativePath} has a missing local link: ${match[1]}`);
      continue;
    }
    if (
      reference.fragment !== "" &&
      targetPath.toLowerCase().endsWith(".md") &&
      !hasMarkdownAnchor(targetPath, reference.fragment)
    ) {
      report(`${relativePath} has a missing local heading: ${match[1]}`);
    }
  }
}

if (errors.length > 0) {
  process.stderr.write(`Repository check failed with ${errors.length} error(s).\n`);
  process.exit(1);
}

process.stdout.write(
  `Repository check passed: ${requiredFiles.length} required files and ${sourceFiles.length} tracked/unignored source files.\n`,
);
