import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, isAbsolute, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

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
  ".github/dependabot.yml",
  ".github/actionlint.yaml",
  ".github/dependencies.json",
  ".github/workflows/build.yml",
  ".github/workflows/codeql.yml",
  ".github/workflows/dependencies.yml",
  ".github/workflows/deploy-avr.yml",
  ".github/workflows/firmware.yml",
  ".github/workflows/host.yml",
  ".github/workflows/release.yml",
  ".github/workflows/repository-health.yml",
  ".github/workflows/virtual-board.yml",
  ".github/scripts/package-directory.mjs",
  ".github/scripts/package-directory.test.mjs",
  ".github/scripts/codebase-summary.mjs",
  ".github/scripts/codebase-summary.test.mjs",
  ".github/scripts/dependency-report.mjs",
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
let filesCameFromGit = false;
try {
  trackedFiles = execFileSync("git", ["ls-files", "-z"], {
    cwd: root,
    encoding: "utf8",
  }).split("\0").filter(Boolean);
  filesCameFromGit = trackedFiles.length > 0;
} catch {
  // The initial local baseline may be checked before `git init`; CI always has Git.
}

if (trackedFiles.length === 0) {
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
        trackedFiles.push(relative(root, absolutePath).replaceAll("\\", "/"));
      }
    }
  }
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

function localLinkTarget(rawTarget) {
  let target = rawTarget.trim();
  if (target.startsWith("<") && target.endsWith(">")) {
    target = target.slice(1, -1);
  } else {
    target = target.split(/\s+["']/u, 1)[0];
  }
  if (
    target.length === 0 ||
    target.startsWith("#") ||
    /^[a-z][a-z0-9+.-]*:/iu.test(target) ||
    /^[A-Za-z]:[\\/]/u.test(target)
  ) {
    return null;
  }
  target = target.split("#", 1)[0].split("?", 1)[0];
  if (target.replaceAll("\\", "/").split("/").includes(".build")) {
    return null;
  }
  try {
    return decodeURIComponent(target);
  } catch {
    return target;
  }
}

for (const relativePath of trackedFiles.filter((path) => path.endsWith(".md"))) {
  const markdownPath = resolve(root, relativePath);
  const markdown = readFileSync(markdownPath, "utf8");
  const links = markdown.matchAll(/!?\[[^\]]*\]\(([^)]+)\)/gu);
  for (const match of links) {
    const target = localLinkTarget(match[1]);
    if (target === null) {
      continue;
    }
    const targetPath = target.startsWith("/")
      ? resolve(root, target.slice(1))
      : resolve(dirname(markdownPath), target);
    if (!isAbsolute(targetPath) || !existsSync(targetPath)) {
      report(`${relativePath} has a missing local link: ${match[1]}`);
    }
  }
}

if (errors.length > 0) {
  process.stderr.write(`Repository check failed with ${errors.length} error(s).\n`);
  process.exit(1);
}

process.stdout.write(
  `Repository check passed: ${requiredFiles.length} required files and ${trackedFiles.length} source files.\n`,
);
