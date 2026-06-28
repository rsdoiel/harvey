# Agentic Memory Tools — Design

## Source

> Yi Yu, Liuyi Yao, Yuexiang Xie, Qingquan Tan, Jiaqi Feng, Yaliang Li, and Libing Wu.
> **Agentic Memory: Learning Unified Long-Term and Short-Term Memory Management for Large Language Model Agents.**
> arXiv:2601.01885v2 [cs.CL], 30 Apr 2026.
> Wuhan University / Alibaba Group.
> Local copy: `memory-models/2601.01885v2.pdf`

Key sections referenced in this design:
- §3.2 Memory Management via Tool Interface (the six tools, Table 1)
- §3.3 Three-Stage Progressive RL Strategy (not adopted — see §"What we are NOT doing")
- §4.2 Main Results (Table 2, Figure 3 — STM tools outperform static RAG)
- §A.1 Memory Management Tools (precise tool definitions, Figures 6–7)
- §A.2 Reward Function Design (not adopted)

## Motivation

Harvey's current memory architecture is a heuristic pipeline: RAG chunks are injected per-prompt, memory-store recall happens once at session start, and the model has no agency over any of it. The AgeMem paper demonstrates that exposing memory operations as **tool calls the LLM can invoke autonomously** outperforms static injection pipelines across five long-horizon benchmarks. The improvement comes from the model learning when and what to store, retrieve, summarise, and discard — decisions that heuristics handle poorly.

Harvey cannot adopt AgeMem's three-stage GRPO reinforcement-learning training (no training infrastructure). However, the tool interface itself is separable from the training procedure. Models with reliable structured-tool support (granite4.1, Qwen2.5) benefit from having these tools available even when they invoke them on instinct rather than trained policy.

## Harvey's current memory state

| Silo | Files | Injection point | Model agency |
|---|---|---|---|
| RAG store | `rag_support.go`, `agents/rag/*.db` | Per-prompt via `ragAugment()` | None |
| Memory store | `memory_store.go`, `agents/memories/` | Session start via `UnifiedMemory.Recall()` | None |
| Knowledge base | `knowledge.go`, `agents/knowledge.db` | Optional, also via `UnifiedMemory` | None |
| Session context (STM) | `harvey.go` `History []Message` | Always present | None mid-session |

All four silos are written and read by Harvey's infrastructure, not by the model. The model cannot save a new memory, update a stale one, retrieve mid-session, summarise history, or filter distracting messages.

## Problem statement

1. **STM grows unbounded.** `History` is never pruned mid-session except when the user runs `/summarize`. Context overflow is handled reactively (after the fact) rather than proactively.

2. **LTM is write-only from the model's perspective.** The model can observe memory injected at session start but cannot add, update, or delete entries during a session.

3. **No mid-session LTM retrieval.** If a user asks about something not recalled at startup, it stays in the dark for that session.

4. **Dual injection redundancy.** With both `memory.enabled` and `rag.enabled`, RAG chunks are injected per-prompt *and* the memory store recalls at session start, potentially injecting overlapping content twice (see also: dual RAG audit in `DECISIONS.md`).

## Proposed tools

Six tools mirroring AgeMem's interface (Table 1 of the paper). Each maps to existing Harvey code so implementation is wrapping, not new infrastructure.

### STM tools

**`summary_context`**
Compresses a span of conversation history into a concise summary, replacing the original messages with a single summary entry. Reduces context size while preserving essential information. The model should invoke this proactively when context is filling.

- `span` (string): `"all"` for entire non-system history, or an integer N for the last N turns.
- Implementation: reuses the LLM-based summary logic already in `cmdSummarize` / `rollingSummaryPrompt`. The resulting summary replaces the covered messages in `a.History`.
- Recorder: `RecordAgentAction` with `[[summary: N turns → compressed]]`.

**`filter_context`**
Removes messages from the active context whose content is irrelevant to a given criterion. Suppresses noise and distractors. Particularly useful when a prior topic dominates the context but is no longer relevant.

- `criteria` (string): natural-language description of what to remove (e.g. "questions about Python installation").
- Implementation: embed `criteria` using the active embedder; cosine-score each `History` message; remove those scoring above threshold θ_f (default 0.6, matching AgeMem §A.1). Messages with role `system` are never removed.
- Requires an embedder. Falls back to keyword matching if none is configured.

**`retrieve_memory`**
Semantic search across all three LTM silos (memory store, RAG store, knowledge base), returning the top-k most relevant entries and injecting them as a system message into the current context.

- `query` (string): retrieval query.
- `top_k` (int, default 3): maximum results.
- Implementation: wraps `UnifiedMemory.Recall(query, embedder, budget)`. Result is prepended as a `system` message so subsequent turns can reference it.
- This provides *on-demand* mid-session recall as a complement to the startup-time batch recall already in place.

### LTM tools

**`add_memory`**
Saves a new entry to the memory store for future sessions. The model uses this when it identifies information worth preserving across sessions: user preferences, project decisions, tool-use patterns.

- `content` (string): the memory body.
- `memory_type` (string): one of the valid `MemoryType` constants (`tool_use`, `workflow`, `user_preference`, `workspace_profile`, `project_fact`).
- `tags` ([]string, optional): concept tags.
- Implementation: constructs a `MemoryDoc` with auto-generated ID and calls `MemoryStore.Save()`. Requires `a.Memory.Store != nil`.
- Recorder: `RecordAgentAction` with `[[memory: saved <ID>]]`.

**`update_memory`**
Modifies the content of an existing memory entry identified by ID. Use when new information supersedes or corrects a previously stored fact.

- `id` (string): memory ID from a prior `retrieve_memory` result.
- `content` (string): replacement content.
- Implementation: loads existing `MemoryDoc` by ID, updates the body text and modification timestamp, re-saves.

**`delete_memory`**
Archives (soft-deletes) a memory entry by setting its confidence to 0.0, matching the existing `/memory forget` behaviour. The entry is excluded from future recall but preserved in the store for audit purposes.

- `id` (string): memory ID.
- Implementation: wraps the existing confidence-zeroing path in `MemoryStore`.

## Proactive context threshold

Without RL training, the model will not call `summary_context` unless prompted. A lightweight heuristic guard in `runChatTurn`: if `remainingContext(a)` falls below a configured fraction of the total context limit (default 20%), inject a brief `system` message nudging the model to invoke `summary_context` before the next turn consumes the remaining budget. This captures most of AgeMem's "preventive management" benefit without training.

Config key: `chunking.stm_warn_pct` (float, default 0.20). Disable by setting to 0.

## Relationship to the dual RAG audit

After `retrieve_memory` is available as a tool, the per-prompt `ragAugment()` call becomes redundant for sessions where the model is reliably calling `retrieve_memory` itself. The dual RAG audit (deferred in `DECISIONS.md`) can then become a simple config switch: `rag.per_prompt: bool` (default `true` for backward compatibility). For sessions with a capable model and STM tools registered, recommend `rag.per_prompt: false` and let the model drive retrieval.

## What we are NOT doing

- Three-stage GRPO training or any reward-function infrastructure — Harvey is an inference-time agent with no training loop.
- Automatic FILTER threshold tuning — θ_f = 0.6 is fixed (AgeMem Table 5 shows stability across 0.4–0.8).
- Storing embeddings for History messages at write time — filter will use on-the-fly embedding of each message when `filter_context` is called.
- Changing the existing startup-time `UnifiedMemory.Recall()` — it stays. `retrieve_memory` is additive.

## Implementation notes

All six tools register via `RegisterBuiltinTools` in `builtin_tools.go`. They require `a.Memory.Store != nil` (LTM tools) or `a.Client != nil` (STM tools that call the LLM for summarisation). The tools should check prerequisites and return a clear error string (not a Go error) when they are unavailable, so the model can reason about the failure.

Permission model: `add_memory`, `update_memory`, `delete_memory` should respect `safe_mode`. In safe mode, confirm before writing. `filter_context` is destructive to the session context and should also check `safe_mode`.
