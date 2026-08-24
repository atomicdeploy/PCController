<div align="center">
  <a href="../README.md"><img src="assets/doc-banner.svg" width="100%" alt="PCController documentation — return to the main page"></a>
</div>

# GitHub collaboration and handoffs

GitHub is the canonical shared record for PCController source, requirements,
decisions, delivery state, and handoffs. A local checkout may contain useful
work, but another contributor must not need that machine or a private chat to
understand, resume, test, or merge it. This agreement applies equally to human
contributors, agentic tasks, automation, and work performed on another host.

## Working agreement

1. **Reconcile before editing.** Fetch `origin`, inspect current `main`, the
   owning issue, open pull requests, and relevant Draft/WIP branches. Rebase a
   clean lane before starting; never rebase or clean uncheckpointed changes.
2. **Claim a lane.** On the owning issue or pull request, state the outcome,
   affected paths/contracts, branch or PR, owner, dependencies, and next test.
   Parallel lanes should be path- or contract-disjoint. Coordinate before
   modifying another lane's files or assumptions.
3. **Preserve dirty work safely.** Treat unknown edits as someone else's work.
   Inventory and compare them, remove secrets and machine-only noise, then
   checkpoint each semantically unique change to a named remote branch and a
   linked Draft PR. Do not reset, delete, or overwrite the original until the
   remote checkpoint and disposition are verified.
4. **Push meaningful checkpoints promptly.** A resumable WIP includes its
   intent, current behavior, validation, blockers, and next action. A Draft PR
   means implementation is genuinely incomplete; it is not a waiting room for
   completed work.
5. **Coordinate in both directions.** Read issue comments, reviews, and related
   PR discussions before continuing. Answer or disposition new information and
   carry decisions back into code, documentation, tests, issue/PR bodies, and
   project status. Posting an update without consuming replies is not a sync.
6. **Merge finished work promptly.** Once scope is complete, required checks are
   green, review threads are resolved, and the branch is current, mark the PR
   ready and merge it. If another PR supersedes it, link both directions,
   confirm no unique work is lost, record the disposition, and close it.
7. **Leave a resumable handoff.** Before changing machines, owners, or tasks,
   push the branch and update the issue/PR with what changed, what was verified,
   what remains, safety state, blockers, and the exact next command or test.

## Issue body

An issue is the durable requirement, not a paste of a conversation. Keep its
body concise and current:

| Field | Required content |
|---|---|
| Outcome | User-visible need and why it matters |
| Scope | Owned behavior, interfaces, and explicit non-goals |
| Acceptance | Testable software, integration, and physical criteria |
| Coordination | Related issues/PRs, dependencies, claimed lane, and conflicts |
| Safety and privacy | Output, programming, credential, network, and data constraints |
| Evidence and state | Verified facts, current gaps, and truthful workflow status |
| Request provenance | Publication-safe request date/turn reference plus a short relevant excerpt or normalized requirement |

Keep complete prompt transcripts and JSONL audit data private. Never publish a
raw conversation, unrelated user text, credentials, local paths, hostnames,
device serials, or private logs. Quote only the minimum relevant, safely
redacted request text needed to preserve intent.

## Pull request body

A PR must let a different contributor review and finish it without private
context:

| Field | Required content |
|---|---|
| Outcome and linkage | User-visible result; `Closes`/`Relates to` issue links |
| Scope and contract | Changed surfaces plus compatibility or resource impact |
| Coordination | Base used, related WIP reviewed, lane/conflict disposition |
| Verification | Commands, concise results, and pending physical checks |
| Safety and rollback | Output state, migration/recovery path, and known risks |
| Generated content | Generator and tracked outputs changed together, or why none changed |
| Handoff | Remaining blocker and exact next action; omit when fully complete |

Update the body when scope or evidence changes. Put discussion decisions in a
resolvable review thread or an issue/PR comment and summarize their final
disposition in the body.

## Human-readable identities

- In prose, issue comments, PR bodies, tables, and handoffs, prefer issue/PR
  links, release/build names, or a linked short Git commit ID (normally 7-12
  characters).
- Do not fill human-readable text with full Git object IDs or long content
  digests. A short label plus a link is easier to compare and discuss.
- Keep full hashes where integrity actually requires them: signed manifests,
  checksum files, attestations, generated lock data, machine-readable API
  responses, and raw machine logs. Link to that evidence from the human summary.

## Generated artifacts

Generated source or embedded resources are committed only when the repository
declares them tracked. Rebuild them with the canonical generator and commit the
generator/input and resulting tracked output in the same PR; never hand-edit a
generated file. Build directories, executables, packages, backups, caches,
device captures, and local logs stay out of Git unless a documented release or
evidence workflow explicitly publishes a sanitized, deduplicated artifact.

Before merging, fetch again, reconcile relevant WIP in both directions, run the
repository-prescribed checks, and ensure GitHub contains the final decision and
handoff—not just the final diff.
