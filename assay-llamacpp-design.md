# Harvey Assay — llama.cpp Backend Support — Design

**Status (2026-06-30):** Design settled. See
[assay-llamacpp-plan.md](assay-llamacpp-plan.md) for the implementation plan.

---

## Background

`bin/assay` is Harvey's LLM evaluation harness. It reads a corpus of prompts
from `agents/assay/corpus.yaml`, sends each prompt to a model backend, scores
responses against `contains:` checks, and writes a Markdown + JSON report.

As of 2026-06-30, assay supports two backends:

| Backend | Flag | How connected |
|---|---|---|
| Ollama | `--ollama URL` | HTTP to running Ollama daemon |
| Llamafile | `--llamafile PATH` | Assay starts the binary, finds a free port, stops on exit |

Both back-ends are called via a private `callOllama` function that speaks
Ollama's proprietary `/api/chat` JSON shape. For llamafile this works because
llamafile also exposes the Ollama-compatible endpoint alongside its
OpenAI-compatible one.

**llama.cpp** (`llama-server`) does **not** expose the Ollama endpoint. It
speaks only the OpenAI-compatible `/v1/chat/completions` API. The harvey
package already wraps this via `AnyLLMClient` (in `anyllm_client.go`), but
assay has never imported that type.

---

## Goals

1. `bin/assay --llamacpp URL` connects to a running `llama-server` instance
   and runs the full corpus evaluation against it.
2. The existing `--ollama` and `--llamafile` flags continue to work unchanged.
3. The report header identifies the backend as "LlamaCpp" and records the URL.
4. Token stats (prompt tokens, reply tokens, TPS) are correctly populated for
   all three backends.
5. RAG evaluation (`--rag-db`, `--rag-compare`) continues to work; embeddings
   still use Ollama (the only embedding backend available).

---

## Non-Goals

- Assay does **not** start or stop `llama-server`. The user is responsible for
  starting it before running assay and stopping it afterward. This matches the
  `--ollama` pattern and avoids assay needing to know the model path, GPU
  layers, context size, and other server flags.
- No new embedding backend. RAG always requires Ollama regardless of inference
  backend — same constraint as llamafile.
- No automatic model discovery for llama.cpp. `llama-server` does expose
  `/v1/models` but returns only the one loaded model. The user must supply
  `--models NAME` explicitly when using `--llamacpp`. If `--models` is omitted
  and `--llamacpp` is set, assay queries `/v1/models` and uses whatever the
  server reports.

---

## Design

### Flag interface

```
bin/assay --llamacpp URL [--models NAME] [other flags]
```

`URL` — full base URL of the running `llama-server`, e.g.
`http://localhost:8081`. The `/v1/chat/completions` path is appended
internally.

When `--llamacpp` is set, `--ollama` is used only for embeddings (RAG path).
They are not mutually exclusive.

### Replacing `callOllama` with `AnyLLMClient`

The current `callOllama` function is Ollama-specific. Rather than adding a
parallel `callOpenAI` function, the cleaner fix is to replace `callOllama`
with `harvey.AnyLLMClient` (already tested against Ollama, llamafile, and
llama.cpp in the harvey package).

`AnyLLMClient` accepts an OpenAI-compatible base URL and model name. For
Ollama, the base URL is `http://localhost:11434/v1` (Ollama exposes an OpenAI-
compatible shim at `/v1`). For llamafile and llama.cpp, it is whatever port
the server is listening on.

This eliminates the private `ollamaRequest`, `ollamaMessage`, and
`ollamaResponse` structs entirely. Token stats come from `harvey.ChatStats`
(returned by `AnyLLMClient.Chat`).

### Stats mapping

| Old field (`ollamaResponse`) | New source (`harvey.ChatStats`) |
|---|---|
| `EvalCount` (reply tokens) | `ChatStats.ReplyTokens` |
| `PromptEvalCount` | `ChatStats.PromptTokens` |
| TPS computed from `EvalDuration` | `ChatStats.TokensPerSec` |

### Backend selection logic

```
--llamacpp set  →  backend = "LlamaCpp",  llmURL = *llamacppURL + "/v1"
--llamafile set →  backend = "Llamafile", llmURL = derived from free port + "/v1"
(neither set)   →  backend = "Ollama",    llmURL = *ollamaURL + "/v1"
```

Mutually exclusive: `--llamafile` and `--llamacpp` cannot both be set. If
both are provided, assay exits with an error.

### Model name resolution

| Condition | How model name is obtained |
|---|---|
| `--llamafile PATH` | `harvey.LlamafileModelNameFromPath(PATH)` (existing) |
| `--llamacpp URL`, `--models NAME` | Use NAME directly |
| `--llamacpp URL`, no `--models` | Query `GET /v1/models`, use `data[0].id` |
| `--ollama URL`, `--models NAME` | Use NAME directly |
| `--ollama URL`, no `--models` | Query `GET /api/tags` (existing) |

### RAG guard for llama.cpp

When `--llamacpp` and `--rag-db` are both set, assay verifies Ollama is
reachable for embeddings before loading the corpus, exactly as it does for
llamafile:

```
assay: RAG evaluation with --llamacpp requires Ollama for embeddings.
Start Ollama or use --ollama to specify a running instance.
```

### Report header

The report header gains a "URL" row for llama.cpp (and for llamafile now that
the URL is dynamically assigned):

```markdown
| Backend | LlamaCpp              |
| URL     | http://localhost:8081 |
| Model   | phi4-Q4_K_M           |
```

For Ollama:
```markdown
| Backend | Ollama                     |
| URL     | http://localhost:11434      |
| Model   | llama3.2:3b                |
```

For Llamafile:
```markdown
| Backend | Llamafile                               |
| Binary  | /home/user/Models/phi4-mini.llamafile   |
| Model   | phi4-mini                               |
```

---

## Migration of `callOllama`

The switch from `callOllama` to `AnyLLMClient` is internal and does not change
any flags or output format. It is the riskiest part of the change because it
touches every prompt evaluation call. Acceptance criteria include running the
corpus against Ollama after the change to confirm scores are unchanged.

`AnyLLMClient` streams responses token-by-token; assay needs the full string.
The existing pattern (pass a `strings.Builder` as `out`, read after `Chat`
returns) already handles this correctly in the harvey package.

---

## Out of Scope

- Starting/stopping `llama-server` from assay. Users manage the process.
- Side-by-side backend comparison (e.g. Ollama vs. llama.cpp on the same
  corpus). Deferred.
- llama.cpp embedding support. Not available upstream.
- Corpus management CLI. Corpus remains a hand-edited YAML file.
