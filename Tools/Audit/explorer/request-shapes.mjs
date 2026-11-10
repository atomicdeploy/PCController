import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync, existsSync, mkdirSync } from "node:fs";
import { resolve, join } from "node:path";
export function extractRequestShapes(root, sources) {
  const source = resolve(root, "Tools/Audit/explorer/request-shapes.go");
  const directory =
    process.platform === "win32"
      ? join(process.env.LOCALAPPDATA, "PCController", "build-programs")
      : resolve(root, ".build/api-tools");
  mkdirSync(directory, { recursive: true });
  const binary = join(
    directory,
    process.platform === "win32"
      ? "api-request-shapes.exe"
      : "api-request-shapes",
  );
  const identity = createHash("sha256")
    .update(readFileSync(source))
    .digest("hex");
  const receipt = binary + ".sha256";
  if (
    !existsSync(binary) ||
    !existsSync(receipt) ||
    readFileSync(receipt, "utf8") !== identity
  ) {
    execFileSync("go", ["build", "-o", binary, source], {
      cwd: resolve(root, "Tools/Controller"),
      windowsHide: true,
      env: { ...process.env, CGO_ENABLED: "0" },
      stdio: ["ignore", "pipe", "pipe"],
    });
    writeFileSync(receipt, identity);
  }
  return JSON.parse(
    execFileSync(binary, [], {
      input: JSON.stringify(sources),
      encoding: "utf8",
      windowsHide: true,
      maxBuffer: 8 << 20,
    }),
  );
}
