# Harvey — Linting the Feedforward Side (Direction B) — Design

**Date**: 2026-07-13
**Status**: ✅ Both phases implemented 2026-07-13 — see `DECISIONS.md` and
[feedforward-budget-plan.md](feedforward-budget-plan.md)
**Related**: [harness-engineering-exploration.md](harness-engineering-exploration.md)
Direction B, [small-model-budget-design.md](small-model-budget-design.md)
(Directions 1/2, which this document implements rather than re-designs),
[computational-sensors-design.md](computational-sensors-design.md) /
[computational-sensors-plan.md](computational-sensors-plan.md) (Direction A,
whose sensors Phase B of this document extends), `DECISIONS.md` (2026-07-12
entries this builds on)

---

## What's already there (found during scoping, not assumed)

Direction B lists three items. Checked each against current code before
proposing anything.

### Item 2 (BudgetTracker) — the gap is real and exactly as described

`runChatTurn` (`terminal.go:1091-1119`) runs, unconditionally, in this order:

1. `a.ragAugment(input)` — always requests top-5 chunks
   (`rag_support.go:626`), discards anything below `ragMinScore` (0.3), but
   never checks whether the *surviving* chunks fit in what's left of the
   context window.
2. `a.injectOrChunk(ctx, augmented, out)` (when `!a.toolsReliable()`) — for
   each path-like token in the prompt: files ≤ `maxInjectFileBytes` (16KB)
   are injected directly with **no budget check at all**, one file at a
   time, independent of how many other files are also being injected in the
   same prompt or how much RAG already consumed. Files over the cap *do*
   check `remainingContext(a)` before triggering chunked analysis — that
   path is already budget-aware.
3. `a.AddMessage("user", augmented)` — content is now in history.
4. *Only then* does `a.contextUsage()` / `formatContextUsage()` run, to
   decide whether to print a warning. By this point the content is already
   committed to history; the warning is diagnostic, not preventive.

This confirms `small-model-budget-design.md`'s own diagnosis
("Multi-file request → neither mechanism handles this... no budget check is
run across the aggregate") is still accurate. Two more things not called
out there, found while re-checking against current code:

- `injectMemoryContext` (`harvey.go:332-357`) already takes an explicit
  `budget` parameter and `UnifiedMemory.Recall` already enforces it
  (`memory_unified.go:93-99`) — but that budget is `Config.Ollama.ContextLength
  * Config.Memory.BudgetPct` (a fixed fraction of the *nominal* window,
  computed once at session start when `memoryContextPending` fires), not
  coordinated with `remainingContext(a)` or with what RAG/files consume
  later in the same or subsequent turns. It's already "budget-aware" in
  isolation, just not part of the same shared pool.
- The existing large-file chunking path (`injectOrChunk`'s oversized-file
  branch) already does real budget math: `budget := int(float64(rem) *
  a.Config.Chunking.Threshold)` where `rem := remainingContext(a)`. This
  document does not touch that branch's logic — only the two *direct-inject*
  branches (small files, and oversized-but-fits-anyway files) are missing a
  shared, cumulative check.

So the fix is narrower than "add budget-awareness from scratch": one
component (chunking) already has it in isolation; two others (RAG,
direct file inject) have none; and a fourth (memory) has its own
independent budget that isn't part of the same pool. The job is
coordination, not invention — matching `small-model-budget-design.md`
Direction 2's own framing.

### Item 3 (sensor messages as micro-guides) — narrower than the exploration doc implies

Checked both of Direction A's shipped sensors' actual message text:

- `go vet` findings (`runGoVet`, `builtin_tools.go:1462`) are already
  concrete and actionable as-is — `go vet`'s own output format is
  `file:line:col: message`, e.g. `main.go:10:2: Printf format %d has arg of
  wrong type string`. There is nothing generic to sharpen. `go vet` also has
  no first-class inline-suppress convention the way `staticcheck` has
  `//lint:ignore` — inventing an escape-hatch instruction here ("suppress
  and justify") would describe a mechanism that doesn't exist, not a real
  option available to the model. **No change proposed for `go vet`.**
- `Check()`/gofmt findings (`code_formatters.go:152`,
  `PipeExternalFormatter.Check`) return one hardcoded, generic message
  regardless of cause: `"content differs from formatted output"`.

  **Correction, found only by re-checking existing test coverage before
  implementing (not assumed):** the design's first draft claimed this finding
  "almost never fires after a successful `applyAutoFormat`" and that the
  realistic residual case is a syntax error. That's incomplete.
  `TestWriteFile_FormatCheckSensor_reportsWhenAutoFormatOff` /
  `_injectsWhenConfigured` (`builtin_tools_test.go:277-324`) deliberately set
  `Config.AutoFormat = false` and write syntactically-valid-but-unformatted
  Go (extra whitespace, no syntax error) — a real, tested, supported
  configuration where `runPostWriteSensors` still calls `Check()` (it always
  runs regardless of `AutoFormat`), and the finding is genuinely "auto-format
  is disabled," not a parse failure. `Check()` itself has no way to
  distinguish the two causes — it only ever compares raw content against
  `gofmt`'s canonical output. **Confirmed with the user (2026-07-13):** the
  message must be cause-neutral, covering both real cases, rather than
  asserting "likely a syntax error" as if it were the only or dominant one.

The exploration doc's example escape hatch ("you may raise this file's
max-lines threshold... if it's only slightly over") describes a
*threshold/style* sensor that doesn't exist yet in Harvey (`gocyclo`,
unused-param, max-lines — all still deferred per
`computational-sensors-design.md`'s own scope cut). Force-fitting an
override mechanism onto `go vet`/`gofmt` — sensors with no legitimate
override case — would encode a fake option. Scoping this item down: make
the one genuinely vague message (`gofmt` `Check()`'s) name its likely real
cause instead of restating the symptom. The full "escape hatch" pattern is
worth revisiting once a threshold-style sensor actually exists to need it —
noting that explicitly rather than skipping it silently.

### Item 1 (content-aware guide selection) — no concrete target in Harvey yet

The exploration doc's own example (`services -> clients + domain`,
gated to files under `./service/`) is from the user's external notes about
a different codebase, not an existing Harvey skill. Checked
`agents/skills/*/SKILL.md` (`compile-skill`, `fountain-analysis`,
`fountain-session`, `harvey-memory`, `multi-file`,
`review-knowledge-base`, `setup-knowledge-base`,
`update-knowledge-base`) — none are file-path-scoped the way the example
assumes; all are workflow/procedure skills invoked by name, not by touching
a particular directory. There is currently nothing in Harvey's skill
catalog for a content-aware selector to gate. Building the selection
mechanism now would be speculative infrastructure with no real skill to
apply it to — the same anti-pattern flagged in `CLAUDE.md`'s "don't design
for hypothetical future requirements."

**Not pursued in this increment.** Revisit if/when a path-scoped skill or
convention is added to Harvey's own skill catalog. This also sidesteps the
exploration doc's own open question about item 1 (risk of removing a guide
still needed for an unrelated, non-path-scoped reason) — there's nothing to
build prematurely wrong.

---

## Scope for this increment

Two phases, mirroring `computational-sensors-design.md`'s Phase A / Phase B
split:

- **Phase A — `BudgetTracker`**: a shared, per-turn token pool threaded
  through `ragAugment` and the two direct-inject branches of
  `injectOrChunk`, so each consumes from (and is aware of) what the other
  already spent, instead of three independent, uncoordinated checks.
- **Phase B — sharpen the one vague sensor message**: `gofmt` `Check()`'s
  generic string names its likely real cause (unparseable syntax) instead
  of restating the symptom.

Item 1 is out of scope entirely, per the audit above.

## Phase A — proposed shape

```go
// BudgetTracker is a per-turn, shared token pool. Callers Reserve() before
// adding content; a failed Reserve means "this doesn't fit — skip it and
// note that it was skipped," not an error.
type BudgetTracker struct {
    Total int
    Used  int
}

func NewBudgetTracker(total int) *BudgetTracker
func (b *BudgetTracker) Reserve(tokens int) bool // false when it would exceed Total
func (b *BudgetTracker) Remaining() int
```

Wiring, in `runChatTurn`:

```go
tracker := NewBudgetTracker(remainingContext(a))   // one per turn, before ragAugment
augmented, ragInfo := a.ragAugment(input, tracker)
...
augmented = a.injectOrChunk(ctx, augmented, out, tracker)
```

**`ragAugment`**: iterate `relevant` chunks in existing score-descending
order; for each, `tracker.Reserve(estimateTokens(chunk))` before appending
it to the context block; stop at the first chunk that doesn't fit (later
chunks are lower-scored, so this is "keep the best chunks that fit," not an
arbitrary cutoff). When at least one chunk was dropped this way, append a
short note to the context block (`[N lower-relevance chunk(s) omitted —
context budget]`) so the model knows retrieval found more than it received,
mirroring the existing "N additional files ... were not injected" pattern
`small-model-budget-design.md` Direction 1 proposed for files.

**`injectOrChunk`**: for the two direct-inject branches only (size ≤
`maxInjectFileBytes`, and oversized-but-fits-`remainingContext` today) —
`tracker.Reserve(estimateTokens(content))` before appending; on failure,
skip with the same per-file dim note style already used for the "too
large to inject" case, plus one summary line if any files were skipped
this way. The oversized-and-chunking branch (interactive
`promptChunkInstruction` → `RunChunkedAnalysis`) is unchanged — it already
does its own `remainingContext(a)` check and is interactive/self-limiting,
not part of this increment's scope.

Memory injection (`injectMemoryContext`) is **not** wired into the same
tracker in this increment — it fires once per session (not per turn,
gated by `memoryContextPending`), so it isn't part of the same
per-turn coordination gap the other two are. Noted as a candidate for a
later increment if session-start memory budget and per-turn RAG/file
budget are ever found to actually collide in practice.

## Phase B — proposed shape

`code_formatters.go:152` (`PipeExternalFormatter.Check`), change:

```go
return false, []FormatIssue{{Line: 1, Column: 0,
    Message: "content differs from formatted output", Severity: "info"}}
```

to a cause-neutral message covering both real cases (confirmed 2026-07-13):

```go
return false, []FormatIssue{{Line: 1, Column: 0,
    Message: "gofmt: content is not in canonical format — if auto-format is " +
        "disabled, run gofmt or write already-formatted content; if it was " +
        "enabled, a syntax error may have prevented it (check the file " +
        "compiles)", Severity: "info"}}
```

Only `PipeExternalFormatter` (the gofmt path, the only one wired into
`runPostWriteSensors` per Direction A's Go-only scope) changes.
`FileExternalFormatter`/`BuiltinFormatter` messages are untouched — neither
is currently reachable from `runPostWriteSensors`, so rewriting their text
would be speculative.

---

## Decisions confirmed (2026-07-13)

Confirmed with the user before writing the plan:

1. **Omitted-content notes are model-visible**, appended to the prompt text
   (not only a `SensorEvent`). Rationale: "some retrieved/requested content
   was withheld" is directly relevant to how the model should answer this
   turn — unlike a code-quality finding (Direction A's territory), this is
   about the completeness of what the model is about to reason over, so the
   default-off token-cost gating Direction A uses for low-severity findings
   doesn't apply the same way here.
2. **`BudgetTracker.Total = remainingContext(a)` as-is**, no additional
   response-headroom reservation in this increment. `remainingContext(a)`
   already carries its own 10% safety margin. A dedicated response-headroom
   constant (closing `small-model-budget-design.md`'s priority-table gap)
   is left as a separate, adjacent future change, not bundled into this one.
3. **Phase A / Phase B scope confirmed as written** — proceed to the plan
   doc. Item 1 (content-aware guide selection) remains out of scope per the
   audit above.

## Remaining open question for the plan

- Exact token-estimate granularity for `ragAugment`'s per-chunk `Reserve`
  call: `estimateTokens(chunk content)` alone, or the chunk content plus its
  `[%d] (source: %s)` header/formatting overhead? Small either way, but
  worth being precise about in the plan so tests assert the right number.
