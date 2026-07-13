# Harvey — Architecture & UX Decision Log

This file records significant architectural and UX decisions, their rationale, and known trade-offs. New decisions are added at the top. Each entry names the decision, the context that prompted it, the chosen approach, the rejected alternatives, and the consequences.

---

## 2026-07-13 — `assay --guide-compare`: the mechanism only, no real experiment run yet (Direction E)

**Context.** `guide-compare-design.md` / `guide-compare-plan.md`. Direction E's own framing ("once with a guide's prose included in the system prompt and once with it stripped but the corresponding sensor active") assumes `cmd/assay` already has a system-prompt path to toggle. Auditing `cmd/assay/main.go` in full found it doesn't: every dispatch is a bare `[]harvey.Message{{Role: "user", Content: promptText}}` (`main.go:939` pre-change) — no system message of any kind, ever. A second finding: checked `HARVEY.md`'s guide sections against Direction A's two shipped sensors (`gofmt` `Check()`, `go vet`) — neither corresponds. Harvey's formatting is silent/automatic (the guide never told the model to format manually, so there's no prose to test removing), and nothing in `HARVEY.md` addresses `go vet`-style correctness bugs. The closest candidate ("Documentation conventions," doc-comment presence) has no sensor at all. **Confirmed with the user (2026-07-13): this increment builds the mechanism only** — no real "does removing this guide hurt" experiment exists to run yet, and manufacturing one (new guide prose to match an existing sensor, or a new sensor to match existing prose, purely to have a subject) would defeat the point.

**Decision.** Added `--guide-compare` (bool) and `--guide-file PATH` (string) to `cmd/assay`, mirroring `--rag-compare`'s existing shape as closely as possible per the exploration doc's own open question about the smallest viable assay change: **no corpus-format changes at all** — the guide is a whole-run, system-prompt-level concern, not a per-prompt one, so `Prompt`/`Checks` are untouched; only new top-level flags plus new `AssayResults`/report fields, exactly mirroring how `RagCompare`/`RagChunks` were added. New pure function `buildGuideMessages(guideText, promptText string, useGuide bool) []harvey.Message` (mirroring `buildRAGContext`'s existing extracted-helper pattern) — the "base" variant is byte-identical to today's existing default dispatch; the "guide" variant prepends a system message built from `--guide-file`'s content. The guide file is treated as an **opaque, externally-supplied string**, never derived from Harvey's own `HARVEY.md`/skills-catalog assembly — respects `cmd/assay/main.go`'s established "otherwise self-contained" boundary (`CLAUDE.md`). `variant` struct gained a third `useGuide bool` field rather than a new enum. `--guide-compare` requires `--guide-file` and is mutually exclusive with `--rag-compare` (a combined guide×RAG comparison would need a 2×2 variant matrix the report format isn't shaped for — deferred, not this increment's). `writeReport` gained a third branch (mirroring `RagCompare`'s structure exactly, minus the "chunks injected" row, which has no guide analog) in both the summary-table and per-prompt-detail sections.

**Rejected.** Sharing a `writeReport` helper between the `RagCompare` and `GuideCompare` branches — the two differ in header label and one row; extracting a helper now would mean parameterizing for a third mode that doesn't exist yet, against this repo's own "don't design for hypothetical future requirements" convention. Re-deriving the guide text from Harvey's own system-prompt assembly inside assay — would cross `cmd/assay`'s self-contained boundary.

**Consequences.** TDD-first: `TestBuildGuideMessages_WithGuide`, `TestBuildGuideMessages_WithoutGuide`, `TestBuildGuideMessages_EmptyGuideTextFallsBackToPlain`, and `TestWriteReport_GuideCompare_RendersDeltaTable` — the last of these is the first direct test of `writeReport`'s rendering logic at all (none existed before this, for `RagCompare` either); all confirmed red then green. Flag validation (`--guide-compare` requires `--guide-file`; mutual exclusivity with `--rag-compare`) stays inline in `main()`, uncovered by a unit test — matching the existing precedent that `--rag-compare requires --rag-db`'s validation has no test either, not introducing a new testing standard for one flag pair when the directly analogous existing one doesn't have it. Manually verified end-to-end against a local fake OpenAI-compatible server: a 16-call run (8 prompts × base/guide) completed without error and produced a report with distinct Base/Guide sections; confirmed separately (by reading `chatInternal`/`harvestMessagesToAnyllm` in `anyllm_client.go`) that a `Message{Role: "system"}` is forwarded through the exact same, already-in-production code path Harvey's normal chat turns use for `Config.SystemPrompt` — not new, unverified SDK territory. `AssayHelpText` (`helptext.go`) and the derived `assay.1.md` updated and regenerated via `./bin/assay -help > assay.1.md`, matching the Makefile's own generation recipe. Full suite green except the same pre-existing, unrelated `TestCmdModelList_ShowsLlamafileEntries` failure noted in every entry above. `go vet ./...` clean.

**No real first experiment was run** — deferred per the confirmed scope. A plausible future subject, noted but not built: `HARVEY.md`'s "Documentation conventions" section paired with a new doc-comment-presence sensor, neither of which exists yet.

---

## 2026-07-13 — Shared `foldBackTurn` helper (Direction D, Phase D) — Direction D complete

**Context.** `subagent-dispatch-design.md` / `subagent-dispatch-plan.md` Phase D, the last of four. `cmdReadChunks` (`commands.go`) and `@mention` route dispatch (`terminal.go`) both duplicated the same two-call pattern — `a.AddMessage("user", ...)` followed by `a.AddMessage("assistant", ...)` — to summarize a bounded sub-dispatch back into the main conversation. `/plan next` deliberately does not fold back into history at all (a plan step is a side-effecting action, not a chat turn) and was correctly left untouched, per the design doc's original scope decision.

**Decision.** Added `Agent.foldBackTurn(userContent, assistantContent string)` (`harvey.go`, next to `AddMessage`) — two `AddMessage` calls. `cmdReadChunks` and the `@mention` route-dispatch branch both call it instead of inlining the pair independently.

**Rejected.** Nothing rejected — this phase was a pure mechanical extraction with no behavioral judgment calls, per the plan.

**Consequences.** No new tests needed, per the plan — this is a pure refactor of already-tested call sites. Confirmed every existing test that inspects `a.History` after a `/read-chunks` or `@mention` dispatch (`TestCmdReadChunks_AddsResultToHistory`, `TestAtMentionDispatch_landsInHistory`, plus the full `read_chunks_cmd_test.go`/`routing_test.go` suites) passes unmodified. Full suite green except the same pre-existing, unrelated `TestCmdModelList_ShowsLlamafileEntries` failure noted in every entry above. `go vet ./...` clean.

**This completes all four phases of `subagent-dispatch-design.md`/`subagent-dispatch-plan.md` (Direction D).** Summary across the full direction: fixed two real, previously-uncaught bugs found by auditing before designing (Bug 1: `@mention` in chunk-analysis instructions was cosmetic-only, contradicting documented design intent; Bug 2: `/plan next`'s post-step model restore read a config field the switch itself had already overwritten, so it never actually restored anything) via one shared, correctly-tested `resolveDispatchTarget` primitive (Phases A–B); then removed two further real duplications, each verified behavior-preserving by re-running every pre-existing affected test unmodified rather than assumed (Phases C–D). `@mention`'s local-model-switch fallthrough and chunk analysis's tool-free design were both explicitly audited and correctly left out of scope throughout, rather than folded in for the sake of a larger-looking consolidation.

Directions E (guide-vs-sensor token-cost comparison via `assay --guide-compare`) and F (harness-template skills) from `harness-engineering-exploration.md` remain open.

---

## 2026-07-13 — Shared `runBoundedTurn` tool-vs-plain-chat helper (Direction D, Phase C)

**Context.** `subagent-dispatch-design.md` / `subagent-dispatch-plan.md` Phase C. `cmdPlanNext` (`plan_cmd.go`) and `DispatchToEndpoint` (`routing.go`) each independently implemented the same branch: build a `ToolExecutor` and call `RunToolLoop` when tools are enabled and a registry exists, otherwise call `client.Chat` directly. Chunk analysis (`chunk_analyzer.go`) does not have this branch at all — confirmed during Direction D's initial audit that it's deliberately tool-free (each chunk is read-only text synthesis; no side-effecting tools wanted), so it's correctly excluded from this helper, not an oversight.

**Decision.** Added `bounded_turn.go`: `runBoundedTurn(ctx, client, registry, cfg, useTools, messages, dbg, w) (updatedHistory []Message, stats ChatStats, err error)`. Takes the `ToolExecutor` path when `useTools && registry != nil` (returning the tool-extended history, or the unchanged input messages if `RunToolLoop` itself falls back because the client isn't `ToolCapable`); otherwise a direct `client.Chat` call, returning `nil` for `updatedHistory` — the `nil`-vs-non-nil distinction is what `cmdPlanNext`'s `planStepHadErrors(updatedHistory)` depends on, and ranging over a `nil` slice is already a no-op matching the prior `else`-branch behavior exactly. Both call sites now delegate to it: `cmdPlanNext` passes `a.Tools`/`a.Config.ToolsEnabled` (identical gate to before); `DispatchToEndpoint` passes `registry`/`ep.Tools` (identical gate to before, `dbg` is `nil` since that function never had a `DebugLog` parameter to begin with).

**Rejected.** Extending `chunk_analyzer.go`'s `RunChunkedAnalysis` to also use `runBoundedTurn` — would add tool-calling to chunk analysis, a scope expansion the design doc explicitly ruled out, not a duplication this phase is meant to remove.

**Consequences.** TDD-first: `TestRunBoundedTurn_UsesToolLoopWhenEnabled`, `TestRunBoundedTurn_PlainChatWhenToolsDisabled`, `TestRunBoundedTurn_PlainChatReturnsNilHistory` (registry `nil` even with `useTools=true` still takes the plain-chat path) — confirmed red then green; no `ToolCapable` mock exists anywhere in the test suite (`RunToolLoop`'s actual tool-calling branch has never been unit-tested, only its non-`ToolCapable` fallback), so these tests exercise the fallback path, which is still sufficient to prove `runBoundedTurn` picks the right branch and returns the right nil-vs-non-nil history — introducing a full `ToolCapable` test double was judged out of scope for this extraction. Both call sites' full existing test suites (`TestCmdPlanNext_modelAnnotationSwitches` plus all `plan_test.go` tests; all `routing_test.go` tests including `TestDispatchToEndpoint_toolsDisabledFallsBackToChat`/`_toolsNilRegistryFallsBackToChat`) pass unmodified after the swap — direct evidence the extraction is behavior-preserving. Full suite green except the same pre-existing, unrelated `TestCmdModelList_ShowsLlamafileEntries` failure noted in every entry above. `go vet ./...` clean.

Phase D (shared fold-back helper) is next, per `subagent-dispatch-plan.md`.

---

## 2026-07-13 — `@mention` in chunk-analysis instructions actually dispatches now (Direction D, Phase B)

**Context.** `subagent-dispatch-design.md` / `subagent-dispatch-plan.md` Phase B — fixing Bug 1, confirmed during Direction D's audit: `cmdReadChunks` (`commands.go`) and `injectOrChunk`'s auto-chunk path (`file_inject.go`) both parsed an `@mention` out of the chunk instruction, but only used it to relabel `ChunkAnalysisParams.Model` for recording — the actual `RunChunkedAnalysis` call always ran against `a.Client` unconditionally, contradicting the documented design intent (`chunked-analysis-design.md`, `DECISIONS.md` 2026-07-05: "Harvey's existing `@mention` routing infrastructure routes each chunk analysis call to the named model").

**Decision.** Both call sites now call `resolveDispatchTarget(a, mentionName, out)` (added in Phase A) after parsing the mention out of the instruction, and pass `target.Client` — not `a.Client` — to `RunChunkedAnalysis`. `cmdReadChunks` (one file, one resolution) uses `defer target.Restore()`. `injectOrChunk` loops over potentially multiple path-like tokens in one prompt, each independently mentionable — using `defer` there would queue every iteration's restore until the whole function returns, leaving an earlier iteration's switched-in model still active while a later iteration's chunk analysis runs. Instead each iteration calls `restore()` explicitly right after its own `RunChunkedAnalysis` call (on both the success and error paths), keeping each token's resolve/dispatch/restore cycle fully self-contained within its own loop iteration. An unresolvable `@name` in either path prints the same "not found — using current model" warning style already used elsewhere in the codebase (`/plan next`, `@mention` routing) rather than silently falling back.

**Rejected.** Using `defer` inside `injectOrChunk`'s loop for symmetry with `cmdReadChunks` — rejected once the multi-file-per-prompt case was considered: it would incorrectly leave a still-switched model active across unrelated later iterations instead of restoring immediately after each one's own use.

**Consequences.** TDD-first: `TestCmdReadChunks_MentionDispatchesToNamedModel` and `TestInjectOrChunk_LargeFileMentionDispatchesToNamedModel`, both using `attemptModelSwitchOverride` (from Phase A) to make the resolved client's replies distinguishable from `a.Client`'s without spawning a real process. Both confirmed red against the pre-fix code (temporarily reverted, confirmed the exact "default reply" output the bug predicts, then restored) before confirming green against the fix — direct proof this was a real, previously-uncaught bug, not a hypothetical. Full suite green except the same pre-existing, unrelated `TestCmdModelList_ShowsLlamafileEntries` failure noted in every entry above. `go vet ./...` clean.

Phases C (shared tool-vs-plain execution helper) and D (shared fold-back helper) are next, per `subagent-dispatch-plan.md`.

---

## 2026-07-13 — `resolveDispatchTarget`: one correct model-resolution primitive, fixing a real `/plan next` restore bug (Direction D, Phase A)

**Context.** `subagent-dispatch-design.md` / `subagent-dispatch-plan.md` Phase A. Auditing `/plan next`, `/read-chunks`/`injectOrChunk`'s auto-chunk path, and `@mention` registry dispatch in full (per `harness-engineering-exploration.md` Direction D) found three independent ways of resolving "which model should run this bounded turn" — and confirmed two of them are actually broken, not just duplicated. **Bug 2** (this phase's target): `cmdPlanNext` (`plan_cmd.go`) captured `defaultModel := activeModelLabel(a)` before a step-scoped model switch, but its restore call ignored that captured value entirely and instead read `a.Config.Llamafile.Active` live, at restore time — a field `switchLlamafileModel` (`llamafile.go:220`) had already overwritten to the *step's own* model as part of performing the switch. Traced the exact mutation to confirm this wasn't a hypothetical: the restore call was, by construction, always trying to "switch to" the model that was already active, never the one that was active before the step. Confirmed with the user (2026-07-13) that this is in scope to fix as part of Direction D rather than filed separately, since the fix is exactly the shared model-resolution primitive this direction already needs.

**A second, deeper finding while designing the fix:** a cheap reference-swap restore (saving `a.Backend`/`a.Client` and reassigning them back) would not have worked either — `switchLlamafileModel` *stops* the previous backend process before starting the new one, so by restore time the pre-step process is already dead. A correct restore must capture the pre-step *identifying name* before switching and genuinely relaunch through the same mechanism, paying a second real cold-start cost — not something this fix can make cheaper, only correct (consistent with the Hardware Reality Check's framing that a model swap costs minutes each way).

**Decision.** Added `dispatch_target.go`: `DispatchTarget{Client LLMClient, Restore func()}` and `resolveDispatchTarget(a *Agent, name string, out io.Writer) (DispatchTarget, bool, error)`, resolving in order: (1) a registered route endpoint (`a.Routes.Lookup`) — an independent `LLMClient` via `clientForEndpoint`, never touching `a.Client`/`a.Backend`, `Restore` a no-op; (2) `name` already matches the active local model (case-insensitive, added as genuine no-op avoidance — auditing found the original code never actually had this despite the misleading `defaultModel` capture) — `Restore` a no-op; (3) a real local switch via `attemptModelSwitch`, capturing `prevLlamafileActive`/`prevOllamaModel` **before** switching so `Restore` relaunches the correct name afterward, not a post-switch-clobbered one. Added `Agent.attemptModelSwitchOverride func(name string, out io.Writer) (bool, error)` (nil-checked, same pattern as the existing `toolsReliableOverride`) so tests can simulate a successful local switch without spawning a real llamafile/llama.cpp process. `cmdPlanNext` now calls `resolveDispatchTarget` instead of its own broken switch/restore block, using the resolved `client` for the step's `Chat`/`RunToolLoop` call and calling `restore()` unconditionally afterward — including on the step-failure path, which the original code never restored on at all (a deliberate, disclosed small improvement: leaving the agent stuck on a temporary step model after a failed step is a footgun, not a behavior worth preserving).

**A third finding, while writing the regression test:** `TestCmdPlanNext_modelAnnotationSwitches` originally constructed its test `PlanStep` with `Model: "phi-mini"` set directly on the struct, title text left unannotated. Tracing why the rewritten test initially failed with *zero* switch calls (not merely "wrong restore name") found that `formatPlan` (called by `SavePlan`) only ever writes `s.Title` verbatim and never re-emits `Model` separately — so a `Model` field set without a `[model: NAME]` marker embedded in `Title` is silently dropped on the `SavePlan` → `LoadPlan` round trip `cmdPlanNext` performs. This means the *original* (pre-existing) test never exercised the switch path at all, for a completely different reason than its own stated one ("switch will fail, server not running") — it never even reached a real switch attempt. Fixed by embedding the marker in `Title` directly, matching the actual file format. Not treated as a bug to fix elsewhere (plan-file serialization is outside Direction D's scope) — noted here as a real gap found, not silently worked around.

**Rejected.** Filing the two dispatch bugs as separate, unrelated fixes — rejected per the confirmed decision; both live in the exact code this direction already consolidates. Skipping restore on the step-failure path (matching the original's silence there) — rejected as encoding an accident (never restoring on error) as if it were a deliberate choice, when no positive reason for it was found.

**Consequences.** TDD-first: `TestResolveDispatchTarget_RouteEndpoint_NoAgentMutation`, `TestResolveDispatchTarget_AlreadyActive_SkipsSwitch`, `TestResolveDispatchTarget_LocalSwitch_RestoreReturnsToOriginal` (the direct regression test for Bug 2 — simulates `switchLlamafileModel`'s exact mutation via the override and asserts `Restore()` switches back to the pre-step name, not the step's own), `TestResolveDispatchTarget_UnknownName_ReturnsNotFound` — all confirmed red then green. `TestCmdPlanNext_modelAnnotationSwitches` was rewritten to actually assert on switch/restore behavior instead of only "doesn't panic"; verified it fails against the pre-Phase-A `cmdPlanNext` code (temporarily reverted, confirmed red, restored) before confirming it passes against the fix. Full suite green except the same pre-existing, unrelated `TestCmdModelList_ShowsLlamafileEntries` failure noted in every entry above (untouched by this change). `go vet ./...` clean.

Phase B (wiring `resolveDispatchTarget` into `/read-chunks`/`injectOrChunk`, fixing Bug 1) is next, per `subagent-dispatch-plan.md`.

---

## 2026-07-13 — Cause-neutral `gofmt` `Check()` message (Direction B, Phase B)

**Context.** `feedforward-budget-design.md` / `feedforward-budget-plan.md` Phase B. The original plan proposed sharpening `PipeExternalFormatter.Check`'s generic `"content differs from formatted output"` message to name what the design doc assumed was the realistic residual cause: a syntax error `gofmt` can't parse, on the reasoning that `Check()` only fires after `applyAutoFormat` already ran and presumably fixed everything else.

**Correction found before implementing, by grepping every `.go` file for the exact string being replaced (not just the formatter's own test file).** Two existing Direction-A tests, `TestWriteFile_FormatCheckSensor_reportsWhenAutoFormatOff` and `TestWriteFile_FormatCheckSensor_injectsWhenConfigured` (`builtin_tools_test.go:277-324`), deliberately set `Config.AutoFormat = false` and write syntactically-valid-but-unformatted Go (extra whitespace, no syntax error) — a real, tested, supported configuration. `runPostWriteSensors` calls `Check()` unconditionally regardless of `AutoFormat`, so in that config the finding is genuinely "auto-format is disabled," not a parse failure. `Check()` has no way to distinguish the two causes internally — it only ever compares raw content against the formatter's canonical output. The original plan's proposed message ("likely a syntax error") would have been actively wrong in the `AutoFormat=false` case.

**Decision.** Confirmed with the user: use a cause-neutral message that names both real causes with actionable guidance for each, rather than asserting one as dominant. `code_formatters.go:152` (`PipeExternalFormatter.Check`) now returns: `"gofmt: content is not in canonical format — if auto-format is disabled, run gofmt or write already-formatted content; if it was enabled, a syntax error may have prevented it (check the file compiles)"`.

**Rejected.** The original "likely a syntax error" wording (shown wrong by the `AutoFormat=false` test path). Branching the message on `Config.AutoFormat` at the call site (passing the flag into `runPostWriteSensors`/`Check()` so each config gets a precise, single-cause message) — considered, but adds a parameter to thread through for a message-wording nicety; the cause-neutral single message covers both cases accurately without a signature change. Reverting to the fully generic original message — rejected since the cause-neutral version is still strictly more actionable (names the two remedies) even without pinpointing which applies.

**Consequences.** Updated the two `builtin_tools_test.go` assertions that pinned the old substring (`"differs from formatted"` → `"not in canonical format"`). Added `TestPipeExtFormatter_Check_ReportsCauseNeutralMessage` (`code_formatters_test.go`, using `tr a-z A-Z` as a formatter guaranteed to differ from lowercase input) — the first direct test of `PipeExternalFormatter.Check`'s "differs" message text; none existed before this change. `FileExternalFormatter.Check` (returns no message at all — file-mode check unsupported) and `BuiltinFormatter.Check`'s message are confirmed byte-identical to before, per scope. `go vet ./...` clean. Full suite green except the same pre-existing, unrelated `TestCmdModelList_ShowsLlamafileEntries` failure noted in every entry above (untouched by this change).

This completes both phases of `feedforward-budget-design.md`/`feedforward-budget-plan.md` (Direction B). Item 1 (content-aware guide selection) remains explicitly out of scope, per that document's own audit — no path-scoped skill exists yet in Harvey's own catalog to gate.

---

## 2026-07-13 — Shared `BudgetTracker` across `ragAugment` and `injectOrChunk` (Direction B, Phase A)

**Context.** `feedforward-budget-design.md` / `feedforward-budget-plan.md` Phase A. Confirmed real via code audit before designing: `runChatTurn` ran `ragAugment` (always requesting top-5 chunks, no budget check on the survivors) then `injectOrChunk` (each small file checked only against a flat 16KB `maxInjectFileBytes` cap, no aggregate check across multiple files in one prompt) — three independent, uncoordinated budget mechanisms (RAG, direct-file-inject, the pre-existing oversized-file chunking path), exactly as `small-model-budget-design.md` Direction 2 diagnosed. Three decisions were confirmed with the user before implementing: omitted-content notes are model-visible (not just a `SensorEvent`) since "some retrieved/requested content was withheld" is directly relevant to how the model should answer; `BudgetTracker.Total = remainingContext(a)` as-is, no additional response-headroom reservation this increment; and the overall Phase A/B scope as drafted.

**Decision.** Added `budget_tracker.go`: `BudgetTracker{Total, Used int}`, `NewBudgetTracker(total int) *BudgetTracker`, `(*BudgetTracker) Reserve(tokens int) bool`, `(*BudgetTracker) Remaining() int`. `ragAugment(prompt string, tracker *BudgetTracker)` gained a second parameter — chunks are kept in score-descending order until one fails `tracker.Reserve(estimateTokens(c.Content))`; that chunk and all lower-scored ones after it are omitted, and a `"[Note: %d lower-relevance chunk(s) omitted — context budget]"` line is appended to the context block. `injectOrChunk(ctx, prompt, out, tracker *BudgetTracker)` gained a fourth parameter — its two direct-inject branches (≤16KB, and oversized-but-fits-`remainingContext`) now `Reserve` before appending; a file that doesn't fit is skipped with the existing dim-style human note plus a `"[Note: %d file(s) exceeded context budget and were not injected: ...]"` line appended after the loop. `runChatTurn` constructs one `BudgetTracker` per turn, before the `ragAugment` call, and threads the *same instance* into `injectOrChunk` — this is what makes it a shared pool rather than two independent trackers. A `nil` tracker is treated as "unconstrained" by both consumers, preserving every pre-existing call site's behavior unchanged.

**A real regression found and fixed during verification, not just a test artifact.** `remainingContext(a)` returns 0 both when the context is genuinely full and when the limit is simply unknown (`effectiveContextLimit() <= 0`) — its own doc comment says so. Naively passing that straight into `NewBudgetTracker` meant any session with an unset/unprobed context length (e.g. `newTestAgent`'s default config, and plausibly some real llamafile-entry-less configs) got `Total = 0`, silently blocking *all* file/RAG injection, even a one-line file — caught by `TestRunChatTurn_CannotReadRetry_SkipsWhenAlreadyInjected` turning red after Phase A landed. Fixed by reusing the exact convention `injectOrChunk`'s pre-existing oversized-file branch already applies for the same ambiguity: fall back to a conservative 4096-token default only when the limit is positively unknown (`a.effectiveContextLimit() <= 0`), not when it's known and genuinely exhausted.

**Rejected.** Reserving additional response headroom on top of `remainingContext(a)` in this increment (would close `small-model-budget-design.md`'s "response headroom" priority-table gap, but was confirmed out of scope — a separate, adjacent change). Wiring `injectMemoryContext`'s session-start memory budget into the same shared tracker (fires once per session, not per turn — not the same coordination gap). Wiring the interactive chunked-analysis branch of `injectOrChunk` into the shared tracker (already budget-aware via its own `remainingContext(a)` call and interactive/self-limiting).

**Consequences.** TDD-first for `budget_tracker.go` (`TestBudgetTracker_ReserveFitsWithinTotal`, `TestBudgetTracker_ReserveRejectsWhenExceeded`, `TestBudgetTracker_ReserveExactlyAtTotal`, `TestBudgetTracker_RemainingReflectsUsed`, confirmed red then green) and for `ragAugment`'s trimming logic (`TestRAGAugment_BudgetTracker_TrimsLowerScoredChunks`, confirmed red — first for an unrelated reason, a same-source `Ingest` call overwriting the prior batch, fixed in the test, then red for the right reason, then green). One honest deviation: `injectOrChunk`'s budget-skip logic and its test (`TestInjectOrChunk_BudgetTracker_SkipsFileThatDoesNotFit`) were written in the same pass rather than strict red-then-green; verified after the fact by temporarily disabling the new `Reserve` checks and confirming the test fails for the expected reason, then restoring — a real check, but not the same order as the rest of this session's TDD-first work, noted rather than silently presented as identical. A new `TestBudgetTracker_SharedAcrossRAGAndFileInjection` proves the pool is genuinely shared (RAG consuming ~200 of a 220-token tracker leaves too little for a file that would otherwise fit) rather than two independent budgets that happen to run in sequence. Five existing `ragAugment` call sites and five existing `injectOrChunk` call sites were updated to pass `nil` (unconstrained), confirmed behavior-unchanged before the new trimming/skip logic was added. `go vet ./...` clean. Full suite green except the same pre-existing, unrelated `TestCmdModelList_ShowsLlamafileEntries` failure noted in every 2026-07-12 entry above (untouched by this change).

Phase B (sharpen `gofmt` `Check()`'s generic message) is next, per `feedforward-budget-plan.md`.

---

## 2026-07-12 — Add `go vet` as the first genuinely new sensor (Direction A, Phase B)

**Context.** `computational-sensors-design.md` / `computational-sensors-plan.md` Phase B, following Phase A's `Check()` wiring. `go vet` was chosen over `staticcheck`/`gocyclo`/unused-param/hunspell as the first genuinely new sensor because it only reports near-certain bugs by design — no style-nit noise floor to tune, unlike a general linter — making it the lowest-risk addition to introduce first (per the exploration doc's own warning against activating a wave of rules at once). The two judgment calls flagged as open in the design doc were resolved in the plan doc before implementing: `go vet` only runs when the workspace has a `go.mod`; a genuine `go vet` finding is distinguished from `go vet` itself failing to run (mid-edit compile error) via a `"# "`-prefixed package-header line in its output, which never appears before a real per-analyzer finding.

**Decision.** Added `runGoVet(a *Agent, absPath string) []SensorEvent` (`builtin_tools.go`), invoked from `runPostWriteSensors` for `.go` files. Findings are always both human-visible (`SensorEvent`) and appended to the tool-result string unconditionally — no config gate, unlike Phase A's `Check()` findings — since `go vet` has no low-severity tier to withhold tokens for. A `"# "`-prefixed line in `go vet`'s output causes the entire result to be suppressed (nil, no events, no appendix), treating a build failure as Harvey's own sensor failing to run rather than a code-quality finding, matching `applyAutoFormat`'s existing "errors are silently suppressed" policy. Timeout is a dedicated 15s constant (`goVetTimeout`), not `Config.Security.RunTimeout` (5 minutes, meant for user-driven `run_command`/`git_command` — inappropriate for something that runs synchronously on every `.go` write on Pi-class CPU).

**Rejected.** Reusing `Config.Security.RunTimeout` for `go vet`'s timeout, as the plan doc originally suggested ("reuse whatever timeout convention run_command already uses") — on reflection during implementation, the two have different risk profiles (an occasional user-invoked shell command vs. a sensor running on every single file write) and reusing a 5-minute default would let a hung/slow `go vet` invocation block every write for far too long.

**Consequences.** TDD-first: `TestWriteFile_GoVetSensor_reportsRealFinding` (a `Printf` format/argument mismatch — both `SensorEvent` and tool-result appendix), `TestWriteFile_GoVetSensor_suppressesCompileError` (an undefined-symbol compile error — neither), `TestWriteFile_GoVetSensor_skipsWithoutGoModule` (no `go.mod` → `go vet` never invoked). The two suppression-path tests were vacuously green before implementation (nothing ran at all yet) and were re-confirmed as meaningfully green afterward — real subprocess timings (1.32s / 0.07s) confirm `go vet` actually ran and the suppression logic was genuinely exercised, not just trivially passing. Full suite green except the same pre-existing, unrelated `TestCmdModelList_ShowsLlamafileEntries` failure noted in the earlier 2026-07-12 entries.

This completes both phases of `computational-sensors-design.md`/`computational-sensors-plan.md` (Direction A's first increment). `staticcheck`/`gocyclo`/unused-param/hunspell-style checks remain deliberately deferred to a follow-on increment, per that document's own scope decision.

---

## 2026-07-12 — Wire dormant `CodeFormatter.Check()` into a post-write sensor (Direction A, Phase A)

**Context.** `computational-sensors-design.md` / `computational-sensors-plan.md` Phase A, the first harness-engineering direction resumed after the prerequisite refactor cycle. Scoping found that `CodeFormatter.Check(content, filePath) (bool, []FormatIssue)` (`language_registry.go`) already exists, is implemented for Go via `gofmt` (`PipeExternalFormatter`), and is unit-tested — but nothing outside its own tests ever calls it. Separately, `applyAutoFormat` (`builtin_tools.go`) already runs the formatter automatically after every `write_file`, with its result note appended into the tool's own returned string (model-visible, costs tokens), gated by `Config.AutoFormat` (default `true`). An earlier draft of the design doc assumed a separate `edit_file` tool existed and needed the same wiring — checked, and Harvey has no `edit_file` tool at all, only whole-file `write_file`; corrected before implementing.

**Decision.** Added `runPostWriteSensors(a *Agent, relPath string) string` (`builtin_tools.go`), called from `write_file` after `applyAutoFormat`, running `Check()` against whatever content is currently on disk (i.e. after any auto-format rewrite already happened). Confirmed with the user beforehand: findings are human-visible by default (always a `SensorEvent` via a new `Agent.ActiveStatus StatusReporter` field, free, no token cost) and model-visible (appended to the tool-result string) only when `Config.SensorInjectFormatFindings` (default `false`, surfaced in `harvey.yaml` as `sensor_inject_format_findings`) is set. Rationale for the default: after a successful auto-format, `Check()` on a Go file should almost never find anything left to report; the residual case (a syntax error `gofmt` can't parse) is rare enough not to warrant spending tokens on by default, but still cheap and useful to surface to a human.

`Agent.ActiveStatus` is new plumbing needed because tool handlers (closures created in `RegisterBuiltinTools(r, a)`, capturing `*Agent`) had no path to the turn's live `StatusReporter` — that lived only on `ToolExecutor.Status`, which handler closures never see, and `ToolExecutor` itself has no `*Agent` reference. Rather than changing `ToolHandler`'s signature (which would touch every registered tool), `ActiveStatus` is set alongside the two existing `ex.Status = sp` / `ex.Status = retrySp` assignments in `terminal.go` and cleared to `nil` once each spinner stops.

**Rejected.** Changing `ToolHandler`'s signature to thread a reporter through explicitly — correct in principle but a much larger, more invasive change for this increment; `Agent.ActiveStatus` achieves the same reporting path with two call-site changes instead of touching every tool.

**Consequences.** TDD-first: `TestWriteFile_FormatCheckSensor_reportsWhenAutoFormatOff`, `TestWriteFile_FormatCheckSensor_injectsWhenConfigured`, `TestWriteFile_FormatCheckSensor_silentWhenAlreadyFormatted` (using a new `captureStatus` `StatusReporter` test double), confirmed red, then green. Full suite green except the same pre-existing, unrelated `TestCmdModelList_ShowsLlamafileEntries` failure noted in the earlier 2026-07-12 entries. Phase B (`go vet`, a genuinely new sensor) is next.

---

## 2026-07-12 — Unify sensor/status reporting into one SensorEvent shape

**Context.** `harness-prerequisite-refactor-design.md` / `harness-prerequisite-refactor-plan.md` Phase C (Item 3). Three disconnected mechanisms reported what are all, conceptually, sensor signals: `tool_executor.go`'s `e.Status.UpdateStatus(...)` calls (transient, rendered on the spinner's status line), `groundingCheck`'s result (printed directly to `out` after the spinner had already stopped), and the prose-tool-call "only tool-call syntax" warning (also printed directly, independently). This was flagged in `harness-engineering-exploration.md` as the direct prerequisite for that document's Direction A (new computational sensors need somewhere to report) and Direction C (the sensor-sidecar pattern).

**Decision.** Added `sensor_event.go`: `SensorClass` (`Computational`/`Inferential`, per the harness-engineering framework's computational/inferential axis) and `SensorEvent{Kind, Message, Class}`, plus `reportSensorEvent(out io.Writer, ev SensorEvent)` — the one place the "print a warning line" formatting now exists, used by both `groundingCheck`'s call site and the prose-tool-call-syntax warning in `terminal.go`. `StatusReporter` (`tool_executor.go`) gained `ReportSensor(ev SensorEvent)` alongside the existing `UpdateStatus(msg string)`; `*Spinner` implements it as a thin delegate (`s.UpdateStatus(ev.Message)`) — no rendering change, since the spinner's live status line doesn't yet do anything with `Kind`/`Class`. All three original signals now construct a `SensorEvent`: the three `ExecuteToolCalls` call sites (`tool_executor.go`) via `ReportSensor`, and the two `terminal.go` call sites via `reportSensorEvent`. Every event constructed today is `Computational` — each is a deterministic check over already-known content (a string match, a tool-dispatch result), not an LLM judgment; `Inferential` exists for a future sensor (e.g. an LLM-based review) with nothing emitting it yet.

Scope was deliberately limited to this plumbing, per the plan: no UI redesign (the sensor-sidecar two-view pattern from `harness-engineering-exploration.md` Direction C remains unbuilt follow-on design work), and the spinner's rendering is unchanged.

**Rejected.** Rendering `Class`/`Kind` differently now (e.g. distinct symbols per kind) — premature without a second, non-`Computational` sensor to actually distinguish from; deferred to whenever Direction A's tooling introduces a real second sensor kind.

**Consequences.** Neither `groundingCheck`'s nor the prose-tool-call-syntax warning's *printed output* had prior test coverage (only `groundingCheck`'s own return value was tested, in `grounding_test.go`) — new tests were written first: `TestReportSensorEvent_writesMessage`, `TestReportSensorEvent_matchesPreRefactorGroundingFormat` (exact-byte characterization of the grounding call site's pre-refactor format), `TestSpinner_ReportSensor_delegatesToUpdateStatus`. One honest caveat: the grounding call site's output is byte-for-byte unchanged (`reportSensorEvent`'s format was matched to it exactly), but the prose-tool-call-syntax warning's output is only *visually* identical, not byte-identical — its pre-refactor code had the space after "⚠" outside the ANSI color wrap (`yellow("  ⚠")+" "+text`) while `reportSensorEvent` puts it inside (`yellow("  ⚠ ")+text`); both render as the same visible text in a terminal, since ANSI color codes don't affect a plain space's appearance, but the two original call sites already disagreed with each other at the byte level before this change, so a single shared formatter could not be byte-identical to both simultaneously — matched to the grounding style since that had the more precise characterization test. Full package suite green except the same pre-existing, unrelated `TestCmdModelList_ShowsLlamafileEntries` failure noted in the other 2026-07-12 entries.

This completes all three items of `harness-prerequisite-refactor-design.md`/`harness-prerequisite-refactor-plan.md` (Item 4 remains owned entirely by `small-model-budget-design.md`). Harvey is now ready to resume the `harness-engineering-exploration.md` directions this cycle was a prerequisite for.

---

## 2026-07-12 — Unify token-budget/context-percentage reporting

**Context.** `harness-prerequisite-refactor-design.md` / `harness-prerequisite-refactor-plan.md` Phase B (Item 1). The same "estimate tokens used, compute %, pick a warning tier" logic was written three times: the turn-time warning's Ollama branch (`terminal.go`, exact count via `CountTokens`), its llamafile/llama.cpp branch (estimated), and `/status`'s `cmdStatus` (a third, near-identical copy branching on provider again). Auditing all three side by side surfaced three real discrepancies, not just duplicated code:

1. The turn-time Ollama branch read `limit` directly from `a.Config.Ollama.ContextLength`, while `/status` and the llamafile branch both used `effectiveContextLimit()` — which additionally falls back to a probed llamafile-entry/`ModelCache` value when `Ollama.ContextLength` is unset in `harvey.yaml`. This meant the live overflow warning could silently never fire for a session where `/status` would correctly report high usage.
2. The ≥100%-full message text differed by backend for no documented reason: Ollama said "reply may be truncated"; llamafile said "try /clear or switch to a model with larger context."
3. The estimate methodology itself differed: the turn-time llamafile branch summed `estimateTokens` per message (`for _, m := range a.History { used += estimateTokens(m.Content) }`), while `/status`'s estimate branch called `estimateTokens` once on the whole concatenated history (`estimateTokens(HistoryText(a.History))`). Since `estimateTokens` floors at a minimum of 1 token per call, per-message summation over-counts whenever history has many short/compacted messages.

**Decision.** All three, confirmed with the user before implementing:

1. Use `effectiveContextLimit()` as the limit source everywhere, including the Ollama turn-time path — fixes the silent-gap case.
2. Unify the ≥100% message to one combined line: `"Context full: %s%d / %d tokens (%d%%) — reply may be truncated; try /clear or switch to a model with larger context"`.
3. Use the whole-history-string estimate (`estimateTokens(HistoryText(a.History))`) everywhere, matching `/status`'s pre-existing (more accurate) approach rather than per-message summation.

Implemented as `Agent.contextUsage() (used, limit int, exact bool)` and `formatContextUsage(used, limit int, exact bool) (contextTier, string)` in `context_estimator.go`. `runChatTurn` (`terminal.go`) and `cmdStatus` (`commands.go`) both call `contextUsage()`; `runChatTurn` additionally calls `formatContextUsage()` and gates printing on tier (≥80%/≥100%), while `cmdStatus` keeps its own always-shown "Tokens: ..." line format using just the raw `used`/`limit`/`exact` values.

**Rejected.** Preserving today's three-way divergence behind explicit per-caller parameters — considered for each of the three discrepancies, but none had a positive rationale on inspection; all three read as unintentional drift from being implemented separately at different times rather than deliberate design choices.

**Consequences.** None of `contextUsage()`/`formatContextUsage()` had direct unit tests before this change (the logic was only reachable indirectly through `runChatTurn`/`cmdStatus`); characterization/new-behavior tests were written first, confirmed red, then made green by the implementation: `TestContextUsage_estimatedPath`, `TestContextUsage_ollamaExactPath`, `TestContextUsage_ollamaUsesEffectiveContextLimit` (regression test for discrepancy 1), `TestFormatContextUsage_belowWarnThreshold`, `TestFormatContextUsage_warnTierExact`, `TestFormatContextUsage_warnTierEstimated`, `TestFormatContextUsage_fullTier`, `TestFormatContextUsage_unknownLimit`. Full package suite green except the same pre-existing, unrelated `TestCmdModelList_ShowsLlamafileEntries` failure noted in the 2026-07-12 tool-call-dispatch entry above (untouched by this change either).

---

## 2026-07-12 — Unify tool-call dispatch-and-report (`tryExecuteApertusToolCalls` / `tryExecuteProseToolCalls`)

**Context.** `harness-prerequisite-refactor-design.md` Item 2. `tryExecuteApertusToolCalls` (Apertus-native `<SPECIAL_71>...<SPECIAL_72>` syntax) and `tryExecuteProseToolCalls` (fenced JSON blocks, used by small models like qwen2.5/llama3.2) were near-byte-identical: same `NewToolExecutor` setup, same `ExecuteToolCalls` call, same per-result reporting loop — differing only in which parser produced `calls`. Auditing them while scoping the pre-harness-engineering refactor cycle surfaced a real behavioral inconsistency, not just duplicated code: only the prose path printed an immediate "⚠ Unknown tool(s): ... Available tools: ..." terminal warning (`tool_executor.go:316-324` in the pre-refactor code); the Apertus path silently omitted it, even though both paths fed their `unknownNames` into the same downstream self-correction message injected into history (`terminal.go:1367-1378`). No code comment or design doc justified the asymmetry — it reads as Apertus support having been added after prose support without carrying the same reporting over.

**Decision.** Extracted the shared execute-and-report step into `executeAndReportToolCalls(a *Agent, calls []anyllm.ToolCall, out io.Writer) (dispatched bool, unknownNames []string)` (`tool_executor.go`). Both `tryExecuteApertusToolCalls` and `tryExecuteProseToolCalls` are now thin wrappers: parse, bail on `len(calls) == 0`, delegate. Resolved the inconsistency by making both paths print the "Unknown tool(s)" warning — user confirmed (2026-07-12) this should be the fix rather than preserving the old asymmetric behavior. Also extracted `availableToolNames(a *Agent) []string` (the `GetToolSchemas()` → name-slice loop), which was duplicated a third time in `terminal.go`'s history-injection correction message; both call sites now share it.

**Rejected.** Preserving the Apertus path's silence via a boolean flag/branch in the merged function — considered, but there was no positive reason found for the asymmetry, so carrying it forward as an explicit option would have encoded an accident as a decision.

**Consequences.** Neither original function had direct unit test coverage before this change (only their parsers, `ParseProseToolCalls`/`ParseApertusToolCalls`, were tested in `codeblock_test.go`) — characterization/new-behavior tests were written first per this repo's TDD-first convention, confirmed red against the pre-refactor code, then the refactor made them green: `TestExecuteAndReportToolCalls_dispatchesKnownTool`, `TestExecuteAndReportToolCalls_unknownToolWarnsWithAvailableList`, `TestTryExecuteProseToolCalls_toolsDisabled`, `TestTryExecuteProseToolCalls_unknownToolWarns`, `TestTryExecuteApertusToolCalls_dispatchesKnownTool`, `TestTryExecuteApertusToolCalls_unknownToolWarns` (the last of these locks in the new, consistent warning behavior). Full package test run is otherwise green; the one unrelated pre-existing failure (`TestCmdModelList_ShowsLlamafileEntries`, tied to already-in-progress, uncommitted llamafile-discovery work in `llamafile.go`/`backend_startup.go` predating this session) is untouched by this change. `go test -race` cannot run in this sandbox (`ThreadSanitizer: unsupported VMA range`) — pre-existing environment limitation, not caused by this change.

## 2026-07-08 — IBM generative computing / Granite Switch / Mellea research review

**Context.** TODO.md carried two research items: review IBM's aLoRA, Mellea, and Granite Libraries/Project Switch blog posts, and review github.com/generative-computing/granite-switch for implications across Harvey, Henry, and Mable. Reviewed all three IBM Research blog posts (inference-friendly aLoRAs, generative computing/Mellea, Granite libraries/Project Switch) and the granite-switch repo.

**Findings.**
- **aLoRA** (activated LoRA) adapts weights only for tokens after an in-prompt invocation string, so the base model's KV cache computed before that point is reusable — no full reprocessing on adapter switch. IBM claims 20–30x faster per-task-switch vs. standard LoRA.
- **Granite Switch** composes multiple task-specific aLoRA "adapter functions" (RAG query-rewrite, hallucination detection, citation gen, requirement-checking, Guardian safety checks) onto one base Granite model via a control-token dispatcher, sharing one KV cache across switches. Apache-2.0, Python/PyTorch, served via vLLM or HF Transformers. Only trained for Granite-family models (3.2/4.1 lineage) — of models currently on Harvey's benchmark list, only `granite-4.1-8b-source-Q4_K_M` would qualify.
- **Mellea** is a Python orchestration layer on top: converts type hints to schemas, auto-inserts control tokens, enforces output constraints at decode time — "generative computing" as typed function calls with verifiers instead of free-text prompting.
- **Ecosystem check (the load-bearing finding):** aLoRA is not vLLM/HF-exclusive. llama.cpp merged native aLoRA support upstream at release **b6396** (ggml-org/llama.cpp#15212, discussion #15213) — GGUF format plus `llama-server` support, not just static LoRA merge/scale. mozilla-ai/llamafile (what Henry produces) has generic `--lora`/`--lora-scaled` support (PR #786) aligned with llama.cpp's API, but it is unconfirmed whether that predates or includes the b6396 aLoRA-specific commit. Ollama's `ADAPTER` support is the older static-adapter path with no confirmed aLoRA invocation-string/KV-reuse mechanics.
- Harvey's own `~/Laboratory/hullicination.txt` (garbled Gemma4-E4B chunked-read output) is a live example of the failure mode Granite Switch's hallucination-detection adapter targets — concrete motivation if the runtime-support question resolves favorably.

**Decision.** No immediate adoption. Do not pull in vLLM/Mellea as a Python orchestration layer for Harvey — that doubles the runtime stack Harvey deliberately avoids (Go + Ollama/llama.cpp). Henry's existing Python/HF/torch pipeline is the more natural integration point *if* the tokenizer/control-token propagation through GGUF conversion checks out. Concrete next checks captured in `~/Laboratory/henry/continue_next_time.txt` rather than acted on now.

**Rejected.** Adopting Mellea directly (Python-only, no Go bridge). Treating this as applicable to Mable today (Mable is from-scratch corpus training, not Granite adapter composition) — noted as a design pattern worth remembering when Mable reaches its training-architecture decision, not an action item.

**Consequences.** TODO.md research items checked off, pointing here. Henry gets a follow-up list in `continue_next_time.txt` covering the llama.cpp/llamafile version check and GGUF tokenizer-metadata question. Separately under discussion: whether a Mellea-equivalent (typed instructions + verifiers) belongs in Harvey itself as a Go module, and whether KV-cache reuse for such a scheme is llama.cpp's responsibility or something Harvey would need to manage at the orchestration layer.

---

## 2026-07-05 — `/read-chunks` command: explicit chunked analysis, independent of the overflow trigger

**Context.** The only way to exercise `RunChunkedAnalysis` (map-reduce chunking) was to hit the automatic context-overflow guard — either by feeding a genuinely huge file, or by artificially shrinking `effectiveContextLimit()` (as the 2026-07-05 live retests did). This made comparing chunking strategies, or comparing chunk-quality across models with different context windows, needlessly fragile: results were confounded by whatever the current model's context budget happened to be.

**Decision.** Add `/read-chunks PATH [--chunk-size N] [--max-chunks N] [--overlap paragraph|sentence|none] [INSTRUCTION...]` (`cmdReadChunks`, `read_chunks_cmd_test.go`). It calls `ChunkDocument` + `RunChunkedAnalysis` directly — the identical functions the automatic path uses — with no threshold check at all; invoking the command is the confirmation. `--chunk-size`/`--max-chunks`/`--overlap` override `a.Config.Chunking` for that invocation only (not persisted), so the same file/model can be swept across chunking strategies in one session. `INSTRUCTION` falls back to the last user message when omitted, matching the implicit path's pre-fill convenience; `@model` mentions are parsed the same way. Unlike the tool-call path (where the synthesis is returned as a tool result and re-composed by another model turn) or the pre-inject path (where it's prepended as context for the next model turn), `/read-chunks` prints the full synthesis directly to the terminal — the point is to see the chunking pipeline's raw output for evaluation, not have it filtered through an additional model pass — and also appends it to history as a normal user/assistant exchange so follow-up questions can reference it.

**Rejected.** Gating the command behind the same overflow check it's meant to bypass (defeats the purpose). Silently ignoring `a.Config.Chunking.Enabled: false` — that flag only gates the *automatic* trigger; an explicit command isn't automatic, so it's not checked here at all (only the per-invocation `--chunk-size`/`--max-chunks`/`--overlap` flags override the loaded config, leaving other fields like `Enabled` irrelevant to this path).

**Consequences.** Naming follows the existing hyphenated convention (`/read-pdf`, `/read-dir`), not the `/read_chunks` placeholder. Command registered alongside `/read` in `commands.go`. Tests: `TestCmdReadChunks_NoArgs`, `TestCmdReadChunks_NoClient`, `TestCmdReadChunks_BypassesThreshold`, `TestCmdReadChunks_InstructionFallsBackToLastUserMessage`, `TestCmdReadChunks_NoInstructionNoHistory`, `TestCmdReadChunks_PermissionDenied`, `TestCmdReadChunks_InvalidChunkSizeFlag`, `TestCmdReadChunks_AddsResultToHistory`.

---

## 2026-07-05 — `/resume` slash command as a thin alias for `/session use`

**Context.** Live-testing the chunking fix (see below) via piped/tmux-driven input was fragile in part because the interactive startup flow's prompt sequence is hard to predict from outside (resume-session prompt, model picker, possible external-server-adopt prompt). Investigating a general "invoke CLI flags from inside the REPL" mechanism, most of it turned out to already exist: `/model use`, `/session continue`, `/session replay`, and `/record start` already delegate to the same underlying functions the equivalent CLI flags call (established by the 2026-06-20 unified-`/model`-command decision). `/session use` with no arguments already shows the identical interactive picker the startup "Resume a prior session?" prompt uses, and loads the chosen file via `ContinueFromFountain` — functionally identical to what a `/resume` command would do.

**Decision.** Add `/resume [FILE]` as a thin alias: `cmdResume` simply calls `cmdSession(a, append([]string{"use"}, args...), out)`. No new generic flag-to-command mechanism was built — the existing per-command delegation pattern already covers this need, and `/resume` is the discoverable name matching the `--resume` CLI flag.

**Rejected.** Building a generic mechanism to invoke arbitrary startup flags mid-session. Most flags either already have a natural slash-command equivalent (model selection, session load/replay, recording) or don't make sense mid-session (`--workdir`, `--llamafile-dir`). A generic passthrough would duplicate the existing per-command pattern without covering meaningfully more ground.

**Consequences.** `TestCmdResume_aliasForSessionUse`, `TestCmdResume_noArgsShowsPicker` added. The startup-time interactive "Resume a prior session? [y/N]" prompt itself is unchanged — dropping it in favor of `--resume`/`/resume` was considered but deferred, see TODO.md.

---

## 2026-07-05 — Llamafile GPULayers defaults to 0 (CPU-only), not 99

**Context.** A live chunking retest against `bonsai-8b` (Q1_0 quantization) on Raspberry Pi hardware appeared to hang for 20+ minutes. Investigation found the underlying `llama-server` process had been running for **over 2 hours of CPU time** with no output, launched with `-ngl 99` (maximise GPU offload) — Harvey's default for every llamafile model (`config.go`, `LlamafileConfig.GPULayers: 99`). Raspberry Pi hardware has no usable GPU-compute backend; forcing maximum GPU-layer offload on such hardware is a plausible cause of severe degradation or an effective hang, independent of the quantization type. `LlamaCppConfig.GPULayers` (the sibling backend's config) already defaulted to `0` for this exact reason — the llamafile default was an inconsistency, not a deliberate choice.

**Decision.** Change `LlamafileConfig.GPULayers`'s default from `99` to `0`. `buildLlamafileArgs` (`llamafile_service.go`) already treats `0` as "explicitly pass `-ngl 0`" (forces CPU-only), distinct from a negative value (omits the flag, defers to the binary's own default) — so `0` is the correct, unambiguous safe default, not just an arbitrary placeholder. Users with real GPU-compute hardware opt in via `gpu_layers:` in `harvey.yaml`. The "only persist when overriding the default" check in `SaveLlamafileConfig` was updated from `!= 99` to `!= 0` to match.

**Consequences.** `TestDefaultConfig_LlamafileGPULayersDefaultsToZero`, `TestSaveLlamafileConfig_DoesNotPersistDefaultGPULayers`, `TestSaveLlamafileConfig_PersistsCustomGPULayers` added. Existing `harvey.yaml` files that don't already set `gpu_layers` explicitly silently pick up the new safer default on next load — no migration needed. Users on capable GPU hardware who were relying on the implicit 99-default will need to add `gpu_layers: 99` explicitly.

---

## 2026-07-05 — Chunking guard fix: unknown context limit must not bypass overflow detection

**Context.** TODO.md reported garbled output from Gemma4-E4B when asked to review a document for topic drift, with the note "Never got the chunk prompt entry option." Root-cause investigation traced this to `remainingContext()` (`context_estimator.go`) returning `0` both when the model's context limit is genuinely unknown and when it is known but exhausted. `read_file`'s chunking pre-read guard in `builtin_tools.go` used `if rem := remainingContext(a); rem > 0` as its sole gate — so an unknown limit silently skipped the entire overflow check and fell through to a full raw read, rather than triggering the chunk-prompt UX. `file_inject.go`'s `injectOrChunk` already handled this correctly (falling back to a conservative 4096-token budget when `rem <= 0` and the limit truly is unknown), but `builtin_tools.go`'s tool-call path did not share that logic.

A second, deeper cause was found for the llamafile backend specifically: `effectiveContextLimit()` (`harvey.go`) only resolves a context window for llamafile models from the `LlamafileEntry.ContextLength` field in `harvey.yaml` — the `ModelCache` fallback never carries a value for llamafile models, because `probeLlamaCppAndCache` (used by `useLlamafileEntry` on every llamafile connect, including adoption) only writes `SupportsTools`/`ToolMode` into the cache, never `ContextLength`. `switchLlamafileModel` and `addAndStartLlamafile` both call `ProbeLlamafileContextLength` to populate `LlamafileEntry.ContextLength`, but `adoptExternalServer` (`llamafile.go`) — used when Harvey detects an already-running llamafile server at startup and offers to adopt it — did not. Any model adopted this way has `ContextLength` permanently stuck at `0` for the session.

**Decision.** Two fixes: (1) `builtin_tools.go`'s `read_file` chunking guard now uses the same fallback pattern as `injectOrChunk`: `rem <= 0` sets `rem = 4096` rather than skipping the guard outright. (2) `adoptExternalServer` now calls `ProbeLlamafileContextLength` and stores the result on the registered `LlamafileEntry`, matching the other two llamafile-registration call sites.

**Known remaining gap.** `startAndUseLlamafile` (`backend_startup.go`) has a similar hole: when it detects a server already running under a *different* model name than the configured active entry, it adopts the detected name via `useLlamafileEntry` without registering (or probing) a matching `LlamafileEntry`. Deferred — this is a narrower edge case (requires an already-running server serving an unexpected model) than the primary adopt-on-first-connect path just fixed.

**Consequences.** `TestReadFile_ChunkingEnabledContextLimitUnknown` (`builtin_tools_test.go`) and `TestAdoptExternalServer_probesContextLength` (`llamafile_test.go`) cover the two fixes. Live retest against `bonsai-8b` (Q1_0 quantization) was inconclusive on chunk-quality: the test document (~12.7KB) fell just under the 4096-token fallback budget so chunking did not trigger, and the single-shot response did not complete within ~20 minutes, suggesting Q1_0 quantization has poor CPU dequantization throughput on this hardware independent of the chunking fix. Re-testing chunk-quality on a document large enough to force chunking, and/or against a non-Q1_0 4B–8B model, is still open — see TODO.md.

---

## 2026-06-30 — Assay switches from `callOllama` to `harvey.LLMClient` for all backends

**Context.** `bin/assay` had a private `callOllama` function that spoke Ollama's proprietary `/api/chat` endpoint directly. Llamafile happened to also expose this Ollama-compatible API, so the single function covered two backends. Adding llama.cpp support requires the OpenAI-compatible `/v1/chat/completions` path, which Ollama does not expose at `/api/chat`. Writing a parallel `callOpenAI` function would create two diverging code paths that must be kept in sync.

**Decision.** Replace `callOllama` (and the private `ollamaRequest` / `ollamaResponse` structs) with `harvey.LLMClient` — the same interface used throughout the harvey interactive agent. The concrete implementation (`AnyLLMClient` backed by `mozilla-ai/any-llm-go`) already supports Ollama, llamafile, and llama.cpp via the OpenAI-compatible API. All three backends follow the same `client.Chat(ctx, messages, &buf)` call path. Token stats come from `harvey.ChatStats` instead of the proprietary Ollama response shape.

**Rejected.** Adding a parallel `callLlamaCpp` function that speaks `/v1/chat/completions` directly. This avoids touching the existing Ollama path but creates maintenance burden: two implementations of the same call, diverging error handling, different stat fields. Token stat normalisation would have to be done twice.

**Consequences.** The `ollamaRequest`, `ollamaMessage`, and `ollamaResponse` types are removed from `cmd/assay/main.go`. The `callOllama` function is removed. Token stats now come from `ChatStats.PromptTokens`, `ChatStats.ReplyTokens`, and `ChatStats.TokensPerSec`. The Ollama model-listing path (`listOllamaModels`) is retained as-is since it calls `/api/tags` directly and is Ollama-specific by nature. A parallel `listOpenAIModels` helper is added for llama.cpp's `/v1/models` endpoint.

---

## 2026-06-30 — Assay does not manage the llama-server process lifecycle

**Context.** For llamafile, assay starts the binary itself (`StartLlamafileService`), finds a free port, and defers cleanup. This was necessary because llamafile is a single self-contained executable with no prior setup. llama.cpp (`llama-server`) requires separate configuration: model path, context size, GPU layers, quantisation, threading. These are server-administrator decisions that assay should not make on behalf of the user.

**Decision.** `--llamacpp URL` connects to a running `llama-server` at the given URL. Assay does not start or stop it. The user starts the server before running assay and stops it afterward. This matches the `--ollama` pattern exactly.

**Rejected.** A `--llamacpp-path PATH` flag that mirrors `--llamafile PATH`. The reason it was rejected: llama.cpp needs too many server-tuning flags (ctx, threads, GPU layers) to make a one-flag launch practical without exposing all of them in assay's flag set — which would duplicate harvey's own LlamaCpp configuration. Requiring the user to start the server explicitly keeps assay's surface area small.

**Consequences.** Users must start `llama-server` before running assay. Error messages guide them: if the URL is unreachable, assay exits immediately with a clear message rather than timing out.

---

## 2026-06-30 — Shared `probeLlamaCppAndCache` helper for capability detection

**Context.** `ProbeLlamafileProps` was already implemented and wired for llamafile (via `useLlamafileEntry` in `backend_startup.go`), but the llama.cpp startup path (`startLlamaCppModelPath` in `backend_llamacpp.go`) never called it. As a result, `toolsReliable()` always found no `ModelCache` entry for llama.cpp models and returned false — but tool definitions were still sent, causing the model to hallucinate rather than call them.

**Decision.** Extract the probe-then-cache-write logic into a `probeLlamaCppAndCache(a *Agent, modelName, baseURL string)` helper in `backend_llamacpp.go`. Wire it into both paths:
- `useLlamafileEntry` in `backend_startup.go` (replaced inline block with one call)
- `startLlamaCppModelPath` in `backend_llamacpp.go` (new, called after backend is fully wired)

The function is a no-op when `a.ModelCache == nil` or when an existing entry has `ProbeLevel != "none"`, preserving the skip-on-re-probe behaviour.

**Rejected.** Duplicating the inline block in `startLlamaCppModelPath` would have caused the two paths to drift. A method on `Agent` was also considered but adds no real benefit over a package-level helper that takes `*Agent`.

**Consequences.** Both llamafile and llama.cpp models now populate `model_cache.db` immediately after startup. `toolsReliable()` sees the capability entry on the first turn rather than defaulting to `CapNo`.

---

## 2026-06-30 — `/ollama` command removed; Ollama management delegated to the Ollama CLI

**Context.** Harvey had a large `/ollama` command with subcommands covering start/stop/status, server lifecycle, model listing, pull/push/rm, probe, logs, env, ps, and alias management. Most of these duplicate functionality already covered by the `ollama` CLI itself. The surface was redundant, brittle (Harvey had to maintain parity with the Ollama API), and added cognitive load for users who switch between Harvey and the shell. The unified `/model` command (introduced 2026-06-20) already provided a backend-agnostic entry point for model switching. The alias subcommand was already shared.

**Decision.** Remove `/ollama` entirely. Ollama model management (pull, push, rm, list, show, run) is delegated entirely to the `ollama` CLI. Harvey's responsibilities for Ollama reduce to:

1. **Model discovery** — `aggregateModels` queries the live Ollama `/api/tags` endpoint when Ollama is reachable; result appears in `/model list`.
2. **Model switching** — `/model use` resolves across all backends including Ollama.
3. **Alias management** — `/model alias` unchanged; aliases now carry an `Engine` field ("ollama", "llamafile", "llamacpp", or "" for legacy).
4. **Auto-probe on alias creation** — when `/model use` creates a new Ollama alias, `FastProbeModel` is called immediately so capability data (tool support, embed support, context length) is cached without a separate `/ollama probe` command.
5. **Stale alias cleanup** — `/model clean` replaces `/ollama clean`, pruning aliases for all engines (not just Ollama) using `pruneStaleModelRefs`. Legacy aliases with no engine field are preserved.
6. **Service lifecycle** — Harvey starts Ollama only when a model needs it and Harvey was configured to manage it. `/model stop` and `/model status` remain backend-agnostic. Detailed service management (logs, env, ps) is the `ollama` CLI's job.

The `cmdOllama`, `ollamaProbe`, `pruneStaleOllamaRefs`, `ollamaModelTable`, and `removeModelFromConfig` functions are deleted.

**Rejected alternatives.**

- *Keep `/ollama` as a thin wrapper around the `ollama` CLI* — adds indirection without value; users who want `ollama` output should just type `!ollama`.
- *Keep only the useful subcommands (`start`, `stop`, `status`, `list`)* — `/model` already provides all of these in a backend-agnostic way. Keeping a subset of `/ollama` would confuse the command vocabulary.
- *Preserve `/ollama probe` as an explicit command* — auto-probe on alias creation covers the same need at the moment the alias is most useful (right after setup). An explicit probe command is redundant.

**Consequences.**

- `commands.go` loses ~600 lines: `cmdOllama`, `ollamaProbe`, `pruneStaleOllamaRefs`, `ollamaModelTable`, `removeModelFromConfig`.
- `ModelAlias` struct gains `Engine string` (persisted as `engine:` in YAML; legacy aliases without this field match any backend).
- `pruneStaleModelRefs(a, liveOllama, liveLlamafile, liveLlamaCpp, out)` replaces `pruneStaleOllamaRefs`.
- `aggregateModels` provides the unified model list for `/model list` across all backends.
- Users who relied on `/ollama pull`, `/ollama rm`, etc. must use the `ollama` CLI or `! ollama <subcommand>`.
- Tests for all deleted functions removed; `TestOllamaCommandRemoved` verifies the command table entry is gone.

---

## 2026-06-30 — Blank-slate active model: no persistence, no auto-start from config

**Context.** Harvey previously persisted the last-used Llamafile model to `harvey.yaml` as `llamafile.active` and auto-started it at next session. This caused surprising behavior: starting Harvey would silently launch the last Llamafile process without prompting, the "active" model name was meaningless across session restarts (the user may have added or removed models), and the pattern did not generalise to Ollama or llama.cpp. The concept of "sticky active model" proved idiosyncratic across all three backends.

**Decision.** Drop the active-model persistence concept for sessions not being resumed. Specifically:

- `SaveLlamafileConfig` never writes the `active:` YAML field (always saved as `""`).
- `selectBackend` Case 1 (auto-start the persisted active Llamafile) is removed.
- When Harvey is started with `--llamafile PATH`, the path is threaded as a `hint` into `selectBackend` for that session only; it is not persisted.
- When resuming a session (`--continue` / `--resume`), the session's model is restored via existing session-resume logic, not via the config's `active:` field.

The intent is that at startup Harvey always shows the full model picker, giving the user explicit control every time, rather than guessing which model they want.

**Rejected alternatives.**

- *Persist active model and let the user opt out* — adds a config knob; the cost of always picking is low and the benefit of a clean slate is high for users who cycle through many models.
- *Persist active model only for Ollama* — inconsistent across backends; users would need to learn different startup behavior per engine.

**Consequences.**

- `Config.Llamafile.Active` is still loaded from YAML for backward compat but is never written back, so it decays naturally as the user's config is saved.
- `backend_startup.go` Case 1 deleted; Case 2 (picker from registered models) is now the first case.
- `TestSaveLlamafileConfig_DoesNotPersistActive` verifies the new behavior.
- Users who depended on the auto-start behavior must use `--llamafile PATH` at the CLI or pick from the model picker at startup.

---

## 2026-06-28 — `/plan` IVR support deferred — design incomplete

**Context.** Harvey's `/plan` feature provides bounded-context task execution but lacks output validation and automatic repair. The Instruct-Validate-Repair (IVR) pattern (from the [Mellea project](https://mellea.ai/blogs/why-mellea/)) was evaluated as a candidate extension. Two integration options were considered: (A) extend `/plan` with opt-in inline validation annotations; or (B) add a new `/ivr` command.

**Decision.** **Deferred to a future phase.** Design review revealed too many open questions to proceed safely in the current release cycle:

- What does "step output" mean for each validation type? `validate: command:go build ./...` clearly tests workspace state after the step, but `validate: regex:^func Test` is ambiguous — does it match the model's raw response text or a file? This distinction determines the entire implementation.
- The repair prompt design includes "previous output" verbatim, which could overflow the context window of a small model when the prior step produced large text (a build log, a file listing, etc.).
- The `no_errors` validation type has no deterministic definition — "common error patterns" is LLM-like vagueness in a system that explicitly requires deterministic validation.
- The relationship between IVR and Harvey's strict output enforcement work (Idea 3 in [capability-adapter-concept.md](capability-adapter-concept.md)) is unexplored. Idea 3 may provide the structural foundation IVR needs before behavioral validation can be layered on top.

**Preferred path when ready.** Option A (extend `/plan` with annotations) remains the correct integration point. Option B (separate `/ivr` command) is rejected — IVR is fundamentally about making `/plan` more reliable, not a separate workflow.

**Consequences.**

- No code changes in this release.
- [plan-ivr-design.md](plan-ivr-design.md) is marked incomplete and deferred.
- Idea 3 in [capability-adapter-concept.md](capability-adapter-concept.md) should be developed first; its strict output enforcement pattern may resolve IVR's core validation ambiguity.
- IVR design resumes after Idea 3 is implemented and the validation-target question is answered.

---

## 2026-06-28 — Local model backend design deferred — unified abstraction under exploration

**Context.** Users want llama.cpp as a Harvey backend for better performance on ARM devices. A server-based integration design was drafted in [llamacpp-support.design.md](llamacpp-support.design.md). Design review identified a broader problem: Ollama and Llamafile management both have reliability issues in their current form, and adding a third ad-hoc backend without resolving the underlying inconsistency would make the situation worse. All three backends (Ollama, Llamafile, llama.cpp) share the same lifecycle concerns — start, stop, status, model listing, client wiring — but are currently implemented independently with different data structures and command patterns.

**Decision.** **Implementation deferred.** The design phase continues toward a unified model backend abstraction covering all three local inference backends. The existing `llamacpp-support.design.md` is a reference, not a finalized design.

The unified design will address:
- A common `ManagedBackend` interface for lifecycle management (start, stop, status, list models, active model, base URL, new LLM client)
- Consistent client wiring in `Agent` when the active backend changes — replacing the current split between `OllamaStartedByHarvey bool` and `llamafileProc *os.Process`
- A unified or parallel command surface that users can discover consistently across backends
- Server detection and adoption (currently only Llamafile has `adoptExternalServer`; Ollama and llama.cpp need equivalent)
- PID or process persistence across Harvey sessions for backends Harvey started

**Rejected alternatives.**

- *Direct embedding of llama.cpp via CGO* — too much build complexity and maintenance burden for the Pi-first use case. Remains deferred.
- *Ollama-only* — insufficient for users who need GGUF control or llama.cpp-specific quantization options.
- *Proceed with ad-hoc llama.cpp commands* — would entrench the inconsistency rather than fix it.

**Consequences.**

- No new `/llamacpp` command implementation in this release.
- [llamacpp-support.design.md](llamacpp-support.design.md) retains value as a backend-specific reference.
- New exploration document: [unified-model-backend-design.md](unified-model-backend-design.md).

---

## 2026-06-27 — Chunked document analysis uses paragraph/block boundaries, not fixed-size tokens

**Context.** The chunked analysis feature (see
[chunked-analysis-design.md](chunked-analysis-design.md)) must split a
document into chunks before the map phase. The three practical options are:
fixed-size token windows, semantic embedding-based splits, and
structure-aware splits on natural document boundaries (paragraphs for prose,
function/block boundaries for source code).

**Decision.** Use structure-aware splitting: paragraph boundaries (double
newline) for prose document types (`.md`, `.txt`, `.rst`, `.tex`, `.html`),
and function/block boundaries (blank-line-then-signature heuristic) for
source code types (`.go`, `.ts`, `.py`, `.js`, `.c`, `.h`, others). Each
chunk includes the last paragraph or signature of the preceding chunk as
overlap. Document type is detected by file extension; unknown extensions
default to paragraph splitting.

Structure-aware chunking is supported by multiple independent studies. The
Bioengineering evaluation (Gomez-Cabello et al., 2025) found adaptive
boundary alignment achieved 87% accuracy vs 50% for fixed-size chunking
across identical RAG pipelines. AutoChunker (Jain et al., ACL 2025)
demonstrated that preserving document hierarchy at boundaries reduces noise
and improves chunk coherence. The KES 2026 systematic comparison
(Śmigielski et al.) confirmed structure-aware outperforms fixed-size across
diverse document and query types.

**Rejected alternatives.**

- *Fixed-size token windows* — simple to implement but consistently
  underperforms in the literature. QASC (Rastogi, 2605.22834) found 20%
  of technical documents yield inconsistent retrieval outcomes for
  identical queries when chunk size varies, because fixed windows cut
  across sentence and argument boundaries.
- *Semantic embedding-based splits* — requires an active embedding model
  (not guaranteed in all Harvey configurations) and adds indexing latency.
  Gains over paragraph splitting are modest (Oil & Gas study: structure-
  aware outperforms semantic at lower computational cost). Deferred.
- *Language-aware parser for source code* — using `go/parser`,
  `tree-sitter`, or similar for precise function boundary detection. Adds
  a per-language dependency tree. The blank-line-then-signature heuristic
  correctly identifies boundaries in well-formatted Go and TypeScript
  without any dependency. Deferred until the heuristic proves insufficient.

**Consequences.**

- Document type detection by file extension is required before chunking;
  unknown types fall back to paragraph splitting.
- Chunk size (default 1,500 tokens / ~6,000 bytes) and overlap strategy
  are configurable in `harvey.yaml` under a new `chunking:` stanza.
- If a document would produce more than `max_chunks` (default 20), Harvey
  warns the user before proceeding — processing 20 of 100 chunks omits
  material and the user should know.

---

## 2026-06-27 — Chunked analysis uses map-reduce; sliding window and ephemeral RAG deferred

**Context.** When a document is split into N chunks, Harvey must process
each chunk and combine the results. Three patterns were considered:
map-reduce (process chunks independently, synthesize once), sliding-window
summarization (process chunks sequentially, carry a rolling summary), and
ephemeral RAG (ingest the file into a temporary vector store and query it).

**Decision.** Use map-reduce. Each chunk is processed independently with
the user's chunk instruction as the prompt (map phase). After all chunks
complete, a single synthesis pass combines the partial results into a final
answer that is injected into the main conversation history (reduce phase).
The synthesis model defaults to the same model used for the map phase.

DocETL (Shankar et al., 2410.12189) showed that decomposing single-pass
LLM document operations into map→reduce sequences improved accuracy 21–80%
over single-pass approaches across four complex document analysis tasks.
NexusSum (Kim & Kim, ACL 2025) demonstrated a 30% improvement in
BERTScore F1 using a hierarchical multi-LLM pipeline with controlled
per-chunk output, the same two-phase structure Harvey adopts here.

**Rejected alternatives.**

- *Sliding-window summarization* — processes chunks sequentially, passing
  a rolling summary to each subsequent chunk prompt. Information loss
  compounds at each step; for a 20-chunk document, the summary entering
  chunk 20 is a distillation of nineteen distillations. ContextWeaver
  (Wu et al., 2604.23069) confirms that approaches which discard earlier
  reasoning context degrade multi-step performance. Rejected.
- *Ephemeral RAG* — ingests the file into a temporary vector store and
  uses the existing `ragAugment` pipeline to retrieve relevant chunks.
  Suits retrieval use cases (find relevant chunks) but not analysis use
  cases (process every chunk). Requires an active embedding model not
  guaranteed in all Harvey configurations. Deferred as a future option
  for very large corpora where the user wants to query rather than
  analyze exhaustively.
- *Per-chunk user confirmation* — pause after each chunk result for user
  review before proceeding. Adds control at the cost of 20+ interruptions
  for a single document. Progress is displayed in the terminal during the
  map phase but no per-chunk confirmation is required.

**Consequences.**

- The map phase runs unattended; terminal progress (`Processing chunk
  3/12…`) is the user's visibility into it.
- The synthesized result is injected into main conversation history as the
  assistant's response to the original user message; the conversation
  continues normally from that point.
- The synthesis prompt is derived automatically from the user's chunk
  instruction; no second prompt is shown to the user (D4).
- If any individual chunk fails, Harvey records `error` in the Fountain
  note for that chunk and continues. A partial synthesis is still
  attempted; the user is informed of failed chunks in the terminal.

---

## 2026-06-27 — Chunked analysis is user-directed; overflow triggers an alert, not silent chunking

**Context.** When Harvey detects that reading a file would overflow the
model's remaining context, it must decide whether to chunk silently and
automatically or to pause and involve the user. The trigger for this
decision uses two signals: an `os.Stat` byte-size estimate before the file
is read, and a remaining-context estimate that accounts for current
history, system prompt, and injected memories (not the raw context window).

**Decision.** Harvey alerts the user rather than chunking silently. The
alert shows the file name, estimated size, and estimated remaining context,
then presents the user's most recent message as a pre-filled chunk prompt
with the instruction: *Enter instructions to process each chunk in turn,
or "no" to return to the conversation.* The user may edit the prompt,
accept it as-is, or cancel. The chunk prompt may include an `@model`
directive, which Harvey's existing `@mention` routing infrastructure uses
to route each chunk analysis call to the named model.

QASC (Rastogi, 2605.22834) provides direct empirical support: treating
the user query as a first-class input to segmentation improves relevance
18–27% over fixed or automatically derived chunking prompts. Chunking
quality is tied to query specificity; only the user knows what they want
from each chunk.

The two-signal trigger (byte estimate before read, remaining-context
estimate accounting for history) addresses the underlying accounting bug:
Harvey currently compares file size against the raw context window rather
than the context that remains after history and injected content are
accounted for. ContextWeaver (Wu et al., 2604.23069) identifies this as
the primary cause of unexpected context overflow in agentic systems.

**Rejected alternatives.**

- *Silent automatic chunking* — derives the chunk prompt from the original
  user message and proceeds without interruption. Rejected: (1) a generic
  "read this file" prompt produces poor chunk results per QASC; (2) silent
  chunking obscures Harvey's behavior and prevents the user from cancelling
  before many LLM calls are made; (3) the user may realize the file is
  irrelevant to their question only when the alert appears.
- *Trigger on raw context window only* — compare file size against the
  model's total context window, ignoring existing history. This is the
  current (buggy) behavior. It causes overflow when the conversation has
  accumulated significant context. Rejected as the trigger for the new
  feature; the remaining-context estimate is used instead.

**Consequences.**

- The `read_file` tool path gains a pre-read size check using `os.Stat`
  and a remaining-context estimate derived from serialized history length.
- A new alert UX path is required in `terminal.go`; it resembles the
  existing tool-confirmation flow.
- `@mention` in the chunk prompt is parsed by the existing routing
  infrastructure; no new routing code is needed.
- The chunk sub-conversation (user's instruction, per-chunk status notes,
  synthesis status) is recorded as a new `INT. CHUNK ANALYSIS` scene in
  the session `.spmd` file. The user's chunk instruction in that scene is
  available to `/memory mine` for extraction as a reusable workflow
  pattern.
- A new `chunking:` stanza in `harvey.yaml` controls the alert threshold
  (default 80% of remaining context), chunk size, max chunks, and overlap
  strategy.

---

## 2026-06-25 — Source registry lives in `knowledge.db`; not a separate database

**Context.** The scholarly provenance design (see
[scholarly-provenance-design.md](scholarly-provenance-design.md))
requires a `sources` authority table and an `observation_sources` join
table. Two placement options were considered: a new `provenance.db`
alongside `knowledge.db`, or new tables inside the existing
`knowledge.db`.

**Decision.** Add `sources` and `observation_sources` directly to
`knowledge.db`. The sources table needs to join against `observations`,
`concepts`, and `kb_fts`, all of which live in `knowledge.db`. SQLite
cross-database joins via `ATTACH DATABASE` cannot use foreign keys and
require every query to name the attached database alias, making all
query code more fragile. A single database with multiple tables is the
correct SQLite idiom.

**Rejected alternatives.**

- *Separate `provenance.db`* — eliminates foreign keys between
  observations and sources; requires `ATTACH` in every query that spans
  the two files; adds a new runtime file that users must back up and
  move with their workspace.
- *In-memory provenance (no persistence)* — RAG provenance that
  disappears on session end has no scholarly value. The whole point is
  a durable, auditable record.

**Consequences.**

- `knowledge.go` gains DDL for `sources` and `observation_sources` in
  its `Open` path.
- The data migration from `observations.source_doi` runs once on first
  open after upgrade; `source_doi` is retained as a read-only backward-
  compat column.
- No new runtime files are introduced; `agents/knowledge.db` remains the
  single knowledge-base file.

---

## 2026-06-25 — Scholarly provenance: inference-time only; training-time attribution deferred

**Context.** Two Scholarly Kitchen articles (2026-06-17 and 2026-06-25)
and the Cambridge Scholarly AI Workshop identified that AI systems
interact with scholarly content at two points: training time (content
absorbed into model weights) and inference time (content retrieved and
injected via RAG at query time). The workshop explicitly classified
training-time attribution as technically intractable at current model
scales and recommended focusing practical interventions on inference-time
retrieval.

Harvey's architecture makes inference-time provenance fully tractable:
the RAG pipeline (`ragAugment`, `RagStore.Query`) has complete
observability of what was retrieved and from where. Training-time
attribution for Ollama or Llamafile models is not accessible to Harvey
and would require coordination with model providers.

**Decision.** The scholarly provenance work (v0.0.15) focuses entirely
on inference-time provenance:
1. A minimum provenance payload on RAG chunks (source, DOI, title,
   version, rights, content hash, retraction flag).
2. A source registry in `knowledge.db` as the authority for source
   metadata, linked to observations via `observation_sources`.
3. Per-source `[[rag-source: ...]]` Fountain notes so session files
   serve as citable records of what evidence informed each response.
4. `HARVEY.md` system-prompt guidance to retrieve before generating
   and to attribute content at the point of use, not post-hoc.

Training-time attribution is explicitly deferred and recorded as out of
scope, not a gap in the design.

**Rejected alternatives.**

- *Attempt training-data disclosure via model metadata* — Ollama's
  `/api/show` endpoint returns a Modelfile and template but not a
  training corpus manifest. No standard interface exists. Not tractable.
- *Restrict Harvey to models with published data cards* — would exclude
  most locally-available models and undermine the local-first principle.

**Consequences.**

- See [scholarly-provenance-design.md](scholarly-provenance-design.md)
  for the full architecture and
  [scholarly-provenance-plan.md](scholarly-provenance-plan.md) for the
  phased implementation.
- Provenance metadata added to `chunks` schema (S1), source registry
  added to `knowledge.db` (S2), Fountain notes enhanced (S3), `/kb`
  commands extended (S4).

---

## 2026-06-24 — INT./EXT. scene prefix redefined as local/remote computation

**Context.** The original Fountain format spec (v1.0–1.1) defined `INT.` as "Harvey is involved as orchestrator" and `EXT.` as "direct model-human conversation without Harvey." This made `EXT.` scenes effectively hypothetical — the recorder never wrote one, because Harvey is always involved. Remote Ollama route dispatches (e.g. `@pi2`) and cloud API calls were both recorded as `INT.` despite running on remote machines. The distinction was meaningless in practice.

**Decision.** Redefine the prefix semantically as **location of computation**: `INT.` = runs on the local machine where Harvey is running; `EXT.` = runs on a remote system. This maps naturally to the theatrical meaning (interior/exterior), gives `EXT.` scenes real-world frequency, and encodes practically important information (network latency, data exposure, cost). Remote Ollama routes and cloud API routes are now `EXT.` HARVEY still appears in `EXT.` scene dialogue as the forwarding character when Harvey initiated the route dispatch; HARVEY is absent only in truly direct conversations (no Harvey involvement). The `RecordExteriorTurn` recorder method writes EXT. scenes; `RecordTurnWithStats` continues to write INT. scenes.

**Rejected alternatives.**

- *Keep the old semantic, just document it better* — the old definition made EXT. permanently dead code and gave parsers no useful locality signal. The new semantic costs nothing to implement and adds real diagnostic value.
- *Use a `Remote: true` field in the scene description instead of the prefix* — keeps the prefix consistent but buries locality in metadata. The theatrical prefix is the primary structural signal in Fountain; using it for locality is more idiomatic.
- *Make every forwarded turn EXT. regardless of locality* — local model-switch via `@mention` (where `attemptModelSwitch` succeeds) is still local computation. Only registered route dispatches (`DispatchToEndpoint`) are genuinely remote.

**Consequences.**

- `FOUNTAIN_FORMAT.md` updated to v1.2 with corrected INT./EXT. definitions, updated scenarios 2 and 3, and updated best practices.
- `recorder.go` gains `RecordExteriorTurn(endpoint, userInput, reply string)`.
- `terminal.go` route dispatch path calls `RecordExteriorTurn` instead of `RecordTurn`.
- Existing `.spmd` session files recorded before v0.0.15 have INT. for route dispatches — this is a known inaccuracy, not a migration target.

---

## 2026-06-24 — Fountain sessions become full audit trails (v0.0.15)

**Context.** Corin Wagen's article "Tool Use and AI Scientists" argues that tool calls are the primary mechanism for AI interpretability — the decision trace of what an agent chose to do and why. Harvey's Fountain session files record dialogue, file writes, and shell commands, but tool calls appear only as unstructured prose ("Harvey calls read_file: {args}"), tool results are not recorded, RAG context retrieval leaves no trace, and memory injection at session start is invisible. See [audit-trail-design.md](audit-trail-design.md).

**Decision.** Extend the Fountain format to v1.2 with four new audit elements. A Harvey session file is a *sequence of many scenes* — one per discrete interaction (chat turn, shell command, file write, skill activation). The placement of new elements respects this: notes go inside existing scenes; only one new scene type is added.

- `[[tool: name(args) — status]]` notes replace prose action blocks for tool calls. They appear **inside the existing `INT. HARVEY AND … TALKING` scene** for the turn where the tool loop ran, between HARVEY's forwarding line and the model's reply. Multiple tool-call rounds within one turn produce multiple flat notes in the same scene — no new scene is opened per round.
- `[[CHARACTER.tool: name(args) — status]]` variant attributes tool calls to a forwarded model in `@mention` turns. Same placement as above; only the prefix changes.
- `[[rag: N chunks from STORE, top score S.SS]]` notes record RAG retrieval **inside the existing `INT. HARVEY AND … TALKING` scene** for the turn where RAG fired, before the user dialogue line. Turns where RAG did not fire have no `[[rag:]]` note.
- `INT. CONTEXT RECALL TIMESTAMP` is the only new scene type. It appears once at session start — before the first chat turn — when `UnifiedMemory.Recall` injects memories. It contains `[[recall: ID (SOURCE) — score S.SS]]` notes, one per recalled item.

**Rejected alternatives.**

- *Bridge `audit.jsonl` and Fountain* — routing `AuditBuffer` events to the recorder would couple two unrelated systems (security audit vs. session narrative) and require the audit buffer to hold a recorder reference. Rejected: keep them separate.
- *Full tool result content in Fountain* — maximally auditable but bloats session files and degrades memory miner quality for large `read_file` or search outputs. Status-only (`ok` / `error: first line`) achieves the diagnostic goal.
- *RAG note in the scene description block* — the scene description is written at scene open; RAG fires later in `runChatTurn`. A separate note just before user dialogue is temporally accurate.
- *`INT. TOOL LOOP` scene per tool-call round* — a multi-round tool loop (model calls tool, gets result, calls another tool, gets result, produces final answer) could open a new scene for each round. Rejected: a "turn" from the user's perspective is one request-response cycle; splitting it across multiple scenes makes the session harder to read and harder for the memory miner to extract question-answer pairs. Flat notes inside the single turn scene preserve both.
- *Per-message character attribution via `Message.Model`* — accurate multi-round character attribution requires tagging each `Message`, which ripples through serialisation, compaction, and replay. Deferred: single character per turn covers the real-world case.

**Consequences.**

- `recorder.go`: `ToolCallRecord` gains `Result` and `Character` fields; `RAGAugmentInfo` struct added; `RecordTurnWithStats` gains `ragInfo *RAGAugmentInfo` parameter; `RecordContextRecall` method added.
- `terminal.go`: `ragAugment` returns `(string, *RAGAugmentInfo)`; `toolCallsFromHistory` gains `charName string` parameter; `runChatTurn` gains `charName string` parameter.
- `harvey.go`: `injectMemoryContext` calls `a.Recorder.RecordContextRecall` when results are non-empty.
- `tool_executor.go`: `ToolExecutor` gains `CharacterName string` field.
- `FOUNTAIN_FORMAT.md` updated to v1.2 with new syntax and scene type.
- All existing callers of `RecordTurnWithStats` pass `nil` for the new `ragInfo` parameter; all callers of `toolCallsFromHistory` pass `""` for `charName` except the `@mention` local-switch path.

---

## 2026-06-20 — Command vocabulary standardised across all resource-management commands

**Context.** Harvey's command families share a common resource-management pattern but use inconsistent verbs: `/llamafile drop`, `/rag drop`, `/route rm`, and `/model alias delete` all mean the same thing; `/skill info` and `/skill-set info` duplicate `/memory profile show`'s pattern under a different name; `/session` has no `list` or `show`; `/route` has no `use`. Users must learn each command family independently rather than applying a single vocabulary pattern. See [llamafile-primary-design.md](llamafile-primary-design.md) and TODO.md.

**Decision.** Standardise on eight core verbs for all resource-management commands: `list`, `add` (register external resource), `new` (create internal item), `use` (activate), `show` (display content/details), `edit` (open in `$EDITOR`), `remove` (delete/unregister), `rename`. Backend service commands additionally support `start`, `stop`, and `status` (health/connection — distinct from `show`). The `add` vs `new` distinction is preserved: `add` registers something that already exists externally (a file path, a URL); `new` creates something Harvey owns (a database, a skill, a plan). Existing non-standard verbs (`drop`, `rm`, `info`, `create`, `set`) are kept as backward-compatible aliases; the canonical verb is the one documented and tab-completed.

**Rejected alternatives.**

- *Rename only the worst offenders* — partial fixes leave the vocabulary inconsistent enough that users still cannot predict subcommands. The value comes from universal coverage.
- *Single `delete` verb everywhere* — `delete` implies permanent destruction; `remove` better conveys "unregister from Harvey's knowledge" (the underlying file or database is not deleted).
- *Collapse `add` and `new` into a single verb* — the distinction maps to a real semantic difference users already understand. `add` = "I have a thing, register it"; `new` = "create a thing for me".

**Consequences.**

- `/rag remove`, `/route remove`, `/session list`, `/session show`, `/session use`, `/llamafile show`, `/rag show`, `/route use`, `/skill show`, `/skill-set new`, `/skill-set show`, `/model alias add` are all new subcommand aliases or additions.
- Existing verbs (`drop`, `rm`, `info`, `create`, `set`, `continue`) remain as aliases; no existing scripts or muscle-memory broken.
- `user_manual.md` and `getting-started.md` gain a "Command vocabulary" section explaining the eight verbs once, making every command family self-documenting.
- Tab completion `ArgCompletion` maps for each command are updated to list canonical verbs first.

---

## 2026-06-20 — Llamafile becomes the primary model backend; Ollama is secondary

**Context.** Harvey has supported both Llamafile and Ollama since v0.0.11, but startup logic, documentation, and default prompts all treat Ollama as the assumed backend. New users who want a fully local, no-server-required setup must discover Llamafile through man pages rather than finding it naturally in the startup flow. See [llamafile-primary-design.md](llamafile-primary-design.md).

**Decision.** Reverse the priority: at startup Harvey probes for an active Llamafile first, registered Llamafiles second, Ollama third. The model picker (shown when no session is being continued) lists Llamafile models above Ollama models. `getting-started.md` and `INSTALL.md` lead with the Llamafile path; Ollama is documented as an advanced alternative. Ollama support is fully retained — no existing config or commands change.

**Rejected alternatives.**

- *Keep Ollama as primary, improve Llamafile docs only* — documentation-only change leaves the startup UX inconsistent with the stated priority. New users still encounter Ollama first.
- *Detect "better" backend heuristically (GPU present → Llamafile, else Ollama)* — GPU detection is platform-specific and error-prone. User intent (registered a Llamafile → prefer Llamafile) is a cleaner signal.
- *Single `preferred_backend` setting in harvey.yaml* — adds config surface without improving the default experience for users who have not read the config docs.

**Consequences.**

- Startup probing order changes in `terminal.go` backend selection block.
- `getting-started.md` and `harvey-getting-started.7.md` are rewritten.
- The model picker presents Llamafile entries before Ollama entries.
- No breaking changes to `harvey.yaml` schema, API, or slash commands.

---

## 2026-06-20 — At-mention (`@model`) switches the active model while preserving history

**Context.** Switching models mid-session requires `/llamafile use NAME` or `/ollama use NAME`, which breaks conversational flow. Users who want a different model for the next question should be able to express that inline. The theatrical framing — a model switch is a new character entering the scene — also clarifies how downstream systems (memory miner, replay, plan executor) should handle boundaries. See [llamafile-primary-design.md](llamafile-primary-design.md).

**Decision.** If the REPL input begins with `@name` where `name` matches a registered Llamafile or Ollama model, Harvey switches to that model and forwards the remainder as the prompt. Conversation history is preserved unchanged. If `@name` is not recognised, the whole input is forwarded to the current model without warning (false positives on natural `@` mentions are rare enough that silent pass-through is less surprising than an error). Mid-session switches are recorded in the session file as `[[model switch: NAME (BACKEND) at TIMESTAMP]]` Fountain notes rather than starting a new session file — continuing in the same file preserves pre-switch context for memory mining and replay. The memory miner, session replay, and plan executor each gain logic to track model attribution across switch boundaries.

**Rejected alternatives.**

- *Error if `@name` is unknown* — would break natural-language inputs that begin with a person or file mention.
- *Require separator syntax `@name: rest`* — adds friction; a space is sufficient and consistent with how `@route` mentions already work in routing.
- *Start a new session file on switch* — orphans the pre-switch context; the `[[model switch: ...]]` note preserves the boundary without splitting the file.

**Consequences.**

- `terminal.go` REPL input handler gains an `@` prefix check before the `/command` check.
- `attemptModelSwitch(a, name, out)` looks up Llamafiles first, then Ollama models.
- `Recorder.RecordModelSwitch(model, backend)` writes a Fountain note at the switch point.
- `NewRecorder` gains a `Backend:` title-page field.
- Memory miner splits sessions at switch notes and attributes turns to the generating model.
- Session replay parses switch notes and performs mid-replay model switches.
- Plan executor supports `[model: name]` step annotations and restores the default model after each annotated step.

---

## 2026-06-20 — Unified `/model` command as a backend-agnostic delegating facade

**Context.** Users who switch between Llamafile and Ollama must remember which backend is active to choose the right command. As more backends are added (remote routes, encoderfiles), per-backend command proliferation increases cognitive load for users who just want to switch models. See [llamafile-primary-design.md](llamafile-primary-design.md).

**Decision.** Add `/model [list|use NAME|show NAME|status]` as a backend-agnostic facade. `/model use NAME` resolves the name by checking Llamafile models first, then Ollama models, then named routes, and delegates to the appropriate backend command. `/model list` merges all backends into one sorted table. The backend-specific commands (`/llamafile`, `/ollama`) are unchanged and remain the authoritative interfaces for backend-specific operations (`/llamafile start`, `/ollama pull`, etc.).

**Rejected alternatives.**

- *Deprecate `/llamafile` and `/ollama` in favour of `/model`* — too disruptive; power users and scripts depend on the specific subcommands.
- *`/model` with no subcommand shows an interactive picker* — inconsistent with Harvey's pattern: pickers appear when a required argument is omitted from a subcommand, not when the command itself is invoked without arguments.
- *Top-level `/use NAME`* — shorter but conflicts with the established convention that `use` appears only as a subcommand.

**Consequences.**

- `commands.go` gains a `"model"` registration; `cmdModel` dispatcher added.
- `/model use NAME` resolves across backends; no new switching code — delegates to existing handlers.
- `helptext.go` gains `ModelHelpText` and `ModelAliasHelpText` (the latter covering both `/model alias` subcommands and `@mention` switching; source for regenerating the currently sourceless `harvey-model-alias.7.md`).

---

## 2026-06-20 — Context utilization reads `n_ctx` from `/v1/models`; config override available

**Context.** A `[ctx: N%]` indicator requires knowing both the current token count (available from `ChatStats.PromptTokens`) and the model's maximum context window. Context window size is model-specific and not always available at runtime. See [llamafile-primary-design.md](llamafile-primary-design.md).

**Decision.** Priority order for context length: (1) `context_length` field on `LlamafileEntry` in `harvey.yaml` — explicit user override; (2) `data[0].meta.n_ctx` from the `/v1/models` API response — tested on Qwen3.5-2B, Qwen3.5-4B, and Apertus-8B, consistently present across all three model families; (3) `OllamaContextLength` on `Config`, already populated by `ShowModel`; (4) unknown — suppress the indicator entirely. The `n_ctx` value is the *runtime* context window (what llamafile loaded), not `n_ctx_train` (training context). When the probe succeeds and no user config is present, the result is stored in memory only — not written back to `harvey.yaml` — to avoid config churn on every startup.

**Rejected alternatives.**

- *Hardcode context lengths per known model family* — goes stale as model versions change; does not cover user-downloaded custom models.
- *Always show token count without percentage* — `[tokens: 4.2k]` is informative but gives no sense of urgency; percentage is more actionable for deciding when to `/clear`.
- *Use `n_ctx_train` as the window size* — this is the training context, which can be 4× larger than the runtime window. Using it would make the utilization % appear artificially low and mislead users.

**Consequences.**

- `LlamafileEntry` gains `ContextLength int \`yaml:"context_length,omitempty"\``.
- `llamafile_service.go` gains `ProbeLlamafileContextLength(url string) int` parsing `data[0].meta.n_ctx`.
- `terminal.go` appends `[ctx: N%]` to the post-turn status line when context length is known and non-zero.
- `CONFIGURATION.md` documents the new `context_length` field on `LlamafileEntry`.

---

## 2026-06-19 — Tab completion: two-layer design with shared SelectFrom helper

**Context.** Harvey's `buildCompleter()` only completes top-level command names, `@route` references, Ollama model names, and file paths. Users must remember subcommand names by heart and must know exact RAG store/model names to use `use` and `drop` subcommands. Several commands already show numbered pickers when no name is given, but each reimplements the pattern differently. See [tab-completion-design.md](tab-completion-design.md).

**Decision.** Extend completion in two layers: (1) second-token subcommand names using a new `Subcommands []string` field on `Command`; (2) third-token argument values using a new `ArgCompletion map[string]func(*Agent) []string` field that maps each subcommand to a candidate-list function. Additionally, introduce a shared `SelectFrom` / `SelectItem` / `SelectFromStrings` API in a new `ui.go` file. Commands whose first positional argument comes from a finite, enumerable list (`/rag use`, `/memory show`, `/llamafile use`, etc.) display the picker when no argument is given. `ui.go` lives in the `harvey` package; promotion to `termlib` is deferred until a clean generalisation is proven.

**Rejected alternatives.**

- *Parse Usage strings* — brittle; the Usage field is for display, not machine consumption. A `Subcommands` field is explicit and refactoring-safe.
- *Single CompletionFunc per Command* — more flexible but requires each command to handle prefix filtering, sorting, and the active-marker display pattern itself. The `ArgCompletion map[string]func` approach keeps candidate production separate from completion mechanics.
- *Fuzzy matching* — adds complexity without a proven need. Prefix matching is sufficient for short subcommand names; fuzzy can be added later without changing the API.
- *Move SelectFrom to termlib immediately* — premature. We don't know the right generalisation until it has been used in several places. Standard design → plan → decision process applies if/when that move happens.

**Consequences.**

- `Command` struct gains `Subcommands []string` and `ArgCompletion map[string]func(*Agent) []string`. The doc comment is updated. No existing registration is broken (new fields are optional).
- `buildCompleter()` gains two new blocks before the existing file-path switch. Existing file-path and model-name completion is unchanged.
- `ui.go` is a new file; `ui_test.go` covers all exported symbols.
- Existing picker implementations in `llamafile.go` and `commands.go` are refactored to call `SelectFrom` in Phase E. Behaviour is identical; code shrinks.
- Harvey YAML and configuration are not changed.

---

## 2026-06-18 — MinIO replaced with aws-sdk-go-v2 S3 client

**Context.** `remote_s3.go` uses `github.com/minio/minio-go/v7` as the S3 protocol client. MinIO's Go client has moved to a closed-source license, making it unsuitable for Harvey's AGPL-3.0 codebase. The affected surface is small: `Stat`, `Get`, and `List` operations on S3-compatible stores (AWS S3, MinIO server, Cloudflare R2). See [s3-replacement-design.md](s3-replacement-design.md).

**Decision.** Replace the MinIO client with `github.com/aws/aws-sdk-go-v2` (Apache-2.0 licensed). The AWS SDK v2 supports all S3-compatible endpoints via the `BaseEndpoint` override option. The call sites in `remote_s3.go` map cleanly: `StatObject` → `HeadObject`, `GetObject` → `GetObject`, `ListObjects` → `ListObjectsV2`. Credentials continue to come from environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`) and the SDK's default credential chain.

**Rejected alternatives.**

- *Minimal net/http + AWS Signature V4 from scratch* — eliminates the dependency but requires maintaining the signing and error-parsing logic. The AWS SDK already does this correctly for all S3 variants; hand-rolling it for three methods is low-leverage.
- *rclone/rclone as a library* — comprehensive but extremely heavy (~100+ package imports). Overkill for three read-only S3 operations.
- *Continue using MinIO client* — violates Harvey's open-source license requirements.

**Consequences.**

- `go.mod` removes `github.com/minio/minio-go/v7`, adds three `aws-sdk-go-v2` modules (`config`, `service/s3`, `credentials`).
- `remote_s3.go` is rewritten; public interface (`RemoteReader` implementation) is unchanged.
- Existing S3 URIs and `harvey.yaml` config fields are unaffected.
- AWS credential chain (env vars, `~/.aws/credentials`, IAM roles) works automatically.

---

## 2026-06-18 — Spinner gains dynamic status message channel

**Context.** Harvey's spinner currently shows rotating Edward Lear quotes and a timer while waiting for the LLM. Users have no way to tell whether Harvey is embedding a query, calling a tool, waiting for Ollama, or doing something else. Claude Code and similar tools display live status messages that update as work progresses. See [spinner-ux-design.md](spinner-ux-design.md).

**Decision.** Add a `StatusCh chan string` field to the `Spinner` struct and an `UpdateStatus(msg string)` method. The spinner's message line shows the most-recent status update instead of the next Lear quote whenever a message is pending; Lear quotes resume when no status is pending. The caller sends non-blocking updates via `UpdateStatus`; the spinner goroutine reads them on the fast tick. This preserves the existing Lear personality while surfacing actionable progress at key moments: tool call start/end, RAG embedding, context injection, model switching. Tab completion is out of scope for this work item; it is a separate, larger effort.

**Rejected alternatives.**

- *Replace Lear messages entirely with status strings* — loses the personality that distinguishes Harvey from generic CLI tools. The mixed approach preserves Lear for idle periods.
- *Print status on a separate line below the spinner* — requires the spinner to know its vertical position relative to other output, which it does not; scrolling behavior would be unpredictable.
- *Atomic string (sync/atomic or sync.Mutex)* — functionally equivalent but a channel fits Harvey's existing goroutine patterns and avoids a lock.

**Consequences.**

- `spinner.go` adds `StatusCh chan string`, `UpdateStatus(string)`, and `lastStatus string` to the `Spinner` type.
- `terminal.go` calls `UpdateStatus` at: RAG embedding start, tool call start, tool call complete, context injection.
- The message line now shows status text (dim green) when present; falls back to a Lear quote (colored) when idle.
- No change to the timer or frame tick behavior.

---

## 2026-06-18 — Assay evaluation output moves to workspace-level directory

**Context.** `bin/assay` writes evaluation results to `testout/` inside the `harvey/` source repository. This directory is gitignored, but the JSON and Markdown artifacts look like test output to language models that read the source tree, causing models to misinterpret stale evaluation results as current test failures. See [assay-llamafile-design.md](assay-llamafile-design.md).

**Decision.** Change the default output directory for `bin/assay` from `testout/` to `$WORKSPACE/assay-results/<timestamp>/` where `$WORKSPACE` is resolved the same way Harvey resolves its workspace (walk up from cwd to the directory containing `agents/harvey.yaml`). If no workspace is found, fall back to a `assay-results/` directory in the current working directory. The `--output` flag overrides the default as before.

**Rejected alternatives.**

- *Keep output in `testout/` but add a note file* — models still read and misinterpret the directory.
- *Always require `--output` flag* — breaks existing workflows that rely on the default.
- *Use `$XDG_DATA_HOME/harvey/assay-results/`* — correct in principle but separates results from the workspace they were generated against, making correlation harder.

**Consequences.**

- `cmd/assay/main.go` gains workspace discovery logic (same heuristic as Harvey's `NewWorkspace`).
- Default report and results paths change; documented in `--help` output.
- `testout/` in the harvey repo is no longer populated by `bin/assay` in normal use.

---

## 2026-06-18 — Assay adds Llamafile backend via `--llamafile` flag

**Context.** `bin/assay` currently only supports Ollama as a model backend, but Harvey supports both Ollama and Llamafile. Users evaluating a Llamafile model must run it manually and point assay at it with a custom URL, which is error-prone and undocumented. See [assay-llamafile-design.md](assay-llamafile-design.md).

**Decision.** Add a `--llamafile PATH` flag to `bin/assay`. When provided, assay starts the llamafile process on an ephemeral port (same `startLlamafile` logic as in `llamafile_service.go`), runs the evaluation suite against that endpoint, then terminates the process on exit. The `--model` flag is still respected (it sets the model name in the report) but `--ollama` is ignored when `--llamafile` is given. Embeddings continue to use the Ollama embedder unless `--rag-db` is also given and the store's recorded embedding model differs, in which case the operation fails fast with a clear error.

**Rejected alternatives.**

- *Separate `assay-llamafile` binary* — duplicates 95% of the evaluation harness; not maintainable.
- *Auto-discover a running Llamafile process* — fragile; depends on port conventions that are not enforced.
- *Require user to start Llamafile and pass URL* — current workaround; acceptable as an escape hatch but the `--llamafile` flag makes the common case ergonomic.

**Consequences.**

- `cmd/assay/main.go` imports `llamafile_service.go` functions already in the package; no new files needed.
- Llamafile process is always terminated on assay exit, even if evaluation panics (deferred cleanup).
- The report header records the llamafile path and version alongside the model name.

---

## 2026-06-18 — Web developer template added to built-in profile set

**Context.** The five templates shipped in v1 (`backend-developer`, `frontend-developer`, `dataset-developer`, `data-scientist`, `technical-writer`) do not have a template that covers the full polyglot web development stack used in this workspace: Go backends, uv-managed Python scripts, SQL (SQLite3 and Postgres), Deno+TypeScript frontends, and vanilla JavaScript/CSS/HTML5. A backend developer using Deno or a frontend developer writing Go API clients currently reaches for an incomplete template. See [web-developer-template-design.md](web-developer-template-design.md).

**Decision.** Add a `web-developer.spmd` template to `templates/profiles/`. It covers: Go (net/http, database/sql), uv+Python (scripting, data processing), SQL (SQLite3 dialect and Postgres), Deno+TypeScript (runtime, standard library, no bundler by default), JavaScript (ES modules, no framework by default), CSS (custom properties, no utility framework by default), HTML5 (semantic markup). The template's `NOTE:` recommends `qwen2.5-coder:7b` or `granite3.3:2b` and suggests ingesting both Go source and the `deno.json`/`package.json` for context.

**Rejected alternatives.**

- *Extend the existing `backend-developer` template* — the existing template is already a good fit for pure Go/Python/SQL work; adding Deno and CSS would make it too broad and undermine the template picker's value as a role-specific starting point.
- *Split into `go-web` and `deno-web` templates* — two templates for what is effectively one stack in this workspace is unnecessarily granular.

**Consequences.**

- `templates/profiles/web-developer.spmd` is added to the embedded binary.
- The onboarding template picker shows a seventh option.
- No code changes required; `ListTemplates()` discovers it automatically.

---

## 2026-06-18 — `/memory profile` subcommand set expanded and naming standardized

**Context.** The current `/memory profile` command has three subcommands — `show`, `update`, `use` — but their semantics do not match Harvey's established command vocabulary. `show` lists active profiles (like `list` does elsewhere) rather than showing the *content* of the active profile. `use` creates a new profile from a template (like `new` does elsewhere) rather than selecting an existing saved profile. `update` opens the current profile in `$EDITOR`. There is no way to rename a workspace. See [memory-profile-ux-design.md](memory-profile-ux-design.md).

**Decision.** Standardize the subcommand set:

| Subcommand | New behaviour | Was |
|---|---|---|
| `list` | List all profiles (active + archived) | (`show` partial) |
| `show` | Print the *content* of the current active profile | (missing) |
| `edit` | Open the active profile in `$EDITOR` (rename of `update`) | `update` |
| `use [NAME]` | Switch to a named template or picker | unchanged |
| `rename NAME` | Rename the workspace display name in the active profile | (missing) |

`update` is kept as a deprecated alias for `edit` with a one-line deprecation notice, to avoid breaking existing workflows. The `/profile` top-level alias continues to delegate to all subcommands. The help text for `/memory` is updated to list all five subcommands.

**Rejected alternatives.**

- *Rename `use` to `new` to match the `new/list/use` pattern elsewhere* — `/profile use` is already shipped, documented, and matches `/ollama use`, `/rag use`. Breaking the alias would confuse users more than the current inconsistency.
- *Keep `show` with list semantics* — defeats discoverability; users type `/memory profile show` expecting to see what their profile says, not a list of IDs.

**Consequences.**

- `commands.go`: `cmdMemoryProfile` gains `list`, `rename`, and `show` (content-display) cases. `show` (old list behavior) becomes `list`. `update` remains as alias for `edit`.
- `helptext.go`: memory and profile help text updated.
- `harvey-memory.7.md`: man page updated to document all five subcommands.

---

## 2026-06-18 — PDF capability disclosed in HARVEY.md system prompt

**Context.** Harvey's `read_file` built-in tool description states that PDF files are extracted automatically via poppler. But when tools are disabled — or when a small model uses prose tool calls and does not consistently read all tool descriptions — the model has no knowledge of this capability and asks the user to manually convert PDFs to text. HARVEY.md is always injected as the system prompt, making it the correct place to disclose capabilities that should be known regardless of tool-call mode. See [quick-fixes-design.md](quick-fixes-design.md).

**Decision.** Add a **File reading capabilities** section to `HARVEY.md` that enumerates what Harvey can read without conversion: plain text, Markdown, Go/TypeScript/Python source, and PDF (extracted via poppler automatically). This mirrors the pattern of the existing "Tagged code blocks" section — documenting Harvey's automatic behaviors so the model can confidently use them rather than guessing.

**Rejected alternatives.**

- *Only fix the `read_file` tool description* — already done; the problem is the model doesn't see tool descriptions when tools are disabled.
- *Inject a capability summary at each turn* — wasteful in context tokens; a one-time system prompt disclosure is sufficient.
- *Print a reminder when the user asks about a PDF* — reactive; the bug is the model prompting the user to convert, not the user asking Harvey.

**Consequences.**

- `HARVEY.md` gains a short "File reading" section (4-6 bullet points).
- No code changes required; `HARVEY.md` is loaded by `LoadHarveyMD()` at startup.
- Models that previously asked users to convert PDFs will instead use `read_file` directly.

---

## 2026-06-18 — Llamafile model discovery includes Windows .exe extensions

**Context.** `scanLlamafileModels()` in `llamafile.go` uses `strings.HasSuffix(e.Name(), ".llamafile")` to identify llamafile binaries. On Windows, llamafile binaries end in `.exe` (plain) or `.llamafile.exe` (when distributed with the double extension). Users on Windows who place binaries in `~/Models` see an empty picker even with valid models present. The same bug affects `llamafileModelName`, which only strips the `.llamafile` suffix and leaves `.exe` on Windows paths. See [quick-fixes-design.md](quick-fixes-design.md).

**Decision.** Extend `scanLlamafileModels` to match three patterns: `.llamafile`, `.llamafile.exe`, and (on Windows only) any `.exe` file in the models directory. `llamafileModelName` is updated to strip suffixes in the correct order: strip `.exe` first (if present), then `.llamafile` (if present). The `llamafileDefaultModelsDir()` platform function already returns the correct OS-appropriate path; no change needed there.

**Rejected alternatives.**

- *Require users to rename binaries on Windows* — poor UX; llamafile project ships `.exe` files and users should not need to rename them.
- *Add a config field for custom extensions* — over-engineering a simple extension check.
- *Match all `.exe` files unconditionally* — would pick up non-llamafile executables; restrict to `.exe` only when the scan finds no `.llamafile` or `.llamafile.exe` files, or only match `.exe` files that also check for the llamafile magic bytes (deferred to a future improvement).

**Consequences.**

- `llamafile.go`: `scanLlamafileModels` matches `.llamafile`, `.llamafile.exe`, and `.exe` (Windows-only guard); `llamafileModelName` strips suffixes in the correct order.
- Windows users with binaries in `~/Models` now see them in the picker.
- No change to Linux/macOS behavior.

---

## 2026-06-18 — `--resume` flag auto-selects the most recent session

**Context.** Harvey's `--continue PATH` flag resumes from a specific session file. When the user simply wants to pick up where they left off (the most common case), they must find and type the session path, or navigate the interactive picker. Both are unnecessary friction when the intent is always "resume my last session." See [quick-fixes-design.md](quick-fixes-design.md).

**Decision.** Add a `--resume` flag (no argument) that resolves to the most recently modified `.spmd` file in `agents/sessions/` and sets `cfg.ContinuePath` to that path before `Run`. If no sessions exist, Harvey prints a one-line notice and starts fresh. The implementation delegates entirely to the existing `ContinueFromFountain` path — no new session-loading logic is needed.

**Rejected alternatives.**

- *Make `--continue` with no argument mean "most recent"* — changes the semantics of an existing flag; would break scripts that pass `--continue` expecting a required argument.
- *Add `--resume` as an alias for opening the interactive picker* — the picker is useful for choosing among multiple sessions; `--resume` should be zero-friction and not prompt.

**Consequences.**

- `cmd/harvey/main.go` gains a `--resume` case that calls a new `mostRecentSession(sessDir string) string` helper.
- `harvey.go` or `sessions_files.go` gains `mostRecentSession` (walks `agents/sessions/`, returns path of newest `.spmd` by `ModTime`).
- No change to `--continue` semantics.
- If called with `--record`, the resumed session is not re-recorded (existing guard in `terminal.go:333-338` already handles this).

---

## 2026-06-09 — Programming language support uses a central LanguageRegistry with pluggable handlers

**Context.** Harvey's RAG system already supports ingesting 17 programming language file extensions (commands.go:4975-4979), but the `looksLikePath` function (commands.go:3463-3467) was missing extensions for C, C++, Pascal, Oberon, Lisp, and Basic. Additionally, all languages used generic paragraph-based chunking which breaks code structures (functions, procedures) across chunk boundaries, reducing RAG retrieval quality for programming queries. Users working with source code need language-aware features: code-aware chunking, documentation extraction, syntax highlighting, and auto-formatting.

**Decision.** Create a comprehensive language support system with the following architecture:

1. **Central LanguageRegistry** (`language_registry.go`) — Maps language identifiers to handlers (detectors, chunkers, extractors, formatters, highlighters). Each language has a `LanguageInfo` struct with metadata (name, extensions, comment markers, block delimiters, capabilities).

2. **Pluggable Interfaces** — Define Go interfaces for each capability:
   - `LanguageDetector` — Identifies language from file path and/or content
   - `CodeChunker` — Splits source into meaningful units (functions, classes, procedures)
   - `DocExtractor` — Extracts comments, docstrings, and symbol documentation
   - `CodeFormatter` — Formats source code according to language conventions
   - `SyntaxHighlighter` — Adds ANSI color to code blocks for terminal display

3. **Code-Aware Chunking** — Language-specific chunkers that respect code structure:
   - C/C++: Split at function boundaries, preserve preprocessor directives and structs
   - Pascal: Split at PROCEDURE/FUNCTION boundaries, preserve TYPE/RECORD definitions
   - Oberon: Split at MODULE/PROCEDURE boundaries
   - Lisp: Split at top-level forms (balanced parentheses), keep DEFUN/DEFMACRO together
   - Basic: Split at SUB/FUNCTION boundaries

4. **Progressive Enhancement** — All features are opt-in. Basic file I/O works for all languages. If a language-specific handler fails, fall back to generic behavior.

5. **Immediate Fix** — Add missing extensions (`.c`, `.cpp`, `.h`, `.hpp`, `.pas`, `.Mod`, `.obn`, `.lisp`, `.bas`) to `looksLikePath` function for tagged code block detection.

**Rejected alternatives.**

- *Use Tree-sitter for all parsing* — Tree-sitter provides excellent AST-based parsing but adds ~5MB per language grammar, significant build complexity, and external dependencies. Rejected in favor of simpler regex-based and state-machine approaches for initial implementation, with Tree-sitter as a future enhancement.

- *Single monolithic chunker* — One chunker handling all languages with conditional logic. Rejected for being hard to maintain, test, and extend. The interface-based approach allows independent development and testing of each language's chunker.

- *Cloud-based language services* — Use external APIs for formatting, analysis, etc. Rejected for violating Harvey's local-first philosophy and introducing privacy/security concerns (sending user code to external services).

- *Mandatory formatting* — Always format code on write without user control. Rejected for being too opinionated and potentially breaking user workflows. Auto-formatting must be opt-in and configurable.

**Consequences.**

- **File Changes:** New files `language_registry.go`, `code_chunkers.go`, `doc_extractors.go`, `syntax_highlighters.go`, `code_formatters.go` with corresponding test files. Modified `commands.go`, `config.go`, `builtin_tools.go`, `terminal.go`.

- **Backward Compatibility:** Existing RAG stores continue to work. Generic chunking remains as fallback. No breaking changes to SQLite schema or session format.

- **Performance:** Language registry initialization at startup adds < 10ms. Chunking with language-specific handlers adds ~10-20% overhead vs. generic chunking. Formatters only invoked when auto-format is enabled.

- **Extensibility:** New languages can be added by implementing the interfaces and registering them, without modifying core code.

- **Improved RAG Quality:** Code-aware chunking preserves function/procedure boundaries, improving retrieval quality for code-related queries by an estimated 20%+ over generic chunking.

- **Better UX:** Syntax highlighting in terminal output and auto-formatting on file write improve the user experience when working with source code.

---

## 2026-06-09 — Code block path detection (`looksLikePath`) extended to support all RAG-ingestible languages

**Context.** The `looksLikePath` function in `commands.go` (lines 3463-3467) determines whether a string looks like a file path rather than a language identifier. This is used by `fencePathToken` when parsing tagged code blocks (e.g., ````c:program.c`). The function had a hardcoded list of known extensions that was missing: `.c`, `.cpp`, `.h`, `.hpp`, `.pas`, `.Mod`, `.obn`, `.lisp`, `.bas`. This meant that tagged code blocks for these languages were not recognized as file paths, preventing the auto-write feature from working.

**Decision.** Extend the `knownExts` slice in `looksLikePath` to include all extensions supported by RAG ingestion (from `ragIngestableExts` in commands.go:4975-4979). Additionally, add a comment noting that these are programming languages supported by RAG ingestion for future maintainability.

**Rejected alternatives.**

- *Refactor to use the language registry* — While this would be more maintainable long-term, it would introduce a circular dependency (the registry isn't initialized when `looksLikePath` is first used during startup). Deferred to a future cleanup.

- *Create a separate list* — Maintain a separate, parallel list of extensions. Rejected for creating a maintenance burden and potential for divergence.

- *Make it dynamic* — Load extensions from configuration. Rejected as over-engineering for a static list that rarely changes.

**Consequences.**

- Tagged code blocks for all RAG-supported languages now work correctly, e.g., ````c:src/main.c` or ````pascal:module.pas`.

- The hardcoded list remains a maintenance point but now includes all 17 supported languages.

- Future additions to RAG ingestion must remember to also update `looksLikePath`. This is documented in the code comments.

---

## 2026-06-08 — `/loop` chat iterations use a shared `runChatTurn` helper that skips skill auto-trigger and `autoExecuteReply`

**Context.** The REPL's plain-chat path does more than call the model: it
checks whether the input matches a skill trigger pattern (auto-dispatching to
a different flow entirely), and after the reply, offers to write fenced code
blocks to disk via an interactive Y/n prompt (`autoExecuteReply`). Both make
sense for a human typing one message at a time; both are problematic when the
same prompt is sent N times unattended — a skill could fire on iteration 3
but not iteration 1, and a Y/n prompt would block forever waiting on stdin
that nothing will type.

**Decision.** Factor the REPL's inline chat block (`terminal.go`, roughly
lines 635-820) into a shared `(a *Agent) runChatTurn(ctx, input, out) (reply
string, stats ChatStats, err error)`. It keeps everything that defines "how
Harvey answers a prompt" — RAG augmentation, the tool-loop-or-plain-chat
branch, token/context warnings, stats, Fountain recording — and excludes
skill auto-trigger matching and `autoExecuteReply`, both of which belong to
"how the REPL reacts to a typed line." `/loop` calls this helper directly for
its chat-mode iterations; the REPL becomes a thin wrapper around the same
helper plus its own skill-trigger/`autoExecuteReply` handling.

**Rejected alternatives.**

- *Reuse the REPL's inline chat block as-is* — a looped prompt could silently
  jump to a different skill mid-run, or stall on iteration 1 waiting for a
  keypress that never comes.
- *Duplicate the chat block inside `cmdLoop`* — roughly 150 lines of
  copy-paste that would drift from the REPL's version on the next change to
  the chat path.

**Consequences.**

- `terminal.go`'s plain-chat branch is refactored but behaviourally unchanged
  for normal typed input — verified with `go test -race` after extraction.
- `/loop` behaves predictably: the same prompt produces the same kind of
  exchange every time, with no surprise skill redirects or stalled prompts.
- If `a.Config.ToolsEnabled`, looped prompts can still cause the model to
  write files or run commands via the normal tool loop — `/loop` does not
  suppress this, since doing so would make looped chat behave differently
  from normal chat (see `loop-design.md`, "Safety Considerations").

---

## 2026-06-08 — `/loop` caps iterations at 100 and defaults to 10

**Context.** `/loop` is the first Harvey command that can run LLM calls — and,
with tools enabled, write files or execute shell commands — repeatedly and
unattended. Harvey's existing security posture (safe mode, permission system,
audit log) is built around bounding and surfacing risky actions rather than
trusting the user to always type the right thing.

**Decision.** `/loop` takes an optional `--count N` (following the
`--depth N` convention already established by `/read-dir`), defaulting to 10
and capped at 100. There is no "run forever" option.

**Rejected alternatives.**

- *Unbounded by default* — the one command that could turn a typo
  (`/loop 1s tell me a joke`) into thousands of unattended LLM calls before
  the user notices.
- *Confirmation prompt before starting* — adds a keypress without adding much
  safety; the printed plan summary (`Looping every 5m, up to 10 times: ...`)
  gives the same "last chance to Ctrl+C" moment without an extra interaction
  step, consistent with how `/pipeline` announces its plan before running.

**Consequences.**

- A fully unattended `/loop` run is bounded to at most 100 iterations — e.g.
  roughly 8 hours at a 5-minute interval — which still covers realistic
  "check on this periodically" use cases.
- Users who need more must re-invoke `/loop`, a deliberate speed bump rather
  than an oversight.

---

## 2026-06-08 — `/loop` requires an explicit interval; no self-pacing mode

**Context.** Claude Code's `/loop` can omit the interval and let the agent
self-pace via a wake-scheduling primitive. Harvey has no equivalent — it is a
synchronous CLI process with no persistent scheduler or "wake me up later"
mechanism.

**Decision.** `INTERVAL` is a required first argument to `/loop`, parsed with
the existing `parseDurationString` helper (`config.go:650`, already used for
`run_timeout`/`ollama_timeout` in `harvey.yaml`). There is no self-pacing
mode.

**Rejected alternatives.**

- *Have Harvey "guess" an interval once and run at that fixed cadence* — just
  a worse version of asking the user, with an extra layer of
  unpredictability.
- *Keep the process resident and let it wake itself* — a fundamentally
  different program shape than Harvey's synchronous REPL; far outside the
  scope of adding one command.

**Consequences.**

- `/loop`'s usage string and help text always show `INTERVAL` as required.
- Users coming from Claude Code's `/loop` will notice the difference; the
  help text explains why (no async scheduler in Harvey).

---

## 2026-06-08 — `/loop` runs as a blocking foreground command, not a background goroutine

**Context.** Harvey's REPL (`terminal.go:Run`) is a single-threaded loop that
blocks on each turn, mutating `a.History`, `a.Recorder`, and the shared output
writer with no locking — because nothing has ever run concurrently with it.
Adding a command that repeats a prompt on an interval raises the question of
whether it should run in the background while the user keeps typing, or take
over the REPL until it finishes.

**Decision.** `/loop` runs in the foreground inside its own command handler,
reusing the SIGINT-cancellation pattern already used three times in
`terminal.go` (chat, `!` commands, `@mention` dispatch): one cancellable
context for the whole run, a goroutine watching `os.Signal`, and a
`wasCancelled` check. Any Ctrl+C — mid-iteration or during the
inter-iteration sleep — stops the whole loop and returns to the prompt.

**Rejected alternatives.**

- *Background goroutine* — would require introducing locking around
  `a.History`, `a.Recorder`, and `out`, none of which exist today. The
  concurrency-safety surface this opens is large relative to the value of
  letting the user type while the loop runs.
- *"Ctrl+C cancels the iteration; a second Ctrl+C stops the loop"* — a second
  control surface nothing else in Harvey has; rejected for consistency with
  the existing single-Ctrl+C-aborts convention.

**Consequences.**

- `/loop` blocks the REPL for its duration — communicated up front via a
  printed plan summary before the first iteration runs.
- `/loop status` / `/loop stop` subcommands aren't meaningful (the REPL can't
  read them while blocked) and are not implemented.
- No new synchronization primitives are introduced anywhere in Harvey.

---

## 2026-06-05 — Profile templates and help guides ship embedded in the binary

**Context.** Harvey installs by copying a single executable to `$HOME/bin`. Users on three OS / two CPU architectures should not need to install a separate asset package. Templates and help guides must therefore travel with the binary.

**Decision.** Use Go's `//go:embed` directive (standard library since Go 1.16) to compile a `templates/` directory tree into the binary at build time. A single `EmbeddedTemplates embed.FS` variable in `templates.go` gives the rest of Harvey read access to template and help guide content at runtime. Workspace-local templates in `agents/templates/profiles/` are checked at runtime and merged with the built-in list, allowing organisations to add shared templates without patching Harvey.

**Rejected alternatives.**

- *Separate asset directory alongside the binary* — breaks the single-file install model.
- *Download templates from the internet on first run* — requires network access, adds failure modes, complicates offline use on a Raspberry Pi.
- *Templates in `harvey.yaml`* — templates are multi-line prose documents; embedding them in YAML is unreadable and fragile to edit.

**Consequences.**

- `templates/` directory added to the Harvey source tree; must be maintained alongside code.
- Binary size increases modestly (six `.spmd` files and three Markdown guides are small).
- `templates.go` is the single registration point for all embedded assets.

---

## 2026-06-05 — Initial developer/writer template set; library templates deferred

**Context.** Harvey needs a useful starting set of profile templates but the full range of library staff roles requires domain expertise and UX review that is not yet available.

**Decision.** Ship five developer/writer templates for v1:

| Template | Role |
|----------|------|
| `backend-developer` | Go, Python, TypeScript+Deno, SQL for application work |
| `frontend-developer` | HTML, CSS, TypeScript/JavaScript, Deno bundling |
| `dataset-developer` | Front end plus SQL, dataset CLI, datasetd web service |
| `data-scientist` | Data analysis, SQL for exploration, Python data tooling |
| `technical-writer` | Documentation, man pages, tutorials, Markdown and Fountain |

Library role templates (subject specialist, systems/digital, instruction/data literacy, support staff) are deferred until library staff and a UX colleague can define the categories and content correctly. Placeholder files are named in the plan but not authored.

**Rejected alternatives.**

- *Ship library templates based on external assumptions* — risks producing templates that do not match how library staff actually work, which would undermine trust in the feature.

**Consequences.**

- Library users who try Harvey before the library templates ship will use `blank.spmd` or one of the developer templates as a starting point. Acceptable short-term.

---

## 2026-06-05 — `/profile use` verb; `/profile` top-level alias

**Context.** The profile switching command needed a name consistent with Harvey's existing command vocabulary. Two candidates were considered: `switch` and `use`.

**Decision.** Use `use` as the subcommand verb because it matches the established pattern in Harvey: `/ollama use`, `/rag use`, and `/kb use` all select the active item from a list. Register `/profile` as a top-level alias delegating to `/memory profile`, following the same one-line handler pattern as `/recall` → `/memory recall`.

**Rejected alternatives.**

- *`/profile switch`* — `switch` does not appear elsewhere in Harvey's command vocabulary. `use` is already the selection verb.
- *`/switch-profile` or `/change-profile`* — hyphenated commands are not the Harvey convention.

**Consequences.**

- `commands.go` gains a `"profile"` entry in the top-level command table (identical in structure to `"recall"`).
- `cmdMemoryProfile` gains a `"use"` dispatch case.
- `/memory profile use`, `/profile use`, and `/profile` (showing subcommand help) all work.

---

## 2026-06-05 — Profile switching writes a Fountain handoff document

**Context.** When a user switches profiles mid-session with `/profile use`, the in-progress conversation context would be lost after `ClearHistory()`. The user may need to resume the previous context in a future session.

**Decision.** Before clearing history, `/profile use` writes a `.spmd` summary file to `agents/hand-off/<timestamp>.spmd`. The handoff captures the last N assistant messages as bullet points and lists file paths and open questions from recent turns. No LLM call is required — the handoff is structural, not summarised. Because it is a `.spmd` file, the memory miner can extract facts from it in a later session, migrating context from the old role into the new session's experience memories over time.

The previous `workspace_profile` document is archived (status set to `archived`) rather than deleted, preserving the history of who this workspace has been used as.

**Rejected alternatives.**

- *No handoff* — context is lost on profile switch; acceptable only if profiles are rarely switched.
- *LLM-generated summary* — higher quality but requires a blocking model call during the switch, adding latency and a failure mode.
- *Write handoff to the session file* — session files record conversation turns, not profile transitions; mixing them would complicate the memory miner.

**Consequences.**

- `agents/hand-off/` directory is created at workspace init alongside `agents/sessions/`.
- `writeHandoff()` function added to `harvey.go`.
- Memory miner learns to process files from `agents/hand-off/` as well as `agents/sessions/`.

---

## 2026-06-05 — Help guides for Ollama and PDF tools embedded in binary

**Context.** New users frequently fail to install Ollama or PDF extraction tools before running Harvey. The error messages Harvey currently produces do not explain what is missing or how to fix it. Users on three operating systems need platform-specific install instructions.

**Decision.** Embed short Markdown help guides (`templates/help/ollama.md`, `templates/help/pdf-tools.md`) in the binary using the same `//go:embed` infrastructure as profile templates. Surface them via `/help ollama` and `/help pdf-tools`. Print a one-line pointer to the relevant guide when a detection failure occurs at startup (Ollama unreachable) or during a command (PDF extraction fails). Guides are deliberately short: what it is, how to install on each platform, one troubleshooting line.

**Rejected alternatives.**

- *Link to external documentation only* — requires network access to get help; unhelpful in offline or restricted environments.
- *Inline error messages only* — install instructions for three platforms embedded in Go string literals are unmaintainable; Markdown guides are editable without touching code.

**Consequences.**

- `templates/help/` directory contains three Markdown files maintained alongside the code.
- `helptext.go` gains `OllamaHelpText` and `PDFToolsHelpText` helpers.
- `terminal.go` and `pdf_extract.go` each gain one conditional pointer line.

---

## 2026-06-02 — Persistent command history across sessions

**Context.** Harvey's `termlib.LineEditor` supports Up/Down arrow history navigation within a session, but the history is in-memory only and lost on exit. Users must retype slash commands, `!` shell commands, and prompts from prior sessions, which breaks flow — especially for repeated workflows like `/rag ingest`, `/memory mine`, or iterating on a prompt.

**Decision.** Persist the input history to `agents/harvey_history` inside the workspace (one entry per line, plain text). On startup Harvey loads this file and seeds the `LineEditor` before entering the REPL. On clean exit the in-memory history is written back, capped at **1000 entries** (most recent kept). Consecutive duplicate suppression is already handled by `AppendHistory`; no further deduplication is applied at write time.

The implementation requires two changes:

1. **`termlib` (`lineeditor.go`)** — add two methods to `LineEditor`:
   - `SetHistory(lines []string)` — replaces the in-memory history slice wholesale (used at startup).
   - `History() []string` — returns a copy of the current history slice (used at exit to write back).

2. **Harvey (`terminal.go`)** — add `loadCmdHistory(ws, le)` called after `le` is created (line ~225), and `saveCmdHistory(ws, le)` called in the REPL exit path. Both functions resolve the path as `ws.AbsPath("agents/harvey_history")`. `saveCmdHistory` truncates to the last 1000 entries before writing.

The history file path is not configurable in this iteration; `agents/` is Harvey's conventional home for all runtime state (`harvey.yaml`, `sessions/`, `memories/`, `rag/`, `knowledge.db`).

**Rejected alternatives.**

- *Global `~/.harvey_history`* — shares history across workspaces, which leaks commands and paths between projects. Harvey's workspace-boundary model makes per-workspace the correct scope.
- *Storing history in `agents/harvey.yaml`* — would pollute the config file with ephemeral runtime data and complicate config schema evolution.
- *Parsing `.spmd` session files for history* — session recordings are conversation transcripts, not command logs; extraction would be fragile and slow.

**Consequences.**

- `termlib/lineeditor.go` gains `SetHistory` and `History` methods.
- `harvey/terminal.go` gains `loadCmdHistory` and `saveCmdHistory` helper functions wired into the REPL startup and exit.
- No changes to `harvey.yaml` schema, `Config`, or any other subsystem.
- Concurrent Harvey sessions in the same workspace will silently overwrite each other's history on exit (last-writer-wins), consistent with bash's behaviour without `HISTFILE` locking.

---

## 2026-06-02 — UX nudge system for memory discoverability

**Context.** Users who understand the three storage silos (RAG / Memory / Knowledge Base) can get significantly better results, but the ingestion decision ("where does this go?") breaks flow. No built-in mechanism surfaced actionable hints about pending mining, empty RAG stores, or RAG being disabled.

**Decision.** Implement a four-part nudge system:

1. **Session-start digest** — a `sessionMemoryDigest()` function called after the ready line that prints dim hints only when a condition is actionable:
   - Unmined sessions pending → suggest `/memory mine`
   - Active RAG store is empty → suggest `/rag ingest`
   - RAG off but chunks exist → suggest `/rag on`
   No output is printed when everything looks healthy.

2. **Enhanced `/status`** — extend `cmdStatus` with a Memory/RAG summary block (active memories, unmined sessions, active store, chunk count, RAG on/off). Keeps the one-stop status view complete.

3. **New `/hint` command** — on-demand improvement suggestions that aggregate all three silos and explain the decision rule. Verbose version of the session digest with context about *why* each suggestion matters.

4. **`/help learn` topic** — a unified "How Harvey learns" help page with a three-column table (what to ingest → which command → where it goes) and the single decision rule:
   - Have a text file or document? → `/rag ingest`
   - Something useful happened in a session? → `/memory mine`
   - Making an observation about an experiment? → `/kb observe`

5. **`/recall` alias** — routes to `/memory recall` to make the unified retrieval interface the obvious entry point.

**Rejected alternatives.**
- *Single storage silo* — would reduce configuration but lose retrieval precision for small models. Topic-scoped RAG stores (e.g., `deno_typescript`, `go`) give better recall than one large mixed store.
- *Always-on verbose status* — printing all memory info on every startup is too noisy. Only surface hints when actionable.
- *Merging `/rag on` + `/memory recall` into a single toggle* — the per-prompt RAG injection (`ragAugment`) and session-start injection (`UnifiedMemory.Recall`) are different channels. A single toggle would require auditing whether `UnifiedMemory` already includes RAG chunks. Deferred to a future audit.

**Consequences.**
- `terminal.go` gains a `sessionMemoryDigest()` call after the ready line.
- `commands.go` gains `cmdHint`, enhanced `cmdStatus`, and a `/recall` registration.
- `helptext.go` gains `LearnHelpText`.
- `cmdHelp` dispatches `"learn"` and `"memory-overview"` to `LearnHelpText`.
- `help` topic list is updated to include `learn`.

---

## 2026-06-02 — model_map in RAG stores (deferred simplification)

**Context.** Each RAG store entry in `harvey.yaml` has a `model_map` field that maps generation models to embedding models. In practice every store uses `nomic-embed-text` for all generation models, making the map redundant.

**Decision.** Deferred. Do not remove `model_map` now. The code is already correct and operational. Remove it when there is a concrete reason to simplify the config schema (e.g., adding a new embedder type that makes the override meaningful).

**Consequences.** `model_map` remains in the config and `ragAugment` continues to honour it. No user-visible change.

---

## 2026-06-02 — Dual RAG injection audit (deferred)

**Context.** Harvey has two RAG injection paths that run independently:
1. Per-prompt via `ragAugment()` in `terminal.go` (when `a.RagOn`)
2. Session-start via `UnifiedMemory.Recall()` which also queries the RAG store

A user with both `memory.enabled` and `rag.enabled` may receive RAG content twice per turn — once in the system prompt injection and once prepended to each prompt. This wastes context tokens and may confuse small models.

**Decision.** Deferred. Audit and fix when a user observes noticeably degraded context efficiency. The fix would be to either: (a) skip RAG chunks in `UnifiedMemory.Recall()` when `a.RagOn` is true, or (b) make `ragAugment` a no-op when `UnifiedMemory` already injected from the same store.

**Consequences.** Known overlap. No immediate action required.

---

## 2026-05-31 — prose tool call correction injection

**Context.** Small models emit tool calls as JSON fenced blocks rather than structured API responses. The original `tryExecuteProseToolCalls` returned `bool` and could not distinguish "dispatched successfully" from "dispatched but every call errored". When models hallucinated tool names the warning was suppressed because `len(results) > 0` was always true.

**Decision.** Change `tryExecuteProseToolCalls` to return `(dispatched bool, unknownNames []string)`. Track a `succeeded` counter internally; set `dispatched = true` only when ≥1 call succeeded. When `unknownNames` is non-empty, inject a correction message into history *after* `a.AddMessage("assistant", ...)` so history ordering is: user → assistant → correction-user. This gives the model a chance to retry with the correct tool names.

**Consequences.** The `noToolCalls` guard also gates `autoExecuteReply` to prevent directory-tree code blocks from being offered as files to write after successful tool-call turns.

---

## 2026-05-31 — histLenBeforeChat pattern for noToolCalls guard

**Context.** Harvey needs to know whether a chat turn resulted in structured tool calls (via `RunToolLoop`) so it can skip `autoExecuteReply` when tool calls already handled file writing. The check `len(a.History) == histLenBeforeChat` correctly detects no tool calls only when captured before `a.AddMessage`.

**Decision.** Capture `histLenBeforeChat := len(a.History)` before the `Chat/RunToolLoop` call. Compute `noToolCalls := len(a.History) == histLenBeforeChat` *before* `a.AddMessage`. This invariant must be preserved: any refactor that moves `a.AddMessage` before the `noToolCalls` check will silently break the guard.

**Consequences.** Documented as a key invariant in `CLAUDE.md`.

---

## 2026-05-28 — Three-silo memory architecture

**Context.** Harvey needs to accumulate knowledge across sessions without polluting the LLM context window on every turn. Three distinct content types require different ingestion and retrieval strategies: (1) external documents, (2) session experience, (3) research observations.

**Decision.** Three independent silos unified at retrieval time by `UnifiedMemory.Recall()`:

| Silo | Ingestion | Retrieval |
|---|---|---|
| RAG store | `/rag ingest` (explicit) | Per-prompt via `ragAugment()` |
| Memory store | `/memory mine` or auto-mine on exit | Session-start via `UnifiedMemory` |
| Knowledge base | `/kb observe` (explicit) | On-demand via `UnifiedMemory` |

**Consequences.** Each silo has its own command namespace (`/rag`, `/memory`, `/kb`). The unified retrieval via `/memory recall` is the recommended entry point. All three silos share a token budget enforced at injection time.

---

## 2026-06-30 — Skill suggestions from session transcripts

**Context.** Sessions accumulate reusable multi-step workflows that would benefit from being captured as skills. A mechanism is needed to propose skill candidates automatically rather than requiring users to author SKILL.md files by hand.

**Decision 1 — Output goes directly to the live `agents/skills/` directory**, not a staging area. Each accepted candidate immediately becomes loadable via `/skill load`. The generated SKILL.md is clearly marked as auto-generated and the user is expected to review and refine it before committing. This avoids a two-step accept-then-move workflow that adds friction without safety benefit.

**Decision 2 — Command lives on `/skill`, not `/memory`**, as `suggest`. `/skill suggest [SESSION]` fits the skill management namespace naturally; using `/memory suggest` would imply the output is a memory record. The subcommand reads from a session file (defaulting to the most recent `.spmd`) and is otherwise independent of the memory mining pipeline.

**Decision 3 — Separate LLM prompt from `memory mine`**. `skillSuggestorPrompt` in `skill_suggestor.go` is a distinct constant from the memory miner's extraction prompt. The output schemas differ (skills need `steps[]` and `variables[]`; memories need `kind` and `confidence`), and mixing them into one prompt would degrade extraction quality for both.

**Consequences.** `Suggestor` in `skill_suggestor.go` owns the full pipeline. `cmdSkill` wires the `suggest` subcommand. `SkillCandidate` reuses `SkillVariable` (updated to add `Type` field and JSON tags). No staging directory is needed.
