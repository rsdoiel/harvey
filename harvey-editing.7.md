%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

EDITING — line editing and multi-line input

# SYNOPSIS

Type at the "harvey >" prompt. Use key bindings below to navigate and edit.
For multi-line input, press Ctrl+X Ctrl+E to open an external editor.

# LINE EDITING

Harvey's prompt supports readline-style single-line editing.

Navigation:

  Left / Right arrows    move cursor one character
  Home / Ctrl+A          jump to beginning of line
  End  / Ctrl+E          jump to end of line
  Up / Down arrows       cycle through prompt history

Editing:

  Backspace              delete character before cursor
  Ctrl+D                 delete character under cursor; exits on empty line
  Ctrl+K                 delete from cursor to end of line

Actions:

  Enter                  submit the prompt to the model
  Ctrl+C                 cancel current input and return to prompt

# MULTI-LINE INPUT WITH $EDITOR

Press Ctrl+X then Ctrl+E to open the current line in your preferred editor.
Harvey reads the environment variables in this order to find the editor:

  1. $EDITOR
  2. $VISUAL
  3. vi  (hard fallback)

Write or paste your multi-line text in the editor, then save and quit.
Harvey reads the file on exit and submits the full contents as your prompt.
This is the recommended approach for long prompts, pasted code, or anything
with embedded newlines.

# TIPS

  - Up/Down arrows recall previous prompts, including multi-line ones
    that were composed in $EDITOR.
  - Ctrl+C on an empty line has no effect (Harvey does not exit on ^C).
    Use /exit, /quit, or /bye to end the session.
  - If $EDITOR is not set, export it in your shell profile:
      export EDITOR=nano    # or vim, emacs, hx, micro, etc.

