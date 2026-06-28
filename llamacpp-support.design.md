# Llama.cpp Server Support Design (Option A - Server-Based)

**Author**: Harvey Team  
**Date**: 2026-06-28  
**Status**: Draft  
**Related**: DECISIONS.md entry "2026-06-28 — Llama.cpp integration via server mode (Option A)"  

---

## Overview

This document describes the design for adding **server-based llama.cpp support** to Harvey. This approach integrates llama.cpp as a first-class backend alongside Ollama, Llamafile, and cloud providers, while leveraging llama.cpp's HTTP server mode (`llama-server`) rather than embedding the library directly.

### Why Server-Based?

- **Leverages existing infrastructure**: Harvey already has a robust `any-llm-go` integration that includes a `llamacpp` provider
- **Simpler implementation**: No CGO, no build complexity, no direct memory management
- **Proven architecture**: Mirrors the existing Ollama integration pattern
- **Lower risk**: Uses llama.cpp in its intended deployment mode (client-server)
- **Faster delivery**: Can be implemented in 1-2 days vs. 1-2 weeks for direct embedding

### Key Design Decision

**Harvey's model storage**: All GGUF model files are stored in `~/Models/` and use the `.gguf` extension to distinguish them from Llamafile models (which use `.llamafile` extension).

---

## Goals

1. **Parity with Ollama**: Users should have equivalent experience for model management between Ollama and llama.cpp backends
2. **Zero-config for common cases**: ` /llamacpp start phi-3-mini.gguf` should just work
3. **Graceful degradation**: If llama.cpp isn't installed, provide clear error messages
4. **Security by default**: Bind to `127.0.0.1` by default, validate model paths
5. **Integration with existing routing**: Registered `llamacpp://` endpoints continue to work

---

## Non-Goals

- **Direct embedding** (Option B): Embedding llama.cpp via CGO for in-process inference is deferred to a future phase
- **Automatic model downloading**: Users must explicitly pull models (or we provide a simple download helper)
- **Multi-GPU support**: Out of scope for Raspberry Pi-focused use case
- **Model conversion**: Converting models to GGUF format is out of scope (users do this externally)

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Harvey Agent                               │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌───────────────────────────┐  │
│  │  /ollama     │  │ /llamacpp    │  │   RouteRegistry            │  │
│  │  commands    │  │ commands     │  │ (existing)                 │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────────┬─────────────┘  │
│         │                │                      │               │
│         ▼                ▼                      ▼               │
│  ┌─────────────┐  ┌─────────────┐      ┌─────────────────┐        │
│  │ Ollama      │  │ LlamaCpp     │      │ RouteEndpoint    │        │
│  │ Server      │  │ Server       │      │ (llamacpp://...) │        │
│  │ Process     │  │ Process      │      │                 │        │
│  └──────┬──────┘  └──────┬──────┘      └─────────────────┘        │
│         │                │                                      │
│         ▼                ▼                                      │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │                 AnyLLMClient (any-llm-go)                  │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌───────────────────┐   │   │
│  │  │ ollama      │  │ llamacpp    │  │ openai, mistral,   │   │   │
│  │  │ provider    │  │ provider    │  │ anthropic, etc.    │   │   │
│  │  └─────────────┘  └─────────────┘  └───────────────────┘   │   │
│  └───────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │   LLMClient     │ ← Harvey's interface
                    │   interface      │
                    └─────────────────┘
```

### Component Model

#### 1. Model Storage (`~/Models/`)
- All GGUF files are stored in `~/Models/` directory
- File extension: `.gguf` (distinguishes from Llamafile's `.llamafile`)
- Example: `~/Models/phi-3-mini-4k-instruct.Q4_K_M.gguf`

#### 2. LlamaCppClient (New)
A helper client for llama.cpp-specific operations:

```go
// llamacpp.go
type LlamaCppClient struct {
    baseURL    string           // e.g., "http://127.0.0.1:8080/v1"
    modelDir   string           // Path to ~/Models
    serverProc *os.Process      // Track running llama-server process
}

func (c *LlamaCppClient) StartServer(modelPath string, host string, port int) error
func (c *LlamaCppClient) StopServer() error
func (c *LlamaCppClient) ListModels() ([]ModelInfo, error)
func (c *LlamaCppClient) DownloadModel(repoID string, filename string) error
```

#### 3. Command Handler (`cmdLlamaCpp`)
New command group in `commands.go`:

```go
func cmdLlamaCpp(a *Agent, args []string, out io.Writer) error
```

Subcommands:
- `start MODEL [host] [port]` - Launch llama-server with a model
- `stop` - Stop the running llama-server
- `pull REPO [FILENAME]` - Download a GGUF model from HuggingFace Hub
- `list` - List available GGUF models in `~/Models/`
- `status` - Check if llama-server is running
- `use MODEL` - Switch to a different loaded model

#### 4. Agent Integration
Add to `Agent` struct:

```go
type Agent struct {
    // ... existing fields ...
    
    // Llama.cpp integration
    llamaCppProc   *os.Process  // Track llama-server subprocess
    llamaCppURL    string       // Current llama-server URL (e.g., "http://127.0.0.1:8080/v1")
    llamaCppModel  string       // Current loaded model name
}
```

#### 5. Configuration
Add to `config.go`:

```go
type LlamaCppConfig struct {
    ServerPath string        // Path to llama-server binary (default: "llama-server")
    ModelDir   string        // Path to model directory (default: "~/Models")
    Host       string        // Default host to bind (default: "127.0.0.1")
    Port       int           // Default port (default: 8080)
    Timeout    time.Duration // HTTP timeout (default: 30s)
}

type Config struct {
    // ... existing fields ...
    LlamaCpp LlamaCppConfig
}
```

---

## File Changes

| File | Change Type | Description |
|------|-------------|-------------|
| `harvey/llamacpp.go` | **New** | LlamaCppClient and helper functions |
| `harvey/commands.go` | Modify | Add `cmdLlamaCpp` and subcommands |
| `harvey/harvey.go` | Modify | Add fields to `Agent` struct |
| `harvey/config.go` | Modify | Add `LlamaCppConfig` struct and defaults |
| `harvey/routing.go` | None | Existing `KindLlamaCpp` support is sufficient |
| `harvey/anyllm_client.go` | None | Existing `newLlamaCppLLMClient` is sufficient |
| `harvey/DECISIONS.md` | Modify | Record the decision to use server-based approach |
| `harvey/ROUTING.md` | Modify | Document llama.cpp endpoint usage |
| `man/man7/harvey-llamacpp.7.md` | **New** | Man page for `/llamacpp` commands |
| `templates/help/llamacpp.md` | **New** | Help template for `/help llamacpp` |

---

## Command Reference

### `/llamacpp start [MODEL] [HOST] [PORT]`

Starts a llama-server instance with the specified model.

**Arguments:**
- `MODEL` (required): Model name or path. If just a name (e.g., `phi-3-mini`), Harvey searches `~/Models/` for `{name}.gguf`. If a path, uses it directly.
- `HOST` (optional): Host to bind. Default: `127.0.0.1`
- `PORT` (optional): Port to listen on. Default: `8080`

**Behavior:**
1. Checks if `llama-server` binary is available on PATH
2. Resolves model path (search `~/Models/` if needed)
3. Validates the model file exists and has `.gguf` extension
4. Launches `llama-server --model <path> --host <host> --port <port> --nobrowse`
5. Probes until server is ready (timeout: 30s)
6. Updates `a.Client` to use the new server
7. Sets `a.llamaCppProc` and `a.llamaCppURL` for tracking

**Example:**
```
/llamacpp start phi-3-mini
/llamacpp start ~/Models/phi-3-mini.Q4_K_M.gguf 0.0.0.0 9090
```

### `/llamacpp stop`

Stops the currently running llama-server instance.

**Behavior:**
1. If `a.llamaCppProc` is set, sends SIGTERM
2. Waits for process to exit (timeout: 5s)
3. Clears `a.llamaCppProc` and `a.llamaCppURL`
4. Falls back to previous client (if any) or no client

### `/llamacpp pull REPO [FILENAME]`

Downloads a GGUF model from HuggingFace Hub.

**Arguments:**
- `REPO` (required): HuggingFace repository ID (e.g., `bartowski/Meta-Llama-3-8B-Instruct-GGUF`)
- `FILENAME` (optional): Specific GGUF file to download. If omitted, Harvey lists available GGUF files and prompts for selection.

**Behavior:**
1. Queries HuggingFace Hub API for repository files
2. Filters for `.gguf` files
3. If `FILENAME` provided, downloads that specific file
4. If no `FILENAME`, presents list of options
5. Downloads to `~/Models/`
6. Validates file integrity (basic size check)

**Example:**
```
/llamacpp pull bartowski/Meta-Llama-3-8B-Instruct-GGUF Meta-Llama-3-8B-Instruct-Q4_K_M.gguf
/llamacpp pull bartowski/Meta-Llama-3-8B-Instruct-GGUF
```

### `/llamacpp list`

Lists all GGUF models available in `~/Models/`.

**Output:**
```
Available GGUF models in ~/Models/:
  phi-3-mini-4k-instruct.Q4_K_M.gguf    2.1 GB  2026-06-01
  mistral-7b-instruct-v0.2.Q4_0.gguf     3.8 GB  2026-05-15
```

### `/llamacpp status`

Checks if a llama-server instance is running and accessible.

**Output:**
- If running: `llama-server is running at http://127.0.0.1:8080 with model phi-3-mini`
- If not: `llama-server is not running`

### `/llamacpp use MODEL`

Switches to a different model on the running server (requires server restart).

**Behavior:**
1. Stops current server (if running)
2. Starts new server with specified model
3. Updates client connection

**Example:**
```
/llamacpp use mistral-7b-instruct.Q4_K_M.gguf
```

---

## Integration with Existing Features

### Routing
Existing `llamacpp://` endpoints continue to work:

```
/route add pi5 llamacpp://192.168.1.100:8080
@pi5 write a test
```

The new `/llamacpp start` command simply provides a convenient way to start and manage the local server.

### Session Persistence
When Harvey starts a llama-server:
- `a.llamaCppProc` tracks the subprocess
- On Harvey exit, the server **continues running** (user must manually `/llamacpp stop` or Harvey can auto-stop if it started it)
- On next Harvey session, if a server is detected at the default URL, Harvey can automatically connect

### Model Caching
- Downloaded GGUF files persist in `~/Models/`
- No re-download on subsequent starts
- Users can manually manage `~/Models/` directory

---

## Error Handling

| Scenario | Behavior |
|----------|----------|
| `llama-server` not installed | Clear error: "llama-server not found. Install from https://github.com/ggerganov/llama.cpp" |
| Model not found in `~/Models/` | List available models: "Model not found. Available: phi-3-mini.gguf, ..." |
| Invalid `.gguf` file | Error: "File is not a valid GGUF model (magic bytes mismatch)" |
| Server fails to start | Error with stderr output from llama-server |
| Port already in use | Error: "Port 8080 already in use. Try a different port with /llamacpp start model 127.0.0.1 8081" |
| Server timeout | Error: "llama-server failed to start within 30 seconds" |

---

## Security Considerations

### ✅ Implemented
1. **Default bind to `127.0.0.1`**: Server not exposed to network by default
2. **Model path validation**: Only allow `.gguf` files from `~/Models/` or explicit paths
3. **No automatic execution**: Users must explicitly start servers
4. **Process isolation**: llama-server runs as separate subprocess

### ⚠️ User Responsibilities
1. **Model trust**: Users must trust downloaded GGUF files (malicious models could have harmful prompts)
2. **Network exposure**: Users explicitly opt-in to `0.0.0.0` binding
3. **Resource limits**: Users responsible for monitoring server resource usage

### 🔒 Future Enhancements
1. Model file checksum verification
2. Sandboxing for llama-server process
3. Rate limiting for remote endpoints

---

## Performance Considerations

### Server Startup Time
- **Cold start** (model not in memory): 5-30 seconds depending on model size
- **Warm start** (model cached): <1 second
- Harvey shows a spinner during startup with estimated time

### Token Throughput
Expected performance on Raspberry Pi 5 (16GB):

| Model | Quantization | Tokens/sec | VRAM Usage |
|-------|--------------|------------|------------|
| phi-3-mini | Q4_K_M | 8-12 | 1.2 GB |
| mistral-7b | Q4_K_M | 2-4 | 3.8 GB |
| llama-3-8b | Q4_K_M | 1-2 | 4.3 GB |

*Note: Performance varies based on CPU, RAM, and system load*

### Memory Usage
- **Model weights**: Loaded into RAM (or VRAM if GPU available)
- **KV cache**: Grows with context length
- **Server overhead**: ~100-200 MB

Harvey warns if model exceeds available RAM:
```
Warning: mistral-7b-instruct.Q4_K_M.gguf requires ~4GB RAM. 
Your system has 8GB free. Proceed? (y/N)
```

---

## Testing Strategy

### Unit Tests
1. `TestParseLlamaCppModelPath` - Path resolution logic
2. `TestLlamaCppConfigDefaults` - Configuration defaults
3. `TestLlamacppAPIURL` - URL conversion (existing, extend for new cases)

### Integration Tests
1. `TestLlamaCppStartStop` - Start and stop server
2. `TestLlamaCppListModels` - Model enumeration
3. `TestLlamaCppRouting` - Integration with `@mention` routing

### Manual Testing
1. Start server with various model sizes
2. Test concurrent requests
3. Test error conditions (invalid model, port in use, etc.)
4. Test integration with Harvey's chat loop

---

## Migration Path

### For Existing Users
No changes required. Existing workflows continue to work:
- Remote `llamacpp://` endpoints: Unchanged
- Ollama: Unchanged
- Other backends: Unchanged

### For New Users
1. Ensure `llama-server` is installed and on PATH
2. Download a GGUF model to `~/Models/`
3. Use `/llamacpp start model` or register a route

### For Harvey Developers
1. Update documentation to mention llama.cpp support
2. Add examples to getting started guide
3. Update architecture diagrams

---

## Rollout Plan

### Phase 1: Core Implementation (1-2 days)
- [ ] Create `llamacpp.go` with `LlamaCppClient`
- [ ] Add `/llamacpp start` command
- [ ] Add `/llamacpp stop` command
- [ ] Add `/llamacpp status` command
- [ ] Update `Agent` struct with tracking fields
- [ ] Update `Config` with `LlamaCppConfig`

### Phase 2: Model Management (1 day)
- [ ] Add `/llamacpp list` command
- [ ] Add `/llamacpp use` command
- [ ] Add `/llamacpp pull` command (basic version)

### Phase 3: Polish (1 day)
- [ ] Add man page (`harvey-llamacpp.7.md`)
- [ ] Add help template (`templates/help/llamacpp.md`)
- [ ] Add warnings for large models
- [ ] Add startup spinner with progress
- [ ] Update `ROUTING.md` with llama.cpp examples

### Phase 4: Documentation & Testing (1 day)
- [ ] Write unit tests
- [ ] Write integration tests
- [ ] Update `README.md` or `getting_started.md`
- [ ] Add to decision log

---

## Open Questions

1. **Should Harvey auto-stop llama-server on exit?**
   - Pro: Clean resource management
   - Con: Users might want server to persist
   - **Proposed**: Add config option `llamacpp.auto_stop: true` (default: false)

2. **Should we support multiple simultaneous llama-server instances?**
   - Pro: Run different models concurrently
   - Con: Complexity, port management
   - **Proposed**: Defer to future phase. For now, one server at a time.

3. **Should we bundle llama-server with Harvey?**
   - Pro: Better out-of-box experience
   - Con: Large binary, versioning complexity
   - **Proposed**: No. Document installation requirements.

4. **Should we support streaming model downloads?**
   - Pro: Better UX for large models
   - Con: Complexity
   - **Proposed**: Defer. For now, download entire file then start server.

---

## Success Criteria

✅ `/llamacpp start phi-3-mini` works on first try (assuming model exists)  
✅ `/llamacpp pull` successfully downloads a model from HuggingFace Hub  
✅ `/llamacpp list` shows all `.gguf` files in `~/Models/`  
✅ `@llamacpp` routing works with locally started server  
✅ Error messages are clear and actionable  
✅ No security regressions from existing Ollama integration  
✅ All existing tests continue to pass  

---

## References

- [llama.cpp GitHub](https://github.com/ggerganov/llama.cpp)
- [HuggingFace Hub](https://huggingface.co/models?search=gguf)
- [any-llm-go llamacpp provider](https://github.com/mozilla-ai/any-llm-go/tree/main/providers/llamacpp)
- [Harvey Routing Design](ROUTING.md)
- [Harvey Architecture](ARCHITECTURE.md)
