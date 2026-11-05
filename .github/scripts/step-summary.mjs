#!/usr/bin/env node

import { createHash } from "node:crypto";
import {
  appendFileSync,
  existsSync,
  readFileSync,
  readdirSync,
  statSync,
} from "node:fs";
import { basename, dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { loadProjectEnv } from "../../Tools/Build/env.mjs";
import { PRODUCT_METADATA } from "../../Tools/Build/product-metadata.mjs";
import { repositoryWebUrl, resolveRepository } from "./repository-context.mjs";
import { TARGETS } from "./codebase-summary.mjs";
import { findDistributionArtifact, distributionDownloadLabel, findDistributionChecksum, distributionArchivePattern } from "./artifact-downloads.mjs";

loadProjectEnv();

import { usageProgress as progress } from "./usage-progress.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const productName = PRODUCT_METADATA.productName;
const [kind, ...arguments_] = process.argv.slice(2);

const escape = (value) => String(value ?? "").replaceAll("|", "\\|");
const code = (value) => `\`${String(value ?? "").replaceAll("`", "\\`")}\``;
const absolute = (path) => resolve(root, path);
const hashFile = (path) =>
  createHash("sha256").update(readFileSync(path)).digest("hex");
const number = (value) => new Intl.NumberFormat("en-US").format(Number(value || 0));
const bytes = (value) => {
  const size = Number(value || 0);
  if (size < 1024) return `${number(size)} B`;
  if (size < 1024 ** 2) return `${(size / 1024).toFixed(1)} KiB`;
  return `${(size / 1024 ** 2).toFixed(2)} MiB`;
};
const percent = (used, total) =>
  total ? Math.round((Number(used) / Number(total)) * 10000) / 100 : 0;
const bar = (value, width = 50) => {
  const bounded = Math.min(100, Math.max(0, Number(value || 0)));
  const filled = Math.round((bounded / 100) * width);
  return `${"█".repeat(filled)}${"░".repeat(width - filled)} ${bounded.toFixed(2)}%`;
};
const directoryBytes = (path) => {
  if (!existsSync(path)) return 0;
  const entry = statSync(path);
  if (entry.isFile()) return entry.size;
  return readdirSync(path, { withFileTypes: true }).reduce(
    (total, item) => total + directoryBytes(resolve(path, item.name)),
    0,
  );
};
const archiveDetails = (path) => {
  const resolved = absolute(path);
  return {
    name: basename(resolved),
    bytes: statSync(resolved).size,
    sha256: hashFile(resolved),
  };
};
const repository = resolveRepository(process.env, { cwd: root });
const repositoryUrl = repositoryWebUrl(repository, process.env);
const runUrl = process.env.GITHUB_RUN_ID
  ? `${repositoryUrl}/actions/runs/${process.env.GITHUB_RUN_ID}`
  : repositoryUrl;
const commit = process.env.GITHUB_SHA || "local-build";
const commitUrl = commit === "local-build" ? repositoryUrl : `${repositoryUrl}/commit/${commit}`;
const shortCommit = commit.slice(0, 12);
const workflowName = process.env.GITHUB_WORKFLOW || "Local build";
const releaseBuild =
  /release/iu.test(workflowName) || process.env.PCCONTROLLER_ATTESTED === "1";
const retention = process.env.GITHUB_EVENT_NAME === "pull_request" ? "14 days" : "90 days";
const artifactCta = (url, label) =>
  url
    ? `> [**⬇️ Download ${escape(label)}**](${url})
>
> Sign in to GitHub to download. Available for ${retention}.`
    : `> **${escape(label)}** — upload metadata is unavailable outside GitHub Actions.`;
const downloadLink = (url, label = "Download .tar.gz") => url
  ? `[⬇️ ${escape(label)}](${url})`
  : "⚠️ Download unavailable";
const integrityTable = (archive, digest) => `| Checksum | SHA-256 |
|---|---|
| Package archive | ${code(archive.sha256)} |
| Actions artifact | ${digest ? code(digest) : "—"} |`;
const emit = (markdown) => {
  const text = `${markdown.trim()}\n`;
  if (process.env.GITHUB_STEP_SUMMARY) {
    appendFileSync(process.env.GITHUB_STEP_SUMMARY, text, "utf8");
  } else {
    process.stdout.write(text);
  }
};
const readJson = (path) => JSON.parse(readFileSync(absolute(path), "utf8"));
const friendlyRole = (role) =>
  ({
    application: "Application image",
    eeprom: "EEPROM image",
    "default-eeprom": "Safe default EEPROM (1 KiB)",
    "flash+bootloader": "Full flash + Urboot (ISP recovery only)",
  })[role] || role;
const provenanceCommand = (archiveName) =>
  releaseBuild
    ? `gh attestation verify ${archiveName} --repo ${repository}`
    : "# GitHub provenance is attached only when this package is promoted by the Release workflow.";
const verificationBlock = (target, archiveName) => {
  const provenance = provenanceCommand(archiveName);
  if (/windows/iu.test(target)) {
    return `~~~powershell
$expected = (Get-Content ./${archiveName}.sha256 -Raw).Split()[0]
$actual = (Get-FileHash ./${archiveName} -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected.ToLowerInvariant()) { throw "SHA-256 mismatch" }
${provenance}
~~~`;
  }
  const checksum = /macos/iu.test(target)
    ? `shasum -a 256 -c ${archiveName}.sha256`
    : `sha256sum -c ${archiveName}.sha256`;
  return `~~~bash
${checksum}
${provenance}
~~~`;
};

if (kind === "firmware") {
  const [
    manifestPath,
    archivePath,
    artifactUrl = "",
    artifactDigest = "",
    dependenciesPath = "Tools/Controller/toolchain-lock.json",
    packageRootPath = ".build/firmware",
    checksumUrl = "",
  ] = arguments_;
  const manifest = readJson(manifestPath);
  const archive = archiveDetails(archivePath);
  const application = manifest.artifacts?.find((item) => item.role === "application");
  const stack = manifest.stackBudget || {};
  const staticPercent = percent(stack.staticSramBytes, stack.sramCapacityBytes);
  const peakPercent = percent(stack.estimatedPeakSramBytes, stack.sramCapacityBytes);
  const flashPercent = Number(application?.usagePercent || 0);
  const rawBytes = directoryBytes(absolute(packageRootPath));
  const compressedPercent = rawBytes ? percent(archive.bytes, rawBytes) : 0;
  const dependencies = existsSync(absolute(dependenciesPath))
    ? readJson(dependenciesPath)
    : {};
  const dependencyLibraries = Array.isArray(dependencies.libraries)
    ? dependencies.libraries
    : Object.entries(dependencies.libraries || {}).map(([name, version]) => ({
        name,
        version,
        url: name === "rc-switch" ? "https://github.com/sui77/rc-switch" : repositoryUrl,
      }));
  const libraries = dependencyLibraries
    .map((item) => `| [${escape(item.name)}](${item.url || repositoryUrl}) | ${code(item.version)} | pinned and resolved during this build |`)
    .join("\n");
  const artifacts = (manifest.artifacts || [])
    .map(
      (artifact) =>
        `| ${escape(friendlyRole(artifact.role))} | ${code(basename(artifact.path))} | ${number(artifact.dataBytes)} / ${number(artifact.capacityBytes)} | ${Number(artifact.usagePercent).toFixed(2)}% | ${number(artifact.freeBytes)} B | ${code(artifact.sha256)} |`,
    )
    .join("\n");
  const staticSections = (stack.staticSections || [])
    .map((item) => `| ${code(item.name)} | ${number(item.bytes)} B |`)
    .join("\n");
  const stackPath = (stack.serialPath || [])
    .map((item) => `| ${escape(item.name)} | ${code(item.function)} | ${number(item.bytes)} B | ${escape(item.qualifier)} |`)
    .join("\n");
  const headroom = Number(application?.freeBytes || 0);
  const flashNotice = headroom < 256
    ? `> [!WARNING]\n> **${number(headroom)} application-flash bytes free.**`
    : `> [!NOTE]\n> ${number(headroom)} application-flash bytes remain.`;

  emit(`
# ✅ ${escape(productName)} firmware built

ATmega328P firmware passed compilation, Intel HEX, flash, SRAM/stack, package, and SHA-256 validation.

${artifactCta(artifactUrl, `${productName} Firmware · ATmega328P`)}

## 📦 Build

| Property | Value |
|---|---|
| Sketch | ${code("PCController.ino")} |
| Target | ${code(manifest.target?.mcu || "atmega328p")} · ${number(manifest.target?.clockHz)} Hz |
| Bootloader | ${escape(manifest.target?.bootloader)} · ${number(manifest.target?.baud)} baud |
| Version | ${code(process.env.PCCONTROLLER_VERSION || "development")} |
| Source | [${code(shortCommit)}](${commitUrl}) · ${number(manifest.source?.files)} files · ${code(manifest.source?.sha256)} |
| Workflow | [${escape(workflowName)}](${runUrl}) |
| Download | ${downloadLink(artifactUrl, "Firmware · ATmega328P")} |
| SHA-256 sidecar | ${checksumUrl ? `[🔐 Download checksum](${checksumUrl})` : "⚠️ Download unavailable"} |
| Package | ${code(archive.name)} · ${bytes(archive.bytes)} |

${flashNotice}

## 💾 Application flash

${progress(flashPercent, "Application flash")}

${code(bar(flashPercent))}

| Used | Capacity | Free | Result |
|---:|---:|---:|---|
| **${number(application?.dataBytes)} B** | ${number(application?.capacityBytes)} B | **${number(application?.freeBytes)} B** | ✅ Intel HEX check passed |

## 🧠 SRAM and stack headroom

| Budget | Visualization | Used | Free |
|---|---|---:|---:|
| Static SRAM | ${progress(staticPercent, "Static SRAM")} | ${number(stack.staticSramBytes)} / ${number(stack.sramCapacityBytes)} B (${staticPercent.toFixed(2)}%) | ${number(Number(stack.sramCapacityBytes || 0) - Number(stack.staticSramBytes || 0))} B |
| Conservative peak | ${progress(peakPercent, "Peak SRAM")} | ${number(stack.estimatedPeakSramBytes)} / ${number(stack.sramCapacityBytes)} B (${peakPercent.toFixed(2)}%) | **${number(stack.estimatedFreeSramBytes)} B** |

~~~text
Flash ${bar(flashPercent)}
SRAM  ${bar(peakPercent)}
~~~

<details>
<summary><strong>🔬 SRAM details</strong></summary>

The release gate reserves ${number(stack.rfInterruptAllowanceBytes)} B for the RF interrupt path and enforces at least ${number(stack.minimumFreeSramBytes)} B free at the modeled peak. Analyzer: ${code(stack.analyzer)}.

| Static section | Bytes |
|---|---:|
${staticSections}

| Active path stage | Final linked function | Frame | Evidence |
|---|---|---:|---|
${stackPath}

</details>

<details>
<summary><strong>⚙️ Build configuration</strong></summary>

| Setting | Resolved value |
|---|---|
| FQBN | ${code(manifest.target?.fqbn)} |
| MCU | ${code(manifest.target?.mcu)} |
| Clock | ${number(manifest.target?.clockHz)} Hz external |
| Brown-out detection | 2.7 V |
| EEPROM policy | keep |
| LTO | optimized for size |
| Build hash | ${code(manifest.source?.buildHash)} |
| Packed timestamp | ${code(manifest.source?.packedTimestamp)} |

</details>

<details>
<summary><strong>📚 Dependencies</strong></summary>

| Dependency | Version | Resolution |
|---|---:|---|
| [Arduino CLI](https://github.com/arduino/arduino-cli/releases) | ${code(dependencies.arduinoCli?.version || "recorded in artifact")} | official archive, pinned SHA-256 |
| [MiniCore](https://github.com/MCUdude/MiniCore) | ${code(dependencies.miniCore?.version || "recorded in artifact")} | canonical package index |
${libraries}

</details>

## 📊 Firmware images

| Role | File | Data / capacity | Usage | Free | SHA-256 |
|---|---|---:|---:|---:|---|
${artifacts}

## 🗜️ Package size

${progress(compressedPercent, "Archive size versus staged files")}

| Archive | Download | Compressed | Unpacked | Ratio |
|---|---|---:|---:|---:|
| ${code(archive.name)} | ${downloadLink(artifactUrl)} | ${bytes(archive.bytes)} | ${bytes(rawBytes)} | ${compressedPercent.toFixed(2)}% |

<details>
<summary><strong>🔐 Checksums and verification</strong></summary>

${integrityTable(archive, artifactDigest)}

${verificationBlock("Linux", archive.name)}

</details>

> Urclock uses the application image. ISP recovery uses the full-flash image. Hardware validation is separate.

---

Built by [${escape(productName)} Actions](${runUrl}) · source [${shortCommit}](${commitUrl}) · [CI/CD guide](${repositoryUrl}/blob/${commit}/docs/CI-CD-and-Releases.md)
`);
} else if (kind === "host") {
  const [manifestPath, archivePath, target, artifactUrl = "", artifactDigest = ""] = arguments_;
  const manifest = readJson(manifestPath);
  const archive = archiveDetails(archivePath);
  const artifacts = (manifest.artifacts || [])
    .map((artifact) => `| ${code(artifact.path)} | ${bytes(artifact.bytes)} | ${code(artifact.sha256)} |`)
    .join("\n");
  const defaults = manifest.validation?.embeddedDefaults || {};
  const firmwareDefault = defaults.firmwareEnabled === true
    ? `✅ enabled · ${code(defaults.firmwareSHA256)}`
    : "❌ disabled";
  const eepromDefault = defaults.eepromEnabled === true
    ? `✅ enabled · ${number(defaults.eepromDataBytes)} B · ${code(defaults.eepromSHA256)}`
    : "❌ disabled";
  emit(`
# ✅ ${escape(productName)} Host built

Tests, Go vet, identity, and C ABI checks passed for **${escape(target)}**.

${artifactCta(artifactUrl, `Host · ${target}`)}

## 🖥️ Package information

| Property | Value |
|---|---|
| Platform | **${escape(target)}** |
| Version | ${code(manifest.identity?.version)} |
| Native target | ${code(`${manifest.target?.platform}/${manifest.target?.architecture}`)} |
| Source | [${code(shortCommit)}](${commitUrl}) · tree ${code(manifest.identity?.sourceSHA256)} |
| Workflow | [${escape(workflowName)}](${runUrl}) |
| Download | ${downloadLink(artifactUrl, `Host · ${target}`)} |
| Package | ${code(archive.name)} · ${bytes(archive.bytes)} |

## 🧪 Validation matrix

| Gate | Result |
|---|---|
| Go tests | ✅ ${escape(manifest.validation?.tests)} |
| Go vet | ✅ ${escape(manifest.validation?.vet)} |
| C ABI shared library | ✅ ${escape(manifest.validation?.sharedLibrary)} |
| Native resources | ✅ ${escape(manifest.validation?.windowsResources || "not applicable")} |
| Embedded firmware default | ${firmwareDefault} |
| Embedded EEPROM default | ${eepromDefault} |

<details>
<summary><strong>📦 Packaged files and hashes</strong></summary>

| File | Size | SHA-256 |
|---|---:|---|
${artifacts}

</details>

<details>
<summary><strong>🔐 Checksums and verification</strong></summary>

${integrityTable(archive, artifactDigest)}

${verificationBlock(target, archive.name)}

</details>

---

[Download](${artifactUrl || runUrl}) · [source ${shortCommit}](${commitUrl}) · [Host guide](${repositoryUrl}/blob/${commit}/Tools/Controller/README.md)
`);
} else if (kind === "simulator") {
  const [target, binaryPath, archivePath, artifactUrl = "", artifactDigest = ""] = arguments_;
  const binary = absolute(binaryPath);
  const archive = archiveDetails(archivePath);
  emit(`
# ✅ ${escape(productName)} Virtual Board built

Protocol, hardware-model, UART, and smoke tests passed for **${escape(target)}**.

${artifactCta(artifactUrl, `Virtual Board · ${target}`)}

## 🧪 Simulator package

| Property | Value |
|---|---|
| Target | **${escape(target)}** |
| Binary | ${code(basename(binary))} · ${bytes(statSync(binary).size)} |
| Binary SHA-256 | ${code(hashFile(binary))} |
| Download | ${downloadLink(artifactUrl, `Virtual Board · ${target}`)} |
| Package | ${code(archive.name)} · ${bytes(archive.bytes)} |
| Native CTest | ✅ passed |
| Source | [${code(shortCommit)}](${commitUrl}) |
| Workflow | [${escape(workflowName)}](${runUrl}) |

<details>
<summary><strong>🔐 Checksums and verification</strong></summary>

${integrityTable(archive, artifactDigest)}

${verificationBlock(target, archive.name)}

</details>

---

[Download](${artifactUrl || runUrl}) · [source ${shortCommit}](${commitUrl}) · [Virtual Board guide](${repositoryUrl}/blob/${commit}/Tools/VirtualBoard/README.md)
`);
} else if (kind === "catalog") {
  const [directory] = arguments_;
  const base = absolute(directory);
  const files = [];
  let firmwareManifestPath = "";
  const visit = (path) => {
    for (const entry of readdirSync(path, { withFileTypes: true })) {
      const child = resolve(path, entry.name);
      if (entry.isDirectory()) visit(child);
      else {
        if (/\.(?:tar\.gz|zip)$/u.test(entry.name)) files.push(child);
        if (entry.name === "firmware-manifest.json") firmwareManifestPath = child;
      }
    }
  };
  visit(base);
  files.sort();
  const indexPath = resolve(base, "artifact-index.json");
  const artifactIndex = existsSync(indexPath) ? readJson(indexPath) : { artifacts: [] };
  const uploadedArtifacts = artifactIndex.artifacts || [];
  const availableUrl = (artifact) => artifact?.id && !artifact.expired
    ? `${runUrl}/artifacts/${artifact.id}` : "";
  const downloadFor = (name) => {
    const artifact = findDistributionArtifact(uploadedArtifacts, name);
    const checksum = findDistributionChecksum(uploadedArtifacts, artifact);
    return downloadLink(availableUrl(artifact), distributionDownloadLabel(artifact)) +
      (checksum && availableUrl(artifact) ? ` · [🔐 SHA-256](${availableUrl(checksum)})` : "");
  };
  const expectedArtifacts = ["PCController-Firmware-ATmega328P", ...TARGETS.flatMap(({id}) =>
    [`PCController-Host-${id}`, `PCController-VirtualBoard-${id}`])];
  const missingArtifacts = expectedArtifacts.filter(name => {
    const artifact = findDistributionArtifact(uploadedArtifacts, name);
    return !availableUrl(artifact) || (/\.(?:tar\.gz|zip)$/u.test(artifact.name) &&
      !availableUrl(findDistributionChecksum(uploadedArtifacts, artifact)));
  });
  const complete = missingArtifacts.length === 0 && files.length === expectedArtifacts.length;
  const artifactForPackage = (path) => {
    const fileName = basename(path);
    const raw = uploadedArtifacts.find(item => item.name === fileName);
    if (raw) return raw;
    if (/PCController-Firmware-/u.test(fileName)) {
      return uploadedArtifacts.find(
        (item) => item.name === "PCController-Firmware-ATmega328P",
      );
    }
    for (const product of ["Host", "VirtualBoard"]) {
      const prefix = `PCController-${product}-`;
      const match = uploadedArtifacts.find(
        (item) =>
          item.name.startsWith(prefix) &&
          distributionArchivePattern(`PCController-${product}`, item.name.slice(prefix.length)).test(fileName),
      );
      if (match) return match;
    }
    return undefined;
  };
  const rows = files
    .map((path) => {
      const uploaded = artifactForPackage(path);
      return `| ${code(basename(path))} | ${downloadLink(availableUrl(uploaded), distributionDownloadLabel(uploaded))} | ${bytes(statSync(path).size)} | ${code(hashFile(path))} |`;
    })
    .join("\n");
  const firmwareManifest = firmwareManifestPath
    ? JSON.parse(readFileSync(firmwareManifestPath, "utf8"))
    : {};
  const application = firmwareManifest.artifacts?.find(
    (item) => item.role === "application",
  );
  const stack = firmwareManifest.stackBudget || {};
  const flashPercent = Number(application?.usagePercent || 0);
  const peakPercent = percent(
    stack.estimatedPeakSramBytes,
    stack.sramCapacityBytes,
  );
  emit(`
# ${complete ? "✅" : "⚠️"} ${escape(productName)} ${complete ? "build complete" : "build incomplete"}

Available packages for source [${code(shortCommit)}](${commitUrl}).
${complete ? "" : "\n> [!WARNING]\n> Some packages were not produced or are unavailable. Check the failed/skipped jobs; missing downloads are not successful builds.\n"}

> [**⬇️ Open all ${files.length} downloadable packages**](${runUrl}#artifacts)

## ⬇️ Download packages

Sign in to GitHub and download a package plus its SHA-256 sidecar below.
Verify the checksum, then extract the package once. Firmware HEX/EEPROM files
are inside the firmware package. Older ZIP-labeled runs retain their original layout.

| Platform / board | 🖥️ Host application | 🧪 Virtual Board | ⚡ Firmware |
|---|---|---|---|
| **Windows x64** | ${downloadFor("PCController-Host-Windows-x64")} | ${downloadFor("PCController-VirtualBoard-Windows-x64")} | — |
| Linux x64 | ${downloadFor("PCController-Host-Linux-x64")} | ${downloadFor("PCController-VirtualBoard-Linux-x64")} | — |
| Linux ARM64 | ${downloadFor("PCController-Host-Linux-ARM64")} | ${downloadFor("PCController-VirtualBoard-Linux-ARM64")} | — |
| macOS Intel | ${downloadFor("PCController-Host-macOS-Intel")} | ${downloadFor("PCController-VirtualBoard-macOS-Intel")} | — |
| macOS Apple Silicon | ${downloadFor("PCController-Host-macOS-Apple-Silicon")} | ${downloadFor("PCController-VirtualBoard-macOS-Apple-Silicon")} | — |
| **AVR ATmega328P** | — | — | ${downloadFor("PCController-Firmware-ATmega328P")} |

## ⚡ AVR ATmega328P target

${firmwareManifestPath ? `
| Budget | Visual | Used | Free |
|---|---|---:|---:|
| Application flash | ${progress(flashPercent, "Application flash")} | **${number(application?.dataBytes)} / ${number(application?.capacityBytes)} B** (${flashPercent.toFixed(2)}%) | **${number(application?.freeBytes)} B** |
| Conservative peak SRAM | ${progress(peakPercent, "Peak SRAM")} | **${number(stack.estimatedPeakSramBytes)} / ${number(stack.sramCapacityBytes)} B** (${peakPercent.toFixed(2)}%) | **${number(stack.estimatedFreeSramBytes)} B** |

~~~text
Flash ${bar(flashPercent)}
SRAM  ${bar(peakPercent)}
~~~
` : "⚠️ Firmware memory report unavailable for this run."}

Firmware: ${code("PCController-Firmware-ATmega328P")}. Includes application and recovery images, dependencies, and manifest.

<details>
<summary><strong>🔐 Package sizes and SHA-256 checksums</strong></summary>

| Package | Download | Size | SHA-256 |
|---|---|---:|---|
${rows}

</details>

## 🛡️ Validation

${complete ? `
- ✅ MiniCore ATmega328P compilation and strict Intel HEX validation
- ✅ Static and modeled peak-SRAM enforcement with stack-path evidence
- ✅ Native Go tests, vet, C ABI smoke checks, and cross-platform packaging
- ✅ Native CMake/CTest Virtual Board validation across supported targets
- ✅ SHA-256 sidecars, canonical manifests, and direct Actions downloads
` : "See the individual job summaries for validation results. Available artifacts do not imply that the complete build passed."}

Release runs add attestations, a release manifest, firmware images, and a download chooser.

---

[Run details](${runUrl}) · [source ${shortCommit}](${commitUrl}) · [release guide](${repositoryUrl}/blob/${commit}/docs/CI-CD-and-Releases.md)
`);
  if (!complete) process.exitCode = 1;
} else {
  process.stderr.write("Usage: step-summary.mjs firmware|host|simulator|catalog ...\n");
  process.exit(2);
}
