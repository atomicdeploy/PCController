// Rebuild our own short sound fixtures; FFmpeg is a developer tool only.
// Run: node Tools/NativeDesktop/internal/sfx/testdata/generate.mjs
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

const directory = fileURLToPath(new URL('.', import.meta.url));
const source = resolve(directory, '../../../assets/sfx/on.wav');
for (const [extension, codec] of [['mp3', 'libmp3lame'], ['flac', 'flac']]) {
  for (const long of [false, true]) {
    const input = long
      ? ['-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=44100:duration=11']
      : ['-i', source];
    const result = spawnSync('ffmpeg', ['-nostdin', '-hide_banner', '-loglevel', 'error',
      '-y', ...input, '-map_metadata', '-1', '-c:a', codec,
      resolve(directory, `${long ? 'long' : 'chime'}.${extension}`)], { encoding: 'utf8' });
    if (result.status !== 0) throw new Error(result.stderr || String(result.error));
  }
}
