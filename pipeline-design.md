# Harvey Pipeline — Design

## Overview

The `/pipeline` command chains a sequence of Markdown prompt files, passing
each step's response as input to the next. A configurable confidence threshold
gates progression: if a step's measured confidence falls below the threshold
the pipeline stops immediately with a diagnostic error. The pipeline is a
first-class session construct — its execution appears as a distinct scene block
in the Fountain session recording, then returns to the original scene.

## Command Syntax

```
/pipeline <CONFIDENCE%> FILE [FILE ...]
```

| Argument | Format | Example |
|----------|--------|---------|
| `CONFIDENCE%` | Required, first arg, integer or decimal | `90%` |
| `FILE` | Workspace-relative path, one or more | `prompts/step1.md` |

Examples:

```
/pipeline 85% review.md summarise.md
/pipeline 90% setup.md step1.md step2.md finalise.md
```

Tab completion applies to FILE arguments via the existing
`workspacePathCandidates` helper; `/pipeline` is added to `buildCompleter`
with path positions starting at token index 2 (index 1 is the confidence %).

## Pipeline File Format

Each FILE is a plain Markdown document. Its body is sent verbatim to the
model as the user message, with one detected annotation and one hidden
addition described below.

### @mention — Model Routing

Harvey scans the file body for the **first** occurrence of `@[\w:.-]+`. That
token determines the model for this step. All subsequent @mentions are left in
the body and become part of the prompt text passed to the model unchanged.

No frontmatter is required; @mention may appear anywhere in the file — first
line, mid-paragraph, or at the end. This keeps the format accessible to users
who are not familiar with YAML frontmatter.

If no @mention is found the step uses Harvey's currently active `a.Client`.

If an @mention is found but the model cannot be resolved the pipeline stops
immediately (before executing any further steps) with an error; `a.History`
and `a.Client` are unchanged.

### Hidden Confidence Instruction

Harvey appends the following block to every outgoing user message **before**
sending it to the model. It is never shown to the user in the pipeline file
display, and it is stripped from the response before the response is displayed
or forwarded to the next step.

```
---
After your response, append the following JSON block on its own line.
Do not include any text after the block.
{"confidence": 0.0-1.0, "reason": "one-line rationale"}
```

The appended instruction is logged in the Fountain session recording as a
parenthetical note for auditability: `(hidden confidence instruction appended)`.

## Confidence Extraction — Fallback Chain

After each step Harvey attempts to extract a confidence score using the
following priority order:

| Priority | Method | Trigger |
|----------|--------|---------|
| 1 | Parse `{"confidence": X.X, ...}` JSON block from end of response | Always tried first |
| 2 | Follow-up message: "Rate your confidence 0.0–1.0. Reply only: `CONFIDENCE: <score>`" | JSON not found or unparseable |
| 3 | Keyword scan: hedging phrases → 0.30; no hedging → 0.80 | Follow-up also fails |

**Hedging phrases (keyword scan):** "I'm not sure", "I cannot determine",
"I don't know", "unclear", "uncertain", "it's possible", "might be",
"I'm unsure".

The confidence block and any follow-up exchange are stripped from the response
before it is displayed or forwarded. Only the clean response body is shown and
passed to the next step.

## Context Flow

### Step 1

Step 1 carries Harvey's full current conversation history so the first model
has session context:

```
Messages:  a.History
         + User{ <step1.md body> + hidden confidence instruction }
Client:    model resolved from step1.md @mention, or a.Client
```

### Step N (N > 1)

Each subsequent step starts a fresh conversation (system prompt only) and
receives only the previous step's stripped response. This keeps context
usage minimal across the chain — each step distils the previous output into
a refined prompt for the next.

```
Messages:  SystemMsg{ a.Config.SystemPrompt }
         + User{ <step N-1 stripped response>
                 \n\n---\n\n
                 <step N markdown body>
                 + hidden confidence instruction }
Client:    model resolved from step N's @mention, or a.Client
```

## Model Resolution

When a step's @mention differs from Harvey's active model, Harvey constructs
a temporary `AnyLLMClient` struct for that step. This struct is a lightweight
HTTP wrapper; no OS subprocess is spawned. The temporary client is discarded
after the step completes.

`a.Client` is never mutated. Harvey returns to its original active model after
the pipeline exits, regardless of outcome.

## User-Visible UX

### Spinner

During each step's LLM call the spinner shows:

```
[1/3] setup.md | context 12%
```

Context percentage: `(tokens_used / effective_context_limit) * 100`.

### Per-Step Result Line

After each step completes, Harvey prints:

```
Step 1/3 [setup.md] — confidence: 0.91 ✓
Step 2/3 [step1.md] — confidence: 0.72 ✗ (threshold: 0.90)
```

Response text streams live to the terminal as tokens arrive, so the user can
interrupt with Ctrl-C if a step is producing unhelpful output.

## Session State on Exit

| Condition | `a.History` | `a.Client` |
|-----------|-------------|------------|
| All steps pass | Final stripped response appended as assistant turn | Unchanged |
| Confidence threshold not met | Unchanged | Unchanged |
| @mention not resolved | Unchanged | Unchanged |
| File not found | Unchanged | Unchanged |
| Context/OOM error | Unchanged | Unchanged |

## Error Conditions

| Condition | Message | Behaviour |
|-----------|---------|-----------|
| Confidence below threshold | `Step N/M [file] — confidence: X.XX ✗ (threshold: Y.YY)` | Stop; History unchanged |
| @mention not resolved | `pipeline: @mention "name" did not resolve to a known model` | Stop immediately |
| File not in workspace | `pipeline: cannot read "file": <os error>` | Stop immediately |
| Context overflow | `pipeline: step N context limit exceeded` | Stop |
| All confidence methods fail | Keyword scan always produces a score; pipeline continues | — |

## Fountain Session Recording

### Entry

```
INT. PIPELINE SETUP.MD STEP 1/3 2026-05-20 14:30:00

RSDOIEL executes /pipeline 90% setup.md step1.md step2.md.
Model: llama3:latest. Workspace: /path/to/workspace.
(hidden confidence instruction appended)

RSDOIEL
<step file content>

HARVEY
<step response>

(confidence: 0.91 — threshold met)
```

### Between Steps

Each step begins a new scene heading. A `CUT TO:` line separates steps.

### Exit (Success)

```
CUT BACK TO:

INT. HARVEY AND RSDOIEL TALKING 2026-05-20 14:33:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /path.

HARVEY
<final step response appended to History>
```

### Exit (Failure)

```
CUT BACK TO:

INT. HARVEY AND RSDOIEL TALKING 2026-05-20 14:32:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /path.
(pipeline stopped at step 2/3: confidence 0.72 below threshold 0.90)
```

## Security Considerations

- Pipeline files are resolved with `resolveWorkspacePath`; path traversal
  outside the workspace root is blocked.
- Temporary `AnyLLMClient` instances use the same provider backend as the
  active session; no new credential material is introduced.
- The hidden confidence instruction is logged as a parenthetical note for
  auditability.
- No pipeline output is stored outside `a.History` (on success) or the
  terminal scrollback (on failure or during execution).
