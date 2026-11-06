#!/usr/bin/env node
// Data-only, one-time repair. Never executes transcript commands or pushes refs.
import fs from 'node:fs';
import path from 'node:path';
import readline from 'node:readline';
import crypto from 'node:crypto';
import { createZstdDecompress } from 'node:zlib';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const git = (...args) => execFileSync('git', args, { cwd: root, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 }).trimEnd();
const manifestPath = path.join(root, 'docs/history/recovery.json');
const manifest = JSON.parse(fs.readFileSync(manifestPath));
if (manifest.dateRepair) throw Error('Dates already repaired');
const logPath = process.argv[2];
if (!logPath) throw Error('Usage: node tools/repair-history-dates.mjs ROOT_JSONL [--apply]');
const apply = process.argv.includes('--apply');
const checkpoints = new Map(manifest.checkpoints.map(c => [c.commit, c]));
const completions = new Map();
const sourceHash = crypto.createHash('sha256');
let input = fs.createReadStream(logPath);
if (logPath.endsWith('.zst')) input = input.pipe(createZstdDecompress());
const reader = readline.createInterface({ input, crlfDelay: Infinity });
let lineNumber = 0;
for await (const line of reader) {
  lineNumber++;
  let record;
  try { record = JSON.parse(line); } catch { sourceHash.update(line + '\n'); continue; }
  if (record.timestamp >= manifest.cutoff) break;
  sourceHash.update(line + '\n');
  const p = record.payload;
  if (lineNumber === 1 && p?.id !== manifest.rootSession) throw Error('Wrong root session');
  if (record.type === 'event_msg' && p?.type === 'task_complete' && p.completed_at) {
    if (completions.has(p.turn_id)) throw Error('Ambiguous completion record');
    completions.set(p.turn_id, { timestamp: new Date(p.completed_at * 1000).toISOString(),
      unixSeconds: p.completed_at, sourceSession: manifest.rootSession, sourceLine: lineNumber,
      recordTimestamp: record.timestamp, recordSHA256: crypto.createHash('sha256').update(line).digest('hex'),
      basis: 'root event_msg task_complete.payload.completed_at' });
  }
}
reader.close(); input.destroy();
if (sourceHash.digest('hex') !== manifest.sources.find(s => s.session === manifest.rootSession).sha256) {
  throw Error('Raw JSONL prefix does not match recovered source hash');
}
if (git('branch', '--show-current') !== 'main') throw Error('Expected main');
if (git('diff', '--name-only') || git('diff', '--cached', '--name-only')) throw Error('Tracked tree must be clean');
const oldTip = git('rev-parse', 'HEAD');
const commits = git('rev-list', '--reverse', 'HEAD').split('\n');
const plan = [];
let previousTime = 0;
for (const oldCommit of commits) {
  const raw = execFileSync('git', ['cat-file', 'commit', oldCommit], { cwd: root, encoding: 'utf8' });
  const parents = [...raw.matchAll(/^parent ([a-f0-9]{40})$/gm)];
  if (parents.length > 1 || (plan.length && parents[0]?.[1] !== plan.at(-1).oldCommit)) throw Error('Expected linear history');
  const cp = checkpoints.get(oldCommit);
  const date = cp && completions.get(cp.turn);
  if (cp && !date) throw Error(`Missing exact task completion: ${cp.turn}`);
  if (cp) {
    const evidence = JSON.parse(git('show', `${oldCommit}:${cp.evidence}`));
    const latest = Math.max(...evidence.events.map(e => Date.parse(e.orderTime)));
    if (date.unixSeconds * 1000 < latest || date.timestamp > evidence.observedEnd) throw Error('Completion outside checkpoint bounds');
  }
  const seconds = date?.unixSeconds ?? Number(/^committer .* (\d+) [+-]\d{4}$/m.exec(raw)[1]);
  if (seconds < previousTime) throw Error('Non-monotonic checkpoint dates');
  previousTime = seconds;
  plan.push({ oldCommit, raw, date });
}
if (plan.filter(c => c.date).length !== checkpoints.size) throw Error('Checkpoint missing from ancestry');
console.log(`Verified raw JSONL hash and ${checkpoints.size} exact task completion timestamps.`);
if (!apply) {
  for (const p of plan.filter(p => p.date)) console.log(p.oldCommit, p.date.timestamp, `JSONL:${p.date.sourceLine}`);
  process.exit(0);
}
const backupRef = 'refs/heads/backup/pre-date-repair-' + oldTip.slice(0, 12);
git('update-ref', backupRef, oldTip, '0'.repeat(40));
const backupBundle = path.join(root, '..', '..', 'work', 'history-recovery', 'PCControllerDesktop-before-date-repair.bundle');
if (fs.existsSync(backupBundle)) throw Error('Backup bundle already exists');
git('bundle', 'create', backupBundle, backupRef);
git('bundle', 'verify', backupBundle);
let newParent;
const mapping = [];
for (const p of plan) {
  let raw = p.raw;
  if (newParent) raw = raw.replace(/^parent [a-f0-9]{40}$/m, `parent ${newParent}`);
  if (p.date) raw = raw.replace(/^(author|committer) (.*) \d+ [+-]\d{4}$/gm,
    (_, field, identity) => `${field} ${identity} ${p.date.unixSeconds} +0000`);
  const newCommit = execFileSync('git', ['hash-object', '-t', 'commit', '-w', '--stdin'], { cwd: root, input: raw, encoding: 'utf8' }).trim();
  if (git('rev-parse', `${p.oldCommit}^{tree}`) !== git('rev-parse', `${newCommit}^{tree}`)) throw Error('Tree changed');
  mapping.push({ oldCommit: p.oldCommit, newCommit, ...(p.date ? { date: p.date } : { datesPreserved: true }) });
  newParent = newCommit;
}
git('update-ref', 'refs/heads/main', newParent, oldTip);
for (const cp of manifest.checkpoints) {
  const m = mapping.find(m => m.oldCommit === cp.commit);
  cp.previousCommit = cp.commit;
  cp.commit = m.newCommit;
  cp.historicalDate = m.date;
}
manifest.dateRepair = { repairedAt: new Date().toISOString(), oldTip, rewrittenTip: newParent, backupRef,
  policy: 'Author and committer dates use exact root task completion seconds, not inferred child tool timestamps. Later real commits retain their original dates.', mapping };
fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + '\n');
console.log(`Rewrote ${mapping.length} commits with identical trees. Backup: ${backupRef}. Remote untouched.`);
