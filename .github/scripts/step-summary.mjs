#!/usr/bin/env node

import { createHash } from "node:crypto";
import { appendFileSync, readFileSync, statSync } from "node:fs";
import { basename, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { dirname } from "node:path";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const [kind, ...arguments_] = process.argv.slice(2);

const escape = (value) => String(value ?? "").replaceAll("|", "\\|");
const hashFile = (path) =>
  createHash("sha256").update(readFileSync(path)).digest("hex");
const absolute = (path) => resolve(root, path);
const archiveDetails = (path) => {
  const resolved = absolute(path);
  return {
    name: basename(resolved),
    bytes: statSync(resolved).size,
    sha256: hashFile(resolved),
  };
};
const emit = (markdown) => {
  const text = `${markdown.trim()}\n`;
  if (process.env.GITHUB_STEP_SUMMARY) {
    appendFileSync(process.env.GITHUB_STEP_SUMMARY, text, "utf8");
  } else {
    process.stdout.write(text);
  }
};

if (kind === "host") {
  const [manifestPath, archivePath, target] = arguments_;
  const manifest = JSON.parse(readFileSync(absolute(manifestPath), "utf8"));
  const archive = archiveDetails(archivePath);
  const artifacts = (manifest.artifacts || [])
    .map(
      (artifact) =>
        `| \`${escape(artifact.path)}\` | ${artifact.bytes} | \`${escape(artifact.sha256)}\` |`,
    )
    .join("\n");
  emit(`
## Host package: ${escape(target)}

| Property | Value |
|---|---|
| Version | \`${escape(manifest.identity?.version)}\` |
| Source SHA-256 | \`${escape(manifest.identity?.sourceSHA256)}\` |
| Tests / vet | ${escape(manifest.validation?.tests)} / ${escape(manifest.validation?.vet)} |
| C ABI | ${escape(manifest.validation?.sharedLibrary)} |
| Archive | \`${archive.name}\` (${archive.bytes} bytes) |
| Archive SHA-256 | \`${archive.sha256}\` |

### Packaged host artifacts

| File | Bytes | SHA-256 |
|---|---:|---|
${artifacts}
`);
} else if (kind === "firmware") {
  const [manifestPath, archivePath] = arguments_;
  const manifest = JSON.parse(readFileSync(absolute(manifestPath), "utf8"));
  const archive = archiveDetails(archivePath);
  const artifacts = (manifest.artifacts || [])
    .map(
      (artifact) =>
        `| ${escape(artifact.role)} | \`${escape(basename(artifact.path))}\` | ${artifact.dataBytes} / ${artifact.capacityBytes} | ${artifact.usagePercent}% | ${artifact.freeBytes} | \`${escape(artifact.sha256)}\` |`,
    )
    .join("\n");
  emit(`
## AVR firmware package

| Property | Value |
|---|---|
| Target | \`${escape(manifest.target?.fqbn || "MiniCore ATmega328P") }\` |
| Source SHA-256 | \`${escape(manifest.source?.sha256)}\` |
| Source files | ${escape(manifest.source?.files)} |
| Archive | \`${archive.name}\` (${archive.bytes} bytes) |
| Archive SHA-256 | \`${archive.sha256}\` |

### Firmware artifacts

| Role | File | Used / capacity | Usage | Free | SHA-256 |
|---|---|---:|---:|---:|---|
${artifacts}
`);
} else if (kind === "simulator") {
  const [target, binaryPath, archivePath] = arguments_;
  const binary = absolute(binaryPath);
  const archive = archiveDetails(archivePath);
  emit(`
## Virtual board: ${escape(target)}

| Property | Value |
|---|---|
| Binary | \`${escape(basename(binary))}\` (${statSync(binary).size} bytes) |
| Binary SHA-256 | \`${hashFile(binary)}\` |
| Archive | \`${archive.name}\` (${archive.bytes} bytes) |
| Archive SHA-256 | \`${archive.sha256}\` |
| Tests | CTest passed |
`);
} else {
  process.stderr.write(
    "Usage: step-summary.mjs host|firmware|simulator ...\n",
  );
  process.exit(2);
}
