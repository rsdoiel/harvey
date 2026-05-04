%harvey(1) user manual | version 0.0.1b 6f586b0
% R. S. Doiel
% 2026-05-02

# NAME

harvey

# SYNOPSIS

harvey [OPTIONS] 

# DESCRIPTION

harvey is a terminal agent for local large language models. It was
inspired by Claude Code but focused on working with large language models
in small computer environments like a Raspberry Pi computer running
Raspberry Pi OS. While the inspiration was to run an agent locally with
Ollama it can also be run on larger computers like Linux, macOS and Windows
systems you find on desktop and laptop computers. It should compile for most
systems where Ollama is available and Go is supported (example: *BSD).

harvey looks for HARVEY.md in the current directory and uses it as a
system prompt. It then connects to a local Ollama server and starts an
interactive chat session. Cloud providers (Anthropic, DeepSeek, Gemini,
Mistral, OpenAI) can be added as named routes via /route add.

All file I/O is constrained to the workspace directory (--workdir or ".").
A knowledge base is stored at <workdir>/harvey/knowledge.db and is created
automatically on first run. Session recordings (.spmd files) are stored in
<workdir>/harvey/sessions/. Both paths can be overridden in harvey/harvey.yaml.

Type /help inside the session for available slash commands.

# OPTIONS

-h, --help
: display this help message

-v, --version
: display version information

-l, --license
: display license information

-m, --model
: MODEL   Ollama model to use on startup

--ollama URL
: Ollama base URL (default: http://localhost:11434)
-w, --workdir DIR
: workspace directory (default: current directory)

-r, --record
: start a Fountain recording automatically at startup

--record-file FILE
: path for the auto-recording file (implies --record)

--continue FILE
: load conversation history from a Fountain recording and open the REPL

--replay FILE
: re-send every user turn from FILE to the current model and record fresh responses

--replay-output FILE
: write replay responses to FILE (default: auto-named timestamped file; implies --replay)

# ENVIRONMENT

ANTHROPIC_API_KEY   API key for Anthropic Claude (optional, for /route add NAME anthropic://)
DEEPSEEK_API_KEY    API key for DeepSeek (optional, for /route add NAME deepseek://)
GEMINI_API_KEY      API key for Google Gemini (optional; GOOGLE_API_KEY also accepted)
MISTRAL_API_KEY     API key for Mistral (optional, for /route add NAME mistral://)
OPENAI_API_KEY      API key for OpenAI (optional, for /route add NAME openai://)

# LINE EDITING

Harvey's prompt supports readline-style editing. All key bindings apply
while typing at the "harvey >" prompt.

Navigation:

  Left / Right arrows    move cursor one character
  Home / Ctrl+A          jump to beginning of line
  End  / Ctrl+E          jump to end of line
  Up / Down arrows       cycle through command history

Editing:

  Backspace              delete character before cursor
  Ctrl+D                 delete character under cursor (EOF on empty line)
  Ctrl+K                 delete from cursor to end of line

Actions:

  Ctrl+C                 cancel current input and return to prompt
  Ctrl+X  Ctrl+E         open $EDITOR (then $VISUAL, then vi) to compose
                         a multi-line prompt; content is submitted when
                         the editor exits

