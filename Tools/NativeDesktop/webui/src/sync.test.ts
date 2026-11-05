import { afterEach, describe, expect, it, vi } from "vitest";
import type { RayanLampClient } from "./api";
import {
  audioCueCount,
  connectionSyncNames,
  relayDisconnectReason,
  RealtimeSync,
  serverSequenceDecision
} from "./sync";
import {
  appPhase,
  applyBootstrap,
  config,
  controllerAvailable,
  isOnline,
  snapshot
} from "./store";
import { testBootstrap } from "./test/fixtures";

class TestBroadcastChannel {
  static readonly groups = new Map<string, Set<TestBroadcastChannel>>();

  readonly name: string;
  private readonly listeners =
    new Set<(event: MessageEvent<unknown>) => void>();

  constructor(name: string) {
    this.name = name;
    const group =
      TestBroadcastChannel.groups.get(name) ??
      new Set<TestBroadcastChannel>();
    group.add(this);
    TestBroadcastChannel.groups.set(name, group);
  }

  addEventListener(
    type: string,
    listener: EventListenerOrEventListenerObject
  ): void {
    if (type === "message" && typeof listener === "function") {
      this.listeners.add(
        listener as (event: MessageEvent<unknown>) => void
      );
    }
  }

  removeEventListener(
    type: string,
    listener: EventListenerOrEventListenerObject
  ): void {
    if (type === "message" && typeof listener === "function") {
      this.listeners.delete(
        listener as (event: MessageEvent<unknown>) => void
      );
    }
  }

  postMessage(data: unknown): void {
    for (const channel of TestBroadcastChannel.groups.get(this.name) ?? []) {
      if (channel === this) continue;
      for (const listener of channel.listeners) {
        listener({ data } as MessageEvent<unknown>);
      }
    }
  }

  close(): void {
    const group = TestBroadcastChannel.groups.get(this.name);
    group?.delete(this);
    if (group?.size === 0) TestBroadcastChannel.groups.delete(this.name);
    this.listeners.clear();
  }
}

class TestRealtimeSocket extends EventTarget {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static readonly instances: TestRealtimeSocket[] = [];

  readonly url: string;
  readyState = TestRealtimeSocket.CONNECTING;

  constructor(url: string) {
    super();
    this.url = url;
    TestRealtimeSocket.instances.push(this);
  }

  open(): void {
    this.readyState = TestRealtimeSocket.OPEN;
    this.dispatchEvent(new Event("open"));
  }

  close(code = 1000, reason = ""): void {
    if (this.readyState === TestRealtimeSocket.CLOSED) return;
    this.readyState = TestRealtimeSocket.CLOSED;
    this.dispatchEvent(
      new CloseEvent("close", {
        code,
        reason,
        wasClean: code !== 1006
      })
    );
  }
}

afterEach(() => {
  TestBroadcastChannel.groups.clear();
  TestRealtimeSocket.instances.length = 0;
  vi.unstubAllGlobals();
});

describe("server event sequence epochs", () => {
  it("accepts an authoritative hello after the server counter restarts", () => {
    expect(serverSequenceDecision("hello", 0, 47)).toEqual({
      accept: true,
      next: 0
    });
    expect(serverSequenceDecision("snapshot", 1, 0)).toEqual({
      accept: true,
      next: 1
    });
  });

  it("rejects duplicate ordinary events within one stream epoch", () => {
    expect(serverSequenceDecision("snapshot", 12, 12)).toEqual({
      accept: false,
      next: 12
    });
    expect(serverSequenceDecision("config", 11, 12)).toEqual({
      accept: false,
      next: 12
    });
  });
});

describe("audible cue projection", () => {
  it("accepts only the normalized beep count without exposing a raw frame", () => {
    expect(audioCueCount({ kind: "beep", count: 2 })).toBe(2);
    expect(audioCueCount({ kind: "message", count: 2 })).toBeNull();
    expect(audioCueCount({ kind: "beep", count: "2" })).toBeNull();
    expect(audioCueCount({ kind: "beep", count: 0 })).toBeNull();
    expect(audioCueCount({ kind: "beep", count: 10_000 })).toBe(10);
  });
});

describe("per-connection realtime coordination", () => {
  it("isolates relay and direct targets while remaining stable per target", () => {
    const first = connectionSyncNames("direct:http://rayanlamp.local");
    expect(connectionSyncNames("direct:http://rayanlamp.local")).toEqual(first);
    expect(connectionSyncNames("relay:http://127.0.0.1:47831")).not.toEqual(
      first
    );
    expect(connectionSyncNames("direct:http://other-lamp.local")).not.toEqual(
      first
    );
  });

  it("keeps config synchronization active when device events are disabled", () => {
    vi.stubGlobal("BroadcastChannel", TestBroadcastChannel);
    const realtimeURL = vi.fn(() => "ws://rayanlamp.local/ws");
    const api = {
      kind: "direct",
      displayName: "Direct to lamp",
      syncKey: "direct:http://rayanlamp.local",
      realtimeURL
    } as unknown as RayanLampClient;
    const bootstrap = structuredClone(testBootstrap);
    bootstrap.config.ui.theme = "dark";
    applyBootstrap(bootstrap);
    const sync = new RealtimeSync(api, { connectEvents: false });
    sync.start();

    const remoteConfig = structuredClone(bootstrap.config);
    remoteConfig.ui.theme = "light";
    const publisher = new TestBroadcastChannel(
      connectionSyncNames(api.syncKey).channel
    );
    publisher.postMessage({
      protocol: 1,
      sender: "another-static-tab",
      kind: "config",
      revision: 12,
      config: remoteConfig,
      connected: false
    });

    expect(config.value?.ui.theme).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(realtimeURL).not.toHaveBeenCalled();
    publisher.close();
    sync.stop();
  });

  it("plays a direct initial-sync cue in the elected WebSocket tab only", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("BroadcastChannel", TestBroadcastChannel);
    vi.stubGlobal("WebSocket", TestRealtimeSocket);
    try {
      const bootstrap = structuredClone(testBootstrap);
      bootstrap.config.notifications.sound = true;
      bootstrap.config.notifications.initial_sync = true;
      applyBootstrap(bootstrap);
      const playAudio = vi.fn(async () => ({}));
      const api = {
        kind: "direct",
        displayName: "Direct to lamp",
        syncKey: "direct:http://rayanlamp.local",
        realtimeURL: () => "ws://rayanlamp.local/ws",
        realtimeOpened: () => [
          { type: "snapshot", snapshot: structuredClone(bootstrap.snapshot) }
        ],
        realtimeClosed: () => [],
        decodeRealtimeMessage: () => [],
        playAudio
      } as unknown as RayanLampClient;

      const sync = new RealtimeSync(api);
      sync.start();
      await vi.advanceTimersByTimeAsync(100);
      TestRealtimeSocket.instances[0].open();

      expect(playAudio).toHaveBeenCalledOnce();
      expect(playAudio).toHaveBeenCalledWith("off", 1);
      sync.stop();
    } finally {
      vi.useRealTimers();
    }
  });

  it("uses one BroadcastChannel lease owner when Web Locks are unavailable", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("BroadcastChannel", TestBroadcastChannel);
    vi.stubGlobal("WebSocket", TestRealtimeSocket);
    try {
      const bootstrap = structuredClone(testBootstrap);
      bootstrap.config.notifications.sound = true;
      bootstrap.config.notifications.initial_sync = true;
      applyBootstrap(bootstrap);
      const playAudio = vi.fn(async () => ({}));
      const api = {
        kind: "direct",
        displayName: "Direct to lamp",
        syncKey: "direct:http://rayanlamp.local",
        realtimeURL: () => "ws://rayanlamp.local/ws",
        realtimeOpened: () => [
          { type: "snapshot", snapshot: structuredClone(bootstrap.snapshot) }
        ],
        realtimeClosed: () => [],
        decodeRealtimeMessage: () => [],
        playAudio
      } as unknown as RayanLampClient;
      const first = new RealtimeSync(api);
      const second = new RealtimeSync(api);

      first.start();
      second.start();
      await vi.advanceTimersByTimeAsync(100);

      expect(TestRealtimeSocket.instances).toHaveLength(1);
      TestRealtimeSocket.instances[0].open();
      expect(playAudio).toHaveBeenCalledOnce();
      first.stop();
      second.stop();
    } finally {
      vi.useRealTimers();
    }
  });

  it("applies HTTP snapshots through the elected direct-state cue policy", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("BroadcastChannel", TestBroadcastChannel);
    try {
      const bootstrap = structuredClone(testBootstrap);
      bootstrap.config.notifications.sound = true;
      bootstrap.config.notifications.initial_sync = false;
      applyBootstrap(bootstrap);
      const playAudio = vi.fn(async () => ({}));
      const api = {
        kind: "direct",
        displayName: "Direct to lamp",
        syncKey: "direct:http://rayanlamp.local",
        realtimeURL: () => "ws://rayanlamp.local/ws",
        playAudio
      } as unknown as RayanLampClient;
      const sync = new RealtimeSync(api, { connectEvents: false });
      sync.start();
      await vi.advanceTimersByTimeAsync(100);

      sync.acceptSnapshot(structuredClone(bootstrap.snapshot));
      sync.acceptSnapshot({ ...bootstrap.snapshot, state: "on" });

      expect(playAudio).toHaveBeenCalledOnce();
      expect(playAudio).toHaveBeenCalledWith("on", 1);
      sync.stop();
    } finally {
      vi.useRealTimers();
    }
  });

  it("never replays direct audio on BroadcastChannel followers", () => {
    vi.stubGlobal("BroadcastChannel", TestBroadcastChannel);
    const bootstrap = structuredClone(testBootstrap);
    applyBootstrap(bootstrap);
    const playAudio = vi.fn(async () => ({}));
    const api = {
      kind: "direct",
      displayName: "Direct to lamp",
      syncKey: "direct:http://rayanlamp.local",
      realtimeURL: () => "ws://rayanlamp.local/ws",
      playAudio
    } as unknown as RayanLampClient;
    const sync = new RealtimeSync(api, { connectEvents: false });
    sync.start();
    const publisher = new TestBroadcastChannel(
      connectionSyncNames(api.syncKey).channel
    );

    publisher.postMessage({
      protocol: 1,
      sender: "leader-tab",
      kind: "snapshot",
      snapshot: { ...bootstrap.snapshot, state: "on" },
      connected: true
    });
    publisher.postMessage({
      protocol: 1,
      sender: "leader-tab",
      kind: "message",
      title: "Rayan Lamp",
      message: "beep",
      cueCount: 2,
      connected: true
    });

    expect(playAudio).not.toHaveBeenCalled();
    publisher.close();
    sync.stop();
  });

  it("marks the coordinator unavailable on its graceful close frame", async () => {
    vi.useFakeTimers();
    vi.stubGlobal("BroadcastChannel", TestBroadcastChannel);
    vi.stubGlobal("WebSocket", TestRealtimeSocket);
    try {
      applyBootstrap(structuredClone(testBootstrap));
      const api = {
        kind: "relay",
        displayName: "Go coordinator",
        syncKey: "relay:http://127.0.0.1:47831",
        realtimeURL: () => "ws://127.0.0.1:47831/events",
        realtimeOpened: () => [],
        realtimeClosed: () => [],
        decodeRealtimeMessage: () => [],
        establishSession: vi.fn(async () => "csrf-next")
      } as unknown as RayanLampClient;
      const sync = new RealtimeSync(api);
      sync.start();
      await vi.advanceTimersByTimeAsync(100);

      const socket = TestRealtimeSocket.instances[0];
      expect(socket).toBeTruthy();
      socket.open();
      expect(controllerAvailable.value).toBe(true);
      expect(isOnline.value).toBe(true);

      socket.close(1001, "server shutting down");

      expect(controllerAvailable.value).toBe(false);
      expect(isOnline.value).toBe(false);
      expect(appPhase.value).toBe("offline");
      expect(snapshot.value.last_error).toBe(
        "The RayanLamp controller shut down."
      );
      sync.stop();
    } finally {
      vi.useRealTimers();
    }
  });

  it("renews the browser session before reconnecting to a restarted executable", async () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0.5);
    vi.stubGlobal("BroadcastChannel", TestBroadcastChannel);
    vi.stubGlobal("WebSocket", TestRealtimeSocket);
    try {
      applyBootstrap(structuredClone(testBootstrap));
      const establishSession = vi.fn(async () => "csrf-next");
      const api = {
        kind: "relay",
        displayName: "Go coordinator",
        syncKey: "relay:http://127.0.0.1:47831",
        realtimeURL: () => "ws://127.0.0.1:47831/events",
        realtimeOpened: () => [],
        realtimeClosed: () => [],
        decodeRealtimeMessage: () => [],
        establishSession
      } as unknown as RayanLampClient;
      const sync = new RealtimeSync(api);
      sync.start();
      await vi.advanceTimersByTimeAsync(100);
      TestRealtimeSocket.instances[0].open();
      TestRealtimeSocket.instances[0].close(
        1001,
        "server shutting down"
      );

      await vi.advanceTimersByTimeAsync(500);

      expect(establishSession).toHaveBeenCalledWith(true);
      expect(TestRealtimeSocket.instances).toHaveLength(2);
      sync.stop();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("relay close messaging", () => {
  it("distinguishes deliberate shutdown from an unclean loss", () => {
    expect(
      relayDisconnectReason({
        code: 1001,
        reason: "server shutting down",
        wasClean: true
      })
    ).toBe("The RayanLamp controller shut down.");
    expect(
      relayDisconnectReason({
        code: 1006,
        reason: "",
        wasClean: false
      })
    ).toBe("The RayanLamp controller connection was lost.");
  });
});
