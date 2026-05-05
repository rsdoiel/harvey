
# Using RAGs with Harvey

## Overview

**Retrieval-Augmented Generation (RAG)** enhances Harvey's capabilities by grounding model responses in your own documents, code, and notes. Instead of relying solely on the model's training data, RAG allows Harvey to retrieve relevant snippets from a local knowledge store and inject them into the conversation context.

### What RAG Solves

| Problem | Without RAG | With RAG |
|---------|-------------|---------|
| Model doesn't know about your codebase | Manual `/read` of files, limited context window | Automatic retrieval of relevant code |
| Model hallucinates facts | No grounding in your documents | Responses grounded in your ingested docs |
| Context window too small | Must manually select what to include | Only most relevant chunks are injected |
| Outdated model knowledge | Can't access information post-training | Your current documents are always available |
| Domain-specific knowledge | Generic responses | Domain-expert responses |

### Key Benefits

1. **Reduced Hallucination** — Model answers are grounded in your actual documents
2. **Larger Effective Context** — Access more content than fits in the model's context window
3. **Project-Specific Knowledge** — Each project can have its own focused knowledge base
4. **Offline Operation** — Entirely local; no cloud dependencies once documents are ingested
5. **Transparent** — You can inspect exactly what context is being injected

## Quick Start

### Prerequisites

- Harvey installed and working
- Ollama running with at least one LLM model pulled (e.g., `llama3.1:8b`)
- At least one embedding model installed (recommended: `nomic-embed-text`)

```bash
# Install an embedding model (one-time)
ollama pull nomic-embed-text
```

### 5-Minute RAG Setup

```bash
# Start Harvey
harvey

# Step 1: Create a named RAG store
harvey> /rag new myproject
  Embedding models installed:
    [1] nomic-embed-text (recommended)
    [2] mxbai-embed-large
  Select embedding model [1]: 1
  Proposed generation → embedding model mapping:
    llama3.1:8b → nomic-embed-text
  Create store 'myproject' at agents/rag/myproject.db? [y/N]: y
  Created RAG store 'myproject' with embedding model 'nomic-embed-text'
  Active store: myproject

# Step 2: Ingest your documents
harvey> /rag ingest README.md
  Ingesting README.md...
  Split into 8 chunks, embedding and storing...
  ✓ Ingested README.md (8 chunks)

harvey> /rag ingest docs/
  Ingesting docs/...
  ✓ Ingested docs/guide.md (12 chunks)
  ✓ Ingested docs/api.md (5 chunks)

# Step 3: Verify retrieval quality
harvey> /rag query "how do I configure Harvey"
  Top 5 result(s) for "how do I configure Harvey":
    [1] score=0.872  source=docs/guide.md
        Configuration is done through harvey.yaml and HARVEY.md...
    [2] score=0.741  source=README.md
        See docs/guide.md for configuration options...

# Step 4: RAG is now active - ask questions!
harvey> How do I set up a RAG store?
  [RAG context injected: 3 chunks from myproject]
  HARVEY
  Forwarding to LLAMA3.1.
  
  LLAMA3.1
  To set up a RAG store, use the `/rag new NAME` command...
```

## Concepts

### Architecture Overview

```
┌───────────────────────────────────────────────────────────────────────┐
│                      HARVEY RAG ARCHITECTURE                          │
├───────────────────────────────────────────────────────────────────────┤
│                                                                       │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐  │
│  │  Your Documents │────▶│   RAG Store     │────▶│   LLM Model     │  │
│  │   (text files)  │     │  (SQLite + DB)  │     │ (generation)    │  │
│  └─────────────────┘     └─────────────────┘     └─────────────────┘  │
|         |                       ▲                       |             |
│         │ Ingest                │ Query                 │ Generate    │
│         │ (embedding)           │ (similarity)          │ (response)  │
│         ▼                       |                       ▼             │
│  ┌─────────────────┐     ┌─────────────────┐                          │
│  │  Chunking       │     │  Vector Search  │                          │
│  │  (~500 chars)   │     │  (cosine sim)   │                          │
│  └─────────────────┘     └─────────────────┘                          │
│         │                       ▲                                     │
│         └─ Embedding Model ─────┘                                     |
│                                                                       │
└───────────────────────────────────────────────────────────────────────┘
```

### Key Components

#### 1. Embedding Model

A small neural network that converts text to **vector embeddings** — numerical representations that capture semantic meaning. Unlike the generation model (which produces text), the embedding model understands what text *means*.

**Recommended Embedding Models:**

| Model | Size | Quality | Best For |
|-------|------|---------|----------|
| `nomic-embed-text:v1.5` | ~274 MB | ⭐⭐⭐⭐⭐ | General-purpose retrieval |
| `mxbai-embed-large:335m` | ~670 MB | ⭐⭐⭐⭐ | High-quality, larger |
| `qllama/bge-small-en-v1.5:latest` | ~46 MB | ⭐⭐⭐ | Small but retrieval-optimized |
| `bge-m3:567m` | ~1.2 GB | ⭐⭐⭐⭐ | Multilingual |

**Why model choice matters:**

- Models like `all-MiniLM-L6-v2` were trained on **sentence-similarity** tasks (NLI, STS)
- Retrieval-optimized models like `nomic-embed-text` were trained on **document retrieval** tasks
- On the MTEB benchmark: `all-MiniLM-L6-v2` scores ~56%, `nomic-embed-text` scores ~68%

```bash
# Install recommended embedding model
ollama pull nomic-embed-text:v1.5

# List installed models
ollama list
```

#### 2. RAG Store

An **SQLite database** that stores:
- Text chunks (the actual content)
- Vector embeddings (the numerical representations)
- Source metadata (which file each chunk came from)

**Store Characteristics:**
- **Embedding-model-scoped:** Each store is bound to one embedding model
- **Isolated:** Stores don't share data; switch between them as needed
- **Persistent:** Configuration saved in `agents/harvey.yaml`
- **Memory-efficient:** Only the active store is kept open

#### 3. Chunks

Documents are split into **paragraph-sized chunks** (~500 characters each) before embedding. This ensures:
- Embeddings represent coherent units of meaning
- Individual chunks fit within embedding model context limits
- Retrieval can find the most relevant passage, not just the most relevant document

#### 4. Vector Similarity Search

When you ask a question:
1. Your query is embedded using the same embedding model
2. **Cosine similarity** is computed between your query vector and every chunk vector
3. Top-K most similar chunks (score > 0.3 threshold) are injected into context
4. The model generates a response grounded in those chunks

**Cosine Similarity:** Measures the angle between two vectors in multi-dimensional space. A score of 1.0 means identical, 0.0 means unrelated, -1.0 means opposite.

## Named Stores

### Why Multiple Stores?

Harvey supports **multiple named RAG stores** for these reasons:

| Scenario | Single Store | Multiple Stores |
|----------|--------------|-----------------|
| Different projects | All docs mixed together | Separate stores per project |
| Different domains | One big knowledge base | `golang`, `writing`, `research` |
| Different embedding models | Limited to one model | Each store can use a different model |
| Resource-constrained hardware | Large, bloated store | Small, focused stores fit in RAM |
| Team collaboration | Shared everything | Share only relevant stores |

### Store Management Commands

| Command | Description |
|---------|-------------|
| `/rag list` | List all registered stores with active marker |
| `/rag new NAME` | Interactive wizard to create a named store |
| `/rag switch NAME` | Activate a different store |
| `/rag drop NAME` | Remove a store from registry (doesn't delete .db file) |
| `/rag status` | Show active store details and all registered stores |

### Example: Multiple Project Stores

```bash
# Create stores for different projects
harvey> /rag new harvey-docs
  ... (select nomic-embed-text) ...
  Created RAG store 'harvey-docs'

harvey> /rag new my-novel
  ... (select nomic-embed-text) ...
  Created RAG store 'my-novel'

# Ingest project-specific documents
harvey> /rag switch harvey-docs
harvey> /rag ingest harvey/
harvey> /rag ingest HARVEY.md

harvey> /rag switch my-novel
harvey> /rag ingest ~/writing/notes/
harvey> /rag ingest ~/projects/novel/drafts/

# Switch between stores as you work
harvey> /rag switch harvey-docs
harvey> /rag query "how does RAG work"

harvey> /rag switch my-novel
harvey> /rag query "describe the protagonist"
```

### Store Configuration

Stores are configured in `agents/harvey.yaml`:

```yaml
rag:
  enabled: true
  active: harvey-docs
  stores:
    - name: harvey-docs
      db_path: agents/rag/harvey-docs.db
      embedding_model: nomic-embed-text
      model_map:
        llama3.1:8b: nomic-embed-text
        granite3.3:2b: nomic-embed-text
    - name: my-novel
      db_path: agents/rag/my-novel.db
      embedding_model: nomic-embed-text
      model_map:
        llama3.1:8b: nomic-embed-text
```

**Fields:**
- `name` — Short identifier for the store
- `db_path` — Path to the SQLite database (relative to workspace root)
- `embedding_model` — Name of the embedding model bound to this store
- `model_map` — Generation model → embedding model overrides (usually same for all)

## Setup

### Step 1: Install an Embedding Model

```bash
# Recommended: nomic-embed-text (best retrieval quality)
ollama pull nomic-embed-text

# Alternative: mxbai-embed-large (higher quality, larger)
ollama pull mxbai-embed-large

# Budget option: bge-small-en-v1.5 (small but good)
ollama pull bge-small-en-v1.5

# Verify installation
ollama list
# Should show your embedding model alongside your generation models
```

### Step 2: Create a RAG Store

```bash
# Interactive wizard
harvey> /rag new my-store

# The wizard will:
# 1. Detect installed embedding models
# 2. Show the best available options
# 3. Propose a generation → embedding model mapping
# 4. Ask for confirmation

# Example output:
#   Embedding models installed:
#     [1] nomic-embed-text (recommended)
#     [2] mxbai-embed-large
#   Select embedding model [1]: 1
#   Proposed generation → embedding model mapping:
#     llama3.1:8b → nomic-embed-text
#   Create store 'my-store' at agents/rag/my-store.db? [y/N]: y
#   Created RAG store 'my-store' with embedding model 'nomic-embed-text'
#   Active store: my-store
```

**Store Location:**
- Default: `agents/rag/{name}.db`
- Can be customized: `/rag new my-store --path custom/path/my-store.db`

### Step 3: Ingest Documents

```bash
# Ingest single files
harvey> /rag ingest README.md
harvey> /rag ingest LICENSE

# Ingest directories (recursive)
harvey> /rag ingest docs/
harvey> /rag ingest src/

# Ingest multiple paths at once
harvey> /rag ingest README.md docs/ src/

# Ingest from absolute paths
harvey> /rag ingest ~/projects/myproject/docs/
```

**Supported File Types:**

- `.md` (Markdown)
- `.txt` (Plain text)
- `.go` (Go source)
- `.ts` (TypeScript)
- `.js` (JavaScript)
- `.py` (Python)
- `.json` (JSON)
- Any plain-text file

**Chunking Behavior:**

- Files are split into ~500-character chunks
- Each chunk is embedded separately
- Source file path is preserved for each chunk

### Step 4: Verify Retrieval

```bash
# Test retrieval with a query
harvey> /rag query "what is the license"

# Example output:
#   Top 5 result(s) for "what is the license":
#     [1] score=0.872  source=LICENSE
#         GNU AFFERO GENERAL PUBLIC LICENSE Version 3...
#     [2] score=0.741  source=README.md
#         Harvey is licensed under AGPL-3.0...

# If scores are low (< 0.3):
# - The query may not match any ingested content
# - Try rephrasing the query
# - Ingest more relevant documents
```

### Step 5: Enable RAG (Automatic)

RAG is **enabled automatically** when you create or switch to a store. To manually control:

```bash
# Enable RAG for current session
harvey> /rag on

# Disable RAG for current session (database preserved)
harvey> /rag off

# Check status
harvey> /rag status
#   RAG: enabled
#   Active store: my-store (agents/rag/my-store.db)
#   Embedding model: nomic-embed-text
#   Chunk count: 42
#   
#   Registered stores:
#     * my-store — agents/rag/my-store.db (nomic-embed-text) — 42 chunks
#       old-store — agents/rag/old-store.db (nomic-embed-text) — 156 chunks
```

## Usage Patterns

### Pattern 1: Project Documentation RAG

Keep your project's documentation searchable:

```bash
# Setup
harvey> /rag new myproject-docs
harvey> /rag ingest README.md CONTRIBUTING.md docs/

# Now ask about your project
harvey> How do I build this project?
# [RAG context injected: 3 chunks from myproject-docs]

# Add new documentation as it's created
harvey> /rag ingest new-feature-guide.md
```

### Pattern 2: Codebase RAG

Make your entire codebase searchable:

```bash
# Ingest all source code
harvey> /rag new codebase
harvey> /rag ingest src/ internal/ pkg/

# Ask about code structure
harvey> How does the authentication system work?

# Find specific functions or patterns
harvey> /rag query "func.*HandleRequest"
```

### Pattern 3: Research RAG

Build a knowledge base for a research project:

```bash
# Collect research materials
harvey> /rag new research-llm-security
harvey> /rag ingest ~/Downloads/paper1.pdf.txt
# Note: Use Firefox "Save Page as... > Text Files" for web pages
harvey> /rag ingest ~/research/notes/

# Query your research
harvey> What are the key findings about prompt injection?
```

### Pattern 4: Multi-Project Workflow

Switch between different knowledge domains:

```bash
# Monday: Working on Harvey
harvey> /rag switch harvey-docs

# Tuesday: Writing a novel
harvey> /rag switch my-novel

# Wednesday: Research
harvey> /rag switch research-llm-security

# Quick switch back
harvey> /rag switch harvey-docs
```

### Pattern 5: Session-Specific RAG

Enable RAG only for specific sessions:

```bash
# Start with RAG off
harvey --no-rag

# Enable when needed
harvey> /rag on

# Disable when done
harvey> /rag off
```

## Commands Reference

### `/rag list`

List all registered RAG stores.

```bash
harvey> /rag list
  RAG Stores:
    * my-store    — agents/rag/my-store.db    (nomic-embed-text, 42 chunks)
      old-store   — agents/rag/old-store.db   (nomic-embed-text, 156 chunks)
      research    — agents/rag/research.db    (nomic-embed-text, 89 chunks)

  * = active store
```

### `/rag new NAME`

Create a new named RAG store with interactive setup.

```bash
harvey> /rag new research
  Embedding models installed:
    [1] nomic-embed-text (recommended)
    [2] mxbai-embed-large
    [3] bge-small-en-v1.5
  Select embedding model [1]: 1
  
  Proposed generation → embedding model mapping:
    llama3.1:8b → nomic-embed-text
    granite3.3:2b → nomic-embed-text
  
  Create store 'research' at agents/rag/research.db? [y/N]: y
  Created RAG store 'research' with embedding model 'nomic-embed-text'
  Active store: research
```

**Options:**
- `--path PATH` — Custom database path (default: `agents/rag/{NAME}.db`)
- `--embedding-model MODEL` — Skip selection, use specific model

### `/rag switch NAME`

Activate a different RAG store.

```bash
harvey> /rag switch research
  Switched to RAG store 'research'
  Embedding model: nomic-embed-text
  Chunk count: 89

harvey> /rag query "latest findings"
```

### `/rag drop NAME`

Remove a store from the registry (does NOT delete the .db file).

```bash
harvey> /rag drop old-store
  Remove RAG store 'old-store'? [y/N]: y
  Removed store 'old-store' from registry.
  Database file still exists at: agents/rag/old-store.db
  
  # To delete the file manually:
  rm agents/rag/old-store.db
```

### `/rag setup`

Backward-compatible alias for `/rag new`. If no store is active, creates a store named "default".

```bash
harvey> /rag setup
  No active store. Create 'default' store? [y/N]: y
  ... (same as /rag new default) ...
```

### `/rag ingest PATH [PATH...]`

Ingest files or directories into the active store.

```bash
# Ingest single file
harvey> /rag ingest README.md
  Ingesting README.md...
  Split into 8 chunks
  ✓ Ingested README.md (8 chunks)

# Ingest directory (recursive)
harvey> /rag ingest docs/
  Ingesting docs/
  ✓ Ingested docs/guide.md (12 chunks)
  ✓ Ingested docs/api.md (5 chunks)
  ✓ Ingested docs/examples/test.go (3 chunks)

# Ingest multiple paths
harvey> /rag ingest README.md docs/ src/

# Ingest with progress
harvey> /rag ingest large-directory/
  Ingesting large-directory/...
  Processing file 1/156: large-directory/file1.md
  Processing file 2/156: large-directory/file2.md
  ...
```

**Ingestion Process:**

1. Walk the file/directory tree
2. Filter for supported file types
3. Read file content
4. Split into ~500-character chunks
5. Generate embeddings for each chunk
6. Store chunks + embeddings + source metadata in SQLite

**Supported Formats:** All text-based files. Binary files are skipped.

### `/rag query TEXT`

Query the active store and show top matching chunks.

```bash
harvey> /rag query "how does the RAG system work"
  Top 5 result(s) for "how does the RAG system work":
    
    [1] score=0.872  source=harvey/RAG_Support_Design.md
        RAG augments each user prompt with relevant chunks retrieved...
    
    [2] score=0.789  source=harvey/Using_RAGs_with_Harvey.md
        RAG lets Harvey find relevant snippets from a local knowledge...
    
    [3] score=0.741  source=harvey/ARCHITECTURE.html
        The RAG subsystem provides retrieval-augmented generation...
    
    [4] score=0.653  source=harvey/helptext.go
        RAG lets Harvey find relevant snippets from a local knowledge store...
    
    [5] score=0.432  source=harvey/README.md
        Harvey supports Retrieval-Augmented Generation (RAG)...

  Threshold: 0.3 (chunks below this score are not injected)
```

**Use Cases:**
- Verify retrieval quality before trusting answers
- Debug why RAG isn't finding what you expect
- Explore what's in your store
- Test different query phrasings

### `/rag status`

Show RAG configuration and store statistics.

```bash
harvey> /rag status
  RAG: enabled
  Auto-inject: on
  Active store: my-store
  Database: agents/rag/my-store.db
  Embedding model: nomic-embed-text
  Chunk count: 42
  
  Registered stores:
    * my-store    — agents/rag/my-store.db    (nomic-embed-text) — 42 chunks
      old-store   — agents/rag/old-store.db   (nomic-embed-text) — 156 chunks
      research    — agents/rag/research.db    (nomic-embed-text) — 89 chunks
```

### `/rag on`

Enable automatic RAG context injection for the current session.

```bash
harvey> /rag on
  RAG auto-injection enabled
  Active store: my-store (42 chunks)
```

### `/rag off`

Disable automatic RAG context injection (store remains configured).

```bash
harvey> /rag off
  RAG auto-injection disabled
  Use /rag on to re-enable
```

## Configuration

### YAML Configuration

RAG configuration is stored in `agents/harvey.yaml`:

```yaml
# agents/harvey.yaml
rag:
  enabled: true              # Global RAG enable/disable
  active: my-store           # Currently active store name
  stores:                   # All registered stores
    - name: my-store
      db_path: agents/rag/my-store.db
      embedding_model: nomic-embed-text
      model_map:
        llama3.1:8b: nomic-embed-text
        granite3.3:2b: nomic-embed-text
    - name: research
      db_path: agents/rag/research.db
      embedding_model: nomic-embed-text
      model_map:
        llama3.1:8b: nomic-embed-text
```

### Configuration Fields

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `enabled` | bool | Global RAG enable/disable | `false` |
| `active` | string | Name of active store | `""` |
| `stores` | list | List of store configurations | `[]` |
| `stores[].name` | string | Store identifier | required |
| `stores[].db_path` | string | SQLite database path | required |
| `stores[].embedding_model` | string | Embedding model name | required |
| `stores[].model_map` | map | Generation → embedding model mapping | auto-populated |
| `stores[].embedder_kind` | string | "ollama" or "encoderfile" | "ollama" |
| `stores[].embedder_url` | string | Custom embedder URL | `""` |

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `OLLAMA_HOST` | Ollama server URL | `http://localhost:11434` |
| `OLLAMA_ORIGIN` | Ollama origin header | Harvey |

### Command-Line Options

| Flag | Description | Default |
|------|-------------|---------|
| `--rag` | Enable RAG on startup | Disabled |
| `--no-rag` | Disable RAG on startup | Enabled if configured |

## Advanced Topics

### Chunking Strategy

**Why chunking matters:**
- Embedding models have context length limits (typically 8192 tokens)
- Shorter chunks = more precise retrieval
- Longer chunks = more context per retrieval
- Harvey uses ~500 characters as a balance

**Chunking Behavior:**
```
File: README.md (10,000 characters)
  ↓ Split by paragraphs/blank lines
  Chunk 1: 0-500 chars
  Chunk 2: 500-1000 chars
  ...
  Chunk 20: 9500-10000 chars

Each chunk is embedded separately with its source file path.
```

### Vector Similarity

**Cosine Similarity Formula:**
```
similarity(a, b) = (a · b) / (||a|| × ||b||)
```

Where:
- `a · b` = dot product of vectors a and b
- `||a||` = magnitude (Euclidean norm) of vector a
- `||b||` = magnitude of vector b

**Score Interpretation:**
| Score Range | Interpretation |
|------------|----------------|
| 0.8-1.0 | Very similar / nearly identical meaning |
| 0.6-0.8 | Similar / related concepts |
| 0.4-0.6 | Somewhat related |
| 0.3-0.4 | Weakly related |
| < 0.3 | Unrelated (filtered out by default) |

### Threshold Tuning

The **relevance threshold** (0.3 by default) determines which chunks are injected:

```bash
# Currently not configurable via CLI (hardcoded at 0.3)
# Can be changed in code: ragMinScore in harvey.go
```

**When to adjust the threshold:**
- **Increase (0.4-0.5):** If RAG is injecting too much irrelevant context
- **Decrease (0.2-0.25):** If RAG is missing relevant chunks
- **Default (0.3):** Good balance for most use cases

### Top-K Selection

Harvey injects the **top-K most similar chunks** that exceed the threshold:

```bash
# Currently hardcoded to inject up to 5 chunks
# Can be changed in code: ragTopK in harvey.go
```

**Context Window Considerations:**
- Each injected chunk consumes ~500 characters of context
- 5 chunks ≈ 2500 characters
- Leave room for: system prompt, conversation history, user query
- Total context = prompt + history + RAG chunks + response

### Model Mapping

The `model_map` allows different generation models to use different embedding models:

```yaml
stores:
  - name: mixed
    db_path: agents/rag/mixed.db
    embedding_model: nomic-embed-text
    model_map:
      llama3.1:8b: nomic-embed-text
      granite3.3:2b: mxbai-embed-large  # Different embedding model for granite
```

**When this is useful:**
- You have a generation model that works better with a specific embedding model
- You're experimenting with different embedding models
- You want to optimize for specific use cases

### Embedding Model Consistency

Harvey **strictly enforces** embedding model consistency:

```go
// In rag_support.go
if embedder.Name() != r.embeddingModel {
    return errors.New("embedding model mismatch")
}
```

This prevents:
- Silent retrieval failures from mixed embedding spaces
- Incompatible vector dimensions
- Unpredictable similarity scores

**Implications:**
- You cannot query a store with a different embedding model than it was created with
- You cannot ingest documents using a different embedding model
- To change embedding models, create a new store and re-ingest

### Performance Considerations

#### Query Time

Harvey uses a **linear scan** approach:
- O(n) time complexity where n = number of chunks
- Computes cosine similarity against every chunk
- Sorts and returns top-K results

**Performance Characteristics:**
| Chunks | Query Time (approximate) | Recommended Use Case |
|--------|--------------------------|---------------------|
| 100 | < 10ms | Small projects |
| 1,000 | ~50ms | Medium projects |
| 10,000 | ~500ms | Large projects |
| 100,000 | ~5s | Very large (consider splitting) |

**For large knowledge bases (>10,000 chunks):**
- Consider splitting into multiple topic-focused stores
- Use more powerful hardware
- Wait for future ANN (Approximate Nearest Neighbor) indexing

#### Memory Usage

- Each chunk embedding: ~1-4KB (depending on vector dimension)
- 10,000 chunks ≈ 10-40MB
- All embeddings are loaded into memory for each query
- Only the active store's embeddings are in memory

### Database Schema

Each RAG store uses this SQLite schema:

```sql
CREATE TABLE IF NOT EXISTS chunks (
    id        INTEGER PRIMARY KEY,
    content   TEXT NOT NULL,
    embedding BLOB NOT NULL,
    source    TEXT NOT NULL DEFAULT ''
);
```

**Fields:**
- `id` — Auto-incrementing primary key
- `content` — The text chunk (up to ~500 characters)
- `embedding` — Binary-serialized vector embedding
- `source` — Source file path for this chunk

**Serialization Format:**
```
[int32 length][float64 vector...]
```

Little-endian binary encoding of the vector.

### Custom Embedders

Harvey supports custom embedders via the `Embedder` interface:

```go
type Embedder interface {
    Embed(text string) ([]float64, error)
    Name() string
}
```

**Built-in Embedders:**

1. **OllamaEmbedder** — Uses Ollama's embedding API
2. **EncoderfileEmbedder** — Uses a custom HTTP endpoint

**Configuration:**

```yaml
stores:
  - name: custom
    db_path: agents/rag/custom.db
    embedding_model: my-custom-model
    embedder_kind: encoderfile
    embedder_url: http://localhost:8080/embed
```

## Troubleshooting

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| No results from `/rag query` | Store is empty | Ingest documents first with `/rag ingest` |
| Low similarity scores | Documents don't match query | Rephrase query or ingest more relevant docs |
| "Embedding model mismatch" | Wrong embedding model | Use the correct embedding model for the store |
| RAG not injecting context | RAG is disabled | Run `/rag on` or check configuration |
| Slow queries | Too many chunks | Split into multiple stores or use more powerful hardware |
| Chunks not appearing | File type not supported | Use supported text formats (.md, .txt, .go, etc.) |

### Diagnosing Poor Retrieval

1. **Check what's in your store:**
   ```bash
   harvey> /rag status
   ```

2. **Test retrieval directly:**
   ```bash
   harvey> /rag query "your query here"
   ```

3. **Check similarity scores:**
   - Scores < 0.3 are filtered out
   - Scores 0.3-0.6 are weakly related
   - Scores 0.6-0.8 are similar
   - Scores 0.8-1.0 are very similar

4. **Try different query phrasings:**
   - Use keywords from your documents
   - Try synonyms
   - Be more specific

5. **Re-ingest documents:**
   ```bash
   harvey> /rag ingest README.md
   ```

### Verifying Ingestion

```bash
# Check chunk count
harvey> /rag status
  Chunk count: 42

# Query for a specific document
harvey> /rag query "unique phrase from your document"

# Check database directly
sqlite3 agents/rag/my-store.db "SELECT COUNT(*) FROM chunks"
sqlite3 agents/rag/my-store.db "SELECT source, COUNT(*) FROM chunks GROUP BY source"
```

### Checking Embedding Model

```bash
# List installed embedding models
ollama list

# Check which model a store is using
harvey> /rag status

# Recreate store with different model if needed
harvey> /rag new new-store --embedding-model mxbai-embed-large
harvey> /rag ingest docs/
```

### Database Migration

Harvey automatically migrates the schema on open. The `source` column was added after initial release:

```sql
-- Migration runs automatically
ALTER TABLE chunks ADD COLUMN source TEXT NOT NULL DEFAULT ''
```

If migration fails:
1. Backup your database: `cp agents/rag/my-store.db agents/rag/my-store.db.bak`
2. Harvey will recreate the table on next ingest

## Best Practices

### Organizing Your Knowledge

1. **One store per project/domain:**
   - `harvey-docs` — Harvey-specific documentation
   - `golang` — Go language and standard library
   - `myproject` — Your project's code and docs
   - `research` — Research papers and notes

2. **Keep stores focused:**
   - Small, topical stores retrieve better than large, general stores
   - A 5,000-chunk focused store > 50,000-chunk general store
   - Easier to maintain and update

3. **Use descriptive names:**
   - `golang-2024` instead of `store1`
   - `research-llm-security` instead of `research`
   - `project-x-docs` instead of `projectx`

### Ingesting Documents

1. **Start with essentials:**
   - README.md
   - Documentation files
   - Key source files

2. **Add iteratively:**
   - Ingest a few files, test retrieval
   - Add more files as needed
   - Monitor retrieval quality

3. **Re-ingest after changes:**
   - When documents are updated, re-ingest them
   - Old chunks remain until replaced
   - Consider dropping and recreating stores for major changes

4. **Use supported formats:**
   - Prefer Markdown (.md) for structured content
   - Use plain text (.txt) for web pages (Firefox "Save as Text")
   - Source code files work well for code search

### Querying Effectively

1. **Use specific terms:**
   - ❌ "how does it work" → Too generic
   - ✅ "how does the RAG ingestion pipeline work" → Specific

2. **Use keywords from your documents:**
   - Match the language used in your ingested content
   - Use technical terms and jargon

3. **Ask focused questions:**
   - ❌ "tell me everything about this project" → Too broad
   - ✅ "what are the dependencies for the web server module" → Focused

4. **Use the knowledge base alongside RAG:**
   - Store structured information in KB (projects, observations, concepts)
   - Use KB for facts, RAG for documents
   - They complement each other

### Resource Management

1. **On resource-constrained hardware (Raspberry Pi):**
   - Keep stores small (< 5,000 chunks)
   - Use smaller embedding models (bge-small-en-v1.5)
   - Close stores when not in use
   - Switch between stores as needed

2. **On powerful hardware:**
   - Can handle larger stores (10,000+ chunks)
   - Use higher-quality embedding models (nomic-embed-text, mxbai-embed-large)
   - Keep multiple stores open simultaneously

3. **Storage considerations:**
   - Each chunk ≈ 1-4KB (vector + text + metadata)
   - 10,000 chunks ≈ 10-40MB per store
   - SQLite files grow but compress well

### Integration with Workflow

1. **At project start:**
   - Create a store for the project
   - Ingest essential documentation
   - Verify retrieval with test queries

2. **During development:**
   - Ingest new files as they're created
   - Use RAG for answering questions about the codebase
   - Switch stores when changing projects

3. **At project end:**
   - Archive the store (move .db file)
   - Document what's in each store
   - Consider merging related stores

4. **For research:**
   - Create a dedicated research store
   - Ingest papers, notes, references
   - Use RAG to find relevant information quickly

## Comparison: RAG vs Knowledge Base

Harvey provides two complementary systems for managing information:

| Feature | RAG | Knowledge Base |
|---------|-----|----------------|
| **Purpose** | Retrieve relevant document snippets | Store structured project data |
| **Data Type** | Unstructured text (files) | Structured entities (projects, observations, concepts) |
| **Query Type** | Semantic similarity search | Full-text search (FTS5) + structured queries |
| **Best For** | Finding information in documents | Tracking projects, observations, decisions |
| **Update** | Re-ingest files | CRUD operations on entities |
| **Linking** | Automatic (by content similarity) | Manual (explicit links between entities) |
| **Output** | Raw text chunks | Formatted summaries, Markdown export |

### When to Use Each

**Use RAG when:**
- You have documents you want to search through
- You want semantic search (finding conceptually similar content)
- You want to ground model responses in specific documents
- You're working with large amounts of text

**Use Knowledge Base when:**
- You want to track projects and their status
- You want to record observations, findings, decisions
- You want to categorize things with concepts/tags
- You want structured data that you can query and report on

**Use Both Together:**
```bash
# Store project metadata in KB
harvey> /kb project add "My Project" "A test project"
harvey> /kb obs add 1 finding "Discovered issue with RAG retrieval"

# Store project documents in RAG
harvey> /rag new my-project
harvey> /rag ingest docs/ src/

# Now you can:
# - Query KB for project status and observations
# - Query RAG for document content
# - Get comprehensive project understanding
```

## Command Line Usage

### Start Harvey with RAG Pre-configured

```bash
# Enable RAG with default store
harvey --rag

# Specify session file to continue
harvey --continue previous-session.spmd

# Replay a session with RAG
harvey --replay old-session.spmd --replay-output new-session.spmd
```

### Scripting with RAG

```bash
# Create a store and ingest documents in a script
cat << 'EOF' | harvey --batch
/rag new scripted-store
/rag ingest /path/to/docs/
/rag on
EOF

# Or use harvey in non-interactive mode
harvey --batch -c "/rag new test; /rag ingest README.md; /rag query test"
```

## Reference

### Related Files

| File | Description |
|------|-------------|
| `harvey/rag_support.go` | Core RAG implementation |
| `harvey/config.go` | RAG configuration types |
| `harvey/commands.go` | RAG command handlers |
| `harvey/helptext.go` | RAG help text |
| `agents/harvey.yaml` | RAG store configurations |

### Related Skills

| Skill | Description |
|-------|-------------|
| `fountain-analysis` | Analyze session files, including RAG usage patterns |
| `review-knowledge-base` | Review and analyze knowledge base content |
| `update-knowledge-base` | Update knowledge base with session content |

### External Resources

- [Ollama Embedding Models](https://ollama.ai/library) — Official model library
- [Fountain Format](https://fountain.io) — Session file format specification
- [MTEB Benchmark](https://huggingface.co/papers/2212.04355) — Massive Text Embedding Benchmark
- [SQLite](https://sqlite.org) — Database engine used for RAG stores

## See Also

- [CONFIGURATION.md](CONFIGURATION.md) — Configuration file reference
- [KNOWLEDGE_BASE.md](KNOWLEDGE_BASE.md) — Knowledge base documentation
- [SESSIONS.md](SESSIONS.md) — Session recording and Fountain format
- [ROUTING.md](ROUTING.md) — Remote endpoint routing
- [SKILLS.md](SKILLS.md) — Agent Skills system
- [user_manual.md](user_manual.md) — General Harvey usage
- [getting_started.md](getting_started.md) — Quick start guide

