
# Llamafile Notes

[Llamafile](https://mozilla-ai.github.io/llamafile/) is a Mozilla AI project
to deliver OS-agnostic, hardware-agnostic language models as a single executable
file. They achieve this by combining llama.cpp with the Cosmopolitan C library,
with no emulators or virtual machines required. See the
[project page](https://github.com/mozilla-ai/llamafile) for details, including
[example Llamafiles](https://github.com/mozilla-ai/llamafile/blob/main/docs/example_llamafiles.md).

After experimentation I concluded that Llamafiles cannot be directly integrated
with Harvey. Llamafiles bundle their own integrated chat client and restrict
interaction to that interface; they do not expose an accessible web API the way
Ollama does. Harvey requires a web API endpoint to communicate with a model
backend, so Llamafiles fall outside what Harvey can use.

The chat-only restriction is a deliberate security choice, and the file format
itself is genuinely interesting. It is worth monitoring the project in case it
evolves to expose a compatible API in the future.
