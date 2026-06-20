# Harvey v0.0.14 — Llamafile-Primary Implementation Plan

See [llamafile-primary-design.md](llamafile-primary-design.md) for the full
design rationale. See [DECISIONS.md](DECISIONS.md) for key architectural
choices and rejected alternatives.

---

## Phase A — Connection Feedback & Health Check on `--resume`

**Goal:** Every backend connection attempt is narrated. `--resume` verifies
the model is reachable before loading session history.

### Files to modify

| File | Change |
|---|---|
| `terminal.go` | Add `connectWithFeedback(a *Agent, out io.Writer) error` helper; call it in `Run` after backend selection |
| `terminal.go` | Add health check before `ContinueFromFountain` when `a.Config.ResumeLatest` or `a.Config.ContinuePath != ""` |

### `connectWithFeedback`

```go
func connectWithFeedback(a *Agent, out io.Writer) error {
    model := activeModelLabel(a) // e.g. "qwen-coding (llamafile)"
    fmt.Fprintf(out, "Connecting to %s… ", model)
    if err := probeActiveBackend(a); err != nil {
        fmt.Fprintln(out, red("✗ failed"))
        fmt.Fprintln(out, "  "+err.Error())
        return err
    }
    fmt.Fprintln(out, green("✓ ready"))
    return nil
}
```

`probeActiveBackend` calls `ProbeLlamafile(a.Config.LlamafileURL)` when a
llamafile is active, or `ProbeOllama(a.Config.OllamaURL)` otherwise.

When the connection takes more than 2 seconds (e.g. llamafile startup),
replace the inline wait with a spinner: `fmt.Fprintln(out, "starting server")`
then spin until the server responds or `LlamafileStartupTimeout` expires.

### `--resume` health check

In the existing `--resume` / `--continue` pre-flight in `terminal.go`, before
calling `ContinueFromFountain`:

```go
if cfg.ResumeLatest || cfg.ContinuePath != "" {
    if !probeActiveBackendBool(a) {
        fmt.Fprintf(out, "Model %s is not running.\nStart it now? [Y/n]: ",
            a.Config.LlamafileActive)
        if userConfirms(a.In) {
            if err := startActiveModel(a, out); err != nil {
                fmt.Fprintln(out, yellow("  ⚠ Could not start model: "+err.Error()))
                fmt.Fprintln(out, "  Continuing without a backend — use /llamafile start or /ollama to connect.")
            }
        }
    }
}
```

### Acceptance criteria

- `go test ./...` passes.
- Starting Harvey with an active llamafile prints "Connecting to X (llamafile)… ✓ ready".
- Starting with `--resume` and a stopped llamafile offers to restart before loading history.
- `connectWithFeedback` on a non-reachable backend prints the error and returns it.

---

## Phase B — First-Run Onboarding & Stale Server Adoption

**Goal:** When no backend is reachable at startup, guide the user rather than
erroring. When a foreign llamafile server is found, offer to adopt it.

### Files to modify

| File | Change |
|---|---|
| `terminal.go` | Add `runFirstRunWizard(a *Agent, out io.Writer) error`; call it from `connectWithFeedback` when all backends fail |
| `llamafile.go` | Add `adoptExternalServer(a *Agent, out io.Writer) error`; call it from `cmdLlamafileAdd` and startup when a server is already running |
| `helptext.go` | Add `FirstRunWizardText` and `LlamafileDownloadText` constants |

### `runFirstRunWizard`

Prints `FirstRunWizardText` (Llamafile download instructions + Ollama
alternative). Then reads one line from `a.In`:

- Empty input → return without error; Harvey exits gracefully.
- A path → call `cmdLlamafileAdd(a, []string{path}, out)`, then retry
  `connectWithFeedback`.

```go
func runFirstRunWizard(a *Agent, out io.Writer) error {
    fmt.Fprint(out, FirstRunWizardText)
    fmt.Fprint(out, "Enter a llamafile path (or press Enter to exit): ")
    line, _ := bufio.NewReader(a.In).ReadString('\n')
    path := strings.TrimSpace(line)
    if path == "" {
        return fmt.Errorf("no backend available")
    }
    return cmdLlamafileAdd(a, []string{path}, out)
}
```

### `adoptExternalServer`

Called when `ProbeLlamafile(a.Config.LlamafileURL)` returns true but
`a.llamafileProc == nil`:

```go
func adoptExternalServer(a *Agent, out io.Writer) error {
    name, err := probeRunningLlamafileName(a.Config.LlamafileURL)
    if err != nil || name == "" {
        name = "external"
    }
    fmt.Fprintf(out, "  A llamafile server is already running at %s\n", a.Config.LlamafileURL)
    fmt.Fprintf(out, "  Detected model: %s\n", name)
    fmt.Fprint(out, "  Adopt as active model? [Y/n]: ")
    if !userConfirms(a.In) {
        return nil
    }
    a.Config.AddOrUpdateLlamafileEntry(LlamafileEntry{Name: name, Path: ""})
    a.Config.LlamafileActive = name
    return a.useLlamafileEntry(name, out)
}
```

`probeRunningLlamafileName` calls `GET /v1/models`, parses the JSON response,
and returns the first model ID found.

### Acceptance criteria

- Starting Harvey with no Ollama and no llamafile registered shows the
  first-run wizard text.
- Typing a valid llamafile path in the wizard completes the add flow.
- `/llamafile add` with a server already running offers adoption instead of
  the "stop it manually" warning.
- Adopted model appears in `/llamafile list`.

---

## Phase C — At-Mention Model Switch, Unified `/model`, & `/llamafile remove`

**Goal:** `@model` at the start of a prompt switches the active model while
preserving history. `/model` provides a backend-agnostic interface. `/llamafile
remove` is an alias for `drop`.

### Files to modify

| File | Change |
|---|---|
| `terminal.go` | Parse `@name` prefix in the REPL input handler before the normal chat/command dispatch |
| `llamafile.go` | Add `remove` case to `cmdLlamafile` switch (one line) |
| `commands.go` | Register `/model` command; add `cmdModel` dispatcher |
| `helptext.go` | Add `ModelHelpText` |
| `recorder.go` | Add `RecordModelSwitch(newModel, backend string) error` method |

### `@mention` parsing in `terminal.go`

At the top of the REPL input handler, before checking for `/` commands:

```go
if strings.HasPrefix(input, "@") {
    parts := strings.SplitN(input, " ", 2)
    name := strings.TrimPrefix(parts[0], "@")
    rest := ""
    if len(parts) > 1 {
        rest = parts[1]
    }
    switched, err := attemptModelSwitch(a, name, out)
    if err != nil {
        fmt.Fprintln(out, yellow("  ⚠ Model switch failed: "+err.Error()))
        continue
    }
    if switched && rest != "" {
        input = rest // forward remainder as the prompt
    } else if switched {
        continue // switch-only with no prompt
    }
    // name not found: fall through to normal chat
}
```

`attemptModelSwitch(a *Agent, name string, out io.Writer) (bool, error)`:

1. Check `a.Config.LlamafileModels` for a matching entry → call
   `cmdLlamafileUse(a, []string{name}, out)`.
2. Check Ollama models via `OllamaClient.ModelSummaries` → call the existing
   Ollama model switch path.
3. Return `(false, nil)` if the name is not found (no-op, fall through).

After a successful switch, call `a.Recorder.RecordModelSwitch(name, backend)`.

### `cmdModel`

```go
func cmdModel(a *Agent, args []string, out io.Writer) error {
    sub := ""
    if len(args) > 0 {
        sub = args[0]
    }
    switch sub {
    case "list":
        return cmdModelList(a, out)
    case "use":
        return cmdModelUse(a, args[1:], out)
    case "status":
        return cmdModelStatus(a, out)
    default:
        return cmdModelShow(a, out)
    }
}
```

`cmdModelList` merges `a.Config.LlamafileModels` and Ollama model summaries
into a single sorted table. `cmdModelUse` resolves the name by checking
`LlamafileModels` first, then Ollama, and delegates.

### `RecordModelSwitch`

```go
func (r *Recorder) RecordModelSwitch(newModel, backend string) error {
    ts := time.Now().Format("2006-01-02 15:04:05")
    note := fmt.Sprintf("model switch: %s (%s) at %s", newModel, backend, ts)
    _, err := fmt.Fprintln(r.f, "\n[["+note+"]]")
    return err
}
```

Also add a `Backend:` field to the title page in `NewRecorder`:

```go
{Type: fountain.TitlePageType, Name: "Backend", Content: backendLabel},
```

where `backendLabel` is `"llamafile"` or `"ollama"`.

### `/llamafile remove` alias

In `cmdLlamafile`, add one case before `default`:

```go
case "remove":
    return cmdLlamafileDrop(a, args[1:], out)
```

### Memory miner updates (`memory_miner.go`)

`Miner.Mine()` pre-processes the `.spmd` source before building the extraction
prompt. Add a helper `splitAtModelSwitches(spmd string) []modelSegment` where:

```go
type modelSegment struct {
    model   string // model name active for this segment
    backend string // "llamafile" or "ollama"
    text    string // the turns in this segment
}
```

The helper scans for `[[model switch: NAME (BACKEND) at TIMESTAMP]]` notes and
splits the session text at each boundary. The extraction prompt for each
segment prepends: `The following turns were generated by model NAME (BACKEND).`

Extracted memories gain a `source_model` metadata field in the Fountain output.

### Session replay updates (`replay.go`)

Add `parseModelSwitchNote(line string) (name, backend string, ok bool)` that
matches the `[[model switch: ...]]` pattern. In the replay loop, when a model-
switch note is encountered, call `attemptModelSwitch(a, name, out)` before
continuing to the next turn. The resulting replay recording emits switch notes
at the same positions.

### Plan executor updates (`plan.go`, `plan_cmd.go`)

Extend the step-line parser to detect `[model: NAME]` annotations:

```
- [ ] Step 3 [model: phi-mini]: compress the output to under 200 words
```

Before executing such a step, call `attemptModelSwitch(a, name, out)`. Track
the plan's *default model* (the model active when `/plan` was invoked) and
restore it after each annotated step unless the next step also has a
`[model:]` annotation.

`@mention` syntax in a step's text is already handled by the REPL input path
and does not require special plan handling.

### Acceptance criteria

- `@phi-mini tell me a joke` with phi-mini registered switches to phi-mini and
  forwards "tell me a joke".
- `@unknown-model hello` with no such model proceeds as a normal prompt.
- `/model list` shows all registered llamafile models and Ollama models in one table.
- `/model use phi-mini` switches to phi-mini.
- `/llamafile remove qwen-coding` removes qwen-coding (same behaviour as `drop`).
- Session recorder emits `[[model switch: ...]]` after a switch.
- Session recorder title page includes `Backend: llamafile` or `Backend: ollama`.
- Memory miner extracts memories with correct `source_model` attribution across switch boundaries.
- Session replay with a switch note changes the active model at the correct turn.
- Plan step with `[model: phi-mini]` annotation switches to phi-mini for that step and restores the default model after.

---

## Phase D — `/llamafile download`, Context Utilization, Routing Feedback

**Goal:** Print a model download guide. Show context utilization when the
context length is known. Show which route handled a turn in the spinner.

### Files to modify

| File | Change |
|---|---|
| `llamafile.go` | Add `download` case to `cmdLlamafile`; add `cmdLlamafileDownload` |
| `helptext.go` | Add `LlamafileDownloadText` constant (table from design doc) |
| `config.go` | Add `ContextLength int` field to `LlamafileEntry` |
| `config.go` | Add YAML marshal/unmarshal for the new field |
| `terminal.go` | Compute and display `[ctx: N%]` on the status/ready line after each turn |
| `terminal.go` | Call `spin.UpdateStatus(fmt.Sprintf("routed → %s", name))` in the routing dispatch path |
| `llamafile_service.go` | Add `ProbeLlamafileContextLength(url string) int` that queries `/v1/models` |

### Context utilization display

After each turn, if context length is known:

```go
func contextPct(promptTokens, contextLength int) string {
    if contextLength <= 0 || promptTokens <= 0 {
        return ""
    }
    pct := (promptTokens * 100) / contextLength
    return fmt.Sprintf("[ctx: %d%%]", pct)
}
```

The `[ctx: N%]` string is appended to the ready/status line Harvey prints
after each turn. Source of `promptTokens`: `stats.PromptTokens` from the last
`ChatStats` returned by `runChatTurn`. Source of `contextLength`: checked in
priority order (see design doc).

### `LlamafileEntry.ContextLength`

```go
type LlamafileEntry struct {
    Name          string `yaml:"name"`
    Path          string `yaml:"path"`
    ContextLength int    `yaml:"context_length,omitempty"`
}
```

After a successful llamafile start in `cmdLlamafileAdd` and `cmdLlamafileUse`,
call `ProbeLlamafileContextLength` and store the result in the entry if it was
not already set by the user. The result is stored in memory only (not written
back to `harvey.yaml`) to avoid config churn on every startup.

### `ProbeLlamafileContextLength` implementation note

Tested against Qwen3.5-2B, Qwen3.5-4B, and Apertus-8B: the field is
consistently at `data[0].meta.n_ctx` in the `/v1/models` JSON response, NOT
a field named `context_length`. Parse it as:

```go
func ProbeLlamafileContextLength(url string) int {
    // GET url + "/v1/models"
    // parse: response.data[0].meta.n_ctx
    // return 0 if absent or on any error
}
```

The `n_ctx` value is the *runtime* context window (how much llamafile loaded),
which may be smaller than `n_ctx_train` (the training context). Always use
`n_ctx` for the utilization indicator.

### Acceptance criteria

- `/llamafile download` prints the curated table and exits cleanly.
- `harvey.yaml` with `context_length: 8192` on a LlamafileEntry causes
  `[ctx: N%]` to appear after each turn.
- When no context length is known, the indicator is omitted (no `[ctx: 0%]`).
- A routing-dispatched turn shows `routed → <route-name>` in the spinner.

---

## Phase E — Auto-Reconnect on Dropped Llamafile

**Goal:** When the llamafile process dies mid-session, detect this on the
next turn and offer to restart rather than returning an opaque API error.

### Files to modify

| File | Change |
|---|---|
| `llamafile_service.go` | Add `(*LlamafileProcess) HasExited() bool` method |
| `terminal.go` | Wrap the `runChatTurn` call with a reconnect-check path |

### `HasExited`

```go
func (p *LlamafileProcess) HasExited() bool {
    if p == nil || p.cmd == nil || p.cmd.Process == nil {
        return false
    }
    return p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited()
}
```

### Reconnect wrapper in `terminal.go`

```go
reply, stats, err := runChatTurn(ctx, a, input, out, spin)
if err != nil && isConnectionError(err) && a.llamafileProc != nil && a.llamafileProc.HasExited() {
    fmt.Fprintln(out, yellow("  ⚠ The llamafile server stopped unexpectedly."))
    fmt.Fprintf(out, "  Restart %s? [Y/n]: ", a.Config.LlamafileActive)
    if userConfirms(a.In) {
        if restartErr := restartActiveLlamafile(a, out); restartErr == nil {
            reply, stats, err = runChatTurn(ctx, a, input, out, spin)
        }
    }
}
```

`isConnectionError(err)` checks whether the error string contains `"connection
refused"` or `"EOF"` or `"connect: no route to host"` — the standard net
package error strings for a dead server.

`restartActiveLlamafile` calls `StartLlamafileService` with the same
parameters as the original start and reassigns `a.llamafileProc`.

### Acceptance criteria

- Killing the llamafile process mid-session causes Harvey to offer a restart
  on the next prompt rather than printing a raw HTTP error.
- Declining the restart returns to the prompt without crashing.
- A successful restart allows the original prompt to be processed normally.

---

## Phase F — Session Quality (Model Provenance)

This phase is partially implemented in Phase C (title page `Backend:` field
and `RecordModelSwitch`). The remaining work:

### Files to modify

| File | Change |
|---|---|
| `terminal.go` | Pass `backendLabel` (not just model name) to `NewRecorder` |
| `recorder.go` | Accept `backend string` parameter in `NewRecorder` and write it to title page |

### `NewRecorder` signature change

```go
func NewRecorder(path, model, backend, workspace string) (*Recorder, error)
```

The `backend` parameter is written as a `Backend:` title-page key:

```go
{Type: fountain.TitlePageType, Name: "Backend", Content: backend},
```

Call sites in `terminal.go` pass `"llamafile"` or `"ollama"` as appropriate.

### Acceptance criteria

- `.spmd` files from llamafile sessions contain `Backend: llamafile` in the
  title block.
- `.spmd` files from Ollama sessions contain `Backend: ollama`.
- Model-switch events appear as `[[model switch: ...]]` notes at the correct
  position in the session file.

---

## Phase G — Documentation

**Goal:** Llamafile leads in all setup documentation. All new features have
man pages. All cross-references are current.

### Files to modify

| File | Change |
|---|---|
| `getting-started.md` | Rewrite to lead with Llamafile; move Ollama to "Advanced" section |
| `harvey-getting-started.7.md` | Mirror `getting-started.md` changes |
| `INSTALL.md` | Lead with Llamafile; note Ollama as optional |
| `harvey-llamafile.7.md` | Add `download` subcommand; add `remove` alias note |
| `helptext.go` | Add `ModelAliasHelpText` constant covering `/model alias` subcommands AND `@mention` switching |
| `harvey-model-alias.7.md` | Regenerated from `ModelAliasHelpText` (the `.html` exists but the `.md` source was lost; recreate via `cmt`) |
| `CONFIGURATION.md` | Add `LlamafileEntry.context_length` field documentation |
| `user_manual.md` | Add `/model` command and `@mention` to the command index |
| `reference.md` | Add `/model`, `@mention`, `/llamafile download`, `/llamafile remove` entries |
| Multiple `.7.md` files | SEE ALSO audit — add cross-refs for commands missing since v0.0.11 |

### `ModelAliasHelpText` content scope

The constant must cover:
- `@mention` syntax for inline model switching (new in v0.0.14)
- `/model alias set ALIAS FULLNAME` — define a short name
- `/model alias list` — list defined aliases
- `/model alias remove ALIAS` — remove an alias
- Interaction between aliases and `@mention` (aliases resolve at the `@` lookup step)
- SEE ALSO: `harvey-llamafile.7`, `harvey-ollama.7`, `harvey-routing.7`

This consolidates content previously split between the Ollama help block and
the now-sourceless HTML file.

### SEE ALSO audit checklist

Run `grep -L "harvey-model-alias\|harvey-llamafile\|harvey-routing" harvey-*.7.md` to
find man pages that reference model switching without pointing to the new
`harvey-model-alias.7.md`.

Commands introduced since v0.0.11 that need SEE ALSO coverage:
`/profile`, `--resume`, `/hint`, `/loop`, `/plan`, `/safe`, `/memory flag`,
`/llamafile download`, `/model`.

### Acceptance criteria

- `getting-started.md` step 1 is "download a llamafile".
- `harvey-llamafile.7.md` documents `download` and `remove`.
- `harvey-model-alias.7.md` exists and documents `@mention` syntax.
- `CONFIGURATION.md` documents `context_length` on LlamafileEntry.
- `user_manual.md` index includes `/model`.

---

## Dependency Graph

```
Phase A (connection feedback) ────────────────────────────────┐
Phase B (first-run wizard)  ──────────────────────────────────┤
Phase C (at-mention + /model + remove) ───────────────────────┤→ Phase F (session quality) → Phase G (docs)
Phase D (download + ctx% + routing)  ─────────────────────────┤
Phase E (auto-reconnect)   ───────────────────────────────────┘
```

Phases A–E are independent of each other and can be developed in parallel.
Phase F depends on Phase C (`RecordModelSwitch`, `NewRecorder` signature).
Phase G depends on all preceding phases being complete.

---

## Resolved Questions

- **`/v1/models` context length field** — Tested on Qwen3.5-2B, Qwen3.5-4B,
  and Apertus-8B. Field is `data[0].meta.n_ctx` (NOT `context_length`).
  Consistently present across all three model families. `n_ctx` is the runtime
  window; `n_ctx_train` is the training context and should not be used.

- **`@mention` session continuity** — Confirmed: continue in the existing
  session file. The switch is a new character entering the scene; `[[model
  switch: ...]]` notes mark the boundary. Memory miner, replay, and plan
  executor each need updates to track model attribution across boundaries
  (see Phase C).

- **`harvey-model-alias.7.md` source** — The `.md` source was lost; the
  `.html` exists. Resolution: create `ModelAliasHelpText` in `helptext.go`
  covering both `/model alias` subcommands and `@mention` switching, then
  regenerate the `.md` via `cmt` (Phase G).
