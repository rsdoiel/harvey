# Knowledge-base learning mode — Feature Request

> **2026-09-15: Filed.** Captured from a conversation with RSDOIEL about
> bringing harvey up to date with the `knowledge` module's v0.0.6 release and
> building the "learning mode" already named in `TODO.md`. No design/decide/
> plan cycle has been run yet. This document preserves the idea and the
> decisions already implied by existing code/docs — a starting point for that
> cycle, not a committed design.
>
> Decided (or already true) going into the filing conversation:
> - `knowledge` v0.0.4–v0.0.6 shipped three feature requests
>   (`wikilink-tagging`, `concept-tag-retrieval`, `narrative-documents` — all
>   in `../knowledge/CHANGES.md`) that each explicitly deferred their harvey-
>   side consumption. That consumption is this document's scope.
> - harvey's `go.mod` still pins `github.com/rsdoiel/knowledge v0.0.3` — three
>   minor releases behind. The `TODO.md` entry that blocked tagging a release
>   on `knowledge`'s wikilink/ingest instability (`observation 398`, project
>   `harvey`) is stale: that instability is what v0.0.4–v0.0.6 resolved.
> - "Learning mode" is not a new subsystem to invent from scratch — it is
>   harvey driving the **review lifecycle `knowledge` already built**
>   (`unsummarized` → `drafted` → `reviewed`, human-gated promotion, no
>   auto-promotion) via `kb document`/`kb record` verbs, using harvey's
>   **already-working multi-model dispatch** (`resolveDispatchTarget`,
>   `@mention`) to source the "drafted by a model" half of that lifecycle,
>   rather than a new routing mechanism.

## Motivation

`TODO.md` already names this work directly:

> Fully integrate the updates to the knowledge model, Harvey should support a
> learning mode that integrates both human, model and hybrid dialogs for
> evaluation, summarization, concept tagging and re-ingest for the knowledge
> base.

and, related:

> I've evolved the development methodology since last working on Harvey. The
> knowledge tool kb has been updated to reflect those changes. Harvey repo
> needs to be brought into alignment with the new practices around design
> decision reviews and recording them in a decisions directory...

Two things converge on the same gap. `knowledge`'s three most recent feature
requests (`wikilink-tagging`, `concept-tag-retrieval`, `narrative-documents`)
each ship a real capability but explicitly stop at the module boundary:

- `concept-tag-retrieval-feature-request.md`: "harvey's `recallKB` calls the
  new query as a first pass" is item 3 of its own proposal — never done.
- `narrative-documents-feature-request.md`: "Consuming any of this from
  harvey's `UnifiedMemory.Recall`, and building an eventual interactive/
  dialogic re-ingest mode in harvey, remain explicitly out of scope here."

So `knowledge` built the storage, the tagging, the retrieval query, and the
human-gated review lifecycle. harvey has consumed none of it — `recallKB`
still does a `strings.Contains` scan against a v0.0.3 API, and nothing in
harvey drives `kb document`/`kb record`'s review queue at all.

## What harvey assumes today

- **`go.mod:14`**: `github.com/rsdoiel/knowledge v0.0.3` — predates `records`
  (v0.0.4), `kb init`/portable record layout (v0.0.5), and concepts/
  documents/retrieval (v0.0.6) entirely. Per `CLAUDE.md`, harvey consumes
  `knowledge` via a `go.mod` `replace` pending its own publish, so this is a
  version bump plus a rebuild/retest, not a vendoring change.
- **`memory_unified.go:275-314`** (`UnifiedMemory.recallKB`) is the entire
  current KB retrieval path:
  ```go
  obs, err := kb.Observations(u.cfg.CurrentProjectID)
  ...
  for _, o := range obs {
      if qLower != "" && !strings.Contains(strings.ToLower(o.Body), qLower) {
          continue
      }
      out = append(out, UnifiedResult{Source: "kb", ..., Score: 0.5})
      if len(out) >= 5 { break }
  }
  ```
  Substring match on raw query text, scoped to `CurrentProjectID` only, fixed
  score 0.5, first-5-found — exactly the three limitations
  `concept-tag-retrieval-feature-request.md` already diagnosed, still
  present because the fix was never applied harvey-side. It also has no path
  to `records` or `documents` at all — both are invisible to `Recall`.
- **No document ingestion path exists in harvey.** Session recordings
  (`agents/sessions/*.spmd`, Fountain format) and hand-off notes
  (`agents/hand-off/*.spmd`) are exactly the format `kb document ingest`
  already supports (`github.com/rsdoiel/fountain`, the same module harvey
  depends on) — nothing currently offers them to `knowledge` as documents.
- **No review-queue surface exists in harvey.** `kb document review list`
  (or equivalent) and the `unsummarized`/`drafted`/`reviewed` lifecycle have
  no REPL command, skill, or dialog driving them from harvey's side.
- **`agents/skills/review-knowledge-base` and `update-knowledge-base`** only
  cover `project`/`observation`/`concept`/`source` — no `record` or
  `document` verbs, so even manual (non-learning-mode) use of the v0.0.4+
  surface has no skill support yet.
- **Decisions live only in `DECISIONS.md`**, a single hand-appended file —
  not `knowledge`'s `agents/decisions/` + `agents/projects/<project>/
  decisions/` record layout (DR-0021, `knowledge` v0.0.5), which `kb ingest`/
  `kb index` already know how to read. harvey's own decision history is
  therefore outside `knowledge.db` entirely, unlike every other project in
  the workspace.

## What is already fine — reuse points

- **`resolveDispatchTarget`** (`dispatch_target.go`) plus `@mention` parsing
  is harvey's existing, working way to send a task to a specific model
  distinct from the active chat model. This is the natural mechanism for
  "delegate simpler summarization passes to a module" — no new routing layer
  needed. Per past guidance in this workspace, an existing per-feature
  dispatch pattern should be reused rather than a new generic one designed
  alongside it.
- **`MemoryStore`'s confidence convention** (`memory_store.go`: 0.0–1.0,
  auto-archive at ≤0.2, `SetConfidence`) is already the exact shape
  `narrative-documents-feature-request.md` left open ("exact shape of the
  confidence value... same scale as harvey's memory confidence scores, for
  consistency across the workspace" — that document names harvey's
  convention directly as the target to match). Reuse it rather than invent a
  second confidence scale.
- **`knowledge` v0.0.6's `MatchConceptNames`/`RecallByConceptNames`**
  (`retrieval.go`) are exactly the embedder-free, whole-word, case-
  insensitive query `concept-tag-retrieval-feature-request.md` proposed —
  already implemented, already merged/exported, just unconsumed.
  `RecallByConceptNames` already spans `observation_concepts` and
  `record_concepts`, and widened to `documents` in the same release.
- **Decision records' `proposed`/human-promoted split** and documents'
  `unsummarized`/`drafted`/`reviewed` split are the same pattern knowledge
  already enforces at the module level ("a model may write a record but may
  not accept one"). Learning mode does not need to invent a new human-in-
  the-loop gate — it needs to drive the one that exists.
- **The three-silo REPL command namespace** (`/rag`, `/memory`, `/kb`,
  `DECISIONS.md` 2026-05-28) is precedent for where a new command surface
  should live — most likely `/kb` gains subcommands rather than a fourth
  namespace being invented.
- **`agents/skills/setup-knowledge-base`, `review-knowledge-base`,
  `update-knowledge-base`** are the existing skill-driven pattern for `kb`
  interaction from a Claude Code/harvey session — new record/document/
  learning-mode capability should extend these, not bypass them with raw
  `kb` shell-outs.

## Proposal

1. **Bump the dependency.** `go.mod` → `github.com/rsdoiel/knowledge v0.0.6`,
   `go build`/`go test`/`go test -race`, confirm nothing in harvey's own
   `knowledge.go`/`knowledge_merge.go` wrapper code (extracted *from*
   `knowledge` per `CHANGES.md` v0.0.1) assumed the old schema. Update the
   stale `TODO.md` "Blocked on `knowledge`" entry once confirmed clean.

2. **Rewire `recallKB`** to call `RecallByConceptNames`/`MatchConceptNames`
   as the primary path (concept-tag match against the current prompt, not
   project-scoped, no embedder), falling back to the existing substring-
   over-`CurrentProjectID`-observations behavior when no concept matches —
   exactly the sequencing `concept-tag-retrieval-feature-request.md`
   proposed. Widen the silo to surface `records` and `reviewed`-status
   `documents` (gist level first, matching `RecallByConceptNames`'s v0.0.6
   scope), not just `observations`.

3. **A learning-mode command/dialog** (working shape — `/kb learn`, or a
   `/learn` command; naming is an open question below) that surfaces the
   review queue (`unsummarized`/`drafted` records and document sections) and
   drives three dialog modes, matching `TODO.md`'s own wording:
   - **Human** — human writes or edits a summary/tag directly; equivalent to
     today's manual `kb record`/`kb document` authoring, just surfaced
     in-session instead of requiring a separate `kb` invocation.
   - **Model** — harvey dispatches a drafting pass via
     `resolveDispatchTarget`, writing back through `kb document draft`/
     equivalent, never touching `reviewed` status itself (matches
     `narrative-documents-feature-request.md`'s decided no-auto-promotion
     policy).
   - **Hybrid** — model drafts, harvey presents the draft plus its
     mechanical triage signals (size, concept-tag density, confidence) for
     the human to accept (`kb document review`/promote to `reviewed`), edit,
     or reject — the same accept/reject shape `autoExecuteReply`'s
     Y/n `promptAction` already uses elsewhere in harvey, reusable here
     rather than inventing a new confirmation UI.

4. **A session-to-document pipeline.** Offer harvey's own session
   recordings (`agents/sessions/*.spmd`) and hand-off notes
   (`agents/hand-off/*.spmd`) as `kb document ingest` candidates — Fountain
   format is already supported end-to-end. This gives learning mode a real,
   continuously-growing corpus to operate on without requiring the user to
   hand-feed files.

5. **Skill updates.** Extend `review-knowledge-base` to report on
   `record`/`document` state (counts by status, review-queue size) alongside
   today's project/observation/concept sections; extend
   `update-knowledge-base` with `record`/`document` `ACTION` values so manual
   use of the v0.0.4+ surface doesn't require raw `kb` invocations either.

6. **Decisions-directory realignment** (`TODO.md`'s second item). Adopt
   `knowledge`'s `agents/decisions/` (workspace-tier) and
   `agents/projects/harvey/decisions/` (project-tier) layout for harvey's own
   design decisions going forward, authored via `kb record new` and indexed
   via `kb ingest`/`kb index`, so harvey's own decision history becomes
   queryable through `kb search` the way every other project's already is.
   `DECISIONS.md` as a hand-appended log and `kb`-managed records are not
   mutually exclusive short-term — this item needs its own scoping pass
   separate from items 1-5 above, since it changes harvey's own process, not
   just its `knowledge` integration, and touches the "boundary between
   memory layers" question `TODO.md` flags as still open.

## Open questions for the design cycle

- **Where does learning mode live as a UX surface?** New top-level `/learn`
  command, or subcommands under the existing `/kb` namespace? The three-silo
  precedent (`/rag`, `/memory`, `/kb`) argues for extending `/kb` rather than
  adding a fourth namespace, but the dialog-driven, multi-turn nature of
  triage-then-review may not fit a single-shot slash command well. RSDOIEL
  frames learning mode primarily as an **interactive curation experience**,
  not a scripting surface — likely more than plain REPL commands: a menu for
  navigating the drafted/unsummarized queue, and an **external-editor
  handoff** (write the current summary or record body to a temp file,
  launch `$EDITOR`, read the result back on return) for genuinely flexible
  editing of multi-paragraph text, rather than editing summaries inline at a
  prompt. This UX shape should be settled before the command-surface
  question above, since it constrains what a `/learn`-vs-`/kb` choice needs
  to support in the first place.
- **Which model drafts?** A configured "small model" role in
  `agents/harvey.yaml` (paralleling the small/CPU-only-hardware motivation
  `concept-tag-retrieval-feature-request.md` was filed under), or whatever
  model is currently active? `narrative-documents-feature-request.md`
  explicitly flagged that the model drafting a summary at ingest time should
  not be the same small model doing retrieval-time matching — harvey's
  `resolveDispatchTarget`/`@mention` mechanism can express this, but which
  model gets the role isn't decided.
- **Synchronous dialog vs. batch queue.** Does a learning-mode session walk
  the review queue interactively turn-by-turn, or does a human kick off a
  batch drafting pass (harvey dispatches drafts for N queued items
  unattended) and review results later via the queue listing? The batch
  shape fits unattended/overnight small-model runs already established for
  chunked analysis (`/read-chunks`); the interactive shape fits ad hoc
  single-document review. Possibly both, but which ships first is open.
- **Confidence value shape.** `narrative-documents-feature-request.md` left
  open whether a drafting model's confidence is self-reported or heuristic
  (e.g. how much of a segment's own concept tags appear reflected in its
  summary). harvey's `MemoryStore.SetConfidence` convention gives a scale to
  reuse, not an answer to this — still needs deciding here, since harvey is
  the one that would compute or request it.
- **Scope of session-to-document ingestion.** Every `.spmd` automatically,
  or only ones a human explicitly marks worth keeping (mirroring
  `MemoryManifest.UnminedSessions()`'s existing opt-in-by-length gating,
  `sessionTurns >= 10`)? Ingesting everything risks flooding the review
  queue with low-value drafts before learning mode has proven itself useful.
- **Relationship to harvey's existing memory-mining loop.** `memory_miner.go`
  already runs a model-extraction-then-human-review cycle for experiential
  memories, independent of `knowledge`. Does learning mode unify with that
  pipeline (one review surface, two content types) or stay parallel to it
  (two review surfaces, each already working)? Building a second, slightly
  different review loop next to a working one is a real risk worth naming
  before design starts.
- **PDF, and other format gaps.** `narrative-documents-feature-request.md`
  left PDF ingestion unresolved workspace-wide (no extraction tooling
  exists). Not blocking for a first pass (Fountain/Markdown sessions and
  hand-off notes cover the session-to-document pipeline above), but worth
  naming so it isn't assumed solved.

## Related

- `TODO.md` — both "Action Items" entries this document responds to, and the
  now-stale "Blocked on `knowledge`" entry under "Update next."
- `../knowledge/CHANGES.md` v0.0.4–v0.0.6 — the released capability this
  integrates.
- `../knowledge/concept-tag-retrieval-feature-request.md`,
  `../knowledge/narrative-documents-feature-request.md`,
  `../knowledge/wikilink-tagging-feature-request.md` — the three requests
  whose harvey-side consumption this document scopes.
- `CLAUDE.md` — "Three-silo memory architecture" section; the existing
  `/rag`/`/memory`/`/kb` namespace precedent.
- `DECISIONS.md`, 2026-05-28 entry — the three-silo architecture decision
  this proposal extends rather than replaces.
- `memory_unified.go`, `dispatch_target.go`, `memory_store.go` — the code
  this proposal reuses or modifies.
- `agents/skills/review-knowledge-base/SKILL.md`,
  `agents/skills/update-knowledge-base/SKILL.md` — skills to extend.
