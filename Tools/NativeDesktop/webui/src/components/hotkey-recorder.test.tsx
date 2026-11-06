import {
  act,
  fireEvent,
  render,
  screen,
  waitFor
} from "@testing-library/preact";
import { useState } from "preact/hooks";
import { describe, expect, it, vi } from "vitest";
import {
  canonicalizeHotkey,
  HotkeyRecorder
} from "./hotkey-recorder";

describe("hotkey chord recorder", () => {
  it("canonicalizes the same aliases and modifier order as the native parser", () => {
    expect(canonicalizeHotkey(" shift + control + pgdn ")).toBe(
      "Ctrl+Shift+PageDown"
    );
    expect(canonicalizeHotkey("meta+numpad7")).toBe("Win+Num7");
    expect(canonicalizeHotkey("control+menu")).toBe("Ctrl+Apps");
    expect(canonicalizeHotkey("alt+print_screen")).toBe(
      "Alt+PrintScreen"
    );
    expect(canonicalizeHotkey("win+browser_back")).toBe(
      "Win+BrowserBack"
    );
    expect(canonicalizeHotkey("ctrl+launch_media")).toBe(
      "Ctrl+LaunchMediaSelect"
    );
    expect(canonicalizeHotkey("shift+;")).toBe("Shift+Semicolon");
    expect(canonicalizeHotkey("Ctrl+Ctrl+L")).toBeNull();
    expect(canonicalizeHotkey("Ctrl+L+K")).toBeNull();
  });

  it("shows existing shortcuts as individual key chips", () => {
    const { container } = render(
      <HotkeyRecorder
        label="Toggle device"
        value="Ctrl+Alt+L"
        onCommit={() => undefined}
      />
    );

    expect(
      Array.from(container.querySelectorAll("kbd"), (chip) => chip.textContent)
    ).toEqual(["Ctrl", "Alt", "L"]);
    expect(screen.getByRole("status").textContent).toContain(
      "Ctrl plus Alt plus L is assigned"
    );
  });

  it("previews modifiers locally and commits only a completed canonical chord", () => {
    const onCommit = vi.fn();
    render(
      <HotkeyRecorder label="Toggle device" value="" onCommit={onCommit} />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Record Toggle device shortcut" })
    );
    const recorder = screen.getByRole("button", {
      name: "Cancel recording for Toggle device"
    });

    fireEvent.keyDown(recorder, {
      key: "Control",
      code: "ControlLeft",
      ctrlKey: true
    });
    expect(onCommit).not.toHaveBeenCalled();
    expect(screen.getByRole("status").textContent).toContain(
      "Holding Ctrl"
    );

    fireEvent.keyDown(recorder, {
      key: "k",
      code: "KeyK",
      ctrlKey: true,
      shiftKey: true
    });
    expect(onCommit).toHaveBeenCalledOnce();
    expect(onCommit).toHaveBeenCalledWith("Ctrl+Shift+K");
  });

  it("cancels recording when the controlled binding changes elsewhere", () => {
    const onCommit = vi.fn();
    const view = render(
      <HotkeyRecorder
        label="Toggle device"
        value="Ctrl+Alt+L"
        onCommit={onCommit}
      />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Record Toggle device shortcut" })
    );
    const recorder = screen.getByRole("button", {
      name: "Cancel recording for Toggle device"
    });
    fireEvent.keyDown(recorder, {
      key: "Control",
      code: "ControlLeft",
      ctrlKey: true
    });

    view.rerender(
      <HotkeyRecorder
        label="Toggle device"
        value="Ctrl+Shift+N"
        onCommit={onCommit}
      />
    );

    expect(onCommit).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "Record Toggle device shortcut" })
    ).toBeTruthy();
    expect(screen.getByRole("status").textContent).toContain(
      "Shortcut changed elsewhere. Recording cancelled."
    );
    expect(screen.getByRole("status").textContent).toContain(
      "Ctrl plus Shift plus N is now assigned"
    );

    view.rerender(
      <HotkeyRecorder
        label="Toggle device"
        value="Ctrl+Alt+T"
        onCommit={onCommit}
      />
    );
    expect(screen.getByRole("status").textContent).toBe(
      "Ctrl plus Alt plus T is assigned."
    );
  });

  it("does not treat its own controlled commit as an external change", () => {
    const onCommit = vi.fn();
    function ControlledRecorder() {
      const [current, setCurrent] = useState("Ctrl+Alt+L");
      return (
        <HotkeyRecorder
          label="Toggle device"
          value={current}
          onCommit={(next) => {
            onCommit(next);
            setCurrent(next);
          }}
        />
      );
    }
    render(<ControlledRecorder />);

    fireEvent.click(
      screen.getByRole("button", { name: "Record Toggle device shortcut" })
    );
    fireEvent.keyDown(
      screen.getByRole("button", {
        name: "Cancel recording for Toggle device"
      }),
      {
        key: "k",
        code: "KeyK",
        ctrlKey: true,
        shiftKey: true
      }
    );

    expect(onCommit).toHaveBeenCalledWith("Ctrl+Shift+K");
    expect(screen.getByRole("status").textContent).toBe(
      "Ctrl plus Shift plus K is assigned."
    );
    expect(screen.queryByText(/changed elsewhere/i)).toBeNull();
  });

  it("cancels with Escape without changing the configured shortcut", () => {
    const onCommit = vi.fn();
    render(
      <HotkeyRecorder
        label="Turn on"
        value="Ctrl+Alt+O"
        onCommit={onCommit}
      />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Record Turn on shortcut" })
    );
    fireEvent.keyDown(
      screen.getByRole("button", {
        name: "Cancel recording for Turn on"
      }),
      { key: "Escape", code: "Escape" }
    );

    expect(onCommit).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "Record Turn on shortcut" })
    ).toBeTruthy();
  });

  it("clears with Delete while recording", () => {
    const onCommit = vi.fn();
    render(
      <HotkeyRecorder
        label="Turn off"
        value="Ctrl+Alt+F"
        onCommit={onCommit}
      />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Record Turn off shortcut" })
    );
    fireEvent.keyDown(
      screen.getByRole("button", {
        name: "Cancel recording for Turn off"
      }),
      { key: "Delete", code: "Delete" }
    );

    expect(onCommit).toHaveBeenCalledWith("");
  });

  it("records modified editing and OEM keys instead of treating them as clear", () => {
    const onCommit = vi.fn();
    render(
      <HotkeyRecorder
        label="Turn off"
        value="Ctrl+Alt+F"
        onCommit={onCommit}
      />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Record Turn off shortcut" })
    );
    fireEvent.keyDown(
      screen.getByRole("button", {
        name: "Cancel recording for Turn off"
      }),
      { key: "Delete", code: "Delete", ctrlKey: true }
    );
    expect(onCommit).toHaveBeenLastCalledWith("Ctrl+Delete");

    fireEvent.click(
      screen.getByRole("button", { name: "Record Turn off shortcut" })
    );
    fireEvent.keyDown(
      screen.getByRole("button", {
        name: "Cancel recording for Turn off"
      }),
      { key: ":", code: "Semicolon", shiftKey: true }
    );
    expect(onCommit).toHaveBeenLastCalledWith("Shift+Semicolon");
  });

  it("keeps recording and reports duplicate or Windows-reserved chords", () => {
    const onCommit = vi.fn();
    render(
      <HotkeyRecorder
        label="Show window"
        value=""
        conflicts={[{ label: "Toggle device", value: "Ctrl+Alt+L" }]}
        onCommit={onCommit}
      />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Record Show window shortcut" })
    );
    const recorder = screen.getByRole("button", {
      name: "Cancel recording for Show window"
    });
    fireEvent.keyDown(recorder, {
      key: "l",
      code: "KeyL",
      ctrlKey: true,
      altKey: true
    });

    expect(screen.getByRole("alert").textContent).toContain(
      "Already assigned to Toggle device"
    );
    expect(onCommit).not.toHaveBeenCalled();

    fireEvent.keyDown(recorder, { key: "F12", code: "F12" });
    expect(screen.getByRole("alert").textContent).toContain(
      "F12 is reserved by Windows"
    );
    expect(onCommit).not.toHaveBeenCalled();
  });

  it("blocks stored F12 and warns without blocking risky single-key chords", () => {
    const onCommit = vi.fn();
    const view = render(
      <HotkeyRecorder
        label="Show window"
        value="Ctrl+F12"
        onCommit={onCommit}
      />
    );

    expect(screen.getByRole("alert").textContent).toContain(
      "F12 is reserved by Windows"
    );
    expect(screen.getByRole("status").textContent).toContain(
      "stored but unavailable"
    );

    view.rerender(
      <HotkeyRecorder label="Show window" value="L" onCommit={onCommit} />
    );
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByRole("note").textContent).toContain(
      "System-wide single-key shortcut"
    );
    expect(
      (screen.getByRole("button", {
        name: "Record Show window shortcut"
      }) as HTMLButtonElement).disabled
    ).toBe(false);

    view.rerender(
      <HotkeyRecorder
        label="Show window"
        value="Shift+Semicolon"
        onCommit={onCommit}
      />
    );
    expect(screen.getByRole("note").textContent).toContain("use carefully");

    view.rerender(
      <HotkeyRecorder
        label="Show window"
        value="Ctrl+Shift+L"
        onCommit={onCommit}
      />
    );
    expect(screen.queryByRole("note")).toBeNull();
  });

  it("keeps recording when Windows reports that a candidate is unavailable", async () => {
    const onCommit = vi.fn();
    const onCaptureStop = vi.fn();
    const onProbe = vi.fn(async () => ({
      available: false,
      message: "Another application already owns Ctrl+Shift+K."
    }));
    render(
      <HotkeyRecorder
        label="Toggle device"
        value="Ctrl+Alt+L"
        captureActive
        onCaptureStart={() => ({ active: true })}
        onCaptureStop={onCaptureStop}
        onProbe={onProbe}
        onCommit={onCommit}
      />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Record Toggle device shortcut" })
    );
    fireEvent.keyDown(
      screen.getByRole("button", {
        name: "Cancel recording for Toggle device"
      }),
      { key: "k", code: "KeyK", ctrlKey: true, shiftKey: true }
    );

    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toContain(
        "Another application already owns"
      )
    );
    expect(onProbe).toHaveBeenCalledWith("Ctrl+Shift+K");
    expect(onCommit).not.toHaveBeenCalled();
    expect(onCaptureStop).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", {
        name: "Cancel recording for Toggle device"
      })
    ).toBeTruthy();
  });

  it("holds capture through persistence and releases only after apply completes", async () => {
    let resolveApply!: () => void;
    const apply = new Promise<void>((resolve) => {
      resolveApply = resolve;
    });
    const onCaptureStop = vi.fn(async () => undefined);
    const onCommit = vi.fn(() => apply);
    render(
      <HotkeyRecorder
        label="Toggle device"
        value="Ctrl+Alt+L"
        captureActive
        onCaptureStart={() => ({ active: true })}
        onCaptureStop={onCaptureStop}
        onProbe={async () => ({ available: true })}
        onCommit={onCommit}
      />
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Record Toggle device shortcut" })
    );
    fireEvent.keyDown(
      screen.getByRole("button", {
        name: "Cancel recording for Toggle device"
      }),
      { key: "k", code: "KeyK", ctrlKey: true, shiftKey: true }
    );

    await waitFor(() => expect(onCommit).toHaveBeenCalledWith("Ctrl+Shift+K"));
    expect((
      screen.getByRole("button", {
        name: "Applying Toggle device shortcut"
      })
    ) as HTMLButtonElement).toHaveProperty("disabled", true);
    expect(screen.getByRole("status").textContent).toContain("Applying");
    expect(onCaptureStop).not.toHaveBeenCalled();

    await act(async () => {
      resolveApply();
      await apply;
    });
    await waitFor(() =>
      expect(onCaptureStop).toHaveBeenCalledWith("complete")
    );
    expect(
      screen.getByRole("button", { name: "Record Toggle device shortcut" })
    ).toBeTruthy();
  });

  it("stops a handed-off controlled recorder without releasing the shared capture", () => {
    const onCaptureStop = vi.fn();
    const props = {
      label: "Toggle device",
      value: "Ctrl+Alt+L",
      onCaptureStart: () => ({ active: true }),
      onCaptureStop,
      onCommit: () => undefined
    };
    const view = render(<HotkeyRecorder {...props} captureActive />);

    fireEvent.click(
      screen.getByRole("button", { name: "Record Toggle device shortcut" })
    );
    view.rerender(<HotkeyRecorder {...props} captureActive={false} />);

    expect(
      screen.getByRole("button", { name: "Record Toggle device shortcut" })
    ).toBeTruthy();
    expect(onCaptureStop).not.toHaveBeenCalled();
  });
});
