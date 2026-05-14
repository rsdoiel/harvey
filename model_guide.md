# Harvey Model Selection Guide

*Last updated: 2026-05-12 — model inventory from `/ollama probe` (agents/model_cache.db).
M1 Mac is the primary machine; Raspberry Pi 500+ runs a subset.*

---

## Capability legend

**Tools** — model sends `tool_call` responses that Harvey's tool executor can
dispatch (required for `/run`, `/git`, file-write operations in agent mode).

**Tagged** — model respects Harvey's ` ```path ` fenced-block syntax so
autoExecute can write files without a `/apply` prompt. `-1` = not yet probed.

---

## Installed model inventory

### Embedding-only models (not for chat)

| Model | Size | Notes |
|-------|------|-------|
| `nomic-embed-text:latest` | 137 MB | Harvey default for RAG; 2K context |
| `mxbai-embed-large` | 334 MB | Strong English-only embedding |
| `bge-m3:latest` | 567 MB | Best installed embed; 8K context, multilingual |
| `locusai/all-minilm-l6-v2:latest` | — | Lightweight sentence embedder |

Do not select these for Harvey chat sessions — they produce unusable responses.

---

### Chat / coding models by capability tier

#### Tier 1 — Very small (≤ 1 B params)

| Model | Params | Context | Tools | Tagged |
|-------|--------|---------|-------|--------|
| `smollm:360m` | 362 M | 2K | — | -1 |
| `sailor2:1b` | 988 M | 32K | — | -1 |
| `smollm:1.7b` | 1.7 B | 2K | — | -1 |

**Good for:** Trivial string transformations, testing Harvey plumbing, one-sentence RAG lookups where the model just reads injected context and echoes it back.

**Avoid for:** Any reasoning, multi-step logic, code generation, anything requiring the model to hold more than one idea at once.

**Note:** `sailor2:1b` has a surprisingly large 32K context for its size — use it
for token-constrained machines that need a slightly longer window.

---

#### Tier 2 — Small (2–4 B params)

| Model | Params | Context | Tools | Tagged |
|-------|--------|---------|-------|--------|
| `granite3-moe:3b` | 3.4 B | **4K** | ✓ | ✓ |
| `smallthinker:latest` | 3.4 B | 32K | — | -1 |
| `stable-code:3b` | 3 B | 16K | — | -1 |
| `cogito:3b` | 3.6 B | 131K | ✓ | -1 |
| `phi4-mini:latest` | 3.8 B | 131K | ✓ | -1 |

**Good for:** Single-function code generation with a clear spec, adding docstrings,
answering scoped questions about a file already in context, writing short unit tests.

**Avoid for:** Multi-file analysis, architectural decisions, anything requiring
context > 4K (for granite3-moe) or > ~16K (for others at this tier).

**Recommended defaults at this tier:**
- Agent tasks needing tool-calling *and* tagged blocks: **`granite3-moe:3b`**
  (only 4K context — short sessions only)
- Reasoning tasks: **`smallthinker:latest`** (chain-of-thought trained, 32K)
- General fast chat with large context: **`cogito:3b`** or **`phi4-mini`** (both 131K)
- Code-only, no tool support needed: **`stable-code:3b`**

---

#### Tier 3 — Medium (7–9 B params)

| Model | Params | Context | Tools | Tagged |
|-------|--------|---------|-------|--------|
| `apertus-tools:8b` | 8.1 B | 65K | ✓ | ✓ |
| `gemma2:latest` | 9.2 B | 8K | — | -1 |
| `gemma4:latest` | 8 B | 131K | ✓ | — |
| `llama3.1:latest` | 8 B | 131K | ✓ | -1 |
| `ministral-3:latest` | 8.9 B | 262K | ✓ | -1 |

**Good for:** Writing complete functions or small files, explaining existing code in
depth, writing tests that require understanding of the code under test, debugging
with a stack trace in context, most day-to-day Harvey coding tasks.

**Avoid for:** `gemma2` for anything needing more than ~8K context.

**Recommended defaults at this tier:**
- Agent/tool tasks with autoExecute: **`apertus-tools:8b`** — only 8B model confirmed
  tools=✓ *and* tagged=✓. Best pick for iterative coding loops.
- General assistant: **`llama3.1:latest`** — reliable, well-tested instruction following.
- Session handoffs / long docs: **`ministral-3:latest`** — 262K context fits entire
  session histories; Mistral models follow structured formatting reliably.

---

#### Tier 4 — Large (23–24 B params)

| Model | Params | Context | Tools | Tagged |
|-------|--------|---------|-------|--------|
| `mistral-small:latest` | 23.6 B | 32K | ✓ | -1 |
| `mistral-small3.2` | 24 B | 131K | ✓ | -1 |
| `devstral-small-2:24b` | 24 B | **393K** | ✓ | — |

**Good for:** Multi-file refactoring, architectural reasoning, writing new features
end-to-end, security review, writing documentation, anything that benefits from
a large context window and strong reasoning.

**Notes:**
- **`devstral-small-2:24b`** is Mistral's coding-specialized model with the largest
  context window installed (393K tokens). First choice for complex software tasks
  on the M1 Mac.
- **`mistral-small3.2`** (131K) is the best general-purpose large model when you
  don't need coding specialisation.
- **`mistral-small:latest`** (32K context, older version) — prefer `mistral-small3.2`.
- All Tier 4 models **require ~16–20 GB free RAM**. They will not run on the
  Raspberry Pi 500+.

---

## Hardware constraints

| Machine | Max practical model | Notes |
|---------|---------------------|-------|
| M1 Mac | `devstral-small-2:24b` (15 GB) | All models available |
| Raspberry Pi 500+ | `ministral-3:latest` (8.9B) or smaller | 24B models will not run |

When working on the Pi, treat `ministral-3:latest` as the Tier 4 ceiling and
`apertus-tools:8b` as the everyday coding model.

---

## Task rubric

| Task | Min tier | Recommended (Mac) | Recommended (Pi) |
|------|----------|-------------------|------------------|
| Add a docstring | 2 | `phi4-mini` | `phi4-mini` |
| Quick Q&A on a `/read` file | 2 | `cogito:3b` | `cogito:3b` |
| Write a unit test (known signature) | 2–3 | `apertus-tools:8b` | `apertus-tools:8b` |
| Fix a bug with error + context | 2–3 | `apertus-tools:8b` | `apertus-tools:8b` |
| Write a new function | 3 | `apertus-tools:8b` | `apertus-tools:8b` |
| Write a complete new file | 3 | `apertus-tools:8b` | `llama3.1` |
| Debug across multiple files | 3–4 | `ministral-3` | `ministral-3` |
| Multi-file feature (new code) | 4 | `devstral-small-2:24b` | `ministral-3` |
| Architectural review | 4 | `devstral-small-2:24b` | `mistral-small3.2`* |
| Write documentation | 3–4 | `ministral-3` | `ministral-3` |
| Security review | 4 | `devstral-small-2:24b` | `ministral-3` |
| Reasoning / multi-step planning | 2–4 | `smallthinker` or `mistral-small3.2` | `smallthinker` |
| Agent loop (tool-calling + file writes) | 2–3 | `apertus-tools:8b` | `apertus-tools:8b` |
| Session handoff (Fountain writing) | 3 | `ministral-3` | `ministral-3` |
| RAG-augmented Q&A | 1–2 | `cogito:3b` | `cogito:3b` |
| Embedding for RAG | — | `bge-m3:latest` | `nomic-embed-text` |

*mistral-small3.2 is 24B and may be marginal on Pi; use with care.

---

## Suggested model aliases

Add to `agents/harvey.yaml` under `model_aliases:`. Use
`/model alias set NAME MODEL` or edit the file directly.

```yaml
model_aliases:
  # Coding and agent work
  agent:      abb-decide/apertus-tools:8b-instruct-2509-q4_k_m
  agent-fast: granite3-moe:3b
  coder:      devstral-small-2:24b

  # General assistant
  chat:       llama3.1:latest
  chat-big:   mistral-small3.2
  fast:       cogito:3b

  # Long-context / documents
  docs:       ministral-3:latest
  long:       devstral-small-2:24b

  # Reasoning
  think:      smallthinker:latest

  # Code only (no tools needed)
  code-light: stable-code:3b

  # Embedding (for /rag setup)
  embed:      bge-m3:latest
  embed-fast: nomic-embed-text:latest
```

---

## Planning a Harvey work session

Before starting, ask:

1. **What is the task?** Pick a tier and model from the table above.
2. **Which machine?** `devstral-small-2:24b` needs ~16 GB free — Mac only.
3. **Do I need tool-calling?** Agent mode requires tools=✓. Use `apertus-tools:8b`
   or `granite3-moe:3b` (short context) for reliable tool dispatch.
4. **How much context?** If `/read-dir` loads a full package, pick a 131K+ model.
   Tier 2 models below 32K will truncate silently.
5. **Is RAG useful?** If the task involves an API the model doesn't know well,
   run `/rag on` before asking.

### Typical session setup

```
# 1. Start Harvey
harvey

# 2. Select model mid-session (or use an alias):
/model agent              # → apertus-tools:8b (agent tasks)
/model coder              # → devstral-small-2:24b (complex coding)
/model docs               # → ministral-3:latest (long documents)
/model fast               # → cogito:3b (quick Q&A)

# 3. Enable RAG if needed
/rag on

# 4. Load context
/read harvey/commands.go
/read-dir harvey/ --depth 1

# 5. Optionally load a skill bundle
/skill-set load fountain
```

---

## What Harvey can realistically do with small models

Small models (Tier 2) work well when:
- The task is **scoped to a single function** and you provide the signature
- You have **injected the relevant file** via `/read` first
- You ask for **one thing at a time** (no "also do X and Y and Z")
- The answer fits in **a few hundred tokens**

Small models fail when:
- Asked to reason about code they haven't seen
- Context is filled with unrelated content
- Asked to make design decisions without constraints
- Asked to generate large amounts of code in one shot

**Best pattern for small models in Harvey:**
1. `/read` the specific file or function
2. Ask a focused question or give a tightly scoped task
3. Review the output before using `/apply`

---

## Keeping this guide current

Run `/ollama probe` after installing new models to update `agents/model_cache.db`.
Re-examine the capability columns (tools, tagged blocks) and update the inventory
table above when results change. The `probed_at` column tracks when each entry
was last verified.
