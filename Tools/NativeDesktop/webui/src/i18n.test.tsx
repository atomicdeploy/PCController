import { render, screen } from "@testing-library/preact";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  BidiLtr,
  applyWebLocale,
  detectLocale,
  formatNumber,
  localizeDigits,
  normalizeLocalePreference,
  resolvedLocale,
  startWebLocaleController,
  t
} from "./i18n";

describe("Web UI internationalization", () => {
  afterEach(() => {
    applyWebLocale("en");
  });

  it("normalizes persisted and backward-compatible locale values", () => {
    expect(normalizeLocalePreference("auto")).toBe("auto");
    expect(normalizeLocalePreference("en")).toBe("en");
    expect(normalizeLocalePreference("fa")).toBe("fa");
    expect(normalizeLocalePreference(undefined)).toBe("auto");
    expect(normalizeLocalePreference("fa-IR")).toBe("auto");
  });

  it("detects Persian without treating every RTL language as Persian", () => {
    expect(detectLocale(["fa-IR", "en-US"])).toBe("fa");
    expect(detectLocale(["ar", "en-US"])).toBe("en");
    expect(detectLocale(["en-US"])).toBe("en");
  });

  it("applies Persian language, direction, typography metadata, and digits", () => {
    applyWebLocale("fa");

    expect(resolvedLocale.value).toBe("fa");
    expect(document.documentElement.lang).toBe("fa-IR");
    expect(document.documentElement.dir).toBe("rtl");
    expect(document.documentElement.dataset.resolvedLocale).toBe("fa");
    expect(document.title).toBe("PCController");
    expect(localStorage.getItem("pccontroller-desktop.web.locale")).toBe("fa");
    expect(t("Settings")).toBe("تنظیمات");
    expect(t("Updated {date}", { date: "امروز" })).toBe("به‌روزرسانی امروز");
    expect(formatNumber(123456)).toMatch(/[۱۲۳۴۵۶]/);
    expect(localizeDigits("Port 47831")).toBe("Port ۴۷۸۳۱");
  });

  it("keeps protocol values in an explicit LTR isolation boundary", () => {
    applyWebLocale("fa");
    render(<BidiLtr>http://pccontroller-desktop.local:47831</BidiLtr>);

    const value = screen.getByText("http://pccontroller-desktop.local:47831");
    expect(value.tagName).toBe("BDI");
    expect(value.getAttribute("dir")).toBe("ltr");
  });

  it("re-resolves automatic locale after a browser language change", () => {
    const languages = vi
      .spyOn(window.navigator, "languages", "get")
      .mockReturnValue(["en-US"]);
    applyWebLocale("auto");
    expect(resolvedLocale.value).toBe("en");

    const stop = startWebLocaleController();
    languages.mockReturnValue(["fa-IR", "en-US"]);
    window.dispatchEvent(new Event("languagechange"));
    expect(resolvedLocale.value).toBe("fa");
    stop();
  });
});
