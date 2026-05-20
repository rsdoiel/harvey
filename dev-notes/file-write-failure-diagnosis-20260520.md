# Harvey File-Write Failure: Diagnosis (2026-05-20)

## Summary

Harvey was tested with the APERTUS-TOOLS model (abb-decide/apertus-tools:8b-instruct-2509-q4_k_m)
to generate and write a Bash script (`parse_pdf.sh`) to the workspace. The model repeatedly
produced the script as a markdown code block in its prose response but never wrote the file
to disk. This behavior was confirmed via `--debug` replay of session
`agents/sessions/harvey-session-20260520-083311.spmd`.

## What Happened

The session had four user turns:
1. Ask for key points about picking Ollama models.
2. Choose option 1 (create a bash parser for PDFs).
3. Ask to see how to create `parse_pdf.sh`.
4. Explicitly: "Write the bash script parse_pdf.sh to the workspace directory."

In all four turns, the debug log (`agents/logs/harvey-20260520-100551.jsonl`) showed only
`llm_request` and `llm_response` events — **no `tool_call`, `command_exec`, `file_write`,
or `command_blocked` events at all**.

## Root Causes

### 1. Model does not use tool-call protocol for file writes

APERTUS-TOOLS treated "write the file" as a prose generation task. It narrated what the
script would contain and wrapped it in a markdown code block, but never invoked any Harvey
tool or command to actually create the file. The model lacks reliable tool-use behaviour
for action requests like file creation.

### 2. No write-capable command in `allowed_commands`

Harvey's `harvey.yaml` `allowed_commands` list contains only read-only shell commands:
`ls`, `cat`, `grep`, `head`, `tail`, `wc`, `find`, `stat`, `jq`, `htmlq`, `bat`, `batcat`.
Even if the model had attempted to shell out with `echo ... > file` or `tee`, it would have
been blocked by Harvey's command filter. There is no built-in write-file tool exposed to
the model.

### 3. Script content was also incorrect

As a secondary issue, the generated script used `tac` on a binary PDF file (which would
produce garbage output). A correct implementation requires `pdftotext` (poppler) to extract
text before processing. This suggests the model was not grounding its tool choices in
knowledge of available system utilities.

## Recommended Fixes

1. **Add a write-file built-in tool** — expose a `write_file(path, content)` tool in
   Harvey's tool registry so models can write files without needing shell access. This
   keeps writes inside the workspace boundary and auditable in the debug log.

2. **Add `tee` (or a wrapper) to `allowed_commands`** as a stopgap so that a shell-savvy
   model can at least pipe content to a file via an allowed command.

3. **Test with a model that has stronger tool-use reliability** — `qwen2.5-coder:7b` or
   `devstral-small-2:24b` are candidates. Run a standardised write-file prompt against
   candidate models before using them in colleague demos.

4. **Code-block extraction** — as a fallback, Harvey could detect a lone fenced code block
   in a model response and offer to write it to disk interactively (similar to how Claude
   Code handles generated scripts).

## Status

Not ready for colleague demonstration. Needs at least fix #1 or #2 above before the
file-write workflow is reliable.
