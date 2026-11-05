import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const appCSS = readFileSync(
  resolve(process.cwd(), "src/styles/app.css"),
  "utf8"
);

function declarationsFor(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = appCSS.match(
    new RegExp(`${escapedSelector}\\s*\\{(?<declarations>[^}]*)\\}`)
  );

  if (!match?.groups?.declarations) {
    throw new Error(`CSS rule not found: ${selector}`);
  }
  return match.groups.declarations;
}

describe("toast vertical alignment", () => {
  it("centers the status icon, copy, and dismiss action on one axis", () => {
    expect(declarationsFor(".app-toast")).toMatch(
      /\balign-items\s*:\s*center\s*;/
    );
    expect(declarationsFor(".toast-copy")).toMatch(
      /\balign-content\s*:\s*center\s*;/
    );
  });
});
