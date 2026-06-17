
# Llamafile Notes

[Llamafile](https://mozilla-ai.github.io/llamafile/) is a Mozilla AI project
to deliver OS-agnostic, hardware-agnostic language models as a single
executable file. They achieve this by combining llama.cpp with the
Cosmopolitan C library, with no emulators or virtual machines required. See
the [project page](https://github.com/mozilla-ai/llamafile) for details,
including
[example Llamafiles](https://github.com/mozilla-ai/llamafile/blob/main/docs/example_llamafiles.md).

## Current Status: Integrated

As of Harvey's llamafile integration (see [llamafile-design.md](llamafile-design.md)
and [llamafile-plan.md](llamafile-plan.md)), llamafile is a supported backend.

An earlier investigation concluded that llamafiles could not be integrated
with Harvey because they only exposed an interactive chat interface with no
accessible web API. **That conclusion is now outdated.** Llamafile v0.10.x
exposes a full OpenAI-compatible HTTP API at `http://localhost:8080/v1`,
including:

| Capability | Status |
|---|---|
| Chat completions (streaming) | Yes |
| Embeddings | Yes |
| Tool / function calling | Yes |
| `/v1/models` list endpoint | Yes |
| Multimodal image input | Yes, model-dependent |

## Using Llamafile with Harvey

Configure the path to your llamafile binary in `agents/harvey.yaml`:

```yaml
llamafile:
  path: /home/user/models/Llama-3.2-3B-Instruct.Q4_K_M.llamafile
  url:  http://localhost:8080   # optional; this is the default
```

Or pass `--llamafile PATH` on the command line for a one-shot override.

Harvey will probe whether the llamafile server is already running at startup.
If it is not, Harvey offers to launch it automatically. From the user's
perspective, only one program needs to be installed and configured.

## Implementation

- `llamafile_service.go` — `ProbeLlamafile` and `StartLlamafileService`
- `anyllm_client.go` — `newLlamafileLLMClient` (via `any-llm-go`)
- `terminal.go` — `selectBackend`, `useLlamafile`, `llamafileModelName`
- `config.go` — `LlamafilePath`, `LlamafileURL` fields and YAML loading
