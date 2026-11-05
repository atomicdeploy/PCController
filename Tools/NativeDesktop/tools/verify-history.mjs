#!/usr/bin/env node
// Verifies recovered checkpoint content without requiring private chat logs.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const git = (...args) => execFileSync('git', args, {
  cwd: root, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024,
});
const manifest = JSON.parse(fs.readFileSync(path.join(root, 'docs/history/recovery.json')));
const expectations = new Map();
let changes = 0;
let previousDate = 0;
if (!manifest.dateRepair) throw new Error('Historical date provenance is required');
const ancestry = new Set(git('rev-list', 'HEAD').trim().split('\n'));
for (const checkpoint of manifest.checkpoints) {
  if (!/^[a-f0-9]{40}$/.test(checkpoint.commit)) throw new Error('Invalid commit ID');
  if (!ancestry.has(checkpoint.commit)) throw new Error('Checkpoint is not in current history');
  const date = checkpoint.historicalDate;
  if (!date || date.basis !== 'root event_msg task_complete.payload.completed_at' ||
      date.sourceSession !== manifest.rootSession || !Number.isInteger(date.sourceLine) ||
      !/^[a-f0-9]{64}$/.test(date.recordSHA256) ||
      new Date(date.unixSeconds * 1000).toISOString() !== date.timestamp) {
    throw new Error('Missing or inconsistent exact timestamp evidence');
  }
  const [authorDate, committerDate] = git('show', '-s', '--format=%at %ct', checkpoint.commit).trim().split(' ').map(Number);
  if (authorDate !== date.unixSeconds || committerDate !== date.unixSeconds || authorDate < previousDate) {
    throw new Error(`Historical commit date mismatch: ${checkpoint.commit}`);
  }
  previousDate = authorDate;
  const evidence = JSON.parse(git('show', `${checkpoint.commit}:${checkpoint.evidence}`));
  if (evidence.turn !== checkpoint.turn) throw new Error('Turn/commit mismatch');
  const tree = new Map(git('ls-tree', '-r', '-z', checkpoint.commit)
    .split('\0').filter(Boolean).map(entry => {
      const [metadata, name] = entry.split('\t');
      return [name, metadata.split(' ')[2]];
    }));
  for (const item of evidence.finalChanges) {
    const object = tree.get(item.path);
    if (item.sha256 === null) {
      if (object) throw new Error(`Expected deleted path: ${item.path}`);
    } else {
      if (!object) throw new Error(`Missing recovered path: ${item.path}`);
      const previous = expectations.get(object);
      if (previous && previous !== item.sha256) throw new Error('Conflicting content hashes');
      expectations.set(object, item.sha256);
    }
    changes++;
  }
}
const objects = [...expectations.keys()];
const batch = execFileSync('git', ['cat-file', '--batch'], {
  cwd: root, input: objects.join('\n') + '\n', maxBuffer: 128 * 1024 * 1024,
});
let offset = 0;
for (const object of objects) {
  const end = batch.indexOf(10, offset);
  const [actual, type, length] = batch.subarray(offset, end).toString().split(' ');
  if (actual !== object || type !== 'blob') throw new Error('Invalid Git batch object');
  const size = Number(length);
  const body = batch.subarray(end + 1, end + 1 + size);
  if (crypto.createHash('sha256').update(body).digest('hex') !== expectations.get(object)) {
    throw new Error(`Recovered content hash mismatch: ${object}`);
  }
  offset = end + 1 + size + 1;
}
git('fsck', '--full');
console.log(`Verified ${manifest.checkpoints.length} recovery commits and historical author/committer dates, ${changes} path changes, ${objects.length} unique source blobs; git fsck passed.`);
