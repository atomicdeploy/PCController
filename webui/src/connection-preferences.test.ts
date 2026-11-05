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
      lampURL: "http://rayanlamp.local"
    });
  });

  it("defaults a static export to direct lamp control", () => {
    vi.stubEnv("VITE_RAYANLAMP_STATIC_EXPORT", "true");
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
      lampURL: "http://lamp.example"
    });
    stop();

    expect(readConnectionPreferences()).toEqual({
      mode: "direct",
      lampURL: "http://lamp.example"
    });
    expect(received).toEqual([
      { mode: "direct", lampURL: "http://lamp.example" }
    ]);
  });

  it("fails closed to defaults for malformed storage", () => {
    localStorage.setItem("rayanlamp.web.connection.v1", "{broken");
    expect(readConnectionPreferences()).toEqual(
      defaultConnectionPreferences()
    );
  });

  it("fails closed to defaults for an unsafe stored lamp target", () => {
    localStorage.setItem(
      "rayanlamp.web.connection.v1",
      JSON.stringify({
        mode: "direct",
        lampURL: "https://user:secret@lamp.example/path"
      })
    );
    expect(readConnectionPreferences()).toEqual(
      defaultConnectionPreferences()
    );
  });
});
