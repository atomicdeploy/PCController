import type {
  DiagnosticDeviceInfo,
  DiagnosticDocument,
  DiagnosticField,
  DiagnosticValueKind,
  LampState
} from "./contracts";

export const DIRECT_DIAGNOSTIC_ENDPOINTS = [
  "/info",
  "/update/info",
  "/wifi/signal",
  "/wifi/status",
  "/wifi/country",
  "/description.xml"
] as const;

export type DirectDiagnosticEndpoint =
  (typeof DIRECT_DIAGNOSTIC_ENDPOINTS)[number];

// These firmware routes are intentionally not part of the public diagnostics
// allowlist: both return the configured Wi-Fi password in plaintext. Direct
// mode fetches them only as private projection sources, immediately parses and
// redacts them, and never exposes their documents through the raw-response UI.
const DIRECT_PROJECTION_DIAGNOSTIC_ENDPOINTS = [
  "/config/ap",
  "/config/st"
] as const;

export type DirectProjectionDiagnosticEndpoint =
  (typeof DIRECT_PROJECTION_DIAGNOSTIC_ENDPOINTS)[number];

type DirectParsedDiagnosticEndpoint =
  | DirectDiagnosticEndpoint
  | DirectProjectionDiagnosticEndpoint;

export interface DirectLampURLs {
  baseURL: string;
  websocketURL: string;
}

export type DirectLampEvent =
  | {
      type: "snapshot";
      state: Exclude<LampState, "unknown">;
      raw: string;
    }
  | {
      type: "message";
      title: "Rayan Lamp";
      message: string;
      icon: number;
      raw: string;
    }
  | {
      type: "beep";
      count: number;
      raw: string;
    };

export type DirectDiagnosticDocuments = Partial<
  Record<DirectParsedDiagnosticEndpoint, DiagnosticDocument>
>;

const stateAliases: Readonly<Record<string, LampState>> = {
  ON: "on",
  "1": "on",
  TRUE: "on",
  HIGH: "on",
  OFF: "off",
  "0": "off",
  FALSE: "off",
  LOW: "off"
};

const stateObjectKeys = new Set(["state", "status", "lamp", "value"]);
const auditedEndpointSet = new Set<string>(DIRECT_DIAGNOSTIC_ENDPOINTS);
const projectionEndpointSet = new Set<string>(
  DIRECT_PROJECTION_DIAGNOSTIC_ENDPOINTS
);
const usefulDescriptionTags = new Set([
  "friendly_name",
  "model_name",
  "model_number",
  "manufacturer",
  "manufacturer_url",
  "serial_number",
  "udn",
  "device_type",
  "presentation_url"
]);
const sensitiveMarkers = [
  "password",
  "passwd",
  "passphrase",
  "secret",
  "token",
  "authorization",
  "credential",
  "cookie",
  "session",
  "wepkey"
] as const;

const maxRemoteEventBytes = 16 << 10;
const maxRemoteMessageBytes = 4 << 10;
const maxDiagnosticBytes = 1 << 20;
const maxDiagnosticFields = 256;
const maxDiagnosticDepth = 12;
const redactedValue = "[redacted]";

export function normalizeDirectLampURL(raw: string): string {
  let value = raw.trim();
  if (!value) {
    throw new TypeError("Lamp URL is required.");
  }
  if (/[\u0000-\u001f\u007f\\]/u.test(value)) {
    throw new TypeError("Lamp URL contains invalid characters.");
  }
  if (!value.includes("://")) {
    value = `http://${value}`;
  }
  // URL.search/hash cannot distinguish an absent delimiter from a trailing
  // empty one. Reject the delimiters themselves to keep the device authority
  // and root path exact.
  if (value.includes("?") || value.includes("#")) {
    throw new TypeError("Lamp URL must not contain a query or fragment.");
  }

  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new TypeError("Lamp URL is invalid.");
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new TypeError("Lamp URL must use HTTP or HTTPS.");
  }
  if (!parsed.hostname) {
    throw new TypeError("Lamp URL must include a hostname.");
  }
  if (parsed.username || parsed.password) {
    throw new TypeError("Lamp URL must not contain credentials.");
  }
  if (parsed.pathname !== "/") {
    throw new TypeError("Lamp URL must not contain a path prefix.");
  }
  return parsed.origin;
}

export function deriveDirectWebSocketURL(rawBaseURL: string): string {
  const parsed = new URL(normalizeDirectLampURL(rawBaseURL));
  parsed.protocol = parsed.protocol === "https:" ? "wss:" : "ws:";
  parsed.pathname = "/ws";
  return parsed.href;
}

export function normalizeDirectLampURLs(rawBaseURL: string): DirectLampURLs {
  const baseURL = normalizeDirectLampURL(rawBaseURL);
  return {
    baseURL,
    websocketURL: deriveDirectWebSocketURL(baseURL)
  };
}

export function directLampCommandURL(
  rawBaseURL: string,
  command: "get" | "on" | "off"
): string {
  return `${normalizeDirectLampURL(rawBaseURL)}/lamp/${command}`;
}

export function isDirectDiagnosticEndpoint(
  endpoint: string
): endpoint is DirectDiagnosticEndpoint {
  return auditedEndpointSet.has(endpoint);
}

export function directDiagnosticURL(
  rawBaseURL: string,
  endpoint: DirectDiagnosticEndpoint
): string {
  requireAuditedEndpoint(endpoint);
  return `${normalizeDirectLampURL(rawBaseURL)}${endpoint}`;
}

export function directProjectionDiagnosticURLs(
  rawBaseURL: string
): ReadonlyArray<{
  endpoint: DirectProjectionDiagnosticEndpoint;
  url: string;
}> {
  const baseURL = normalizeDirectLampURL(rawBaseURL);
  return DIRECT_PROJECTION_DIAGNOSTIC_ENDPOINTS.map((endpoint) => ({
    endpoint,
    url: `${baseURL}${endpoint}`
  }));
}

export function parseDirectLampState(payload: string): LampState {
  const text = stripLeadingBOM(payload).trim();
  const direct = parseStateScalar(text);
  if (direct !== "unknown") return direct;

  let decoded: unknown;
  try {
    decoded = JSON.parse(text);
  } catch {
    return "unknown";
  }
  if (typeof decoded === "string") {
    return parseStateScalar(decoded);
  }
  if (!isPlainRecord(decoded)) return "unknown";

  for (const [key, value] of Object.entries(decoded)) {
    if (!stateObjectKeys.has(key.toLowerCase())) continue;
    if (typeof value === "string") {
      const state = parseStateScalar(value);
      if (state !== "unknown") return state;
    } else if (typeof value === "boolean") {
      return value ? "on" : "off";
    }
  }
  return "unknown";
}

export function parseDirectWebSocketFrame(
  frame: unknown
): DirectLampEvent | null {
  if (
    typeof frame !== "string" ||
    frame.length === 0 ||
    utf8ByteLength(frame) > maxRemoteEventBytes ||
    frame.includes("\0")
  ) {
    return null;
  }
  const text = frame.trim();
  if (!text) return null;

  if (/^(?:ON|OFF)$/iu.test(text)) {
    return {
      type: "snapshot",
      state: text.toUpperCase() === "ON" ? "on" : "off",
      raw: text
    };
  }

  if (hasPrefixFold(text, "message:")) {
    const message = text.slice("message:".length).trim();
    if (!validRemoteMessage(message)) return null;
    return {
      type: "message",
      title: "Rayan Lamp",
      message,
      icon: 0,
      raw: text
    };
  }

  if (hasPrefixFold(text, "msg:")) {
    const fields = text.slice("msg:".length);
    const separator = fields.indexOf(",");
    if (separator < 0) return null;
    const iconText = fields.slice(0, separator).trim();
    if (!/^[+-]?\d+$/u.test(iconText)) return null;
    const icon = Number(iconText);
    if (!Number.isSafeInteger(icon) || icon < 0 || icon > 255) return null;

    let message: string;
    try {
      message = decodeURIComponent(
        fields.slice(separator + 1).replaceAll("+", " ")
      ).trim();
    } catch {
      return null;
    }
    if (!validRemoteMessage(message)) return null;
    return {
      type: "message",
      title: "Rayan Lamp",
      message,
      icon,
      raw: text
    };
  }

  if (hasPrefixFold(text, "beep:")) {
    const countText = text.slice("beep:".length).trim();
    if (!/^[+-]?\d+$/u.test(countText)) return null;
    const count = Number(countText);
    if (!Number.isSafeInteger(count) || count < 1 || count > 10) return null;
    return { type: "beep", count, raw: text };
  }

  // The firmware also serves unrelated ASA traffic. Failing closed here keeps
  // those frames out of the lamp state and notification surfaces.
  return null;
}

export function parseDurationMilliseconds(value: string): number | null {
  const input = value.trim();
  if (!input) return null;
  if (input === "0") return 0;

  const token = /(\d+(?:\.\d+)?)(ms|s|m|h)/giu;
  let cursor = 0;
  let total = 0;
  let matched = false;
  for (const match of input.matchAll(token)) {
    if (match.index !== cursor) return null;
    cursor += match[0].length;
    matched = true;
    const amount = Number(match[1]);
    const multiplier =
      match[2].toLowerCase() === "ms"
        ? 1
        : match[2].toLowerCase() === "s"
          ? 1_000
          : match[2].toLowerCase() === "m"
            ? 60_000
            : 3_600_000;
    total += amount * multiplier;
    if (!Number.isFinite(total) || total > Number.MAX_SAFE_INTEGER) return null;
  }
  return matched && cursor === input.length ? total : null;
}

export function reconnectDelayMilliseconds(
  attempt: number,
  initialMilliseconds: number,
  maximumMilliseconds: number,
  jitter = 0.2,
  random: () => number = Math.random
): number {
  if (
    !Number.isFinite(attempt) ||
    !Number.isFinite(initialMilliseconds) ||
    !Number.isFinite(maximumMilliseconds) ||
    !Number.isFinite(jitter) ||
    initialMilliseconds <= 0 ||
    maximumMilliseconds < initialMilliseconds ||
    jitter < 0 ||
    jitter > 1
  ) {
    throw new RangeError("Invalid reconnect policy.");
  }
  const ordinal = Math.max(1, Math.floor(attempt));
  const base = Math.min(
    maximumMilliseconds,
    initialMilliseconds * 2 ** (ordinal - 1)
  );
  const sample = random();
  if (!Number.isFinite(sample) || sample < 0 || sample > 1) {
    throw new RangeError("Reconnect jitter source must return 0 through 1.");
  }
  const factor = 1 - jitter + 2 * jitter * sample;
  return Math.round(Math.min(maximumMilliseconds, base * factor));
}

export function parseDirectDiagnostic(
  endpoint: DirectDiagnosticEndpoint,
  payload: string,
  fetchedAt: Date = new Date()
): DiagnosticDocument {
  requireAuditedEndpoint(endpoint);
  return parseDiagnosticDocumentCore(endpoint, payload, fetchedAt);
}

export function parseDirectProjectionDiagnostic(
  endpoint: DirectProjectionDiagnosticEndpoint,
  payload: string,
  fetchedAt: Date = new Date()
): DiagnosticDocument {
  requireProjectionEndpoint(endpoint);
  return parseDiagnosticDocumentCore(endpoint, payload, fetchedAt);
}

function parseDiagnosticDocumentCore(
  endpoint: DirectParsedDiagnosticEndpoint,
  payload: string,
  fetchedAt: Date
): DiagnosticDocument {
  const bytes = utf8ByteLength(payload);
  if (bytes > maxDiagnosticBytes) {
    throw new RangeError("Diagnostic response exceeds 1 MiB.");
  }

  const warnings: string[] = [];
  if (payload.includes("\0")) {
    warnings.push("response contained NUL bytes");
  }
  const raw = stripLeadingBOM(payload).replace(/\0+$/gu, "");
  const input = raw.trim();
  let format: DiagnosticDocument["format"] = "text";
  let fields: DiagnosticField[] = [];
  let safeRaw = redactText(raw);

  if (!input) {
    warnings.push("endpoint returned an empty response");
  } else if (input.startsWith("{") || input.startsWith("[")) {
    try {
      const decoded = JSON.parse(input) as unknown;
      format = "json";
      fields = flattenDiagnosticJSON(endpoint, decoded, warnings);
      safeRaw = JSON.stringify(redactJSONValue("", decoded), null, 2);
    } catch (error) {
      warnings.push(
        `JSON-looking response was malformed: ${errorMessage(error)}`
      );
      fields = parseTextDiagnosticFields(endpoint, input, warnings);
    }
  } else if (input.startsWith("<") || endpoint === "/description.xml") {
    const parsed = parseDescriptionXML(endpoint, input);
    if (parsed.ok) {
      format = "xml";
      fields = parsed.fields;
      safeRaw = redactXML(raw);
    } else {
      warnings.push(`XML-looking response was malformed: ${parsed.error}`);
      fields = parseTextDiagnosticFields(endpoint, input, warnings);
    }
  } else {
    fields = parseTextDiagnosticFields(endpoint, input, warnings);
  }

  fields = makeFieldKeysUnique(fields);
  return {
    endpoint,
    format,
    fetched_at: validDate(fetchedAt).toISOString(),
    bytes,
    raw: safeRaw.replaceAll("\0", "␀"),
    fields,
    ...(warnings.length > 0 ? { warnings } : {})
  };
}

export function projectDirectDiagnosticSummary(
  documents: DirectDiagnosticDocuments
): DiagnosticDeviceInfo {
  const info: DiagnosticDeviceInfo = {};
  const device = documents["/info"];
  if (device) projectInfo(info, device);

  const signal = documents["/wifi/signal"];
  if (signal) projectSignal(info, signal);

  const wifi = documents["/wifi/status"];
  if (wifi) projectWiFi(info, wifi);

  const station = documents["/config/st"];
  if (station) projectStationConfig(info, station);

  const accessPoint = documents["/config/ap"];
  if (accessPoint) projectAccessPointConfig(info, accessPoint);

  const country = documents["/wifi/country"];
  if (country) {
    info.country = fieldText(
      country,
      "country",
      "country_code",
      "code",
      "value",
      "line_1"
    ).toUpperCase();
  }

  const update = documents["/update/info"];
  if (update) projectUpdate(info, update);

  const description = documents["/description.xml"];
  if (description) projectDescription(info, description);

  if (!info.device_name) {
    info.device_name = info.description?.friendly_name;
  }
  return info;
}

// Stable integration-facing names. The longer aliases above remain descriptive
// for call sites that want to make the direct transport explicit.
export const normalizeLampBaseURL = normalizeDirectLampURL;
export const directWebSocketURL = deriveDirectWebSocketURL;
export const parseLampStatePayload = parseDirectLampState;
export const parseDirectEvent = parseDirectWebSocketFrame;
export const parseDiagnosticDocument = parseDirectDiagnostic;
export const diagnosticDeviceFromDocuments =
  projectDirectDiagnosticSummary;

function parseStateScalar(value: string): LampState {
  return stateAliases[value.trim().toUpperCase()] ?? "unknown";
}

function hasPrefixFold(value: string, prefix: string): boolean {
  return (
    value.length >= prefix.length &&
    value.slice(0, prefix.length).toLowerCase() === prefix.toLowerCase()
  );
}

function validRemoteMessage(value: string): boolean {
  return (
    value.length > 0 &&
    utf8ByteLength(value) <= maxRemoteMessageBytes &&
    !value.includes("\0") &&
    !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/u.test(value)
  );
}

function requireAuditedEndpoint(
  endpoint: string
): asserts endpoint is DirectDiagnosticEndpoint {
  if (!isDirectDiagnosticEndpoint(endpoint)) {
    throw new TypeError("Diagnostic endpoint is not in the audited allowlist.");
  }
}

function requireProjectionEndpoint(
  endpoint: string
): asserts endpoint is DirectProjectionDiagnosticEndpoint {
  if (!projectionEndpointSet.has(endpoint)) {
    throw new TypeError(
      "Diagnostic endpoint is not a private projection source."
    );
  }
}

function flattenDiagnosticJSON(
  endpoint: DirectParsedDiagnosticEndpoint,
  value: unknown,
  warnings: string[]
): DiagnosticField[] {
  const fields: DiagnosticField[] = [];
  const visit = (current: unknown, prefix: string, depth: number): void => {
    if (fields.length >= maxDiagnosticFields) return;
    if (depth > maxDiagnosticDepth) {
      if (!warnings.includes("diagnostic nesting was truncated")) {
        warnings.push("diagnostic nesting was truncated");
      }
      return;
    }
    if (Array.isArray(current)) {
      if (current.length === 0) {
        fields.push(classifyField(endpoint, prefix || "value", "[]"));
        return;
      }
      for (let index = 0; index < current.length; index += 1) {
        visit(current[index], `${prefix || "value"}[${index}]`, depth + 1);
      }
      return;
    }
    if (isPlainRecord(current)) {
      for (const key of Object.keys(current).sort()) {
        visit(
          current[key],
          prefix ? `${prefix}.${key}` : key,
          depth + 1
        );
      }
      return;
    }

    const sourceKey = prefix || "value";
    const field = classifyField(endpoint, sourceKey, jsonScalar(current));
    if (typeof current === "boolean" && field.kind === "string") {
      field.kind = "boolean";
      field.boolean = current;
    } else if (
      typeof current === "number" &&
      Number.isFinite(current) &&
      field.kind === "string"
    ) {
      if (Number.isSafeInteger(current)) {
        field.kind = "integer";
        field.integer = current;
      } else {
        field.kind = "number";
        field.number = current;
      }
    }
    fields.push(field);
  };
  visit(value, "", 0);
  if (fields.length >= maxDiagnosticFields) {
    warnings.push("diagnostic fields were truncated");
  }
  return fields.slice(0, maxDiagnosticFields);
}

function parseTextDiagnosticFields(
  endpoint: DirectParsedDiagnosticEndpoint,
  input: string,
  warnings: string[]
): DiagnosticField[] {
  const fields: DiagnosticField[] = [];
  let unkeyed = 0;
  for (const source of input.replace(/\r\n?/gu, "\n").split("\n")) {
    const line = source.trim();
    if (!line) continue;
    if (line.toLowerCase() === "fail") {
      warnings.push("firmware reported failure");
    }
    const pair = splitKeyValueLine(line);
    if (pair) {
      fields.push(classifyField(endpoint, pair.key, pair.value));
    } else {
      unkeyed += 1;
      let key = `line_${unkeyed}`;
      if (endpoint === "/wifi/signal") {
        if (unkeyed === 1) key = "rssi";
        if (unkeyed === 2) key = "quality";
      } else if (endpoint === "/wifi/country" && unkeyed === 1) {
        key = "country";
      }
      fields.push(classifyField(endpoint, key, line));
    }
    if (fields.length >= maxDiagnosticFields) {
      warnings.push("diagnostic fields were truncated");
      break;
    }
  }
  return fields;
}

function parseDescriptionXML(
  endpoint: DirectParsedDiagnosticEndpoint,
  input: string
):
  | { ok: true; fields: DiagnosticField[] }
  | { ok: false; error: string } {
  if (/<!DOCTYPE|<!ENTITY/iu.test(input)) {
    return { ok: false, error: "DOCTYPE and entity declarations are not allowed" };
  }
  const parser = new DOMParser();
  const document = parser.parseFromString(input, "application/xml");
  if (document.querySelector("parsererror")) {
    return { ok: false, error: "invalid XML" };
  }
  const fields: DiagnosticField[] = [];
  for (const element of Array.from(document.querySelectorAll("*"))) {
    const key = canonicalKey(element.localName);
    if (
      !usefulDescriptionTags.has(key) ||
      element.children.length > 0 ||
      fields.length >= maxDiagnosticFields
    ) {
      continue;
    }
    const value = element.textContent?.trim() ?? "";
    if (value) fields.push(classifyField(endpoint, element.localName, value));
  }
  if (fields.length === 0) {
    return { ok: false, error: "no recognized device description fields" };
  }
  return { ok: true, fields };
}

function classifyField(
  endpoint: DirectParsedDiagnosticEndpoint,
  sourceKey: string,
  rawValue: string
): DiagnosticField {
  const key = canonicalKey(sourceKey) || "value";
  const value = rawValue.trim();
  const field: DiagnosticField = {
    key,
    label: humanizeKey(sourceKey || key),
    value,
    kind: "string"
  };
  if (isSensitiveKey(key)) {
    field.value = redactedValue;
    field.kind = "redacted";
    field.redacted = true;
    return field;
  }

  const lower = value.toLowerCase();
  if (isSignalKey(key)) {
    const number = strictInteger(lower.replace(/dbm$/iu, "").trim());
    if (number !== null) {
      field.kind = "signal";
      field.unit = "dBm";
      field.integer = number;
    }
  } else if (isPercentKey(key)) {
    const number = strictNumber(value.replace(/%$/u, "").trim());
    if (number !== null) {
      field.kind = "percent";
      field.unit = "%";
      field.number = number;
    }
  } else if (isByteKey(endpoint, key)) {
    const number = parseByteCount(value);
    if (number !== null) {
      field.kind = "bytes";
      field.unit = "bytes";
      field.integer = number;
    }
  } else if (key === "uptime" || key === "uptime_seconds") {
    const seconds =
      key === "uptime_seconds" ? strictInteger(value) : parseUptime(value);
    if (seconds !== null) {
      field.kind = "duration";
      field.unit = "seconds";
      field.duration_seconds = seconds;
    }
  } else if (isBooleanKey(key)) {
    const boolean = parseBoolean(value);
    if (boolean !== null) {
      field.kind = "boolean";
      field.boolean = boolean;
    }
  } else if (isTimeKey(key)) {
    const time = parseWireTime(value);
    if (time) {
      field.kind = "time";
      field.time = time;
    }
  } else if (isMACKey(key) && /^(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}$/iu.test(value)) {
    field.kind = "mac";
  } else if (isIPKey(key) && isIPAddress(value)) {
    field.kind = "ip";
  } else if (isHashKey(key)) {
    field.kind = "hash";
  } else if (key === "state") {
    field.kind = "state";
  } else if (isIntegerKey(key)) {
    const integer = strictInteger(value);
    if (integer !== null) {
      field.kind = "integer";
      field.integer = integer;
    }
  }
  return field;
}

function makeFieldKeysUnique(fields: DiagnosticField[]): DiagnosticField[] {
  const seen = new Map<string, number>();
  return fields.map((field, index) => {
    const original = field.key || `field_${index + 1}`;
    const count = (seen.get(original) ?? 0) + 1;
    seen.set(original, count);
    return count === 1 ? field : { ...field, key: `${original}_${count}` };
  });
}

function splitKeyValueLine(
  line: string
): { key: string; value: string } | null {
  const colon = line.indexOf(":");
  const equals = line.indexOf("=");
  const candidates = [colon, equals].filter((value) => value >= 0);
  if (candidates.length === 0) return null;
  const separator = Math.min(...candidates);
  const key = line.slice(0, separator).trim();
  if (
    !key ||
    utf8ByteLength(key) > 80 ||
    !/^\p{L}[\p{L}\p{N}\s._/()\-]*$/u.test(key)
  ) {
    return null;
  }
  return { key, value: line.slice(separator + 1).trim() };
}

function canonicalKey(input: string): string {
  return input
    .trim()
    .replace(/([\p{Ll}\p{N}])(\p{Lu})/gu, "$1_$2")
    .replace(/[^\p{L}\p{N}]+/gu, "_")
    .replace(/^_+|_+$/gu, "")
    .toLowerCase();
}

function humanizeKey(input: string): string {
  const acronyms = new Set([
    "AP",
    "ESP",
    "ID",
    "IP",
    "MAC",
    "MD5",
    "RSSI",
    "SSID",
    "UDN",
    "URL",
    "WIFI"
  ]);
  return input
    .trim()
    .replaceAll(".", " · ")
    .replace(/[_-]+/gu, " ")
    .split(/\s+/u)
    .filter(Boolean)
    .map((word) => {
      const upper = word.toUpperCase();
      return acronyms.has(upper)
        ? upper
        : `${word.slice(0, 1).toUpperCase()}${word.slice(1)}`;
    })
    .join(" ");
}

function isSensitiveKey(key: string): boolean {
  const compact = key.replaceAll("_", "");
  return (
    sensitiveMarkers.some((marker) => compact.includes(marker)) ||
    ["pass", "pwd", "psk"].some(
      (short) => key === short || key.endsWith(`_${short}`)
    )
  );
}

function redactText(input: string): string {
  const jsonSecret =
    /("(?:[^"\\]|\\.)*(?:password|passwd|passphrase|secret|token|authorization|credential|cookie|session)(?:[^"\\]|\\.)*"\s*:\s*)("(?:[^"\\]|\\.)*"|[^,\s}\]]+)/giu;
  const jsonShortSecret =
    /("(?:pass|pwd|psk|wep[_-]?key|(?:[^"\\]|\\.)*[_-](?:pass|pwd|psk|wep[_-]?key))"\s*:\s*)("(?:[^"\\]|\\.)*"|[^,\s}\]]+)/giu;
  const lines = input
    .replace(/\r\n?/gu, "\n")
    .replace(jsonSecret, `$1"${redactedValue}"`)
    .replace(jsonShortSecret, `$1"${redactedValue}"`)
    .split("\n");
  return lines
    .map((line) => {
      const pair = splitKeyValueLine(line.trim());
      if (!pair || !isSensitiveKey(canonicalKey(pair.key))) return line;
      const separator = firstSeparatorIndex(line);
      return separator < 0
        ? line
        : `${line.slice(0, separator + 1)} ${redactedValue}`;
    })
    .join("\n");
}

function redactXML(input: string): string {
  const marker =
    "(?:password|passwd|passphrase|secret|token|authorization|credential|cookie|session)";
  return redactText(input).replace(
    new RegExp(
      `(<([\\w:.-]*${marker}[\\w:.-]*)\\b[^>]*>)[\\s\\S]*?(<\\/\\2\\s*>)`,
      "giu"
    ),
    `$1${redactedValue}$3`
  );
}

function redactJSONValue(key: string, value: unknown): unknown {
  if (isSensitiveKey(canonicalKey(key))) return redactedValue;
  if (Array.isArray(value)) {
    return value.map((child) => redactJSONValue(key, child));
  }
  if (isPlainRecord(value)) {
    return Object.fromEntries(
      Object.entries(value).map(([childKey, child]) => [
        childKey,
        redactJSONValue(childKey, child)
      ])
    );
  }
  return value;
}

function projectInfo(
  info: DiagnosticDeviceInfo,
  document: DiagnosticDocument
): void {
  info.device_name = fieldText(document, "device", "device_name", "name");
  info.hostname = fieldText(document, "hostname", "host_name");
  info.firmware = fieldText(
    document,
    "firmware",
    "firmware_version",
    "version"
  );
  info.wifi_version = fieldText(document, "wifi_version");
  info.esp_chip_id = fieldText(document, "esp_chip_id", "chip_id");
  info.flash_chip_id = fieldText(document, "flash_chip_id");

  const firmwareHash = fieldText(document, "firmware_hash", "hash");
  if (firmwareHash) {
    info.firmware_corrupted = firmwareHash.toUpperCase().includes("CORRUPTED");
    info.firmware_hash = firmwareHash.split("(", 1)[0].trim();
  }
  const protocol = fieldText(document, "protocol_rev", "protocol_revision");
  if (protocol) {
    info.protocol_match = !protocol.toLowerCase().includes("no match");
    info.protocol_revision = protocol.split("(", 1)[0].trim();
  }

  info.station_mac = fieldText(document, "st_mac", "station_mac");
  info.access_point_mac = fieldText(document, "ap_mac", "access_point_mac");
  info.client_ip = fieldText(document, "client_ip");

  const flash = findField(document, "flash_size");
  if (flash) {
    info.flash_size_bytes = numericFieldValue(flash) ?? undefined;
    const real = flash.value.match(/\(\s*real\s*:\s*([^)]+)\)/iu);
    if (real) info.flash_real_size_bytes = parseByteCount(real[1]) ?? undefined;
  }
  const usage = findField(
    document,
    "program_usage",
    "program_usage_percent"
  );
  if (usage) {
    info.program_usage_percent =
      usage.number ??
      usage.integer ??
      strictNumber(usage.value.replace(/%$/u, "").trim()) ??
      undefined;
  }
  const settings = fieldText(document, "settings_hash");
  if (settings) {
    const [hash, status] = settings.split(",", 2);
    info.settings_hash = hash.trim();
    if (status) {
      info.settings_status = status
        .trim()
        .replace(/^status\s*:/iu, "")
        .trim()
        .toLowerCase();
    }
  }
  const heap = findField(document, "heap_memory", "heap_free", "free_heap");
  if (heap) info.heap_free_bytes = numericFieldValue(heap) ?? undefined;
  info.build = fieldText(document, "build");
  const buildPrefix = info.build?.split(" by ", 1)[0];
  if (buildPrefix) info.build_time = parseWireTime(buildPrefix) ?? undefined;
  const uptime = findField(document, "uptime", "uptime_seconds");
  if (uptime) {
    info.uptime_seconds =
      uptime.duration_seconds ??
      uptime.integer ??
      parseUptime(uptime.value) ??
      undefined;
  }
  info.reported_state = fieldText(document, "state").toLowerCase();
}

function projectSignal(
  info: DiagnosticDeviceInfo,
  document: DiagnosticDocument
): void {
  const rssi = findField(document, "rssi", "signal", "signal_strength", "line_1");
  if (rssi) {
    info.rssi_dbm =
      rssi.integer ??
      strictInteger(rssi.value.replace(/dbm$/iu, "").trim()) ??
      undefined;
  }
  info.signal_quality = fieldText(
    document,
    "quality",
    "signal_quality",
    "line_2"
  );
}

function projectWiFi(
  info: DiagnosticDeviceInfo,
  document: DiagnosticDocument
): void {
  const connected = findField(document, "connected", "wifi_connected");
  if (connected) {
    info.wifi_connected =
      connected.boolean ?? parseBoolean(connected.value) ?? undefined;
  }
  setIfText(info, "ssid", fieldText(document, "ssid"));
  setIfText(info, "wifi_ip", fieldText(document, "ip", "wifi_ip"));

  const accessPointStatus = accessPointNetworkStatus(
    findField(document, "ap_status", "access_point_status")
  );
  if (accessPointStatus) {
    info.access_point_status = accessPointStatus;
  }
  const stationStatus = stationNetworkStatus(
    findField(document, "st_status", "station_status")
  );
  if (stationStatus) {
    info.station_status = stationStatus.label;
    if (info.wifi_connected === undefined && stationStatus.connected !== undefined) {
      info.wifi_connected = stationStatus.connected;
    }
  }
  if (info.rssi_dbm === undefined) {
    const rssi = findField(document, "rssi", "signal");
    if (rssi) info.rssi_dbm = rssi.integer;
  }
}

function projectStationConfig(
  info: DiagnosticDeviceInfo,
  document: DiagnosticDocument
): void {
  setIfText(info, "ssid", fieldText(document, "ssid"));
  setIfText(info, "wifi_ip", fieldText(document, "ip", "wifi_ip"));
  setIfText(info, "station_netmask", fieldText(document, "netmask"));
  setIfText(info, "station_gateway", fieldText(document, "gw", "gateway"));
  setIfText(info, "station_dns", fieldText(document, "dns", "dns_servers"));

  const dhcp = findField(document, "dhcp");
  if (dhcp) {
    info.station_dhcp =
      dhcp.boolean ?? parseBoolean(dhcp.value) ?? undefined;
  }
}

function projectAccessPointConfig(
  info: DiagnosticDeviceInfo,
  document: DiagnosticDocument
): void {
  setIfText(info, "access_point_ssid", fieldText(document, "ssid"));
  setIfText(info, "access_point_ip", fieldText(document, "ip"));
  setIfText(info, "access_point_netmask", fieldText(document, "netmask"));
}

function stationNetworkStatus(
  field: DiagnosticField | undefined
): { label: string; connected?: boolean } | null {
  if (!field) return null;
  const code = field.integer ?? strictInteger(field.value);
  if (code !== null) {
    switch (code) {
      case 0:
        return { label: "Unknown" };
      case 1:
        return { label: "Connected", connected: true };
      case 2:
        return { label: "Disconnected", connected: false };
      case 3:
        return { label: "Connecting", connected: false };
      case 4:
        return { label: "SSID not found", connected: false };
      case 5:
        return { label: "Wrong password", connected: false };
      case 6:
        return { label: "Connection failed", connected: false };
      case 7:
        return { label: "Idle", connected: false };
    }
  }

  const value = field.value.trim();
  const connected = parseBoolean(value);
  const label =
    connected === true
      ? "Connected"
      : connected === false
        ? "Disconnected"
        : value;
  return value
    ? {
        label,
        ...(connected === null ? {} : { connected })
      }
    : null;
}

function accessPointNetworkStatus(
  field: DiagnosticField | undefined
): string | null {
  if (!field) return null;
  const code = field.integer ?? strictInteger(field.value);
  if (code === 1) return "Enabled";
  if (code === 2) return "Disabled";
  if (code === 0) return "Unknown";

  const value = field.value.trim();
  switch (value.toLowerCase()) {
    case "ok":
    case "enabled":
    case "on":
      return "Enabled";
    case "disconnected":
    case "disabled":
    case "off":
      return "Disabled";
    case "unknown":
    case "undefined":
      return "Unknown";
    default:
      return value || null;
  }
}

function setIfText<
  Key extends
    | "ssid"
    | "wifi_ip"
    | "station_netmask"
    | "station_gateway"
    | "station_dns"
    | "access_point_ssid"
    | "access_point_ip"
    | "access_point_netmask"
>(
  info: DiagnosticDeviceInfo,
  key: Key,
  value: string
): void {
  if (value) info[key] = value;
}

function projectUpdate(
  info: DiagnosticDeviceInfo,
  document: DiagnosticDocument
): void {
  const hash = fieldText(document, "hash", "firmware_hash");
  const size = findField(document, "size", "firmware_size", "size_bytes");
  const free = findField(document, "free", "free_space", "free_bytes");
  const build = fieldText(document, "date", "build", "build_date");
  const date = findField(document, "date", "build_date");
  info.update = {
    ...(hash ? { hash } : {}),
    ...(size && numericFieldValue(size) !== null
      ? { size_bytes: numericFieldValue(size) ?? undefined }
      : {}),
    ...(free && numericFieldValue(free) !== null
      ? { free_bytes: numericFieldValue(free) ?? undefined }
      : {}),
    ...(build ? { build } : {}),
    ...(date && (date.time || parseWireTime(date.value))
      ? { date: date.time || parseWireTime(date.value) || undefined }
      : {})
  };
}

function projectDescription(
  info: DiagnosticDeviceInfo,
  document: DiagnosticDocument
): void {
  info.description = {
    friendly_name: fieldText(document, "friendly_name"),
    model_name: fieldText(document, "model_name"),
    model_number: fieldText(document, "model_number"),
    manufacturer: fieldText(document, "manufacturer"),
    manufacturer_url: fieldText(document, "manufacturer_url"),
    serial_number: fieldText(document, "serial_number"),
    udn: fieldText(document, "udn")
  };
}

function findField(
  document: DiagnosticDocument,
  ...keys: string[]
): DiagnosticField | undefined {
  for (const key of keys.map(canonicalKey)) {
    const field = document.fields.find((candidate) => candidate.key === key);
    if (field) return field;
  }
  return undefined;
}

function fieldText(document: DiagnosticDocument, ...keys: string[]): string {
  const field = findField(document, ...keys);
  return field && !field.redacted ? field.value.trim() : "";
}

function numericFieldValue(field: DiagnosticField): number | null {
  return field.integer ?? parseByteCount(field.value);
}

function isSignalKey(key: string): boolean {
  return (
    key === "rssi" ||
    key === "signal" ||
    key === "signal_strength" ||
    key.endsWith("_rssi")
  );
}

function isPercentKey(key: string): boolean {
  return (
    key.includes("percent") ||
    key === "program_usage" ||
    key.endsWith("_usage")
  );
}

function isByteKey(
  endpoint: DirectParsedDiagnosticEndpoint,
  key: string
): boolean {
  if (key.endsWith("_bytes")) return true;
  return (
    (endpoint === "/info" || endpoint === "/update/info") &&
    [
      "flash_size",
      "flash_real_size",
      "heap_memory",
      "free_heap",
      "heap_free",
      "size",
      "free",
      "firmware_size",
      "free_space"
    ].includes(key)
  );
}

function isBooleanKey(key: string): boolean {
  return (
    ["connected", "available", "valid", "corrupted", "protocol_match"].includes(
      key
    ) ||
    key.startsWith("is_") ||
    key.startsWith("has_")
  );
}

function isTimeKey(key: string): boolean {
  return (
    ["time", "date", "build_time", "fetched_at"].includes(key) ||
    key.endsWith("_time") ||
    key.endsWith("_at")
  );
}

function isMACKey(key: string): boolean {
  return key === "mac" || key.endsWith("_mac");
}

function isIPKey(key: string): boolean {
  return key === "ip" || key.endsWith("_ip");
}

function isHashKey(key: string): boolean {
  return key.includes("hash") || key.endsWith("_chip_id");
}

function isIntegerKey(key: string): boolean {
  return ["count", "clients", "attempt", "revision", "code"].some((marker) =>
    key.includes(marker)
  );
}

function parseByteCount(value: string): number | null {
  const match = value.match(
    /^\s*([+-]?(?:\d+(?:\.\d*)?|\.\d+))\s*(B|KB|MB|GB|KIB|MIB|GIB)?\b/iu
  );
  if (!match) return null;
  const number = Number(match[1]);
  if (!Number.isFinite(number) || number < 0) return null;
  const unit = (match[2] ?? "B").toUpperCase();
  const multiplier =
    unit === "KB" || unit === "KIB"
      ? 1024
      : unit === "MB" || unit === "MIB"
        ? 1024 ** 2
        : unit === "GB" || unit === "GIB"
          ? 1024 ** 3
          : 1;
  const result = Math.round(number * multiplier);
  return Number.isSafeInteger(result) ? result : null;
}

function parseUptime(value: string): number | null {
  const match = value.match(
    /^\s*(?:(\d+)\s+days?,\s*)?(\d{1,2}):(\d{2}):(\d{2})\s*$/iu
  );
  if (!match) return null;
  const days = Number(match[1] ?? 0);
  const hours = Number(match[2]);
  const minutes = Number(match[3]);
  const seconds = Number(match[4]);
  if (hours > 23 || minutes > 59 || seconds > 59) return null;
  return days * 86_400 + hours * 3_600 + minutes * 60 + seconds;
}

function parseBoolean(value: string): boolean | null {
  switch (value.trim().toLowerCase()) {
    case "1":
    case "true":
    case "yes":
    case "on":
    case "enabled":
    case "connected":
    case "ok":
    case "valid":
      return true;
    case "0":
    case "false":
    case "no":
    case "off":
    case "disabled":
    case "disconnected":
    case "invalid":
      return false;
    default:
      return null;
  }
}

function parseWireTime(value: string): string | null {
  const trimmed = value.trim();
  let parsed = Date.parse(trimmed);
  if (!Number.isNaN(parsed)) return new Date(parsed).toISOString();

  const firmware = trimmed.match(
    /^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+(\d{1,2})\s+(\d{4})\s+(\d{2}):(\d{2}):(\d{2})$/u
  );
  if (!firmware) return null;
  const month = [
    "Jan",
    "Feb",
    "Mar",
    "Apr",
    "May",
    "Jun",
    "Jul",
    "Aug",
    "Sep",
    "Oct",
    "Nov",
    "Dec"
  ].indexOf(firmware[1]);
  parsed = Date.UTC(
    Number(firmware[3]),
    month,
    Number(firmware[2]),
    Number(firmware[4]),
    Number(firmware[5]),
    Number(firmware[6])
  );
  return new Date(parsed).toISOString();
}

function strictInteger(value: string): number | null {
  if (!/^[+-]?\d+$/u.test(value)) return null;
  const number = Number(value);
  return Number.isSafeInteger(number) ? number : null;
}

function strictNumber(value: string): number | null {
  if (!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$/u.test(value)) return null;
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

function isIPAddress(value: string): boolean {
  if (/^(?:\d{1,3}\.){3}\d{1,3}$/u.test(value)) {
    return value.split(".").every((part) => Number(part) <= 255);
  }
  // A conservative IPv6 shape is sufficient for presentation classification;
  // values are never used as a URL or network target by this parser.
  return value.includes(":") && /^[0-9a-f:.]+$/iu.test(value);
}

function jsonScalar(value: unknown): string {
  if (value === null) return "null";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return String(value);
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function stripLeadingBOM(value: string): string {
  return value.startsWith("\ufeff") ? value.slice(1) : value;
}

function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength;
}

function firstSeparatorIndex(value: string): number {
  const colon = value.indexOf(":");
  const equals = value.indexOf("=");
  const candidates = [colon, equals].filter((index) => index >= 0);
  return candidates.length > 0 ? Math.min(...candidates) : -1;
}

function validDate(value: Date): Date {
  return Number.isNaN(value.valueOf()) ? new Date() : value;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "invalid data";
}
