import { describe, expect, it } from "vitest";
import { guardXtermScheduler, xtermSchedulerGuard } from "../xterm-scheduler-guard";

describe("xterm scheduler sender boundary", () => {
  it.each([false, true])("accepts only same-window same-origin messages (UMD=%s)", (umd) => {
    const start = umd ? "(e=>{" : "e=>{";
    const end = umd ? "}));" : "});";
    const source = `g.addEventListener("message",${start}if(e.data&&e.data.vscodeScheduleAsyncWork)run()${end}`;
    let listener: (event: unknown) => void = () => {};
    let calls = 0;
    const owner = { location: { origin: "http://127.0.0.1:8787" }, addEventListener: (_: string, callback: typeof listener) => { listener = callback; } };
    new Function("globalThis", "g", "run", guardXtermScheduler(source))(owner, owner, () => { calls++; });
    const message = { source: owner, origin: owner.location.origin, data: { vscodeScheduleAsyncWork: 1 } };
    listener({ ...message, source: {} });
    listener({ ...message, origin: "https://untrusted.invalid" });
    expect(calls).toBe(0);
    listener(message);
    expect(calls).toBe(1);
  });

  it("fails closed on upstream scheduler changes and leaves unrelated modules untouched", () => {
    expect(() => guardXtermScheduler("unrecognized source")).toThrow("expected one handler");
    expect(xtermSchedulerGuard().transform("unrecognized source", "/src/app.ts")).toBeUndefined();
  });
});
