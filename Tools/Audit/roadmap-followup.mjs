// Preserve the reviewed follow-up across catalog regeneration. Fail closed on
// malformed markers rather than silently dropping or duplicating later work.
const start = '<!-- media-mcu-roadmap-follow-up -->';
const end = '<!-- /media-mcu-roadmap-follow-up -->';

export function roadmapFollowup(body = '') {
  const text = String(body ?? '');
  const first = text.indexOf(start);
  const last = text.indexOf(end);
  if (first < 0 && last < 0) return '';
  if (first < 0 || last < first || text.indexOf(start, first + start.length) >= 0
      || text.indexOf(end, last + end.length) >= 0) {
    throw new Error('Malformed media/MCU roadmap follow-up; preserve and inspect before syncing');
  }
  return text.slice(first, last + end.length);
}

export function withRoadmapFollowup(generated, existing) {
  const section = roadmapFollowup(existing);
  return section ? `${generated.trimEnd()}\n\n${section}\n` : generated;
}
