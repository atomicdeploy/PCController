# Requirement audit helper

`extract-user-turns.mjs` reads a Codex rollout JSONL file and extracts only
human-authored user messages. Generated environment-context messages are
excluded by default.

Keep generated transcripts under the repository's ignored `.cache` directory;
they can contain local paths or other private conversation context and must not
be committed to the public project. The canonical publishable acceptance
artifact is [`../../docs/Project-Checklist.md`](../../docs/Project-Checklist.md).

```sh
node Tools/Audit/extract-user-turns.mjs SESSION.jsonl \
  --json .cache/user-turns.json \
  --markdown .cache/user-turns.md
```

`sync-github-requirements.mjs` contains the normalized public requirements
catalog. It validates the existing repository labels, creates or updates issues
by stable `requirement-id` markers, links every item to its epic as a true
GitHub sub-issue, synchronizes evidence-based states and epic counts, and writes
[`../../docs/Requirements-Backlog.md`](../../docs/Requirements-Backlog.md).
It never reads or uploads the private extracted transcript.

Run it without a flag for a read-only plan, then apply the idempotent sync:

```sh
node Tools/Audit/sync-github-requirements.mjs
node Tools/Audit/sync-github-requirements.mjs --apply
```
