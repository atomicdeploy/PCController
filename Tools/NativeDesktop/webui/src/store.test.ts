import { describe, expect, it } from "vitest";
import {
  appPhase,
  applyBootstrap,
  applySnapshot,
  config,
  configureSettingsPersistence,
  controllerAvailable,
  derivePhase,
  diagnostics,
  flushConfigPatch,
  isOnline,
  markControllerUnavailable,
  snapshot,
  updateConfig,
  webTheme
} from "./store";
import type { RayanLampAPI } from "./api";
import type { UIConfigResult } from "./contracts";
import {
  testBootstrap,
  testConfig,
  testSnapshot
} from "./test/fixtures";
import { vi } from "vitest";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

describe("application store", () => {
  it("applies the shared native theme and parsed diagnostics from bootstrap", () => {
    const bootstrap = structuredClone(testBootstrap);
    bootstrap.config.ui.theme = "dark";
    applyBootstrap(bootstrap);

    expect(config.value?.ui.theme).toBe("dark");
    expect(webTheme.value).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(snapshot.value).toEqual(testSnapshot);
    expect(diagnostics.value?.device_url).toBe("http://rayanlamp.local");
    expect(appPhase.value).toBe("online");
  });

  it("derives degraded and reconnecting states without inventing lamp state", () => {
    expect(
      derivePhase({
        ...testSnapshot,
        websocket_online: false,
        http_reachable: true
      })
    ).toBe("degraded");
    expect(
      derivePhase({
        ...testSnapshot,
        websocket_online: false,
        http_reachable: false,
        reconnect_attempt: 2
      })
    ).toBe("connecting");
  });

  it("invalidates stale lamp connectivity as soon as the coordinator exits", () => {
    applyBootstrap(structuredClone(testBootstrap));
    expect(controllerAvailable.value).toBe(true);
    expect(isOnline.value).toBe(true);

    markControllerUnavailable("The RayanLamp controller shut down.");

    expect(controllerAvailable.value).toBe(false);
    expect(isOnline.value).toBe(false);
    expect(appPhase.value).toBe("offline");
    expect(snapshot.value.state).toBe(testSnapshot.state);
    expect(snapshot.value.http_reachable).toBe(false);
    expect(snapshot.value.websocket_online).toBe(false);
    expect(snapshot.value.last_error).toBe(
      "The RayanLamp controller shut down."
    );

    applySnapshot(structuredClone(testSnapshot));
    expect(controllerAvailable.value).toBe(true);
    expect(isOnline.value).toBe(true);
    expect(appPhase.value).toBe("online");
  });

  it("waits for an existing save and drains a newer immediate patch before resolving", async () => {
    vi.useFakeTimers();
    try {
      applyBootstrap(structuredClone(testBootstrap));
      const first = deferred<UIConfigResult>();
      const second = deferred<UIConfigResult>();
      const patchConfig = vi
        .fn()
        .mockReturnValueOnce(first.promise)
        .mockReturnValueOnce(second.promise);
      configureSettingsPersistence({
        patchConfig,
        getConfig: vi.fn()
      } as unknown as RayanLampAPI);

      updateConfig({ ui: { always_on_top: true } }, true);
      const firstFlush = flushConfigPatch();
      expect(patchConfig).toHaveBeenCalledTimes(1);

      updateConfig({ hotkeys: { toggle: "Ctrl+Shift+K" } }, true);
      let joinedFlushSettled = false;
      const joinedFlush = flushConfigPatch().then(() => {
        joinedFlushSettled = true;
      });
      await Promise.resolve();
      expect(joinedFlushSettled).toBe(false);

      const firstConfig = structuredClone(testConfig);
      firstConfig.ui.always_on_top = true;
      first.resolve({ config: firstConfig, revision: 8 });
      await vi.waitFor(() => expect(patchConfig).toHaveBeenCalledTimes(2));
      expect(patchConfig.mock.calls[1][0]).toEqual({
        hotkeys: { toggle: "Ctrl+Shift+K" }
      });
      expect(joinedFlushSettled).toBe(false);

      const secondConfig = structuredClone(firstConfig);
      secondConfig.hotkeys.toggle = "Ctrl+Shift+K";
      second.resolve({ config: secondConfig, revision: 9 });
      await Promise.all([firstFlush, joinedFlush]);

      expect(joinedFlushSettled).toBe(true);
      expect(config.value?.hotkeys.toggle).toBe("Ctrl+Shift+K");
    } finally {
      vi.useRealTimers();
    }
  });
});
