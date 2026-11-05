const escapeRegExp = value => String(value).replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");

// v7 raw uploads use the versioned filename as their artifact name. Keep old
// ZIP-bundle names readable when rendering historical runs, never match SHA files.
export function findDistributionArtifact(artifacts, legacyName) {
  let pattern;
  if (legacyName === "PCController-Firmware-ATmega328P") {
    pattern = /^PCController-Firmware-.+-AVR-ATmega328P\.tar\.gz$/u;
  } else {
    const match = /^(PCController-(?:Host|VirtualBoard))-(.+)$/u.exec(legacyName);
    if (!match) return undefined;
    pattern = new RegExp(`^${escapeRegExp(match[1])}-.+-${escapeRegExp(match[2])}\\.tar\\.gz$`, "u");
  }
  return artifacts.filter(item => item.name === legacyName || pattern.test(item.name))
    .sort((a, b) => Number(b.id) - Number(a.id))[0];
}

export const distributionDownloadLabel = artifact => artifact?.name?.endsWith(".tar.gz") ? "Download .tar.gz" : "Download ZIP";
export const findDistributionChecksum = (artifacts, artifact) => artifacts.find(item =>
  item.name === `${artifact?.name}.sha256` && !item.expired);
