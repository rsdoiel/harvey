# Harvey Unified Model Support — Design

**Status (2026-06-28):** Design draft.
See [unified-models-plan.md](unified-models-plan.md) for the phased
implementation plan.

---

## Background

Harvey supports three model backends: **Ollama** (active), **Llamafile**
(active), and **llama.cpp** (planned). Each backend has its own model
management, configuration, and startup lifecycle. As a result, the
user experience for registering models and routing to them diverges
across backends, and metadata important for model selection is either
missing or captured inconsistently.

Two open questions from [unified-models-questions.md](unified-models-questions.md)
drive this design:

1. **Purpose metadata**: How does a user tell Harvey that a given model
   is good at code generation but not vision, so that Harvey (and the
   user via `@mention`) can route to the right model for the task?

2. **Cross-backend discovery**: How does Harvey integrate with models
   already installed by Ollama or llama.cpp/llamafile without
   re-downloading them?

---

## Current state

### Ollama

Harvey probes `/api/tags` to list installed models and `/api/show` for
per-model metadata (context size, parameter count, tool-call capability).
This is stored in `model_cache.db` as operational capability data.

`model_aliases` in `harvey.yaml` maps a short name to the full Ollama
model ID (e.g. `"granite" → "granite3.3:8b"`). Aliases are set via
`/ollama alias set` and used for `@mention` routing.

**Gap**: `/ollama use MODEL` switches the active model but does not
prompt for an alias. Users who want `@mention` routing must run
`/ollama alias set` as a separate step, which they often skip.

### Llamafile

`/llamafile add [PATH]` scans `~/Models` for `.llamafile` files, shows
a numbered picker, prompts for a short name, starts the process, and
saves the registration to `harvey.yaml`. The name is the alias used
for `@mention`.

`/llamafile use NAME` switches between already-registered models.

**Gap**: No purpose/capability tags beyond what the operational probe
captures (context size, tool-call support, embedding ability).

### llama.cpp (planned)

llama.cpp uses `.gguf` model files (the same format as many llamafiles
internally) and a separate `llama-server` binary. The model management
pattern is identical to llamafile (files in `~/Models`, external binary,
local HTTP server) but with a different binary and flag interface.

**Gap**: No Harvey integration yet.

---

## Design goals

1. **Consistent registration UX across backends.** All three backends
   converge on the same gesture: pick a model file or installed model,
   give it a short name (alias), optionally annotate its purpose. Harvey
   saves the registration and routes `@mention` through the alias.

2. **Purpose tags.** Extend model aliases from a flat string map to a
   struct carrying the full model name plus an optional tags list.
   Tags are user-authored (not auto-probed), short, and drawn from a
   common vocabulary. Example: `tags: [code, instruct]`.

3. **No re-downloading installed models.** Harvey discovers models
   already available to each backend and offers them in pickers rather
   than requiring manual path entry or a separate pull step.

4. **`@mention` routing by tag.** When the user types `@code`, Harvey
   resolves it to the highest-capability model tagged `code` (combining
   purpose tag match with the existing capability score from
   `model_cache.db`).

---

## Architecture

### U1 — `/ollama use` alias prompt

After switching to a model via `/ollama use`, Harvey checks whether any
entry in `Config.ModelAliases` already maps to that model name. If none
is found, it prompts:

```
Short alias for 'granite3.3:8b' (Enter to skip): 
```

If the user provides a non-empty alias, Harvey calls the existing
`/ollama alias set` logic and saves to `harvey.yaml`. This brings
`/ollama use` into alignment with the registration UX of `/llamafile add`.

No structural change to the alias storage format is required for this
step; purpose tags come in U2.

### U2 — Purpose tags in model aliases

**Current YAML:**
```yaml
model_aliases:
  granite: granite3.3:8b
  qwen: qwen3:8b
```

**New YAML (backward-compatible):**
```yaml
model_aliases:
  granite:
    model: granite3.3:8b
    tags: [code, instruct]
  qwen:
    model: qwen3:8b
    tags: [reasoning, instruct]
```

To preserve backward compatibility, the YAML loader accepts both forms:
a plain string value is treated as `model: VALUE, tags: []`.

In Go, `ModelAliases` changes from `map[string]string` to
`map[string]ModelAlias`:

```go
type ModelAlias struct {
    Model string   // full model name passed to the backend
    Tags  []string // e.g. ["code", "instruct", "embedding", "vision", "reasoning"]
}
```

The helper `Config.ResolveAlias(alias string) string` continues to work
unchanged from callers' perspective; it reads `ModelAlias.Model`.

**Common tag vocabulary:**

| Tag | Meaning |
|-----|---------|
| `code` | Trained for code generation / completion |
| `embedding` | Produces text embeddings (not chat) |
| `vision` | Accepts image input |
| `reasoning` | Extended chain-of-thought / math reasoning |
| `instruct` | Fine-tuned to follow instructions |
| `general` | General-purpose chat; no specialisation |

Tags are advisory — there is no validation against this list. Users may
add their own.

**Management commands** — extend `/ollama alias`:

```
/ollama alias set CODE granite3.3:8b --tags code,instruct
/ollama alias tags CODE code instruct
/ollama alias list              # existing; shows tags column
```

### U3 — `@mention` routing by purpose tag

When the user types `@code`, the routing resolver currently looks up
`code` as a literal alias. With U2 in place, the resolver also searches
tags:

1. Exact alias name match → use that model.
2. Tag match → collect all aliases with that tag; pick the one with the
   highest `CapabilityStatus.Score` in `model_cache.db`; fall back to
   first listed if no score data.
3. No match → existing "model not found" behaviour.

This extends the existing routing resolver in `routing.go`; no change
to the `@mention` parsing or the REPL loop is required.

### U4 — llama.cpp backend (mirroring llamafile)

llama.cpp uses `.gguf` files in `~/Models` (same directory as llamafile)
and a separate `llama-server` binary. The registration and use flow
mirrors llamafile exactly:

| Step | Llamafile | llama.cpp |
|------|-----------|-----------|
| Scan dir | `scanLlamafileModels` finds `*.llamafile` | `scanLlamacppModels` finds `*.gguf` |
| Start server | Execute the `.llamafile` binary | Run `llama-server --model PATH` |
| Stop server | `proc.Kill()` | Same |
| Probe ready | `ProbeLlamafile(url)` | Same URL, same probe |
| Harvey config | `LlamafileEntry{Name, Path, ContextLength}` | `LlamacppEntry{Name, Path, ContextLength, ServerBin}` |

`ServerBin` is the path to the `llama-server` binary (default: `llama-server`
on `$PATH`). Since both backends serve the same OpenAI-compatible HTTP
API, the rest of the chat loop is unchanged.

Commands: `/llamacpp add`, `/llamacpp use`, `/llamacpp list`,
`/llamacpp drop`, `/llamacpp status` — exact same subcommand set as
`/llamafile`.

### U5 — Cross-workspace model registry (deferred)

Ollama is typically a single system-wide server; all Harvey workspaces
share the same installed models. The current design requires each
workspace's `harvey.yaml` to list aliases independently, leading to
repetition.

A future global registry at `$HOME/.config/harvey/models.yaml` (or
merged into a global `harvey.yaml`) would let all workspaces inherit
aliases and tags defined once, with per-workspace overrides. This
requires a merge strategy for the config loader and is deferred until
the per-workspace alias UX (U1–U3) is stable.

---

## Alternatives considered

**Auto-probing purpose from model metadata.** Ollama's `/api/show`
returns the Modelfile, which sometimes contains a SYSTEM prompt hinting
at purpose. HuggingFace model cards use structured tags. Both sources
are inconsistent and require parsing heuristics. User annotation at
registration time is more reliable and already matches the workflow for
llamafile naming. Auto-probing can supplement user tags in a later pass
if needed.

**Separate `purposes.yaml` file.** Keeping purpose metadata outside
`harvey.yaml` avoids touching `model_aliases`. Rejected: splitting
alias and purpose data across two files adds lookup complexity and a
second file to keep in sync. Embedding tags in the alias entry keeps
the information co-located.

**Tag validation against a fixed vocabulary.** Enforcing a closed tag
set avoids misspellings but prevents users from using domain-specific
terms (e.g. `medical`, `legal`, `japanese`). Open vocabulary with a
documented common set is a better fit for a single-user tool.

**llama.cpp as a separate binary.** llama.cpp could be wrapped into a
llamafile-style self-contained binary. In practice, users who have
llama.cpp installed already have `llama-server` and `.gguf` files.
Harvey should work with that setup rather than require repackaging.

---

## Appendix — ONNX in-process embedding (future path)

### What ONNX would add

Harvey currently has two embedder backends, both HTTP-based: `OllamaEmbedder`
(calls Ollama's `/api/embeddings`) and `EncoderfileEmbedder` (calls a
local Encoderfile HTTP server). Both require an external process to be
running before embedding can happen.

An `ONNXEmbedder` would run an embedding model **in-process** — no
server, no HTTP round-trip, no external dependency at query time. The
model file (`.onnx`) is loaded once at startup; `Embed(text)` calls
directly into the ONNX Runtime C library via CGo. This is the lowest
possible latency path and works fully offline.

The `Embedder` interface Harvey already defines is the right abstraction;
`ONNXEmbedder` would implement it alongside the existing two backends.
`NewEmbedderForEntry` would dispatch on `embedder_kind: "onnx"`, reading
`embedder_url` as a local file path to the `.onnx` model file.

### Obstacles

**CGo dependency.** Every production-grade Go ONNX binding
(`onnxruntime_go` by yalue-gio is the main one) wraps the ONNX Runtime
C shared library via CGo. Harvey currently cross-compiles to six targets
(Linux x86/ARM, macOS x86/ARM64, Windows) as pure Go. Adding CGo
requires pre-built ONNX Runtime shared libraries for each target and a
more complex build pipeline. The ONNX embedder would need to be behind
a build tag so non-CGo builds remain possible.

**Tokenization.** ONNX Runtime runs the model computation graph, but
the caller must tokenize raw text into token ID sequences first — in
exactly the vocabulary and format the embedding model expects. HuggingFace
models use a `tokenizer.json` file. There is no mature pure-Go
implementation of the full HuggingFace tokenizers spec; the available Go
options (`sugarme/tokenizer`) are also CGo-based. The tokenizer is not
optional — without it, the ONNX model cannot be called at all.

These two obstacles mean the ONNX path adds significant build complexity
with no functional gain over the existing `EncoderfileEmbedder` when an
Encoderfile server is available. The right trigger for revisiting this is
a stable pure-Go tokenizer or an acceptable CGo build strategy.

### Role of Henry (the llamafile factory)

Henry currently packages GGUF chat models into self-contained llamafile
executables. The same packaging concept applies to ONNX embedding models:
bundle the `.onnx` model file + ONNX Runtime + a small HTTP server
shim into a single executable, producing an Encoderfile-compatible
binary that Harvey's existing `EncoderfileEmbedder` can consume without
any code changes to Harvey.

This is actually a cleaner path than native ONNX embedding in Harvey:

```
HuggingFace ONNX embedding model
  → Henry packages it as an Encoderfile-style binary
  → Harvey uses it via the existing EncoderfileEmbedder (embedder_kind: encoderfile)
  → No CGo in Harvey; no tokenizer problem in Harvey
```

Henry would need a new `package-encoderfile.sh` script (or a new YAML
model type, e.g. `kind: encoderfile`) alongside its existing
`package.sh` for llamafile/GGUF models. The ONNX Runtime library would
be bundled into the binary using the same APE polyglot technique that
llamafile uses for llama.cpp.

### Role of Mable (the model builder)

Mable trains a transformer model from scratch on a curated corpus and
targets deployment via Ollama (i.e. conversion to GGUF). ONNX connects
to Mable in two ways:

**1. Export path for the trained model.** PyTorch models can be exported
to ONNX via `torch.onnx.export` or the HuggingFace `optimum` library.
This is not useful for the generative (decoder) model Mable is building
— Ollama/GGUF is the right target for that. But if Mable ever trains an
**encoder** model on the same corpus (for semantic similarity or
embedding-based retrieval within the classical corpus), ONNX is the
natural export format, and that model could be packaged by Henry and
consumed by Harvey's RAG pipeline.

**2. Tokenizer export.** Mable trains a custom BPE tokenizer (vocab
50,000) using HuggingFace tokenizers. That tokenizer is already stored
as `tokenizer.json` — the exact format an ONNX embedding model would
need. If Mable later exports an ONNX embedding model, the tokenizer is
already in the right format; no conversion step is needed.

**The full pipeline, when mature:**

```
Mable trains classical corpus embedding model (PyTorch)
  → exports encoder to ONNX via optimum
  → Henry packages ONNX model + tokenizer.json + ONNX Runtime
      as a self-contained Encoderfile binary
  → Harvey /rag new classical --embedder encoderfile --embedder-url http://localhost:8080
  → RAG queries use Mable's classical-corpus embedding space
```

This means Harvey's RAG retrieval for classical texts would use
embeddings from a model that "understands" that corpus rather than a
general-purpose embedding model like nomic-embed-text. That is a
meaningful quality improvement for Mable's intended use.

### Summary

| Project | ONNX role | When relevant |
|---------|-----------|---------------|
| **Harvey** | `ONNXEmbedder` implementing `Embedder` | After pure-Go tokenizer matures or CGo accepted |
| **Henry** | Package ONNX models as Encoderfile binaries | As soon as an ONNX embedding model worth packaging exists |
| **Mable** | Export trained encoder to ONNX; tokenizer.json already compatible | When/if Mable trains an encoder model alongside its decoder |

The near-term path that requires no new CGo work: Mable exports an
encoder model → Henry packages it as an Encoderfile binary → Harvey
consumes it via the existing `EncoderfileEmbedder`. Native ONNX support
in Harvey (`ONNXEmbedder`) is a later optimization, not a prerequisite.
