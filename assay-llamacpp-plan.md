# Harvey Assay — llama.cpp Backend Support — Implementation Plan

See [assay-llamacpp-design.md](assay-llamacpp-design.md) for full design
rationale and decisions.

All work is in `cmd/assay/main.go`. No other files require changes unless an
export is missing from the harvey package (verified in W0).

---

## W0 — Verify harvey package exports needed by assay

**Before any code changes**, confirm the following symbols are exported from
the harvey package. If any are missing, export them in the appropriate file
(no logic change, just rename/export).

| Symbol | File | Needed for |
|---|---|---|
| `AnyLLMClient` / `NewAnyLLMClient`-style constructor | `anyllm_client.go` | Inference calls |
| `ChatStats` | `anyllm_client.go` | Token stats |
| `newLlamafileLLMClient` or equivalent OpenAI-path constructor | `anyllm_client.go` | Creating client for each backend |
| `FindFreePort` | `llamafile_service.go` | Already used by assay ✓ |
| `StartLlamafileService` | `llamafile_service.go` | Already used by assay ✓ |
| `LlamafileModelNameFromPath` | `llamafile.go` | Already used by assay ✓ |
| `ProbeLlamafile` | `llamafile_service.go` | Already used by assay ✓ |

**Key question:** What is the correct constructor to call to get a
`harvey.LLMClient` for an OpenAI-compatible URL + model? In the harvey package
this is done via `newLlamafileLLMClient(baseURL, modelName, timeout)` (in
`anyllm_client.go`) — verify it is exported or can be exported without
breaking callers.

Acceptance: `go build ./...` passes with no new export errors.

---

## W1 — Replace `callOllama` with `harvey.LLMClient`

**Goal:** Eliminate the private Ollama-specific HTTP call; use
`harvey.LLMClient.Chat` for all inference.

### Changes to `cmd/assay/main.go`

**Remove** the following private types and the `callOllama` function:

```go
type ollamaRequest  { ... }
type ollamaMessage  { ... }
type ollamaResponse { ... }
func callOllama(baseURL, model, prompt string) (string, ollamaResponse, error)
```

**Add** a helper that creates the right client given a base URL and model
name:

```go
func newAssayClient(baseURL, model string) harvey.LLMClient {
    return harvey.NewLlamafileLLMClient(baseURL+"/v1", model, 120*time.Second)
}
```

(Exact constructor name TBD from W0 — adjust once exports are confirmed.)

**Replace** every `callOllama(llmURL, model, prompt)` call site with:

```go
client := newAssayClient(llmURL, model)
var buf strings.Builder
stats, err := client.Chat(ctx, []harvey.Message{{Role: "user", Content: prompt}}, &buf)
response := buf.String()
```

**Update** `PromptResult` token fields from `harvey.ChatStats`:

```go
// Before (from ollamaResponse):
result.TokensPerSec    = or.EvalCount / (or.EvalDuration / 1e9)
result.PromptTokens    = or.PromptEvalCount
result.CompletionTokens = or.EvalCount

// After (from ChatStats):
result.TokensPerSec     = stats.TokensPerSec
result.PromptTokens     = stats.PromptTokens
result.CompletionTokens = stats.ReplyTokens
```

**Update** `listOllamaModels` to use the Ollama base URL unchanged (it calls
`/api/tags` directly via `http.Get`, which is fine to keep as-is for the
Ollama-specific model-list path).

**Tests:** Run the corpus against Ollama before and after; confirm pass/fail
counts are identical.

Acceptance: `go build ./...` passes; `go test ./...` passes; a real Ollama
run produces the same report structure as before.

---

## W2 — Add `--llamacpp URL` flag and backend selection

**Goal:** Connect to a running `llama-server` via the new flag.

### New flag

```go
llamacppURL := flag.String("llamacpp", "", "base URL of a running llama-server (e.g. http://localhost:8081)")
```

### Mutual exclusion guard

```go
if *llamafilePath != "" && *llamacppURL != "" {
    fmt.Fprintln(os.Stderr, "assay: --llamafile and --llamacpp are mutually exclusive")
    os.Exit(1)
}
```

### Backend selection block (replaces the existing llamafile-only block)

```go
llmURL  := *ollamaURL   // default: Ollama base URL (without /v1)
backend := "Ollama"

switch {
case *llamafilePath != "":
    // ... existing llamafile lifecycle (unchanged) ...
    backend = "Llamafile"

case *llamacppURL != "":
    // RAG guard.
    if *ragDB != "" && !harvey.ProbeLlamafile(*ollamaURL+"/api/tags") {
        fmt.Fprintf(os.Stderr,
            "assay: RAG evaluation with --llamacpp requires Ollama for embeddings.\n"+
            "Start Ollama or use --ollama to specify a running instance.\nOllama URL: %s\n",
            *ollamaURL)
        os.Exit(1)
    }
    llmURL  = *llamacppURL
    backend = "LlamaCpp"
}
```

### Model name resolution for llama.cpp

```go
case *llamacppURL != "":
    if *modelsFlag != "" {
        // Use --models directly.
        for _, m := range strings.Split(*modelsFlag, ",") {
            if m = strings.TrimSpace(m); m != "" {
                models = append(models, m)
            }
        }
    } else {
        // Query /v1/models — returns the loaded model's id.
        m, err := listOpenAIModels(*llamacppURL)
        if err != nil {
            fmt.Fprintf(os.Stderr, "assay: could not list llama.cpp models: %v\n"+
                "Use --models NAME to specify the model explicitly.\n", err)
            os.Exit(1)
        }
        models = m
    }
```

Add a small helper:

```go
// listOpenAIModels queries GET /v1/models and returns the model IDs.
func listOpenAIModels(baseURL string) ([]string, error) {
    resp, err := http.Get(baseURL + "/v1/models")
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    var out struct {
        Data []struct{ ID string `json:"id"` } `json:"data"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, err
    }
    ids := make([]string, 0, len(out.Data))
    for _, d := range out.Data {
        if d.ID != "" {
            ids = append(ids, d.ID)
        }
    }
    return ids, nil
}
```

### Report header

Extend `AssayResults` and `writeReport` to include the backend URL for
Ollama and llama.cpp:

```go
type AssayResults struct {
    ...
    Backend       string `json:"backend"`
    LlamafilePath string `json:"llamafile_path,omitempty"`
    BackendURL    string `json:"backend_url,omitempty"`   // new
    ...
}
```

In `writeReport`:

```go
fmt.Fprintf(&sb, "Backend: %s  \n", ar.Backend)
if ar.LlamafilePath != "" {
    fmt.Fprintf(&sb, "Binary: %s  \n", ar.LlamafilePath)
} else if ar.BackendURL != "" {
    fmt.Fprintf(&sb, "URL: %s  \n", ar.BackendURL)
}
```

### Update `--help` text

Add to the flag description block:

```
  --llamacpp URL    evaluate against a running llama-server at URL
                    (user is responsible for starting/stopping llama-server)
                    mutually exclusive with --llamafile
```

Acceptance: `go build ./...` and `go test ./...` pass. `bin/assay --llamacpp
http://localhost:8081 --models phi4-Q4_K_M` connects, runs the corpus, and
writes a report with "LlamaCpp" in the header.

---

## W3 — Update helptext and DECISIONS.md

**Goal:** Document the new flag in assay's `--help` output and record the
`callOllama` → `AnyLLMClient` architectural decision.

- Confirm `--help` output lists `--llamacpp` with a clear one-line description.
- Add entry to `DECISIONS.md` (done in parallel with this plan — see below).
- Mark TODO item as complete.

Acceptance: `bin/assay --help` shows `--llamacpp`; DECISIONS.md entry exists.

---

## Dependency graph

```
W0 (export check)
  └── W1 (replace callOllama)
        └── W2 (--llamacpp flag + backend selection)
              └── W3 (help text + decisions)
```

W0 must complete before W1. W1 must complete before W2 (the new flag uses the
new client). W3 can be done alongside W2.

---

## Risks

| Risk | Mitigation |
|---|---|
| `AnyLLMClient` constructor not exported | W0 catches this; trivial to export |
| Token stats differ from old Ollama path | Compare report output before/after W1 on a real Ollama run |
| `/v1/models` unavailable on older llama-server | `--models` flag as fallback; documented in help text |
| Streaming vs non-streaming differences | `AnyLLMClient.Chat` already handles streaming internally; assay reads the full buffer after return |
