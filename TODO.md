
# Action Items

## Bugs

- [ ] Still getting chunk hallucinations (tested with Gemma4 EB4 model in Harvey debug mode), review logs and session for problem. Is this a problem with our approach (map reduce) or something else? Is this approach reasonable to pursue? Never got the chunk prompt entry option. Here is the console output:

```harvey
harvey > Please review natural_language_programming.md for topic drift. I want to identify the sentences and paragraphs that drive off topic (I need paragraph and line numbers). The arti

This file has been placed in an unsafe place (a new description of what the data it represents):
.   (and)
.   (and)
.   (and)
.   (And)
.  (And)
.. (And)
   ... (And)
  .... (Or)
 .  (And)
  .. (Or)
   .. (Or)
    .. (Or)
     .. (Or)
     .. (And)
      ... (And)
  ... (And)
  .. (Or)
   .  (And)
  ... (And)
  ...  (And)
   ..  (Or)
  ...  (Or)
  ...  (And)
  ....  (And)
  .....  (And)
  ..... (And)
  ... (Var[Char](20))
  ... (Var[Char](16))
  .. (Or)
   .. (And)
   .. (Or)
    .. (And)
     .. (Or)
     .. (And)
      ... (Or)
     .. (And)
      ... (And)
   ....  (And)
  ... (Or)
   ..  (And)
   .. (Or)
  .. (And)
  ... (And)
   .. (And)
  ...  (And)
   .. (Or)
   .. (And)
  ... (And)
  .. (Var[Char](32))
 .  (And)
  .. (And)
   ... (And)
  ...  (And)
  .. (Or)
   ... (Or)
  .. (And)
  .. (Var[Var(Text) or Text] (
  ..  (Or)
  ... (And)
  .. (Or)
  .. (And)
  .. (Or)
   .. (And)
   ... (And)
  ..  (Or)
  .. (Var [Str(ring) or Date or  Date (and Date).
  ... (And)
  ... (Var[Var[Str](Str(DateTime, Format(Date, Time and DateTime) and I18u + 67678909796e4i921)//, i.e. In my first Year of Employment, I can't remember where I first encountered it.
  ... (And)
  ...  (And)
    ...  (And)
  ...  (And)
  ..  (And)
  ..  (And)
  .  (Or)
  ..  (And)
  ..  (And)
   ... (And)
  ... (And)
  ..  (And)
  .. (Or)
  .. (And)
   .. (And)
  .. (And)  --  (And)
  .. (Or)  --  (And)
  .. (Or)
  .. (And)  --  (And)
  .  (Or)  --  (And)
  .. (And)  --  (And)
  .. (And)  --  (And)
  .  (And)  --  (Or)  --  (And)
  .. (And)  --  (And)  --  (Or)  --  (And)
  llamafile (Gemma4-E4B-Q4_K_M) · 4m50.538s
harvey > /exit
Goodbye.
  Session saved to /Users/rsdoiel/Laboratory/agents/sessions/harvey-session-20260630-163946.spmd
```

  Context: the chunk *isolation* mechanism itself is already confirmed
  working (`TestRunChunkedAnalysis_ChunkMessagesAreIsolated` — each chunk
  call receives only system+user, no history). This garbled output is a
  deeper, separate problem: the model's coherence collapsing under the
  chunking prompt itself. See `chunked-analysis-design.md` for the overall
  approach.

## Research Question

- [x] How to improve cold starts with models. Investigated 2026-07-03,
  designed/decided/implemented 2026-07-04 — see `cold-start-latency-findings.md`.
  Not Q1_0/Bonsai-specific: cold-start time scales with parameter count
  across every model tested. Shipped: workspace-boundary fix + preflight
  context-size check (fails clearly instead of a raw 400 when the system
  prompt alone can't fit), skills catalog reformatted (-1270 tokens on the
  real 11-skill catalog), and agentPreamble/both HARVEY.md files rewritten
  for density (-30% to -58%). Not pursued: raw prompt-processing speed
  tuning (`--batch-size`/`--ubatch-size`) — re-measure cold-start with the
  trimmed prompt first; revisit only if still too slow.

## Dual RAG injection audit

See [DECISIONS.md](DECISIONS.md) (2026-06-02 — Dual RAG injection audit, deferred).
M6 (`rag.per_prompt: bool`, shipped) is the preferred resolution for models
with tool support.

- [ ] (Legacy option) Users with both `memory.enabled` and `rag.enabled` receive RAG content
  twice per turn: once via `UnifiedMemory.Recall()` at session start and once via `ragAugment()`
  per prompt. M6 above is the preferred resolution; this item tracks the fallback for models
  without tool support.
