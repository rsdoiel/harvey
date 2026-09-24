# Knowledge-Base Learning Mode — Implementation Plan

See [knowledge-learning-mode-design.md](knowledge-learning-mode-design.md)
for rationale and decisions (DR-0001, DR-0002 in `decisions/`, both
`proposed`). TDD-first: `_test.go` written and confirmed red before each
implementation file. Commit only when RSDOIEL asks.

Two tracks, **both in v0.0.16** (decided 2026-09-24). **Track A** has no dependency. **Track B** was
gated on `knowledge` v0.0.12 (see
`../knowledge/library-lift-plan.md`, L1–L4).

---

## Track A

### H0 — Bump `knowledge` to v0.0.11

`go.mod` v0.0.9 → v0.0.11; `go build ./...`, `go test ./...`. No
consumer-facing API in v0.0.10/v0.0.11 is used by harvey today, so this
should be mechanical. `go test -race` does not run on this Pi
(ThreadSanitizer VMA limit); say so rather than claim it. Update `TODO.md`.

### H1 — Decisions cutover (DR-0002)

- `decisions/` already exists with DR-0001 and DR-0002 (`proposed`).
- Add a pointer at the top of `DECISIONS.md`: frozen as of this date, new
  decisions in `decisions/`.
- `kb ingest harvey/decisions` and `kb index harvey/decisions`; confirm with
  `kb search "learning mode"`.
- Update `CLAUDE.md`'s decision-log guidance in `harvey/CLAUDE.md` if it
  points only at `DECISIONS.md`.

### H2 — Skill updates

**DONE 2026-09-23.** Correction to this plan's original premise: the
canonical `agents/skills/` copies already had `record`/`document` actions and
the v0.0.10 verbs (the feature request's claim that they lacked them was
stale). The real gap was v0.0.11 only. Both skills are now 0.6.0 /
`kb_version` 0.0.11: `update-knowledge-base` gained `document-fuzzy-tag` and
`document-frontmatter` plus the `concept suggest` variants/near-existing/JSON
shape change; `review-knowledge-base` gained the near-existing block and a
review-queue breakdown. Commands were verified against the real `kb` 0.0.11
(read-only/`--dry-run`, or on a scratch file). The `harvey/agents/skills/`
copies were left stale, as recommended.

---

## Track B (was gated on `knowledge` v0.0.12, which shipped 2026-09-24; in v0.0.16)

### H3 — Bump to v0.0.12 and extract `editTextInEditor`

**DONE 2026-09-24** (uncommitted). `go.mod` was already at `knowledge` v0.0.12 (tag published
2026-09-24, on the module proxy); `go build`, `go vet` and `go test ./...` pass against it.
`editor.go` holds `editTextInEditor(text, ext string) (string, error)` and `findEditor` (moved
beside it); `editInEditor` in `memory_miner.go` is now `doc.Bytes()`, then `editTextInEditor(...,
".fountain")`, then `ParseMemoryDoc`. **Signature change from the plan:** an `ext` parameter, so a
Markdown summary and a Fountain memory each get the right syntax highlighting. 8 new tests in
`editor_test.go`, written red first against a do-nothing stub (5 failed on behavior; the two
`editInEditor` guards passed against the old code, so they are valid regression checks). A fake
`$EDITOR` shell script rewrites the file, records what it was given, or fails.
**Found, then fixed (next paragraph):** `findEditor` returns the whole `$EDITOR` string and `exec.Command` takes it as
one program name, so `EDITOR="code --wait"` (or `emacsclient -t`, `subl -w`) fails with `executable
file not found` (verified). It predates this change and also affects the other `findEditor` caller,
`memory_onboarding.go`. Learning mode's whole editing flow rests on `$EDITOR`, so it is worth fixing
before H5; the approach (split with `strings.Fields`, or run through the shell as git does) is a
small design choice.

**`$EDITOR` arguments FIXED 2026-09-24** (RSDOIEL chose `strings.Fields`). `editorCommand(editor, path)`
in `editor.go` splits the string on whitespace: first field is the program, the rest come before the
file. Both launch sites use it (`editTextInEditor` and `editTemplateRaw` in `memory_onboarding.go`,
which had its own copy of the launch code). A blank `$EDITOR` now falls back to micro/nano/vi instead
of being used as-is. Documented limitation: no quote handling, so a program path or argument that
contains a space is not supported. 5 new tests written red first against a behavior-preserving stub
(4 failed on behavior); verified live with a real program, `EDITOR="sed -i -e s/original/changed/"`.
Left alone: `editTemplateRaw` still duplicates the temp-file round trip and could call
`editTextInEditor`; a follow-up, not needed for learning mode.

`go.mod` bump. Extract `editTextInEditor(text string) (string, error)` from
`editInEditor` (`memory_miner.go:498`); `editInEditor` becomes a thin caller.
**Red test first:** round-trips text through a fake `$EDITOR` script;
existing miner tests pass unchanged.

### H4 — Session-to-document candidates

**DONE 2026-09-24** (uncommitted). `learn_ingest.go`: `LearnCandidate`, `learnCandidates`,
`parseSelection`, and `/kb learn ingest [--min-words N] [--all] [--dry-run]` (`kbLearn`), routed from
`cmdKB`, with a help line. 22 tests in `learn_ingest_test.go`, written red first against a stub (19
failed on behavior; 3 passed vacuously against the stub and became real guards).
**Design changed by real data (RSDOIEL chose, 2026-09-24):** the plan's "mirror the miner's 10-turn
gate" does not survive contact with the corpus. That gate is a live in-memory counter, not derivable
from a stored file; and on disk 31 of 34 sessions are under 200 words (median 24), none reaches 10
chat turns (the busiest has 5), while all 32 hand-offs are substantial (540 to 2,039 words). So:
**hand-offs are always offered; a session is offered if it has at least 200 words** (`--min-words`
overrides; 0 offers all). On the real files that is 3 sessions plus 32 hand-offs. Turn counts are
shown for information only (a chat turn is the `INT. HARVEY AND <USER> TALKING` scene heading).
**Path contract:** the stored path is relative to the working directory with forward slashes,
exactly what `kb document ingest FILE` stores from there; a file already stored under its absolute
path is also recognised, so another tool cannot cause a duplicate. Harvey never chdirs and its
workspace root comes from `Config.WorkDir`, so relative-to-cwd (not relative-to-root) is the form
that always matches what `kb` does. **Verified with the real binary, not just unit tests:** copied
the real hand-offs and sessions to a scratch workspace, ingested all 35 through `kbLearn`, then
re-ingested three of them with the release `kb` 0.0.12 from the same directory: all `skipped`, the
document count unchanged. One bad file does not stop the rest (reported as `failed`).
**Scale finding for H5:** ingesting all 35 queues **539 unsummarized sections** (5 to 25 per
document). Reviewing that by hand is not realistic, so batch drafting (`/kb learn draft`) is the
point of H5, and `/kb learn ingest` deliberately asks before ingesting and has no default-all.

New `learn_ingest.go`: list candidate `.spmd` files (sessions meeting the
turn gate, plus hand-offs) not yet found by `DocumentByPath`, and ingest
confirmed ones via `IngestDocument`. **Red tests first:** gate boundary at
9/10 turns; an already-ingested file is excluded; the stored path form
matches what `kb document ingest` would store (verify against the real `kb`
binary, not just the unit test). Command: `/kb learn ingest`.

### H5 — Draft and review primitives

**DONE 2026-09-24** (uncommitted). `learn.go` (terminal-free `LearnSession`, `Drafter`, `llmDrafter`,
`learnConfidence`), `learn_cmd.go` (`/kb learn draft`, `/kb learn review`, bare `/kb learn`), and
`learn_model` in `harvey.yaml` (RSDOIEL chose the config key). 54 new tests: 30 core, 21 commands, 3
config, all written red first (the core against a stub: 20 red on behavior, 5 vacuous guards).
`PromoteDocumentSummary` has exactly one call site, in `LearnSession.Accept`, and a test scans the source
to keep it that way. Nothing else promotes: a blank line, a bad key, `s`, `q` and end of input never do.
**Model resolution:** `@name` on the command, else `learn_model`, else the active model, through
`resolveDispatchTarget` (not `DispatchToEndpoint` as the plan wrote; it is the one that also handles a
local model switch and gives a `Restore`). **Deviations from the plan, each forced by evidence:**
1. **Empty sections.** A Markdown H1 becomes a section with an empty body. The first draft of the code
   asked the model to summarize nothing (which invites an invented summary) and burned the `--limit`;
   such a section can also never have a summary, so it would have blocked its document's gist forever.
   Now skipped, not counted toward the limit, and ignored when deciding a gist is ready.
2. **Gist.** A gist has no source text, so it is drafted from the section summaries, once every
   non-empty section has one. Reviewed last, so a human who edited a section can redraft the gist.
3. **`reject` dropped.** `knowledge` has no operation that un-drafts a summary, so there is nothing for
   it to call; skip covers it. Add one to `knowledge` (v0.0.13) if a real reject is wanted.
4. **Confidence basis changed on real data.** The plan's formula (fraction of the section's *linked* tags
   that the draft names) is nil almost everywhere: 9 of 12 real sections had no linked tag, because a link
   needs a wikilink or more than one mention. It is now the fraction of the known concepts *mentioned in the
   source* (what tag_density counts) that the draft names; nil when the source mentions none. Because those
   concepts are also given to the model as hints, it measures **concept coverage, not faithfulness**; on
   real runs it reads 1.00 where concepts are present and "unknown" where none are.
**Verified with a real model, not just fakes:** `olmo-3:7b-instruct` on Ollama, real hand-offs in a scratch
workspace (one run on a fresh KB, two on a copy of the real vocabulary): ingest, draft (about 55 s per item
on this Pi), review, accept; the accepted summary was then found by the release `kb search`, and a draft left
unaccepted was not. Draft quality is plausible: two clearly faithful, one weak (a conversation-header
section), one altered a name ("RSDoiel"); exactly what the human step is for. Scale: 25 items is about 23
minutes, so all 539 queued sections would be about 8 hours; hence the default limit of 25.
**Found, not fixed:** `kb search` fails on any hyphenated term (`map-reduce`, `records-portability`); see
`../knowledge/TODO.md`. It also affects `/kb search`.

New `learn.go` with a terminal-free `LearnSession`:
- Draft: dispatch through `DispatchToEndpoint`, then `DraftDocumentSummary`
  with `generatedBy` set to the model name and a heuristic confidence.
- Review: accept → `PromoteDocumentSummary`; edit → `editTextInEditor` then
  redraft as `"human"`; skip; reject.
- **Red tests first:** a fake endpoint; assert `PromoteDocumentSummary` is
  reachable from the accept path only (no draft path promotes).
- **Confidence formula:** hand-compute the worked example on paper *before*
  writing the test; do not trust the design's prose (the v0.0.11 lesson).

Commands: `/kb learn draft [--limit N] [@model]`, `/kb learn review`, bare
`/kb learn`. Config: `learn_model:` in `harvey.yaml` if approved (Open
question 2).

### H6 — Concept curation pass

**DONE 2026-09-24** (uncommitted). `learn_concepts.go` (`/kb learn concepts [--limit N]`), 27 tests in
`learn_concepts_test.go`, written red first against a stub (19 red on behavior, the rest guards). Three
mutations (no Fountain skip, no write-permission check, no restore after a failed re-ingest) each turn the
intended tests red, and a 500-case property test checks that applying `lineDiff`'s edits reproduces the
target text.

Flow: `SuggestConcepts` lists candidates (default 20) with mentions and spelling variants; the human picks
by number, range, `all` or `none`; each pick becomes a concept (`AddConcept`, no description); then for each
project document, `planConceptTagging` (`EligibleTagConcepts` + `TagDocumentText`, then
`FuzzyMatchConceptNames` + `FuzzyEligible` + `FuzzyTagDocumentText`, both restricted to the picked concepts)
produces the new text, `lineDiff` shows only the changed lines, and `promptAction` asks. A yes goes through
`applyConceptTagWrite`: `CheckWritePermission`, `Workspace.WriteFile`, `IngestDocument` on the stored path,
audit log and session record; if the re-ingest fails the original bytes are put back.

**Correction to the plan.** It asked for a red test "with `safe_mode` on". `safe_mode` limits which commands
`!` and `/run` may execute and does not gate file writes; the gate is the `permissions:` table
(`Agent.CheckWritePermission`). The denial tests use that. The two stay separate; if
`safe_mode` should also block curation writes, it is one extra check in `checkTagWritable`.
**Resolved 2026-09-24: keep them separate.**

Decisions taken while building it. **All confirmed by RSDOIEL 2026-09-24**, plus `safe_mode` stays separate
from the permissions table (no `safe_mode` check on curation writes):
- **Accepting a candidate creates the concept immediately**, before any file is shown. Declining every
  preview leaves the concepts and leaves every file and document row alone. This matches `kb concept add`
  and keeps "accept" meaning one thing; the alternative (create only when a file is written) would lose a
  concept the human explicitly chose.
- **Fountain documents are never rewritten.** Sessions are records of what happened, and harvey's replay
  and miner read `[[...]]` in Fountain as notes and file events. This excludes the whole `learn ingest`
  corpus, so today curation tags only Markdown documents; the concepts it creates still count in tag density
  for the Fountain ones. Reported as "Skipped N Fountain document(s)".
- **A path harvey may not write is refused before the prompt**, not after a yes.
- **Tagging changes source text**, so summaries of changed sections go stale on the re-ingest; the closing
  line reports how many.
- Only the picked concepts are linked or footnoted, not every known concept (`kb document tag` with no
  `--concept` links all of them).
- No `--dry-run`: each file's preview is the dry run, and nothing is written without a yes.

### H7 — Live verification

**DONE 2026-09-24** (uncommitted). Run with the built binary on a scratch workspace holding a **copy** of
`agents/knowledge.db` (documents rows removed), copies of three real Markdown files from `harvey/`
(`DECISIONS.md`, `developer_guide.md`, the design doc), two real hand-offs, and the sessions the run itself
recorded. The real database and real files were not touched.

What was checked, and what it agreed with:
- **Curation write.** Picked three real candidates (llamafile, ollama, sensor), wrote one file, declined
  one. `DECISIONS.md` written by Harvey is **byte-identical** to what release `kb document tag --concept
  llamafile,ollama,sensor` writes; the declined file is byte-identical to the original; disk sha256 equals
  the stored `documents.checksum` for all three documents.
- **Permission denial.** After `/permissions set notes/ read`, all three documents were refused before any
  prompt ("write permission denied; not touched"), files unchanged. Seven Fountain documents reported as
  skipped.
- **Ingest.** `/kb learn ingest` added 7 real Fountain files (5 sessions, 2 hand-offs).
- **Draft, accept, edit.** With olmo-3:7b-instruct, `/kb learn draft --limit 2` drafted two items; review
  accepted one and edited the other through `$EDITOR`. Release `kb document review list` shows both as
  `reviewed`; the edited one is recorded `generated_by = human`; the fixed `kb search` finds the accepted
  edited summary by a hyphenated term (`HUMAN-EDITED`), so the knowledge hyphen fix works through the real
  data path too.

Found and fixed during H7 (each red first): **end of input was read as "yes"** at the write prompt
(`promptAction` cannot tell Enter from Ctrl-D; `promptActionEOF` added, `/kb learn concepts` treats it as
quit; two older callers filed in `TODO.md`); **long changed lines hid the change** (real `DECISIONS.md`
paragraphs are ~1000 characters, so `lineDiff` now shows an excerpt around the change, both sides in the
same window).

Not a bug, but worth knowing: a `DECISIONS.md` section is 4-5k characters and one draft on this CPU-only Pi
took minutes, not the ~55 s the H5 run saw on hand-off sections. `--limit` and the ordering matter more than
they looked. Two harness slips of mine, not product bugs: restoring a database copy without deleting its
`-wal` file replays the earlier run's inserts, and piping several lines into Harvey lets a prompt's
buffered reader swallow the lines after it (a terminal delivers one line at a time).

Docs: `harvey.1.md` regenerated from the binary (as `make build` does, so its header moves from 0.0.15a to
0.0.16); `helptext.go`'s `/kb learn` lines and a new LEARNING MODE section in `KBHelpText`, with
`harvey-kb.7.md` regenerated from it; `CONFIGURATION.md` already documented `learn_model`. README not
regenerated.

---

## Not in this plan

The bubbletea menu, PDF ingestion, unattended scheduling, and unifying with
the memory miner (see the design's Deferred section).
