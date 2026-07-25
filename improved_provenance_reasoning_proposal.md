# Harvey — Improved Provenance for RAG-Augmented Reasoning — Proposal

**Status (2026-07-23):** Proposed. Not yet implemented or scheduled.

---

## Background

Andrew Potter, "When Search Becomes Inquiry: What the Palme Investigation
Archive Reveals About the Future of Archival Access," *Metaarchivist*
(Substack), July 10, 2026,
<https://metaarchivist.substack.com/p/when-search-becomes-inquiry>.

The article traces PalmeNet-Chat, a tool built to give journalists access to
the ~35-year, 250-meter Palme assassination investigation archive, through
two generations. The first (2024) solved an *availability* problem: making a
huge, previously unsearchable archive queryable while keeping it on-premise.
The second (2025) moved to cloud infrastructure and added agentic reasoning
— the tool now generates its own follow-up questions and chains fragments of
testimony and forensic reporting into structured lines of inquiry, returning
reasoned conclusions rather than documents.

Potter frames this as a shift from **search** (retrieval — find material,
let a human decide what it means) to **inquiry** (synthesis — the system
itself decides what's relevant, chains it, and states a conclusion). He
identifies three risks that come with that shift:

1. **Obscured reasoning** — synthesized answers are "plausible in tone,
   coherent in structure," which can mask omissions or weightings that would
   have been visible in a raw list of search results.
2. **Incomplete coverage presented as comprehensive** — PalmeNet-Chat
   reasons well but only reaches ~10% of the released archive; better
   reasoning over a fraction of the material creates an illusion of
   completeness it can't back up.
3. **Custody separation** — moving the reasoning layer to cloud
   infrastructure separates analytical capability from the archive's
   traditional custody model, which complicates who is accountable for a
   conclusion.

Harvey has its own retrieval-augmented-generation layer (`rag_support.go`,
`agents/rag/*.db`, documented in `harvey-rag.7.md`). It is currently a
**search** tool, not an **inquiry** tool — `Agent.ragAugment()` does one
embed-and-retrieve pass per turn and silently prepends matching chunks to
the prompt; there is no RAG-callable tool the model can invoke repeatedly
to chain follow-up queries the way PalmeNet-Chat's agentic layer does. That
means risk (2), incomplete-coverage-as-comprehensive, and a version of risk
(1), obscured reasoning, already apply to Harvey *today*, before any move
toward agentic multi-hop retrieval. Risk (3), custody separation, is mostly
avoided by Harvey's local-first design, with one caveat (see below).

## Problem

### 1. Grounding checks don't see RAG-injected content

`grounding.go`'s `groundingCheck` is Harvey's only defense against
plausible-but-hallucinated answers. It compares quoted text in the model's
reply against content returned by "content tools" called during the turn:

```go
// grounding.go:18
var contentToolNames = map[string]bool{
    "read_file": true,
}
```

RAG retrieval never goes through a tool call. `ragAugment` (`rag_support.go:613`)
prepends matching chunks directly into the *user* message before it reaches
the model (`terminal.go:1108`), so the grounding check has no way to know
those chunks exist. A reply built entirely from RAG-injected context —
including a hallucinated misreading of that context — passes grounding
silently, because grounding only ever looks at `read_file` output.

This is Harvey's version of Potter's "plausible in tone, coherent in
structure" risk: the one mechanism designed to catch ungrounded claims
doesn't cover the one retrieval path most likely to produce synthesized,
confident-sounding answers.

### 2. Retrieval confidence isn't visible in the reply the user reads

`ragAugment` returns a `RAGAugmentInfo` (`recorder.go:99`) with the sources
and cosine-similarity scores of whatever it injected, stored on
`Agent.LastRAGInfo` (`harvey.go:174`). Today that information is:

- written to the session recording (`RecordTurnWithStats`, for later
  forensic review), and
- surfaced interactively only if the user immediately runs `/kb add`, which
  offers to link `LastRAGInfo.Sources` into a knowledge-base observation
  (`commands_kb.go:241`).

It is **not** shown alongside the reply itself. Harvey already prints an
inline tier warning for context-window usage right after each reply
(`terminal.go:1123-1128` — `contextWarn`/`contextFull`), but there is no
equivalent for RAG confidence. A reply built from five chunks scoring 0.8
and a reply where nothing cleared the 0.3 threshold (`ragMinScore`, sent to
the model unmodified) render identically to the user. This is Potter's
"incomplete coverage read as comprehensive" risk, reproduced at the scale of
a single chat turn instead of an archive: the user has no signal to
distinguish a well-grounded answer from an ungrounded one without manually
running `/rag query` themselves beforehand.

### 3. Local storage is not the same as local custody

Harvey's RAG stores are local SQLite files bound to a local embedding model
(`harvey-rag.7.md`), which sidesteps most of Potter's custody-separation
concern — there's no cloud vendor sitting between the archive and the
reasoning step by default. The caveat: Harvey's backend is pluggable
(`any-llm-go`, `backend_ollama.go`, `backend_llamafile.go`,
`backend_llamacpp.go`), so if a user points the *generation* model at a
cloud provider, RAG-retrieved chunks still leave the machine at that point,
even though the store itself never does. This isn't a bug, but it's an
invariant worth stating explicitly rather than leaving implicit: **local
storage guarantees nothing once a cloud generation backend is selected.**

## Candidate approaches

These are options for a design phase to weigh, not a decision already made.
Each names a leaning but lists the alternatives it passed over and why they
weren't obviously better, so the trade-off is visible rather than assumed.

### A. Close the RAG blind spot in grounding checks

The gap: `groundingCheck` only ever inspects `read_file` tool output
(`contentToolNames`, `grounding.go:18`); RAG-injected chunks never pass
through a tool call, so nothing checks a reply against them.

- **Leaning:** extend `groundingCheck` to accept the RAG-injected block as
  an additional source alongside `read_file` output — pass the string
  `ragAugment` (`rag_support.go:613`) built for the prompt, or
  `RAGAugmentInfo.Sources` chunk contents, into the check at its
  `terminal.go:1254` call site. Additive: one new parameter, no change to
  the existing quoted-string-matching logic or to `contentToolNames`.
- **Alternative — route RAG through a synthetic tool call.** Instead of
  `ragAugment` prepending text directly to the user message, model it as an
  internal `rag_search` tool call/result pair in history, so it falls under
  the *existing* `contentToolNames` mechanism with zero changes to
  `groundingCheck` itself. More architecturally consistent (RAG and
  `read_file` grounded the same way), but changes how RAG injection appears
  in history/session recordings, which other code may depend on
  (`recorder.go`'s `RAGAugmentInfo` handling, `/kb add`'s use of
  `LastRAGInfo`) — larger blast radius than it looks.
- **Alternative — separate RAG-specific grounding pass.** A parallel
  function scoped only to RAG content, run independently of
  `groundingCheck`. Avoids touching the existing function at all, but means
  two grounding code paths to maintain and two warning styles for the user
  to learn.

### B. Surface RAG confidence to the user, not just to the session log

The gap: `RAGAugmentInfo` (scores, sources) is computed every RAG turn but
only reaches the user if they proactively run `/rag query` beforehand or
`/kb add` immediately after (`commands_kb.go:241`); the reply itself gives
no signal of retrieval confidence.

- **Leaning:** an inline one-line note after the reply, reusing the
  warning-tier pattern already established for context-window usage
  (`terminal.go:1123-1128`) — e.g.
  `  ℹ RAG: 3 chunks injected, avg score 0.62 (golang)` or
  `  ⚠ RAG: no chunks above threshold — answered without retrieval
  context`. Both branches are already computable from the existing
  `RAGAugmentInfo`; this is a display change at the `ragAugment` call site.
- **Alternative — status-line/prompt indicator instead of a printed line.**
  Lower-friction (doesn't add output the user has to read past each turn)
  but easy to miss, and Harvey doesn't currently have a persistent status
  line to put it on — would require adding one.
- **Alternative — `/rag last` command instead of automatic display.**
  Zero output-noise cost, consistent with how `LastRAGInfo` is already
  exposed on-demand via `/kb add`. Trade-off: purely opt-in, so it repeats
  the current failure mode (the user has to know to ask) rather than fixing
  it — this is the strongest argument against it as the primary fix, though
  it could still ship alongside whichever automatic option is chosen.

### C. Document the local-storage-vs-custody distinction

The gap: RAG stores are local SQLite bound to a local embedding model, but
nothing states that retrieved chunks still leave the machine once a cloud
generation backend is configured via `any-llm-go`.

- **Leaning:** a short paragraph in `harvey-rag.7.md`, either folded into
  "EMBEDDING MODEL CHOICE" or as a new "DATA CUSTODY" section. No code
  change — this is a documentation gap, not a behavioral one.
- **Alternative — put it in `SECURITY.md`/`SECURITY_REVIEW.md` instead (or
  as well).** Those files already collect other data-handling invariants,
  so it may be a more discoverable home for security-focused readers than a
  man page someone consults for RAG usage syntax. Not mutually exclusive
  with the `harvey-rag.7.md` placement — a design decision just needs to
  pick where the canonical statement lives, if not both.

## Constraints for the design phase

Facts a chosen design must respect, so they don't need rediscovering:

- `ragMinScore = 0.3` is duplicated between `terminal.go` and
  `cmd/assay/main.go` (per `harvey/CLAUDE.md`'s "Key invariants to
  preserve") — any design touching threshold behavior must keep both in
  sync, or centralize the constant.
- `groundingCheck`'s existing quoted-string heuristic
  (`minQuotedLen = 20`, `grounding.go:14`) and its tool-based path for
  `read_file` must keep working unmodified — A is additive, not a rewrite.
- RAG stores are local-first by design (`harvey-rag.7.md`); any design for
  A or B should not assume or require network access, since Harvey is used
  fully offline with local Ollama/llamafile/llamacpp backends.
- `RAGAugmentInfo` (`recorder.go:99`) and `Agent.LastRAGInfo`
  (`harvey.go:174`) already exist and are populated every RAG turn — B is a
  display problem, not a data-availability problem; no new retrieval-time
  computation should be needed.
- Session recordings (`.spmd` files via `Recorder.RecordTurnWithStats`)
  already capture `RAGAugmentInfo` — any design should preserve that
  forensic trail rather than duplicating or replacing it.

## Non-goals

This proposal does **not** recommend building PalmeNet-Chat-style agentic,
multi-hop RAG (a model-callable `/rag query` tool that chains its own
follow-up retrievals). That capability is a legitimate future direction for
Harvey, but per Potter's own framing it should not be added *before* A and B
above are in place — chaining retrieval without grounding coverage or
visible confidence would import Potter's "obscured reasoning" risk in full,
at exactly the point where Harvey starts resembling the tool the article is
about.

## Open questions

1. Should the RAG confidence line (B) be togglable independently of
   `/rag on|off` (e.g. a quiet mode for users who've already internalized
   the workflow), or always shown when RAG fires?
2. Should (A)'s grounding check apply the same `minQuotedLen` threshold to
   RAG content, or does injected-chunk text warrant a different threshold
   given it's typically denser/shorter than file content?
3. Does (C) belong in `harvey-rag.7.md` alone, or also in `SECURITY.md` /
   `SECURITY_REVIEW.md` where other data-handling invariants live?
