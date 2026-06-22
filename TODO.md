
# Action Items

## Bugs

- `/model use` (no arg): prints usage error instead of showing a picker of registered
  llamafile models and aliases. Should call SelectFromStrings(allModelNames(a), ...) when
  len(args) < 2, matching the UX of /ollama use, /llamafile use, and /rag use.

- `/session use` / `/session continue` (no arg): prints usage error instead of showing a
  picker of available session files. Should call ListSessionFiles(a.SessionsDir) and
  SelectFrom when len(args) < 2, matching the UX of /rag use and /llamafile use.

## Release Review

## Next (v0.0.15 release)


## llama.cpp server-side tool call parsing for Apertus

Apertus 4B uses a native tool call format (`<SPECIAL_71>[{"tool_name": args}]<SPECIAL_72>`)
that llama.cpp's server does not recognise as structured tool calls. Apertus tool calls
currently travel as raw text in the API content field and are parsed client-side by
Harvey's prose fallback (`tryExecuteApertusToolCalls` in `terminal.go`).

When llama.cpp adds support for registering a custom tool-call token pattern (or adds
built-in Apertus support), update `templates/apertus-4b-toolcall.jinja` in the henry
project to register the token pair so the server converts them to OpenAI API `tool_calls`
in the response. That would enable full multi-turn structured tool use instead of the
current single-turn prose fallback.

Track: https://github.com/ggml-org/llama.cpp/issues — search "custom tool call format"
or "Apertus".
