#!/usr/bin/env node

// Publishes canonical repository documentation to the GitHub wiki.
// GitHub must have one UI-created page before its *.wiki.git remote exists.

import { spawnSync } from "node:child_process";
import {
  copyFileSync,
  existsSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  createChalk,
  renderUnicodeTable,
} from "../Build/presentation.mjs";
import {
  PRODUCT_METADATA,
  resolveProductTitle,
} from "../Build/product-metadata.mjs";
import {
  repositoryWebUrl,
  resolveRepository,
} from "../../.github/scripts/repository-context.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "..", "..");
const repository = resolveRepository(process.env, { cwd: root });
const repositoryMain = repositoryWebUrl(repository, process.env);
const remote = `${repositoryMain}.wiki.git`;
const apply = process.argv.includes("--apply");
const canonicalProductName = PRODUCT_METADATA.productName.trim();
const displayProductTitle = resolveProductTitle(process.env);
const chalk = createChalk(
  { noColor: Boolean(process.env.NO_COLOR) },
  process.stdout.isTTY,
);

const pages = [
  ["Getting-Started", "docs/Getting-Started-and-Operations.md", "Getting started and operations"],
  ["Front-Panel-and-Menus", "docs/Front-Panel-and-Menus.md", "Front panel and menus"],
  ["Hardware-Initialization-and-Tuning", "docs/Hardware-Initialization-and-Tuning.md", "Hardware initialization and tuning"],
  ["Host-Configuration-and-Integrations", "docs/Host-Configuration-and-Integrations.md", "Host configuration and integrations"],
  ["Protocol-and-Network-API", "Tools/Controller/docs/Protocol-and-Network-API.md", "Protocol and network API"],
  ["C-Library-API", "Tools/Controller/docs/C-Library-API.md", "C library API"],
  ["Control-Surface-Capability-Matrix", "Tools/Controller/docs/Control-Surface-Capability-Matrix.md", "Control-surface capability matrix"],
  ["Toolchain-and-Safe-Programming", "docs/Toolchain-and-Safe-Programming.md", "Toolchain bootstrap and safe programming"],
  ["Memory-and-Feature-Tradeoffs", "docs/Memory-and-Feature-Tradeoffs.md", "Memory and feature tradeoffs"],
  ["CI-CD-and-Releases", "docs/CI-CD-and-Releases.md", "CI/CD and releases"],
  ["Urboot-Custom", "Tools/Bootloader/Urboot-Custom/README.md", "Urboot-Custom reproducible fork"],
  ["Project-Acceptance", "docs/Project-Checklist.md", "Project acceptance"],
  ["Requirements-Backlog", "docs/Requirements-Backlog.md", "Requirements backlog"],
];
const obsoletePages = [
  "Architecture-and-Source-History.md",
  "Completion-Recovery-Audit.md",
  "Local-Library-Merge-History.md",
  "Project-Checklist.md",
  "Upstream-Source-Audit.md",
];

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: options.cwd ?? root,
    encoding: "utf8",
    windowsHide: true,
  });
  if (result.status !== 0) {
    const detail = `${result.stdout ?? ""}${result.stderr ?? ""}`.trim();
    throw new Error(`${command} ${args.join(" ")} failed${detail ? `:\n${detail}` : ""}`);
  }
  return (result.stdout ?? "").trim();
}

function validateSources() {
  for (const [, source] of pages) {
    const sourcePath = join(root, source);
    if (!existsSync(sourcePath)) {
      throw new Error(`canonical wiki source is missing: ${source}`);
    }
    if (!readFileSync(sourcePath, "utf8").trim()) {
      throw new Error(`canonical wiki source is empty: ${source}`);
    }
  }
  const banner = join(root, "docs", "assets", "doc-banner.svg");
  if (!existsSync(banner) || !readFileSync(banner, "utf8").trim()) {
    throw new Error("canonical documentation banner is missing or empty");
  }
}

function homeText() {
  const links = pages
    .map(([slug, , title]) => `- [${title}](${slug})`)
    .join("\n");
  return `# ${canonicalProductName} Wiki\n\n` +
    `${canonicalProductName} combines the ATmega328P firmware, native host, ` +
    `embedded WebUI, protocol/API surfaces, safe programming, and Virtual Board.\n\n` +
    `## Maintained documentation\n\n${links}\n\n` +
    `Repository Markdown remains canonical; this wiki is its published mirror.\n`;
}

function sidebarText() {
  return `### ${canonicalProductName}\n\n` + pages
    .map(([slug, , title]) => `- [${title}](${slug})`)
    .join("\n") + "\n";
}

function wikiPageText(source) {
  return readFileSync(join(root, source), "utf8")
    .replaceAll(/href="(?:\.\.\/)+README\.md"/gu, `href="${repositoryMain}"`)
    .replaceAll(/src="(?:\.\.\/)*docs\/assets\/doc-banner\.svg"/gu, 'src="pccontroller-doc-banner.svg"')
    .replaceAll('src="assets/doc-banner.svg"', 'src="pccontroller-doc-banner.svg"');
}

validateSources();

if (!apply) {
  console.log(chalk.bold.cyanBright(`◆ ${displayProductTitle} wiki publication plan`));
  console.log(chalk.dim(`Remote: ${remote}`));
  console.log(renderUnicodeTable(
    [
      { label: "Source", align: "left" },
      { label: "Wiki page", align: "left" },
    ],
    pages.map(([slug, source]) => [source, `${slug}.md`]),
    { chalk },
  ));
  console.log(chalk.green("Generated: Home.md, _Sidebar.md"));
  console.log(chalk.dim(`Retired pages removed: ${obsoletePages.join(", ")}`));
  console.log(chalk.dim("Run with --apply after GitHub's first wiki page has been created."));
  process.exit(0);
}

const staging = mkdtempSync(join(tmpdir(), "pccontroller-wiki-"));
const tempRoot = resolve(tmpdir());
const expectedPrefix = tempRoot.toLowerCase();
if (!resolve(staging).toLowerCase().startsWith(expectedPrefix)) {
  throw new Error(`refusing unsafe wiki staging path: ${staging}`);
}

try {
  try {
    run("git", ["clone", "--depth", "1", remote, staging]);
  } catch (error) {
    throw new Error(
      `${error.message}\nGitHub has not exposed the wiki Git remote yet. ` +
      `Create its first page once at ${repositoryMain}/wiki/_new, then retry.`,
    );
  }

  for (const [slug, source] of pages) {
    writeFileSync(join(staging, `${slug}.md`), wikiPageText(source), "utf8");
  }
  for (const page of obsoletePages) {
    rmSync(join(staging, page), { force: true });
  }
  copyFileSync(join(root, "docs", "assets", "doc-banner.svg"), join(staging, "pccontroller-doc-banner.svg"));
  writeFileSync(join(staging, "Home.md"), homeText(), "utf8");
  writeFileSync(join(staging, "_Sidebar.md"), sidebarText(), "utf8");

  const changes = run("git", ["status", "--short"], { cwd: staging });
  if (!changes) {
    console.log(chalk.bold.green("✓ Wiki is already synchronized."));
  } else {
    console.log(changes);
    run("git", ["add", "--all"], { cwd: staging });
    run("git", ["commit", "-m", "docs: synchronize project wiki"], { cwd: staging });
    run("git", ["push", "origin", "HEAD"], { cwd: staging });
    const revision = run("git", ["rev-parse", "--short=12", "HEAD"], { cwd: staging });
    console.log(chalk.bold.green(`✓ Wiki synchronized at ${revision}.`));
  }
} finally {
  const resolved = resolve(staging);
  const child = relative(tempRoot, resolved);
  if (resolved.toLowerCase().startsWith(expectedPrefix) &&
      child.startsWith("pccontroller-wiki-") && !child.includes("..")) {
    rmSync(resolved, { recursive: true, force: true });
  }
}
