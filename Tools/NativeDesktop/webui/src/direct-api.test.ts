import compatibilityProtocol from "../../compatibility/device-protocol.json";
import { describe, expect, it, vi } from "vitest";
import { APIError } from "./api";
import type { BrowserAudioPlayback } from "./audio";
import type { Snapshot } from "./contracts";
import {
  DirectDeviceAPI,
  directPageAccessError
} from "./direct-api";
import { DIRECT_DIAGNOSTIC_ENDPOINTS } from "./direct-protocol";
import { subscribeConnectionPreferences } from "./connection-preferences";

function textResponse(value: string, status = 200): Response {
  return new Response(value, { status });
}

describe("DirectDeviceAPI", () => {
  it("bootstraps with CORS-simple GETs and parsed audited diagnostics", async () => {
    const requests: Array<[RequestInfo | URL, RequestInit | undefined]> = [];
    const fetcher = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        requests.push([input, init]);
        const url = String(input);
        if (url.endsWith("" + compatibilityProtocol.statePrefix + "get")) return textResponse("OFF");
        if (url.endsWith("/info")) {
          return textResponse("hostname: pccontroller-desktop\nfirmware: 2.1.0");
        }
        if (url.endsWith("/wifi/signal")) return textResponse("-62\nGood");
        if (url.endsWith("/wifi/status")) {
          return textResponse("ap status: 1\nst status: 1");
        }
        if (url.endsWith("/config/st")) {
          return textResponse(
            "ssid: Office\npass: station-plaintext-secret\n" +
              "dhcp: enabled\nip: 172.20.5.130\nnetmask: 255.255.255.0"
          );
        }
        if (url.endsWith("/config/ap")) {
          return textResponse(
            "ssid: PCControllerDesktop Setup\npass: ap-plaintext-secret\n" +
              "ip: 192.168.4.1\nnetmask: 255.255.255.0"
          );
        }
        return textResponse("");
      }
    );
    const api = new DirectDeviceAPI("pccontroller-desktop.local", {
      fetch: fetcher as typeof fetch,
      pageURL: "http://dashboard.local/"
    });

    const bootstrap = await api.bootstrap();

    expect(bootstrap.snapshot).toMatchObject({
      state: "off",
      http_reachable: true,
      device_url: "http://pccontroller-desktop.local"
    });
    expect(bootstrap.capabilities).toMatchObject({
      transport_direct: true,
      transport_relay: false,
      native_shell: false,
      device_terminal: false,
      audio_sfx: true
    });
    expect(bootstrap.diagnostics?.device).toMatchObject({
      hostname: "pccontroller-desktop",
      firmware: "2.1.0",
      rssi_dbm: -62,
      wifi_connected: true,
      ssid: "Office",
      wifi_ip: "172.20.5.130",
      station_status: "Connected",
      access_point_status: "Enabled",
      access_point_ssid: "PCControllerDesktop Setup",
      access_point_ip: "192.168.4.1"
    });
    expect(Object.keys(bootstrap.diagnostics?.endpoints ?? {})).toEqual(
      expect.arrayContaining([...DIRECT_DIAGNOSTIC_ENDPOINTS])
    );
    expect(bootstrap.diagnostics?.endpoints).not.toHaveProperty("/config/st");
    expect(bootstrap.diagnostics?.endpoints).not.toHaveProperty("/config/ap");
    expect(JSON.stringify(bootstrap)).not.toContain("station-plaintext-secret");
    expect(JSON.stringify(bootstrap)).not.toContain("ap-plaintext-secret");
    expect(requests).toHaveLength(
      1 + DIRECT_DIAGNOSTIC_ENDPOINTS.length + 2
    );
    for (const [, init] of requests) {
      expect(init?.method).toBe("GET");
      expect(init?.mode).toBe("cors");
      expect(init?.credentials).toBe("omit");
      expect(init?.headers).toBeUndefined();
      expect(init?.body).toBeUndefined();
    }
  });

  it("opens while offline and preserves an actionable failure", async () => {
    const api = new DirectDeviceAPI("pccontroller-desktop.local", {
      fetch: vi.fn(async () => {
        throw new TypeError("network");
      }) as typeof fetch,
      pageURL: "http://dashboard.local/"
    });

    const bootstrap = await api.bootstrap();

    expect(bootstrap.snapshot.http_reachable).toBe(false);
    expect(bootstrap.snapshot.state).toBe("unknown");
    expect(bootstrap.snapshot.last_error).toMatch(/could not reach/i);
    expect(
      Object.values(bootstrap.diagnostics?.endpoints ?? {})
    ).toHaveLength(DIRECT_DIAGNOSTIC_ENDPOINTS.length);
  });

  it("toggles from the confirmed state without using an unaudited endpoint", async () => {
    const urls: string[] = [];
    const api = new DirectDeviceAPI("http://pccontroller-desktop.local", {
      fetch: vi.fn(async (input: RequestInfo | URL) => {
        urls.push(String(input));
        if (String(input).endsWith("" + compatibilityProtocol.statePrefix + "get")) return textResponse("OFF");
        if (String(input).endsWith("" + compatibilityProtocol.statePrefix + "off")) return textResponse("");
        return textResponse("ON");
      }) as typeof fetch,
      pageURL: "http://dashboard.local/"
    });
    await api.status();

    const result = (await api.command("toggle")) as Snapshot;
    const off = (await api.command("off")) as Snapshot;
    const on = (await api.command("on")) as Snapshot;

    expect(result.state).toBe("on");
    expect(off.state).toBe("off");
    expect(on.state).toBe("on");
    expect(urls).toEqual([
      "http://pccontroller-desktop.local" + compatibilityProtocol.statePrefix + "get",
      "http://pccontroller-desktop.local" + compatibilityProtocol.statePrefix + "on",
      "http://pccontroller-desktop.local" + compatibilityProtocol.statePrefix + "off",
      "http://pccontroller-desktop.local" + compatibilityProtocol.statePrefix + "on"
    ]);
  });

  it("normalizes firmware WebSocket state, message, msg, and beep frames", () => {
    const api = new DirectDeviceAPI("http://pccontroller-desktop.local", {
      pageURL: "http://dashboard.local/"
    });
    const socket = {
      send: vi.fn(),
      close: vi.fn()
    } as unknown as WebSocket;

    expect(api.realtimeURL()).toBe("ws://pccontroller-desktop.local/ws");
    expect(api.realtimeOpened(socket)[0].snapshot?.websocket_online).toBe(true);
    expect(socket.send).toHaveBeenCalledWith("get");
    expect(api.decodeRealtimeMessage("ON")[0].snapshot?.state).toBe("on");
    expect(api.decodeRealtimeMessage("message:Hello")[0]).toMatchObject({
      type: "message",
      message: "Hello"
    });
    expect(api.decodeRealtimeMessage("msg:4,Hello+device")[0]).toMatchObject({
      message: "Hello device",
      result: { kind: "message", icon: 4 }
    });
    expect(api.decodeRealtimeMessage("beep:2")[0]).toMatchObject({
      type: "message",
      result: { kind: "beep", count: 2 }
    });
    expect(api.decodeRealtimeMessage("unrelated:asa")).toEqual([]);
    expect(api.realtimeClosed()[0].snapshot).toMatchObject({
      websocket_online: false,
      reconnect_attempt: 1
    });
  });

  it("persists browser-local settings and enforces the diagnostic allowlist", async () => {
    const fetcher = vi.fn(async () => textResponse("ok"));
    const announcements: string[] = [];
    const unsubscribe = subscribeConnectionPreferences((value) => {
      announcements.push(value.deviceURL);
    });
    const api = new DirectDeviceAPI("http://pccontroller-desktop.local", {
      fetch: fetcher as typeof fetch,
      pageURL: "http://dashboard.local/"
    });
    const changed = await api.patchConfig(
      {
        device: {
          request_timeout: "7s",
          http_poll: "0s",
          telnet_port: 2_323
        }
      },
      1
    );
    const reloaded = new DirectDeviceAPI("http://pccontroller-desktop.local", {
      fetch: fetcher as typeof fetch,
      pageURL: "http://dashboard.local/"
    });

    expect(changed.config.device.request_timeout).toBe("7s");
    expect(changed.config.device.http_poll).toBe("0s");
    expect(changed.config.device.telnet_port).toBe(2_323);
    expect(announcements).toContain("http://pccontroller-desktop.local");
    expect((await reloaded.getConfig()).config.device.request_timeout).toBe("7s");
    expect((await reloaded.getConfig()).config.device.telnet_port).toBe(2_323);
    await expect(reloaded.diagnostics("/pu")).rejects.toMatchObject({
      code: "endpoint_not_allowed"
    });
    await expect(reloaded.diagnostics("/config/st")).rejects.toMatchObject({
      code: "endpoint_not_allowed"
    });
    await expect(reloaded.diagnostics("/config/ap")).rejects.toMatchObject({
      code: "endpoint_not_allowed"
    });
    expect(fetcher).not.toHaveBeenCalled();
    await expect(reloaded.command("window/show")).rejects.toMatchObject({
      code: "native_feature_unavailable"
    });
    unsubscribe();
  });

  it("validates the coordinator terminal port even in browser-local config", async () => {
    for (const telnet_port of [0, 65_536, 23.5]) {
      const api = new DirectDeviceAPI("http://pccontroller-desktop.local", {
        pageURL: "http://dashboard.local/"
      });
      await expect(
        api.patchConfig({ device: { telnet_port } }, 1)
      ).rejects.toMatchObject({
        code: "invalid_config"
      });
    }
  });

  it("plays in-browser cues and imports a content-addressed custom WAV", async () => {
    const play = vi.fn(async () => undefined);
    const importCustomAudio = vi.fn(async () => ({
      id: "a".repeat(64),
      name: "Soft switch.wav",
      bytes: 1_024,
      channels: 1,
      sampleRate: 44_100,
      bitsPerSample: 16,
      dataBytes: 980,
      durationMs: 120,
      importedAt: "2026-07-24T00:00:00Z"
    }));
    const audio = {
      play,
      importCustomAudio,
      describe: vi.fn(async () => null),
      armUserGestureUnlock: vi.fn(() => () => undefined)
    } satisfies BrowserAudioPlayback;
    const api = new DirectDeviceAPI("http://pccontroller-desktop.local", {
      pageURL: "http://dashboard.local/",
      audio
    });

    await api.playAudio("on", 2);
    const file = new File([new Uint8Array([1])], "Soft switch.wav", {
      type: "audio/wav"
    });
    const imported = await api.importAudio(file, 1);

    expect(play).toHaveBeenCalledWith(
      "on",
      expect.objectContaining({ sound_source: "bundled" }),
      2
    );
    expect(importCustomAudio).toHaveBeenCalledWith(file);
    expect(imported.config.notifications).toMatchObject({
      sound: true,
      sound_source: "custom",
      sound_file: "a".repeat(64),
      sound_name: "Soft switch.wav"
    });

    await api.playAudio("off");
    const repaired = await api.getConfig();
    expect(audio.describe).toHaveBeenCalledWith("a".repeat(64));
    expect(play).toHaveBeenLastCalledWith(
      "off",
      expect.objectContaining({
        sound_source: "bundled",
        sound_file: "",
        sound_name: ""
      }),
      1
    );
    expect(repaired).toMatchObject({
      revision: 3,
      config: {
        notifications: {
          sound_source: "bundled",
          sound_file: "",
          sound_name: ""
        }
      }
    });
  });

  it("rejects invalid themes and recovers safely from malformed stored themes", async () => {
    const api = new DirectDeviceAPI("http://pccontroller-desktop.local", {
      pageURL: "http://dashboard.local/"
    });
    await expect(
      api.patchConfig(
        { ui: { theme: "sepia" as "dark" } },
        1
      )
    ).rejects.toMatchObject({
      code: "invalid_config"
    });

    const malformed = (await api.getConfig()).config;
    malformed.ui.theme = "sepia" as "dark";
    localStorage.setItem(
      "pccontroller-desktop.web.direct.config.v1",
      JSON.stringify(malformed)
    );
    const reloaded = new DirectDeviceAPI("http://pccontroller-desktop.local", {
      pageURL: "http://dashboard.local/"
    });

    expect((await reloaded.getConfig()).config.ui.theme).toBe("system");
  });
});

describe("direct browser security diagnostics", () => {
  it("explains file origins and HTTPS-to-HTTP mixed content", () => {
    expect(
      directPageAccessError(
        "file:///C:/PCControllerDesktop/index.html",
        "http://pccontroller-desktop.local"
      )
    ).toMatchObject({ code: "file_origin_unsupported" });
    expect(
      directPageAccessError(
        "https://dashboard.example/",
        "http://pccontroller-desktop.local"
      )
    ).toMatchObject({ code: "mixed_content" });
    expect(
      directPageAccessError(
        "http://dashboard.local/",
        "http://pccontroller-desktop.local"
      )
    ).toBeNull();
    expect(
      directPageAccessError(
        "https://dashboard.example/",
        "https://pccontroller-desktop.local"
      )
    ).toBeNull();
  });

  it("returns typed errors", () => {
    const error = directPageAccessError(
      "file:///index.html",
      "http://pccontroller-desktop.local"
    );
    expect(error).toBeInstanceOf(APIError);
  });
});
