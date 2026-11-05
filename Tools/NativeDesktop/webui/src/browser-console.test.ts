import { describe, expect, it, vi } from "vitest";
import {
  browserErrorMetadata,
  StyledBrowserConsole,
  type BrowserConsoleSink
} from "./browser-console";

function sink(): BrowserConsoleSink {
  return {
    debug: vi.fn(),
    info: vi.fn(),
    log: vi.fn(),
    warn: vi.fn(),
    error: vi.fn(),
    groupCollapsed: vi.fn(),
    groupEnd: vi.fn()
  };
}

describe("StyledBrowserConsole", () => {
  it("uses collapsed CSS-styled startup output with explicit colors", () => {
    const target = sink();
    const output = new StyledBrowserConsole(target);

    output.start({
      surface: "web",
      mode: "production",
      secureContext: true
    });

    expect(target.groupCollapsed).toHaveBeenCalledOnce();
    const group = vi.mocked(target.groupCollapsed).mock.calls[0];
    expect(group[0]).toContain("%c");
    expect(group[0]).toContain("💡");
    expect(group[1]).toMatch(/color:#[0-9a-f]{6}/i);
    expect(group[1]).toMatch(/background:/);
    expect(target.info).toHaveBeenCalledOnce();
    expect(target.debug).toHaveBeenCalledOnce();
    expect(target.log).toHaveBeenCalledOnce();
    expect(target.groupEnd).toHaveBeenCalledOnce();
  });

  it.each([
    ["debug", "debug"],
    ["info", "info"],
    ["success", "info"],
    ["warn", "warn"],
    ["error", "error"]
  ] as const)("maps %s to console.%s", (level, method) => {
    const target = sink();
    const output = new StyledBrowserConsole(target);

    output.event(level, "device.event", "Useful activity", {
      type: "unit-test"
    });

    expect(target[method]).toHaveBeenCalledOnce();
    const call = vi.mocked(target[method]).mock.calls[0];
    expect(call[0]).toContain("%c");
    expect(call[1]).toContain("color:");
    expect(call[1]).toContain("background:");
    expect(call[2]).toContain("color:");
    expect(call[2]).toContain("background:");
    expect(call[3]).toContain("background:transparent");
    expect(call[4]).toMatchObject({
      level,
      event: "device.event",
      detail: "Useful activity"
    });
  });

  it("allowlists metadata and redacts secrets recursively", () => {
    const target = sink();
    const output = new StyledBrowserConsole(target);

    output.event("error", "transport.error", "Relay failed", {
      source: "relay",
      error_code: 401,
      action:
        "Bearer abc.def.ghi https://david:password@device.local/?token=private",
      csrf_token: "must-not-appear",
      unexpected: "must-not-appear",
      nested: { password: "must-not-appear" }
    });

    const call = vi.mocked(target.error).mock.calls[0];
    const metadata = call[4] as Record<string, unknown>;
    expect(metadata).toMatchObject({
      level: "error",
      event: "transport.error",
      detail: "Relay failed",
      source: "relay",
      error_code: 401,
      action:
        "Bearer [REDACTED] https://[REDACTED]@device.local/?token=[REDACTED]"
    });
    expect(JSON.stringify(call)).not.toContain("must-not-appear");
    expect(JSON.stringify(call)).not.toContain("private");
    expect(JSON.stringify(call)).not.toContain("password@");
  });

  it("suppresses a repeated keyed event within its quiet window", () => {
    const target = sink();
    let now = 100;
    const output = new StyledBrowserConsole(target, () => now);
    const options = { dedupeKey: "same-state", dedupeWindowMs: 1_000 };

    output.event("info", "device.state", "Device is on", { state: "on" }, options);
    now = 500;
    output.event("info", "device.state", "Device is on", { state: "on" }, options);
    now = 1_101;
    output.event("info", "device.state", "Device is on", { state: "on" }, options);

    expect(target.info).toHaveBeenCalledTimes(2);
  });

  it("normalizes Error context without exposing bearer or query tokens", () => {
    const error = Object.assign(
      new Error("Bearer secret-value at /events?token=swordfish"),
      { code: "unauthorized", status: 401 }
    );

    expect(browserErrorMetadata(error)).toEqual({
      error_code: "unauthorized"
    });
  });

  it("keeps console lines single-line and strips terminal and bidi controls", () => {
    const target = sink();
    const output = new StyledBrowserConsole(target);

    output.event(
      "warn",
      "device.event",
      "first\u001b[31m\nsecond\u202Ehidden",
      {
        source: "device\u009b",
        action: "refresh\r\nnow"
      }
    );

    const call = vi.mocked(target.warn).mock.calls[0];
    expect(call[0]).toContain("first[31m secondhidden");
    expect(call[0]).not.toMatch(/[\u001b\u202e\r\n]/);
    expect(JSON.stringify(call[4])).not.toMatch(/[\u009b\r\n]/);
  });
});
