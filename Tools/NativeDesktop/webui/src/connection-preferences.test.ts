import { describe, expect, it, vi } from "vitest";
import {
  defaultConnectionPreferences,
  readConnectionPreferences,
  saveConnectionPreferences,
  subscribeConnectionPreferences
} from "./connection-preferences";

describe("connection preferences", () => {
  it("defaults the embedded build to automatic relay-first selection", () => {
    expect(defaultConnectionPreferences()).toEqual({
      mode: "auto",
      deviceURL: "http://pccontroller-desktop.local"
    });
  });

  it("defaults a static export to direct device control", () => {
    vi.stubEnv("VITE_PCCONTROLLER_DESKTOP_STATIC_EXPORT", "true");
    expect(defaultConnectionPreferences().mode).toBe("direct");
    vi.unstubAllEnvs();
  });

  it("persists and immediately announces a mode change in this tab", () => {
    const received: unknown[] = [];
    const stop = subscribeConnectionPreferences((value) => {
      received.push(value);
    });
    saveConnectionPreferences({
      mode: "direct",
      deviceURL: "http://device.example"
    });
    stop();

    expect(readConnectionPreferences()).toEqual({
      mode: "direct",
      deviceURL: "http://device.example"
    });
    expect(received).toEqual([
      { mode: "direct", deviceURL: "http://device.example" }
    ]);
  });

  it("fails closed to defaults for malformed storage", () => {
    localStorage.setItem("pccontroller-desktop.web.connection.v1", "{broken");
    expect(readConnectionPreferences()).toEqual(
      defaultConnectionPreferences()
    );
  });

  it("fails closed to defaults for an unsafe stored device target", () => {
    localStorage.setItem(
      "pccontroller-desktop.web.connection.v1",
      JSON.stringify({
        mode: "direct",
        deviceURL: "https://user:secret@device.example/path"
      })
    );
    expect(readConnectionPreferences()).toEqual(
      defaultConnectionPreferences()
    );
  });
});
