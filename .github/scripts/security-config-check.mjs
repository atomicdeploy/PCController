import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const defaultRoot = resolve(scriptDirectory, "..", "..");
const ignoredDirectories = new Set([
  ".build",
  ".ci",
  ".git",
  "bin",
  "build",
  "node_modules",
]);

const expectedCodeqlRows = new Map([
  ["actions", {
    runner: "ubuntu-24.04",
    language: "actions",
    build_mode: "none",
    category: "/language:actions/scope:workflows",
  }],
  ["javascript", {
    runner: "ubuntu-24.04",
    language: "javascript-typescript",
    build_mode: "none",
    category: "/language:javascript-typescript/scope:tooling",
  }],
  ["go-linux", {
    runner: "ubuntu-24.04",
    language: "go",
    build_mode: "manual",
    category: "/language:go/platform:linux",
  }],
  ["go-windows", {
    runner: "windows-2025",
    language: "go",
    build_mode: "manual",
    category: "/language:go/platform:windows",
  }],
  ["cpp-avr-linux", {
    runner: "ubuntu-24.04",
    language: "c-cpp",
    build_mode: "manual",
    category: "/language:c-cpp/platform:avr-linux",
  }],
  ["cpp-windows", {
    runner: "windows-2025",
    language: "c-cpp",
    build_mode: "manual",
    category: "/language:c-cpp/platform:windows",
  }],
]);

function unquote(value) {
  const trimmed = value.trim();
  if (
    trimmed.length >= 2 &&
    ((trimmed.startsWith('"') && trimmed.endsWith('"')) ||
      (trimmed.startsWith("'") && trimmed.endsWith("'")))
  ) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

function slashPath(value) {
  return value.replaceAll("\\", "/");
}

function walkFiles(root) {
  const files = [];
  const pending = [root];
  while (pending.length > 0) {
    const directory = pending.pop();
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (entry.isDirectory() && ignoredDirectories.has(entry.name)) {
        continue;
      }
      const absolutePath = resolve(directory, entry.name);
      if (entry.isDirectory()) {
        pending.push(absolutePath);
      } else if (entry.isFile()) {
        files.push(slashPath(relative(root, absolutePath)));
      }
    }
  }
  return files.sort();
}

function normalizedManifestDirectory(path) {
  const directory = slashPath(dirname(path));
  return directory === "." ? "/" : `/${directory}`;
}

export function inventoryRepository(files) {
  const npmDirectories = files
    .filter((path) => path.endsWith("/package.json") || path === "package.json")
    .map(normalizedManifestDirectory);
  const goDirectories = files
    .filter((path) => path.endsWith("/go.mod") || path === "go.mod")
    .map(normalizedManifestDirectory);
  const languages = new Set();
  if (files.some((path) => /^\.github\/workflows\/[^/]+\.ya?ml$/u.test(path))) {
    languages.add("actions");
  }
  if (files.some((path) => /\.(?:cjs|js|mjs|ts|tsx)$/iu.test(path))) {
    languages.add("javascript-typescript");
  }
  if (files.some((path) => /\.go$/iu.test(path))) {
    languages.add("go");
  }
  if (files.some((path) => /\.(?:c|cc|cpp|cxx|h|hh|hpp|hxx|ino)$/iu.test(path))) {
    languages.add("c-cpp");
  }
  return {
    npmDirectories: [...new Set(npmDirectories)].sort(),
    goDirectories: [...new Set(goDirectories)].sort(),
    languages,
  };
}

export function parseDependabotEntries(source) {
  const starts = [...source.matchAll(/^  - package-ecosystem:\s*(.+?)\s*$/gmu)];
  return starts.map((match, index) => {
    const start = match.index;
    const end = starts[index + 1]?.index ?? source.length;
    const block = source.slice(start, end);
    const ecosystem = unquote(match[1]);
    const directory = block.match(/^    directory:\s*(.+?)\s*$/mu);
    const lines = block.split(/\r?\n/u);
    const directories = [];
    let collectingDirectories = false;
    for (const line of lines) {
      if (/^    directories:\s*$/u.test(line)) {
        collectingDirectories = true;
        continue;
      }
      if (!collectingDirectories) {
        continue;
      }
      const item = line.match(/^      -\s*(.+?)\s*$/u);
      if (item) {
        directories.push(unquote(item[1]));
        continue;
      }
      if (line.trim() !== "") {
        collectingDirectories = false;
      }
    }
    if (directory) {
      directories.push(unquote(directory[1]));
    }
    return { ecosystem, directories, block };
  });
}

export function validateDependabot(source, inventory) {
  const errors = [];
  const entries = parseDependabotEntries(source);
  const required = new Map([
    ["github-actions", ["/"]],
    ["gomod", inventory.goDirectories],
    ["npm", inventory.npmDirectories],
  ]);

  if (!/^version:\s*2\s*$/mu.test(source)) {
    errors.push("Dependabot must use schema version 2");
  }
  if (/^\s+target-branch:/mu.test(source)) {
    errors.push("Dependabot must not redirect updates away from the protected default branch");
  }

  for (const [ecosystem, expectedDirectories] of required) {
    const matching = entries.filter((entry) => entry.ecosystem === ecosystem);
    const configured = matching.flatMap((entry) => entry.directories).sort();
    for (const expected of expectedDirectories) {
      if (!configured.includes(expected)) {
        errors.push(`Dependabot ${ecosystem} coverage is missing ${expected}`);
      }
    }
    for (const entry of matching) {
      if (!/^    schedule:\s*$/mu.test(entry.block)) {
        errors.push(`Dependabot ${ecosystem} entry has no schedule`);
      }
      if (!/^        applies-to:\s*version-updates\s*$/mu.test(entry.block)) {
        errors.push(`Dependabot ${ecosystem} entry has no explicit version-update group`);
      }
      if (!/^        applies-to:\s*security-updates\s*$/mu.test(entry.block)) {
        errors.push(`Dependabot ${ecosystem} entry has no grouped security updates`);
      }
      const wildcardCount = [...entry.block.matchAll(/^          -\s*["']?\*["']?\s*$/gmu)].length;
      if (wildcardCount < 2) {
        errors.push(`Dependabot ${ecosystem} version and security groups must cover every dependency`);
      }
      if (!/^        update-types:\s*\r?\n          - minor\s*\r?\n          - patch\s*$/mu.test(entry.block)) {
        errors.push(`Dependabot ${ecosystem} must group minor and patch version updates`);
      }
    }
  }
  return errors;
}

export function parseCodeqlRows(source) {
  const starts = [...source.matchAll(/^          - key:\s*(.+?)\s*$/gmu)];
  return starts.map((match, index) => {
    const start = match.index;
    const end = starts[index + 1]?.index ?? source.length;
    const block = source.slice(start, end);
    const field = (name) => {
      const value = block.match(new RegExp(`^            ${name}:\\s*(.+?)\\s*$`, "mu"));
      return value ? unquote(value[1]) : "";
    };
    return {
      key: unquote(match[1]),
      runner: field("runner"),
      language: field("language"),
      build_mode: field("build_mode"),
      category: field("category"),
    };
  });
}

export function validateCodeql(source, sourceLanguages) {
  const errors = [];
  const triggerBlock = source.match(/^on:\s*\r?\n([\s\S]*?)^permissions:/mu)?.[1] ?? "";
  const permissionBlock = source.match(/^permissions:\s*\r?\n([\s\S]*?)^concurrency:/mu)?.[1] ?? "";
  const stepStarts = [...source.matchAll(/^      - name:\s*(.+?)\s*$/gmu)];
  const steps = stepStarts.map((match, index) => ({
    start: match.index,
    block: source.slice(match.index, stepStarts[index + 1]?.index ?? source.length),
  }));

  if (!/^  push:\s*\r?\n    branches:\s*\[main\]\s*$/mu.test(triggerBlock)) {
    errors.push("CodeQL must run for pushes to main");
  }
  if (!/^  pull_request:\s*\r?\n    branches:\s*\[main\]\s*$/mu.test(triggerBlock)) {
    errors.push("CodeQL must run for pull requests targeting main");
  }
  for (const event of ["merge_group", "schedule", "workflow_dispatch"]) {
    if (!new RegExp(`^  ${event}:`, "mu").test(triggerBlock)) {
      errors.push(`CodeQL trigger is missing ${event}`);
    }
  }
  if (/pull_request_target|^\s+paths(?:-ignore)?:/mu.test(triggerBlock)) {
    errors.push("CodeQL must not use privileged or path-filtered pull-request analysis");
  }
  for (const permission of [
    "actions: read",
    "contents: read",
    "packages: read",
    "security-events: write",
  ]) {
    if (!permissionBlock.includes(permission)) {
      errors.push(`CodeQL permission is missing ${permission}`);
    }
  }
  if (!/^\s+fail-fast:\s*false\s*$/mu.test(source)) {
    errors.push("CodeQL matrix must keep fail-fast disabled");
  }
  if (/\$\{\{\s*secrets\./u.test(source)) {
    errors.push("CodeQL builds must not receive repository secrets");
  }
  if (!/persist-credentials:\s*false/u.test(source)) {
    errors.push("CodeQL checkout must not persist GitHub credentials");
  }
  if (!/queries:\s*security-extended/u.test(source)) {
    errors.push("CodeQL must include the security-extended query suite");
  }
  if (!/name:\s*["']🛡️ CodeQL · Entire repository["']/u.test(source)) {
    errors.push("CodeQL must expose the stable entire-repository gate");
  }

  const manualBuildRequirements = new Map([
    ["go-linux", [
      "go test ./...",
      "go run ./winres/generate_icon.go",
      "-buildmode=c-shared",
      "./cmd/controllerlib",
    ]],
    ["go-windows", [
      "go test ./...",
      "go run ./winres/generate_icon.go",
      "-buildmode=c-shared",
      "./cmd/controllerlib",
    ]],
    ["cpp-avr-linux", [
      "./build.sh --firmware-only --clean",
      "-S Tools/VirtualBoard",
      "cmake --build .build/codeql-virtual-board",
    ]],
    ["cpp-windows", [
      "-S Tools/VirtualBoard",
      "cmake --build .build/codeql-virtual-board",
      "Tools\\Controller\\examples\\c_abi_smoke.c",
    ]],
  ]);
  const initIndex = source.indexOf("github/codeql-action/init@");
  for (const [key, markers] of manualBuildRequirements) {
    const condition = `if: matrix.key == '${key}'`;
    const guardedSteps = steps.filter((candidate) => candidate.block.includes(condition));
    if (guardedSteps.length === 0) {
      errors.push(`CodeQL has no guarded manual build step for ${key}`);
      continue;
    }
    const guardedSource = guardedSteps.map((step) => step.block).join("\n");
    for (const marker of markers) {
      if (!guardedSource.includes(marker)) {
        errors.push(`CodeQL ${key} build is missing ${marker}`);
      }
    }
    const buildSteps = guardedSteps.filter((step) => markers.some((marker) => step.block.includes(marker)));
    if (initIndex < 0 || buildSteps.some((step) => step.start < initIndex)) {
      errors.push(`CodeQL ${key} build must run after extractor initialization`);
    }
  }

  const completeBlock = source.match(/^  complete:\s*\r?\n([\s\S]*)$/mu)?.[1] ?? "";
  if (!/^    if:\s*always\(\)\s*$/mu.test(completeBlock)) {
    errors.push("CodeQL entire-repository gate must run even when analysis fails");
  }
  if (!/^    needs:\s*analyze\s*$/mu.test(completeBlock)) {
    errors.push("CodeQL entire-repository gate must depend on the full analysis matrix");
  }
  if (!/ANALYSIS_RESULT:\s*\$\{\{\s*needs\.analyze\.result\s*\}\}/u.test(completeBlock)) {
    errors.push("CodeQL entire-repository gate must inspect the aggregate analysis result");
  }
  if (!/^          \[\[\s*"\$ANALYSIS_RESULT"\s*==\s*"success"\s*\]\]\s*$/mu.test(completeBlock)) {
    errors.push("CodeQL entire-repository gate must fail for a non-success analysis result");
  }

  const actionReferences = [...source.matchAll(/github\/codeql-action\/(init|analyze)@([a-f0-9]{40})/gu)];
  if (actionReferences.length !== 2) {
    errors.push("CodeQL must have exactly one SHA-pinned init and analyze action");
  } else {
    const operations = new Set(actionReferences.map((match) => match[1]));
    const commits = new Set(actionReferences.map((match) => match[2]));
    if (operations.size !== 2 || !operations.has("init") || !operations.has("analyze")) {
      errors.push("CodeQL must run both init and analyze");
    }
    if (commits.size !== 1) {
      errors.push("CodeQL init and analyze must use the same immutable release commit");
    }
  }

  const rows = parseCodeqlRows(source);
  if (rows.length !== expectedCodeqlRows.size) {
    errors.push(`CodeQL must define ${expectedCodeqlRows.size} repository-wide analysis categories`);
  }
  const seenCategories = new Set();
  const matrixLanguages = new Set();
  for (const [key, expected] of expectedCodeqlRows) {
    const row = rows.find((candidate) => candidate.key === key);
    if (!row) {
      errors.push(`CodeQL matrix is missing ${key}`);
      continue;
    }
    for (const [field, expectedValue] of Object.entries(expected)) {
      if (row[field] !== expectedValue) {
        errors.push(`CodeQL ${key} ${field} must be ${expectedValue}`);
      }
    }
  }
  for (const row of rows) {
    matrixLanguages.add(row.language);
    if (seenCategories.has(row.category)) {
      errors.push(`CodeQL category is duplicated: ${row.category}`);
    }
    seenCategories.add(row.category);
  }
  for (const language of sourceLanguages) {
    if (!matrixLanguages.has(language)) {
      errors.push(`CodeQL coverage is missing detected language ${language}`);
    }
  }
  for (const language of matrixLanguages) {
    if (!sourceLanguages.has(language)) {
      errors.push(`CodeQL config contains an analyzer with no matching repository source: ${language}`);
    }
  }
  return errors;
}

export function validateActionPins(workflows) {
  const errors = [];
  let references = 0;
  for (const [path, source] of workflows) {
    for (const match of source.matchAll(/^\s*uses:\s*([^\s#]+)(?:\s+#\s*(.+))?\s*$/gmu)) {
      const specification = unquote(match[1]);
      if (specification.startsWith("./")) {
        continue;
      }
      references += 1;
      const at = specification.lastIndexOf("@");
      const revision = at >= 0 ? specification.slice(at + 1) : "";
      if (!/^[a-f0-9]{40}$/u.test(revision)) {
        errors.push(`${path} has a non-immutable action reference: ${specification}`);
      }
      if (!/^v\d+(?:\.\d+){0,2}$/u.test(match[2]?.trim() ?? "")) {
        errors.push(`${path} action pin has no readable version comment: ${specification}`);
      }
    }
  }
  return { errors, references };
}

export function validateProtectedDeployment(source) {
  const errors = [];
  const triggerBlock = source.match(/^on:\s*\r?\n([\s\S]*?)^permissions:/mu)?.[1] ?? "";
  if (!/^  workflow_dispatch:\s*$/mu.test(triggerBlock) || /pull_request|push:|schedule:/u.test(triggerBlock)) {
    errors.push("protected AVR deployment must remain manual-dispatch only");
  }
  if (!/^    if:\s*github\.ref == ['"]refs\/heads\/main['"]\s*$/mu.test(source)) {
    errors.push("AVR bundle preparation must be restricted to a main-branch dispatch");
  }
  if (!/^    environment:\s*avr-release-read\s*$/mu.test(source)) {
    errors.push("AVR bundle preparation must use the main-restricted release environment");
  }
  if (/ref:\s*\$\{\{\s*needs\.prepare\.outputs\.source-sha/u.test(source)) {
    errors.push("the self-hosted deployment runner must not execute release-selected source");
  }

  const stepStarts = [...source.matchAll(/^      - name:\s*(.+?)\s*$/gmu)];
  const steps = stepStarts.map((match, index) =>
    source.slice(match.index, stepStarts[index + 1]?.index ?? source.length));
  const checkouts = steps.filter((step) => step.includes("uses: actions/checkout@"));
  if (checkouts.length !== 2) {
    errors.push("protected AVR deployment must have exactly two explicit checkouts");
  }
  for (const checkout of checkouts) {
    if (!/^          ref:\s*main\s*$/mu.test(checkout)) {
      errors.push("every protected AVR deployment checkout must use protected main");
    }
    if (!/^          persist-credentials:\s*false\s*$/mu.test(checkout)) {
      errors.push("protected AVR deployment checkouts must not persist credentials");
    }
  }
  if (checkouts[0] && !/^          fetch-depth:\s*0\s*$/mu.test(checkouts[0])) {
    errors.push("AVR bundle preparation must fetch protected main history for ancestry validation");
  }
  if (!/source-sha\.txt/u.test(source) || !/deployment-controller-sha\.txt/u.test(source)) {
    errors.push("deployment evidence must preserve firmware and controller source identities");
  }
  if (!/--json targetCommitish/u.test(source) || !/test "\$release_target" = "\$SOURCE_SHA"/u.test(source)) {
    errors.push("the release target must equal the attested firmware source SHA");
  }
  if (!/git cat-file -e "\$\{SOURCE_SHA\}\^\{commit\}"/u.test(source) ||
      !/git merge-base --is-ancestor "\$SOURCE_SHA" HEAD/u.test(source)) {
    errors.push("the firmware source must be a commit in protected main history");
  }
  if (/PCCONTROLLER_VERSION:\s*\$\{\{\s*inputs\.release_tag\s*\}\}/u.test(source)) {
    errors.push("the protected-main programmer must not claim the firmware release identity");
  }
  return errors;
}

export function validateSecurityConfiguration(root = defaultRoot) {
  const errors = [];
  const requiredFiles = [
    ".github/dependabot.yml",
    ".github/dependencies.json",
    ".github/scripts/dependency-report.mjs",
    ".github/workflows/codeql.yml",
    ".github/workflows/dependencies.yml",
  ];
  for (const path of requiredFiles) {
    if (!existsSync(resolve(root, path))) {
      errors.push(`security configuration file is missing: ${path}`);
    }
  }
  if (errors.length > 0) {
    return { errors, stats: { actionReferences: 0, codeqlCategories: 0 } };
  }

  const files = walkFiles(root);
  const inventory = inventoryRepository(files);
  const dependabot = readFileSync(resolve(root, ".github/dependabot.yml"), "utf8");
  const codeql = readFileSync(resolve(root, ".github/workflows/codeql.yml"), "utf8");
  const deployment = readFileSync(resolve(root, ".github/workflows/deploy-avr.yml"), "utf8");
  const workflowPaths = files.filter((path) => /^\.github\/workflows\/[^/]+\.ya?ml$/u.test(path));
  const workflows = new Map(
    workflowPaths.map((path) => [path, readFileSync(resolve(root, path), "utf8")]),
  );
  errors.push(...validateDependabot(dependabot, inventory));
  errors.push(...validateCodeql(codeql, inventory.languages));
  errors.push(...validateProtectedDeployment(deployment));
  const actionPins = validateActionPins(workflows);
  errors.push(...actionPins.errors);
  return {
    errors,
    stats: {
      actionReferences: actionPins.references,
      codeqlCategories: parseCodeqlRows(codeql).length,
      dependabotEcosystems: parseDependabotEntries(dependabot).length,
    },
  };
}

function main() {
  const rootIndex = process.argv.indexOf("--root");
  const root = rootIndex >= 0 ? resolve(process.argv[rootIndex + 1]) : defaultRoot;
  const { errors, stats } = validateSecurityConfiguration(root);
  if (errors.length > 0) {
    for (const error of errors) {
      process.stderr.write(`ERROR: ${error}\n`);
    }
    process.stderr.write(`Security configuration check failed with ${errors.length} error(s).\n`);
    process.exitCode = 1;
    return;
  }
  process.stdout.write(
    `Security configuration passed: ${stats.dependabotEcosystems} Dependabot ecosystems, ` +
      `${stats.codeqlCategories} CodeQL categories, and ` +
      `${stats.actionReferences} SHA-pinned action uses.\n`,
  );
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  main();
}
