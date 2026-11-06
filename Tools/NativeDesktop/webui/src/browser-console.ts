/**
 * Small, structured browser-console surface shared with the native/TUI event
 * vocabulary. It is deliberately an operational trace rather than a dump:
 * metadata is allowlisted per event, recursively sanitized, and repeated
 * heartbeats/state polls are never logged by callers.
 */

export type BrowserConsoleLevel =
  | "debug"
  | "info"
  | "success"
  | "warn"
  | "error";

export type BrowserConsoleEvent =
  | "app.start"
  | "transport.ready"
  | "transport.error"
  | "device.state"
  | "device.command"
  | "websocket.connected"
  | "websocket.disconnected"
  | "message.received"
  | "audio.cue"
  | "audio.error"
  | "device.event"
  | "diagnostics.loaded"
  | "diagnostics.error"
  | "settings.saved"
  | "settings.error";

export type BrowserConsoleMetadata = Record<string, unknown>;

export interface BrowserConsoleOptions {
  dedupeKey?: string;
  dedupeWindowMs?: number;
}

export interface BrowserConsoleSink {
  debug(...data: unknown[]): void;
  info(...data: unknown[]): void;
  log(...data: unknown[]): void;
  warn(...data: unknown[]): void;
  error(...data: unknown[]): void;
  groupCollapsed(...data: unknown[]): void;
  groupEnd?(): void;
}

interface EventPresentation {
  emoji: string;
}

interface LevelPalette {
  foreground: string;
  background: string;
  detailForeground: string;
  detailBackground: string;
}

const eventPresentation: Record<BrowserConsoleEvent, EventPresentation> = {
  "app.start": { emoji: "🚀" },
  "transport.ready": { emoji: "🔌" },
  "transport.error": { emoji: "⚠️" },
  "device.state": { emoji: "💡" },
  "device.command": { emoji: "⚡" },
  "websocket.connected": { emoji: "📡" },
  "websocket.disconnected": { emoji: "📡" },
  "message.received": { emoji: "💬" },
  "audio.cue": { emoji: "🔊" },
  "audio.error": { emoji: "🔇" },
  "device.event": { emoji: "📟" },
  "diagnostics.loaded": { emoji: "🩺" },
  "diagnostics.error": { emoji: "⚠️" },
  "settings.saved": { emoji: "✅" },
  "settings.error": { emoji: "⚠️" }
};

// These literals mirror internal/design/tokens.go. DevTools CSS does not
// consistently resolve page custom properties, so token values are embedded
// explicitly while retaining the app's shared cyan/warm/success/danger theme.
const levelPalette: Record<BrowserConsoleLevel, LevelPalette> = {
  debug: {
    foreground: "#F3F8FB",
    background: "#17354A",
    detailForeground: "#A9BAC8",
    detailBackground: "#0B1724"
  },
  info: {
    foreground: "#07111C",
    background: "#22BEF2",
    detailForeground: "#F3F8FB",
    detailBackground: "#0B1724"
  },
  success: {
    foreground: "#07111C",
    background: "#4BDA97",
    detailForeground: "#F3F8FB",
    detailBackground: "#102334"
  },
  warn: {
    foreground: "#07111C",
    background: "#FFD35B",
    detailForeground: "#F3F8FB",
    detailBackground: "#102334"
  },
  error: {
    foreground: "#07111C",
    background: "#FF677D",
    detailForeground: "#F3F8FB",
    detailBackground: "#102334"
  }
};

// Keep this vocabulary intentionally small so the Browser and TUI surfaces
// can show the same useful fields. Base timestamp/level/event/detail fields
// are generated below and cannot be overridden by callers.
const metadataAllowlist = keys(
  "source",
  "action",
  "state",
  "attempt",
  "elapsed_ms",
  "count",
  "endpoint",
  "bytes",
  "error_code"
);

const sensitiveKey =
  /(?:^|[_-])(?:authorization|cookie|credential|csrf|password|passwd|passphrase|secret|session|token|api[_-]?key)(?:$|[_-])/i;
const jwtPattern = /\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b/g;
const bearerPattern = /\bBearer\s+[A-Za-z0-9._~+/-]+=*/gi;
const credentialURLPattern = /(\b[a-z][a-z0-9+.-]*:\/\/)[^/\s:@]+:[^/\s@]+@/gi;
const sensitiveParameterPattern =
  /([?&](?:authorization|csrf|password|secret|session|token|api[_-]?key)=)[^&#\s]*/gi;
const unsafeConsoleCharacterPattern =
  /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f\u202a-\u202e\u2066-\u2069]/g;
const maximumStringLength = 500;
const maximumDepth = 4;
const maximumArrayItems = 20;
const maximumObjectKeys = 30;

function keys(...values: string[]): ReadonlySet<string> {
  return new Set(values);
}

function redactText(value: string): string {
  const clean = value
    .replace(/[\r\n\t]+/g, " ")
    .replace(unsafeConsoleCharacterPattern, "")
    .replace(credentialURLPattern, "$1[REDACTED]@")
    .replace(bearerPattern, "Bearer [REDACTED]")
    .replace(jwtPattern, "[REDACTED JWT]")
    .replace(sensitiveParameterPattern, "$1[REDACTED]");
  return clean.length <= maximumStringLength
    ? clean
    : `${clean.slice(0, maximumStringLength)}…`;
}

function sanitizeValue(
  value: unknown,
  seen: WeakSet<object>,
  depth: number
): unknown {
  if (
    value === null ||
    typeof value === "boolean" ||
    typeof value === "number"
  ) {
    return value;
  }
  if (typeof value === "string") return redactText(value);
  if (typeof value === "bigint") return `${value}n`;
  if (typeof value === "undefined") return undefined;
  if (typeof value === "symbol") return value.description ?? "symbol";
  if (typeof value === "function") return `[Function ${value.name || "anonymous"}]`;
  if (value instanceof Error) return sanitizeError(value);
  if (depth >= maximumDepth) return "[Maximum depth]";
  if (seen.has(value)) return "[Circular]";
  seen.add(value);
  try {
    if (Array.isArray(value)) {
      const result = value
        .slice(0, maximumArrayItems)
        .map((item) => sanitizeValue(item, seen, depth + 1));
      if (value.length > maximumArrayItems) {
        result.push(`[${value.length - maximumArrayItems} more items]`);
      }
      return result;
    }
    if (ArrayBuffer.isView(value)) {
      return `[${value.constructor.name} ${value.byteLength} bytes]`;
    }
    if (value instanceof ArrayBuffer) return `[ArrayBuffer ${value.byteLength} bytes]`;
    if (value instanceof Date) return value.toISOString();
    if (typeof Element !== "undefined" && value instanceof Element) {
      const id = value.id ? `#${value.id}` : "";
      return `<${value.tagName.toLowerCase()}${id}>`;
    }

    const result: BrowserConsoleMetadata = {};
    const entries = Object.entries(value as Record<string, unknown>).slice(
      0,
      maximumObjectKeys
    );
    for (const [key, item] of entries) {
      result[key] = sensitiveKey.test(key)
        ? "[REDACTED]"
        : sanitizeValue(item, seen, depth + 1);
    }
    if (Object.keys(value).length > maximumObjectKeys) {
      result.omitted = Object.keys(value).length - maximumObjectKeys;
    }
    return result;
  } finally {
    seen.delete(value);
  }
}

function sanitizeMetadata(
  metadata?: BrowserConsoleMetadata
): BrowserConsoleMetadata | undefined {
  if (!metadata) return undefined;
  const result: BrowserConsoleMetadata = {};
  for (const [key, value] of Object.entries(metadata)) {
    if (!metadataAllowlist.has(key)) continue;
    result[key] = sensitiveKey.test(key)
      ? "[REDACTED]"
      : sanitizeValue(value, new WeakSet(), 0);
  }
  return Object.keys(result).length > 0 ? result : undefined;
}

function sanitizeError(error: Error): BrowserConsoleMetadata {
  const extended = error as Error & {
    code?: unknown;
    status?: unknown;
  };
  const result: BrowserConsoleMetadata = {};
  if (
    typeof extended.code === "string" ||
    typeof extended.code === "number"
  ) {
    result.error_code = sanitizeValue(extended.code, new WeakSet(), 0);
  } else if (typeof extended.status === "number") {
    result.error_code = `http_${extended.status}`;
  } else {
    result.error_code = redactText(error.name || "Error");
  }
  return result;
}

function methodFor(
  sink: BrowserConsoleSink,
  level: BrowserConsoleLevel
): (...data: unknown[]) => void {
  switch (level) {
    case "debug":
      return sink.debug.bind(sink);
    case "info":
      return sink.info.bind(sink);
    case "success":
      // Browsers have no console.success. Info retains the success entry when
      // users filter out verbose/debug traffic, matching the TUI's visibility.
      return sink.info.bind(sink);
    case "warn":
      return sink.warn.bind(sink);
    case "error":
      return sink.error.bind(sink);
  }
}

function badgeStyle(palette: LevelPalette): string {
  return [
    `color:${palette.foreground}`,
    `background:${palette.background}`,
    "font:600 11px/1.8 ui-monospace,SFMono-Regular,Consolas,monospace",
    "letter-spacing:.045em",
    "padding:2px 8px",
    "border-radius:999px 0 0 999px"
  ].join(";");
}

function detailStyle(palette: LevelPalette): string {
  return [
    `color:${palette.detailForeground}`,
    `background:${palette.detailBackground}`,
    "font:500 11px/1.8 system-ui,-apple-system,\"Segoe UI\",sans-serif",
    "padding:2px 9px",
    "border-radius:0 999px 999px 0"
  ].join(";");
}

function brandStyle(): string {
  return [
    "color:#07111C",
    "background:linear-gradient(135deg,#A9ECFF,#24C8FF 52%,#FFD465)",
    "font:800 13px/2 system-ui,-apple-system,\"Segoe UI\",sans-serif",
    "letter-spacing:.08em",
    "padding:3px 11px",
    "border-radius:999px 0 0 999px"
  ].join(";");
}

function brandDetailStyle(): string {
  return [
    "color:#F3F8FB",
    "background:#0B1724",
    "font:600 12px/2 system-ui,-apple-system,\"Segoe UI\",sans-serif",
    "padding:3px 11px",
    "border-radius:0 999px 999px 0"
  ].join(";");
}

function resetStyle(): string {
  return "color:inherit;background:transparent;font:inherit;padding:0;border-radius:0";
}

export class StyledBrowserConsole {
  private readonly recent = new Map<string, number>();

  constructor(
    private readonly sink: BrowserConsoleSink = console,
    private readonly now: () => number = () => Date.now()
  ) {}

  start(metadata?: BrowserConsoleMetadata): void {
    this.sink.groupCollapsed(
      "%c 💡 PCController %c Browser · GUI · TUI activity contract%c",
      brandStyle(),
      brandDetailStyle(),
      resetStyle()
    );
    this.event(
      "info",
      "app.start",
      "Web controller initialized",
      metadata,
      { dedupeKey: "app.start", dedupeWindowMs: Number.POSITIVE_INFINITY }
    );
    this.sink.debug(
      "%c 🔐 PRIVACY %c Structured context is allowlisted and credentials are redacted%c",
      badgeStyle(levelPalette.debug),
      detailStyle(levelPalette.debug),
      resetStyle()
    );
    this.sink.log(
      "%c ✨ TIP %c Filter by event name, level, or source; heartbeats and unchanged polls stay quiet%c",
      badgeStyle(levelPalette.success),
      detailStyle(levelPalette.success),
      resetStyle()
    );
    this.sink.groupEnd?.();
  }

  event(
    level: BrowserConsoleLevel,
    event: BrowserConsoleEvent,
    message: string,
    metadata?: BrowserConsoleMetadata,
    options: BrowserConsoleOptions = {}
  ): void {
    if (this.isDuplicate(options)) return;
    const presentation = eventPresentation[event];
    const palette = levelPalette[level];
    const call = methodFor(this.sink, level);
    const detail = redactText(message);
    const format =
      `%c ${presentation.emoji} ${level.toUpperCase()} · ${event} ` +
      `%c ${detail}%c`;
    const safe = sanitizeMetadata(metadata);
    const structured = {
      timestamp: new Date(this.now()).toISOString(),
      level,
      event,
      detail,
      ...safe
    };
    call(
      format,
      badgeStyle(palette),
      detailStyle(palette),
      resetStyle(),
      structured
    );
  }

  private isDuplicate(options: BrowserConsoleOptions): boolean {
    if (!options.dedupeKey) return false;
    const windowMs = options.dedupeWindowMs ?? 1_000;
    const now = this.now();
    const previous = this.recent.get(options.dedupeKey);
    if (previous !== undefined && now - previous < windowMs) return true;
    this.recent.set(options.dedupeKey, now);
    return false;
  }
}

export function browserErrorMetadata(error: unknown): BrowserConsoleMetadata {
  if (error instanceof Error) return sanitizeError(error);
  return {
    error_code: "unknown_error"
  };
}

export const browserConsole = new StyledBrowserConsole();
