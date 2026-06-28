# Harvey Chunked Document Analysis — Design

**Status (2026-06-27):** Design draft. Target version TBD (after v0.0.15
audit trail work). See [chunked-analysis-plan.md](chunked-analysis-plan.md)
(forthcoming) for the phased implementation plan.

**References:**

- AutoChunker: Structured Text Chunking and its Evaluation (Jain, Aggarwal,
  Saladi — Amazon; ACL 2025 Industry Track). Bottom-up structure-aware
  chunking with tree-based hierarchy; evaluated on noise reduction,
  completeness, context coherence, task relevance, and retrieval performance.

- Comparative Evaluation of Advanced Chunking for RAG in LLMs for Clinical
  Decision Support (Gomez-Cabello et al., Bioengineering 2025, doi:
  10.3390/bioengineering12111194). Adaptive boundary chunking achieves 87%
  accuracy vs 50% for fixed-size; aligning chunks to logical topic boundaries
  is the decisive factor.

- DocETL: Agentic Query Rewriting and Evaluation for Complex Document
  Processing (Shankar et al., UC Berkeley; arXiv:2410.12189). Map→reduce
  decomposition of LLM document operations improves accuracy 21–80% over
  single-pass approaches; user-defined operations must be decomposed to be
  accurate.

- Query-Adaptive Semantic Chunking for RAG: A Dynamic Strategy with
  Contextual Window Expansion (Rastogi; arXiv:2605.22834). Making the user
  query a first-class input to segmentation improves relevance 18–27% over
  fixed chunking baselines; same document chunked differently depending on
  what the user is seeking.

- NexusSum: Hierarchical LLM Agents for Long-Form Narrative Summarization
  (Kim & Kim, CJ Corporation; ACL 2025 Long Papers). Hierarchical multi-LLM
  pipeline with controlled per-chunk output achieves 30% improvement in
  BERTScore F1; validates multi-model routing across chunk and synthesis
  steps.

- ContextWeaver: Selective and Dependency-Structured Memory Construction for
  LLM Agents (Wu et al., UT Austin / AWS; arXiv:2604.23069). Context
  overflow in LLM agents is driven by accumulated conversation dependency
  structure, not raw token count alone; sliding window and prompt compression
  fail by losing causal links between reasoning steps.

- Chunking Methods on RAG — Effectiveness Evaluation Against Computational
  Cost and Limitations (Śmigielski et al., Wrocław University; KES 2026,
  arXiv:2606.00881). First systematic cross-method comparison; structure-aware
  chunking consistently outperforms fixed-size across diverse scenarios.

- Evaluating Chunking Strategies for RAG in Oil and Gas Enterprise Documents
  (Taiwo & Yusoff, Nigeria LNG; 2026, doi:10.5121/csit.2026.160507).
  Structure-aware chunking yields highest retrieval effectiveness (top-K
  metrics); semantic chunking incurs higher cost without proportionate gain.

---

## Motivation

Harvey's `read_file` tool reads the full content of a file and injects it
into the conversation history before sending to the model. This works when
file content fits comfortably within the model's remaining context budget.
It fails in two related but distinct ways.

**The accounting bug.** Harvey compares estimated file size against the
model's raw context window, not the context that *remains* after existing
history, system prompt, injected memories, and RAG chunks are accounted
for. A 30,000-token model that already carries 25,000 tokens of history has
only 5,000 tokens of usable space — but Harvey may attempt to read a
10,000-token file because it appears to fit within the raw window. The
ContextWeaver paper (Wu et al., 2026) identifies this pattern precisely:
context overflow in agentic systems is driven by accumulated interaction
history, not raw input size. Sliding-window and simple compression
approaches fail because they discard the causal links between earlier
reasoning steps.

**The genuine overflow case.** Some documents are simply too large for
any reasonable context budget regardless of how carefully history is
managed: research papers, codebases, log files, long manuscripts. No
accounting correction will make a 200,000-token document fit in a
32,000-token context. These require a fundamentally different strategy.

This design addresses both problems. The accounting fix is a prerequisite;
the chunked analysis feature builds on top of it.

---

## Core design decisions

These decisions were settled in discussion before the design was written.
They are recorded here so the plan phase has a stable foundation.

**D1 — User-directed, not automatic.** When overflow is detected, Harvey
pauses and alerts the user rather than silently chunking the document. The
user writes the chunk processing instruction, optionally including an
`@model` routing directive. QASC (Rastogi, 2605.22834) validates this
choice: treating the user query as a first-class input to segmentation
improves relevance 18–27% over fixed or automatic chunking. The user owns
the question; only the user knows what they want from each chunk.

**D2 — Two-signal trigger.** Harvey uses `os.Stat` to estimate file size
*before* reading, then compares against estimated *remaining* context (not
raw window size). If the pre-read estimate exceeds a configurable threshold
(default 80% of remaining context), the alert fires before any file content
is sent to the model.

**D3 — Paragraph boundaries by default; code-block boundaries for source.**
The literature consistently shows structure-aware chunking at logical topic
boundaries outperforms fixed-size. The Bioengineering paper found adaptive
boundary chunking at 87% vs 50% accuracy for fixed-size. AutoChunker
demonstrates tree-based structure awareness reduces noise and improves
coherence. For prose documents, double-newline paragraph boundaries are the
first-pass split; for source code (detected by file extension), function and
block boundaries are used. Overlap defaults to one paragraph.

**D4 — Synthesis is automatic.** After chunk results are collected, Harvey
synthesizes them in a single pass without prompting the user again. The
synthesis prompt is derived from the original chunk instruction. This keeps
the UX simple for the initial implementation.

**D5 — Chunk sub-conversation recorded as a Fountain scene.** The chunk
processing workflow is recorded as a new `INT. CHUNK ANALYSIS` scene in the
session `.spmd` file. This makes the chunk prompt and its results available
to `/memory mine` for extraction as reusable patterns — the primary
persistence path for chunk instructions.

---

## Context estimation

Before reading any file, Harvey estimates whether the file content will
overflow available context using two checks in sequence.

**Check 1 — Pre-read byte estimate.** `os.Stat(path).Size()` returns the
file size in bytes. Harvey uses `bytes / 4` as a conservative token estimate
(a well-established heuristic for English prose; source code is denser and
may require a lower divisor). If this estimate exceeds the remaining context
budget, the alert fires without reading the file.

**Check 2 — Remaining context budget.** Remaining context is estimated as:

```
remaining = model.ContextWindow
           − estimateTokens(serializedHistory)
           − estimateTokens(systemPrompt)
           − estimatedMemoryOverhead
           − safetyMargin
```

The model's context window is available from `ModelCapabilities` in
`model_cache.go`. `estimateTokens` applies the same bytes/4 heuristic to
serialized content. `safetyMargin` defaults to 10% of the raw window (space
for the model's response and any tool calls it may make).

The Ollama API returns `prompt_eval_count` in chat responses; these counts
could calibrate the heuristic over time. Calibration is deferred — the byte
heuristic is sufficient for a first implementation.

---

## Chunking strategy

Harvey splits a document into chunks before the map phase. The algorithm is:

1. **Detect document type** from file extension. Prose extensions (`.md`,
   `.txt`, `.rst`, `.tex`, `.html`) use paragraph splitting. Source
   extensions (`.go`, `.ts`, `.py`, `.js`, `.c`, `.h`, and others) use
   block splitting. Unknown extensions default to paragraph.

2. **Paragraph split (prose).** Split on double newline (`\n\n`). Recombine
   adjacent short paragraphs until the chunk reaches approximately 75% of
   the target chunk size. This prevents isolated single-sentence paragraphs
   from becoming their own chunks.

3. **Block split (source).** Split on function and method boundaries detected
   by a blank-line-then-signature heuristic — no language parser is required.
   This is intentionally simple; a language-aware parser is deferred.

4. **Overlap.** Each chunk includes the last paragraph (or last function
   signature comment) of the preceding chunk. This preserves local coherence
   across boundaries. AutoChunker (Jain et al.) demonstrates that preserving
   document hierarchy at boundaries significantly improves chunk stickiness
   and retrieval coherence.

5. **Target chunk size.** Default 1,500 tokens (~6,000 bytes). Configurable
   in `harvey.yaml`.

6. **Maximum chunks.** Default 20. If the document would produce more chunks
   than the maximum, Harvey warns the user and asks for confirmation before
   proceeding. Processing 20 chunks of a 100-chunk document will miss
   material; the user should know.

The QASC paper (Rastogi, 2605.22834) found that fixed-size chunking produces
inconsistent outcomes across document types, with 20% of technical documents
yielding different retrieval results for identical queries depending on chunk
size. Paragraph-boundary chunking mitigates this by preserving complete
arguments within each chunk.

---

## Alert UX and chunk prompt

When the overflow threshold is crossed, Harvey pauses before reading the
file and displays an alert in the terminal. The alert includes:

- The file name and estimated token size
- The estimated tokens remaining in the current context
- A suggested chunk prompt pre-filled from the user's most recent message
- The instruction: *Enter instructions to process each chunk in turn, or
  "no" to return to the conversation.*

The user may:

- **Edit** the suggested prompt — for example, to focus it ("extract only
  the methodology") or add an `@model` directive to route chunk analysis to
  a specific model
- **Accept** the pre-fill as-is by pressing enter
- **Type "no"** to cancel and return to the conversation without reading the
  file

If the user includes `@model` in the chunk prompt, Harvey's existing
`@mention` routing infrastructure routes each chunk analysis call to the
named model. The synthesis step uses the same model unless the chunk prompt
specifies otherwise.

---

## Map-reduce workflow

The chunked analysis follows a two-phase map-reduce pattern. DocETL (Shankar
et al., 2410.12189) showed that decomposing single-pass LLM document
operations into map→reduce sequences improved accuracy 21–80% across complex
document tasks. NexusSum (Kim & Kim, ACL 2025) demonstrated a 30% gain in
summarization quality using a hierarchical multi-LLM pipeline with controlled
per-chunk output — the same pattern Harvey adopts here.

**Map phase.** For each chunk *i* of *N*:

1. Construct a prompt: `[chunk_instruction]\n\n---\nChunk [i] of [N]:\n[chunk_content]`
2. Send to the selected model (current session model, or the `@mention` model
   if specified in the chunk prompt)
3. Collect the result as a partial answer
4. Record a `[[chunk-result: i/N — ok|error]]` note in the Fountain scene
5. Display progress to the user: `Processing chunk 3/12…`

**Reduce (synthesis) phase.** After all chunks complete:

1. Construct a synthesis prompt from the original chunk instruction plus all
   partial answers, labelled by chunk number
2. Send to the synthesis model (same as map model by default)
3. Inject the synthesized result into the main conversation history as the
   assistant's response to the original user message
4. The main conversation continues from this point

---

## Fountain scene model

Chunk processing is recorded as a new `INT. CHUNK ANALYSIS` scene inserted
into the session file between the turn that triggered the overflow and the
turn that receives the synthesized result. This fits the theatrical metaphor
as a structured aside — a sub-conversation between characters that resolves
before the main scene resumes.

```
INT. HARVEY AND RSDOIEL TALKING 2026-06-27 10:04:00

Harvey and RSDOIEL are in chat mode. Model: LLAMA3. Workspace: <workspace>.

RSDOIEL
Read document.md and summarize the methodology section.

HARVEY
Context overflow: document.md is approximately 68,000 tokens;
12,000 tokens remain in current context.
Enter instructions to process each chunk in turn, or "no" to
return to the conversation.


INT. CHUNK ANALYSIS — document.md 2026-06-27 10:04:08

[[chunk: file=document.md, chunks=12, model=LLAMA3,
         boundary=paragraph, chunk-size=1500]]

RSDOIEL
Summarize the methodology and findings of each section. @llama3

[[chunk-result: 1/12 — ok]]
[[chunk-result: 2/12 — ok]]
[[chunk-result: 3/12 — ok]]
[[chunk-result: 4/12 — ok]]
[[chunk-result: 5/12 — ok]]
[[chunk-result: 6/12 — ok]]
[[chunk-result: 7/12 — ok]]
[[chunk-result: 8/12 — ok]]
[[chunk-result: 9/12 — ok]]
[[chunk-result: 10/12 — ok]]
[[chunk-result: 11/12 — ok]]
[[chunk-result: 12/12 — ok]]
[[synthesis: model=LLAMA3 — ok]]

HARVEY
Chunk analysis complete. Synthesized result injected into conversation.


INT. HARVEY AND RSDOIEL TALKING 2026-06-27 10:07:45

Harvey and RSDOIEL are in chat mode. Model: LLAMA3. Workspace: <workspace>.

LLAMA3
The methodology section describes a three-phase experiment…
```

The `INT. CHUNK ANALYSIS` scene name includes the file being processed for
easy identification in session browsing. The `[[chunk:]]` header note records
parameters for reproducibility and memory mining. Individual chunk results
are status-only notes — no chunk content is written to the session file —
keeping `.spmd` files manageable and memory-miner output clean. The
synthesized result lands in the next normal turn so it appears naturally in
the conversation flow.

The RSDOIEL line in `INT. CHUNK ANALYSIS` (the user's chunk instruction) is
automatically available to `/memory mine` and can be extracted as a reusable
workflow pattern for similar document types.

---

## harvey.yaml configuration

A new `chunking:` stanza in `harvey.yaml` controls defaults:

```yaml
chunking:
  enabled: true
  threshold: 0.80          # alert at 80% of remaining context
  chunk_size_tokens: 1500  # target tokens per chunk
  max_chunks: 20           # warn if document would exceed this
  overlap: paragraph       # "paragraph", "sentence", or "none"
```

All fields are optional; Harvey uses the defaults above when the stanza is
absent. `enabled: false` disables the feature entirely — file reads behave
as they do today, context overflow or not.

---

## Alternatives considered

**Silent automatic chunking.** Harvey could chunk automatically without user
input, deriving the chunk prompt from the original user message. Rejected
because: (1) QASC shows chunking quality is directly tied to query
specificity — a generic "read this file" prompt produces poor chunk results;
(2) silent automatic behavior obscures what Harvey is doing, counter to
Harvey's transparency philosophy; (3) a user who realizes the file is
irrelevant to their question should be able to cancel before 20+ LLM calls
are made.

**Ephemeral RAG.** Large files could be ingested into a temporary RAG store
and queried via the existing `ragAugment` pipeline. This reuses existing
infrastructure but requires an active embedding model, adds indexing latency,
and suits *retrieval* use cases (find relevant chunks) rather than *analysis*
use cases (process every chunk). Deferred as a future option for very large
corpora where the user wants to query rather than analyze.

**Sliding-window summarization.** Process chunks sequentially, passing a
rolling summary into each subsequent chunk prompt. Rejected because
information loss compounds at each step — for a 20-chunk document, the
summary entering chunk 20 is a distillation of nineteen distillations.
Map-reduce avoids this by treating all chunks independently and synthesizing
once. The ContextWeaver paper (Wu et al., 2026) confirms that approaches
that discard earlier reasoning context degrade multi-step performance.

**Per-chunk user confirmation.** Pause after each chunk result for user
review before proceeding to the next. Adds control at the cost of requiring
the user to attend to 20+ interruptions for a single document. The initial
implementation runs the map phase unattended; progress is displayed in the
terminal but no confirmation is required per chunk.

**Fixed-size token chunking as default.** Fixed-size chunks are the simplest
to implement but the literature consistently shows they underperform
structure-aware alternatives. The Bioengineering paper (87% vs 50% accuracy)
and the Śmigielski et al. KES 2026 comparison both show structure-aware
outperforms fixed-size at comparable computational cost. Paragraph boundaries
are nearly as simple to implement and significantly better in practice.

**Language-aware parser for source code.** Using `go/parser`, `tree-sitter`,
or similar to detect function boundaries precisely. Deferred: a blank-line-
then-signature heuristic correctly identifies boundaries for well-formatted
Go and TypeScript source without any dependency, which is sufficient for the
initial implementation.
