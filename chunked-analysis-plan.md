# Harvey Chunked Document Analysis — Implementation Plan

See [chunked-analysis-design.md](chunked-analysis-design.md) for the full
design rationale, Fountain scene model, and alternatives considered.
See [DECISIONS.md](DECISIONS.md) (2026-06-27 entries) for the three
architectural decisions this plan implements.

Target version: **v0.0.16**

Work items are ordered W0 → W5. W0 is documentation-only. W1 is the
standalone accounting bug fix and can ship before the rest. W2 and W3
are independent of each other and can be done in parallel. W4 depends
on W2. W5 wires everything together and requires W1–W4.

**Key invariant:** `chunking.enabled: false` in `harvey.yaml` must
restore exact pre-feature behavior — the `read_file` tool reads files
unconditionally, no size check, no alert.

---

## W0 — Update `FOUNTAIN_FORMAT.md` to v1.3

**Goal:** Document the new `INT. CHUNK ANALYSIS` scene type and the three
new note types before any code changes. This gives the recorder
implementation (W3) a clear spec to target. No Go files change.

### Changes to `FOUNTAIN_FORMAT.md`

| Section | Change |
|---------|--------|
| Scene Types — reference table | Add `INT. CHUNK ANALYSIS — FILENAME TIMESTAMP` row |
| Special Syntax — note types | Add `[[chunk:]]`, `[[chunk-result:]]`, `[[synthesis:]]` entries |
| Scenarios | Add Scenario N: chunked document analysis, showing the full scene sequence |
| Changelog | Add v1.3 entry |

### New note syntax

```
[[chunk: file=NAME, chunks=N, model=MODEL, boundary=TYPE, chunk-size=N]]
[[chunk-result: i/N — ok|error: <reason>]]
[[synthesis: model=MODEL — ok|error: <reason>]]
```

The `[[chunk:]]` note is the first element inside the scene, before the
RSDOIEL dialogue. `[[chunk-result:]]` notes follow in order. The
`[[synthesis:]]` note is last, before HARVEY's closing line.

### Acceptance criteria

- `FOUNTAIN_FORMAT.md` version is 1.3 in the Changelog.
- `INT. CHUNK ANALYSIS` appears in the Scene Types reference table.
- All three new note types appear in the Special Syntax section with
  example values.

---

## W1 — Context estimator and pre-read guard (accounting bug fix)

**Goal:** Fix the bug where Harvey compares file size against the raw
model context window rather than the context that remains after current
history, system prompt, and injected memories are accounted for. This
work item is the prerequisite for the alert trigger (W5) and ships
independently as a bug fix.

### New file: `context_estimator.go`

```go
// estimateTokens returns a conservative token count for s using the
// bytes/4 heuristic (well-established for English prose; source code
// will be slightly over-estimated, which is the safe direction).
func estimateTokens(s string) int

// remainingContext returns the estimated number of tokens available for
// new content, given the agent's current state. It subtracts serialized
// history, the system prompt, and a safety margin (10% of the window)
// from the model's declared context window.
func remainingContext(a *Agent) int

// fileExceedsBudget returns true if the file at path would, by byte
// estimate alone, exceed the given token budget. Uses os.Stat; does
// not read the file.
func fileExceedsBudget(path string, budget int) (bool, int64, error)
```

`remainingContext` derives the model context window from
`a.ModelCaps.ContextWindow` (already available via `model_cache.go`).
It serializes history by joining all message Content fields and applying
`estimateTokens`. Safety margin = `a.ModelCaps.ContextWindow / 10`.

`fileExceedsBudget` calls `os.Stat(path).Size()` and compares
`int(size/4)` against `budget`. It returns the raw byte size for use
in the alert message.

### Files to modify

| File | Change |
|------|--------|
| `context_estimator.go` | New file (three functions above) |
| `context_estimator_test.go` | New file (tests below) |

No changes to `terminal.go` in this work item — the pre-read guard hook
is added in W5 when the alert UX is ready.

### Tests

- `TestEstimateTokens_Empty` — empty string returns 0
- `TestEstimateTokens_KnownLength` — 400-byte string returns ~100
- `TestFileExceedsBudget_SmallFile` — file smaller than budget returns false
- `TestFileExceedsBudget_LargeFile` — file larger than budget returns true
- `TestFileExceedsBudget_Missing` — non-existent path returns error
- `TestRemainingContext_SubtractsHistory` — agent with long history has
  smaller remaining context than agent with empty history

### Acceptance criteria

- `go test ./...` passes.
- `remainingContext` returns a value strictly less than
  `ModelCaps.ContextWindow` when history is non-empty.
- `fileExceedsBudget` never reads file content; it calls only `os.Stat`.

---

## W2 — Document chunker

**Goal:** Implement structure-aware document splitting into paragraph-
or block-bounded chunks with one-unit overlap. This is a pure data
transformation with no LLM calls or I/O side effects.

### New file: `chunker.go`

```go
// DocType classifies a file for chunking strategy selection.
type DocType int

const (
    DocTypeProse  DocType = iota // .md, .txt, .rst, .tex, .html
    DocTypeSource                // .go, .ts, .py, .js, .c, .h, ...
)

// ChunkConfig holds tuneable chunking parameters, mirroring the
// harvey.yaml chunking: stanza.
type ChunkConfig struct {
    Enabled        bool
    Threshold      float64 // fraction of remaining context; default 0.80
    ChunkSizeBytes int     // target chunk size in bytes; default 6000
    MaxChunks      int     // warn threshold; default 20
    Overlap        string  // "paragraph", "sentence", or "none"
}

// DefaultChunkConfig returns the default ChunkConfig used when no
// chunking: stanza is present in harvey.yaml.
func DefaultChunkConfig() ChunkConfig

// DetectDocType returns the DocType for the given file path based on
// its extension. Unknown extensions return DocTypeProse.
func DetectDocType(path string) DocType

// ChunkDocument splits content into chunks according to cfg and
// docType. Returns at least one chunk. Chunks include overlap from
// the preceding chunk as specified by cfg.Overlap.
func ChunkDocument(content string, cfg ChunkConfig, docType DocType) []string
```

### `ChunkDocument` algorithm

**Prose path** (`DocTypeProse`): Split `content` on `\n\n`. Walk
paragraphs, accumulating a current chunk until `len(chunk) >= cfg.ChunkSizeBytes * 0.75`.
When the threshold is reached, save the chunk and start the next with
the last paragraph as overlap (when `cfg.Overlap == "paragraph"`).

**Source path** (`DocTypeSource`): Split on the pattern
`\n\n` followed by a non-whitespace character that is not `}` or `)` —
a heuristic for blank-line-then-signature. Same accumulation and overlap
logic as prose.

**Overlap `"none"`**: Next chunk starts immediately after the last
paragraph/block of the previous chunk, with no repeated content.

**Minimum chunk count**: Always at least 1 (the full document is one
chunk if it fits within `ChunkSizeBytes`).

### New file: `chunker_test.go`

- `TestDetectDocType` — table-driven: `.md`→Prose, `.go`→Source,
  `.unknown`→Prose
- `TestChunkDocument_SingleChunk` — content smaller than ChunkSizeBytes
  returns one chunk equal to content
- `TestChunkDocument_ParagraphSplit` — multi-paragraph prose splits at
  correct boundaries
- `TestChunkDocument_SourceSplit` — Go source with multiple functions
  splits at function boundaries
- `TestChunkDocument_Overlap` — second chunk begins with last paragraph
  of first chunk when `Overlap == "paragraph"`
- `TestChunkDocument_NoOverlap` — `Overlap == "none"` produces no
  repeated content between chunks
- `TestChunkDocument_MaxChunks` — document producing > MaxChunks chunks
  still returns all chunks (the guard warning is the caller's concern)

### Acceptance criteria

- `go test ./...` passes including new chunker tests.
- `ChunkDocument` never returns an empty slice.
- Overlap content from chunk N appears as the first bytes of chunk N+1
  when `cfg.Overlap == "paragraph"`.
- Source and prose splitting produce different chunk boundaries on a
  mixed-content file.

---

## W3 — Recorder: `INT. CHUNK ANALYSIS` scene

**Goal:** Add three new methods to `Recorder` that write the chunk
analysis scene and its notes. These are called by the map-reduce engine
(W4) and the wiring layer (W5); the recorder itself has no LLM
dependency.

### Files to modify

| File | Change |
|------|--------|
| `recorder.go` | Add `RecordChunkAnalysisStart`, `RecordChunkResult`, `RecordChunkSynthesis` methods (signatures below) |
| `recorder_test.go` | Add tests for new methods |
| `FOUNTAIN_FORMAT.md` | (Already updated in W0) |

### New methods on `Recorder`

```go
// RecordChunkAnalysisStart opens an INT. CHUNK ANALYSIS scene and
// writes the [[chunk:]] header note. Call once before the map phase.
//
// Parameters:
//   filename (string) — base name of the file being analyzed
//   totalChunks (int) — total number of chunks
//   model (string) — model name used for chunk processing
//   boundary (string) — "paragraph" or "source"
//   chunkSizeBytes (int) — target chunk size
//   userInstruction (string) — the chunk prompt the user entered
//
// Returns:
//   error — write error, or nil
//
// Example:
//   r.RecordChunkAnalysisStart("doc.md", 12, "LLAMA3", "paragraph", 6000,
//       "Summarize each section.")
func (r *Recorder) RecordChunkAnalysisStart(filename string,
    totalChunks int, model, boundary string,
    chunkSizeBytes int, userInstruction string) error

// RecordChunkResult writes a [[chunk-result:]] note for chunk i of n.
// status is "ok" or "error: <reason>".
//
// Parameters:
//   i (int) — 1-based chunk index
//   n (int) — total chunks
//   status (string) — "ok" or "error: <first line of error>"
//
// Returns:
//   error — write error, or nil
//
// Example:
//   r.RecordChunkResult(3, 12, "ok")
func (r *Recorder) RecordChunkResult(i, n int, status string) error

// RecordChunkSynthesis writes the [[synthesis:]] note and HARVEY's
// closing line, then closes the INT. CHUNK ANALYSIS scene.
//
// Parameters:
//   model (string) — model name used for synthesis
//   status (string) — "ok" or "error: <reason>"
//
// Returns:
//   error — write error, or nil
//
// Example:
//   r.RecordChunkSynthesis("LLAMA3", "ok")
func (r *Recorder) RecordChunkSynthesis(model, status string) error
```

### Expected scene output (from `RecordChunkAnalysisStart` + two
`RecordChunkResult` calls + `RecordChunkSynthesis`)

```
INT. CHUNK ANALYSIS — doc.md 2026-06-27 10:04:08

[[chunk: file=doc.md, chunks=2, model=LLAMA3, boundary=paragraph, chunk-size=6000]]

RSDOIEL
Summarize each section.

[[chunk-result: 1/2 — ok]]
[[chunk-result: 2/2 — ok]]
[[synthesis: model=LLAMA3 — ok]]

HARVEY
Chunk analysis complete. Synthesized result injected into conversation.
```

### Tests to add in `recorder_test.go`

- `TestRecordChunkAnalysisStart` — verify scene heading contains
  filename and timestamp; verify `[[chunk:]]` note contains all
  parameters; verify RSDOIEL dialogue contains userInstruction
- `TestRecordChunkResult_Ok` — verify `[[chunk-result: 3/12 — ok]]`
- `TestRecordChunkResult_Error` — verify `[[chunk-result: 2/12 — error: context exceeded]]`
- `TestRecordChunkSynthesis_Ok` — verify `[[synthesis: model=LLAMA3 — ok]]`
  and HARVEY closing line
- `TestRecordChunkSynthesis_Error` — error status recorded in note;
  HARVEY closing line still written

### Acceptance criteria

- `go test ./...` passes including new recorder tests.
- A call sequence of `RecordChunkAnalysisStart` → N × `RecordChunkResult`
  → `RecordChunkSynthesis` produces a well-formed Fountain scene matching
  the format in W0.
- `RecordChunkResult` with `i > n` does not panic; it writes the note
  with the given values.

---

## W4 — Map-reduce engine

**Goal:** Implement `RunChunkedAnalysis`, the function that drives the
map and reduce phases, manages LLM calls per chunk, aggregates results,
runs synthesis, and returns the final answer. This function calls no I/O
other than the LLM client and the recorder; it does not touch
`terminal.go` internals.

### New file: `chunk_analyzer.go`

```go
// ChunkAnalysisParams bundles the inputs to RunChunkedAnalysis.
type ChunkAnalysisParams struct {
    Filename    string
    Chunks      []string // from ChunkDocument (W2)
    Instruction string   // the user's chunk prompt (may contain @mention)
    Model       string   // resolved model name (@mention already parsed)
    Boundary    string   // "paragraph" or "source" (for recording)
    Config      ChunkConfig
}

// RunChunkedAnalysis runs the map-reduce workflow described in the
// chunked-analysis design. It calls the LLM once per chunk (map phase)
// and once for synthesis (reduce phase), recording progress to the
// Recorder after each call. Progress is also printed to w.
//
// Parameters:
//   ctx (context.Context) — cancellation context
//   client (LLMClient) — the LLM client to use for map and reduce calls
//   rec (*Recorder) — session recorder; may be nil (recording skipped)
//   params (ChunkAnalysisParams) — all inputs bundled
//   w (io.Writer) — progress output (usually os.Stdout)
//
// Returns:
//   string — the synthesized final answer
//   error  — first unrecoverable error, or nil
//
// Example:
//   result, err := RunChunkedAnalysis(ctx, client, rec, params, os.Stdout)
func RunChunkedAnalysis(ctx context.Context, client LLMClient,
    rec *Recorder, params ChunkAnalysisParams, w io.Writer) (string, error)
```

### Map phase detail

For chunk `i` (1-based) of `n`:

```
prompt = params.Instruction + "\n\n---\nChunk " + i + " of " + n + ":\n" + chunk
```

Send to `client.Chat` with the model from `params.Model`. On success,
append result to `partialResults`. On error, append `"[chunk N failed:
<err>]"` to `partialResults` and record `error: <err>` in the recorder.
Print `Processing chunk i/n…` to `w` before each call.

### Reduce phase detail

Construct synthesis prompt:

```
params.Instruction + "\n\n---\nCombine the following " + n +
" section summaries into a single coherent response:\n\n" +
join(partialResults, "\n\n---\n")
```

Send to `client.Chat`. On success, record synthesis ok and return result.
On error, return an error wrapping the synthesis failure.

### New file: `chunk_analyzer_test.go`

Uses `mockLLMClient` from `tier3_test.go` (the canonical test double).

- `TestRunChunkedAnalysis_TwoChunks` — two chunks, mock returns
  "result1" and "result2"; synthesis mock returns "final"; verify return
  value is "final" and no error
- `TestRunChunkedAnalysis_ChunkError` — mock fails on chunk 2 of 3;
  verify function continues, partial result contains failure note, synthesis
  still runs over remaining results
- `TestRunChunkedAnalysis_SynthesisError` — all chunks succeed, synthesis
  mock returns error; verify error propagated
- `TestRunChunkedAnalysis_NilRecorder` — rec=nil; verify no panic,
  function still returns synthesis result
- `TestRunChunkedAnalysis_Progress` — capture w output; verify
  "Processing chunk 1/2…" and "Processing chunk 2/2…" appear

### Acceptance criteria

- `go test ./...` passes including new chunk analyzer tests.
- A chunk failure does not stop the map phase; synthesis receives all
  available partial results including the failure note.
- `RunChunkedAnalysis` with a nil recorder does not panic.
- Progress lines appear on `w` in chunk order before synthesis begins.

---

## W5 — Alert UX, harvey.yaml stanza, and wiring

**Goal:** Wire W1–W4 into `terminal.go` and the config loader. After
this work item, the full feature is live: a file read that would overflow
context triggers the alert, the user provides a chunk instruction, the
map-reduce engine runs, and the synthesized result is injected into the
main conversation.

### Files to modify

| File | Change |
|------|--------|
| `terminal.go` | Pre-read guard in `read_file` handler; `promptChunkInstruction` function; wiring to `RunChunkedAnalysis`; inject synthesis result into history |
| `harvey.go` | Add `Chunking ChunkConfig` field to `HarveyConfig`; populate defaults in `LoadHarveyYAML` |
| `CONFIGURATION.md` | Document the `chunking:` stanza |

### `promptChunkInstruction` in `terminal.go`

```go
// promptChunkInstruction displays the overflow alert and reads the
// user's chunk instruction from stdin. Returns the instruction and
// false, or ("", true) if the user typed "no".
func promptChunkInstruction(filename string,
    estimatedTokens, remaining int,
    lastUserMessage string) (instruction string, cancelled bool)
```

The alert format:

```
Context overflow: <filename> is approximately <N> tokens;
<M> tokens remain in current context.
Enter instructions to process each chunk in turn, or "no" to return.
[<lastUserMessage>]
```

The pre-filled suggestion is printed inside `[…]` brackets. The user's
input replaces it if they type anything; pressing enter with no input
accepts the suggestion.

### Pre-read guard in `read_file` handler

```
1. Call fileExceedsBudget(path, int(float64(remaining)*cfg.Threshold))
2. If not exceeded, proceed with existing read_file logic (no change).
3. If exceeded and cfg.Enabled == false, proceed anyway (compatibility).
4. If exceeded and cfg.Enabled == true:
   a. Call promptChunkInstruction → (instruction, cancelled)
   b. If cancelled, return a tool result: "File read cancelled by user."
   c. Detect doc type, call ChunkDocument
   d. If len(chunks) > cfg.MaxChunks, warn the user and ask to confirm
   e. Parse @mention from instruction → resolve model name
   f. Call RunChunkedAnalysis → (synthesis, err)
   g. If err, return tool result with error message
   h. Return synthesis as the tool result
   i. Inject HARVEY's overflow alert dialogue into the open Fountain
      scene via recorder before starting the CHUNK ANALYSIS scene
```

### `@mention` parsing from chunk instruction

The existing `parseMention(s string) (model, rest string)` pattern in
`terminal.go` handles `@model` extraction. Apply the same logic to
`instruction`: if a `@mention` is found, `model = resolved endpoint`;
otherwise `model = a.currentModel`.

### harvey.yaml loading

Add to `HarveyConfig` in `harvey.go`:

```go
Chunking ChunkConfig `yaml:"chunking"`
```

In `LoadHarveyYAML`, after unmarshalling, apply defaults for any zero
fields in `Chunking`:

```go
if cfg.Chunking.ChunkSizeBytes == 0 {
    cfg.Chunking = DefaultChunkConfig()
} else {
    if cfg.Chunking.Threshold == 0 {
        cfg.Chunking.Threshold = 0.80
    }
    if cfg.Chunking.MaxChunks == 0 {
        cfg.Chunking.MaxChunks = 20
    }
    if cfg.Chunking.Overlap == "" {
        cfg.Chunking.Overlap = "paragraph"
    }
}
```

### CONFIGURATION.md addition

Document the `chunking:` stanza with the four fields, their types,
defaults, and valid values for `overlap`.

### Tests to add in `terminal_test.go`

- `TestReadFile_UnderBudget` — file smaller than threshold proceeds
  normally, no alert fired
- `TestReadFile_OverBudget_Cancelled` — file over threshold, user input
  "no"; verify return value is "File read cancelled by user."
- `TestReadFile_OverBudget_ChunkingDisabled` — `cfg.Enabled = false`,
  file over threshold; verify file is read normally, no alert

Full integration of the chunked analysis path (map-reduce with real
chunks) is validated by the smoke test below rather than unit tests,
because it requires a live LLM client.

### Acceptance criteria

- `go test ./...` and `go test -race` pass.
- With `chunking.enabled: false`, reading a large file behaves exactly
  as it did before this feature.
- With `chunking.enabled: true` and a file that exceeds the threshold,
  the alert message appears in the terminal, the pre-fill is shown, and
  typing "no" returns without reading the file.
- After a completed chunk analysis, the next `INT. HARVEY AND … TALKING`
  scene contains the synthesized result as the model's reply.
- The `INT. CHUNK ANALYSIS` scene appears in the `.spmd` session file
  between the overflow turn and the synthesis turn.

---

## Fountain format changelog entry

Add to `FOUNTAIN_FORMAT.md` Changelog table in W0:

```
| 1.3 | 2026-06-27 | INT. CHUNK ANALYSIS scene; [[chunk:]], [[chunk-result:]], [[synthesis:]] notes |
```

---

## Full test run (after all work items)

```bash
cd harvey
go test ./...
go test -race
go build -o bin/harvey cmd/harvey/*.go
```

### Manual smoke test

Start Harvey with a model whose context window is small enough to
trigger overflow on a known file (or temporarily lower `chunking.threshold`
to 0.01 to force triggering on any read). Run:

```
> read document.md and extract all section headings
```

Confirm:
- Alert message appears with file name, estimated tokens, remaining tokens
- Pre-filled suggestion matches the original user message
- Editing the suggestion and pressing enter runs the chunk analysis
- Progress lines `Processing chunk i/N…` appear for each chunk
- A synthesized result appears as the model's reply
- The `.spmd` session file contains an `INT. CHUNK ANALYSIS` scene with
  `[[chunk:]]`, N × `[[chunk-result:]]`, and `[[synthesis:]]` notes
- The `INT. CHUNK ANALYSIS` scene falls between the overflow turn and
  the turn containing the synthesized result
- Typing "no" at the alert returns to the conversation without reading
  the file
