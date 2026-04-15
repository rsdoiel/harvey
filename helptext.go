package harvey

var (
	HelpText = `%{app_name}(1) user manual | version {version} {release_hash}
% R. S. Doiel
% {release_date}

# NAME

{app_name}

# SYNOPSIS

{app_name} [OPTIONS] 

# DESCRIPTION

{app_name} is a terminal agent for local large language models and optionally
for publicai.co. It was inspired by Claude Code but focused on working with
large language models in small computer environments like a Raspberry Pi
computer running Raspberry Pi OS. While the inspiration was to run an
agent locally with Ollama it can also be run on larger computers like
Linux, macOS and Windows systems you find on desktop and laptop computers.
It should compile it for most systems where Ollama is avialable and Go 
is supported (exmample *BSD).

{app_name} looks for HARVEY.md in the current directory and uses it as a
system prompt. It then connects to a local Ollama server or publicai.co
and starts an interactive chat session.

All file I/O is constrained to the workspace directory (--workdir or ".").
A knowledge base is stored at <workdir>/.harvey/knowledge.db and is created
automatically on first run.

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

# ENVIRONMENT

PUBLICAI_API_KEY    API key for publicai.co

`


)

