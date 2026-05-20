# Harvey Pipeline — Implementation Plan

See [pipeline-design.md](pipeline-design.md) for the full design rationale.

## New Module Dependencies

None. The pipeline is implemented entirely with existing Harvey packages and
the Go standard library.

## Files to Create

| File | Purpose |
|------|---------|
| `harvey/pipeline.go` | All pipeline logic: arg parsing, file reading, @mention scan, model resolution, confidence extraction, step execution, orchestrator |
| `harvey/pipeline_test.go` | Unit and integration tests |

## Files to Modify

| File | Change |
|------|--------|
| `harvey/commands.go` | Register `/pipeline` in the command dispatch table |
| `harvey/terminal.go` | Extend `buildCompleter`: add `/pipeline` with path positions starting at token index 2 |

## Implementation Phases

### Phase 1 — Argument Parsing

```go
func parsePipelineArgs(root string, args []string) (threshold float64, files []string, err error)
```

- Validate `args[0]` matches `\d+(\.\d+)?%`; parse to float64 in (0, 1]
- Remaining tokens are workspace-relative file paths
- Each path is validated with `resolveWorkspacePath` (path escape blocked)
- Return error on missing files or zero paths

### Phase 2 — File Reading and @mention Extraction

```go
func readPipelineFile(root, relPath string) (body string, err error)
func scanAtMention(body string) string  // returns "" if none found
```

- `readPipelineFile` resolves and reads; enforces workspace boundary
- `scanAtMention` applies regex `@([\w:.-]+)` and returns the first capture group

### Phase 3 — Model Resolution

```go
func resolvePipelineClient(a *Agent, mention string) (LLMClient, error)
```

- Empty mention → return `a.Client`, nil (no allocation)
- Non-empty → construct temporary `AnyLLMClient` using same provider as
  `a.Client` but with model name = mention
- If construction fails or model unavailable → return nil, descriptive error

### Phase 4 — Confidence Extraction

```go
func extractConfidence(ctx context.Context, client LLMClient, response string) (score float64, stripped string, method string, err error)
```

1. Try `{"confidence": X.X ...}` JSON at end of response; strip block
2. If fail: send follow-up `"Rate your confidence 0.0–1.0. Reply only: CONFIDENCE: <score>"`; parse reply
3. If fail: keyword scan (hedging phrases → 0.30; no hedging → 0.80)

Returns the score, the response with the confidence block removed, the method
used ("json", "followup", "keyword"), and any error from the follow-up call.

### Phase 5 — Step Execution

```go
func runPipelineStep(
    ctx context.Context,
    a *Agent,
    client LLMClient,
    messages []Message,
    out io.Writer,
    stepNum, total int,
    filename string,
    threshold float64,
) (strippedResponse string, confidence float64, err error)
```

1. Append hidden confidence instruction to the last `User` message
2. Update spinner: `[N/total] filename | context X%`
3. `client.Chat(ctx, messages, out)` — streams to terminal
4. `extractConfidence` on result
5. Print per-step result line with ✓ or ✗
6. If score < threshold → return error
7. Return stripped response for next step

### Phase 6 — Pipeline Orchestrator

```go
func cmdPipeline(a *Agent, args []string, out io.Writer) error
```

1. `parsePipelineArgs` — validate threshold and files
2. For each file: `readPipelineFile` → `scanAtMention` → `resolvePipelineClient`
   - Stop on any resolution error before running any steps
3. Fountain: open pipeline scene block
4. Step 1: messages = `a.History` + user message from file
5. Step N (N > 1): messages = system prompt + user message (prev response + file body)
6. `runPipelineStep` for each step; stop on error
7. On success: `a.AddMessage("assistant", finalStrippedResponse)`
8. Fountain: `CUT BACK TO:` original scene

### Phase 7 — Tab Completion

In `buildCompleter` switch statement, add:

```go
case "/pipeline":
    pathStart = 2  // tokens[1] is confidence %, paths start at index 2
```

### Phase 8 — Tests (`pipeline_test.go`)

| Test | Covers |
|------|--------|
| `TestParsePipelineArgs_valid` | `90%`, `85.5%`, single and multiple files |
| `TestParsePipelineArgs_invalid` | Missing %, non-numeric, zero files, path escape |
| `TestScanAtMention_first` | First @mention wins, ignores later ones |
| `TestScanAtMention_none` | Returns empty string |
| `TestExtractConfidence_json` | Well-formed JSON block at end of response |
| `TestExtractConfidence_followup` | JSON absent, follow-up succeeds |
| `TestExtractConfidence_keyword` | Both JSON and follow-up fail, keyword scan used |
| `TestCmdPipeline_singleStep` | One file, confidence met, History updated |
| `TestCmdPipeline_multiStep` | Three files, all pass, final response appended |
| `TestCmdPipeline_failAtStep2` | Confidence fails at step 2, History unchanged |
| `TestCmdPipeline_mentionUnresolved` | @mention not found, stops before step 1 |
| `TestCmdPipeline_fileNotFound` | Missing file, stops with error |

## Acceptance Criteria

- [ ] `/pipeline 90% step1.md step2.md` runs end-to-end with mocked LLM client
- [ ] Confidence below threshold stops at the correct step
- [ ] Unresolved @mention stops immediately before any steps execute
- [ ] `a.Client` and `a.History` unchanged on any failure
- [ ] Final response appended to History on success
- [ ] Tab completion offers file paths for argument positions 2 and beyond
- [ ] Spinner shows filename and context percentage during each step
- [ ] Per-step result line printed after each step completes
- [ ] `go test ./...` passes
