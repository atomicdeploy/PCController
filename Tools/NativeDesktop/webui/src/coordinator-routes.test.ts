import { describe, expect, it } from "vitest";
import {
  coordinatorExactProxyPattern,
  coordinatorFamilyProxyPattern
} from "./coordinator-routes";

describe("coordinator development proxy routes", () => {
  const exact = new RegExp(coordinatorExactProxyPattern);
  const family = new RegExp(coordinatorFamilyProxyPattern);

  it("matches exact versionless routes with or without queries", () => {
    expect(exact.test("/status")).toBe(true);
    expect(exact.test("/diagnostics?endpoint=%2Finfo")).toBe(true);
    expect(exact.test("/healthz?probe=1")).toBe(true);
  });

  it("matches routed families without swallowing SPA assets", () => {
    expect(family.test("/device/toggle")).toBe(true);
    expect(family.test("/ui/terminal?session=1")).toBe(true);
    expect(family.test("/audio/play")).toBe(true);
    expect(family.test("/app/assets/index.js")).toBe(false);
    expect(exact.test("/app/session")).toBe(false);
    expect(exact.test("/status-page")).toBe(false);
    expect(exact.test("/v1/status")).toBe(false);
  });
});
