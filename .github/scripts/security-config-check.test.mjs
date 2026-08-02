import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  inventoryRepository,
  validateActionPins,
  validateCodeql,
  validateDependabot,
  validateProtectedDeployment,
  validateSecurityConfiguration,
} from "./security-config-check.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const dependabot = readFileSync(resolve(root, ".github/dependabot.yml"), "utf8");
const codeql = readFileSync(resolve(root, ".github/workflows/codeql.yml"), "utf8");
const deployment = readFileSync(resolve(root, ".github/workflows/deploy-avr.yml"), "utf8");
const inventory = inventoryRepository([
  ".github/workflows/build.yml",
  ".github/scripts/example.mjs",
  "Tools/Build/package.json",
  "Tools/Firmware/package.json",
  "Tools/Controller/go.mod",
  "Tools/Controller/main.go",
  "Tools/VirtualBoard/main.cpp",
  "PCController.ino",
]);

test("the repository security configuration is complete", () => {
  const result = validateSecurityConfiguration(root);
  assert.deepEqual(result.errors, []);
  assert.equal(result.stats.dependabotEcosystems, 3);
  assert.equal(result.stats.codeqlCategories, 6);
  assert.ok(result.stats.actionReferences > 0);
});

test("Dependabot validation rejects an uncovered Node package root", () => {
  const mutated = dependabot.replace("      - /Tools/Firmware\n", "");
  const errors = validateDependabot(mutated, inventory);
  assert.ok(errors.some((error) => error.includes("npm coverage is missing /Tools/Firmware")));
});

test("CodeQL validation rejects a missing repository language", () => {
  const mutated = codeql.replace(
    /          - key: actions[\s\S]*?(?=          - key: javascript)/u,
    "",
  );
  const errors = validateCodeql(mutated, inventory.languages);
  assert.ok(errors.some((error) => error.includes("matrix is missing actions")));
  assert.ok(errors.some((error) => error.includes("detected language actions")));
});

test("workflow pin validation rejects mutable action tags", () => {
  const mutated = codeql.replace(
    "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
    "actions/checkout@v7",
  );
  const result = validateActionPins(new Map([[".github/workflows/codeql.yml", mutated]]));
  assert.ok(result.errors.some((error) => error.includes("non-immutable action reference")));
});

test("CodeQL validation rejects pull_request_target", () => {
  const mutated = codeql.replace("  pull_request:\n", "  pull_request_target:\n");
  const errors = validateCodeql(mutated, inventory.languages);
  assert.ok(errors.some((error) => error.includes("pull requests targeting main")));
  assert.ok(errors.some((error) => error.includes("privileged or path-filtered")));
});

test("CodeQL validation rejects path-filtered required checks", () => {
  const mutated = codeql.replace(
    "  pull_request:\n    branches: [main]\n",
    "  pull_request:\n    branches: [main]\n    paths: ['Tools/**']\n",
  );
  const errors = validateCodeql(mutated, inventory.languages);
  assert.ok(errors.some((error) => error.includes("privileged or path-filtered")));
});

test("CodeQL validation rejects a hollow manual-build category", () => {
  const mutated = codeql.replace("          go test ./...\n", "");
  const errors = validateCodeql(mutated, inventory.languages);
  assert.ok(errors.some((error) => error.includes("go-linux build is missing go test ./...")));
});

test("CodeQL Windows validation requires stable tests and rejects PowerShell", () => {
  const directTests = codeql.replace(
    "node ../Build/go-tests.mjs --module . --output ../../.build/tests/go --go go",
    "go test ./...",
  );
  const directTestErrors = validateCodeql(directTests, inventory.languages);
  assert.ok(directTestErrors.some((error) => error.includes("go-windows build is missing ../Build/go-tests.mjs")));

  const powershell = codeql.replace("        shell: cmd\n", "        shell: pwsh\n");
  const powershellErrors = validateCodeql(powershell, inventory.languages);
  assert.ok(powershellErrors.some((error) => error.includes("not PowerShell")));
});

test("CodeQL validation rejects a cosmetic gate that ignores failures", () => {
  const mutated = codeql
    .replace("    if: always()\n    needs: analyze\n", "    needs: analyze\n")
    .replace(/^          \[\[ "\$ANALYSIS_RESULT" == "success" \]\]\s*$/mu, "");
  const errors = validateCodeql(mutated, inventory.languages);
  assert.ok(errors.some((error) => error.includes("must run even when analysis fails")));
  assert.ok(errors.some((error) => error.includes("must fail for a non-success")));
});

test("deployment validation rejects release-selected code on the self-hosted runner", () => {
  const mutated = deployment.replace(
    '      - name: "📥 Check out protected deployment controller"\n' +
      "        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7\n" +
      "        with:\n" +
      "          ref: main\n",
    '      - name: "📥 Check out protected deployment controller"\n' +
      "        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7\n" +
      "        with:\n" +
      "          ref: ${{ needs.prepare.outputs.source-sha }}\n",
  );
  const errors = validateProtectedDeployment(mutated);
  assert.ok(errors.some((error) => error.includes("must not execute release-selected source")));
  assert.ok(errors.some((error) => error.includes("must use protected main")));
});

test("deployment validation rejects an untrusted release source lineage", () => {
  const mutated = deployment
    .replace('    if: github.ref == \'refs/heads/main\'\n', "")
    .replace("    environment: avr-release-read\n", "")
    .replace("          fetch-depth: 0\n", "")
    .replace('          test "$release_target" = "$SOURCE_SHA"\n', "")
    .replace('          git merge-base --is-ancestor "$SOURCE_SHA" HEAD\n', "");
  const errors = validateProtectedDeployment(mutated);
  assert.ok(errors.some((error) => error.includes("main-branch dispatch")));
  assert.ok(errors.some((error) => error.includes("main-restricted release environment")));
  assert.ok(errors.some((error) => error.includes("fetch protected main history")));
  assert.ok(errors.some((error) => error.includes("release target")));
  assert.ok(errors.some((error) => error.includes("protected main history")));
});
