#!/usr/bin/env node

// Extracts human-authored user turns from a Codex rollout JSONL file. Raw
// session text is intended for local acceptance auditing, not publication.

import { createReadStream, promises as fs } from 'node:fs';
import { createInterface } from 'node:readline';
import { resolve } from 'node:path';

function usage() {
  return [
    'Usage: node extract-user-turns.mjs SESSION.jsonl [options]',
    '',
    'Options:',
    '  --json FILE       write structured user turns',
    '  --markdown FILE   write a local readable transcript',
    '  --include-context include generated <environment_context> turns',
  ].join('\n');
}

function parseArguments(values) {
  const result = { input: '', json: '', markdown: '', includeContext: false };
  for (let index = 0; index < values.length; index += 1) {
    const value = values[index];
    if (value === '--include-context') {
      result.includeContext = true;
    } else if (value === '--json' || value === '--markdown') {
      const output = values[index + 1];
      if (!output) throw new Error(`${value} requires a file path`);
      result[value === '--json' ? 'json' : 'markdown'] = output;
      index += 1;
    } else if (!result.input) {
      result.input = value;
    } else {
      throw new Error(`unexpected argument ${value}`);
    }
  }
  if (!result.input) throw new Error('a session JSONL path is required');
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
      index: turns.length + 1,
      timestamp: record.timestamp ?? '',
      turn_id: message.internal_chat_message_metadata_passthrough?.turn_id ?? '',
      text,
    });
  }
  return turns;
}

function markdown(turns, source) {
  const sections = [
    '# Local User-Turn Audit',
    '',
    `Source: \`${source}\``,
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
  const input = resolve(options.input);
  const turns = await extract(input, options.includeContext);
  if (options.json) {
    await fs.writeFile(resolve(options.json), `${JSON.stringify({ source: input, turns }, null, 2)}\n`);
  }
  if (options.markdown) {
    await fs.writeFile(resolve(options.markdown), markdown(turns, input));
  }
  if (!options.json && !options.markdown) {
    process.stdout.write(`${JSON.stringify({ source: input, turns }, null, 2)}\n`);
  } else {
    process.stdout.write(`Extracted ${turns.length} user turns.\n`);
  }
}

main().catch((error) => {
  process.stderr.write(`${error.stack ?? error.message}\n`);
  process.exitCode = 1;
});
