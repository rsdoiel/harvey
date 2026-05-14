%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

ROUTING

# SYNOPSIS

@name prompt text

# DESCRIPTION

Harvey can dispatch individual prompts to remote LLM endpoints — other Ollama
instances on a Pi cluster, Llamafile servers, or cloud providers — using
@mention syntax. Prefix any prompt with @name to send it to the named endpoint instead
of the local model. The reply is streamed back and lands in the local
conversation history so future turns retain full context.

Routing is explicitly user-driven: there is no automatic classification.
You choose which endpoint handles each prompt by using (or omitting) an
@mention.

# CONTEXT WINDOW

When a prompt is dispatched to a remote endpoint, Harvey sends the last
10 non-system messages from the local history alongside it. System messages
are excluded. This gives the remote model enough context to be useful without
sending the entire conversation over the network. The window size is a
starting point and will be tuned over time.

# ENDPOINT TYPES

Local providers (no API key):

  ollama://host:port    A remote Ollama server (also accepts http:// and https://).
  llamafile://host:port A Llamafile binary server (OpenAI-compatible, port 8080).
  llamacpp://host:port  A llama.cpp server (OpenAI-compatible, port 8080).

Cloud providers (API key read from environment):

  anthropic://  Anthropic Claude  (ANTHROPIC_API_KEY)
  deepseek://   DeepSeek          (DEEPSEEK_API_KEY)
  gemini://     Google Gemini     (GEMINI_API_KEY or GOOGLE_API_KEY)
  mistral://    Mistral           (MISTRAL_API_KEY)
  openai://     OpenAI            (OPENAI_API_KEY)

# EXAMPLE SESSION

~~~
  # Register a Pi cluster node
  /route add pi2 ollama://192.168.1.12:11434 llama3.1:8b

  # Register the Anthropic cloud endpoint
  /route add claude anthropic:// claude-sonnet-4-20250514

  # Enable routing
  /route on

  # Dispatch a complex task to the cloud
  @claude refactor this module to use the repository pattern

  # Run a quick task on a Pi node
  @pi2 write a unit test for the Parse function

  # Local model handles everything else (no @mention)
  what does this error mean?
~~~

# SLASH COMMANDS

~~~
  /route add NAME URL [MODEL]   register a remote endpoint
                                  @pi2    ollama://192.168.1.12:11434 llama3.1:8b
                                  @claude anthropic:// claude-sonnet-4-20250514
  /route rm NAME                remove a registered endpoint
  /route list                   show all endpoints with reachability status
  /route on                     enable @mention dispatch (persisted)
  /route off                    disable @mention dispatch (persisted)
  /route status                 show routing state and endpoint count
~~~

Registered endpoints and the on/off state persist across sessions in
`<workspace>/agents/routes.json.`

