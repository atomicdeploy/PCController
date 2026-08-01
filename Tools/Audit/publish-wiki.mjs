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

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "..", "..");
const remote = "https://github.com/atomicdeploy/PCController.wiki.git";
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
  ["Toolchain-and-Safe-Programming", "docs/Toolchain-and-Safe-Programming.md", "Toolchain bootstrap and safe programming"],
  ["Memory-and-Feature-Tradeoffs", "docs/Memory-and-Feature-Tradeoffs.md", "Memory and feature tradeoffs"],
  ["Urboot-Custom", "Tools/Bootloader/Urboot-Custom/README.md", "Urboot-Custom reproducible fork"],
  ["Local-Library-Merge-History", "docs/Local-Library-Merge-History.md", "Local library merge history"],
  ["Upstream-Source-Audit", "Tools/Controller/docs/Upstream-Source-Audit.md", "Upstream source and license audit"],
  ["Completion-Recovery-Audit", "docs/Completion-Recovery-Audit.md", "Completion recovery audit"],
  ["Project-Checklist", "docs/Project-Checklist.md", "Project checklist"],
  ["Requirements-Backlog", "docs/Requirements-Backlog.md", "Requirements backlog"],
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
}

function homeText() {
  const links = pages
    .slice(0, 10)
    .map(([slug, , title]) => `- [${title}](${slug})`)
    .join("\n");
  return `# ${canonicalProductName} Wiki\n\n` +
    `${canonicalProductName} combines the ATmega328P controller firmware, the native ` +
    `opcode protocol, a reusable Go host/controller library, the Charm TUI, ` +
    `safe UART/Urclock programming, simulation, and project-owned tooling.\n\n` +
    `## Start here\n\n${links}\n\n` +
    `## Delivery and planning\n\n` +
    `- [Completion recovery audit](Completion-Recovery-Audit)\n` +
    `- [Project checklist](Project-Checklist)\n` +
    `- [Requirements backlog](Requirements-Backlog)\n\n` +
    `Repository Markdown remains canonical; this wiki is its published mirror.\n`;
}

function sidebarText() {
  return `### ${canonicalProductName}\n\n` + pages
    .map(([slug, , title]) => `- [${title}](${slug})`)
    .join("\n") + "\n";
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
      `Create its first page once at https://github.com/atomicdeploy/PCController/wiki/_new, then retry.`,
    );
  }

  for (const [slug, source] of pages) {
    copyFileSync(join(root, source), join(staging, `${slug}.md`));
  }
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
