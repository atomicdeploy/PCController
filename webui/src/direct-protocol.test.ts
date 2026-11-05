import { describe, expect, it } from "vitest";
import {
  DIRECT_DIAGNOSTIC_ENDPOINTS,
  deriveDirectWebSocketURL,
  directDiagnosticURL,
  directLampCommandURL,
  directProjectionDiagnosticURLs,
  isDirectDiagnosticEndpoint,
  normalizeDirectLampURL,
  normalizeDirectLampURLs,
  parseDirectDiagnostic,
  parseDirectLampState,
  parseDirectProjectionDiagnostic,
  parseDirectWebSocketFrame,
  parseDurationMilliseconds,
  projectDirectDiagnosticSummary,
  reconnectDelayMilliseconds,
  type DirectDiagnosticDocuments
} from "./direct-protocol";

describe("direct lamp URL policy", () => {
  it("normalizes only root HTTP(S) device authorities", () => {
    expect(normalizeDirectLampURL(" rayanlamp.local ")).toBe(
      "http://rayanlamp.local"
    );
    expect(normalizeDirectLampURL("HTTPS://Lamp.Example:8443/")).toBe(
      "https://lamp.example:8443"
    );
    expect(normalizeDirectLampURLs("http://[::1]:8080")).toEqual({
      baseURL: "http://[::1]:8080",
      websocketURL: "ws://[::1]:8080/ws"
    });
    expect(deriveDirectWebSocketURL("https://rayanlamp.local")).toBe(
      "wss://rayanlamp.local/ws"
    );
    expect(directLampCommandURL("rayanlamp.local", "off")).toBe(
      "http://rayanlamp.local/lamp/off"
    );
  });

  it.each([
    "",
    "ftp://rayanlamp.local",
    "http://user:pass@rayanlamp.local",
    "http://rayanlamp.local/info",
    "http://rayanlamp.local?",
    "http://rayanlamp.local#",
    "http://rayanlamp.local/?state=on",
    "http://rayanlamp.local\\evil",
    "http://rayanlamp.local:70000"
  ])("rejects unsafe base URL %j", (value) => {
    expect(() => normalizeDirectLampURL(value)).toThrow();
  });

  it("constructs only exact audited diagnostics URLs", () => {
    expect(DIRECT_DIAGNOSTIC_ENDPOINTS).toHaveLength(6);
    expect(isDirectDiagnosticEndpoint("/info")).toBe(true);
    expect(isDirectDiagnosticEndpoint("/config/st")).toBe(false);
    expect(isDirectDiagnosticEndpoint("/config/ap")).toBe(false);
    expect(isDirectDiagnosticEndpoint("/pu")).toBe(false);
    expect(directDiagnosticURL("rayanlamp.local", "/wifi/signal")).toBe(
      "http://rayanlamp.local/wifi/signal"
    );
    expect(() =>
      directDiagnosticURL(
        "rayanlamp.local",
        "/pu" as (typeof DIRECT_DIAGNOSTIC_ENDPOINTS)[number]
      )
    ).toThrow(/allowlist/i);
    expect(
      directProjectionDiagnosticURLs("rayanlamp.local")
    ).toEqual([
      {
        endpoint: "/config/ap",
        url: "http://rayanlamp.local/config/ap"
      },
      {
        endpoint: "/config/st",
        url: "http://rayanlamp.local/config/st"
      }
    ]);
  });
});

describe("direct state responses", () => {
  it.each([
    ["ON", "on"],
    [" true ", "on"],
    ["HIGH", "on"],
    ["1", "on"],
    ["OFF", "off"],
    ["false", "off"],
    ["LOW", "off"],
    ["0", "off"],
    ['"ON"', "on"],
    ['{"state":"off"}', "off"],
    ['{"STATUS":"HIGH"}', "on"],
    ['{"lamp":true}', "on"],
    ['{"value":false}', "off"]
  ] as const)("parses %s as %s", (payload, state) => {
    expect(parseDirectLampState(payload)).toBe(state);
  });

  it.each([
    "",
    "maybe",
    "null",
    "[]",
    '{"state":2}',
    '{"other":"ON"}',
    '{"state":"unknown"}',
    "{broken"
  ])("fails closed for %s", (payload) => {
    expect(parseDirectLampState(payload)).toBe("unknown");
  });
});

describe("direct firmware WebSocket frames", () => {
  it("parses state, messages, URL-encoded messages, and bounded beeps", () => {
    expect(parseDirectWebSocketFrame(" on ")).toEqual({
      type: "snapshot",
      state: "on",
      raw: "on"
    });
    expect(parseDirectWebSocketFrame("message: Hello\tlamp")).toEqual({
      type: "message",
      title: "Rayan Lamp",
      message: "Hello\tlamp",
      icon: 0,
      raw: "message: Hello\tlamp"
    });
    expect(
      parseDirectWebSocketFrame("MSG: 12,Hello+Rayan%21")
    ).toEqual({
      type: "message",
      title: "Rayan Lamp",
      message: "Hello Rayan!",
      icon: 12,
      raw: "MSG: 12,Hello+Rayan%21"
    });
    expect(parseDirectWebSocketFrame("beep:10")).toEqual({
      type: "beep",
      count: 10,
      raw: "beep:10"
    });
  });

  it.each([
    "state:ON",
    "temperature:27",
    "message:",
    "message:\u0001bad",
    "msg:256,no",
    "msg:1,%ZZ",
    "beep:0",
    "beep:11",
    "beep:1.5",
    "ON\0"
  ])("ignores unrelated or malformed frame %j", (frame) => {
    expect(parseDirectWebSocketFrame(frame)).toBeNull();
  });

  it("enforces UTF-8 byte limits rather than UTF-16 character counts", () => {
    expect(
      parseDirectWebSocketFrame(`message:${"چراغ".repeat(1_100)}`)
    ).toBeNull();
    expect(parseDirectWebSocketFrame("x".repeat((16 << 10) + 1))).toBeNull();
  });
});

describe("direct reconnect configuration", () => {
  it.each([
    ["0", 0],
    ["250ms", 250],
    ["1.5s", 1_500],
    ["1m30s", 90_000],
    ["2h5m", 7_500_000]
  ] as const)("parses %s", (value, expected) => {
    expect(parseDurationMilliseconds(value)).toBe(expected);
  });

  it.each(["", "-1s", "1 s", "1d", "ms", "1s trailing"])(
    "rejects invalid duration %j",
    (value) => {
      expect(parseDurationMilliseconds(value)).toBeNull();
    }
  );

  it("applies bounded exponential jitter deterministically", () => {
    expect(reconnectDelayMilliseconds(1, 500, 10_000, 0.2, () => 0)).toBe(
      400
    );
    expect(reconnectDelayMilliseconds(2, 500, 10_000, 0.2, () => 0.5)).toBe(
      1_000
    );
    expect(reconnectDelayMilliseconds(99, 500, 10_000, 0.2, () => 1)).toBe(
      10_000
    );
  });
});

describe("direct diagnostics", () => {
  it("parses and projects the real line-oriented device shape safely", () => {
    const document = parseDirectDiagnostic(
      "/info",
      [
        "hostname: RayanLamp",
        "wifi version: SDK:2.2.2/Core:3.1.2",
        "esp chip id: b48537",
        "firmware hash: abcdef (CORRUPTED)",
        "protocol rev.: 2 (no match)",
        "st mac: 84:F3:EB:B4:85:37",
        "client ip: 172.20.5.130",
        "flash size: 1.00 MB (real: 4.00 MB)",
        "program usage: 40%",
        "settings hash: 9b19eed5, status: valid",
        "heap memory: 17976",
        "build: Feb 16 2026 04:47:51 by David@David-PC",
        "uptime: 39 days, 10:30:45",
        "api token: never expose this"
      ].join("\n"),
      new Date("2026-07-23T00:00:00Z")
    );
    expect(document.format).toBe("text");
    expect(document.raw).toContain("api token: [redacted]");
    expect(document.raw).not.toContain("never expose");
    expect(document.fields.find((field) => field.key === "api_token")).toMatchObject({
      kind: "redacted",
      redacted: true
    });

    const summary = projectDirectDiagnosticSummary({ "/info": document });
    expect(summary).toMatchObject({
      hostname: "RayanLamp",
      wifi_version: "SDK:2.2.2/Core:3.1.2",
      esp_chip_id: "b48537",
      firmware_hash: "abcdef",
      firmware_corrupted: true,
      protocol_revision: "2",
      protocol_match: false,
      station_mac: "84:F3:EB:B4:85:37",
      client_ip: "172.20.5.130",
      flash_size_bytes: 1_048_576,
      flash_real_size_bytes: 4_194_304,
      program_usage_percent: 40,
      settings_hash: "9b19eed5",
      settings_status: "valid",
      heap_free_bytes: 17_976,
      uptime_seconds: 3_407_445
    });
  });

  it("parses JSON, compact signal/country text, status, and safe XML", () => {
    const documents: DirectDiagnosticDocuments = {
      "/update/info": parseDirectDiagnostic(
        "/update/info",
        JSON.stringify({
          hash: "abc123",
          size: 1_048_576,
          free: 2_097_152,
          date: "2026-02-16T04:47:51Z",
          session_token: "secret"
        })
      ),
      "/wifi/signal": parseDirectDiagnostic(
        "/wifi/signal",
        "-63\nGood\n"
      ),
      "/wifi/status": parseDirectDiagnostic(
        "/wifi/status",
        '{"connected":true,"ssid":"Office","ip":"172.20.5.130"}'
      ),
      "/wifi/country": parseDirectDiagnostic("/wifi/country", "ir"),
      "/description.xml": parseDirectDiagnostic(
        "/description.xml",
        "<?xml version=\"1.0\"?><device><friendlyName>Rayan Lamp</friendlyName>" +
          "<modelName>ESP8266</modelName><manufacturer>YektaYar</manufacturer>" +
          "<ignored>not projected</ignored></device>"
      )
    };

    expect(documents["/update/info"]?.raw).not.toContain("secret");
    expect(documents["/description.xml"]?.format).toBe("xml");
    const summary = projectDirectDiagnosticSummary(documents);
    expect(summary).toMatchObject({
      device_name: "Rayan Lamp",
      rssi_dbm: -63,
      signal_quality: "Good",
      wifi_connected: true,
      ssid: "Office",
      wifi_ip: "172.20.5.130",
      country: "IR",
      update: {
        hash: "abc123",
        size_bytes: 1_048_576,
        free_bytes: 2_097_152,
        date: "2026-02-16T04:47:51.000Z"
      },
      description: {
        friendly_name: "Rayan Lamp",
        model_name: "ESP8266",
        manufacturer: "YektaYar"
      }
    });
  });

  it("redacts private Wi-Fi configs and projects station and AP data separately", () => {
    const station = parseDirectProjectionDiagnostic(
      "/config/st",
      [
        "ssid: Office",
        "pass: station-plaintext-secret",
        "dhcp: enabled",
        "ip: 172.20.5.130",
        "netmask: 255.255.255.0",
        "gw: 172.20.5.1",
        "dns: 172.20.5.1, 1.1.1.1"
      ].join("\n")
    );
    const accessPoint = parseDirectProjectionDiagnostic(
      "/config/ap",
      [
        "ssid: RayanLamp Setup",
        "pass: ap-plaintext-secret",
        "ip: 192.168.4.1",
        "netmask: 255.255.255.0"
      ].join("\n")
    );
    const status = parseDirectDiagnostic(
      "/wifi/status",
      "ap status: 1\nst status: 1\n"
    );

    expect(station.raw).toContain("pass: [redacted]");
    expect(station.raw).not.toContain("station-plaintext-secret");
    expect(accessPoint.raw).not.toContain("ap-plaintext-secret");
    expect(
      station.fields.find((field) => field.key === "pass")
    ).toMatchObject({
      value: "[redacted]",
      kind: "redacted",
      redacted: true
    });

    expect(
      projectDirectDiagnosticSummary({
        "/wifi/status": status,
        "/config/st": station,
        "/config/ap": accessPoint
      })
    ).toMatchObject({
      wifi_connected: true,
      ssid: "Office",
      wifi_ip: "172.20.5.130",
      station_status: "Connected",
      station_dhcp: true,
      station_netmask: "255.255.255.0",
      station_gateway: "172.20.5.1",
      station_dns: "172.20.5.1, 1.1.1.1",
      access_point_status: "Enabled",
      access_point_ssid: "RayanLamp Setup",
      access_point_ip: "192.168.4.1",
      access_point_netmask: "255.255.255.0"
    });
  });

  it.each([
    [1, true, "Connected"],
    [2, false, "Disconnected"],
    [3, false, "Connecting"],
    [4, false, "SSID not found"],
    [5, false, "Wrong password"],
    [6, false, "Connection failed"],
    [7, false, "Idle"]
  ] as const)(
    "maps firmware station status code %i to %s",
    (code, connected, label) => {
      const status = parseDirectDiagnostic(
        "/wifi/status",
        `st status: ${code}`
      );
      expect(
        projectDirectDiagnosticSummary({ "/wifi/status": status })
      ).toMatchObject({
        wifi_connected: connected,
        station_status: label
      });
    }
  );

  it("rejects unaudited endpoints, active XML declarations, and oversized data", () => {
    expect(() =>
      parseDirectDiagnostic("/pu" as "/info", "unsafe")
    ).toThrow(/allowlist/i);
    expect(() =>
      parseDirectDiagnostic("/config/st" as "/info", "ssid: no")
    ).toThrow(/allowlist/i);

    const xml = parseDirectDiagnostic(
      "/description.xml",
      '<!DOCTYPE x [<!ENTITY y SYSTEM "file:///etc/passwd">]><device>&y;</device>'
    );
    expect(xml.format).toBe("text");
    expect(xml.warnings?.join(" ")).toMatch(/DOCTYPE/i);

    expect(() =>
      parseDirectDiagnostic("/info", "x".repeat((1 << 20) + 1))
    ).toThrow(/1 MiB/i);
  });

  it("redacts short credential aliases in malformed private JSON", () => {
    const document = parseDirectProjectionDiagnostic(
      "/config/st",
      '{"ssid":"Office","psk":"must-not-survive"'
    );
    expect(document.raw).not.toContain("must-not-survive");
    expect(document.raw).toContain("[redacted]");
  });
});
