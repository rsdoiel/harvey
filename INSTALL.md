Installation **harvey**
============================

**harvey** is a terminal coding agent that runs language models locally.
No cloud account or API key required.

## Requirements

- Go >= 1.26.3
- Git
- A running model backend — either [Ollama](https://ollama.com) or a
  [llamafile](https://github.com/Mozilla-Ocho/llamafile)

## Hardware requirements

Harvey runs entirely on local hardware, so the practical limits are RAM (to hold the model) and storage (for the model files themselves), not just raw compute.

- **RAM** — 8 GB minimum for models up to ~7B parameters (matches the default `Qwen2.5-Coder-7B` recommendation below); 16 GB or more gives headroom for larger models, or lets a 7B-class model run while leaving RAM free for the rest of your work.
- **Storage** — at least 64 GB free if you expect to try more than one model. Individual llamafiles and Ollama models routinely run several GB each (the `Qwen2.5-Coder-7B-Q5_K_S.llamafile` below is ~5 GB), and Harvey's RAG stores and session recordings add up over time.
- **Raspberry Pi** — a Raspberry Pi 5 (8 GB or more) runs Harvey fine with CPU-only inference; expect roughly 2–10 tokens/s depending on model size and quantization without dedicated AI accelerator hardware. A hardware NPU accelerator (e.g. Raspberry Pi's AI HAT+ 2) is not currently supported as a backend.

## Compile from source

```shell
git clone https://github.com/rsdoiel/harvey
cd harvey
make
make test
make install
```

`make install` copies the binaries into `$HOME/bin/` by default.
Override with `make install prefix=/usr/local`.

## Model backend

### Ollama (recommended)

Install Ollama from <https://ollama.com/download>, then pull a model:

```shell
ollama pull qwen2.5-coder:7b
```

Harvey detects Ollama automatically on startup.

### Llamafile (self-contained, no install)

Download a pre-built llamafile from:
<https://docs.mozilla.ai/llamafile/getting-started/pre-built-llamafiles>

Recommended:
- `Qwen2.5-Coder-7B-Q5_K_S.llamafile` (~5 GB, good for most hardware)
- `Phi-3.5-mini-instruct-Q4_K_M.llamafile` (~2 GB, low-VRAM / CPU)

Place it in `~/Models/` and make it executable (Linux / macOS):

```shell
chmod +x ~/Models/Qwen2.5-Coder-7B-Q5_K_S.llamafile
```

Harvey finds the llamafile automatically and connects.

## Running harvey

```shell
cd ~/myproject
harvey
```
