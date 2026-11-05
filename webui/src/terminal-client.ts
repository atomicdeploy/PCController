import type { DeviceTerminalStatus } from "./contracts";
import {
  browserConsole,
  browserErrorMetadata
} from "./browser-console";

const maxTerminalInputFrame = 4 << 10;
const maxTerminalInputBurst = 16 << 10;
const socketOpen = 1;
const socketClosing = 2;
const socketClosed = 3;

export type TerminalTransportPhase = "opening" | "ready" | "closed";

export interface TerminalBridgeHandlers {
  onTransportChange?: (phase: TerminalTransportPhase) => void;
  onStatus?: (status: DeviceTerminalStatus) => void;
  onOutput?: (data: Uint8Array) => void;
  onReset?: () => void;
  onError?: (code: string, message: string) => void;
}

type WebSocketFactory = (url: string) => WebSocket;

type TerminalControlType = "connect" | "disconnect" | "status";

/**
 * A page-local bridge to the Go coordinator's terminal socket. Terminal
 * bytes deliberately never enter BroadcastChannel, localStorage, or the
 * application event stream.
 */
export class TerminalBridge {
  private readonly handlers: TerminalBridgeHandlers;
  private readonly createSocket: WebSocketFactory;
  private socket: WebSocket | null = null;
  private disposed = false;

  constructor(
    handlers: TerminalBridgeHandlers,
    createSocket: WebSocketFactory = (url) => new WebSocket(url)
  ) {
    this.handlers = handlers;
    this.createSocket = createSocket;
  }

  open(): void {
    if (
      this.disposed ||
      (this.socket && this.socket.readyState !== socketClosed)
    ) {
      return;
    }

    this.handlers.onTransportChange?.("opening");
    let socket: WebSocket;
    try {
      socket = this.createSocket(terminalWebSocketURL());
    } catch (error) {
      browserConsole.event(
        "error",
        "device.event",
        "Terminal bridge could not be opened",
        {
          source: "coordinator-terminal",
          action: "open",
          ...browserErrorMetadata(error)
        }
      );
      this.handlers.onTransportChange?.("closed");
      this.handlers.onError?.(
        "transport_unavailable",
        error instanceof Error
          ? error.message
          : "The coordinator terminal socket could not be opened."
      );
      return;
    }
    this.socket = socket;
    socket.binaryType = "arraybuffer";

    socket.addEventListener("open", () => {
      if (this.socket !== socket || this.disposed) return;
      browserConsole.event(
        "success",
        "device.event",
        "Terminal bridge ready",
        {
          source: "coordinator-terminal",
          action: "open"
        }
      );
      this.handlers.onTransportChange?.("ready");
      this.sendControl("status");
    });
    socket.addEventListener("message", (event) => {
      if (this.socket !== socket || this.disposed) return;
      void this.acceptMessage(event.data);
    });
    socket.addEventListener("close", (event) => {
      if (this.socket !== socket) return;
      this.socket = null;
      if (!this.disposed) {
        browserConsole.event(
          event.wasClean ? "info" : "warn",
          "device.event",
          "Terminal bridge closed",
          {
            source: "coordinator-terminal",
            action: "close",
            error_code: event.code ? `close_${event.code}` : "socket_closed"
          }
        );
        this.handlers.onTransportChange?.("closed");
      }
    });
    socket.addEventListener("error", () => {
      if (this.socket !== socket || this.disposed) return;
      browserConsole.event(
        "warn",
        "device.event",
        "Terminal bridge was interrupted",
        {
          source: "coordinator-terminal",
          action: "error",
          error_code: "transport_error"
        }
      );
      this.handlers.onError?.(
        "transport_error",
        "The coordinator terminal connection was interrupted."
      );
    });
  }

  connect(): boolean {
    return this.sendControl("connect");
  }

  disconnect(): boolean {
    return this.sendControl("disconnect");
  }

  requestStatus(): boolean {
    return this.sendControl("status");
  }

  sendInput(input: string | Uint8Array): boolean {
    const socket = this.openSocket();
    if (!socket) return false;

    const data =
      typeof input === "string" ? new TextEncoder().encode(input) : input;
    if (data.byteLength === 0) return true;
    if (data.byteLength > maxTerminalInputBurst) {
      this.handlers.onError?.(
        "input_too_large",
        "Paste is limited to 16 KiB. Send one short device command at a time."
      );
      return false;
    }
    for (const frame of terminalInputFrames(input)) {
      const payload = new Uint8Array(frame.byteLength);
      payload.set(frame);
      socket.send(payload.buffer);
    }
    return true;
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    const socket = this.socket;
    this.socket = null;
    if (
      socket &&
      socket.readyState !== socketClosing &&
      socket.readyState !== socketClosed
    ) {
      socket.close(1000, "terminal view closed");
    }
  }

  private sendControl(type: TerminalControlType): boolean {
    const socket = this.openSocket();
    if (!socket) return false;
    socket.send(JSON.stringify({ type }));
    return true;
  }

  private openSocket(): WebSocket | null {
    const socket = this.socket;
    return socket?.readyState === socketOpen ? socket : null;
  }

  private async acceptMessage(raw: unknown): Promise<void> {
    if (raw instanceof ArrayBuffer) {
      this.handlers.onOutput?.(new Uint8Array(raw));
      return;
    }
    if (ArrayBuffer.isView(raw)) {
      this.handlers.onOutput?.(
        new Uint8Array(raw.buffer, raw.byteOffset, raw.byteLength)
      );
      return;
    }
    if (raw instanceof Blob) {
      this.handlers.onOutput?.(new Uint8Array(await raw.arrayBuffer()));
      return;
    }
    if (typeof raw !== "string") {
      this.handlers.onError?.(
        "invalid_frame",
        "The coordinator sent an unsupported terminal frame."
      );
      return;
    }

    let value: unknown;
    try {
      value = JSON.parse(raw);
    } catch {
      this.handlers.onError?.(
        "invalid_frame",
        "The coordinator sent malformed terminal metadata."
      );
      return;
    }
    if (!value || typeof value !== "object") {
      this.handlers.onError?.(
        "invalid_frame",
        "The coordinator sent invalid terminal metadata."
      );
      return;
    }

    const frame = value as Record<string, unknown>;
    switch (frame.type) {
      case "status":
        if (isDeviceTerminalStatus(frame.status)) {
          this.handlers.onStatus?.(frame.status);
        } else {
          this.handlers.onError?.(
            "invalid_frame",
            "The coordinator sent an invalid terminal status."
          );
        }
        return;
      case "reset":
        this.handlers.onReset?.();
        return;
      case "error":
        this.handlers.onError?.(
          typeof frame.code === "string" && frame.code
            ? frame.code
            : "terminal_error",
          typeof frame.message === "string" && frame.message
            ? frame.message
            : "The device terminal reported an error."
        );
        return;
      default:
        this.handlers.onError?.(
          "invalid_frame",
          "The coordinator sent an unknown terminal control frame."
        );
    }
  }
}

export function terminalWebSocketURL(
  pageURL = window.location.href
): string {
  const url = new URL("/ui/terminal", pageURL);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.href;
}

export function terminalInputFrames(
  input: string | Uint8Array,
  maximum = maxTerminalInputFrame
): Uint8Array[] {
  if (!Number.isInteger(maximum) || maximum < 4) {
    throw new RangeError("terminal frame limit must be an integer of at least 4");
  }
  const encoded =
    typeof input === "string" ? new TextEncoder().encode(input) : input;
  const frames: Uint8Array[] = [];
  let offset = 0;
  while (offset < encoded.byteLength) {
    let end = Math.min(offset + maximum, encoded.byteLength);
    if (typeof input === "string" && end < encoded.byteLength) {
      // TextEncoder always produces valid UTF-8. Move a boundary back to the
      // next code-point lead byte rather than starting a frame mid-sequence.
      while (
        end > offset &&
        (encoded[end] & 0xc0) === 0x80
      ) {
        end--;
      }
    }
    if (end === offset) {
      end = Math.min(offset + maximum, encoded.byteLength);
    }
    frames.push(encoded.slice(offset, end));
    offset = end;
  }
  return frames;
}

// xterm emits a bare carriage return for the Enter key. The audited ASA
// firmware executes a shell line on LF, while accepting CRLF, so normalize
// only that single keyboard event. Pasted/raw data remains byte-for-byte
// unchanged.
export function terminalKeyboardInput(input: string): string {
  return input === "\r" ? "\r\n" : input;
}

export function isDeviceTerminalStatus(
  value: unknown
): value is DeviceTerminalStatus {
  if (!value || typeof value !== "object") return false;
  const candidate = value as Partial<DeviceTerminalStatus>;
  return (
    (candidate.state === "disconnected" ||
      candidate.state === "connecting" ||
      candidate.state === "connected") &&
    typeof candidate.host === "string" &&
    Number.isInteger(candidate.port) &&
    typeof candidate.session === "number" &&
    Number.isInteger(candidate.session) &&
    typeof candidate.sequence === "number" &&
    Number.isInteger(candidate.sequence)
  );
}
