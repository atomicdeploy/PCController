import { render, screen } from "@testing-library/preact";
import { describe, expect, it } from "vitest";
import type { PCControllerDesktopClient } from "../api";
import { activeClientKind, capabilities } from "../store";
import {
  DeviceTerminal,
  terminalAcceptsInput,
  terminalThemeFor
} from "./terminal";

describe("device terminal availability", () => {
  it("explains the raw-TCP boundary in direct/static mode", () => {
    activeClientKind.value = "direct";
    capabilities.value = {
      transport_direct: true,
      transport_relay: false,
      device_terminal: false
    };
    const api = {
      kind: "direct"
    } as PCControllerDesktopClient;

    render(<DeviceTerminal api={api} />);

    expect(
      screen.getByRole("heading", {
        name: "Raw TCP is not available in direct browser mode"
      })
    ).toBeTruthy();
    expect(screen.getByText(/same origin that forwards/)).toBeTruthy();
    expect(
      screen.queryByLabelText("PCController device terminal input")
    ).toBeNull();
  });

  it("keeps the feature unavailable when an older coordinator omits its capability", () => {
    activeClientKind.value = "relay";
    capabilities.value = {
      transport_direct: false,
      transport_relay: true,
      device_terminal: false
    };
    const api = {
      kind: "relay"
    } as PCControllerDesktopClient;

    render(<DeviceTerminal api={api} />);

    expect(screen.getByText("Coordinator required")).toBeTruthy();
    expect(screen.queryByText(/frontend-only ZIP/)).toBeNull();
  });
});

describe("terminal palette", () => {
  it("provides readable dark, light, and system-derived themes", () => {
    const dark = terminalThemeFor("dark", true);
    const light = terminalThemeFor("light", false);

    expect(dark).not.toEqual(light);
    expect(terminalThemeFor("system", true)).toEqual(light);
    expect(terminalThemeFor("system", false)).toEqual(dark);
    expect(dark.background).toMatch(/^rgba/);
    expect(light.foreground).toMatch(/^#/);
  });
});

describe("terminal input policy", () => {
  it("stops keyboard forwarding for a firmware-declared read-only shell", () => {
    const status = {
      state: "connected",
      host: "pccontroller-desktop.local",
      port: 23,
      session: 1,
      sequence: 2,
      read_only: true
    } as const;

    expect(terminalAcceptsInput(status, "ready")).toBe(false);
    expect(
      terminalAcceptsInput({ ...status, read_only: false }, "ready")
    ).toBe(true);
    expect(
      terminalAcceptsInput({ ...status, read_only: false }, "closed")
    ).toBe(false);
  });
});
