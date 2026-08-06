#!/usr/bin/env node

import { createHash } from "node:crypto";
import {
  appendFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { basename, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { PRODUCT_METADATA } from "../../Tools/Build/product-metadata.mjs";

import { usageProgress } from "./usage-progress.mjs";

const FORMAT = "pccontroller-release-manifest/v1";
const PRODUCT_NAME = PRODUCT_METADATA.productName;
const GENERATED_FILES = new Set([
  "RELEASE-NOTES.md",
  "release-manifest.json",
]);

const TARGETS = [
  {
    id: "linux-x64",
    label: "Linux x64",
    patterns: [/linux[-_. ]*(?:x64|amd64)/u],
  },
  {
    id: "linux-arm64",
    label: "Linux ARM64",
    patterns: [/linux[-_. ]*(?:arm64|aarch64)/u],
  },
  {
    id: "windows-x64",
    label: "Windows x64",
    patterns: [/(?:windows|win32)[-_. ]*(?:x64|amd64)/u],
  },
  {
    id: "macos-intel",
    label: "macOS Intel",
    patterns: [/(?:macos|darwin)[-_. ]*(?:intel|x64|amd64)/u],
  },
  {
    id: "macos-apple-silicon",
    label: "macOS Apple Silicon",
    patterns: [
      /(?:macos|darwin)[-_. ]*(?:apple[-_. ]*silicon|arm64|aarch64)/u,
    ],
  },
];

const sha256 = (content) => createHash("sha256").update(content).digest("hex");
const escapeMarkdown = (value) =>
  String(value ?? "").replaceAll("|", "\\|").replaceAll("\n", " ");
const code = (value) => `\`${String(value).replaceAll("`", "\\`")}\``;
const humanBytes = (value) => {
  if (value < 1024) return `${value} B`;
  const units = ["KiB", "MiB", "GiB"];
  let amount = value;
  let index = -1;
  do {
    amount /= 1024;
    index += 1;
  } while (amount >= 1024 && index < units.length - 1);
  return `${amount.toFixed(amount >= 10 ? 1 : 2)} ${units[index]}`;
};

function mediaType(name) {
  const lower = name.toLowerCase();
  if (lower.endsWith(".tar.gz")) return "application/gzip";
  if (lower.endsWith(".json")) return "application/json";
  if (lower.endsWith(".md")) return "text/markdown";
  if (lower.endsWith(".txt") || lower.endsWith(".sha256")) return "text/plain";
  if (lower.endsWith(".hex") || lower.endsWith(".eep")) return "text/plain";
  return "application/octet-stream";
}

function classifyTarget(name) {
  const lower = name.toLowerCase();
  return TARGETS.find((target) =>
    target.patterns.some((pattern) => pattern.test(lower)),
  );
}

function classifyComponent(name) {
  const lower = name.toLowerCase();
  if (/virtual[-_. ]*board|simulator/u.test(lower)) return "virtual-board";
  if (/firmware|atmega328|avr/u.test(lower) || /\.(?:hex|eep)$/u.test(lower)) {
    return "firmware";
  }
  if (/(?:^|[-_. ])(?:host|controller)(?:[-_. ]|$)/u.test(lower)) {
    return "controller";
  }
  return "support";
}

function classifyRole(name, component) {
  const lower = name.toLowerCase();
  if (lower.endsWith(".tar.gz.sha256")) return "archive-checksum";
  if (lower.endsWith(".sha256")) return "checksum";
  if (lower === "sha256sums.txt") return "checksum-manifest";
  if (lower.endsWith(".tar.gz")) return `${component}-archive`;
  if (lower.endsWith(".eep")) return "eeprom-image";
  if (lower.endsWith(".hex")) {
    if (
      /with[-_. ]*bootloader|flash[-+_. ]*bootloader|full[-_. ]*flash/u.test(
        lower,
      )
    ) {
      return "flash-with-bootloader-image";
    }
    return "application-image";
  }
  if (lower.includes("firmware-manifest")) return "firmware-build-manifest";
  if (lower.includes("host-manifest")) return "controller-build-manifest";
  if (lower.includes("dependenc")) return "dependency-inventory";
  if (lower.endsWith(".json")) return "metadata";
  if (lower.endsWith(".md") || lower.endsWith(".txt")) return "documentation";
  return "support-file";
}

function inspectAssets(directory) {
  return readdirSync(directory, { withFileTypes: true })
    .filter((entry) => entry.isFile() && !GENERATED_FILES.has(entry.name))
    .map((entry) => {
      const path = resolve(directory, entry.name);
      const content = readFileSync(path);
      const component = classifyComponent(entry.name);
      const target = classifyTarget(entry.name);
      return {
        file: entry.name,
        role: classifyRole(entry.name, component),
        component,
        ...(target ? { target: target.id } : {}),
        mediaType: mediaType(entry.name),
        bytes: content.length,
        sha256: sha256(content),
      };
    })
    .sort((left, right) => left.file.localeCompare(right.file, "en"));
}

function parseChecksumLine(line, source) {
  const match = line.match(/^([0-9a-fA-F]{64})\s+[*]?(.+?)\s*$/u);
  if (!match) throw new Error(`invalid SHA-256 entry in ${source}: ${line}`);
  return { sha256: match[1].toLowerCase(), file: basename(match[2]) };
}

function validateChecksums(directory, assets) {
  const assetByName = new Map(assets.map((asset) => [asset.file, asset]));
  const validated = [];
  for (const asset of assets) {
    if (asset.role !== "archive-checksum" && asset.role !== "checksum") continue;
    const lines = readFileSync(resolve(directory, asset.file), "utf8")
      .split(/\r?\n/u)
      .filter((line) => line.trim());
    if (lines.length !== 1) {
      throw new Error(`${asset.file} must contain exactly one SHA-256 entry`);
    }
    const entry = parseChecksumLine(lines[0], asset.file);
    const target = assetByName.get(entry.file);
    if (!target) {
      throw new Error(`${asset.file} refers to missing asset ${entry.file}`);
    }
    if (target.sha256 !== entry.sha256) {
      throw new Error(`${asset.file} does not match ${entry.file}`);
    }
    validated.push({ checksum: asset.file, asset: target.file });
  }

  const combined = assetByName.get("SHA256SUMS.txt");
  if (!combined) throw new Error("release assets must include SHA256SUMS.txt");
  const entries = readFileSync(resolve(directory, combined.file), "utf8")
    .split(/\r?\n/u)
    .filter((line) => line.trim())
    .map((line) => parseChecksumLine(line, combined.file));
  if (entries.length === 0) throw new Error("SHA256SUMS.txt is empty");
  const combinedNames = new Set();
  for (const entry of entries) {
    if (combinedNames.has(entry.file)) {
      throw new Error(`SHA256SUMS.txt contains duplicate asset ${entry.file}`);
    }
    combinedNames.add(entry.file);
    const target = assetByName.get(entry.file);
    if (!target) {
      throw new Error(`SHA256SUMS.txt refers to missing asset ${entry.file}`);
    }
    if (target.sha256 !== entry.sha256) {
      throw new Error(`SHA256SUMS.txt does not match ${entry.file}`);
    }
  }
  const primaryPayloads = assets.filter(
    (asset) =>
      asset.role.endsWith("-archive") ||
      asset.role === "application-image" ||
      asset.role === "flash-with-bootloader-image" ||
      asset.role === "eeprom-image",
  );
  for (const asset of primaryPayloads) {
    if (!combinedNames.has(asset.file)) {
      throw new Error(`SHA256SUMS.txt is missing primary asset ${asset.file}`);
    }
  }
  return validated;
}

function uniqueAsset(assets, predicate, description) {
  const matches = assets.filter(predicate);
  if (matches.length !== 1) {
    throw new Error(
      `expected exactly one ${description}; found ${matches.length}`,
    );
  }
  return matches[0];
}

function selectPackages(assets) {
  const firmwareArchive = uniqueAsset(
    assets,
    (asset) => asset.role === "firmware-archive",
    "AVR firmware archive",
  );
  const applicationImage = uniqueAsset(
    assets,
    (asset) => asset.role === "application-image",
    "direct AVR application image",
  );
  const bootloaderImage = uniqueAsset(
    assets,
    (asset) => asset.role === "flash-with-bootloader-image",
    "direct AVR flash-with-bootloader image",
  );

  const platforms = TARGETS.map((target) => ({
    id: target.id,
    label: target.label,
    controller: uniqueAsset(
      assets,
      (asset) =>
        asset.role === "controller-archive" && asset.target === target.id,
      `${target.label} Host archive`,
    ),
    virtualBoard: uniqueAsset(
      assets,
      (asset) =>
        asset.role === "virtual-board-archive" && asset.target === target.id,
      `${target.label} Virtual Board archive`,
    ),
  }));

  return {
    firmware: {
      archive: firmwareArchive,
      application: applicationImage,
      flashWithBootloader: bootloaderImage,
      eeprom: assets.find((asset) => asset.role === "eeprom-image") ?? null,
    },
    platforms,
  };
}

function loadBuildManifests(directory, assets) {
  const manifests = [];
  let firmware = null;
  for (const asset of assets.filter((candidate) =>
    candidate.role.endsWith("build-manifest"),
  )) {
    let document;
    try {
      document = JSON.parse(readFileSync(resolve(directory, asset.file), "utf8"));
    } catch (error) {
      throw new Error(`unable to decode ${asset.file}: ${error.message}`);
    }
    manifests.push({
      file: asset.file,
      format: document.format ?? "unknown",
      sha256: asset.sha256,
    });
    if (asset.role === "firmware-build-manifest") firmware = document;
  }
  return { manifests, firmware };
}

function validateFirmwareSelection(selection, firmwareManifest) {
  if (!firmwareManifest) {
    throw new Error("release assets must include the canonical firmware build manifest");
  }
  const expected = [
    ["application", selection.firmware.application],
    ["flash+bootloader", selection.firmware.flashWithBootloader],
  ];
  for (const [role, selected] of expected) {
    const records = (firmwareManifest.artifacts || []).filter(
      (artifact) => artifact.role === role,
    );
    if (records.length !== 1 || !/^[a-f0-9]{64}$/iu.test(records[0].sha256 || "")) {
      throw new Error(`firmware manifest must contain one hashed ${role} artifact`);
    }
    if (records[0].sha256.toLowerCase() !== selected.sha256) {
      throw new Error(
        `${selected.file} does not match the firmware manifest ${role} SHA-256`,
      );
    }
    if (Number(records[0].dataBytes || 0) <= 0) {
      throw new Error(`firmware manifest ${role} image is empty`);
    }
  }
}

function repositoryContext(environment, tag, sourceSha) {
  const repository = environment.GITHUB_REPOSITORY || "";
  const server = environment.GITHUB_SERVER_URL || "https://github.com";
  const repositoryUrl = repository ? `${server}/${repository}` : "";
  const runId = environment.GITHUB_RUN_ID || "";
  const draft = String(environment.PCCONTROLLER_RELEASE_DRAFT || "").toLowerCase() === "true";
  const explicitPrerelease = environment.PCCONTROLLER_RELEASE_PRERELEASE;
  const prerelease = explicitPrerelease == null || explicitPrerelease === ""
    ? tag.includes("-")
    : String(explicitPrerelease).toLowerCase() === "true";
  const runUrl = repositoryUrl && runId ? `${repositoryUrl}/actions/runs/${runId}` : "";
  return {
    repository,
    repositoryUrl,
    sourceUrl: repositoryUrl
      ? `${repositoryUrl}/commit/${encodeURIComponent(sourceSha)}`
      : "",
    runId,
    runAttempt: environment.GITHUB_RUN_ATTEMPT || "",
    runUrl,
    draft,
    prerelease,
    releaseUrl: draft
      ? runUrl || (repositoryUrl ? `${repositoryUrl}/releases` : "")
      : repositoryUrl
      ? `${repositoryUrl}/releases/tag/${encodeURIComponent(tag)}`
      : "",
    downloadBase: repositoryUrl && !draft
      ? `${repositoryUrl}/releases/download/${encodeURIComponent(tag)}`
      : "",
  };
}

function assetLink(asset, context) {
  const label = code(escapeMarkdown(asset.file));
  if (!context.downloadBase) return label;
  return `[${label}](${context.downloadBase}/${encodeURIComponent(asset.file)})`;
}

function sourceLink(sourceSha, context) {
  const short = sourceSha.slice(0, 12);
  return context.sourceUrl ? `[${code(short)}](${context.sourceUrl})` : code(short);
}

function runLink(context) {
  if (!context.runId) return "Local generation";
  const label = `Run ${context.runId}${
    context.runAttempt ? ` · attempt ${context.runAttempt}` : ""
  }`;
  return context.runUrl ? `[${label}](${context.runUrl})` : label;
}

function firmwareMetrics(document) {
  if (!document) return null;
  const application = document.artifacts?.find(
    (artifact) => artifact.role === "application",
  );
  const stack = document.stackBudget ?? document.stack_budget ?? null;
  return {
    target: document.target?.fqbn || document.target?.mcu || "MiniCore ATmega328P",
    flash: application
      ? {
          usedBytes: application.dataBytes,
          capacityBytes: application.capacityBytes,
          freeBytes: application.freeBytes,
          usagePercent: application.usagePercent,
        }
      : null,
    peakSram: stack
      ? {
          usedBytes:
            stack.estimatedPeakSramBytes ??
            stack.estimatedPeakSRAMBytes ??
            stack.estimated_peak_sram_bytes,
          capacityBytes:
            stack.sramCapacityBytes ?? stack.sramBytes ?? stack.sram_bytes ?? 2048,
          freeBytes:
            stack.estimatedFreeSramBytes ??
            stack.estimatedFreeSRAMBytes ??
            stack.estimated_free_sram_bytes,
        }
      : null,
  };
}

function validationRows(selection) {
  const rows = [
    {
      component: "AVR firmware",
      target: "ATmega328P / MiniCore",
      checks: "Compile · Intel HEX parse · manifest/hash validation",
      evidence: selection.firmware.archive.file,
    },
  ];
  for (const platform of selection.platforms) {
    rows.push({
      component: "Host",
      target: platform.label,
      checks: "Native runner build · Go test/vet · packaged identity",
      evidence: platform.controller.file,
    });
    rows.push({
      component: "Virtual Board",
      target: platform.label,
      checks: "Native CMake build · CTest · package",
      evidence: platform.virtualBoard.file,
    });
  }
  return rows;
}

function releaseChannel(tag, prerelease) {
  if (!prerelease) return "Release";
  const lower = tag.toLowerCase();
  if (lower.includes("alpha")) return "Alpha";
  if (lower.includes("beta")) return "Beta";
  if (/(?:^|[.-])rc(?:[.-]|\d|$)/u.test(lower)) return "Release candidate";
  return "Pre-release";
}

function releaseNotes({ tag, sourceSha, assets, selection, context, metrics }) {
  const prerelease = context.prerelease;
  const channel = releaseChannel(tag, prerelease);
  const totalBytes = assets.reduce((sum, asset) => sum + asset.bytes, 0);
  const platformRows = selection.platforms
    .map(
      (platform) =>
        `| ${platform.label} | ${assetLink(platform.controller, context)} | ${assetLink(platform.virtualBoard, context)} | — |`,
    )
    .join("\n");
  const eeprom = selection.firmware.eeprom
    ? `| EEPROM image | Preserves the compiled EEPROM payload; use only with an explicit EEPROM restore plan. | ${assetLink(selection.firmware.eeprom, context)} | ${humanBytes(selection.firmware.eeprom.bytes)} |`
    : "";
  const validations = validationRows(selection)
    .map(
      (row) =>
        `| ${row.component} | ${row.target} | ✅ ${row.checks} | ${code(row.evidence)} |`,
    )
    .join("\n");
  const inventory = assets
    .map(
      (asset) =>
        `| ${code(asset.file)} | ${escapeMarkdown(asset.role)} | ${humanBytes(asset.bytes)} | ${code(asset.sha256)} |`,
    )
    .join("\n");
  const firmwareEvidence = metrics?.flash
    ? `\n| Application flash | ${metrics.flash.usedBytes.toLocaleString("en-US")} / ${metrics.flash.capacityBytes.toLocaleString("en-US")} bytes (${metrics.flash.usagePercent}%; ${metrics.flash.freeBytes.toLocaleString("en-US")} free) |`
    : "";
  const peakSramEvidence = metrics?.peakSram?.usedBytes != null
    ? `\n| Estimated peak SRAM | ${metrics.peakSram.usedBytes.toLocaleString("en-US")} / ${metrics.peakSram.capacityBytes.toLocaleString("en-US")} bytes (${metrics.peakSram.freeBytes.toLocaleString("en-US")} free) |`
    : "";
  const repository = context.repository || "OWNER/REPOSITORY";

  const channelName = channel.toLowerCase();
  const channelArticle = /^[aeiou]/u.test(channelName) ? "an" : "a";
  const channelLimitation = prerelease
    ? `- This is ${channelArticle} ${channelName} build. Configuration and protocol compatibility can still change before a stable release.`
    : "- This release remains subject to the physical-hardware acceptance boundary above.";
  const draftNotice = context.draft
    ? `\n> [!TIP]\n> This is a GitHub draft, whose final tag/download URLs do not exist yet. The chooser therefore names each file without a broken link; use the release page's **Assets** section. Published reruns activate direct links.\n`
    : "";

  return `# 🚀 ${escapeMarkdown(PRODUCT_NAME)} ${escapeMarkdown(tag)} — ${channel}

> [!IMPORTANT]
> AVR firmware, Host, and Virtual Board packages were built for five OS/architecture targets before release assembly.
${draftNotice}

| Release | Value |
|---|---|
| Version | ${code(tag)} |
| Source commit | ${sourceLink(sourceSha, context)} |
| Build run | ${runLink(context)} |
| Staged release inputs | ${assets.length} files · ${humanBytes(totalBytes)} |${firmwareEvidence}${peakSramEvidence}

## 🌟 What ships

| Layer | Highlights |
|---|---|
| ⚡ AVR firmware | COBS/CRC UART, telemetry, 16 PWM outputs, guarded motion, learned 433 MHz actions, persistent settings, and MiniCore/Urboot memory checks |
| 🖥️ Host | Charm TUI and shell, REST/WebSocket/JSON-RPC, automation, programmer orchestration, and C ABI library |
| 🧪 Virtual Board | Protocol and behavior simulator for every host target |

## 🎯 Downloads

| Target | Host application | Virtual Board | AVR firmware |
|---|---|---|---|
| ATmega328P / MiniCore | — | — | ${assetLink(selection.firmware.application, context)} · ${assetLink(selection.firmware.flashWithBootloader, context)} · ${assetLink(selection.firmware.archive, context)} |
${platformRows}

**Host** operates the board; **Virtual Board** is the simulator.

## 🔥 AVR firmware

| Image role | Use it when | Download | Size |
|---|---|---|---:|
| Application only | A compatible Urboot bootloader is already installed; this is the normal firmware-update image. | ${assetLink(selection.firmware.application, context)} | ${humanBytes(selection.firmware.application.bytes)} |
| Flash + bootloader | A hardware programmer will replace the complete flash image, including the bootloader. | ${assetLink(selection.firmware.flashWithBootloader, context)} | ${humanBytes(selection.firmware.flashWithBootloader.bytes)} |
${eeprom}
| Firmware bundle | Images, dependencies, and build manifest. | ${assetLink(selection.firmware.archive, context)} | ${humanBytes(selection.firmware.archive.bytes)} |

> [!CAUTION]
> Back up flash and EEPROM before ISP or EEPROM writes. Normal updates use the application image.

## ✅ Validation matrix

| Component | Target | CI evidence | Package |
|---|---|---|---|
${validations}

CI does not validate a physical board.

## 🔐 Verify before running or flashing

Download ${code("SHA256SUMS.txt")} and the 13 primary payloads into one directory:

**Linux**

\`\`\`bash
sha256sum --check SHA256SUMS.txt
for archive in PCController-*.tar.gz; do
  gh attestation verify "$archive" --repo ${repository}
done
\`\`\`

**macOS**

\`\`\`bash
shasum -a 256 -c SHA256SUMS.txt
for archive in PCController-*.tar.gz; do
  gh attestation verify "$archive" --repo ${repository}
done
\`\`\`

**Windows PowerShell**

\`\`\`powershell
Get-Content .\\SHA256SUMS.txt | ForEach-Object {
  $expected, $file = $_ -split '\\s+', 2
  if ((Get-FileHash -LiteralPath $file.Trim() -Algorithm SHA256).Hash -ne $expected) {
    throw "SHA-256 mismatch: $file"
  }
}
Get-ChildItem -Filter 'PCController-*.tar.gz' | ForEach-Object {
  gh attestation verify $_.FullName --repo ${repository}
}
\`\`\`

All release assets are attested; ${code("SHA256SUMS.txt")} covers the 13 primary payloads.

<details>
<summary><strong>📦 Artifacts</strong></summary>

| Asset | Role | Size | SHA-256 |
|---|---|---:|---|
${inventory}

</details>

## 🧪 ${channel} limits

- Physical AVR upload and on-device behavior still require hardware validation with the intended board, programmer/serial adapter, fuse settings, and peripherals.
- The firmware target is the repository's pinned **ATmega328P + MiniCore/Urboot** profile; it is not a generic AVR image.
- Virtual Board tests protocol and control behavior, but it cannot reproduce every electrical, timing, sensor, or display characteristic of real hardware.
${channelLimitation}

---

Built from ${sourceLink(sourceSha, context)} · ${runLink(context)}
`;
}

function stepSummary({ tag, sourceSha, assets, selection, context, metrics }) {
  const totalBytes = assets.reduce((sum, asset) => sum + asset.bytes, 0);
  const chooser = selection.platforms
    .map(
      (platform) =>
        `| ${platform.label} | ${assetLink(platform.controller, context)} | ${assetLink(platform.virtualBoard, context)} |`,
    )
    .join("\n");
  const flashPercent = Number(metrics?.flash?.usagePercent ?? 0);
  const peakSramPercent = metrics?.peakSram?.capacityBytes
    ? (Number(metrics.peakSram.usedBytes || 0) /
        Number(metrics.peakSram.capacityBytes)) *
      100
    : 0;
  const sramBadge = metrics?.peakSram?.usedBytes != null
    ? `\n${usageProgress(peakSramPercent, "Estimated peak SRAM")}\n`
    : "";
  const flashBlock = metrics?.flash
    ? `\n## 🧠 AVR capacity evidence\n\n${usageProgress(flashPercent, "Application flash")}\n${sramBadge}\n| Resource | Used | Capacity | Free |\n|---|---:|---:|---:|\n| Application flash | ${metrics.flash.usedBytes.toLocaleString("en-US")} B (${metrics.flash.usagePercent}%) | ${metrics.flash.capacityBytes.toLocaleString("en-US")} B | **${metrics.flash.freeBytes.toLocaleString("en-US")} B** |${metrics?.peakSram?.usedBytes != null ? `\n| Estimated peak SRAM | ${metrics.peakSram.usedBytes.toLocaleString("en-US")} B | ${metrics.peakSram.capacityBytes.toLocaleString("en-US")} B | **${metrics.peakSram.freeBytes.toLocaleString("en-US")} B** |` : ""}\n`
    : "";

  return `# 🚀 ${escapeMarkdown(PRODUCT_NAME)} ${escapeMarkdown(tag)} release

> [!TIP]
> **11 build targets passed.**

| Property | Result |
|---|---|
| Source | ${sourceLink(sourceSha, context)} |
| Workflow | ${runLink(context)} |
| Staged release inputs | **${assets.length} files** · **${humanBytes(totalBytes)}** |
| Integrity | SHA-256 manifest + per-archive checksums + GitHub build provenance |

## 🎯 Downloads

| Platform | Host | Virtual Board |
|---|---|---|
${chooser}

| AVR choice | Direct download |
|---|---|
| Application image | ${assetLink(selection.firmware.application, context)} |
| Flash + bootloader image | ${assetLink(selection.firmware.flashWithBootloader, context)} |
| Firmware package | ${assetLink(selection.firmware.archive, context)} |
${flashBlock}
## ✅ Validation

- ✅ Five-platform Host matrix completed on native GitHub runners
- ✅ Five-platform Virtual Board CMake/CTest matrix completed on native GitHub runners
- ✅ AVR compiled for the pinned MiniCore target and Intel HEX was validated
- ✅ Checksum sidecars were re-read and matched their assets
- ✅ A machine-readable ${code("release-manifest.json")} and fixed ${code("RELEASE-NOTES.md")} were generated
- ⚠️ Physical flash/upload remains a separate hardware validation step

${context.releaseUrl ? `**${context.draft ? "Draft assembly run" : "Release"}:** [Open ${escapeMarkdown(PRODUCT_NAME)} ${escapeMarkdown(tag)}](${context.releaseUrl})` : ""}
`;
}

function releaseManifest({
  tag,
  sourceSha,
  assets,
  selection,
  context,
  checksumValidations,
  manifests,
  metrics,
}) {
  return {
    format: FORMAT,
    product: PRODUCT_NAME,
    release: {
      tag,
      draft: context.draft,
      prerelease: context.prerelease,
      sourceSha,
      sourceUrl: context.sourceUrl || null,
      repository: context.repository || null,
      runId: context.runId || null,
      runAttempt: context.runAttempt || null,
      runUrl: context.runUrl || null,
    },
    generatedBy: ".github/scripts/release-showcase.mjs",
    documents: {
      releaseNotes: {
        delivery: "github-release-body",
        generatedFrom: "RELEASE-NOTES.md",
      },
      releaseManifest: "release-manifest.json",
    },
    chooser: {
      firmware: {
        target: "ATmega328P / MiniCore",
        applicationImage: selection.firmware.application.file,
        flashWithBootloaderImage: selection.firmware.flashWithBootloader.file,
        eepromImage: selection.firmware.eeprom?.file ?? null,
        archive: selection.firmware.archive.file,
      },
      platforms: selection.platforms.map((platform) => ({
        target: platform.id,
        label: platform.label,
        controllerArchive: platform.controller.file,
        virtualBoardArchive: platform.virtualBoard.file,
      })),
    },
    validation: validationRows(selection).map((row) => ({
      component: row.component,
      target: row.target,
      status: "passed",
      checks: row.checks.split(" · "),
      evidence: row.evidence,
    })),
    integrity: {
      algorithm: "SHA-256",
      checksumManifest: assets.some((asset) => asset.file === "SHA256SUMS.txt")
        ? "SHA256SUMS.txt"
        : null,
      validatedSidecars: checksumValidations,
      provenance: {
        provider: "GitHub artifact attestations",
        scope: "release archives, direct firmware images, and release manifest",
        verifyCommand: context.repository
          ? `gh attestation verify ASSET --repo ${context.repository}`
          : "gh attestation verify ASSET --repo OWNER/REPOSITORY",
      },
    },
    ...(metrics ? { firmware: metrics } : {}),
    inputManifests: manifests,
    assets,
    limitations: [
      "CI build success is not evidence of a physical upload or on-device acceptance test.",
      "Firmware is specific to the pinned ATmega328P MiniCore/Urboot profile.",
      "Virtual Board cannot reproduce every electrical and timing behavior of real hardware.",
      ...(context.prerelease
        ? ["Pre-releases may change configuration and protocol compatibility."]
        : []),
    ],
  };
}

function validateArguments(tag, sourceSha) {
  if (!tag || /[\u0000-\u001f\u007f]/u.test(tag) || tag.length > 128) {
    throw new Error("TAG must be a non-empty release tag without control characters");
  }
  if (!/^[0-9a-fA-F]{40}$|^[0-9a-fA-F]{64}$/u.test(sourceSha || "")) {
    throw new Error("SOURCE_SHA must be a full 40- or 64-character Git commit hash");
  }
}

export function buildReleasePresentation({
  assetsDirectory,
  tag,
  sourceSha,
  environment = process.env,
}) {
  validateArguments(tag, sourceSha);
  const directory = resolve(assetsDirectory);
  if (!existsSync(directory) || !statSync(directory).isDirectory()) {
    throw new Error(`release asset directory does not exist: ${assetsDirectory}`);
  }

  const assets = inspectAssets(directory);
  if (assets.length === 0) throw new Error("release asset directory is empty");
  const checksumValidations = validateChecksums(directory, assets);
  const selection = selectPackages(assets);
  const buildManifests = loadBuildManifests(directory, assets);
  validateFirmwareSelection(selection, buildManifests.firmware);
  const metrics = firmwareMetrics(buildManifests.firmware);
  const context = repositoryContext(environment, tag, sourceSha.toLowerCase());
  const input = {
    tag,
    sourceSha: sourceSha.toLowerCase(),
    assets,
    selection,
    context,
    metrics,
  };

  const notes = releaseNotes(input);
  const manifest = releaseManifest({
    ...input,
    checksumValidations,
    manifests: buildManifests.manifests,
  });
  const summary = stepSummary(input);
  const notesPath = resolve(directory, "RELEASE-NOTES.md");
  const manifestPath = resolve(directory, "release-manifest.json");
  writeFileSync(notesPath, notes, "utf8");
  writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, "utf8");

  if (environment.GITHUB_STEP_SUMMARY) {
    appendFileSync(environment.GITHUB_STEP_SUMMARY, summary, "utf8");
  }
  if (environment.GITHUB_OUTPUT) {
    const outputPath = (path) => relative(process.cwd(), path).replaceAll("\\", "/");
    appendFileSync(
      environment.GITHUB_OUTPUT,
      [
        `release_notes=${outputPath(notesPath)}`,
        `release_manifest=${outputPath(manifestPath)}`,
        `asset_count=${assets.length}`,
        `total_bytes=${assets.reduce((sum, asset) => sum + asset.bytes, 0)}`,
        `prerelease=${context.prerelease}`,
        "",
      ].join("\n"),
      "utf8",
    );
  }

  return { notesPath, manifestPath, notes, manifest, summary };
}

function main(arguments_) {
  const [assetsDirectory, tag, sourceSha] = arguments_;
  if (!assetsDirectory || !tag || !sourceSha) {
    process.stderr.write(
      "Usage: release-showcase.mjs ASSET_DIRECTORY TAG SOURCE_SHA\n" +
        "Example: node .github/scripts/release-showcase.mjs release-assets v0.1.0-alpha.1 $GITHUB_SHA\n",
    );
    return 2;
  }
  const result = buildReleasePresentation({ assetsDirectory, tag, sourceSha });
  process.stdout.write(
    `Generated ${relative(process.cwd(), result.notesPath)} and ${relative(process.cwd(), result.manifestPath)} for ${result.manifest.assets.length} assets.\n`,
  );
  return 0;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    process.exitCode = main(process.argv.slice(2));
  } catch (error) {
    process.stderr.write(`release showcase failed: ${error.message}\n`);
    process.exitCode = 1;
  }
}
