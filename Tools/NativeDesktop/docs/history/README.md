# Reconstructed source history

These are recovery checkpoints, not original Git commits or certified historical
builds. They contain only source recovered from successful edits and full-file
reads. Missing preimages and unrecorded shell-generated/vendor/binary files cause
gaps. A later reconciliation commit imports the complete current checkout.
Original tool programs were never executed. Recovered commit author and committer
dates now use the exact root task-completion timestamps recorded in the JSONL.
These date the reconstructed checkpoint, not each individual edit or an original
Git commit. Observed development intervals remain in commit trailers.

## Evidence and limits

- [recovery.json](recovery.json) records 167 source sessions, source hashes,
  49 root turns, 2,628 extracted patch records (2,508 explicitly successful),
  and the exact Git IDs of 43 nonempty recovery checkpoints.
- The checkpoints incorporate 1,556 edit/read events. Each `turn-*.json` records
  session IDs, physical JSONL line numbers, original patch/command hashes,
  content hashes, normalization methods, and replay failures. It contains no
  raw chats or command bodies.
- 593 events have one or more file operations that could not be replayed.
  A multi-file edit may therefore contribute its recoverable files while
  other files remain gaps. Two malformed physical JSONL lines and three
  non-static patch expressions are also recorded. Missing contents were not
  fabricated.
- 62 untruncated full-file read records supplied 130 Go-file observations.
  These are observation checkpoints, not claims that the read changed a file.
  Whitespace matching and, where necessary, `gofmt` are explicitly identified.
- Child logs reused their session-start timestamps. Their per-tool ordering
  uses a time-like field in tool IDs, cross-checked against task intervals.
  This is an undocumented inference, **not an exact historical timestamp**.
  In the root log, 9,522 of 9,535 comparable tool IDs were within 60 seconds of
  the actual record time. These inferred child times are NOT used for Git dates.
  All 43 checkpoints have exact root `task_complete.payload.completed_at` values.
- The recovered tip has 370 source files; 234 match the present files after
  CRLF normalization. The remaining differences, absent files, generated
  assets, dependency trees and shell-only changes are reconciled explicitly
  from the preserved current checkout in the next commit. The recovery
  checkpoints are not release tags and are **not suitable for build bisect**.
- Firmware and uploader work outside this repository is intentionally excluded.
  A request with no recovered in-repository changes has no empty invented commit.

The raw JSONL files, scratch reconstruction scripts, generated intermediate
blobs, screenshots, and binary backups remain local. Source hashes use
decompressed LF-terminated physical lines before the recorded cutoff, including
malformed lines; they are not hashes of the compressed container files.

## Timestamp correction (2026-11-05)

The first importer incorrectly assigned the recovery clock to both Git date
fields. This was corrected from the original JSONL, whose prefix SHA-256 was
verified against the recovery manifest before rewriting. 42 checkpoints date to
July 23–24 (UTC); the final regression checkpoint dates to November 5. The four
later, genuinely new reconciliation/validation commits retain their original
dates. UTC is used explicitly; Git stores whole seconds, matching `completed_at`.

`recovery.json` contains the source session, physical line number, record hash,
record timestamp and completion timestamp for each checkpoint, plus all 47
old-to-new commit IDs under `dateRepair.mapping`. Every rewritten commit preserves
its original tree and message byte-for-byte; only dates and parent IDs changed.
Old hashes in validation receipts and regression reports refer to the original
history and can be resolved through this mapping. Those receipts were not
rewritten to imply that binaries had been rebuilt from the new commit IDs.

The original history is retained locally in branch
`backup/pre-date-repair-36743735c6f8` and a verified Git bundle outside this repo.
The new metadata/verifier commit is dated when the repair was actually performed.
`tools/repair-history-dates.mjs` records the guarded, data-only repair procedure;
it refuses to run again against an already repaired manifest and never pushes.

## Verify

```sh
node tools/verify-history.mjs
git log --format=fuller
git fsck --full
```

The verifier checks historical author/committer dates, ancestry and every
checkpoint's net file hashes against the actual Git objects. This proves
consistency of the imported evidence and repository; it
does not prove completeness of unavailable history or runtime correctness.
See the [current-tree validation receipt](validation-2026-11-05.md) for the
separate build and test results.

A local pattern scan covered the recovered source blobs and current source,
including vendored and generated text. The 12 matches were reviewed and were
synthetic credentials in tests/mock data. This is not a guarantee that a
pattern scan detects every possible secret. No raw transcript was published.
