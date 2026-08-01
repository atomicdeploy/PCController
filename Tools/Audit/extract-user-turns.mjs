#!/usr/bin/env node

// Extracts human-authored user turns from one or more Codex rollout JSONL
// files. Raw session text is intended for local acceptance auditing, not
// publication.

import { createReadStream, promises as fs } from 'node:fs';
import { createInterface } from 'node:readline';
import { resolve } from 'node:path';

function usage() {
  return [
    'Usage: node extract-user-turns.mjs SESSION.jsonl [SESSION2.jsonl ...] [options]',
    '',
    'Options:',
    '  --json FILE       write structured user turns',
    '  --markdown FILE   write a local readable transcript',
    '  --include-context include generated <environment_context> turns',
  ].join('\n');
}

function parseArguments(values) {
  const result = { inputs: [], json: '', markdown: '', includeContext: false, help: false };
  for (let index = 0; index < values.length; index += 1) {
    const value = values[index];
    if (value === '--help' || value === '-h') {
      result.help = true;
    } else if (value === '--include-context') {
      result.includeContext = true;
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

function isGeneratedContext(text) {
  return /^<environment_context>[\s\S]*<\/environment_context>$/.test(text);
}

async function extract(input, includeContext) {
  const turns = [];
  const lines = createInterface({
    input: createReadStream(input, { encoding: 'utf8' }),
    crlfDelay: Infinity,
  });
  for await (const line of lines) {
    if (!line.trim()) continue;
    let record;
    try {
      record = JSON.parse(line);
    } catch (error) {
      throw new Error(`invalid JSONL near line ${turns.length + 1}: ${error.message}`);
    }
    const message = record?.type === 'response_item' ? record.payload : null;
    if (message?.type !== 'message' || message.role !== 'user') continue;
    const text = messageText(message);
    if (!text || (!includeContext && isGeneratedContext(text))) continue;
    turns.push({
      timestamp: record.timestamp ?? '',
      turn_id: message.internal_chat_message_metadata_passthrough?.turn_id ?? '',
      source: input,
      text,
    });
  }
  return turns;
}

function mergeTurns(groups) {
  // Compaction/export can copy an earlier rollout wholesale into a later one
  // while changing its synthetic timestamps and coarse turn IDs. Prefer the
  // most complete source, then discard only exact text duplicated by another
  // source. Repeated messages inside one source remain valid timeline events.
  const ordered = groups
    .map((turns, order) => ({ turns, order }))
    .sort((left, right) => right.turns.length - left.turns.length || left.order - right.order);
  const seenFromEarlierSources = new Set();
  const turns = [];
  for (const group of ordered) {
    const sourceTexts = new Set();
    for (const turn of group.turns) {
      if (seenFromEarlierSources.has(turn.text)) continue;
      turns.push(turn);
      sourceTexts.add(turn.text);
    }
    for (const text of sourceTexts) {
      seenFromEarlierSources.add(text);
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
    inputs.map((input) => extract(input, options.includeContext)),
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
