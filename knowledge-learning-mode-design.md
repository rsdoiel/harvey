# Knowledge-Base Learning Mode and v0.0.11 Integration — Design

**Status (2026-09-23):** Decisions drafted with recommendations, awaiting
RSDOIEL's review. Nothing implemented. Supersedes the "Proposal" section of
`knowledge-learning-mode-feature-request.md` (items 3–6), which predates
`knowledge` v0.0.10/v0.0.11 and does not know about the review loop harvey
already has (decision 2). Items 1–2 of that request are done.
See `knowledge-learning-mode-plan.md` for the phased build.

**References:**
- `knowledge-learning-mode-feature-request.md` — the request this resolves.
- `../knowledge/library-lift-design.md` — moves `cmd/kb`'s workflow logic
  into the `knowledge` package so harvey can import it. Prerequisite for
  decisions 6–8 here.
- `../knowledge/CHANGES.md` v0.0.10–v0.0.11 — `concept suggest`,
  `document tag`, `document fuzzy-tag`, `document frontmatter`,
  density-linking, `project rename` for record-owning projects.
- `TODO.md` — both Action Items this responds to.

## What changed since the feature request

1. `knowledge` shipped v0.0.10 and v0.0.11: a full corpus-improvement
   toolchain (suggest → tag / fuzzy-tag → ingest → review) that the request
   never anticipated. Learning mode's natural shape is now *driving that
   pipeline*, not just the review queue.
2. That toolchain lives in `knowledge/cmd/kb` (`package main`), unimportable.
   RSDOIEL chose to move it into the library rather than shell out.
3. **Harvey already has a review loop.** `Miner.reviewInteractive`
   (`memory_miner.go:239`) does accept / edit / skip over proposed items,
   with a near-duplicate check, and `editInEditor` (`:498`) already does the
   `$EDITOR` temp-file round trip the request listed as needing to be
   built. `promptAction` (`commands.go:3569`) is the shared Y/n/edit
   prompt. The request's open question "unify with the miner or stay
   parallel?" is answerable now that the code has been read.
4. `harvey.yaml` has **no model-role configuration** (grep for
   role/draft/summar/route found nothing). "Which model drafts" needs a
   new setting or reuse of `@mention`. Routing itself exists
   (`RouteRegistry`, `DispatchToEndpoint`, `resolveDispatchTarget`,
   `ParseAtMention`).
5. `harvey/go.mod` has **no `charmbracelet` dependency** yet; the
   termlib→Charm migration is still an unreviewed design brief.

## Decisions

1. **Harvey consumes the `knowledge` library; it does not shell out to
   `kb` for these features.** Skills (decision 9) still drive the `kb`
   binary — that is manual use, a different surface. Consequence: items 4
   and 3 are gated on `knowledge` v0.0.12.

2. **Learning mode composes existing pieces; it does not unify with the
   memory miner.** Extract exactly one shared primitive —
   `editTextInEditor(text string) (string, error)`, generalizing
   `editInEditor`'s `$EDITOR` round trip, which today is typed to
   `*MemoryDoc` — and leave the two review state machines separate. They
   review different objects with different rules (memory: near-duplicate
   check and confidence decay; summaries: `unsummarized→drafted→reviewed`,
   human-gated by `PromoteDocumentSummary`). Unifying them would refactor a
   working loop to save a small amount of code, against this workspace's
   standing preference for reusing an existing per-feature pattern over a
   general mechanism.

3. **UX v1 is line-oriented, under `/kb`, over a UI-agnostic core.**
   `/kb learn` walks the queue one item at a time —
   `[a]ccept [e]dit [r]edraft [s]kip [q]uit` — using `promptAction` and
   `editTextInEditor`. The logic lives in a `LearnSession` type that knows
   nothing about terminals. The bubbletea menu RSDOIEL described is a later
   renderer over the same type, taken up after the termlib→Charm migration
   lands. This settles the request's "UX shape before command surface"
   question: the surface is `/kb` (the three-silo namespace precedent), and
   the richer UI is deferred without blocking or forcing a rewrite.

4. **Draft and review are separate primitives; the walk is their
   composition.** `/kb learn draft [--limit N] [@model]` drafts unattended
   for queued items (batch, fits overnight small-model runs as with
   `/read-chunks`); `/kb learn review` is the interactive accept/edit loop;
   bare `/kb learn` drafts-if-missing then reviews per item. Answers the
   request's batch-vs-interactive question: not either/or, and the batch
   half is the simpler thing to build and test first.

5. **Three dialog modes map onto three code paths, none of which promote
   on its own.** *Human*: `editTextInEditor` → `DraftDocumentSummary(...,
   "human", nil)`. *Model*: dispatch via `DispatchToEndpoint` →
   `DraftDocumentSummary(..., modelName, &conf)`. *Hybrid*: model drafts,
   then the review loop presents draft plus triage signals (size, tag
   density, confidence). `PromoteDocumentSummary` is called from the accept
   keypath only. "A model may write but not accept" is `knowledge`'s
   existing rule; harvey enforces it by construction — no caller other than
   the accept handler holds a promote call.

6. **Drafting model: a named route from the existing registry; default is
   the active model.** Proposed optional `learn_model:` in `harvey.yaml`
   naming a route, overridable per call with `@name`. Kept distinct from
   any retrieval-time model, per `narrative-documents-feature-request.md`'s
   warning that the drafting model should not be the one doing retrieval
   matching. **Open:** new config key versus `@mention`-only (see Open
   questions).

7. **Confidence is heuristic, not self-reported.** A small model's stated
   confidence is not trustworthy. Proposed: the fraction of the section's
   own concept tags whose names appear in the draft, on the same 0.0–1.0
   scale as `MemoryStore`. The exact formula is left to the plan's red
   tests, with its worked example **hand-computed before implementation**
   — twice in v0.0.11 a design's own worked example was internally
   contradictory.

   **Revised 2026-09-24 (H5, on real data).** Linked tags are the wrong basis: 9 of 12 real sections have
   none. Confidence is now the fraction of the known concepts *mentioned in the source* that the draft
   names (nil when none). Since those concepts are also given to the model as hints, it measures concept
   coverage, not faithfulness. See the plan's H5 note.

8. **Session-to-document pipeline: opt-in, with the "already ingested?"
   state read from the KB itself.** Candidates are `.spmd` files under
   `agents/sessions/` meeting the miner's existing gate (`sessionTurns >=
   10`) plus every file in `agents/hand-off/`, minus any that
   `DocumentByPath` already finds. Current volume: 34 session files and 32
   hand-offs — small enough to list and confirm, not to auto-ingest.
   No new ledger is needed (contrast `Manifest`, which tracks *mining*);
   `documents.path` is the record. `ParseDocumentFile` already recognizes
   `.spmd` as Fountain (`documents.go:323`). **Check needed:** the stored
   path form (relative vs absolute) is now a cross-consumer contract — see
   the knowledge design's Risks.

9. **Concept curation pass: `/kb learn concepts`.** Runs
   `SuggestConcepts`, lets the human accept candidates (→ concept add),
   then previews `TagDocumentText`/`FuzzyTagDocumentText` edits as diffs
   and applies them through harvey's own permission-checked file write,
   then re-ingests. This depends on the library returning edited text and
   leaving the write to the caller (`library-lift-design.md` decision 2);
   otherwise a library call would rewrite user documents outside harvey's
   permission model.

10. **Skills: extend the canonical `agents/skills/` copies.**
    `review-knowledge-base` and `update-knowledge-base` already covered
    `record`/`document` (the feature request was stale on this); H2 added
    only the v0.0.11 verbs and output changes. Independent of the lift —
    skills drive `kb`.

11. **Decisions realignment: freeze `DECISIONS.md`, write new records with
    `kb record new`, no bulk backfill.** `DECISIONS.md` is 1,721 lines of
    history; converting it is large and mostly of archival value. New
    decisions go to `harvey/decisions/` (the `<repo>/decisions/` layout
    root `CLAUDE.md` documents; `kb record new`'s own default is
    `agents/projects/harvey/decisions/`, so pass `--dir harvey/decisions`).
    First record: the cutover itself. Selective backfill is on-demand for a
    decision that is still load-bearing. Independent of the lift.

## Sequencing

Two tracks, so nothing waits that doesn't have to:

| Track | Items | Depends on |
|---|---|---|
| **A — independent, can start now** | H0 bump `knowledge` to v0.0.11; skills (dec. 10); decisions cutover (dec. 11) | nothing |
| **B — gated on `knowledge` v0.0.12** | session-to-document pipeline (dec. 8); draft/review primitives and `/kb learn` (dec. 3–7); concept curation (dec. 9) | library lift L1 (pipeline, review), L2–L3 (curation) |

## Release scoping — RESOLVED 2026-09-24: v0.0.16 includes both tracks

On 2026-09-17 v0.0.16 was scoped as items 4–6 without the curation UI, and
choosing the library lift made item 4 depend on a new `knowledge` release. That
release shipped on 2026-09-24 (`knowledge` v0.0.12, tag `v0.0.12`, on the Go
module proxy), so nothing has to wait. RSDOIEL decided that **v0.0.16 includes
all of this work: Track A and Track B (H3–H7)**. The 2026-09-17 exclusion of the
learning-mode curation dialog is lifted for the line-oriented v1 (`/kb learn`,
H5 and H6). The bubbletea menu stays deferred (see below), and the separate
termlib→Charm evaluation that gates tagging v0.0.16 (`TODO.md`) is unchanged by
this decision.

## Deferred, explicitly

- The bubbletea curation menu (until termlib→Charm is decided).
- PDF ingestion (no extraction tooling workspace-wide).
- Scheduling unattended batch drafting; `/kb learn draft` is the primitive,
  cron-style orchestration is not designed here.
- Unifying the memory miner's review loop with learning mode (decision 2).

## Open questions for RSDOIEL

1. ~~**v0.0.16 scope**~~ — RESOLVED 2026-09-24: both tracks are in v0.0.16 (above).
2. **`learn_model:` config key** versus `@mention`-only for the drafting
   model.
3. **`harvey/agents/skills/` sync.** Skills exist in three places; the
   canonical `agents/skills/` copy is what decision 10 updates. The
   `harvey/agents/skills/` copies were left stale on purpose 2026-09-17.
   Update them too, or keep leaving them?
4. **Freeze versus backfill** for `DECISIONS.md` (decision 11).
5. **Session ingest gate** — the miner's `>= 10` turns, or a different
   threshold, or explicit-only.
