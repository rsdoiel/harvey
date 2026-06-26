
# Action Items

## Bugs

## Release Review

## Next (v0.0.15 release)

### Bug fixes (must clear before tagging)

- [x] **ollama.go:486** — `FastProbeModel` sets `cap.ToolMode = ToolModeStructured`
  unconditionally for tool-capable models. `ModelCache.Set` UPSERTs this, silently overwriting
  any mode the user set via `/model mode`. Fix: removed `cap.ToolMode = ToolModeStructured` from
  FastProbeModel; toolsReliable() falls through to SupportsTools==CapYes correctly.

- [x] **terminal.go:1297** — After option-2 retry, `toolCallRecords` still holds records from
  the first-pass `RunToolLoop` (populated at line 1141) even though those history entries were
  rolled back at line 1181. Fix: set `toolCallRecords = nil` at the top of the option-2 block
  before rolling back history.

- [x] **terminal.go:1321** — After option-2 retry, `noToolCalls` was always `true`. Fix: capture
  `hadToolCalls := len(a.History) > histLenBeforeChat` before option-2 fires, then compute
  `noToolCalls := !hadToolCalls && len(a.History) == histLenBeforeChat`.

- [x] **terminal.go:1185** — Option-2 retry called `a.Client.Chat` directly, bypassing
  `RunToolLoop`. Fix: when `useStructuredTools` is true, option-2 retry now uses `RunToolLoop`
  (with a fresh spinner) and updates `toolCallRecords` from the retry's history slice.

- [x] **terminal.go:1203** — After option-2 retry with RunToolLoop, `a.History[histLenBeforeChat:]`
  now correctly contains the retry's tool-call messages (populated by the fixed retry path).
  groundingCheck uses these automatically.

- [x] **file_inject.go:116** — `cantReadPhrases` entry `"please provide the file"` was too broad.
  Fix: tightened to `"please provide the file content"`.

- [x] **model_cache.go:47** — Exported `ToolMode*` constants now have `/** ... */` block-doc
  comments with Description and Example sections as required by CLAUDE.md.

### Release steps (after bugs cleared)

- [ ] Bump `codemeta.json` version to 0.0.15, update `releaseDate` and `releaseNotes`
- [ ] Regenerate `version.go`: `cmt codemeta.json version.go`
- [ ] Commit and tag `v0.0.15`

## Deferred to v0.0.16

### Option 2 reactive retry — surgical rollback
See [audit-trail-plan.md](audit-trail-plan.md) W1 and the small-model tool-use mitigation work
(file_inject.go option 2).

- [ ] The current retry in `terminal.go` calls `Client.Chat` directly and rolls back the full
  history by one message. When `RunToolLoop` added intermediate tool-call/tool-result messages
  before the refusal, those are silently dropped. Implement a surgical rollback that only removes
  the assistant refusal message and re-adds the augmented user message, preserving prior tool
  loop history.

### `/model mode MODEL auto` — reset to auto
See [audit-trail-plan.md](audit-trail-plan.md) option 3 (`/model mode` command, `model_cache.go`).

- [ ] `/model mode` currently accepts `structured`, `prose`, `inject`, and `none` but has no
  `auto` value to clear an explicit override and return the model to capability-detected defaults.
  Add `auto` as a valid mode that sets `tool_mode = ''` (empty) in `model_capabilities`, restoring
  `modelToolMode()` fallback to `CapabilityStatus`.

### Retraction monitoring service
See [scholarly-provenance-plan.md](scholarly-provenance-plan.md) S2 and
[scholarly-provenance-design.md](scholarly-provenance-design.md).

- [ ] The `sources` table in `knowledge.db` already has `retracted INTEGER` and `retraction_note TEXT`
  columns for manual marking (via `/kb retract`). Add a periodic background check against the
  Retraction Watch API (`retractionwatch.com`) that flags retracted DOIs automatically. A
  `/kb check-retractions` command (or scheduled task) should query registered sources with
  `identifier_type = 'doi'` and update `retracted`/`retraction_note` on hits.

### llama.cpp Apertus tool-call format
See `henry` project (`henry-handoff-20260622-llamafile-factory.spmd`).

- [ ] When llama.cpp gains stable custom token support, update
  `templates/apertus-4b-toolcall.jinja` in the `henry` project to use structured tool-call
  tokens instead of the current prose JSON fence workaround. Retest with Apertus 4B via
  `bin/assay --llamafile`.

### Dual RAG injection audit
See [DECISIONS.md](DECISIONS.md) (2026-06-02 — Dual RAG injection audit, deferred).

- [ ] Users with both `memory.enabled` and `rag.enabled` receive RAG content twice per turn:
  once via `UnifiedMemory.Recall()` at session start and once via `ragAugment()` per prompt.
  Audit the overlap and either (a) skip RAG chunks in `UnifiedMemory.Recall()` when `a.RagOn`
  is true, or (b) make `ragAugment` a no-op when `UnifiedMemory` already injected from the same
  store this session.
