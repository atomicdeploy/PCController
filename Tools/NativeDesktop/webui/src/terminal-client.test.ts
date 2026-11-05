import { describe, expect, it, vi } from "vitest";
import type { DeviceTerminalStatus } from "./contracts";
import {
  TerminalBridge,
  terminalInputFrames,
  terminalKeyboardInput,
  terminalWebSocketURL
} from "./terminal-client";

class FakeSocket extends EventTarget {
  readyState = 0;
  binaryType: BinaryType = "blob";
  readonly sent: unknown[] = [];
  readonly closes: Array<{ code?: number; reason?: string }> = [];

  send(value: unknown): void {
    this.sent.push(value);
  }

  open(): void {
    this.readyState = 1;
    this.dispatchEvent(new Event("open"));
  }

  receive(value: unknown): void {
    this.dispatchEvent(new MessageEvent("message", { data: value }));
  }

  close(code?: number, reason?: string): void {
    this.closes.push({ code, reason });
    this.readyState = 3;
    this.dispatchEvent(new CloseEvent("close", { code, reason }));
  }
}

const connectedStatus: DeviceTerminalStatus = {
  state: "connected",
  host: "pccontroller-desktop.local",
  port: 23,
  session: 4,
  sequence: 12,
  oldest_sequence: 3,
  read_only: false,
  connected_at: "2026-07-24T00:00:00Z"
};

describe("terminal coordinator bridge", () => {
  it("uses the protected same-origin WebSocket and exchanges control/status frames", () => {
    const socket = new FakeSocket();
    const phases: string[] = [];
    const statuses: DeviceTerminalStatus[] = [];
    const resets = vi.fn();
    const bridge = new TerminalBridge(
      {
        onTransportChange: (phase) => phases.push(phase),
        onStatus: (status) => statuses.push(status),
        onReset: resets
      },
      (url) => {
        expect(url).toBe(terminalWebSocketURL());
        return socket as unknown as WebSocket;
      }
    );

    bridge.open();
    expect(socket.binaryType).toBe("arraybuffer");
    socket.open();
    expect(socket.sent).toEqual([JSON.stringify({ type: "status" })]);

    socket.receive(
      JSON.stringify({ type: "status", status: connectedStatus })
    );
    socket.receive(JSON.stringify({ type: "reset", reset: true }));
    expect(statuses).toEqual([connectedStatus]);
    expect(resets).toHaveBeenCalledOnce();
    expect(phases).toEqual(["opening", "ready"]);

    expect(bridge.connect()).toBe(true);
    expect(bridge.disconnect()).toBe(true);
    expect(socket.sent.slice(-2)).toEqual([
      JSON.stringify({ type: "connect" }),
      JSON.stringify({ type: "disconnect" })
    ]);
  });

  it("forwards binary output and reports structured server errors", () => {
    const socket = new FakeSocket();
    const output: Uint8Array[] = [];
    const errors: Array<[string, string]> = [];
    const bridge = new TerminalBridge(
      {
        onOutput: (data) => output.push(data),
        onError: (code, message) => errors.push([code, message])
      },
      () => socket as unknown as WebSocket
    );
    bridge.open();
    socket.open();

    socket.receive(Uint8Array.of(0x1b, 0x5b, 0x32, 0x4a).buffer);
    socket.receive(
      JSON.stringify({
        type: "error",
        code: "busy",
        message: "Terminal is connecting."
      })
    );

    expect(Array.from(output[0])).toEqual([0x1b, 0x5b, 0x32, 0x4a]);
    expect(errors).toEqual([["busy", "Terminal is connecting."]]);
  });

  it("rejects oversized paste bursts without writing or persisting transcript", () => {
    const socket = new FakeSocket();
    const error = vi.fn();
    const storage = vi.spyOn(Storage.prototype, "setItem");
    const bridge = new TerminalBridge(
      { onError: error },
      () => socket as unknown as WebSocket
    );
    bridge.open();
    socket.open();
    const before = socket.sent.length;

    expect(bridge.sendInput("x".repeat((16 << 10) + 1))).toBe(false);
    expect(socket.sent).toHaveLength(before);
    expect(error).toHaveBeenCalledWith(
      "input_too_large",
      expect.stringMatching(/16 KiB/)
    );
    expect(storage).not.toHaveBeenCalled();
  });

  it("closes only the page-local socket when disposed", () => {
    const socket = new FakeSocket();
    const bridge = new TerminalBridge(
      {},
      () => socket as unknown as WebSocket
    );
    bridge.open();
    socket.open();

    bridge.dispose();

    expect(socket.closes).toEqual([
      { code: 1000, reason: "terminal view closed" }
    ]);
  });
});

describe("terminal input framing", () => {
  it("maps the xterm Enter key to the firmware's CRLF line ending", () => {
    expect(terminalKeyboardInput("\r")).toBe("\r\n");
    expect(terminalKeyboardInput("state\r")).toBe("state\r");
    expect(terminalKeyboardInput("\n")).toBe("\n");
  });

  it("keeps UTF-8 code points intact while honoring the 4096-byte frame cap", () => {
    const input = "چراغ💡".repeat(1_000);
    const frames = terminalInputFrames(input);

    expect(frames.length).toBeGreaterThan(1);
    for (const frame of frames) {
      expect(frame.byteLength).toBeLessThanOrEqual(4_096);
      expect((frame[0] & 0xc0) === 0x80).toBe(false);
    }
    const rebuilt = new Uint8Array(
      frames.reduce((total, frame) => total + frame.byteLength, 0)
    );
    let offset = 0;
    for (const frame of frames) {
      rebuilt.set(frame, offset);
      offset += frame.byteLength;
    }
    expect(new TextDecoder().decode(rebuilt)).toBe(input);
  });

  it("derives ws and wss URLs without query credentials", () => {
    expect(terminalWebSocketURL("http://127.0.0.1:47831/app/")).toBe(
      "ws://127.0.0.1:47831/ui/terminal"
    );
    expect(terminalWebSocketURL("https://device-ui.example/app/")).toBe(
      "wss://device-ui.example/ui/terminal"
    );
  });
});
