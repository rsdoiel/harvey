# Ollama Setup

Ollama runs language models locally on your machine. Harvey uses it
as its default backend.

---

## Install Ollama

**Download:** https://ollama.com/download

Follow the installer for your platform. On macOS and Windows, Ollama
runs as a background service automatically after install. On Linux,
enable the service:

    sudo systemctl enable --now ollama

---

## Pull a Model

After installing, download at least one model:

    ollama pull granite3.3:2b      # small, fast; good for most tasks
    ollama pull qwen2.5-coder:7b   # better for code; needs ~5 GB RAM

List installed models:

    ollama list

---

## Check the Connection

Harvey connects to Ollama at `http://localhost:11434` by default.
If Harvey cannot connect:

1. Confirm Ollama is running: `ollama list` should return results.
2. If not running, start it: `ollama serve`
3. If using a non-default port, set `ollama_url:` in `agents/harvey.yaml`.

---

## Managing Models from Inside Harvey

Harvey's `/ollama` command mirrors the Ollama CLI:

    /ollama list              — list installed models
    /ollama pull MODEL        — download a model
    /ollama use MODEL         — switch to a different model this session
    /ollama status            — check whether Ollama is reachable
    /ollama start             — launch ollama serve in the background

Run `/help ollama` for the full command reference.

---

## See Also

    /help getting-started    — complete first-run prerequisites guide
