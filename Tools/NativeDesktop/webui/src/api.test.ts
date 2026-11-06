import { describe, expect, it, vi } from "vitest";
import { APIError, PCControllerDesktopAPI, unwrapResponse } from "./api";
import { testBootstrap, testConfig } from "./test/fixtures";

function jsonResponse(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" }
  });
}

describe("PCControllerDesktopAPI", () => {
  it("establishes an HttpOnly-cookie session and applies CSRF only to mutations", async () => {
    const requests: Array<[RequestInfo | URL, RequestInit | undefined]> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        requests.push([input, init]);
        if (input === "/app/session") {
          return jsonResponse({
            ok: true,
            result: {
              csrf_token: "csrf-test",
              expires_at: "2026-07-24T00:00:00Z"
            }
          });
        }
        if (input === "/ui/bootstrap") {
          return jsonResponse({ ok: true, result: testBootstrap });
        }
        return jsonResponse({
          ok: true,
          result: { config: testConfig, revision: 8 }
        });
      })
    );

    const api = new PCControllerDesktopAPI();
    await api.bootstrap();
    await api.patchConfig({ ui: { theme: "dark" } }, 7);

    expect(requests[0][0]).toBe("/app/session");
    expect(requests[0][1]?.method).toBe("POST");
    expect(requests[0][1]?.credentials).toBe("same-origin");

    const bootstrapHeaders = new Headers(requests[1][1]?.headers);
    expect(bootstrapHeaders.has("X-PCControllerDesktop-CSRF")).toBe(false);

    const patchHeaders = new Headers(requests[2][1]?.headers);
    expect(patchHeaders.get("X-PCControllerDesktop-CSRF")).toBe("csrf-test");
    expect(patchHeaders.get("If-Match")).toBe("7");
    expect(String(requests[2][1]?.body)).not.toContain("local_api_token");
  });

  it("coalesces forced browser-session renewal during reconnect", async () => {
    let resolveResponse!: (response: Response) => void;
    const response = new Promise<Response>((resolve) => {
      resolveResponse = resolve;
    });
    const fetchMock = vi.fn(() => response);
    vi.stubGlobal("fetch", fetchMock);
    const api = new PCControllerDesktopAPI();

    const first = api.establishSession(true);
    const second = api.establishSession(true);
    expect(fetchMock).toHaveBeenCalledTimes(1);

    resolveResponse(
      jsonResponse({
        ok: true,
        result: {
          csrf_token: "csrf-renewed",
          expires_at: "2026-07-24T00:00:00Z"
        }
      })
    );
    await expect(Promise.all([first, second])).resolves.toEqual([
      "csrf-renewed",
      "csrf-renewed"
    ]);
  });

  it("turns structured failures into typed API errors", () => {
    expect(() =>
      unwrapResponse({
        ok: false,
        error: { code: "invalid_config", message: "Bad duration" }
      })
    ).toThrowError(APIError);
  });

  it("uses only the living versionless coordinator routes", async () => {
    const requests: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        requests.push(String(input));
        if (input === "/app/session") {
          return jsonResponse({
            ok: true,
            result: {
              csrf_token: "csrf-routes",
              expires_at: "2026-07-24T00:00:00Z"
            }
          });
        }
        return jsonResponse({ ok: true, result: {} });
      })
    );

    const api = new PCControllerDesktopAPI();
    await api.bootstrap();
    await api.getConfig();
    await api.patchConfig({ ui: { theme: "system" } }, 1);
    await api.status();
    for (const command of [
      "toggle",
      "on",
      "off",
      "refresh",
      "reconnect",
      "window/show",
      "window/hide"
    ] as const) {
      await api.command(command);
    }
    await api.diagnostics("/info");
    await api.uiDiagnostics();
    await api.refreshUIDiagnostics();
    await api.notify("Device", "Test");
    await api.playAudio();
    await api.importAudio(
      new File([new Uint8Array([1, 2, 3])], "test.wav", {
        type: "audio/wav"
      })
    );

    expect(requests).toEqual([
      "/app/session",
      "/ui/bootstrap",
      "/ui/config",
      "/ui/config",
      "/status",
      "/device/toggle",
      "/device/on",
      "/device/off",
      "/device/refresh",
      "/reconnect",
      "/window/show",
      "/window/hide",
      "/diagnostics?endpoint=%2Finfo",
      "/ui/diagnostics",
      "/ui/diagnostics/refresh",
      "/notifications",
      "/audio/play",
      "/ui/audio/import"
    ]);
    expect(requests.every((path) => !path.includes("/v1"))).toBe(true);
    expect(new URL(api.realtimeURL()).pathname).toBe("/events");
  });

  it("sends exact audio playback and content-addressed import requests", async () => {
    const requests: Array<[RequestInfo | URL, RequestInit | undefined]> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        requests.push([input, init]);
        if (input === "/app/session") {
          return jsonResponse({
            ok: true,
            result: {
              csrf_token: "csrf-audio",
              expires_at: "2026-07-24T00:00:00Z"
            }
          });
        }
        return jsonResponse({
          ok: true,
          result: { config: testConfig, revision: 8 }
        });
      })
    );

    const api = new PCControllerDesktopAPI();
    await api.playAudio("off", 2);
    const file = new File([new Uint8Array([82, 73, 70, 70])], "custom.wav", {
      type: "audio/wav"
    });
    await api.importAudio(file, 7);

    expect(requests.map(([path]) => path)).toEqual([
      "/app/session",
      "/audio/play",
      "/ui/audio/import"
    ]);
    expect(JSON.parse(String(requests[1][1]?.body))).toEqual({
      cue: "off",
      count: 2
    });
    const importHeaders = new Headers(requests[2][1]?.headers);
    expect(importHeaders.get("Content-Type")).toBe("audio/wav");
    expect(importHeaders.get("X-PCControllerDesktop-Filename")).toBe("custom.wav");
    expect(importHeaders.get("If-Match")).toBe("7");
    expect(importHeaders.get("X-PCControllerDesktop-CSRF")).toBe("csrf-audio");
    expect(requests[2][1]?.body).toBe(file);
  });

  it("manages and probes an authenticated hotkey capture lease", async () => {
    const requests: Array<[RequestInfo | URL, RequestInit | undefined]> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        requests.push([input, init]);
        if (input === "/app/session") {
          return jsonResponse({
            ok: true,
            result: {
              csrf_token: "csrf-hotkeys",
              expires_at: "2026-07-24T00:00:00Z"
            }
          });
        }
        const body = JSON.parse(String(init?.body ?? "{}")) as {
          lease_id?: string;
          chord?: string;
        };
        if (input === "/ui/hotkeys/probe") {
          return jsonResponse({
            ok: true,
            result: {
              lease_id: body.lease_id,
              expires_at: "2026-07-24T00:00:12Z",
              lease_ttl_ms: 12_000,
              chord: "Ctrl+Shift+K",
              available: true
            }
          });
        }
        if (init?.method === "DELETE") {
          return jsonResponse({
            ok: true,
            result: { released: true }
          });
        }
        return jsonResponse({
          ok: true,
          result: {
            lease_id: body.lease_id ?? "lease-opaque-token",
            expires_at: "2026-07-24T00:00:12Z",
            lease_ttl_ms: 12_000
          }
        });
      })
    );

    const api = new PCControllerDesktopAPI();
    const begun = await api.beginHotkeyCapture();
    const renewed = await api.renewHotkeyCapture(begun.lease_id);
    const probed = await api.probeHotkey(
      renewed.lease_id,
      "Ctrl+Shift+K"
    );
    const released = await api.releaseHotkeyCapture(
      renewed.lease_id,
      true
    );

    expect(begun.lease_id).toBe("lease-opaque-token");
    expect(renewed.lease_ttl_ms).toBe(12_000);
    expect(probed).toMatchObject({
      chord: "Ctrl+Shift+K",
      available: true
    });
    expect(released.released).toBe(true);

    expect(requests.slice(1).map(([path]) => path)).toEqual([
      "/ui/hotkeys/capture",
      "/ui/hotkeys/capture",
      "/ui/hotkeys/probe",
      "/ui/hotkeys/capture"
    ]);
    expect(requests.slice(1).map(([, init]) => init?.method)).toEqual([
      "POST",
      "POST",
      "POST",
      "DELETE"
    ]);
    expect(JSON.parse(String(requests[1][1]?.body))).toEqual({});
    expect(JSON.parse(String(requests[2][1]?.body))).toEqual({
      lease_id: "lease-opaque-token"
    });
    expect(JSON.parse(String(requests[3][1]?.body))).toEqual({
      lease_id: "lease-opaque-token",
      chord: "Ctrl+Shift+K"
    });
    expect(requests[4][1]?.keepalive).toBe(true);
    for (const [, init] of requests.slice(1)) {
      expect(
        new Headers(init?.headers).get("X-PCControllerDesktop-CSRF")
      ).toBe("csrf-hotkeys");
    }
  });

  it("uses the narrow authenticated registration endpoint and exact action body", async () => {
    const requests: Array<[RequestInfo | URL, RequestInit | undefined]> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        requests.push([input, init]);
        if (input === "/app/session") {
          return jsonResponse({
            ok: true,
            result: {
              csrf_token: "csrf-registration",
              expires_at: "2026-07-24T00:00:00Z"
            }
          });
        }
        return jsonResponse({
          ok: true,
          result: {
            executable: "C:\\Portable\\PCControllerDesktop.exe",
            shell_registered: true,
            com_registered: false
          }
        });
      })
    );

    const api = new PCControllerDesktopAPI();
    await expect(api.integrationRegistrationStatus()).resolves.toMatchObject({
      shell_registered: true
    });
    await api.updateIntegrationRegistration("integration.shell.register");

    expect(requests.map(([path]) => path)).toEqual([
      "/ui/integration/registration",
      "/app/session",
      "/ui/integration/registration"
    ]);
    expect(requests[0][1]?.method).toBe("GET");
    expect(requests[0][1]?.body).toBeUndefined();
    expect(requests[2][1]?.method).toBe("POST");
    expect(JSON.parse(String(requests[2][1]?.body))).toEqual({
      method: "integration.shell.register"
    });
    expect(
      new Headers(requests[2][1]?.headers).get("X-PCControllerDesktop-CSRF")
    ).toBe("csrf-registration");
  });
});
