import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

function assetName(value) {
  return path.basename(String(value));
}

export function findCaseFoldCollisions(desiredPaths, existingAssets) {
  const desiredByFold = new Map();

  for (const desiredPath of desiredPaths) {
    const name = assetName(desiredPath);
    const folded = name.toLowerCase();
    const previous = desiredByFold.get(folded);
    if (previous && previous !== name) {
      throw new Error(`desired release assets collide case-insensitively: ${previous} and ${name}`);
    }
    desiredByFold.set(folded, name);
  }

  return existingAssets
    .flat()
    .filter((asset) => asset && Number.isInteger(asset.id) && typeof asset.name === "string")
    .map((asset) => ({
      id: asset.id,
      name: asset.name,
      replacement: desiredByFold.get(asset.name.toLowerCase()),
    }))
    .filter((asset) => asset.replacement && asset.replacement !== asset.name)
    .sort((left, right) => left.name.localeCompare(right.name));
}

function main() {
  const [existingAssetsPath, ...desiredPaths] = process.argv.slice(2);
  if (!existingAssetsPath || desiredPaths.length === 0) {
    throw new Error("usage: release-asset-collisions.mjs <existing-assets.json> <desired-asset>...");
  }

  const existingAssets = JSON.parse(fs.readFileSync(existingAssetsPath, "utf8"));
  if (!Array.isArray(existingAssets)) {
    throw new Error("existing release assets must be a JSON array");
  }

  for (const collision of findCaseFoldCollisions(desiredPaths, existingAssets)) {
    process.stdout.write(`${collision.id}\t${collision.name}\t${collision.replacement}\n`);
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}
