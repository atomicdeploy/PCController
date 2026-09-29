#!/usr/bin/env node

// Extracts human-authored user turns from one or more Codex rollout JSONL
// files. Raw session text is intended for local acceptance auditing, not
// publication.

import { createReadStream, promises as fs } from 'node:fs';
import { createInterface } from 'node:readline';
import { resolve } from 'node:path';
import { loadProjectEnv } from '../Build/env.mjs';

loadProjectEnv();

function usage() {
  return [
    'Usage: node extract-user-turns.mjs SESSION.jsonl [SESSION2.jsonl ...] [options]',
    '',
    'Options:',
    '  --json FILE       write structured user turns',
    '  --markdown FILE   write a local readable transcript',
    '  --include-generated include generated context and delegation envelopes',
    '  --include-context   legacy alias for --include-generated',
  ].join('\n');
}

function parseArguments(values) {
  const result = { inputs: [], json: '', markdown: '', includeGenerated: false, help: false };
  for (let index = 0; index < values.length; index += 1) {
    const value = values[index];
    if (value === '--help' || value === '-h') {
      result.help = true;
    } else if (value === '--include-generated' || value === '--include-context') {
      result.includeGenerated = true;
    } else if (value === '--json' || value === '--markdown') {
      const output = values[index + 1];
      if (!output) throw new Error(`${value} requires a file path`);
      result[value === '--json' ? 'json' : 'markdown'] = output;
      index += 1;
    } else if (!value.startsWith('-')) {
      result.inputs.push(value);
    } else {
      throw new Error(`unexpected argument ${value}`);
    }
  }
  if (!result.help && result.inputs.length === 0) {
    throw new Error('at least one session JSONL path is required');
  }
  return result;
}

function messageText(message) {
  if (!Array.isArray(message?.content)) return '';
  return message.content
    .filter((item) => item?.type === 'input_text')
    .map((item) => String(item.text ?? ''))
    .join('\n')
    .trim();
}

function isGeneratedEnvelope(text) {
  return /^(?:<environment_context>[\s\S]*<\/environment_context>|<codex_delegation>[\s\S]*<\/codex_delegation>)$/.test(text);
}

async function extract(input, includeGenerated) {
  const turns = [];
  let sessionID = '';
  let physicalLine = 0;
  let pending = '';
  let pendingStartLine = 0;
  const lines = createInterface({
    input: createReadStream(input, { encoding: 'utf8' }),
    crlfDelay: Infinity,
  });
  for await (const line of lines) {
    physicalLine += 1;
    if (!pending && !line.trim()) continue;
    const candidate = pending ? pending + line : line;
    let record;
    try {
      record = JSON.parse(candidate);
    } catch (error) {
      // Older Codex exports can split one otherwise valid JSON record across
      // physical lines inside a large tool result. The continuation begins
      // with an escaped newline, so joining without introducing another byte
      // reconstructs the original record exactly.
      if (!pending && /Unexpected end of JSON input|Unterminated string/.test(error.message)) {
        pending = line;
        pendingStartLine = physicalLine;
        continue;
      }
      if (pending && candidate.length <= 64 * 1024 * 1024) {
        pending = candidate;
        continue;
      }
      throw new Error(`invalid JSONL near physical line ${pendingStartLine || physicalLine}: ${error.message}`);
    }
    pending = '';
    pendingStartLine = 0;
    if (record?.type === 'session_meta') {
      sessionID = String(record.payload?.session_id ?? record.payload?.id ?? sessionID);
    }
    const message = record?.type === 'response_item' ? record.payload : null;
    if (message?.type !== 'message' || message.role !== 'user') continue;
    const text = messageText(message);
    if (!text || (!includeGenerated && isGeneratedEnvelope(text))) continue;
    turns.push({
      timestamp: record.timestamp ?? '',
      turn_id: message.internal_chat_message_metadata_passthrough?.turn_id ?? '',
      source: input,
      text,
    });
  }
  if (pending) {
    throw new Error(`truncated JSONL record beginning at physical line ${pendingStartLine}`);
  }
  return { sessionID: sessionID || input, turns };
}

function mergeTurns(groups) {
  // Compaction/export can copy an earlier rollout wholesale into a later one
  // while changing its synthetic timestamps and coarse turn IDs. Prefer the
  // most complete source, then discard only exact text duplicated by another
  // source. Repeated messages inside one source remain valid timeline events.
  const turns = [];
  const sessions = new Map();
  for (const [order, group] of groups.entries()) {
    const session = sessions.get(group.sessionID) ?? [];
    session.push({ turns: group.turns, order });
    sessions.set(group.sessionID, session);
  }
  for (const session of sessions.values()) {
    const ordered = session.sort(
      (left, right) => right.turns.length - left.turns.length || left.order - right.order,
    );
    const seenFromEarlierSources = new Set();
    for (const group of ordered) {
      const sourceTexts = new Set();
      for (const turn of group.turns) {
        if (seenFromEarlierSources.has(turn.text)) continue;
        turns.push(turn);
        sourceTexts.add(turn.text);
      }
      for (const text of sourceTexts) seenFromEarlierSources.add(text);
    }
  }
  turns.sort((left, right) => left.timestamp.localeCompare(right.timestamp));
  return turns.map((turn, index) => ({ ...turn, index: index + 1 }));
}

function markdown(turns, sources) {
  const sections = [
    '# Local User-Turn Audit',
    '',
    'Sources:',
    '',
    ...sources.map((source) => `- \`${source}\``),
    '',
    `Extracted user turns: ${turns.length}`,
  ];
  for (const turn of turns) {
    sections.push('', `## Turn ${turn.index}`, '', `Timestamp: ${turn.timestamp}`, '', turn.text);
  }
  sections.push('');
  return sections.join('\n');
}

async function main() {
  let options;
  try {
    options = parseArguments(process.argv.slice(2));
  } catch (error) {
    process.stderr.write(`${error.message}\n\n${usage()}\n`);
    process.exitCode = 2;
    return;
  }
  if (options.help) {
    process.stdout.write(`${usage()}\n`);
    return;
  }
  const inputs = options.inputs.map((input) => resolve(input));
  const turns = mergeTurns(await Promise.all(
    inputs.map((input) => extract(input, options.includeGenerated)),
  ));
  if (options.json) {
    await fs.writeFile(resolve(options.json), `${JSON.stringify({ sources: inputs, turns }, null, 2)}\n`);
  }
  if (options.markdown) {
    await fs.writeFile(resolve(options.markdown), markdown(turns, inputs));
  }
  if (!options.json && !options.markdown) {
    process.stdout.write(`${JSON.stringify({ sources: inputs, turns }, null, 2)}\n`);
  } else {
    process.stdout.write(`Extracted ${turns.length} user turns.\n`);
  }
}

main().catch((error) => {
  process.stderr.write(`${error.stack ?? error.message}\n`);
  process.exitCode = 1;
});
