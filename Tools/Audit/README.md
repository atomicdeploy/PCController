<div align="center"><a href="../../README.md"><img src="../../docs/assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a></div>

# Requirement audit helper

## GitHub wiki publisher

`publish-wiki.mjs` validates and mirrors the canonical starter guides,
hardware notes, protocol/API guide, memory tradeoffs, checklist, and backlog to
the repository wiki. Preview the exact page map first:

```cmd
node Tools\Audit\publish-wiki.mjs
```

After GitHub's one-time first wiki page has been created, publish with:

```cmd
node Tools\Audit\publish-wiki.mjs --apply
```

The publisher uses a verified temporary directory, generates `Home.md` and
`_Sidebar.md`, commits only when content changed, and leaves repository
Markdown as the canonical source. Repository identity is resolved from
`GITHUB_REPOSITORY` in automation or the authenticated `gh repo view` result
locally; the publisher and issue synchronizer contain no checkout-specific
owner/repository fallback.

`extract-user-turns.mjs` reads one or more Codex rollout JSONL files and merges
the extracted human-authored user messages chronologically. It removes exact
text duplicated across continuation files belonging to the same root session,
while preserving repeated human turns within one source or between distinct
root discussions as separate timeline events. Generated
`<environment_context>` and `<codex_delegation>` user envelopes are excluded by
default. Supplying every continuation file is important when one long project
conversation spans multiple rollouts. Use `--include-generated` only when an
audit intentionally needs those generated envelopes.

Keep generated transcripts under the repository's ignored `.cache` directory;
they can contain local paths or other private conversation context and must not
be committed to the public project. The canonical publishable acceptance
artifact is [`../../docs/Project-Checklist.md`](../../docs/Project-Checklist.md).

```sh
node Tools/Audit/extract-user-turns.mjs SESSION.jsonl CONTINUATION.jsonl \
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
