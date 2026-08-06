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

function sessionMeta(sessionID) {
  return JSON.stringify({
    timestamp: "2026-08-01T22:59:59Z",
    type: "session_meta",
    payload: { id: sessionID, session_id: sessionID },
  });
}

test("deduplicates exact text across sources while preserving repeated turns within one source", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-turn-audit-"));
  try {
    const short = join(directory, "short.jsonl");
    const complete = join(directory, "complete.jsonl");
    const output = join(directory, "turns.json");
    writeFileSync(short, [
      sessionMeta("continued-session"),
      record("2026-08-02T00:10:00Z", "alpha", "short-alpha"),
      record("2026-08-02T00:10:01Z", "beta", "short-beta"),
      record("2026-08-02T00:10:02Z", "delta", "short-delta"),
    ].join("\n"));
    writeFileSync(complete, [
      sessionMeta("continued-session"),
      record("2026-08-01T23:00:00Z", "alpha", "complete-alpha"),
      record("2026-08-01T23:00:01Z", "beta", "complete-beta"),
      record("2026-08-01T23:00:02Z", "status=?", "complete-status-1"),
      record("2026-08-01T23:00:03Z", "status=?", "complete-status-2"),
      record("2026-08-01T23:00:04Z", "gamma", "complete-gamma"),
      record("2026-08-01T23:00:05Z", "<environment_context>x</environment_context>", "generated-context"),
      record("2026-08-01T23:00:06Z", "<codex_delegation>x</codex_delegation>", "generated-delegation"),
    ].join("\n"));

    const result = spawnSync(process.execPath, [
      script, short, complete, "--json", output,
    ], { encoding: "utf8", windowsHide: true });
    assert.equal(result.status, 0, result.stderr);
    const audit = JSON.parse(readFileSync(output, "utf8"));
    assert.deepEqual(audit.turns.map((turn) => turn.text), [
      "alpha", "beta", "status=?", "status=?", "gamma", "delta",
    ]);
    assert.deepEqual(audit.turns.map((turn) => turn.turn_id), [
      "complete-alpha", "complete-beta", "complete-status-1", "complete-status-2",
      "complete-gamma", "short-delta",
    ]);
    assert.deepEqual(audit.turns.map((turn) => turn.index), [1, 2, 3, 4, 5, 6]);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("preserves identical human turns from distinct root discussions", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-turn-audit-"));
  try {
    const first = join(directory, "first.jsonl");
    const second = join(directory, "second.jsonl");
    const output = join(directory, "turns.json");
    writeFileSync(first, [sessionMeta("first-root"), record("2026-08-01T23:00:00Z", "continue", "one")].join("\n"));
    writeFileSync(second, [sessionMeta("second-root"), record("2026-08-02T00:00:00Z", "continue", "two")].join("\n"));

    const result = spawnSync(process.execPath, [script, first, second, "--json", output], {
      encoding: "utf8", windowsHide: true,
    });
    assert.equal(result.status, 0, result.stderr);
    const audit = JSON.parse(readFileSync(output, "utf8"));
    assert.deepEqual(audit.turns.map((turn) => turn.turn_id), ["one", "two"]);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("filters generated user envelopes by default and can include them explicitly", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-turn-audit-"));
  try {
    const input = join(directory, "session.jsonl");
    const filteredOutput = join(directory, "filtered.json");
    const generatedOutput = join(directory, "generated.json");
    const environment = "<environment_context>generated environment</environment_context>";
    const delegation = "<codex_delegation>generated delegation</codex_delegation>";
    writeFileSync(input, [
      record("2026-08-02T01:00:00Z", "human request", "human"),
      record("2026-08-02T01:00:01Z", environment, "environment"),
      record("2026-08-02T01:00:02Z", delegation, "delegation"),
      record("2026-08-02T01:00:03Z", "Discuss <codex_delegation> as text", "human-tag-reference"),
    ].join("\n"));

    const filtered = spawnSync(process.execPath, [
      script, input, "--json", filteredOutput,
    ], { encoding: "utf8", windowsHide: true });
    assert.equal(filtered.status, 0, filtered.stderr);
    const filteredAudit = JSON.parse(readFileSync(filteredOutput, "utf8"));
    assert.deepEqual(filteredAudit.turns.map((turn) => turn.text), [
      "human request", "Discuss <codex_delegation> as text",
    ]);

    const included = spawnSync(process.execPath, [
      script, input, "--include-generated", "--json", generatedOutput,
    ], { encoding: "utf8", windowsHide: true });
    assert.equal(included.status, 0, included.stderr);
    const generatedAudit = JSON.parse(readFileSync(generatedOutput, "utf8"));
    assert.deepEqual(generatedAudit.turns.map((turn) => turn.text), [
      "human request", environment, delegation, "Discuss <codex_delegation> as text",
    ]);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("reconstructs a legacy JSON record split across physical lines", () => {
  const directory = mkdtempSync(join(tmpdir(), "pccontroller-turn-audit-"));
  try {
    const input = join(directory, "split.jsonl");
    const output = join(directory, "turns.json");
    const generated = record("2026-08-02T02:00:00Z", "first\\nsecond", "split-turn");
    const splitAt = generated.indexOf("\\\\nsecond");
    assert.ok(splitAt > 0);
    writeFileSync(input, `${generated.slice(0, splitAt)}\n${generated.slice(splitAt)}\n`);

    const result = spawnSync(process.execPath, [script, input, "--json", output], {
      encoding: "utf8", windowsHide: true,
    });
    assert.equal(result.status, 0, result.stderr);
    const audit = JSON.parse(readFileSync(output, "utf8"));
    assert.deepEqual(audit.turns.map((turn) => turn.text), ["first\\nsecond"]);
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
  assert.match(result.stdout, /--include-generated/);
});
