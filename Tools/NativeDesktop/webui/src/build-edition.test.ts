import { describe, expect, it } from "vitest";
import { buildEdition, developerTools } from "./build-edition";

describe("build edition", () => {
  it("keeps the compile-time edition and developer payload in lockstep", () => {
    expect(["develop", "production"]).toContain(buildEdition);
    expect(developerTools).toBe(buildEdition === "develop");
  });
});
