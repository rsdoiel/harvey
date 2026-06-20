# Harvey v0.0.14 — Llamafile-Primary UX Design

**Status (2026-06-20):** Design settled. See
[llamafile-primary-plan.md](llamafile-primary-plan.md) for the implementation
plan and [DECISIONS.md](DECISIONS.md) for key architectural choices.

---

## Motivation

Harvey has supported Llamafile since v0.0.11, but Ollama remains the assumed
primary backend in documentation, startup logic, and UX flow. Users who want
a self-contained local setup — no separate server process, no model downloads
through a package manager — have to discover Llamafile support through man
pages rather than finding it in the natural startup path.

Llamafile's model-as-single-executable design aligns well with Harvey's
local-first philosophy and single-binary install model. Making Llamafile the
primary path reduces setup friction for new users, especially on platforms
where Ollama is less convenient (Windows, isolated servers, Raspberry Pi).

Ollama remains fully supported. It becomes the "advanced" path for users who
want a persistent model server, GPU pooling, or access to the full Ollama
model library.

---

## Design Principles

1. **Local-first by default.** If a Llamafile is registered and its server
   can be started, that is the preferred backend. No network required.

2. **Explicit connection feedback.** Users on slower hardware (Raspberry Pi,
   older laptops) need to know Harvey is waiting for the model server, not
   hung. Every backend connection attempt is narrated.

3. **Progressive disclosure.** Getting started shows the simplest path
   (download a llamafile, run Harvey). Advanced features (routing, Ollama,
   RAG) are documented in their own sections.

4. **Preserve personality.** The Lear quotes, spinner, and terse output style
   are unchanged. UX additions are additive, not replacements.

5. **Fail loudly with a path forward.** When no backend is reachable, Harvey
   does not just error — it walks the user toward a solution.

---

## Startup & Connection

### Backend detection order

At startup, Harvey probes backends in this order:

1. **Active Llamafile** — if `LlamafileActive` is set in `harvey.yaml`, probe
   the Llamafile URL. If reachable, connect and proceed.
2. **Registered Llamafiles** — if the active Llamafile is not reachable but
   registered models exist, offer to start the active one (or pick from a
   numbered list).
3. **Ollama** — probe the Ollama URL. If reachable, connect and proceed.
4. **First-run wizard** — if nothing is reachable, run the onboarding wizard.

When no session is being continued (`--continue` / `--resume`), and multiple
backends are available, Harvey presents Llamafile choices first:

```
Available models:

  Llamafile
    1. qwen-coding   (Qwen2.5-Coder-7B, 5.1GB)   ← registered
    2. phi-mini      (Phi-3.5-mini, 2.4GB)         ← registered

  Ollama
    3. llama3.2:3b
    4. nomic-embed-text

Select [1-4] or press Enter to use qwen-coding:
```

When a session is being continued (`--continue` / `--resume`), the picker is
skipped and Harvey connects to the previously-active model directly.

### Connection feedback

Every backend connection attempt emits an explicit status line before the
ready prompt:

```
Harvey 0.0.14  (workspace: ~/src/myproject)
Connecting to qwen-coding (llamafile)… ✓ ready
```

If the model takes more than two seconds to become ready (e.g. startup time):

```
Connecting to qwen-coding (llamafile)… starting server
  ⎿ ⠸ [4s / ~15s]
Connecting to qwen-coding (llamafile)… ✓ ready (18s)
```

If connection fails:

```
Connecting to qwen-coding (llamafile)… ✗ failed
  Could not start server: exit status 1
  Stderr: error loading model weights
  Use /llamafile drop qwen-coding to remove this model, or check the path.
```

### First-run onboarding wizard

When Harvey starts and no backend is reachable, it prints a guided setup
rather than an error:

```
Harvey couldn't find a model to connect to.

To get started with a local model (no internet required after download):

  1. Download a llamafile from:
       https://huggingface.co/Mozilla/llamafile-models
     Recommended for most hardware: Qwen2.5-Coder-7B-Q5_K_S.llamafile (~5GB)
     Low-memory option:              Phi-3.5-mini-instruct-Q4.llamafile (~2GB)

  2. Place it in ~/Models/ (or any directory)

  3. Run Harvey again — it will find the file automatically.

Alternatively, install Ollama (https://ollama.com) and pull a model:
  ollama pull qwen2.5-coder:7b

Press Enter to exit, or type a path to a llamafile to add it now:
```

If the user types a path, Harvey validates it and runs the normal
`/llamafile add` flow before entering the REPL.

### Stale external server adoption

When `/llamafile add` (or startup) discovers a llamafile server already
running at the configured URL that Harvey did not start:

1. Probe `GET /v1/models` on the running server.
2. Extract the model name from the response.
3. Present an adoption offer:

```
  A llamafile server is already running at http://localhost:8080
  Detected model: Qwen2.5-Coder-7B-Q5_K_S
  Adopt this as the active model? [Y/n]:
```

If accepted, Harvey registers the model under the detected name, sets it as
active, and saves to `agents/harvey.yaml`. Harvey does not manage the
process lifecycle (it did not start it, so `a.llamafileProc` remains nil).
The managed/unmanaged distinction is already tracked by the existing
`a.llamafileProc != nil` check.

### Health check on `--resume`

Before loading a resumed session's history, Harvey checks whether the
previously-active model is reachable. Detection uses the same backend probing
as normal startup. If the model is not reachable:

```
Session: agents/sessions/2026-06-19-141523.spmd
Model qwen-coding is not running.
Start it now? [Y/n]:
```

If the user declines, Harvey loads the session context but does not connect to
a backend, allowing the user to switch models manually before chatting.

---

## Mid-Session Awareness

### Auto-reconnect on dropped Llamafile

When a chat request returns a connection error (HTTP transport error, not an
LLM-level error), Harvey checks whether the Llamafile process it started has
exited:

```go
if a.llamafileProc != nil && a.llamafileProc.ProcessExited() {
    // offer restart
}
```

If the process has exited, Harvey interrupts the current turn and prompts:

```
  ⚠ The llamafile server stopped unexpectedly (exit status 1).
  Restart qwen-coding? [Y/n]:
```

If yes, Harvey restarts the process and retries the original prompt once. If
restart fails, Harvey reports the error and returns to the REPL without
retrying. Retrying more than once risks masking a persistent crash loop.

The prompt text is not lost — it is preserved in the REPL input buffer so
the user can re-submit it after a successful restart.

### Context utilization indicator

Small llamafiles (4B–8B parameters) typically have a 4k–8k token context
window. When Harvey knows the active model's context length, it shows a
utilization percentage on the status line after each turn:

```
Harvey 0.0.14  (workspace: ~/src/myproject)  [ctx: 34%]
```

The `[ctx: N%]` suffix updates after every turn. It is omitted when the
context length is unknown (avoids a misleading "0%").

**Context length discovery (in priority order):**

1. `context_length` field on the `LlamafileEntry` in `harvey.yaml` — explicit
   user override; useful when the server does not expose the value or the user
   wants to cap the displayed window.
2. `/v1/models` API response after startup — field `data[0].meta.n_ctx` in the
   llamafile response (tested on Qwen3.5-2B, Qwen3.5-4B, and Apertus-8B;
   consistently present). This is the *runtime* context window — how much
   llamafile actually loaded — not the training context (`n_ctx_train`).
3. Ollama `/api/show` response — `ContextLength` already populated by
   `ShowModel` in `ollama.go`.

**Token count source:** the `usage.prompt_tokens` field returned with each LLM
response (already tracked in `ChatStats`). Harvey accumulates prompt tokens
across the session (the full history is re-sent each turn) rather than summing
per-turn deltas — the last turn's `prompt_tokens` is the current context size.

**New config field on `LlamafileEntry`:**

```go
type LlamafileEntry struct {
    Name          string `yaml:"name"`
    Path          string `yaml:"path"`
    ContextLength int    `yaml:"context_length,omitempty"` // tokens; 0 = probe from server
}
```

### Routing feedback in spinner

When a turn is handled by a route other than the default model, the spinner
shows which model handled it:

```
  ⎿ routed → coding-model
```

This reuses the existing `UpdateStatus` channel added in v0.0.13. The routing
layer already knows which endpoint handled a request; it passes the route name
to the caller, which calls `spin.UpdateStatus(fmt.Sprintf("routed → %s", name))`.

### At-mention model switch

If the user's input begins with `@name`, where `name` matches a registered
Llamafile or Ollama model, Harvey switches to that model and forwards the
remainder of the input as the prompt:

```
you> @phi-mini rewrite this in a more concise style
  Switching to phi-mini…
  Starting phi-mini… ✓ ready (12s)
```

The existing conversation history is preserved unchanged. The model switch
takes effect for this turn and all subsequent turns. The session recorder
emits a model-switch note (see Session Quality below).

**Parsing:**

```
@<name> <rest-of-prompt>
```

- `name` is the token immediately after `@`, up to the first space or
  end-of-line.
- `name` is looked up in `LlamafileModels` first, then Ollama models.
- If `name` is not found, Harvey treats the whole input as a normal prompt
  (no silent fail — just no switch).
- If `name` is found but switching fails, Harvey reports the error and
  cancels the turn.

The `@name` prefix is stripped before the prompt is sent to the model.

---

## Model Management Ergonomics

### Unified `/model` command

A new top-level command `/model` provides a backend-agnostic interface for
common model operations:

| Subcommand | Effect |
|---|---|
| `/model` (no args) | Print the active model name and backend |
| `/model list` | List all registered models across both backends |
| `/model use NAME` | Switch to any registered model (llamafile or Ollama) |
| `/model status` | Equivalent to `/llamafile status` or `/ollama status` for the active backend |

`/model use NAME` resolves the name by checking `LlamafileModels` first, then
Ollama models via `/api/tags`. The switch delegates to the existing
`cmdLlamafileUse` or `cmdOllamaUse` logic — no new switching code.

`/model list` merges both lists and marks the active entry:

```
  Active backend: llamafile
  → qwen-coding    (llamafile)  /home/user/Models/Qwen2.5-Coder-7B.llamafile
    phi-mini       (llamafile)  /home/user/Models/Phi-3.5-mini.llamafile
    llama3.2:3b    (ollama)
    nomic-embed-text (ollama)
```

### `/llamafile remove` alias

`remove` is registered as an alias for the `drop` subcommand inside
`cmdLlamafile`. No new logic — a one-line case addition.

### `/llamafile download` stub

`/llamafile download` prints a curated table of recommended models with
download guidance. No network access is performed; it is a pure information
display. The table is embedded in `helptext.go` alongside other help strings
and updated each release when new recommended models are available.

```
Recommended Llamafile models (Mozilla / HuggingFace):
  https://huggingface.co/Mozilla/llamafile-models

  Name                              Size   Best for
  ───────────────────────────────────────────────────────────────
  Qwen2.5-Coder-7B-Q5_K_S          5.1GB  Code generation, refactoring
  Qwen2.5-Coder-1.5B-Q4_K_M        1.4GB  Code generation (low VRAM / CPU)
  Phi-3.5-mini-instruct-Q4_K_M      2.3GB  Compact reasoning, general tasks
  Mistral-7B-Instruct-v0.3-Q4_K_M   4.1GB  Instruction following, writing
  Llama-3.2-3B-Instruct-Q8_0        3.5GB  General chat

Download a model, place it in ~/Models/, then:
  /llamafile add

Or pass the path directly:
  /llamafile add ~/Downloads/Qwen2.5-Coder-7B-Q5_K_S.llamafile
```

---

## Session Quality

### Model provenance in Fountain sessions

The session recorder's title page already records a `Model:` field. The
theatrical framing — a model switch is a *new character entering the scene* —
guides the design for mid-session switches and the three downstream systems
that need updating.

**Title page additions:**

A new `Backend:` title-page field is written at session open (`llamafile` or
`ollama`).

**Mid-session model switches as scene entries:**

When the model changes (via `@mention` or `/llamafile use`), the original
title page is already written to disk. Instead, a Fountain note is inserted at
the switch point:

```
[[model switch: phi-mini (llamafile) at 2026-06-20 14:32:11]]
```

The `Recorder` gains a `RecordModelSwitch(newModel, backend string) error`
method that writes this note. Subsequent turns in the session file are
associated with the new model until the next switch note.

`RecordModelSwitch` is called from:
- `cmdLlamafileUse` after a successful switch
- the `@mention` handler in `terminal.go` after a successful switch

**Memory miner changes:**

The miner currently sends the full session text to a single extraction model.
With switch notes, the miner must attribute each assistant turn's content to
the model that generated it. Implementation: `Miner.Mine()` pre-processes the
`.spmd` file to split it at `[[model switch: ...]]` boundaries and injects a
"Model generating the following turns: NAME" line into the extraction prompt
for each segment. Extracted memories gain a `source_model` metadata field.

**Session replay changes:**

`replay.go` currently re-sends turns to whatever model is currently active.
With switch notes, replay must parse `[[model switch: ...]]` lines and call
`attemptModelSwitch` at each boundary, so the replayed session follows the
same model sequence as the original. The replay recording's title page
reflects the starting model; switch notes are reproduced in the output file.

**Plan command changes:**

A plan may benefit from different models for different steps — e.g. a large
model for architectural decisions, a small fast model for repetitive edits, a
specialized model for a domain-specific task. Two mechanisms support this:

1. **Step-level `[model: name]` annotation** in `agents/plan.md`:
   ```markdown
   - [ ] Step 3 [model: phi-mini]: compress the output to under 200 words
   ```
   The plan executor detects this annotation and calls `attemptModelSwitch`
   before running the step. On step completion it switches back to the plan's
   default model (the model active when `/plan` was invoked).

2. **`@mention` inside a plan step prompt**: if a step's text begins with
   `@name`, the `@mention` handler fires as normal. The switch persists for
   all subsequent steps unless the next step specifies its own `[model:]`.

The plan file format and executor are updated in Phase C of the implementation
plan.

---

## Documentation

### Getting-started restructure

`getting-started.md` and `harvey-getting-started.7.md` are rewritten so
Llamafile setup is Step 1:

1. Download a llamafile (link + size note)
2. Place it in `~/Models/`
3. Start Harvey — it finds the model automatically
4. First prompt

Ollama appears under "Advanced: Using Ollama" after the Llamafile section,
with a cross-reference to `harvey-ollama.7.md`.

`INSTALL.md` similarly leads with Llamafile and notes Ollama as optional.

### Documentation review scope

A documentation audit pass covers:

- All `.7.md` man pages: verify every flag, subcommand, and config field
  added in v0.0.12–v0.0.13 is documented.
- Cross-references: every command introduced since v0.0.11 appears in at
  least one SEE ALSO section and in the user manual index.
- `CONFIGURATION.md`: add entries for `LlamafileEntry.context_length` and
  any new fields added in v0.0.14.
- `harvey-llamafile.7.md`: add `download` subcommand; update SYNOPSIS and
  SEE ALSO.
- `harvey-model-alias.7.html`: verify it exists and covers `@mention`
  switching (currently this man page has no `.md` source — audit needed).

---

## Out of Scope

- **Tab completion for `/model`** — tab completion is a separate ongoing
  effort; `/model` will participate via the existing `ArgCompletion`
  mechanism but is not added in this cycle.
- **Automatic llamafile download** — Harvey prints the URL and recommended
  filenames but does not execute `wget`/`curl`. Network access in Harvey's
  startup path would complicate offline use.
- **Multiple simultaneous llamafile servers** — one llamafile process at a
  time is the current design. Parallel servers would require dynamic port
  allocation and process tracking beyond the single `a.llamafileProc` field.
- **Llamafile version detection** — Harvey does not parse the llamafile
  binary version; it relies on the `/v1/models` API being present (true since
  llamafile v0.9).
