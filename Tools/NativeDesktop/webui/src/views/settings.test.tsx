import {
  fireEvent,
  render,
  screen,
  waitFor,
  within
} from "@testing-library/preact";
import { describe, expect, it, vi } from "vitest";
import {
  APIError,
  type RayanLampAPI,
  type RayanLampClient
} from "../api";
import type { DeepPartial, AppConfig } from "../contracts";
import { testConfig } from "../test/fixtures";
import {
  AppearanceSettings,
  HotkeySettings,
  IntegrationSettings,
  LampSettings,
  NotificationSettings
} from "./settings";

function renderAppearance(value: AppConfig) {
  const patch = vi.fn<
    (value: DeepPartial<AppConfig>, immediate?: boolean) => void
  >();
  render(<AppearanceSettings value={value} patch={patch} />);
  return patch;
}

describe("native appearance settings", () => {
  it("offers the compatible automatic and frameless border modes", () => {
    renderAppearance(structuredClone(testConfig));

    const border = screen.getByRole("combobox", {
      name: /^Window border/
    }) as HTMLSelectElement;
    expect(border.value).toBe("auto");
    expect(
      Array.from(border.options, (option) => [option.value, option.text])
    ).toEqual([
      ["auto", "Automatic (resizable)"],
      ["none", "None (frameless)"]
    ]);
  });

  it("offers adaptive, forced DirectX, and compatibility renderers", () => {
    const patch = renderAppearance(structuredClone(testConfig));
    const renderer = screen.getByRole("combobox", {
      name: /^Rendering backend/
    }) as HTMLSelectElement;

    expect(renderer.value).toBe("auto");
    expect(
      Array.from(renderer.options, (option) => [option.value, option.text])
    ).toEqual([
      ["auto", "Automatic (DirectX when supported)"],
      ["directx", "DirectX 11 hardware"],
      ["legacy", "Compatibility renderer (lower RAM)"]
    ]);

    fireEvent.change(renderer, { target: { value: "legacy" } });
    expect(patch).toHaveBeenCalledWith(
      { ui: { render_backend: "legacy" } },
      true
    );
  });

  it("moves a native caption to custom controls before removing its frame", () => {
    const value = structuredClone(testConfig);
    value.ui.title_bar_mode = "native";
    value.ui.show_title_bar = false;
    const patch = renderAppearance(value);

    fireEvent.change(screen.getByRole("combobox", { name: /^Window border/ }), {
      target: { value: "none" }
    });

    expect(patch).toHaveBeenCalledWith(
      {
        ui: {
          border_mode: "none",
          title_bar_mode: "custom",
          show_title_bar: true
        }
      },
      true
    );
  });

  it("restores a compatible material and frame when native chrome is selected", () => {
    const value = structuredClone(testConfig);
    value.ui.backdrop = "transparent";
    value.ui.border_mode = "none";
    const patch = renderAppearance(value);

    fireEvent.change(screen.getByLabelText("Title bar"), {
      target: { value: "native" }
    });

    expect(patch).toHaveBeenCalledWith(
      {
        ui: {
          title_bar_mode: "native",
          show_title_bar: false,
          backdrop: "solid",
          border_mode: "auto"
        }
      },
      true
    );
  });

  it("applies tray-menu prewarming immediately and allows disabling it", () => {
    const patch = renderAppearance(structuredClone(testConfig));
    const prewarm = screen.getByRole("checkbox", {
      name: /^Preload and reuse the notification-area menu/
    }) as HTMLInputElement;

    expect(prewarm.checked).toBe(true);
    fireEvent.click(prewarm);

    expect(patch).toHaveBeenCalledWith(
      {
        ui: {
          prewarm_tray_menu: false
        }
      },
      true
    );
  });
});

describe("lamp transport settings", () => {
  it("applies a bounded device terminal port immediately", () => {
    const patch = vi.fn<
      (value: DeepPartial<AppConfig>, immediate?: boolean) => void
    >();
    render(
      <LampSettings
        value={structuredClone(testConfig)}
        patch={patch}
        direct={false}
      />
    );

    fireEvent.input(
      screen.getByRole("spinbutton", { name: /^Device terminal port/ }),
      { target: { value: "2323" } }
    );

    expect(patch).toHaveBeenCalledWith(
      { lamp: { telnet_port: 2323 } },
      true
    );
  });
});

describe("shared audio settings", () => {
  it("keeps audio controls available when visual notifications are unavailable", async () => {
    const playAudio = vi.fn(async () => ({}));
    render(
      <NotificationSettings
        api={{ playAudio } as unknown as RayanLampClient}
        value={structuredClone(testConfig)}
        patch={vi.fn()}
        direct
        visualAvailable={false}
        audioAvailable
        onTest={vi.fn()}
      />
    );

    expect(screen.queryByText("Visual delivery")).toBeNull();
    expect(screen.getByLabelText("Sound source")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Test audio" }));

    await waitFor(() =>
      expect(playAudio).toHaveBeenCalledWith("message", 1)
    );
  });

  it("imports one WAV through the active transport and immediately accepts its config", async () => {
    const next = structuredClone(testConfig);
    next.notifications.sound_source = "custom";
    next.notifications.sound_file = "a".repeat(64);
    next.notifications.sound_name = "Soft switch.wav";
    const importAudio = vi.fn(
      async (_file: File, _revision?: string | number) => ({
        config: next,
        revision: 12
      })
    );
    render(
      <NotificationSettings
        api={{ importAudio } as unknown as RayanLampClient}
        value={structuredClone(testConfig)}
        patch={vi.fn()}
        direct={false}
        visualAvailable
        audioAvailable
        onTest={vi.fn()}
      />
    );
    const file = new File([new Uint8Array([82, 73, 70, 70])], "Soft switch.wav", {
      type: "audio/wav"
    });

    fireEvent.change(screen.getByLabelText("Choose custom WAV"), {
      target: { files: [file] }
    });

    await waitFor(() => expect(importAudio).toHaveBeenCalled());
    expect(importAudio.mock.calls[0][0]).toBe(file);
  });
});

describe("current executable registration", () => {
  it("shows opt-in status for the exact current executable without persisting config", async () => {
    const api = registrationAPI({
      executable: "C:\\Portable\\RayanLamp.exe",
      shell_registered: false,
      com_registered: true
    });
    const patch = renderIntegration(api);

    expect(
      (await screen.findByTitle("C:\\Portable\\RayanLamp.exe")).textContent
    ).toBe("C:\\Portable\\RayanLamp.exe");
    expect(
      screen.getByText(/Portable mode stays unregistered by default/)
    ).toBeTruthy();
    expect(
      (screen.getByRole("button", {
        name: "Register shell"
      }) as HTMLButtonElement).disabled
    ).toBe(false);
    expect(
      (screen.getByRole("button", {
        name: "Unregister COM"
      }) as HTMLButtonElement).disabled
    ).toBe(false);
    expect(patch).not.toHaveBeenCalled();
  });

  it("registers through the canonical action and refreshes authoritative status", async () => {
    const status = vi
      .fn()
      .mockResolvedValueOnce({
        executable: "C:\\Portable\\RayanLamp.exe",
        shell_registered: false,
        com_registered: false
      })
      .mockResolvedValue({
        executable: "C:\\Portable\\RayanLamp.exe",
        shell_registered: true,
        com_registered: false
      });
    const update = vi.fn(async () => ({
      executable: "C:\\Portable\\RayanLamp.exe",
      shell_registered: true,
      com_registered: false
    }));
    const api = registrationAPI(undefined, status, update);
    const patch = renderIntegration(api);

    fireEvent.click(
      await screen.findByRole("button", { name: "Register shell" })
    );

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith("integration.shell.register")
    );
    await waitFor(() => expect(status).toHaveBeenCalledTimes(2));
    expect(
      (await screen.findByRole("button", {
        name: "Unregister shell"
      }) as HTMLButtonElement).disabled
    ).toBe(false);
    expect(patch).not.toHaveBeenCalled();
  });

  it("requires confirmation before unregistering and then refreshes status", async () => {
    const status = vi
      .fn()
      .mockResolvedValueOnce({
        executable: "C:\\Portable\\RayanLamp.exe",
        shell_registered: true,
        com_registered: false
      })
      .mockResolvedValue({
        executable: "C:\\Portable\\RayanLamp.exe",
        shell_registered: false,
        com_registered: false
      });
    const update = vi.fn(async () => ({
      executable: "C:\\Portable\\RayanLamp.exe",
      shell_registered: false,
      com_registered: false
    }));
    renderIntegration(registrationAPI(undefined, status, update));
    enableRenderedDialogs();

    fireEvent.click(
      await screen.findByRole("button", { name: "Unregister shell" })
    );
    expect(update).not.toHaveBeenCalled();

    const dialog = screen.getByRole("dialog", {
      name: "Unregister Windows shell integration?"
    });
    expect(
      within(dialog).getByText(/settings and device data are not removed/)
    ).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Unregister" }));

    await waitFor(() =>
      expect(update).toHaveBeenCalledWith("integration.shell.unregister")
    );
    await waitFor(() => expect(status).toHaveBeenCalledTimes(2));
  });

  it("surfaces a partial Jump List failure and refreshes the successful URI state", async () => {
    const status = vi
      .fn()
      .mockResolvedValueOnce({
        executable: "C:\\Portable\\RayanLamp.exe",
        shell_registered: false,
        com_registered: false
      })
      .mockResolvedValue({
        executable: "C:\\Portable\\RayanLamp.exe",
        shell_registered: true,
        com_registered: false
      });
    const update = vi.fn(async () => {
      throw new APIError(
        500,
        "partial",
        "The URI handler was registered, but Windows policy blocked Jump List tasks."
      );
    });
    renderIntegration(registrationAPI(undefined, status, update));

    fireEvent.click(
      await screen.findByRole("button", { name: "Register shell" })
    );

    expect(await screen.findByText("Partially completed")).toBeTruthy();
    expect(
      screen.getByText(/policy blocked Jump List tasks/)
    ).toBeTruthy();
    await waitFor(() => expect(status).toHaveBeenCalledTimes(2));
    expect(
      (await screen.findByRole("button", {
        name: "Unregister shell"
      }) as HTMLButtonElement).disabled
    ).toBe(false);
  });
});

function renderIntegration(api: RayanLampClient) {
  const patch = vi.fn<
    (value: DeepPartial<AppConfig>, immediate?: boolean) => void
  >();
  render(
    <IntegrationSettings
      api={api}
      value={structuredClone(testConfig)}
      patch={patch}
      onDisableAPI={vi.fn()}
    />
  );
  return patch;
}

function registrationAPI(
  result = {
    executable: "C:\\Portable\\RayanLamp.exe",
    shell_registered: false,
    com_registered: false
  },
  status = vi.fn(async () => result),
  update = vi.fn(async () => result)
): RayanLampClient {
  return {
    integrationRegistrationStatus: status,
    updateIntegrationRegistration: update
  } as unknown as RayanLampClient;
}

function enableRenderedDialogs() {
  for (const dialog of document.querySelectorAll("dialog")) {
    dialog.showModal = () => {
      dialog.open = true;
    };
    dialog.close = () => {
      dialog.open = false;
      dialog.dispatchEvent(new Event("close"));
    };
  }
}

describe("global hotkey settings", () => {
  it("synchronizes only a completed recorded chord as an immediate patch", async () => {
    const patch = vi.fn<
      (value: DeepPartial<AppConfig>, immediate?: boolean) => void
    >();
    render(
      <HotkeySettings value={structuredClone(testConfig)} patch={patch} />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Record Toggle lamp shortcut" })
    );
    const recorder = screen.getByRole("button", {
      name: "Cancel recording for Toggle lamp"
    });
    fireEvent.keyDown(recorder, {
      key: "Control",
      code: "ControlLeft",
      ctrlKey: true
    });
    expect(patch).not.toHaveBeenCalled();

    fireEvent.keyDown(recorder, {
      key: "k",
      code: "KeyK",
      ctrlKey: true,
      shiftKey: true
    });
    await waitFor(() =>
      expect(patch).toHaveBeenCalledWith(
        { hotkeys: { toggle: "Ctrl+Shift+K" } },
        true
      )
    );
  });

  it("keeps exactly one browser-only recorder active when ownership changes", () => {
    const patch = vi.fn<
      (value: DeepPartial<AppConfig>, immediate?: boolean) => void
    >();
    render(
      <HotkeySettings value={structuredClone(testConfig)} patch={patch} />
    );

    const toggle = screen.getByRole("button", {
      name: "Record Toggle lamp shortcut"
    });
    fireEvent.click(toggle);
    const activeToggle = screen.getByRole("button", {
      name: "Cancel recording for Toggle lamp"
    });
    const turnOn = screen.getByRole("button", {
      name: "Record Turn on shortcut"
    });
    fireEvent.blur(activeToggle, { relatedTarget: turnOn });
    fireEvent.click(turnOn);

    expect(
      screen.getByRole("button", { name: "Record Toggle lamp shortcut" })
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Cancel recording for Turn on" })
    ).toBeTruthy();
    expect(
      screen.queryAllByRole("button", { name: /Cancel recording for/ })
    ).toHaveLength(1);
  });

  it("hands one native capture lease between recorders and releases it on cancel", async () => {
    const lease = {
      lease_id: "lease-shared",
      expires_at: "2026-07-24T00:00:12Z",
      lease_ttl_ms: 12_000
    };
    const api = {
      beginHotkeyCapture: vi.fn(async () => lease),
      renewHotkeyCapture: vi.fn(async () => lease),
      releaseHotkeyCapture: vi.fn(async () => ({ released: true })),
      probeHotkey: vi.fn()
    } as unknown as RayanLampAPI;
    const patch = vi.fn<
      (value: DeepPartial<AppConfig>, immediate?: boolean) => void
    >();
    render(
      <HotkeySettings
        api={api}
        captureAvailable
        probeAvailable
        value={structuredClone(testConfig)}
        patch={patch}
      />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Record Toggle lamp shortcut" })
    );
    await screen.findByRole("button", {
      name: "Cancel recording for Toggle lamp"
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Record Turn on shortcut" })
    );
    await screen.findByRole("button", {
      name: "Cancel recording for Turn on"
    });

    expect(api.beginHotkeyCapture).toHaveBeenCalledTimes(1);
    expect(api.releaseHotkeyCapture).not.toHaveBeenCalled();
    expect(
      screen.queryAllByRole("button", { name: /Cancel recording for/ })
    ).toHaveLength(1);

    fireEvent.click(
      screen.getByRole("button", { name: "Cancel recording for Turn on" })
    );
    await waitFor(() =>
      expect(api.releaseHotkeyCapture).toHaveBeenCalledWith("lease-shared")
    );
    expect(api.releaseHotkeyCapture).toHaveBeenCalledTimes(1);
  });

  it("does not call unsupported native capture endpoints", () => {
    const api = {
      beginHotkeyCapture: vi.fn()
    } as unknown as RayanLampAPI;
    render(
      <HotkeySettings
        api={api}
        captureAvailable={false}
        probeAvailable={false}
        value={structuredClone(testConfig)}
        patch={vi.fn()}
      />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Record Toggle lamp shortcut" })
    );

    expect(api.beginHotkeyCapture).not.toHaveBeenCalled();
    expect(screen.getByText(/Browser-only recording cannot preflight/)).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: "Cancel recording for Toggle lamp"
      })
    ).toBeTruthy();
  });
});
