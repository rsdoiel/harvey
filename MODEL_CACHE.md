
# Harvey Model Cache

*Version 1.0 — Complete guide to model capability caching in Harvey*

## Overview

Harvey's **Model Cache** is a SQLite-backed database that stores **capability metadata** for Ollama models. This caching system significantly speeds up Harvey's startup time by avoiding the need to re-probe every model on each launch.

### What It Solves

| Problem | Without Cache | With Cache |
|---------|---------------|------------|
| Slow startup with many models | Probes every model on each startup (5-10s per model) | Loads cached results instantly |
| Redundant network calls | Repeated /api/show requests to Ollama | Single probe per model, cached indefinitely |
| Inconsistent capability detection | Must re-check every time | Results persist until explicitly updated |

### Key Benefits

1. **Fast Startup** — Harvey starts instantly even with dozens of installed models
2. **Offline Operation** — Cached capability data available without Ollama running
3. **Consistent Behavior** — Model capabilities remain stable across sessions
4. **Automatic Management** — Cache is created and updated automatically
5. **Probe Levels** — Supports both fast (heuristic) and thorough (live test) probing

## Quick Start

The model cache works automatically — no configuration required:

```bash
# First run: probes all installed models and caches results
harvey

# Subsequent runs: loads from cache, much faster
harvey

# Force re-probe a specific model
harvey> /ollama probe llama3.2:latest
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                      MODEL CACHE ARCHITECTURE                         │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────┐  │
│  │   Ollama        │     │  Model Cache    │     │   Harvey    │  │
│  │   Server        │────▶│  (SQLite DB)    │────▶│   Startup   │  │
│  │                 │     │                 │     │             │  │
│  │ /api/show       │     │ model_cache.db │     │ Load cache  │  │
│  │ /api/embed      │     │                 │     │             │  │
│  └─────────────────┘     └─────────────────┘     └─────────────┘  │
│           │                       │                             │
│           │ Probe (fast/thorough)  │ Update cache               │
│           ▼                       ▼                             ▼
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │                    ModelCapability                          │  │
│  │  - name, family, parameter_size, quantization                 │  │
│  │  - size_bytes, context_length                                  │  │
│  │  - supports_tools, supports_embed (CapabilityStatus enum)    │  │
│  │  - probe_level ("none", "fast", "thorough")                   │  │
│  │  - probed_at (timestamp)                                        │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Data Flow

1. **Harvey Startup:**
   - Open model cache database (`agents/model_cache.db`)
   - Load all cached ModelCapability entries
   - Use cached data for model selection UI

2. **Model Probe (on demand):**
   - Call `FastProbeModel()` or `ThoroughProbeModel()`
   - Fetch model metadata from Ollama `/api/show`
   - For thorough probe: test `/api/embed` endpoint
   - Store results in cache with timestamp

3. **Cache Query:**
   - Lookup model by name
   - Return cached ModelCapability or nil if not found
   - Display capability status (✓, —, ?)

## Database Schema

### SQLite Schema

```sql
CREATE TABLE IF NOT EXISTS model_capabilities (
    name           TEXT PRIMARY KEY,
    family         TEXT    NOT NULL DEFAULT '',
    parameter_size TEXT    NOT NULL DEFAULT '',
    quantization   TEXT    NOT NULL DEFAULT '',
    size_bytes     INTEGER NOT NULL DEFAULT 0,
    context_length INTEGER NOT NULL DEFAULT 0,
    supports_tools INTEGER NOT NULL DEFAULT -1,
    supports_embed INTEGER NOT NULL DEFAULT -1,
    probe_level    TEXT    NOT NULL DEFAULT 'none',
    probed_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
```

### Table Columns

| Column | Type | Description |
|--------|------|-------------|
| `name` | TEXT (PK) | Full model identifier, e.g., "llama3.2:latest", "nomic-embed-text" |
| `family` | TEXT | Model family, e.g., "llama", "phi", "mistral", "nomic" |
| `parameter_size` | TEXT | Human-readable parameter count, e.g., "8.0B", "70B" |
| `quantization` | TEXT | Quantization level, e.g., "Q4_K_M", "Q8_0" |
| `size_bytes` | INTEGER | Size on disk in bytes |
| `context_length` | INTEGER | Context window in tokens; 0 = unknown |
| `supports_tools` | INTEGER | CapabilityStatus enum: -1=unknown, 0=no, 1=yes |
| `supports_embed` | INTEGER | CapabilityStatus enum: -1=unknown, 0=no, 1=yes |
| `probe_level` | TEXT | "none", "fast", or "thorough" |
| `probed_at` | DATETIME | When the last probe ran |

### Indexes

- **Primary Key:** `name` — fast lookup by model name
- **No secondary indexes** — table is small enough for full scans

## Go API Reference

### Types

#### `CapabilityStatus`

An enum representing whether a model capability is confirmed, denied, or unknown.

```go
type CapabilityStatus int

const (
    CapUnknown CapabilityStatus = -1  // Not yet probed
    CapNo      CapabilityStatus = 0   // Confirmed absent
    CapYes     CapabilityStatus = 1   // Confirmed present
)
```

**String Representation:**
- `CapUnknown` → "?"
- `CapNo` → "—"  
- `CapYes` → "✓"

#### `ModelCapability`

Holds all cached metadata for a single model.

```go
type ModelCapability struct {
    Name          string           // Full model identifier
    Family        string           // Model family
    ParameterSize string           // Human-readable size
    Quantization  string           // Quantization level
    SizeBytes     int64            // Bytes on disk
    ContextLength int              // Context window in tokens
    SupportsTools CapabilityStatus // Tool/function calling support
    SupportsEmbed CapabilityStatus // Embedding support
    ProbeLevel    string           // "none", "fast", or "thorough"
    ProbedAt      time.Time        // When last probed
}
```

#### `ModelCache`

The main handle for the model cache database.

```go
type ModelCache struct {
    db   *sql.DB
    path string
}

// OpenModelCache opens (or creates) the model cache database.
// customPath overrides the default location (agents/model_cache.db).
func OpenModelCache(ws *Workspace, customPath string) (*ModelCache, error)

// Path returns the absolute path of the cache file.
func (mc *ModelCache) Path() string

// Close releases the database connection.
func (mc *ModelCache) Close() error
```

### Methods

#### `Get(name string) (*ModelCapability, error)`

Returns the cached capability for the named model, or nil if not found.

```go
cap, err := mc.Get("llama3.2:latest")
if cap == nil {
    // Model not in cache
}
if cap.SupportsTools == CapYes {
    fmt.Println("Model supports tools")
}
```

**Parameters:**
- `name` — Full model identifier (e.g., "llama3.2:latest")

**Returns:**
- `*ModelCapability` — cached entry; nil if not found
- `error` — non-nil on database error (not on missing row)

#### `Set(cap *ModelCapability) error`

Upserts a ModelCapability into the cache. Existing entries are completely replaced.

```go
cap := &ModelCapability{
    Name:          "llama3.2:latest",
    Family:        "llama",
    SupportsTools: CapYes,
    SupportsEmbed: CapNo,
    ProbeLevel:    "fast",
    ProbedAt:      time.Now(),
}
err := mc.Set(cap)
```

**Parameters:**
- `cap` — Capability record to store

**Returns:**
- `error` — non-nil on database write failure

#### `Delete(name string) error`

Removes the cache entry for the named model.

```go
err := mc.Delete("old-model:tag")
```

**Parameters:**
- `name` — Full model name

**Returns:**
- `error` — non-nil on database write failure

#### `All() ([]ModelCapability, error)`

Returns all cached model capabilities, ordered by name.

```go
allCaps, err := mc.All()
for _, cap := range allCaps {
    fmt.Printf("%s (%s): tools=%s embed=%s\n",
        cap.Name, cap.Family, cap.SupportsTools, cap.SupportsEmbed)
}
```

**Returns:**
- `[]ModelCapability` — all cached entries; empty slice if cache is empty
- `error` — non-nil on database read failure

## Capability Probing

Harvey uses two levels of capability probing, implemented in `ollama.go`:

### Fast Probe (`FastProbeModel`)

Uses heuristics to determine capabilities from the model's `/api/show` response:

**Tool Support Detection:**
1. Checks the `Capabilities` array from `/api/show` (authoritative on Ollama ≥ 0.3)
2. Falls back to checking for known tool-call template markers:
   - `{% if tools %}` (Llama 3, Granite - Jinja2)
   - `[TOOL_CALLS]`, `[AVAILABLE_TOOLS]` (Mistral, Ministral)
   - `<tool_call>`, `✿FUNCTION✿` (Qwen 2.x variants)
   - `<function_calls>` (Gemma 4 and others)

**Embedding Support Detection:**
- Checks if model name contains known embedding-model keywords:
  - `embed`, `e5-`, `bge-`, `gte-`, `minilm`, `nomic`, `mxbai`, `jina`

**Advantages:**
- Single API call (/api/show)
- Fast execution
- No embedding model test required

**Limitations:**
- Embedding detection is heuristic-based
- May produce false positives for models with embedding keywords in name

### Thorough Probe (`ThoroughProbeModel`)

First runs FastProbeModel, then makes a live `/api/embed` request to confirm embedding support:

1. Calls FastProbeModel for initial capability detection
2. Sends a test embedding request to `/api/embed`
3. If successful with non-empty embeddings array → `SupportsEmbed = CapYes`
4. If any error or empty response → `SupportsEmbed = CapNo`

**Advantages:**
- Definitive embedding support confirmation
- Accurate results

**Limitations:**
- Requires embedding model to be loaded in Ollama
- Slower (additional API call)
- Still uses heuristics for tool support

### Probe Levels Summary

| Probe Level | Tool Detection | Embed Detection | Speed | API Calls |
|-------------|---------------|-----------------|-------|-----------|
| none | Not probed | Not probed | Instant | 0 |
| fast | Heuristic + Capabilities | Keyword-based | Fast | 1 |
| thorough | Heuristic + Capabilities | Live test | Slow | 2 |

## Usage Patterns

### Automatic Probing on Startup

Harvey automatically probes models when needed:

```go
// In harvey initialization
if model, ok := knownModels[name]; !ok {
    cap, err := FastProbeModel(ctx, ollamaURL, name)
    if err == nil {
        cache.Set(cap)
    }
}
```

### Manual Probing

```bash
# Probe a specific model
harvey> /ollama probe llama3.2:latest

# Probe all installed models
harvey> /ollama probe --all
```

### Checking Model Capabilities

```bash
# List all models with their capabilities
harvey> /ollama list

# The output shows:
# - Model name and family
# - Parameter size and quantization
# - Context length
# - Tool support (✓, —, ?)
# - Embedding support (✓, —, ?)
```

### Programmatic Access

```go
// Open the cache
mc, err := OpenModelCache(ws, "")
defer mc.Close()

// Get a specific model's capabilities
cap, err := mc.Get("llama3.2:latest")
if cap != nil {
    fmt.Printf("Tools: %s, Embed: %s\n", cap.SupportsTools, cap.SupportsEmbed)
}

// Iterate all cached models
all, _ := mc.All()
for _, c := range all {
    if c.SupportsEmbed == CapYes {
        fmt.Println(c.Name, "supports embeddings")
    }
}
```

## Configuration

### Database Location

**Default:** `agents/model_cache.db` in the workspace root

**Custom Path:**
```go
mc, err := OpenModelCache(ws, "custom/path/model_cache.db")
```

**YAML Configuration:**
```yaml
# In harvey.yaml
model_cache:
  path: custom/path/model_cache.db
```

### SQLite Settings

The database is configured with:
- **Journal Mode:** WAL (Write-Ahead Logging)
- **Foreign Keys:** ON
- **Max Connections:** 1 (prevents locking issues)

## Best Practices

### When to Use Each Probe Level

1. **Fast Probe (Default):**
   - Use for initial model discovery
   - Use when embedding model is known from name
   - Use for quick capability checks

2. **Thorough Probe:**
   - Use when embedding capability is uncertain
   - Use before relying on embedding functionality
   - Use when fast probe returns unknown for embedding

### Cache Management

1. **Let Harvey manage the cache:**
   - Cache is automatically created on first use
   - Probing happens automatically when needed

2. **Clear cache when needed:**
   - If Ollama is updated, consider clearing the cache
   - If model definitions change, re-probe specific models

3. **Backup the cache:**
   - `agents/model_cache.db` contains valuable metadata
   - Backup before major Ollama upgrades

### Model Name Format

- Use **full model identifiers** including tags:
  - ✅ `llama3.2:latest`
  - ✅ `nomic-embed-text:latest`
  - ✅ `granite-code:3b`
  - ❌ `llama3.2` (missing tag)
  - ❌ `nomic-embed-text` (missing tag)

- Tags matter: different tags may have different capabilities

## Troubleshooting

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| Model not found in cache | Never probed or deleted | Run `/ollama probe MODEL` |
| Outdated cache entries | Model updated in Ollama | Re-probe the model or delete and re-probe |
| Database locked | Multiple connections | Harvey uses MaxOpenConns(1) to prevent this |
| "None" probe level | Model never probed | Run a probe to populate |
| Incorrect tool support | Heuristic detection failed | Use thorough probe or manually verify |
| Incorrect embed support | Keyword detection failed | Use thorough probe for definitive answer |

### Force Re-probe

To force a fresh probe of a model:

```go
// Delete the old entry
mc.Delete("model:tag")

// Run a new probe
cap, err := FastProbeModel(ctx, ollamaURL, "model:tag")
if err == nil {
    mc.Set(cap)
}
```

### Database Corruption

If the database file is corrupted:

```bash
# Remove the corrupted file
rm agents/model_cache.db

# Harvey will create a new one on next startup
# All capabilities will be re-probed
```

### Verify Cache Contents

```bash
# Check the database directly
sqlite3 agents/model_cache.db "SELECT name, probe_level, supports_tools, supports_embed FROM model_capabilities"

# Count entries
sqlite3 agents/model_cache.db "SELECT COUNT(*) FROM model_capabilities"
```

## Performance

### Cache Size

- Each entry: ~200-500 bytes
- 100 models: ~20-50KB
- 1000 models: ~200-500KB
- Memory: Only loaded entries are in memory

### Query Performance

- Lookup by name: O(log n) via primary key index
- Full scan (All()): O(n) but fast for typical model counts
- Typical lookup: < 1ms
- Typical full scan: < 10ms for 100 models

### Probe Performance

| Probe Type | Time | Network Calls |
|------------|------|---------------|
| Fast | 50-200ms | 1 (/api/show) |
| Thorough | 1-5s | 2 (/api/show + /api/embed) |

## Migration Guide

### From No Cache (v0.1 and earlier)

Harvey v0.2+ includes automatic model caching:

1. Start Harvey — cache is created automatically
2. Models are probed as they're discovered
3. Cache grows as you use more models

### From Old Cache Format

The cache schema is automatically migrated on open:
- Missing columns are added
- Indexes are created
- Existing data is preserved

No manual migration needed.

## Related Files

| File | Description |
|------|-------------|
| `harvey/model_cache.go` | Core cache implementation |
| `harvey/ollama.go` | Probing functions (FastProbeModel, ThoroughProbeModel) |
| `agents/model_cache.db` | Default cache database location |
| `harvey.yaml` | Configuration for cache path |

## See Also

- [CONFIGURATION.md](CONFIGURATION.md) — Configuration file reference
- [OLLAMA.md](OLLAMA.md) — Ollama integration guide (if exists)
- [ARCHITECTURE.md](ARCHITECTURE.md) — Technical architecture
- [RAG_Support_Design.md](RAG_Support_Design.md) — RAG design document with embedding model info

*Documentation generated from model_cache.go and ollama.go source code. Version 1.0.*
