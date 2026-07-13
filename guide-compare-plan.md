# Harvey — Testing Whether Guides Still Earn Their Tokens (Direction E) — Implementation Plan

See [guide-compare-design.md](guide-compare-design.md) for the full audit
and the 2026-07-13 confirmed decisions. See [DECISIONS.md](DECISIONS.md)
for the finalized decision entry as this lands.

Resolved here, as implementation-level judgment calls rather than
user-facing policy decisions (the design doc's two "open questions"):

- **`variant` struct gains a third bool field**, not a new enum:
  `variant{name string, useRAG, useGuide bool}`. A small, additive change
  matching the existing struct's own shape rather than introducing new
  vocabulary for what's still just "which optional context gets added this
  call."
- **`writeReport`'s `GuideCompare` branch duplicates the `RagCompare`
  branch's structure rather than sharing a helper.** The two tables differ
  in header label ("RAG" vs "Guide") and one row (RAG has "Chunks
  injected," Guide has nothing analogous) — real but small differences.
  Extracting a shared helper now would mean parameterizing for a
  third-mode-that-doesn't-exist-yet's sake; per this repo's own "don't
  design for hypothetical future requirements" convention, duplicate now,
  reconsider only if a third compare axis is ever added.
- **Message construction is a new, directly-testable pure function**,
  `buildGuideMessages(guideText, promptText string, useGuide bool)
  []harvey.Message` — mirrors the existing `buildRAGContext`'s pattern of
  extracting a pure, unit-testable helper out of the dispatch loop, rather
  than leaving the "does this variant get a system message" logic inline
  where only an integration test (needing a live model) could reach it.
- **Flag validation stays inline in `main()`, uncovered by a unit test** —
  matching the existing precedent: `--rag-compare requires --rag-db`'s
  validation has no test today either. Not introducing a new testing
  standard for one flag pair when the established one (for the directly
  analogous existing flag) doesn't have it.

---

## Phase A — `--guide-compare` + `--guide-file` ✅ Complete 2026-07-13

See `DECISIONS.md` (2026-07-13 entry) for the final shape. No deviations
from this plan. **This completes Direction E's scoped increment** (the
mechanism only — no real experiment run, per the confirmed decision). The
plan below is kept as-written for the historical record.

**Goal.** A working, tested mechanism mirroring `--rag-compare`'s existing
shape, report format, and validation style — no real "guide vs sensor"
experiment run yet (per the design doc's confirmed scope), just the tool.

### Files to modify

| File | Change |
|---|---|
| `cmd/assay/main.go` | New flags: `guideCompare := flag.Bool("guide-compare", false, ...)`, `guideFile := flag.String("guide-file", "", ...)`. Validation: `--guide-compare` requires `--guide-file`; mutually exclusive with `--rag-compare` (both checked immediately after `flag.Parse()`, alongside the existing `--rag-compare`/`--rag-db` and `--llamafile`/`--llamacpp` checks) |
| `cmd/assay/main.go` | New pure function `buildGuideMessages(guideText, promptText string, useGuide bool) []harvey.Message` |
| `cmd/assay/main.go` | `variant` struct gains `useGuide bool`; the `variants` switch (`main.go:894-902`) gains a `case *guideCompare: variants = []variant{{"base", false, false}, {"guide", false, true}}` branch |
| `cmd/assay/main.go` | Load `guideFile`'s content once before the model loop (`guideText, _ := os.ReadFile(*guideFile)` when `*guideFile != ""`, trimmed) |
| `cmd/assay/main.go` | Dispatch loop (`main.go:937-939`): replace the hardcoded `[]harvey.Message{{Role: "user", Content: promptText}}` with `buildGuideMessages(string(guideText), promptText, v.useGuide)` |
| `cmd/assay/main.go` | `AssayResults` gains `GuideCompare bool`, `GuideFile string` (populated alongside `RagCompare`/`RagDB` when constructing `ar`) |
| `cmd/assay/main.go` | `writeReport`: extend the summary-table branch and per-prompt detail branch with a third case for `ar.GuideCompare`, mirroring the `ar.RagCompare` branches' structure (`Base pass | Guide pass | Δ | Avg tok/s` summary; per-check delta table; collapsed base/guide response `<details>` blocks — no "chunks injected" row) |
| `cmd/assay/main_test.go` | New tests: `TestBuildGuideMessages_WithGuide` (system + user messages, system content matches guide text), `TestBuildGuideMessages_WithoutGuide` (single user message, no system message), `TestBuildGuideMessages_EmptyGuideTextFallsBackToPlain` (guide requested but text is empty → single user message, no empty system message sent) |
| `cmd/assay/main_test.go` | New test: `TestWriteReport_GuideCompare_RendersDeltaTable` — construct an `AssayResults{GuideCompare: true, Results: [...]}` with one prompt's "base" and "guide" `PromptResult`s (differing `AutoPass`), a matching `Corpus`, call `writeReport` against a temp dir, read back `report.md`, and assert the summary table contains the expected pass counts and a `+1`/`-1`/`+0` delta — the first direct test of `writeReport`'s rendering logic at all (none exists today for `RagCompare` either, so this is new test infrastructure for the package, not just for this feature) |

### Approach

1. Write `buildGuideMessages`'s three tests first (TDD), confirm red,
   implement, confirm green — this function has no dependency on flag
   parsing or the dispatch loop and can be fully verified in isolation.
2. Write `TestWriteReport_GuideCompare_RendersDeltaTable` next, confirm red
   (the `GuideCompare` field/branch don't exist yet), implement the
   `AssayResults` field additions and `writeReport`'s new branch, confirm
   green.
3. Wire the flags, validation, `variant` struct change, and dispatch-loop
   call last, once both pieces independently work. `go build` confirms the
   wiring compiles; there is no unit test for the wiring itself (matching
   the existing `--rag-compare` precedent noted above).
4. Manual check (per this repo's `verify` practice, substituting for the
   live-model check this package's existing tests can't do): run
   `bin/assay --guide-compare --guide-file /tmp/test-guide.txt --models
   <a real local model> --category go` against a real Ollama/llamafile
   instance and confirm the produced `report.md` shows both variants' full
   responses and a sensible delta — this is the closest this increment
   gets to the "does a real comparison actually work end-to-end" question,
   since no guide/sensor pair exists yet to make the *result* meaningful.

### Acceptance criteria

- [ ] `go test ./...` (including `cmd/assay`) green.
- [ ] `go vet ./...` clean.
- [ ] `--guide-compare` without `--guide-file` exits with a clear error,
      matching the existing `--rag-compare`/`--rag-db` error's wording
      style.
- [ ] `--guide-compare` together with `--rag-compare` exits with a clear
      mutual-exclusivity error.
- [ ] The "base" variant of a `--guide-compare` run produces byte-identical
      dispatch behavior to today's default (non-compare) mode — proving
      the guide mechanism is purely additive, not a change to existing
      behavior.
- [ ] The rendered report's summary table and per-prompt detail section
      both distinguish base vs. guide results and compute a delta, matching
      `--rag-compare`'s existing report shape.

---

## Deferred / out of scope

- **A real first "does removing this guide hurt" experiment** — no
  guide/sensor pair exists yet in Harvey's own `HARVEY.md`/skills catalog
  and Direction A's shipped sensors to run one against meaningfully; see
  design doc. Not manufactured for this increment's sake.
- **Combined `--guide-compare` + `--rag-compare`** (a 2×2 variant matrix) —
  mutually exclusive for now; a real feature, not scoped here.
- **Re-deriving the guide text from Harvey's own system-prompt assembly**
  (`HARVEY.md` + skills catalog + `agentPreamble`) inside assay — would
  cross `cmd/assay`'s established self-contained boundary; the guide stays
  an externally-supplied opaque file.
