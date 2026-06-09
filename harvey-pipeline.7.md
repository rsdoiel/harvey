%harvey(7) user manual | version 0.0.9 d061889
% R. S. Doiel
% 2026-06-08

# NAME

PIPELINE — chain Markdown prompt files through models with confidence gating

# SYNOPSIS

/pipeline <CONFIDENCE%> FILE [FILE ...]

# DESCRIPTION

/pipeline executes a sequence of Markdown prompt files as discrete steps,
passing each step's response as input to the next. A confidence threshold
gates progression: if a step's measured confidence score falls below the
threshold the pipeline stops immediately and leaves conversation history
unchanged.

# ARGUMENTS

  CONFIDENCE%   Required first argument. Integer or decimal percentage in
                (0, 100]. The pipeline stops if any step's confidence score
                (0.0–1.0) is below this value divided by 100.

  FILE ...      One or more workspace-relative paths to Markdown files.
                Each file is one pipeline step, executed in order.

# PIPELINE FILE FORMAT

Each FILE is a plain Markdown document. Its body is sent verbatim to the
model as the user message.

  @mention — Model routing

  The first occurrence of @token in the file body selects the model for
  that step. The token is matched against registered routes (/route list)
  first, then used as a model name override on the same provider backend.
  Later @mentions are passed as-is to the model. If the mention cannot be
  resolved the pipeline stops before executing any steps.

# CONTEXT FLOW

  Step 1    carries Harvey's full current conversation history so the first
            model has session context.

  Step N>1  starts a fresh conversation (system prompt only) and receives
            the previous step's response as the user message, followed by
            the step file body. This keeps context usage minimal.

# CONFIDENCE EXTRACTION

After each step Harvey attempts to extract a confidence score using three
methods in priority order:

  1. JSON block — parse {"confidence": X.X, ...} at end of response.
  2. Follow-up — ask the model to rate its confidence 0.0–1.0.
  3. Keyword scan — hedging phrases → 0.30; no hedging → 0.80.

The confidence block is stripped from the response before it is displayed
or forwarded to the next step.

# EXAMPLES

  /pipeline 85% review.md summarise.md
  /pipeline 90% setup.md step1.md step2.md finalise.md

# SESSION STATE

On success the final step's response is appended to conversation history
as an assistant turn. On any failure history and the active model are
unchanged.
