import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("canonical locale assets and responsive contract", () => {
  it("uses the reviewed Vazirmatn regular font as one canonical source", async () => {
    const font = await readFile(
      resolve(process.cwd(), "../assets/fonts/Vazirmatn-Regular.ttf")
    );
    expect(createHash("sha256").update(font).digest("hex")).toBe(
      "b69fd4c680b8f3f225feabcc655a2c585d97627b8f5f5c0f9985e894069f3a56"
    );

    const declaration = await readFile(
      resolve(process.cwd(), "public/locale-font.css"),
      "utf8"
    );
    expect(declaration).toContain('font-family: "Vazirmatn"');
    expect(declaration).toContain('url("./shared/fonts/Vazirmatn-Regular.ttf")');
    expect(declaration).toContain("font-weight: 400");
  });

  it("keeps explicit desktop, tablet, phone, and narrow-phone layouts", async () => {
    const css = await readFile(
      resolve(process.cwd(), "src/styles/app.css"),
      "utf8"
    );
    for (const breakpoint of ["1180px", "900px", "680px", "400px"]) {
      expect(css).toContain(`@media (max-width: ${breakpoint})`);
    }
    expect(css).toContain("env(safe-area-inset-bottom)");
    expect(css).toContain("grid-template-columns: repeat(5, minmax(0, 1fr))");
    expect(css).toContain("max-height: calc(100dvh - 18px)");
    expect(css).toContain("inset-inline-start: 0");
    expect(css).toContain('html[dir="rtl"]');

    const html = await readFile(
      resolve(process.cwd(), "index.html"),
      "utf8"
    );
    expect(html).toContain("interactive-widget=resizes-content");
  });
});
