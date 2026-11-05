import type { RayanLampClient } from "./api";
import type {
  AppConfig,
  AudioCue,
  Revision,
  ServerEvent,
  Snapshot,
  UIDiagnosticsSnapshot
} from "./contracts";
import { isAppConfig, isSnapshot } from "./contracts";
import {
  acceptRemoteConfig,
  applySnapshot as applyStoreSnapshot,
  config,
  diagnostics,
  eventConnected,
  markControllerAvailable,
  markControllerUnavailable,
  revision,
  showToast,
  snapshot as currentSnapshot
} from "./store";
import {
  browserConsole,
  browserErrorMetadata,
  type BrowserConsoleMetadata
} from "./browser-console";
import { webHaptics } from "./haptics";

interface SyncMessage {
  protocol: 1;
  sender: string;
  kind:
    | "snapshot"
    | "snapshot-observed"
    | "config"
    | "diagnostics"
    | "message"
    | "heartbeat"
    | "lease";
  revision?: Revision;
  snapshot?: Snapshot;
  config?: AppConfig;
  diagnostics?: UIDiagnosticsSnapshot;
  title?: string;
  message?: string;
  cueCount?: number;
  connected?: boolean;
  reason?: string;
  leaseAction?: "candidate" | "owner" | "release";
}

export interface RealtimeSyncOptions {
  connectEvents?: boolean;
}

const fallbackElectionDelay = 80;
const fallbackLeaseDuration = 12_000;
const fallbackLeaseHeartbeat = 3_000;

export class RealtimeSync {
  private readonly api: RayanLampClient;
  private readonly connectEvents: boolean;
  private readonly tabID = crypto.randomUUID();
  private readonly channel: BroadcastChannel | null;
  private readonly lockName: string;
  private stopped = false;
  private socket: WebSocket | null = null;
  private retry = 0;
  private heartbeat = 0;
  private followerWatchdog = 0;
  private reconnectTimer = 0;
  private releaseLeader: (() => void) | null = null;
  private ownsLeadership = false;
  private leadershipMode: "locks" | "lease" | "uncoordinated" | null = null;
  private leaseOwner = "";
  private leaseExpiresAt = 0;
  private leaseElectionTimer = 0;
  private leaseExpiryTimer = 0;
  private leaseHeartbeatTimer = 0;
  private readonly leaseCandidates = new Set<string>();
  private lastServerSequence = 0;
  private lastLoggedConnection: boolean | null = null;
  private directKnownStateSeen = false;

  constructor(
    api: RayanLampClient,
    options: RealtimeSyncOptions = {}
  ) {
    this.api = api;
    this.connectEvents = options.connectEvents !== false;
    const names = connectionSyncNames(api.syncKey);
    this.lockName = names.lock;
    this.channel =
      typeof BroadcastChannel === "function"
        ? new BroadcastChannel(names.channel)
        : null;
  }

  start(): void {
    if (this.channel) {
      this.channel.addEventListener("message", this.onChannelMessage);
    }
    if (this.connectEvents) {
      this.heartbeat = window.setInterval(() => {
        this.announceConnection();
      }, 5_000);
      // Ask an existing leader immediately instead of showing a false
      // reconnecting state until the first periodic heartbeat.
      this.broadcast({ kind: "heartbeat" });
    }
    // Leadership also owns direct-mode HTTP transition sounds. Keep the
    // election alive when device WebSockets are disabled so polling from
    // several tabs cannot replay the same cue.
    void this.electLeader();
  }

  stop(): void {
    this.stopped = true;
    if (this.leadershipMode === "lease" && this.ownsLeadership) {
      this.broadcast({ kind: "lease", leaseAction: "release" });
    }
    this.releaseLeader?.();
    this.releaseLeader = null;
    this.releaseLeadership();
    window.clearInterval(this.heartbeat);
    window.clearTimeout(this.followerWatchdog);
    window.clearTimeout(this.reconnectTimer);
    window.clearTimeout(this.leaseElectionTimer);
    window.clearTimeout(this.leaseExpiryTimer);
    window.clearInterval(this.leaseHeartbeatTimer);
    this.channel?.removeEventListener("message", this.onChannelMessage);
    this.channel?.close();
  }

  private async electLeader(): Promise<void> {
    const locks = navigator.locks;
    if (!locks) {
      if (this.channel) {
        this.beginLeaseElection();
      } else {
        // There is no cross-tab primitive left to coordinate with. Retain a
        // usable single-tab dashboard, while modern browsers use either Web
        // Locks or the BroadcastChannel lease above.
        this.acquireLeadership("uncoordinated");
      }
      return;
    }
    while (!this.stopped) {
      let acquired = false;
      await locks.request(
        this.lockName,
        { mode: "exclusive", ifAvailable: true },
        async (lock) => {
          if (!lock || this.stopped) return;
          acquired = true;
          this.acquireLeadership("locks");
          await new Promise<void>((resolve) => {
            this.releaseLeader = resolve;
          });
          this.releaseLeader = null;
          this.releaseLeadership();
        }
      );
      if (!acquired) {
        await new Promise((resolve) => window.setTimeout(resolve, 2_000));
      }
    }
  }

  private openSocket(): void {
    if (
      this.stopped ||
      !this.ownsLeadership ||
      !this.connectEvents ||
      this.socket
    ) {
      return;
    }
    let socket: WebSocket;
    const attempt = this.retry + 1;
    const connectStarted = performance.now();
    try {
      socket = new WebSocket(this.api.realtimeURL());
    } catch (error) {
      const reason = relayDisconnectReason();
      this.setConnectionState(false, reason, {
        action: "leader",
        attempt,
        elapsed_ms: Math.round(performance.now() - connectStarted),
        ...browserErrorMetadata(error)
      });
      this.broadcast({ kind: "heartbeat", connected: false, reason });
      for (const event of this.api.realtimeClosed()) {
        this.handleServerEvent(event);
      }
      if (!this.stopped) {
        this.scheduleReconnect();
      }
      return;
    }
    this.socket = socket;
    socket.addEventListener("open", () => {
      this.retry = 0;
      window.clearTimeout(this.reconnectTimer);
      this.setConnectionState(true, undefined, {
        action: "leader",
        attempt,
        elapsed_ms: Math.round(performance.now() - connectStarted)
      });
      this.broadcast({ kind: "heartbeat", connected: true });
      try {
        for (const event of this.api.realtimeOpened(socket)) {
          this.handleServerEvent(event);
        }
      } catch {
        socket.close();
      }
    });
    socket.addEventListener("message", (event) => {
      this.handleServerMessage(event.data);
    });
    socket.addEventListener("close", (event) => {
      if (this.socket === socket) this.socket = null;
      if (this.stopped || !this.ownsLeadership) return;
      const reason = relayDisconnectReason(event);
      this.setConnectionState(false, reason, {
        action: "leader",
        attempt: this.retry + 1,
        error_code: event.code ? `close_${event.code}` : "socket_closed"
      });
      this.broadcast({ kind: "heartbeat", connected: false, reason });
      for (const event of this.api.realtimeClosed()) {
        this.handleServerEvent(event);
      }
      if (!this.stopped && this.ownsLeadership) {
        this.scheduleReconnect();
      }
    });
    socket.addEventListener("error", () => {
      socket.close();
    });
  }

  private scheduleReconnect(): void {
    if (
      this.stopped ||
      !this.ownsLeadership ||
      !this.connectEvents
    ) {
      return;
    }
    const base = Math.min(10_000, 500 * 2 ** this.retry++);
    const delay = base * (0.8 + Math.random() * 0.4);
    window.clearTimeout(this.reconnectTimer);
    this.reconnectTimer = window.setTimeout(
      () => void this.prepareReconnect(),
      delay
    );
  }

  private async prepareReconnect(): Promise<void> {
    if (
      this.stopped ||
      !this.ownsLeadership ||
      !this.connectEvents ||
      this.socket
    ) {
      return;
    }
    if (this.api.kind === "relay") {
      try {
        // A restarted executable has a fresh in-memory browser-session store.
        // Refresh the cookie before upgrading instead of retrying forever with
        // the session minted by the previous process.
        await this.api.establishSession(true);
      } catch (error) {
        if (!this.stopped) {
          this.scheduleReconnect();
          browserConsole.event(
            "warn",
            "websocket.disconnected",
            "Coordinator session refresh failed; retry scheduled",
            {
              source: this.api.kind,
              attempt: this.retry,
              ...browserErrorMetadata(error)
            }
          );
        }
        return;
      }
    }
    this.openSocket();
  }

  private acquireLeadership(
    mode: "locks" | "lease" | "uncoordinated"
  ): void {
    if (this.stopped) return;
    this.ownsLeadership = true;
    this.leadershipMode = mode;
    if (this.connectEvents) {
      this.openSocket();
    }
  }

  private releaseLeadership(): void {
    this.ownsLeadership = false;
    this.leadershipMode = null;
    window.clearTimeout(this.reconnectTimer);
    this.socket?.close(
      this.stopped ? 1000 : 1001,
      this.stopped ? "page closed" : "leadership released"
    );
    this.socket = null;
  }

  private beginLeaseElection(announce = true): void {
    if (
      this.stopped ||
      this.ownsLeadership ||
      this.hasActiveLeaseOwner() ||
      this.leaseElectionTimer
    ) {
      return;
    }
    this.leaseCandidates.clear();
    this.leaseCandidates.add(this.tabID);
    if (announce) {
      this.broadcast({ kind: "lease", leaseAction: "candidate" });
    }
    this.leaseElectionTimer = window.setTimeout(() => {
      this.leaseElectionTimer = 0;
      this.completeLeaseElection();
    }, fallbackElectionDelay);
  }

  private completeLeaseElection(): void {
    if (this.stopped || this.ownsLeadership || this.hasActiveLeaseOwner()) {
      this.leaseCandidates.clear();
      return;
    }
    const winner = [...this.leaseCandidates].sort()[0] ?? this.tabID;
    this.leaseCandidates.clear();
    if (winner === this.tabID) {
      this.leaseOwner = this.tabID;
      this.leaseExpiresAt = Date.now() + fallbackLeaseDuration;
      this.acquireLeadership("lease");
      this.announceLeaseOwnership();
      window.clearInterval(this.leaseHeartbeatTimer);
      this.leaseHeartbeatTimer = window.setInterval(
        () => this.announceLeaseOwnership(),
        fallbackLeaseHeartbeat
      );
      return;
    }
    // Give the deterministic winner a short window to publish its full lease.
    // If it disappeared during the election, every follower retries promptly.
    this.leaseOwner = winner;
    this.leaseExpiresAt = Date.now() + fallbackElectionDelay * 4;
    this.armLeaseExpiry();
  }

  private announceLeaseOwnership(): void {
    if (
      this.stopped ||
      !this.ownsLeadership ||
      this.leadershipMode !== "lease"
    ) {
      return;
    }
    this.leaseOwner = this.tabID;
    this.leaseExpiresAt = Date.now() + fallbackLeaseDuration;
    this.broadcast({ kind: "lease", leaseAction: "owner" });
  }

  private handleLeaseMessage(value: SyncMessage): void {
    if (navigator.locks || !this.channel || this.stopped) return;
    switch (value.leaseAction) {
      case "candidate": {
        if (this.ownsLeadership && this.leadershipMode === "lease") {
          this.announceLeaseOwnership();
          return;
        }
        if (this.hasActiveLeaseOwner()) return;
        const wasNew = !this.leaseCandidates.has(value.sender);
        this.leaseCandidates.add(value.sender);
        if (!this.leaseElectionTimer) {
          this.beginLeaseElection(false);
          this.leaseCandidates.add(value.sender);
        }
        // A tab that started first may have announced before this listener
        // existed. Echo our candidacy once so all contenders converge on the
        // same lexicographically smallest UUID without a split-brain socket.
        if (wasNew) {
          this.broadcast({ kind: "lease", leaseAction: "candidate" });
        }
        return;
      }
      case "owner":
        this.acceptLeaseOwner(value.sender);
        return;
      case "release":
        if (
          !this.ownsLeadership &&
          this.leaseOwner === value.sender
        ) {
          this.clearLeaseOwner();
          this.beginLeaseElection();
        }
        return;
      default:
        return;
    }
  }

  private acceptLeaseOwner(owner: string): void {
    if (!owner || owner === this.tabID) return;
    if (this.ownsLeadership && this.leadershipMode === "lease") {
      // Two owners can exist briefly if a background tab was throttled past
      // its lease. Deterministic tie-breaking converges without letting both
      // sockets continue to own audio.
      if (owner < this.tabID) {
        window.clearInterval(this.leaseHeartbeatTimer);
        this.releaseLeadership();
      } else {
        this.announceLeaseOwnership();
        return;
      }
    }
    window.clearTimeout(this.leaseElectionTimer);
    this.leaseElectionTimer = 0;
    this.leaseCandidates.clear();
    this.leaseOwner = owner;
    this.leaseExpiresAt = Date.now() + fallbackLeaseDuration;
    this.armLeaseExpiry();
  }

  private hasActiveLeaseOwner(): boolean {
    return (
      this.leaseOwner !== "" &&
      this.leaseOwner !== this.tabID &&
      this.leaseExpiresAt > Date.now()
    );
  }

  private armLeaseExpiry(): void {
    window.clearTimeout(this.leaseExpiryTimer);
    if (!this.hasActiveLeaseOwner()) {
      this.clearLeaseOwner();
      this.beginLeaseElection();
      return;
    }
    this.leaseExpiryTimer = window.setTimeout(() => {
      this.leaseExpiryTimer = 0;
      if (this.hasActiveLeaseOwner()) {
        this.armLeaseExpiry();
        return;
      }
      this.clearLeaseOwner();
      this.beginLeaseElection();
    }, Math.max(1, this.leaseExpiresAt - Date.now()));
  }

  private clearLeaseOwner(): void {
    this.leaseOwner = "";
    this.leaseExpiresAt = 0;
    window.clearTimeout(this.leaseExpiryTimer);
    this.leaseExpiryTimer = 0;
  }

  private setConnectionState(
    connected: boolean,
    reason?: string,
    metadata: BrowserConsoleMetadata = {}
  ): void {
    eventConnected.value = connected;
    if (this.lastLoggedConnection !== connected) {
      this.lastLoggedConnection = connected;
      browserConsole.event(
        connected ? "success" : "warn",
        connected ? "websocket.connected" : "websocket.disconnected",
        connected
          ? "Realtime events connected"
          : "Realtime events disconnected; reconnecting",
        {
          source: this.api.kind,
          ...metadata
        }
      );
    }
    if (this.api.kind !== "relay") return;
    if (connected) {
      markControllerAvailable();
      return;
    }
    markControllerUnavailable(reason);
  }

  private handleServerMessage(raw: unknown): void {
    for (const event of this.api.decodeRealtimeMessage(raw)) {
      this.handleServerEvent(event);
    }
  }

  /**
   * Accepts a confirmed snapshot from HTTP polling or a command refresh.
   * Direct-mode transition policy remains owned by the elected tab: followers
   * update immediately, then forward the observation so the owner plays and
   * broadcasts the cue exactly once.
   */
  acceptSnapshot(next: Snapshot): void {
    if (this.api.kind !== "direct") {
      applyStoreSnapshot(next);
      return;
    }
    if (this.ownsLeadership) {
      this.applyOwnedDirectSnapshot(next);
      return;
    }
    if (next.state !== "unknown") {
      this.directKnownStateSeen = true;
    }
    applyStoreSnapshot(next);
    this.broadcast({
      kind: "snapshot-observed",
      snapshot: next,
      revision: revision.value,
      connected: eventConnected.value
    });
  }

  private applyOwnedDirectSnapshot(next: Snapshot): void {
    this.handleDirectStateCue(next);
    applyStoreSnapshot(next);
    this.broadcast({
      kind: "snapshot",
      snapshot: next,
      revision: revision.value,
      connected: eventConnected.value
    });
  }

  private handleServerEvent(event: ServerEvent): void {
    const sequence = serverSequenceDecision(
      event.type,
      event.sequence,
      this.lastServerSequence
    );
    if (!sequence.accept) return;
    this.lastServerSequence = sequence.next;
    const eventRevision = event.revision ?? revision.value;
    let recognized = false;

    const projectedSnapshot =
      event.snapshot && isSnapshot(event.snapshot)
        ? event.snapshot
        : isSnapshot(event)
          ? event
          : null;
    if (projectedSnapshot) {
      recognized = true;
      this.handleDirectStateCue(projectedSnapshot);
      applyStoreSnapshot(projectedSnapshot);
      this.broadcast({
        kind: "snapshot",
        snapshot: projectedSnapshot,
        revision: eventRevision,
        connected: eventConnected.value
      });
    }
    if (event.config && isAppConfig(event.config)) {
      recognized = true;
      acceptRemoteConfig(event.config, eventRevision);
      this.broadcast({
        kind: "config",
        config: event.config,
        revision: eventRevision,
        connected: eventConnected.value
      });
    }
    if (event.diagnostics && typeof event.diagnostics === "object") {
      recognized = true;
      diagnostics.value = event.diagnostics;
      browserConsole.event(
        "success",
        "diagnostics.loaded",
        "Live diagnostics synchronized",
        {
          source: this.api.kind,
          count: Object.keys(event.diagnostics.endpoints ?? {}).length
        }
      );
      this.broadcast({
        kind: "diagnostics",
        diagnostics: event.diagnostics,
        connected: eventConnected.value
      });
    }
    if (event.message) {
      recognized = true;
      const cueCount = audioCueCount(event.result);
      if (cueCount !== null) {
        if (this.api.kind === "direct") {
          this.playDirectCue("message", cueCount);
        }
        webHaptics.audioCue(cueCount);
        browserConsole.event(
          "info",
          "audio.cue",
          cueCount === 1
            ? "Lamp requested an audible cue"
            : `Lamp requested ${cueCount} audible cues`,
          {
            source: this.api.kind,
            action: "beep",
            count: cueCount
          }
        );
      } else {
        if (
          this.api.kind === "direct" &&
          config.value?.notifications.sound
        ) {
          this.playDirectCue("message");
        }
        webHaptics.trigger("message.received");
        browserConsole.event(
          "info",
          "message.received",
          "Lamp message received",
          {
            source: this.api.kind,
            action: event.title || "Rayan Lamp",
            count: event.message.length
          }
        );
      }
      showToast("info", event.title || "Rayan Lamp", event.message);
      this.broadcast({
        kind: "message",
        title: event.title,
        message: event.message,
        ...(cueCount !== null ? { cueCount } : {}),
        revision: eventRevision,
        connected: eventConnected.value
      });
    }
    if (!recognized && event.type) {
      browserConsole.event(
        "debug",
        "device.event",
        "Unprojected device event received",
        {
          source: this.api.kind,
          action: event.type
        }
      );
    }
  }

  private readonly onChannelMessage = (
    incoming: MessageEvent<SyncMessage>
  ): void => {
    const value = incoming.data;
    if (
      !value ||
      value.protocol !== 1 ||
      value.sender === this.tabID
    ) {
      return;
    }
    if (value.kind === "lease") {
      this.handleLeaseMessage(value);
      return;
    }
    if (
      value.kind === "snapshot-observed" &&
      value.snapshot &&
      isSnapshot(value.snapshot)
    ) {
      if (this.api.kind === "direct" && this.ownsLeadership) {
        this.applyOwnedDirectSnapshot(value.snapshot);
      }
      return;
    }
    if (value.kind === "heartbeat") {
      if (typeof value.connected === "boolean") {
        this.setConnectionState(value.connected, value.reason, {
          action: "follower"
        });
        this.armFollowerWatchdog(value.connected);
      } else if (this.socket?.readyState === WebSocket.OPEN) {
        // A follower has just started (or resumed) and is asking the elected
        // WebSocket owner for the actual stream health.
        this.broadcast({ kind: "heartbeat", connected: true });
        if (this.api.kind === "direct") {
          this.broadcast({
            kind: "snapshot",
            snapshot: currentSnapshot.value,
            revision: revision.value,
            connected: true
          });
        }
      }
      return;
    }
    const connected =
      typeof value.connected === "boolean" ? value.connected : true;
    this.setConnectionState(connected, value.reason, {
      action: "follower"
    });
    this.armFollowerWatchdog(connected);
    if (value.kind === "snapshot" && value.snapshot && isSnapshot(value.snapshot)) {
      if (this.api.kind === "direct" && value.snapshot.state !== "unknown") {
        this.directKnownStateSeen = true;
      }
      applyStoreSnapshot(value.snapshot);
    } else if (
      value.kind === "config" &&
      value.config &&
      isAppConfig(value.config)
    ) {
      acceptRemoteConfig(value.config, value.revision ?? revision.value);
    } else if (value.kind === "diagnostics" && value.diagnostics) {
      diagnostics.value = value.diagnostics;
      browserConsole.event(
        "success",
        "diagnostics.loaded",
        "Diagnostics synchronized from another tab",
        {
          source: this.api.kind,
          action: "follower",
          count: Object.keys(value.diagnostics.endpoints).length
        }
      );
    } else if (value.kind === "message" && value.message) {
      const cueCount = audioCueCount({
        kind: value.cueCount === undefined ? "message" : "beep",
        count: value.cueCount
      });
      if (cueCount !== null) {
        webHaptics.audioCue(cueCount);
        browserConsole.event(
          "info",
          "audio.cue",
          cueCount === 1
            ? "Lamp requested an audible cue"
            : `Lamp requested ${cueCount} audible cues`,
          {
            source: this.api.kind,
            action: "beep",
            count: cueCount
          }
        );
      } else {
        webHaptics.trigger("message.received");
        browserConsole.event(
          "info",
          "message.received",
          "Lamp message synchronized from another tab",
          {
            source: this.api.kind,
            action: value.title || "Rayan Lamp",
            count: value.message.length
          }
        );
      }
      showToast("info", value.title || "Rayan Lamp", value.message);
    }
  };

  private handleDirectStateCue(next: Snapshot): void {
    if (this.api.kind !== "direct" || next.state === "unknown") return;
    const settings = config.value?.notifications;
    const initial = !this.directKnownStateSeen;
    const changed = currentSnapshot.value.state !== next.state;
    this.directKnownStateSeen = true;
    if (
      !settings?.sound ||
      (initial ? !settings.initial_sync : !changed)
    ) {
      return;
    }
    this.playDirectCue(next.state === "on" ? "on" : "off");
  }

  private playDirectCue(cue: AudioCue, count = 1): void {
    void this.api.playAudio(cue, count).catch((error) => {
      browserConsole.event(
        "warn",
        "audio.error",
        "Browser audio playback failed",
        {
          source: this.api.kind,
          action: cue,
          count,
          ...browserErrorMetadata(error)
        }
      );
    });
  }

  private broadcast(
    message: Omit<SyncMessage, "protocol" | "sender">
  ): void {
    this.channel?.postMessage({
      protocol: 1,
      sender: this.tabID,
      ...message
    } satisfies SyncMessage);
  }

  private announceConnection(): void {
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.broadcast({ kind: "heartbeat", connected: true });
      return;
    }
    this.broadcast({ kind: "heartbeat" });
  }

  private armFollowerWatchdog(connected: boolean): void {
    window.clearTimeout(this.followerWatchdog);
    if (!connected || this.socket?.readyState === WebSocket.OPEN) return;
    this.followerWatchdog = window.setTimeout(() => {
      if (this.socket?.readyState !== WebSocket.OPEN) {
        this.setConnectionState(
          false,
          "The RayanLamp controller heartbeat stopped.",
          {
            action: "follower",
            error_code: "heartbeat_timeout"
          }
        );
      }
    }, 12_000);
  }
}

export function audioCueCount(result: unknown): number | null {
  if (!result || typeof result !== "object") return null;
  const candidate = result as { kind?: unknown; count?: unknown };
  if (
    candidate.kind !== "beep" ||
    typeof candidate.count !== "number" ||
    !Number.isInteger(candidate.count) ||
    candidate.count < 1
  ) {
    return null;
  }
  return Math.min(candidate.count, 10);
}

export function relayDisconnectReason(
  event?: Pick<CloseEvent, "code" | "reason" | "wasClean">
): string {
  if (
    event?.code === 1001 &&
    event.reason.trim().toLowerCase() === "server shutting down"
  ) {
    return "The RayanLamp controller shut down.";
  }
  if (event?.wasClean) {
    return "The RayanLamp controller closed its live connection.";
  }
  return "The RayanLamp controller connection was lost.";
}

export function connectionSyncNames(key: string): {
  channel: string;
  lock: string;
} {
  // BroadcastChannel and Web Locks accept arbitrary strings, but a short
  // stable suffix makes diagnostics readable and avoids exposing a full local
  // device URL to browser tooling.
  let hash = 0x811c9dc5;
  for (let index = 0; index < key.length; index++) {
    hash ^= key.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
  }
  const suffix = (hash >>> 0).toString(16).padStart(8, "0");
  return {
    channel: `rayanlamp-web-v1-${suffix}`,
    lock: `rayanlamp-web-event-leader-v1-${suffix}`
  };
}

export function serverSequenceDecision(
  type: string | undefined,
  incoming: number | undefined,
  previous: number
): { accept: boolean; next: number } {
  if (typeof incoming !== "number") {
    return { accept: true, next: previous };
  }
  // A hello is an authoritative snapshot and also establishes a new stream
  // epoch. The local API's counter legitimately restarts when the listener or
  // process restarts, so comparing it to the prior socket would leave a tab
  // permanently stale.
  if (type === "hello") {
    return { accept: true, next: incoming };
  }
  if (incoming <= previous) {
    return { accept: false, next: previous };
  }
  return { accept: true, next: incoming };
}
