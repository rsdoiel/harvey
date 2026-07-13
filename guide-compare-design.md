# Harvey — Testing Whether Guides Still Earn Their Tokens (Direction E) — Design

**Date**: 2026-07-13
**Status**: ✅ Implemented 2026-07-13 — see `DECISIONS.md` and
[guide-compare-plan.md](guide-compare-plan.md)
**Related**: [harness-engineering-exploration.md](harness-engineering-exploration.md)
Direction E, `cmd/assay/main.go` (existing `--rag-compare` mechanism this
mirrors), `computational-sensors-design.md` (Direction A, whose two shipped
sensors this direction checked against), `CLAUDE.md` (`cmd/assay/main.go`
"is otherwise self-contained" — a constraint this design respects)

---

## What's already there (found during scoping, not assumed)

### `--rag-compare`'s existing shape (the mechanism to mirror)

`cmd/assay/main.go` already runs a full A/B comparison for RAG: `--rag-compare`
(requires `--rag-db`) runs each prompt twice — `variant{"base", false}` and
`variant{"rag", true}` — records both as `PromptResult{Variant: "base"|"rag"}`,
and `writeReport` renders a summary table (`Base pass | RAG pass | Δ | Avg
tok/s`) plus a per-prompt, per-check delta table with collapsed base/RAG
response `<details>` blocks. This is a complete, working template — Direction
E's own open question ("what's the smallest assay change...") is best
answered by mirroring this shape as closely as possible, not inventing a new
one.

### Two gaps found that change the scope, not assumed from the exploration doc's phrasing

1. **Assay sends no system prompt at all, today.** Every call is
   `client.Chat(ctx, []harvey.Message{{Role: "user", Content: promptText}},
   &buf)` (`main.go:939`) — no `HARVEY.md`, no skills catalog, no system
   message of any kind. The exploration doc's Direction E framing ("once
   with a guide's prose included in the system prompt and once with it
   stripped") assumes a system-prompt path already exists in assay to
   toggle. It doesn't. This is genuinely new plumbing, not a flag flip —
   but a narrow one: see "Proposed shape" below.
2. **No guide/sensor pair currently exists in Harvey to actually test.**
   Checked `HARVEY.md`'s guide sections against Direction A's two shipped
   sensors (`gofmt` `Check()`, `go vet`): neither corresponds. Harvey's
   formatting is silent and automatic (`applyAutoFormat` runs on every
   `write_file`; `HARVEY.md` never tells the model to format manually, so
   there's no "format your code" prose to test removing), and nothing in
   `HARVEY.md` addresses `go vet`-style correctness bugs (unreachable code,
   format-string mismatches) either. The closest candidate — "Documentation
   conventions" (every exported symbol needs a `/** ... */` block) — has no
   sensor counterpart at all; nothing in Harvey checks doc-comment presence.
   **Confirmed with the user (2026-07-13): this increment builds the
   mechanism only.** No real "does removing this guide hurt" experiment is
   runnable today, and manufacturing one (writing new guide text to match
   an existing sensor, or a new sensor to match existing guide text purely
   to have a subject) would defeat the point — the experiment is only
   meaningful when both already exist independently of this exercise.

### The self-contained boundary (respected, not crossed)

`CLAUDE.md`: *"`cmd/assay/main.go` imports the root `harvey` package for
`RagStore` and `OllamaEmbedder`. It is otherwise self-contained."* Building
Harvey's own system-prompt assembly (`HARVEY.md` + skills catalog +
`agentPreamble`, per `terminal.go`'s startup sequence) into assay would
cross that boundary — assay would need to re-derive workspace/skill
discovery logic it currently has no reason to know about. Instead: the
"guide" is an **opaque, externally-supplied string** (loaded from a plain
text file), the same way a user would extract the specific prose fragment
they want to test removing into its own file before running the comparison.
This keeps assay's system-prompt input as data, not derived logic.

---

## Scope for this increment

Mirror `--rag-compare` as closely as possible:

- `--guide-compare` (bool) — run each prompt twice: once with the guide
  file's content as a system message, once without (identical to today's
  existing bare-prompt behavior — the "base" variant of guide-compare is
  literally unchanged current behavior).
- `--guide-file PATH` (string) — required when `--guide-compare` is set,
  mirroring `--rag-compare requires --rag-db`'s existing validation
  pattern. Plain text, no parsing — its full content becomes the system
  message for the "guide" variant.
- **Mutually exclusive with `--rag-compare`** for this increment, mirroring
  the existing `--llamafile`/`--llamacpp` mutual-exclusivity pattern.
  Combining both would need a 2×2 variant matrix (guide × RAG) the report
  format isn't shaped for yet — a real feature, but not this increment's.
- **No corpus-format changes.** The guide is a whole-run, system-prompt-level
  concern, not a per-prompt one — `Prompt`/`Checks` structs are untouched.
  This directly answers the exploration doc's own open question: the
  smallest change needs zero corpus rework, only new top-level flags plus
  new `PromptResult`/`AssayResults` fields, exactly mirroring how
  `RagCompare`/`RagChunks` were added alongside the corpus format, not
  inside it.

## Proposed shape

```go
// New flags, alongside the existing rag-* ones.
guideCompare := flag.Bool("guide-compare", false, "run each prompt twice (no-guide + guide) and show delta; requires --guide-file")
guideFile    := flag.String("guide-file", "", "path to a text file whose content becomes the system message for the 'guide' variant")
```

Validation (mirroring the existing `--rag-compare` requires `--rag-db`
check):

```go
if *guideCompare && *guideFile == "" {
    fmt.Fprintln(os.Stderr, "assay: --guide-compare requires --guide-file")
    os.Exit(1)
}
if *guideCompare && *ragCompare {
    fmt.Fprintln(os.Stderr, "assay: --guide-compare and --rag-compare are mutually exclusive")
    os.Exit(1)
}
```

Variant construction (mirroring the existing `variants` switch exactly,
`main.go:894-902`):

```go
case *guideCompare:
    variants = []variant{{"base", false}, {"guide", false}}
```

(Reusing the existing `variant{name string, useRAG bool}` struct's `useRAG`
field for RAG only — guide dispatch needs its own signal, since a prompt's
message list now depends on which variant is running. See plan for the
exact struct shape.)

Message construction — the one behavioral change to the actual dispatch
call: when the current variant is `"guide"`, prepend a system message built
from the loaded guide file content; every other variant (including
`--rag-compare`'s existing "base"/"rag") sends exactly what it sends today.

`PromptResult`/`AssayResults` gain `GuideCompare bool`, `GuideFile string`
(mirroring `RagCompare`/`RagDB`) — no new per-result field is needed beyond
the existing `Variant string`, since there's no "chunks injected" analog for
a guide.

`writeReport`'s existing `if ar.RagCompare { ... } else { ... }` two-way
branch becomes a three-way switch (RagCompare / GuideCompare / plain),
with the GuideCompare branch's table headed `Base pass | Guide pass | Δ |
Avg tok/s` — otherwise identical in structure to the RAG-compare table
(no "chunks injected" row, since there's nothing analogous to report).

---

## Decisions confirmed (2026-07-13)

1. **This increment builds the mechanism only** — `--guide-compare` +
   `--guide-file`, mirroring `--rag-compare`'s existing shape, report
   format, and validation style. No new sensor, no new guide prose written
   to manufacture a demonstration case.
2. **Mutually exclusive with `--rag-compare`** for this increment — a
   combined guide×RAG comparison is a real future feature, not scoped here.
3. **The guide is an opaque file**, not derived from Harvey's own
   `HARVEY.md`/skills assembly — respects `cmd/assay`'s established
   self-contained boundary.

## Open questions for the plan

- Exact `variant` struct shape — extend the existing `{name string, useRAG
  bool}` with a third field (`useGuide bool`), or introduce a small enum?
  Small implementation-level call, not a policy decision.
- Whether `writeReport`'s RAG-compare and guide-compare branches should
  share a helper (they're structurally near-identical, differing only in
  column headers and the "chunks injected" row) — worth checking during
  implementation whether extracting one is a net simplification or just
  adds a parameter list for two call sites.
- Noted, not decided: the most plausible **future** subject for a real
  first experiment, once one exists, is `HARVEY.md`'s "Documentation
  conventions" section (doc-comment presence) paired with a new
  doc-comment-presence sensor — neither exists yet, and building either
  purely to justify running this mechanism was explicitly rejected above.
  This is left as a pointer for whoever picks up that thread later, not a
  commitment.
