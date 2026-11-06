import { describe, expect, it, vi } from "vitest";
import {
  AutoPCControllerDesktopClient,
  createPCControllerDesktopClient
} from "./client-factory";
import { DirectDeviceAPI } from "./direct-api";
import { PCControllerDesktopAPI, type PCControllerDesktopClient } from "./api";
import { testBootstrap } from "./test/fixtures";

describe("client factory", () => {
  it("honors explicit modes without wrapping them in fallback", () => {
    expect(
      createPCControllerDesktopClient({
        mode: "relay",
        deviceURL: "http://pccontroller-desktop.local"
      })
    ).toBeInstanceOf(PCControllerDesktopAPI);
    expect(
      createPCControllerDesktopClient({
        mode: "direct",
        deviceURL: "http://pccontroller-desktop.local"
      })
    ).toBeInstanceOf(DirectDeviceAPI);
    expect(
      createPCControllerDesktopClient({
        mode: "auto",
        deviceURL: "http://pccontroller-desktop.local"
      })
    ).toBeInstanceOf(AutoPCControllerDesktopClient);
  });

  it("keeps the relay when same-origin bootstrap succeeds", async () => {
    const relay = fakeClient("relay", vi.fn(async () => testBootstrap));
    const directBootstrap = vi.fn(async () => testBootstrap);
    const direct = fakeClient("direct", directBootstrap);
    const api = new AutoPCControllerDesktopClient(relay, direct);

    await expect(api.bootstrap()).resolves.toBe(testBootstrap);

    expect(api.kind).toBe("relay");
    expect(api.displayName).toBe("relay client");
    expect(directBootstrap).not.toHaveBeenCalled();
  });

  it("falls back to direct only in automatic mode", async () => {
    const relay = fakeClient(
      "relay",
      vi.fn(async () => {
        throw new Error("no coordinator");
      })
    );
    const direct = fakeClient("direct", vi.fn(async () => testBootstrap));
    const api = new AutoPCControllerDesktopClient(relay, direct);

    await expect(api.bootstrap()).resolves.toBe(testBootstrap);

    expect(api.kind).toBe("direct");
    expect(api.syncKey).toBe("direct:key");
    await api.status();
    expect(direct.status).toHaveBeenCalledOnce();
  });
});

function fakeClient(
  kind: "relay" | "direct",
  bootstrap: PCControllerDesktopClient["bootstrap"]
): PCControllerDesktopClient {
  return {
    kind,
    displayName: `${kind} client`,
    syncKey: `${kind}:key`,
    bootstrap,
    establishSession: vi.fn(async () => ""),
    getConfig: vi.fn(),
    patchConfig: vi.fn(),
    beginHotkeyCapture: vi.fn(),
    renewHotkeyCapture: vi.fn(),
    releaseHotkeyCapture: vi.fn(),
    probeHotkey: vi.fn(),
    integrationRegistrationStatus: vi.fn(),
    updateIntegrationRegistration: vi.fn(),
    status: vi.fn(async () => testBootstrap.snapshot),
    command: vi.fn(),
    diagnostics: vi.fn(),
    uiDiagnostics: vi.fn(),
    refreshUIDiagnostics: vi.fn(),
    notify: vi.fn(),
    playAudio: vi.fn(),
    importAudio: vi.fn(),
    realtimeURL: vi.fn(() => "ws://example/ws"),
    realtimeOpened: vi.fn(() => []),
    realtimeClosed: vi.fn(() => []),
    decodeRealtimeMessage: vi.fn(() => [])
  };
}
