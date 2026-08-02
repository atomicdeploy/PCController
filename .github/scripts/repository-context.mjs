import { spawnSync } from "node:child_process";

const repositoryPattern = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/u;

export function normalizeRepository(value) {
  const repository = String(value || "").trim();
  if (!repositoryPattern.test(repository)) {
    throw new Error(`invalid GitHub repository identity: ${repository || "<empty>"}`);
  }
  return repository;
}

// Resolve operational repository identity from CI or the authenticated checkout.
export function resolveRepository(
  environment = process.env,
  { cwd = process.cwd(), run = spawnSync } = {},
) {
  if (String(environment.GITHUB_REPOSITORY || "").trim()) {
    return normalizeRepository(environment.GITHUB_REPOSITORY);
  }
  const result = run(
    "gh",
    ["repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner"],
    { cwd, encoding: "utf8", windowsHide: true, env: environment },
  );
  if (result?.status !== 0) {
    const detail = `${result?.stderr || ""}${result?.stdout || ""}`.trim();
    throw new Error(
      `unable to resolve repository; set GITHUB_REPOSITORY or authenticate gh${detail ? `: ${detail}` : ""}`,
    );
  }
  return normalizeRepository(result.stdout);
}

export function repositoryWebUrl(repository, environment = process.env) {
  const server = String(environment.GITHUB_SERVER_URL || "https://github.com")
    .trim()
    .replace(/\/$/u, "");
  return `${server}/${normalizeRepository(repository)}`;
}
