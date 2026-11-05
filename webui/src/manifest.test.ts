import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { rayanLampManifest } from "../manifest";

describe("installable mobile web app shell", () => {
  it("keeps install and icon URLs portable across /app/ and static exports", () => {
    expect(rayanLampManifest).toMatchObject({
      id: "./",
      start_url: "./",
      scope: "./",
      display: "standalone",
      orientation: "any",
      prefer_related_applications: false
    });
    expect(rayanLampManifest.icons).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          src: "icons/app-192.png",
          sizes: "192x192",
          purpose: "any"
        }),
        expect.objectContaining({
          src: "shared/icons/app.png",
          sizes: "512x512",
          purpose: "any"
        }),
        expect.objectContaining({
          src: "icons/app-maskable-512.png",
          sizes: "512x512",
          purpose: "maskable"
        })
      ])
    );
  });

  it("declares mobile and Apple standalone metadata without disabling zoom", () => {
    const markup = readFileSync(resolve(process.cwd(), "index.html"), "utf8");
    const page = new DOMParser().parseFromString(markup, "text/html");

    const meta = (name: string) =>
      page.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)
        ?.content;
    const viewport = meta("viewport");

    expect(meta("mobile-web-app-capable")).toBe("yes");
    expect(meta("apple-mobile-web-app-capable")).toBe("yes");
    expect(meta("apple-mobile-web-app-status-bar-style")).toBe(
      "black-translucent"
    );
    expect(meta("apple-mobile-web-app-title")).toBe("Rayan Lamp");
    expect(meta("format-detection")).toBe("telephone=no");
    expect(viewport).toContain("viewport-fit=cover");
    expect(viewport).toContain("interactive-widget=resizes-content");
    expect(viewport).not.toContain("user-scalable=no");
    expect(viewport).not.toContain("maximum-scale");

    const touchIcon = page.querySelector<HTMLLinkElement>(
      'link[rel="apple-touch-icon"]'
    );
    expect(touchIcon?.getAttribute("sizes")).toBe("180x180");
    expect(touchIcon?.getAttribute("href")).toBe("./icons/app-180.png");

    const icon = readFileSync(
      resolve(process.cwd(), "public", "icons", "app-180.png")
    );
    expect(icon.subarray(1, 4).toString("ascii")).toBe("PNG");
    expect(icon.readUInt32BE(16)).toBe(180);
    expect(icon.readUInt32BE(20)).toBe(180);
  });
});
