#!/usr/bin/env node

// Publishes canonical repository documentation to the GitHub wiki.
// GitHub must have one UI-created page before its *.wiki.git remote exists.

import { spawnSync } from "node:child_process";
import {
  copyFileSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
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
const preview = process.argv.includes("--preview");
const canonicalProductName = PRODUCT_METADATA.productName.trim();
const displayProductTitle = resolveProductTitle(process.env);
const repositoryOwner = repository.split("/", 1)[0];
const projectBoard = `https://github.com/users/${repositoryOwner}/projects/3`;
const chalk = createChalk(
  { noColor: Boolean(process.env.NO_COLOR) },
  process.stdout.isTTY,
);

const pages = [
  ["Getting-Started", "docs/Getting-Started-and-Operations.md", "Getting started and operations", "🚀", "Start here"],
  ["Repository-Map", "docs/Repository-Map.md", "Repository and file map", "🗺️", "Start here"],
  ["Front-Panel-and-Menus", "docs/Front-Panel-and-Menus.md", "Front panel and menus", "🎛️", "Start here"],
  ["Hardware-Initialization-and-Tuning", "docs/Hardware-Initialization-and-Tuning.md", "Hardware initialization and tuning", "🔧", "Start here"],
  ["Host-Configuration-and-Integrations", "docs/Host-Configuration-and-Integrations.md", "Host configuration and integrations", "⚙️", "Start here"],
  ["Hosted-Front-Panel-Menus", "Tools/Controller/docs/Hosted-Front-Panel-Menus.md", "Hosted front-panel menus", "🧭", "Use and integrate"],
  ["Portable-WebUI", "Tools/Controller/docs/Portable-WebUI.md", "Portable WebUI", "🌐", "Use and integrate"],
  ["Protocol-and-Network-API", "Tools/Controller/docs/Protocol-and-Network-API.md", "Protocol and network API", "🔌", "Use and integrate"],
  ["C-Library-API", "Tools/Controller/docs/C-Library-API.md", "C library API", "🧩", "Use and integrate"],
  ["Control-Surface-Capability-Matrix", "Tools/Controller/docs/Control-Surface-Capability-Matrix.md", "Control-surface capability matrix", "↔️", "Use and integrate"],
  ["Build-Tool", "Tools/Build/README.md", "Build and packaging tool", "🏗️", "Build and test"],
  ["Firmware-Tool", "Tools/Firmware/README.md", "Firmware tool", "💾", "Build and test"],
  ["Host-Controller", "Tools/Controller/README.md", "Host Controller", "🖥️", "Build and test"],
  ["Virtual-Board", "Tools/VirtualBoard/README.md", "Virtual Board", "🧪", "Build and test"],
  ["Dependency-Maintenance", "Tools/Dependencies/README.md", "Dependency maintenance", "📦", "Build and test"],
  ["Toolchain-and-Safe-Programming", "docs/Toolchain-and-Safe-Programming.md", "Toolchain bootstrap and safe programming", "🛡️", "Build and test"],
  ["Memory-and-Feature-Tradeoffs", "docs/Memory-and-Feature-Tradeoffs.md", "Memory and feature tradeoffs", "🧠", "Build and test"],
  ["CI-CD-and-Releases", "docs/CI-CD-and-Releases.md", "CI/CD and releases", "🚦", "Build and test"],
  ["Urboot-Custom", "Tools/Bootloader/Urboot-Custom/README.md", "Urboot-Custom reproducible fork", "🔩", "Build and test"],
  ["Audit-and-Traceability", "Tools/Audit/README.md", "Audit and traceability tools", "🔍", "Project tracking"],
  ["Project-Acceptance", "docs/Project-Checklist.md", "Project acceptance", "✅", "Project tracking"],
  ["Requirements-Backlog", "docs/Requirements-Backlog.md", "Requirements backlog", "📋", "Project tracking"],
  ["Local-Library-Variant-Comparison", "docs/Local-Library-Variant-Comparison.md", "Local-library variant comparison", "🧬", "Project tracking"],
];
const pageSourceToSlug = new Map(pages.map(([slug, source]) => [source, slug]));
const groupIcons = new Map([
  ["Start here", "🚀"],
  ["Use and integrate", "🔌"],
  ["Build and test", "🛠️"],
  ["Project tracking", "📋"],
]);
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
  const icon = join(root, "Tools", "Controller", "web", "public", "favicon.svg");
  if (!existsSync(icon) || !readFileSync(icon, "utf8").trim()) {
    throw new Error("canonical product icon is missing or empty");
  }
}

function homeText() {
  const groups = [...groupIcons.keys()].map((group) => {
    const rows = pages
      .filter(([, , , , pageGroup]) => pageGroup === group)
      .map(([slug, , title, emoji]) => `| ${emoji} | [${title}](${slug}) |`)
      .join("\n");
    return `### ${groupIcons.get(group)} ${group}\n\n| | Guide |\n|:---:|---|\n${rows}`;
  }).join("\n\n");
  return `<div align="center">\n` +
    `  <a href="${repositoryMain}"><img src="pccontroller-doc-banner.svg" width="100%" alt="${canonicalProductName} documentation"></a>\n` +
    `  <br><br>\n` +
    `  <img src="pccontroller-icon.svg" width="72" alt="${canonicalProductName} product icon">\n` +
    `</div>\n\n` +
    `# ${canonicalProductName} Wiki\n\n` +
    `${canonicalProductName} combines the ATmega328P firmware, native Go host, ` +
    `embedded WebUI, protocol/API surfaces, safe programming, and Virtual Board.\n\n` +
    `> [!IMPORTANT]\n` +
    `> Repository Markdown is canonical. This Wiki is a generated, branded mirror; ` +
    `edit the linked source documentation through a pull request rather than changing a Wiki page by hand.\n\n` +
    `## 🧭 Choose a path\n\n` +
    `| Goal | Start here |\n|---|---|\n` +
    `| Run or connect for the first time | [Getting started and operations](Getting-Started) |\n` +
    `| Find the owning source, test, asset, or generated output | [Repository and file map](Repository-Map) |\n` +
    `| Program or recover a board safely | [Toolchain and safe programming](Toolchain-and-Safe-Programming) |\n` +
    `| Integrate an app or another host | [Protocol and network API](Protocol-and-Network-API) |\n` +
    `| See feature parity and remaining gaps | [Control-surface matrix](Control-Surface-Capability-Matrix) and [requirements backlog](Requirements-Backlog) |\n\n` +
    `## 🧱 One system, shared ownership\n\n` +
    `| ⚙️ Firmware | 🖥️ Host | 🌐 Interfaces | 🧪 Verification |\n|---|---|---|---|\n` +
    `| Deterministic safety, I/O, menus, RF, EEPROM | One primary serial owner and command dispatcher | WebUI, TUI, CLI, native shell, APIs and libraries | Virtual Board, local gates, CI and explicit hardware acceptance |\n\n` +
    `## 📚 Maintained knowledge base\n\n${groups}\n\n` +
    `## 🔄 Work and review\n\n` +
    `- [Project board](${projectBoard}) — status, domain, priority, and milestone overview.\n` +
    `- [Issues](${repositoryMain}/issues) — requirements and acceptance evidence.\n` +
    `- [Pull requests](${repositoryMain}/pulls) — reviewable implementation and work in progress.\n` +
    `- [Actions](${repositoryMain}/actions) — build, repository-health, security, and release evidence.\n\n` +
    `<details>\n<summary><strong>Contributor rule: source before mirror</strong></summary>\n\n` +
    `1. Use the [repository map](Repository-Map) to find the authoritative file.\n` +
    `2. Reconcile the change with its issue and existing pull requests.\n` +
    `3. Update source, tests, capability matrix, and relevant guide together.\n` +
    `4. Preview the Wiki from source; never preserve a hand-edited Wiki fork.\n\n` +
    `</details>\n`;
}

function sidebarText() {
  const groups = [...groupIcons.keys()].map((group) => {
    const links = pages
      .filter(([, , , , pageGroup]) => pageGroup === group)
      .map(([slug, , title, emoji]) => `- ${emoji} [${title}](${slug})`)
      .join("\n");
    return `#### ${groupIcons.get(group)} ${group}\n\n${links}`;
  }).join("\n\n");
  return `<div align="center"><img src="pccontroller-icon.svg" width="52" alt="${canonicalProductName}"></div>\n\n` +
    `### [${canonicalProductName}](Home)\n\n${groups}\n\n---\n\n` +
    `- [Source](${repositoryMain})\n` +
    `- [Project board](${projectBoard})\n` +
    `- [Issues](${repositoryMain}/issues) · [Pull requests](${repositoryMain}/pulls)\n`;
}

function separateTarget(raw) {
  const value = raw.trim();
  if (value.startsWith("<")) {
    const close = value.indexOf(">");
    if (close >= 0) return { target: value.slice(1, close), suffix: value.slice(close + 1) };
  }
  const match = value.match(/^(\S+)([\s\S]*)$/u);
  return match ? { target: match[1], suffix: match[2] } : { target: value, suffix: "" };
}

function rewriteWikiTarget(source, raw, image = false) {
  const { target, suffix } = separateTarget(raw);
  if (!target || target.startsWith("#") || target.startsWith("/") ||
      /^[a-z][a-z0-9+.-]*:/iu.test(target) || target.startsWith("//")) {
    return raw;
  }
  const special = target.search(/[?#]/u);
  const pathPart = special >= 0 ? target.slice(0, special) : target;
  const trailing = special >= 0 ? target.slice(special) : "";
  let decoded = pathPart;
  try {
    decoded = decodeURIComponent(pathPart);
  } catch {
    return raw;
  }
  const sourcePath = join(root, source);
  const resolved = resolve(dirname(sourcePath), decoded);
  const relativePath = relative(root, resolved).replaceAll("\\", "/");
  if (!relativePath || relativePath.startsWith("../") || relativePath === "..") return raw;
  const wikiSlug = pageSourceToSlug.get(relativePath);
  if (wikiSlug) return `${wikiSlug}${trailing}${suffix}`;
  if (relativePath === "README.md") return `${repositoryMain}${trailing}${suffix}`;
  if (relativePath === "docs/assets/doc-banner.svg") {
    return `pccontroller-doc-banner.svg${trailing}${suffix}`;
  }
  if (relativePath === "Tools/Controller/web/public/favicon.svg") {
    return `pccontroller-icon.svg${trailing}${suffix}`;
  }
  if (!existsSync(resolved)) return raw;
  const route = statSync(resolved).isDirectory() ? "tree" : image ? "raw" : "blob";
  return `${repositoryMain}/${route}/main/${relativePath.split("/").map(encodeURIComponent).join("/")}${trailing}${suffix}`;
}

function wikiPageText(source) {
  return readFileSync(join(root, source), "utf8")
    .replaceAll(/(!?\[[^\]]*\]\()([^)]+)(\))/gu, (match, open, target, close) =>
      `${open}${rewriteWikiTarget(source, target, open.startsWith("!"))}${close}`)
    .replaceAll(/\b(href|src)="([^"]+)"/gu, (match, attribute, target) =>
      `${attribute}="${rewriteWikiTarget(source, target, attribute === "src")}"`);
}

function materializeWiki(directory) {
  mkdirSync(directory, { recursive: true });
  for (const [slug, source] of pages) {
    writeFileSync(join(directory, `${slug}.md`), wikiPageText(source), "utf8");
  }
  copyFileSync(join(root, "docs", "assets", "doc-banner.svg"), join(directory, "pccontroller-doc-banner.svg"));
  copyFileSync(join(root, "Tools", "Controller", "web", "public", "favicon.svg"), join(directory, "pccontroller-icon.svg"));
  writeFileSync(join(directory, "Home.md"), homeText(), "utf8");
  writeFileSync(join(directory, "_Sidebar.md"), sidebarText(), "utf8");
}

function validateWiki(directory) {
  const expected = [
    "Home.md", "_Sidebar.md", "pccontroller-doc-banner.svg", "pccontroller-icon.svg",
    ...pages.map(([slug]) => `${slug}.md`),
  ];
  for (const file of expected) {
    const path = join(directory, file);
    if (!existsSync(path) || statSync(path).size === 0) {
      throw new Error(`generated Wiki file is missing or empty: ${file}`);
    }
  }
  const expectedFiles = new Set(expected);
  for (const file of expected.filter((name) => name.endsWith(".md"))) {
    const content = readFileSync(join(directory, file), "utf8");
    const targets = [
      ...content.matchAll(/!?\[[^\]]*\]\(([^)]+)\)/gu),
      ...content.matchAll(/\b(?:href|src)="([^"]+)"/gu),
    ].map((match) => separateTarget(match[1]).target);
    for (const target of targets) {
      if (!target || target.startsWith("#") || target.startsWith("/") ||
          /^[a-z][a-z0-9+.-]*:/iu.test(target) || target.startsWith("//")) continue;
      const clean = target.split(/[?#]/u, 1)[0];
      const candidate = clean.endsWith(".md") || clean.endsWith(".svg")
        ? clean
        : `${clean}.md`;
      if (!expectedFiles.has(candidate)) {
        throw new Error(`${file} has an unresolved generated Wiki link: ${target}`);
      }
    }
  }
}

validateSources();

if (apply && preview) {
  throw new Error("--apply and --preview are mutually exclusive");
}

if (preview) {
  const previewDirectory = resolve(root, ".build", "wiki-preview");
  const expectedDirectory = join(resolve(root, ".build"), "wiki-preview");
  if (previewDirectory !== expectedDirectory) {
    throw new Error(`refusing unsafe Wiki preview path: ${previewDirectory}`);
  }
  rmSync(previewDirectory, { recursive: true, force: true });
  materializeWiki(previewDirectory);
  validateWiki(previewDirectory);
  console.log(chalk.bold.green(`✓ Generated and validated ${pages.length} Wiki pages at ${previewDirectory}.`));
  process.exit(0);
}

if (!apply) {
  console.log(chalk.bold.cyanBright(`◆ ${displayProductTitle} wiki publication plan`));
  console.log(chalk.dim(`Remote: ${remote}`));
  console.log(renderUnicodeTable(
    [
      { label: "Group", align: "left" },
      { label: "Source", align: "left" },
      { label: "Wiki page", align: "left" },
    ],
    pages.map(([slug, source, , , group]) => [group, source, `${slug}.md`]),
    { chalk },
  ));
  console.log(chalk.green("Generated: Home.md, _Sidebar.md, branded banner and icon"));
  console.log(chalk.dim("Run with --preview to materialize and validate the complete generated Wiki locally."));
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

  materializeWiki(staging);
  for (const page of obsoletePages) {
    rmSync(join(staging, page), { force: true });
  }
  validateWiki(staging);

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
