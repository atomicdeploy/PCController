import { describe, expect, it, vi } from "vitest";
import { WebHaptics, type WebHapticsRuntime } from "./haptics";

function runtime(
  overrides: Partial<WebHapticsRuntime> = {}
): WebHapticsRuntime {
  return {
    vibrate: vi.fn(() => true),
    isDocumentVisible: () => true,
    prefersReducedMotion: () => false,
    ...overrides
  };
}

describe("mobile Web UI haptics", () => {
  it("uses short semantic patterns for command lifecycle feedback", () => {
    const environment = runtime();
    const haptics = new WebHaptics(environment);

    expect(haptics.trigger("command.start")).toBe(true);
    expect(haptics.trigger("command.success")).toBe(true);
    expect(haptics.trigger("command.error")).toBe(true);

    expect(environment.vibrate).toHaveBeenNthCalledWith(1, 12);
    expect(environment.vibrate).toHaveBeenNthCalledWith(
      2,
      [15, 35, 24]
    );
    expect(environment.vibrate).toHaveBeenNthCalledWith(
      3,
      [28, 42, 28]
    );
  });

  it("projects normalized message and audio cues without raw payloads", () => {
    const environment = runtime();
    const haptics = new WebHaptics(environment);

    haptics.trigger("message.received");
    haptics.audioCue(2);
    haptics.audioCue(1_000);

    expect(environment.vibrate).toHaveBeenNthCalledWith(
      1,
      [18, 55, 18]
    );
    expect(environment.vibrate).toHaveBeenNthCalledWith(2, [20, 65, 20]);
    expect(environment.vibrate).toHaveBeenNthCalledWith(
      3,
      [20, 65, 20, 65, 20]
    );
  });

  it("does nothing when vibration is unavailable", () => {
    const haptics = new WebHaptics(
      runtime({ vibrate: undefined })
    );

    expect(haptics.supported).toBe(false);
    expect(haptics.trigger("command.start")).toBe(false);
    expect(haptics.audioCue(1)).toBe(false);
  });

  it("respects hidden documents and reduced-motion preference", () => {
    const hidden = runtime({ isDocumentVisible: () => false });
    const reduced = runtime({ prefersReducedMotion: () => true });

    expect(new WebHaptics(hidden).trigger("command.start")).toBe(false);
    expect(new WebHaptics(reduced).trigger("command.start")).toBe(false);
    expect(hidden.vibrate).not.toHaveBeenCalled();
    expect(reduced.vibrate).not.toHaveBeenCalled();
  });

  it("contains host API failures and rejects invalid audio counts", () => {
    const failed = runtime({
      vibrate: vi.fn(() => {
        throw new DOMException("Not allowed", "NotAllowedError");
      })
    });
    const haptics = new WebHaptics(failed);

    expect(haptics.trigger("command.error")).toBe(false);
    expect(haptics.audioCue(0)).toBe(false);
    expect(haptics.audioCue(Number.NaN)).toBe(false);
    expect(failed.vibrate).toHaveBeenCalledTimes(1);
  });

  it("fails closed when visibility or preference checks throw", () => {
    const visibilityFailure = runtime({
      isDocumentVisible: () => {
        throw new Error("unavailable");
      }
    });
    const preferenceFailure = runtime({
      prefersReducedMotion: () => {
        throw new Error("unavailable");
      }
    });

    expect(
      new WebHaptics(visibilityFailure).trigger("command.start")
    ).toBe(false);
    expect(
      new WebHaptics(preferenceFailure).trigger("command.start")
    ).toBe(false);
    expect(visibilityFailure.vibrate).not.toHaveBeenCalled();
    expect(preferenceFailure.vibrate).not.toHaveBeenCalled();
  });
});
