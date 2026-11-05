#!/usr/bin/env node
// Read-only source-preservation audit. This does NOT certify runtime parity.
import { execFileSync } from 'node:child_process';
import { existsSync, lstatSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { loadProjectEnv } from './env.mjs';

loadProjectEnv();

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const prefix = 'Tools/NativeDesktop/';
const baseline = {
  donorCommit: 'b5847a0c86f9931a86048d418ff8ff5045f52b0a',
  importCommit: '08ef6eb74ad38ba86a9a356d5d5ed3f8fda95c8a',
  tree: 'c515cb331313153b8866d241c3897aa622b25e3e',
  trackedFiles: 2433,
};
const exclusions = new Map([
  ['assets/fonts/yekanbakh-vf.ttf', 'User-requested replacement with PCController WebUI family Vazirmatn; donor-only redistribution permission. Original blob remains in the immutable import.'],
]);
// Content-hashed bundles regenerated from retained donor Web source after the
// licensed font adaptation and scheduler origin guard. Keep exact old/new pairs: missing source or an
// unrelated deleted asset must still fail this audit.
const regeneratedAssets = new Map([
  ['internal/webui/dist/assets/index-BJJi2oIR.css', 'internal/webui/dist/assets/index-BI9zYtDG.css'],
  ['internal/webui/dist/assets/index-P8wU9w4v.js', 'internal/webui/dist/assets/index-SrMXIX_H.js'],
  ['internal/webui/dist/assets/index-P8wU9w4v.js.map', 'internal/webui/dist/assets/index-SrMXIX_H.js.map'],
  ['internal/webui/dist/assets/addon-webgl-qNQI_Zx4.js', 'internal/webui/dist/assets/addon-webgl-DTes4Zuf.js'],
  ['internal/webui/dist/assets/addon-webgl-qNQI_Zx4.js.map', 'internal/webui/dist/assets/addon-webgl-DTes4Zuf.js.map'],
  ['internal/webui/dist/assets/xterm-B6pQ9H5k.js', 'internal/webui/dist/assets/xterm-BbplF7Um.js'],
  ['internal/webui/dist/assets/xterm-B6pQ9H5k.js.map', 'internal/webui/dist/assets/xterm-BbplF7Um.js.map'],
]);
const git = (cwd, args, input) => execFileSync('git', args, {
  cwd, input, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024,
  stdio: ['pipe', 'pipe', 'pipe'],
});

function audit() {
  const args = process.argv.slice(2);
  const options = { json: false, donor: undefined };
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--json') options.json = true;
    else if (args[i] === '--donor' && args[i + 1]) options.donor = path.resolve(args[++i]);
    else throw new Error('Usage: node Tools/Build/audit-native-donor.mjs [--json] [--donor <checkout>]');
  }
  const failures = [];
  const tree = git(root, ['rev-parse', `${baseline.importCommit}^{tree}`]).trim();
  if (tree !== baseline.tree) failures.push('Imported tree differs from the recorded complete donor tree.');
  const entries = git(root, ['ls-tree', '-rz', '--full-tree', baseline.importCommit])
    .split('\0').filter(Boolean).map(record => {
      const match = /^(\d+) (\w+) ([a-f0-9]+)\t([\s\S]+)$/.exec(record);
      if (!match || match[2] !== 'blob') throw new Error(`Unsupported donor tree entry: ${record}`);
      return { mode: match[1], originalBlob: match[3], path: match[4] };
    });
  if (entries.length !== baseline.trackedFiles) failures.push('Donor tracked-file count changed.');

  let donor;
  if (options.donor) {
    donor = {
      head: git(options.donor, ['rev-parse', 'HEAD']).trim(),
      tree: git(options.donor, ['rev-parse', 'HEAD^{tree}']).trim(),
      dirty: git(options.donor, ['status', '--porcelain', '--untracked-files=all']).trim() !== '',
    };
    if (donor.head !== baseline.donorCommit || donor.tree !== baseline.tree || donor.dirty) {
      failures.push('Donor changed or has uncommitted source: review and update the import before claiming current coverage.');
    }
  }

  const present = [];
  for (const entry of entries) {
    const target = path.join(root, prefix, entry.path);
    if (!existsSync(target)) {
      const replacement = regeneratedAssets.get(entry.path);
      if (replacement && existsSync(path.join(root, prefix, replacement))) {
        entry.status = 'regenerated-asset';
        entry.replacement = replacement;
        continue;
      }
      entry.status = exclusions.has(entry.path) ? 'excluded-asset' : 'missing';
      if (entry.status === 'missing') failures.push(`Missing donor file: ${entry.path}`);
      else entry.reason = exclusions.get(entry.path);
    } else if (!lstatSync(target).isFile()) {
      entry.status = 'not-a-file';
      failures.push(`Donor path is no longer a regular file: ${entry.path}`);
    } else {
      if (exclusions.has(entry.path)) failures.push(`Excluded font was reintroduced: ${entry.path}`);
      present.push(entry);
    }
  }
  // One Git process; respects repository clean/EOL filters. No staging, writes,
  // checkout, network, builds, UI launch or hardware access. JSON quotes paths
  // for Git's --stdin-paths format, including whitespace/backslash escaping.
  if (present.length) {
    const hashes = git(root, ['hash-object', '--stdin-paths'], present
      .map(entry => JSON.stringify(prefix + entry.path)).join('\n') + '\n')
      .trim().split(/\r?\n/);
    if (hashes.length !== present.length) throw new Error('Incomplete Git blob hash response.');
    present.forEach((entry, i) => {
      entry.currentBlob = hashes[i];
      entry.status = hashes[i] === entry.originalBlob ? 'identical' : 'adapted-review-required';
    });
  }
  const counts = {};
  for (const entry of entries) counts[entry.status] = (counts[entry.status] ?? 0) + 1;
  const report = {
    schemaVersion: 1, scope: 'source preservation only; NOT runtime feature parity',
    baseline, donor, counts, failures, files: entries,
  };
  if (options.json) console.log(JSON.stringify(report, null, 2));
  else {
    console.log(`Donor ${baseline.donorCommit}; complete imported tree ${tree}`);
    console.log(`${entries.length} donor files: ${JSON.stringify(counts)}`);
    console.log('Adaptations (review separately from source-preservation proof):');
    for (const entry of entries.filter(entry => entry.status !== 'identical')) {
      console.log(`  ${entry.status}: ${entry.path}`);
    }
    for (const failure of failures) console.error(failure);
    console.log(failures.length ? 'Source audit FAILED.' : 'Source preservation verified. Runtime parity is NOT certified.');
  }
  process.exitCode = failures.length ? 1 : 0;
}

try { audit(); } catch (error) {
  console.error(`Native donor audit failed: ${error.message}`);
  process.exitCode = 1;
}
