# Channel icon assets

`Phosphor.ttf` is the regular Phosphor font shipped in `egui-phosphor 0.14.0`,
matching Pealayer's native and Web icon catalog. `../channel-icon-glyphs.json`
is generated mechanically from that crate's `src/variants/codepoints.rs`:
lowercase constant names, underscores replaced by hyphens, Unicode codepoints
preserved. It contains 1,530 semantic IDs. Product aliases live in `channel-icon.tsx`.

The font is bundled locally; viewing a configured icon never contacts another
Pealayer or a CDN. Do not replace unknown IDs with unrelated default artwork.
Retain both adjacent license notices when redistributing these assets.
