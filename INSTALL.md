Installation **harvey**
============================

**harvey** is a terminal coding agent that runs language models locally.
No cloud account or API key required.

## Requirements

- Go >= 1.26.3
- Git
- A running model backend — either [Ollama](https://ollama.com) or a
  [llamafile](https://github.com/Mozilla-Ocho/llamafile)

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
