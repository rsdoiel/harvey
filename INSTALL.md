Installation **harvey**
============================

**harvey** is a terminal coding agent that runs language models locally.
No cloud account or API key required.

## Quick start (Llamafile — recommended)

The simplest way to get Harvey running is with a llamafile — a single
self-contained file that bundles a language model and its own HTTP server.

1. Download a llamafile from:
   <https://huggingface.co/Mozilla/llamafile-models>

   Recommended:
   - `Qwen2.5-Coder-7B-Q5_K_S.llamafile` (~5 GB, good for most hardware)
   - `Phi-3.5-mini-instruct-Q4_K_M.llamafile` (~2 GB, low-VRAM / CPU)

2. Place it in `~/Models/` and make it executable (Linux / macOS):

   ```shell
   chmod +x ~/Models/Qwen2.5-Coder-7B-Q5_K_S.llamafile
   ```

3. Install Harvey (see below), then run it in your project directory:

   ```shell
   cd ~/myproject
   harvey
   ```

   Harvey finds the llamafile automatically and connects.

## Quick start (Ollama — optional)

If you prefer a persistent model server or want access to the full Ollama
model library:

```shell
# Install Ollama from https://ollama.com/download, then:
ollama pull qwen2.5-coder:7b
```

Harvey detects Ollama automatically on startup.

## Installing Harvey

### Installer script (Linux / macOS / WSL)

```shell
curl https://rsdoiel.github.io/harvey/installer.sh | sh
```

This installs Harvey into `$HOME/bin/`.

### Windows (PowerShell)

```ps1
irm https://rsdoiel.github.io/harvey/installer.ps1 | iex
```

### Security warnings (macOS / Windows)

If you see a security warning about an unverified developer, see:

- [INSTALL_NOTES_macOS.md](INSTALL_NOTES_macOS.md)
- [INSTALL_NOTES_Windows.md](INSTALL_NOTES_Windows.md)

### Installing from source

**Requirements:** Go >= 1.26.3

```shell
git clone https://github.com/rsdoiel/harvey
cd harvey
make
make test
make install
```
