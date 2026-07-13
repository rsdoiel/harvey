# Harvey — Linting the Feedforward Side (Direction B) — Implementation Plan

See [feedforward-budget-design.md](feedforward-budget-design.md) for the
full rationale and the 2026-07-13 confirmed decisions. See
[DECISIONS.md](DECISIONS.md) for the finalized decision entries as each
phase lands.

Resolved here, as implementation-level judgment calls rather than
user-facing policy decisions:

- **`BudgetTracker` treats a `nil` receiver-target as "unconstrained."**
  Concretely: `ragAugment` and `injectOrChunk` both accept `*BudgetTracker`
  and must treat `nil` as "no budget check, behave exactly as today." This
  keeps every existing call site and test that doesn't care about budget
  cutoff behavior (e.g. `TestRAGAugment_RagOff`, `TestInjectFileContext_*`)
  unchanged, and isolates the new cutoff behavior to tests that
  deliberately construct a small `BudgetTracker`.
- **Token estimate for `Reserve` calls is the content alone**
  (`estimateTokens(chunk.Content)` / `estimateTokens(fileContent)`), not
  content-plus-header. The header/formatting overhead
  (`[%d] (source: %s)\n...\n\n` / `### File: %s\n\n...\n\n---\n\n`) is a
  handful of tokens per item — immaterial next to the content itself, and
  keeping the estimate to "the thing the caller actually decided to
  include or not" is simpler to test and reason about than also modeling
  formatting overhead.
- **`BudgetTracker` is constructed once per turn in `runChatTurn`**, before
  the `ragAugment` call, and the *same instance* is threaded through to
  `injectOrChunk` — this is what makes it a shared pool rather than two
  independent trackers. `runChatTurn` is the only production call site that
  constructs a real (non-nil) tracker; both consumer functions must accept
  one built anywhere (tests build their own with a small `Total` to
  exercise the cutoff path).

---

## Phase A — `BudgetTracker` + wire into `ragAugment` and `injectOrChunk` ✅ Complete 2026-07-13

See `DECISIONS.md` (2026-07-13 entry) for the final shape and one real
regression found and fixed during verification (an unknown-context-limit
case that would have silently zeroed the whole turn budget). The plan below
is kept as-written for the historical record.

**Goal.** One shared, per-turn token pool that `ragAugment` and
`injectOrChunk`'s two direct-inject branches consume from, so a prompt with
both RAG chunks and multiple file mentions can no longer silently exceed
`remainingContext(a)` the way three independent, uncoordinated checks allow
today.

### Files to modify

| File | Change |
|---|---|
| `budget_tracker.go` (new) | `BudgetTracker{Total, Used int}`, `NewBudgetTracker(total int) *BudgetTracker`, `(*BudgetTracker) Reserve(tokens int) bool`, `(*BudgetTracker) Remaining() int` |
| `budget_tracker_test.go` (new) | `TestBudgetTracker_ReserveFitsWithinTotal`, `TestBudgetTracker_ReserveRejectsWhenExceeded`, `TestBudgetTracker_ReserveExactlyAtTotal` (boundary: reserving exactly `Remaining()` succeeds), `TestBudgetTracker_RemainingReflectsUsed` |
| `rag_support.go` | `ragAugment(prompt string, tracker *BudgetTracker) (string, *RAGAugmentInfo)` — new second parameter. Iterate `relevant` chunks (already score-descending); for each, if `tracker != nil && !tracker.Reserve(estimateTokens(c.Content))`, stop the loop (later chunks are lower-scored — this is "keep the best chunks that fit"), and record how many were omitted. When ≥1 chunk was omitted this way, append `"\n[Note: %d lower-relevance chunk(s) omitted — context budget]\n"` to the context block before the `"---\n\n"` separator |
| `file_inject.go` | `injectOrChunk(ctx context.Context, prompt string, out io.Writer, tracker *BudgetTracker) string` — new fourth parameter. In the size-≤-cap branch and the oversized-but-fits-`remainingContext` branch: before appending to `header`, if `tracker != nil && !tracker.Reserve(estimateTokens(content))`, skip this file — print the existing dim-style human note (`"  (skipping "+tok+" — context budget)\n"`) to `out`, and track the skipped filename. After the loop, if any files were skipped this way, append `"[Note: N file(s) exceeded context budget and were not injected: a.go, b.go]\n\n"` to `header` before returning |
| `terminal.go` | `runChatTurn`: construct `tracker := NewBudgetTracker(remainingContext(a))` once, before the `ragAugment` call; pass it to both `a.ragAugment(input, tracker)` and `a.injectOrChunk(ctx, augmented, out, tracker)` |
| `terminal_test.go` | Update all 5 existing `ragAugment(...)` call sites (`TestRAGAugment_RagOff`, `TestRAGAugment_NilStore`, `TestRAGAugment_ReturnsInfo`, `TestRAGAugment_SkipPerPrompt`, plus the one more at line 811) to pass `nil` for `tracker` (per the resolved judgment call above — unconstrained, unchanged behavior) |
| `terminal_test.go` (new tests) | `TestRAGAugment_BudgetTracker_TrimsLowerScoredChunks` — ingest ≥2 chunks with distinguishable scores, construct a `BudgetTracker` whose `Total` fits only the top chunk, assert the augmented output contains the top chunk's content, omits the lower one, and contains the omission note |
| `file_inject_test.go` (existing call sites) | Update all `injectOrChunk(...)` call sites (`TestInjectOrChunk_SmallFile`, `TestInjectOrChunk_LargeFileChunkingDisabled`, `TestInjectOrChunk_LargeFileUserCancels`, `TestInjectOrChunk_LargeFileRunsChunking`, `TestInjectOrChunk_LargeFileFitsInBudget`) to pass `nil` for `tracker` |
| `file_inject_test.go` (new tests) | `TestInjectOrChunk_BudgetTracker_SkipsFileThatDoesNotFit` — two small files mentioned in one prompt, a `BudgetTracker` sized to fit only the first; assert the first is injected, the second is skipped, the omission note names the second file, and a dim skip line is written to `out` |

### Approach

1. Write `budget_tracker_test.go` first (TDD), confirm red (package doesn't
   exist), implement `budget_tracker.go`, confirm green. This part has no
   dependency on the other two files and can be fully done in isolation.
2. Update `ragAugment`'s signature and the 5 existing test call sites
   *before* adding the new trimming logic, so the signature-change commit
   and the behavior-change commit are separable in review even if landed
   together — confirm the updated-signature-only state still passes with
   `nil` tracker (behavior unchanged), then add the trimming test and
   implementation.
3. Same two-step approach for `injectOrChunk`.
4. Wire `runChatTurn` last, once both consumer functions independently work
   with a real tracker.
5. Manual check (not just unit tests, per this repo's `verify` practice):
   start a session with a small-context model, ingest enough RAG content to
   retrieve multiple chunks, and reference 2+ small files in one prompt —
   confirm the omission note(s) appear in what's sent to the model (visible
   via `--debug`/`DebugLog.LogRAGInject` or a temporary print) when the
   combined content is deliberately made to exceed a tight `remainingContext`.

### Acceptance criteria

- [ ] `go test ./...` green.
- [ ] `go vet ./...` clean (dogfooding Direction A's own sensor on this
      change).
- [ ] A prompt with more relevant RAG chunks than fit in the shared budget
      keeps the highest-scored chunks and adds a model-visible omission
      note — never silently drops content without saying so.
- [ ] A prompt mentioning multiple small files where not all fit in the
      shared budget injects as many as fit (highest-priority / first-seen
      first) and adds a model-visible omission note naming what didn't fit.
- [ ] RAG consuming most of the shared budget measurably reduces how much
      file content `injectOrChunk` can fit afterward in the same turn
      (proves the pool is actually shared, not two independent budgets).
- [ ] Every existing `ragAugment`/`injectOrChunk` test continues to pass
      unmodified in behavior (only the call-site signature changes, passing
      `nil`).

---

## Phase B — Sharpen `gofmt` `Check()`'s message ✅ Complete 2026-07-13

See `DECISIONS.md` (2026-07-13 entry) for the final shape and a real
correction found before implementing: the message is cause-neutral, not
"likely a syntax error" as originally proposed below — two existing tests
proved that wording would be wrong whenever `Config.AutoFormat = false`.
The plan below is kept as-written for the historical record.

**Goal.** Replace the one hardcoded, generic `FormatIssue.Message` in
`PipeExternalFormatter.Check` with one that names the realistic cause,
since this finding only fires post-`applyAutoFormat` (per
`computational-sensors-design.md`'s own reasoning about when `Check()` can
still find something after `Format()` already ran).

### Files to modify

| File | Change |
|---|---|
| `code_formatters.go` | `PipeExternalFormatter.Check` (line 152): change the `FormatIssue.Message` string from `"content differs from formatted output"` to name the likely cause (unparseable syntax) — see design doc for exact proposed wording |
| `code_formatters_test.go` | Update whatever test currently asserts the old literal message string (grep for `"content differs from formatted output"` before editing) to assert the new wording instead |
| `builtin_tools_test.go` | If any Direction-A test (`TestWriteFile_FormatCheckSensor_*`) asserts the old literal string in a `SensorEvent.Message`, update it too |

### Approach

1. Grep every `.go` file (not just `code_formatters_test.go`) for the exact
   old string first, so every assertion that depends on it is found before
   editing. **Correction found doing exactly this**: `code_formatters_test.go`
   has no assertion on the literal message at all — the two call sites that
   actually pin it are `TestWriteFile_FormatCheckSensor_reportsWhenAutoFormatOff`
   and `TestWriteFile_FormatCheckSensor_injectsWhenConfigured`
   (`builtin_tools_test.go:299`, `:321`), both asserting on the substring
   `"differs from formatted"`. Those two tests also revealed the cause-neutral
   wording requirement (see design doc correction, 2026-07-13): they
   deliberately set `Config.AutoFormat = false` and write valid-but-unformatted
   Go, a real case where the finding is **not** a syntax error.
2. Change the message to the cause-neutral wording confirmed in the design
   doc; update both `builtin_tools_test.go` substring assertions to match
   (e.g. `"not in canonical format"` or whatever substring the final wording
   settles on); confirm green.
3. `FileExternalFormatter`/`BuiltinFormatter` are explicitly **not**
   touched — neither is reachable from `runPostWriteSensors` today (Go-only
   scope, per `computational-sensors-design.md`), so rewriting their text
   would be speculative per this repo's own "don't design for hypothetical
   future requirements" convention.

### Acceptance criteria

- [ ] `go test ./...` green.
- [ ] The new message is cause-neutral: it doesn't assert "syntax error" as
      the cause when `Config.AutoFormat = false` is just as likely a cause,
      but still gives actionable guidance for both cases.
- [ ] `FileExternalFormatter`/`BuiltinFormatter` messages are byte-identical
      to before this change.

---

## Deferred / out of scope

- **Item 1 (content-aware guide selection)** — no path-scoped skill exists
  in Harvey's own catalog to gate yet; see design doc audit. Revisit if one
  is added.
- **Response-headroom reservation** (a dedicated constant subtracted from
  `remainingContext(a)` before it becomes `BudgetTracker.Total`) — confirmed
  out of scope for this increment; `small-model-budget-design.md`'s
  priority-table gap remains, tracked there, not solved here.
- **Session-start memory injection (`injectMemoryContext`) joining the same
  shared tracker** — it fires once per session, not per turn, so it isn't
  part of the same per-turn coordination gap Phase A closes. Revisit only
  if session-start memory budget and per-turn RAG/file budget are found to
  actually collide in practice.
- **The oversized-file interactive chunking branch of `injectOrChunk`** —
  already budget-aware via its own `remainingContext(a)` call and
  interactive/self-limiting; not wired into the shared tracker in this
  increment.
- **`go vet` message wording** — audited, already concrete and actionable;
  no change proposed (see design doc).
