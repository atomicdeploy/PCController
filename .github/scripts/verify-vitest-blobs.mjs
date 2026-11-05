#!/usr/bin/env node
import { appendFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { TARGETS } from "./codebase-summary.mjs";
import { loadProjectEnv } from "../../Tools/Build/env.mjs";

loadProjectEnv();

const directory = resolve(process.argv[2] || "build-summary/vitest");
const missing = TARGETS.filter(({ id }) => !existsSync(resolve(directory, `${id}.json`)));
if (missing.length) {
  const message = `WebUI test report incomplete: missing ${missing.map(({ id }) => id).join(", ")}.`;
  if (process.env.GITHUB_STEP_SUMMARY) {
    appendFileSync(process.env.GITHUB_STEP_SUMMARY, `\n> [!WARNING]\n> ${message}\n`);
  }
  console.error(message);
  process.exitCode = 1;
}
