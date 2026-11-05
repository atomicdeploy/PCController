import { afterEach, describe, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  applyWebTheme,
  resolvedWebTheme,
  startWebThemeController,
  webTheme
} from "./theme";

interface ControllableMedia {
  media: MediaQueryList;
  setMatches(matches: boolean): void;
}

function installMatchMedia(initial: boolean): ControllableMedia {
  let matches = initial;
  const listeners = new Set<(event: MediaQueryListEvent) => void>();
  const media = {
    get matches() {
      return matches;
    },
    media: "(prefers-color-scheme: light)",
    onchange: null,
    addEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject
    ) {
      if (type === "change" && typeof listener === "function") {
        listeners.add(listener as (event: MediaQueryListEvent) => void);
      }
    },
    removeEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject
    ) {
      if (type === "change" && typeof listener === "function") {
        listeners.delete(listener as (event: MediaQueryListEvent) => void);
      }
    },
    addListener() {},
    removeListener() {},
    dispatchEvent() {
      return true;
    }
  } as MediaQueryList;
  vi.stubGlobal("matchMedia", vi.fn(() => media));
  return {
    media,
    setMatches(next) {
      matches = next;
      const event = { matches, media: media.media } as MediaQueryListEvent;
      for (const listener of listeners) listener(event);
    }
  };
}

function installThemeMetadata(): void {
  document.head.innerHTML = `
    <meta name="color-scheme" content="light dark">
    <meta name="theme-color" content="#0b1724">
  `;
}

afterEach(() => {
  vi.unstubAllGlobals();
  document.documentElement.removeAttribute("data-theme");
  document.documentElement.removeAttribute("data-resolved-theme");
  document.documentElement.style.removeProperty("color-scheme");
  document.head.innerHTML = "";
  webTheme.value = "system";
  resolvedWebTheme.value = "dark";
});

describe("web theme controller", () => {
  it("resolves system mode live and updates browser chrome metadata", () => {
    installThemeMetadata();
    const media = installMatchMedia(false);
    const stop = startWebThemeController();
    applyWebTheme("system");

    expect(webTheme.value).toBe("system");
    expect(resolvedWebTheme.value).toBe("dark");
    expect(document.documentElement.dataset).toMatchObject({
      theme: "system",
      resolvedTheme: "dark"
    });
    expect(document.documentElement.style.colorScheme).toBe("dark");
    expect(
      document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
        ?.content
    ).toBe("#0b1724");

    media.setMatches(true);

    expect(resolvedWebTheme.value).toBe("light");
    expect(document.documentElement.dataset.resolvedTheme).toBe("light");
    expect(document.documentElement.style.colorScheme).toBe("light");
    expect(
      document.querySelector<HTMLMetaElement>('meta[name="color-scheme"]')
        ?.content
    ).toBe("light");
    expect(
      document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
        ?.content
    ).toBe("#eef4f8");
    stop();
  });

  it("keeps an explicit override stable across system changes", () => {
    installThemeMetadata();
    const media = installMatchMedia(false);
    const stop = startWebThemeController();

    applyWebTheme("light");
    media.setMatches(false);

    expect(webTheme.value).toBe("light");
    expect(resolvedWebTheme.value).toBe("light");
    expect(localStorage.getItem("pccontroller-desktop.web.theme")).toBe("light");
    expect(
      document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
        ?.content
    ).toBe("#eef4f8");
    stop();
  });
});

describe("pre-paint theme bootstrap", () => {
  it("loads a same-origin classic script before the application module", () => {
    const index = readFileSync(
      resolve(process.cwd(), "index.html"),
      "utf8"
    );
    const bootstrap = readFileSync(
      resolve(process.cwd(), "public", "theme-init.js"),
      "utf8"
    );
    const themeScript = index.indexOf(
      '<script src="/theme-init.js"></script>'
    );
    const appModule = index.indexOf(
      '<script type="module" src="./src/main.tsx"></script>'
    );

    expect(themeScript).toBeGreaterThan(0);
    expect(appModule).toBeGreaterThan(themeScript);
    expect(bootstrap).toContain("pccontroller-desktop.web.theme");
    expect(bootstrap).toContain("prefers-color-scheme: light");
    expect(bootstrap).not.toContain("import ");
  });
});
