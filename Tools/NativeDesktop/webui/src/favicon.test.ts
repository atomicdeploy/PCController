import { describe, expect, it } from "vitest";
import {
  faviconState,
  faviconURL,
  startWebFaviconController,
  updateFavicon
} from "./favicon";
import {
  applyBootstrap,
  applySnapshot,
  markControllerUnavailable
} from "./store";
import {
  testBootstrap,
  testSnapshot
} from "./test/fixtures";

describe("state-reactive favicon", () => {
  it("maps lifecycle, availability, and confirmed lamp state", () => {
    expect(faviconState("on", "booting", false)).toBe("connecting");
    expect(faviconState("on", "connecting", true)).toBe("connecting");
    expect(faviconState("on", "online", true)).toBe("on");
    expect(faviconState("off", "degraded", true)).toBe("off");
    expect(faviconState("unknown", "online", true)).toBe("connecting");
    expect(faviconState("on", "offline", true)).toBe("offline");
    expect(faviconState("off", "online", false)).toBe("offline");
  });

  it("replaces the browser icon when state changes without cache ambiguity", () => {
    document.head.innerHTML = `
      <link
        rel="icon"
        href="./icons/favicon-connecting.svg"
        data-rayanlamp-favicon
        data-lamp-state="connecting"
      />
    `;
    const initial = document.querySelector(
      "link[data-rayanlamp-favicon]"
    );

    updateFavicon("on");
    const on = document.querySelector<HTMLLinkElement>(
      "link[data-rayanlamp-favicon]"
    );
    expect(on).not.toBe(initial);
    expect(on?.dataset.lampState).toBe("on");
    expect(on?.href).toBe(
      new URL(faviconURL("on"), document.baseURI).href
    );
    expect(on?.type).toBe("image/svg+xml");
    expect(on?.getAttribute("sizes")).toBe("any");

    updateFavicon("off");
    const off = document.querySelector<HTMLLinkElement>(
      "link[data-rayanlamp-favicon]"
    );
    expect(off).not.toBe(on);
    expect(off?.dataset.lampState).toBe("off");
    expect(off?.href).toBe(
      new URL(faviconURL("off"), document.baseURI).href
    );
    expect(off?.href).not.toBe(on?.href);
  });

  it("leaves the current icon node alone when state is unchanged", () => {
    document.head.innerHTML = "";
    updateFavicon("offline");
    const current = document.querySelector(
      "link[data-rayanlamp-favicon]"
    );
    updateFavicon("offline");
    expect(
      document.querySelector("link[data-rayanlamp-favicon]")
    ).toBe(current);
  });

  it("tracks live store changes without waiting for an App render", () => {
    document.head.innerHTML = "";
    const stop = startWebFaviconController();
    try {
      applyBootstrap(structuredClone(testBootstrap));
      expect(
        document.querySelector<HTMLLinkElement>(
          "link[data-rayanlamp-favicon]"
        )?.dataset.lampState
      ).toBe("off");

      applySnapshot({ ...testSnapshot, state: "on" });
      expect(
        document.querySelector<HTMLLinkElement>(
          "link[data-rayanlamp-favicon]"
        )?.dataset.lampState
      ).toBe("on");

      markControllerUnavailable();
      expect(
        document.querySelector<HTMLLinkElement>(
          "link[data-rayanlamp-favicon]"
        )?.dataset.lampState
      ).toBe("offline");
    } finally {
      stop();
    }
  });
});
