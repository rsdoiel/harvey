# Harvey Routing: Remote Endpoint Configuration

*Version 1.0 — Complete guide to multi-model routing in Harvey*

## Overview

Harvey's **routing system** allows you to dispatch prompts to **remote LLM
endpoints** — other Ollama instances on your network, Llamafile servers, or
cloud providers (Anthropic, DeepSeek, Gemini, Mistral, OpenAI). This enables:

- **Multi-model workflows**: Use different models for different tasks
- **Hardware distribution**: Offload to more powerful machines
- **Cloud integration**: Access commercial APIs when needed
- **Load balancing**: Distribute work across a cluster

### Core Concept: @mention Routing

Prefix any prompt with `@name` to send it to a registered remote endpoint:

```
RSDOIEL
@claude refactor this module

HARVEY
Forwarding to CLAUDE.

CLAUDE
Here is the refactored code...
```

The response is **streamed back** into your local conversation history, so
Harvey retains full context of the exchange.

## Quick Start

### Register Your First Remote Endpoint

```bash
# In Harvey REPL:
harvey> /route add pi2 ollama://192.168.1.12:11434 llama3.1:8b

# Or from shell:
harvey --route add pi2 ollama://192.168.1.12:11434 llama3.1:8b
```

### Enable Routing

```
harvey> /route on
```

### Use It

```
harvey> @pi2 explain this algorithm

HARVEY
Forwarding to PI2.

PI2
The algorithm implements a depth-first search with...
```

## Endpoint Types

Harvey supports **10 endpoint types** across local and cloud providers:

### Local Providers (No API Key Required)

| Type | Scheme | Backend | Default Port | Notes |
|------|--------|---------|--------------|-------|
| Ollama | `ollama://host:port` | Ollama | 11434 | Official Ollama server |
| Ollama | `http://host:port` | Ollama | 11434 | HTTP (insecure) |
| Ollama | `https://host:port` | Ollama | 11434 | HTTPS (secure) |
| Llamafile | `llamafile://host:port` | Llamafile | 8080 | OpenAI-compatible |
| llama.cpp | `llamacpp://host:port` | llama.cpp | 8080 | OpenAI-compatible |

### Cloud Providers (API Key Required)

| Type | Scheme | Backend | API Key Variable | Notes |
|------|--------|---------|------------------|-------|
| Anthropic | `anthropic://` | Claude | `ANTHROPIC_API_KEY` | All Claude models |
| DeepSeek | `deepseek://` | DeepSeek | `DEEPSEEK_API_KEY` | All DeepSeek models |
| Gemini | `gemini://` | Google | `GEMINI_API_KEY` | Primary |
| Gemini | `gemini://` | Google | `GOOGLE_API_KEY` | Fallback |
| Mistral | `mistral://` | Mistral | `MISTRAL_API_KEY` | All Mistral models |
| OpenAI | `openai://` | OpenAI | `OPENAI_API_KEY` | All OpenAI models |

## Configuration Files

### Route configuration: `<workspace>/agents/routes.json`

All registered endpoints and routing state are persisted in JSON format
inside the workspace. There is no global route configuration.

**Location:** `<workspace>/agents/routes.json`

**Example:**
```json
{
  "enabled": true,
  "endpoints": [
    {
      "name": "pi2",
      "url": "ollama://192.168.1.12:11434",
      "model": "llama3.1:8b",
      "kind": "ollama"
    },
    {
      "name": "pi3",
      "url": "ollama://192.168.1.13:11434",
      "model": "mistral:latest",
      "kind": "ollama"
    },
    {
      "name": "claude",
      "url": "anthropic://",
      "model": "claude-3-haiku",
      "kind": "anthropic"
    },
    {
      "name": "gemini",
      "url": "gemini://",
      "model": "gemini-1.5-flash",
      "kind": "gemini"
    }
  ]
}
```

### Workspace configuration: `<workspace>/agents/harvey.yaml`

Additional Harvey configuration (knowledge base path, sessions directory,
RAG stores, etc.) lives in `agents/harvey.yaml` inside the workspace.

## Commands Reference

### `/route add NAME URL [MODEL]`

Register a new remote endpoint.

**Arguments:**
- `NAME`: Unique identifier (used in @mentions)
- `URL`: Full URL including scheme (see table above)
- `MODEL`: Optional default model for this endpoint

**Examples:**
```
/route add pi2 ollama://192.168.1.12:11434 llama3.1:8b
/route add pi2 ollama://192.168.1.12:11434
/route add claude anthropic:// claude-3-haiku
/route add myollama http://localhost:11434
/route add secure https://my-ollama.example.com:443
```

**Notes:**
- `NAME` must be unique across all endpoints
- If `MODEL` is omitted, you must specify it each time you use the endpoint
- The URL scheme determines the backend type

### `/route rm NAME`

Remove a registered endpoint.

**Examples:**
```
/route rm pi2
/route rm claude
```

### `/route list`

List all registered endpoints with reachability status.

**Output:**
```
  Endpoints registered:

  ✓  @pi2      ollama://192.168.1.12:11434  llama3.1:8b
  ✓  @pi3      ollama://192.168.1.13:11434  mistral:latest
  ✗  @pi4      ollama://192.168.1.14:11434  (unreachable)
  ✓  @claude   anthropic://                   claude-3-haiku
  ?  @gemini   gemini://                      (API key not set)
```

**Status indicators:**
- `✓` (green): Endpoint is reachable
- `✗` (yellow): Endpoint is unreachable
- `?` (yellow): API key not configured (cloud providers)

### `/route on`

Enable routing. State is persisted to `agents/routes.json`.

### `/route off`

Disable routing. @mentions will be rejected. State is persisted to `agents/routes.json`.

### `/route status`

Show routing state and endpoint count.

**Output:**
```
Routing: on
Endpoints: 5 registered
Active endpoint: none (routing uses @mention)
```

## Usage Patterns

### Pattern 1: Pi Cluster Workload Distribution

```
# Register cluster nodes
harvey> /route add pi2 ollama://192.168.1.12:11434 llama3.1:8b
harvey> /route add pi3 ollama://192.168.1.13:11434 mistral:latest
harvey> /route add pi4 ollama://192.168.1.14:11434 phi3:latest

# Enable routing
harvey> /route on

# Distribute work
harvey> @pi2 write unit tests for the parser package
harvey> @pi3 review the API design document
harvey> @pi4 generate documentation
```

### Pattern 2: Local + Cloud Hybrid

```
# Use local model for most work
harvey> Explain this Go code

# Switch to cloud for complex tasks
harvey> @claude This is a complex architecture decision, give me options
```

### Pattern 3: Model Comparison

```
# Same prompt to multiple models
harvey> @llama3 How would you refactor this?
harvey> @mistral How would you refactor this?
harvey> @claude How would you refactor this?

# Compare responses in the same conversation
```

## Context Window Behavior

When you dispatch a prompt to a remote endpoint via @mention, Harvey sends:

1. **Your prompt** (with @mention removed)
2. **Last 10 non-system messages** from local history
3. **System prompt** from HARVEY.md

**Why 10 messages?** This gives the remote model enough context without
sending the entire conversation (which may exceed the remote model's context
window or waste tokens).

**Example:**
```
Local history:
  [system] HARVEY.md content
  [user] Read the parser code
  [assistant] Here is the parser code...
  [user] The ParseExpr function has a bug
  [assistant] Yes, it doesn't handle empty input...
  [user] @claude How should we fix this?

Sent to CLAUDE:
  [system] HARVEY.md content
  [user] Read the parser code
  [assistant] Here is the parser code...
  [user] The ParseExpr function has a bug
  [assistant] Yes, it doesn't handle empty input...
  [user] How should we fix this?
```

The `@claude` prefix is removed from the sent prompt.

## Best Practices

### Naming Endpoints

| Good | Bad | Reason |
|------|-----|--------|
| `pi2`, `pi3`, `pi4` | `node2`, `server2` | Consistent with role |
| `claude`, `mistral` | `model1`, `api1` | Descriptive of model |
| `gpu-box`, `big-iron` | `machine`, `computer` | Descriptive of hardware |
| `dev`, `prod`, `test` | `d`, `p`, `t` | Clear purpose |

**Recommendations:**
- Use **lowercase** names (Harvey normalizes to lowercase internally)
- Use **hyphens** for multi-word: `gpu-box`, `code-review`
- Avoid spaces and special characters
- Keep names **short** (used frequently in @mentions)

### Organizing Endpoints

**By hardware:**
```
/route add pi2 ollama://192.168.1.12:11434
/route add pi3 ollama://192.168.1.13:11434
/route add pi4 ollama://192.168.1.14:11434
```

**By capability:**
```
/route add coder ollama://192.168.1.100:11434 codellama:13b
/route add reasoner ollama://192.168.1.100:11434 llama3:70b
/route add embedder ollama://192.168.1.100:11434 nomic-embed-text
```

**By project:**
```
/route add harvey ollama://localhost:11434 llama3.1:8b
/route add mable ollama://localhost:11434 mistral:latest
```

### Model Selection

**For coding tasks:**
- Local: `codellama:13b`, `llama3:latest`
- Cloud: `@claude` (Claude 3 Sonnet/Haiku)

**For reasoning/complex tasks:**
- Local: `llama3:70b` (if you have the VRAM)
- Cloud: `@claude` (Claude 3 Opus)

**For embedding:**
- Local: `nomic-embed-text`, `mxbai-embed-large`

**For small/quick tasks:**
- Local: `llama3:8b`, `phi3:latest`
- Cloud: `@mistral` (Mistral Small)

## Troubleshooting

### "Unknown endpoint: @name"

**Cause:** The endpoint hasn't been registered.

**Fix:**
```
/route add name URL [MODEL]
```

### "Routing is disabled"

**Cause:** Routing has been turned off.

**Fix:**
```
/route on
```

### "Endpoint @name is not reachable"

**Causes:**
- Ollama server not running
- Wrong IP/port
- Firewall blocking connection
- Ollama not installed on remote machine

**Fix:**
```bash
# Check if Ollama is running on the remote
ssh pi2 ollama --version

# Start Ollama on the remote
ssh pi2 ollama serve

# Verify connectivity
curl http://192.168.1.12:11434/api/tags
```

### "API key not configured for @claude"

**Cause:** The required environment variable isn't set.

**Fix:**
```bash
# For current session
export ANTHROPIC_API_KEY=sk-...
harvey

# Permanently (add to ~/.bashrc or ~/.zshrc)
echo 'export ANTHROPIC_API_KEY=sk-...' >> ~/.bashrc
source ~/.bashrc
```

### "Model not available on @pi2"

**Cause:** The model hasn't been pulled on the remote Ollama instance.

**Fix:**
```bash
# On the remote machine
ssh pi2 ollama pull llama3.1:8b
```

Or pull it directly through Harvey:
```
harvey> /ollama pull llama3.1:8b
```
(Note: This pulls on the local machine, not the remote)

## Advanced: Programmatic Access

Routes can be managed programmatically via the `RouteRegistry` type in Go code.

**Example (from Harvey source):**
```go
// Add a route programmatically
rr := agent.Routes
rr.Add(&RouteEndpoint{
    Name: "pi5",
    URL:  "ollama://192.168.1.15:11434",
    Model: "llama3.1:8b",
    Kind: KindOllama,
})

// Save to disk
SaveRouteConfig(rr)
```

## Security Considerations

### API Key Management

**Never commit API keys to version control.**

**Recommended approach:**
```bash
# Set in shell profile (not in repository)
echo 'export ANTHROPIC_API_KEY=...' >> ~/.bashrc

# Or use a secrets manager
eval "$(vault read -f json secret/harvey/anthropic)"
```

**Environment variable precedence:**
- Harvey checks environment variables at startup
- Changing them requires restarting Harvey
- Each cloud provider uses its own variable name

### Network Security

**For local network routes:**
- Use HTTPS if exposed to untrusted networks
- Consider firewall rules to restrict access
- Use SSH tunneling for remote access

**For cloud providers:**
- API keys have the same permissions as your account
- Monitor usage to detect anomalies
- Rotate keys periodically

## Complete Example Session

```
Title: Multi-Model Development Session
Credit: Recorded by Harvey
Author: RSDOIEL
Date: 2026-05-04 14:00:00
Draft date: 2026-05-04

FADE IN:

INT. HARVEY AND RSDOIEL TALKING 2026-05-04 14:00:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/project.

RSDOIEL
/route list

HARVEY
Endpoints registered:
  ✓  @pi2      ollama://192.168.1.12:11434  llama3.1:8b
  ✓  @pi3      ollama://192.168.1.13:11434  mistral:latest
  ✓  @claude   anthropic://                   claude-3-haiku

RSDOIEL
@pi2 write a unit test for the Parse function

HARVEY
Forwarding to PI2.

PI2
Here is a comprehensive test for Parse:

```go parser_test.go
func TestParse(t *testing.T) {
    // ... test code
}
```

RSDOIEL
/apply


INT. AGENT MODE 2026-05-04 14:05:00

HARVEY
Harvey proposes to write 1 file(s) to the workspace.

HARVEY
Write parser_test.go?

RSDOIEL
yes

[[write: parser_test.go — ok]]


INT. HARVEY AND RSDOIEL TALKING 2026-05-04 14:06:00

Harvey and RSDOIEL are in chat mode. Model: llama3:latest. Workspace: /home/user/project.

RSDOIEL
@claude review this test and suggest improvements

HARVEY
Forwarding to CLAUDE.

CLAUDE
The test is well-structured. Consider adding:
1. Edge case for empty input
2. Property-based testing
3. Benchmark

THE END.
```

*See also:*
- [CONFIGURATION.md](CONFIGURATION.md) — All configuration files
- [fountain-analysis skill](agents/skills/fountain-analysis/SKILL.md) — Analyze session files
- [ARCHITECTURE.md](ARCHITECTURE.md) — Technical deep dive on routing implementation
