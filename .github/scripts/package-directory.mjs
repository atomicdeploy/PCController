#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  appendFileSync,
  cpSync,
  existsSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  utimesSync,
  writeFileSync,
} from "node:fs";
import { basename, dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import { PRODUCT_METADATA } from "../../Tools/Build/product-metadata.mjs";
import { repositoryWebUrl, resolveRepository } from "./repository-context.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const productName = PRODUCT_METADATA.productName;
const [sourceArgument, packageName, targetName] = process.argv.slice(2);

if (!sourceArgument || !packageName || !targetName) {
  process.stderr.write(
    "Usage: package-directory.mjs SOURCE PACKAGE_NAME TARGET_NAME\n",
  );
  process.exit(2);
}

const source = resolve(root, sourceArgument);
if (!existsSync(source)) {
  throw new Error(`package source does not exist: ${sourceArgument}`);
}

const safe = (value, label) => {
  const normalized = String(value).trim().replace(/[^0-9A-Za-z._+-]+/gu, "-");
  if (!normalized || normalized === "." || normalized === "..") {
    throw new Error(`${label} does not contain a safe archive name`);
  }
  return normalized;
};

const version = safe(process.env.PCCONTROLLER_VERSION || "development", "version");
const name = safe(packageName, "package name");
const target = safe(targetName, "target name");
const outputDirectory = resolve(root, ".build", "release");
const stagingDirectory = resolve(root, ".build", "package-staging", `${name}-${target}`);
const packageRootName = `${name}-${version}-${target}`;
const packageRoot = join(stagingDirectory, packageRootName);
mkdirSync(outputDirectory, { recursive: true });
rmSync(stagingDirectory, { recursive: true, force: true });
mkdirSync(packageRoot, { recursive: true });

if (statSync(source).isDirectory()) {
  for (const entry of readdirSync(source, { withFileTypes: true })) {
    cpSync(join(source, entry.name), join(packageRoot, entry.name), {
      recursive: entry.isDirectory(),
      dereference: false,
      preserveTimestamps: true,
    });
  }
} else {
  cpSync(source, join(packageRoot, basename(source)), {
    preserveTimestamps: true,
  });
}

for (const projectFile of ["LICENSE", "THIRD_PARTY_NOTICES.md"]) {
  const projectPath = join(root, projectFile);
  if (existsSync(projectPath)) {
    cpSync(projectPath, join(packageRoot, projectFile));
  }
}

writeFileSync(join(packageRoot, "VERSION"), `${version}\n`, "utf8");
const sourceCommit = process.env.GITHUB_SHA || "local-build";
const sourceRepository = resolveRepository(process.env, { cwd: root });
const sourceRef = sourceCommit === "local-build" ? "main" : sourceCommit;
const sourceUrl = repositoryWebUrl(sourceRepository, process.env);
const packageManifest = {
  format: "pccontroller-distribution-package/v1",
  product: name,
  version,
  target,
  sourceCommit,
  sourceRepository,
  packageRoot: packageRootName,
};
writeFileSync(
  join(packageRoot, "PACKAGE-MANIFEST.json"),
  `${JSON.stringify(packageManifest, null, 2)}\n`,
  "utf8",
);

const packageReadme = (() => {
  const header = `# ${name} ${version} — ${target}\n\nThis is a CI-built, source-identified ${productName} distribution. Source: [${sourceRepository}@${sourceCommit}](${sourceUrl}/tree/${sourceRef}).\n\n`;
  const footer = `\n## Trust and support\n\n- \`PACKAGE-MANIFEST.json\` identifies the exact source and package root.\n- \`THIRD_PARTY_NOTICES.md\` records bundled dependency notices.\n- Release downloads include a sibling \`.sha256\` file and GitHub build-provenance attestation.\n- Full documentation: [${productName} ${target} guide](${sourceUrl}/blob/${sourceRef}/docs/CI-CD-and-Releases.md).\n`;
  if (/firmware/iu.test(name)) {
    return `${header}## Choose the correct image\n\n- \`PCController.ino.hex\`: normal guarded upload through the installed Urboot bootloader.\n- \`PCController.ino.with_bootloader.hex\`: explicit USBasp/ISP recovery only; this replaces the full flash including bootloader.\n- \`safe-default-eeprom.hex\`: complete 1 KiB current-layout defaults for an explicitly authorized recovery transaction.\n- \`firmware-manifest.json\`: flash, EEPROM, SRAM, source, and per-image SHA-256 evidence.\n\nDo not use the full-flash recovery image for a normal serial update. A successful CI build proves compilation and static validation, not physical-device acceptance.\n${footer}`;
  }
  if (/virtualboard/iu.test(name)) {
    const launch = /windows/iu.test(target)
      ? ".\\\\virtual_board.exe"
      : "chmod +x ./virtual_board && ./virtual_board";
    return `${header}## Run the simulator\n\n~~~text\n${launch}\n~~~\n\nThe Virtual Board package is a native simulator for protocol and UI integration work. Its CMake/CTest suite passed on ${target} before packaging.\n${footer}`;
  }
  const executable = /windows/iu.test(target) ? "controller.exe" : "controller";
  const launch = /windows/iu.test(target)
    ? `.\\\\${executable} --help`
    : `chmod +x ./${executable} && ./${executable} --help`;
  return `${header}## Run the Controller\n\n~~~text\n${launch}\n~~~\n\nThe package contains the native host controller and its platform resources for ${target}. Connect a supported board, then use \`${executable} ports\` and \`${executable} exec --port <PORT> status\`.\n${footer}`;
})();
writeFileSync(join(packageRoot, "README.md"), packageReadme, "utf8");

const stagedEntries = [];
const collectEntries = (directory) => {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const fullPath = join(directory, entry.name);
    stagedEntries.push(fullPath);
    if (entry.isDirectory()) collectEntries(fullPath);
  }
};
collectEntries(packageRoot);

// Normalize timestamps after every generated file exists. This also prevents a
// directory mtime from changing while its children are being normalized.
const sourceDateEpoch = Number.parseInt(
  process.env.SOURCE_DATE_EPOCH || "946684800",
  10,
);
if (!Number.isSafeInteger(sourceDateEpoch) || sourceDateEpoch < 0) {
  throw new Error("SOURCE_DATE_EPOCH must be a non-negative integer");
}
const reproducibleTime = new Date(sourceDateEpoch * 1000);
for (const path of [...stagedEntries, packageRoot].sort(
  (left, right) => right.split(/[\\/]/u).length - left.split(/[\\/]/u).length,
)) {
  if (!lstatSync(path).isSymbolicLink()) {
    utimesSync(path, reproducibleTime, reproducibleTime);
  }
}

const archive = resolve(outputDirectory, `${packageRootName}.tar.gz`);
const uncompressedArchive = resolve(outputDirectory, `${packageRootName}.tar`);
rmSync(uncompressedArchive, { force: true });
rmSync(archive, { force: true });
const memberNames = [
  packageRootName,
  ...stagedEntries
    .map((path) => relative(stagingDirectory, path).replaceAll("\\", "/"))
    .sort((left, right) => left.localeCompare(right, "en")),
];
const tarVersion = spawnSync("tar", ["--version"], {
  encoding: "utf8",
  shell: false,
  windowsHide: true,
});
const gnuTar = /GNU tar/iu.test(`${tarVersion.stdout || ""}${tarVersion.stderr || ""}`);
const reproducibleOptions = gnuTar
  ? [
      "--sort=name",
      `--mtime=@${sourceDateEpoch}`,
      "--owner=0",
      "--group=0",
      "--numeric-owner",
    ]
  : ["--uid", "0", "--gid", "0", "--uname", "root", "--gname", "root"];
const result = spawnSync(
  "tar",
  [
    "-cf",
    uncompressedArchive,
    ...reproducibleOptions,
    "--no-recursion",
    "-C",
    stagingDirectory,
    ...memberNames,
  ],
  { encoding: "utf8", shell: false, stdio: "inherit", windowsHide: true },
);
if (result.error) {
  throw new Error(`unable to start tar: ${result.error.message}`);
}
if (result.status !== 0) {
  throw new Error(`tar exited with status ${result.status}`);
}
writeFileSync(
  archive,
  gzipSync(readFileSync(uncompressedArchive), { level: 9, mtime: 0 }),
);
rmSync(uncompressedArchive, { force: true });

const archiveBytes = readFileSync(archive);
const sha256 = createHash("sha256").update(archiveBytes).digest("hex");
const checksum = `${archive}.sha256`;
writeFileSync(checksum, `${sha256}  ${basename(archive)}\n`, "utf8");

const archivePath = relative(root, archive).replaceAll("\\", "/");
const checksumPath = relative(root, checksum).replaceAll("\\", "/");
const packageRootPath = relative(root, packageRoot).replaceAll("\\", "/");
if (process.env.GITHUB_OUTPUT) {
  appendFileSync(
    process.env.GITHUB_OUTPUT,
    `archive=${archivePath}\nchecksum=${checksumPath}\nsha256=${sha256}\nbytes=${statSync(archive).size}\npackage-root=${packageRootPath}\n`,
    "utf8",
  );
}

process.stdout.write(
  `Packaged ${archivePath} with top-level ${packageRootName} (${statSync(archive).size} bytes, SHA256 ${sha256})\n`,
);
