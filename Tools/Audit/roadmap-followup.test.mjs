import assert from 'node:assert/strict';
import test from 'node:test';
import { roadmapFollowup, withRoadmapFollowup } from './roadmap-followup.mjs';

const start = '<!-- media-mcu-roadmap-follow-up -->';
const end = '<!-- /media-mcu-roadmap-follow-up -->';
const section = `${start}\n## Expanded target follow-up\n\n- [ ] Measure and verify.\n${end}`;

test('absent follow-up leaves generated catalog content untouched', () => {
  assert.equal(roadmapFollowup(null), '');
  assert.equal(withRoadmapFollowup('generated\n', 'ordinary issue content'), 'generated\n');
});

test('regeneration preserves the exact reviewed section without copying unrelated content', () => {
  const existing = `old catalog\n\n${section}\nother material`;
  assert.equal(roadmapFollowup(existing), section);
  const regenerated = withRoadmapFollowup('new catalog\n', existing);
  assert.equal(regenerated, `new catalog\n\n${section}\n`);
  assert.equal(withRoadmapFollowup('new catalog\n', regenerated), regenerated);
  assert.equal(roadmapFollowup(existing.replaceAll('\n', '\r\n')), section.replaceAll('\n', '\r\n'));
});

test('malformed or repeated markers stop synchronization instead of losing requirements', () => {
  for (const body of [start, end, `${end}\n${start}`, `${section}\n${section}`, `${start}\n${section}`]) {
    assert.throws(() => roadmapFollowup(body), /Malformed media\/MCU roadmap follow-up/);
  }
});
