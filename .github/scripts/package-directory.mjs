#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  appendFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { basename, dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
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
mkdirSync(outputDirectory, { recursive: true });

const archive = resolve(outputDirectory, `${name}-${version}-${target}.tar.gz`);
const result = spawnSync(
  "tar",
  ["-czf", archive, "-C", dirname(source), basename(source)],
  { encoding: "utf8", shell: false, stdio: "inherit", windowsHide: true },
);
if (result.error) {
  throw new Error(`unable to start tar: ${result.error.message}`);
}
if (result.status !== 0) {
  throw new Error(`tar exited with status ${result.status}`);
}

const bytes = readFileSync(archive);
const sha256 = createHash("sha256").update(bytes).digest("hex");
const checksum = `${archive}.sha256`;
writeFileSync(checksum, `${sha256}  ${basename(archive)}\n`, "utf8");

const archivePath = relative(root, archive).replaceAll("\\", "/");
const checksumPath = relative(root, checksum).replaceAll("\\", "/");
if (process.env.GITHUB_OUTPUT) {
  appendFileSync(
    process.env.GITHUB_OUTPUT,
    `archive=${archivePath}\nchecksum=${checksumPath}\nsha256=${sha256}\nbytes=${statSync(archive).size}\n`,
    "utf8",
  );
}

process.stdout.write(
  `Packaged ${archivePath} (${statSync(archive).size} bytes, SHA256 ${sha256})\n`,
);
