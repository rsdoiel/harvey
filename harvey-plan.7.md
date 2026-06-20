%harvey(7) user manual | version 0.0.13 b9900f6
% R. S. Doiel
% 2026-06-19

# NAME

PLAN — generate and execute step-by-step task plans with bounded context

# SYNOPSIS

/plan TASK
/plan next
/plan status
/plan show
/plan clear

# DESCRIPTION

/plan breaks a complex task into a numbered GFM checklist, saves it to
agents/plan.md, and executes each step using a bounded context — only the
system prompt and the current plan state are sent to the model per step.
This keeps per-step turn times constant regardless of conversation length
and allows large multi-step tasks without filling the context window.

The plan persists to agents/plan.md across /clear and Harvey restarts.
Use /plan status at any time to review progress.

# SUBCOMMANDS

  /plan TASK
    Ask the model to generate a step-by-step GFM checklist for TASK and
    save it to agents/plan.md. Each step becomes a checkbox item:
      - [ ] Step description
    An existing plan is overwritten; use /plan clear first if you want
    a clean start on a different task.

  /plan next
    Execute the next uncompleted step. Harvey sends only the system
    prompt and the current plan state — not the full conversation
    history — keeping context usage bounded. When a step's tool calls
    are blocked or fail, the step is NOT auto-marked complete; fix
    the underlying issue and run /plan next again.

  /plan status
    Print the plan checklist with completion markers, showing which
    steps are done and which remain.

  /plan show
    Print the raw agents/plan.md file.

  /plan clear
    Delete agents/plan.md. Does not affect conversation history.

# BOUNDED CONTEXT MODEL

Each /plan next call sends only:
  1. The system prompt (HARVEY.md).
  2. The current agents/plan.md content.

The full conversation history is NOT included. This means:
  - Per-step token usage is constant regardless of plan length.
  - The model has no memory of earlier steps beyond the plan file.
  - For steps that need context from prior steps, inject that context
    explicitly via /context add before running /plan next, or note it
    directly in agents/plan.md.

# WORKFLOW EXAMPLE

~~~
  # Break a large task into a plan
  /plan Refactor the auth package to use short-lived JWT tokens

  # Review the generated steps
  /plan status

  # Execute one step at a time
  /plan next
  /plan next

  # Check progress
  /plan status

  # If a step gets stuck, investigate and retry
  /plan next
~~~

# FILES

  agents/plan.md   — persisted plan checklist; human-editable

# SEE ALSO

  /help pipeline   — chain Markdown prompt files with confidence gating
  /help context    — inject context that persists across /plan next calls
  /help skills     — skills for structured multi-step task automation

