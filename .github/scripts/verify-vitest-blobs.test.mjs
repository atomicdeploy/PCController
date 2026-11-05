import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { TARGETS } from "./codebase-summary.mjs";

test("merged reports cannot silently omit an untested native runner", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-vitest-"));
  try {
    const summary = join(directory, "summary.md");
    const run = () => spawnSync(process.execPath, [
      resolve(".github/scripts/verify-vitest-blobs.mjs"), directory,
    ], { encoding: "utf8", env: { ...process.env, GITHUB_STEP_SUMMARY: summary } });
    for (const { id } of TARGETS.slice(1)) writeFileSync(join(directory, `${id}.json`), "fixture");
    const missing = run();
    assert.equal(missing.status, 1);
    assert.match(missing.stderr, /missing Linux-x64/u);
    assert.match(readFileSync(summary, "utf8"), /\[!WARNING\]/u);
    writeFileSync(join(directory, `${TARGETS[0].id}.json`), "fixture");
    assert.equal(run().status, 0);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("host keeps native tests and uses supported Vitest reporting and merging", () => {
  const workflow = readFileSync(".github/workflows/host.yml", "utf8");
  const config = readFileSync("Tools/Controller/web/vite.config.ts", "utf8");
  assert.match(workflow, /VITEST_BLOB_LABEL: \$\{\{ matrix.target \}\}/u);
  assert.match(workflow, /--merge-reports=\.\.\/\.\.\/\.\.\/build-summary\/vitest/u);
  assert.match(workflow, /name: WebUI-Test-Report/u);
  assert.doesNotMatch(workflow, /--skip-tests/u);
  assert.match(config, /\['github-actions', \{ jobSummary: \{ enabled: false \} \}\]/u);
  assert.match(config, /\['blob', \{ outputFile: process.env.PCCONTROLLER_VITEST_BLOB \}\]/u);
});
