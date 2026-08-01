import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const script = new URL("./extract-user-turns.mjs", import.meta.url).pathname
  .replace(/^\/(?:[A-Za-z]:)/, (value) => value.slice(1));

function record(timestamp, text, turnID = "shared") {
  return JSON.stringify({
    timestamp,
    type: "response_item",
    payload: {
      type: "message",
      role: "user",
      content: [{ type: "input_text", text }],
      internal_chat_message_metadata_passthrough: { turn_id: turnID },
    },
  });
}

test("merges continuation rollouts without losing repeated timeline events", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-turn-audit-"));
  try {
    const short = join(directory, "short.jsonl");
    const complete = join(directory, "complete.jsonl");
    const output = join(directory, "turns.json");
    writeFileSync(short, [
      record("2026-08-02T00:10:00Z", "alpha"),
      record("2026-08-02T00:10:01Z", "beta"),
    ].join("\n"));
    writeFileSync(complete, [
      record("2026-08-01T23:00:00Z", "alpha"),
      record("2026-08-01T23:00:01Z", "beta"),
      record("2026-08-01T23:00:02Z", "status=?"),
      record("2026-08-01T23:00:03Z", "status=?"),
      record("2026-08-01T23:00:04Z", "gamma"),
      record("2026-08-01T23:00:05Z", "<environment_context>x</environment_context>"),
    ].join("\n"));

    const result = spawnSync(process.execPath, [
      script, short, complete, "--json", output,
    ], { encoding: "utf8", windowsHide: true });
    assert.equal(result.status, 0, result.stderr);
    const audit = JSON.parse(readFileSync(output, "utf8"));
    assert.deepEqual(audit.turns.map((turn) => turn.text), [
      "alpha", "beta", "status=?", "status=?", "gamma",
    ]);
    assert.deepEqual(audit.turns.map((turn) => turn.index), [1, 2, 3, 4, 5]);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("help is available without an input file", () => {
  const result = spawnSync(process.execPath, [script, "--help"], {
    encoding: "utf8", windowsHide: true,
  });
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /SESSION2\.jsonl/);
});
