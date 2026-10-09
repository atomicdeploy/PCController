import assert from "node:assert/strict";
import test from "node:test";

import {
  normalizeRepository,
  repositoryWebUrl,
  resolveRepository,
} from "./repository-context.mjs";

test("repository identity prefers the GitHub Actions environment", () => {
  let called = false;
  const repository = resolveRepository(
    { GITHUB_REPOSITORY: "example-owner/example-project" },
    { run: () => { called = true; } },
  );
  assert.equal(repository, "example-owner/example-project");
  assert.equal(called, false);
});

test("repository identity falls back to authenticated gh metadata", () => {
  const repository = resolveRepository({}, {
    cwd: "fixture",
    run(command, args, options) {
      assert.equal(command, "gh");
      assert.deepEqual(args, [
        "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner",
      ]);
      assert.equal(options.cwd, "fixture");
      return { status: 0, stdout: "dynamic-owner/dynamic-project\n", stderr: "" };
    },
  });
  assert.equal(repository, "dynamic-owner/dynamic-project");
});

test("repository identities and web URLs are validated", () => {
  assert.equal(
    repositoryWebUrl("owner/project", { GITHUB_SERVER_URL: "https://git.example/" }),
    "https://git.example/owner/project",
  );
  assert.throws(() => normalizeRepository("not a repository"), /invalid GitHub repository/u);
});
