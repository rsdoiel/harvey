**Status (2026-05-02):** Implemented with named-store registry. See ARCHITECTURE.md
for the current design. The planning decisions below were adopted; the main
evolution beyond this document is multi-store support (`RagStoreEntry` registry,
`/rag new NAME`, `/rag switch NAME`, `/rag drop NAME`) so different knowledge
domains (golang, writing, research, etc.) can coexist as separate SQLite files
while only the active one is held open in memory.

***

# 🧠 Harvey RAG Integration Plan (Hybrid Embedding Model Approach)

## 🎯 Goal

Add **Retrieval-Augmented Generation (RAG)** support to Harvey while:

*   Leveraging the existing **knowledge base (KB)** as the source of truth
*   Maintaining **loose coupling** with Ollama
*   Avoiding embedding mismatch issues
*   Keeping infrastructure simple (SQLite, no vector DB initially)

***

# 🧩 Core Concept

### ✅ Separation of concerns

    Knowledge Base (raw data)
            ↓
    Embedding Model (e.g. nomic-embed-text)
            ↓
    RAG Index (SQLite per embedding model)
            ↓
    Generation Model (granite4, llama3, etc.)

***

## ✅ Key Design Decisions

### 1. Use **embedding model–scoped RAG databases**

Instead of per-generation-model:

    ❌ granite4.db
    ❌ llama3.db

Use:

    ✅ rag_nomic_v1.db
    ✅ rag_mxbai_v1.db

***

### 2. Map generation model → embedding model

Example:

```go
type ModelConfig struct {
    GenerationModel string
    EmbeddingModel  string
    RagDBPath       string
}

var ModelRegistry = map[string]ModelConfig{
    "granite4": {
        GenerationModel: "granite4",
        EmbeddingModel:  "nomic-embed-text",
        RagDBPath:       "rag_nomic_v1.db",
    },
    "llama3": {
        GenerationModel: "llama3",
        EmbeddingModel:  "nomic-embed-text",
        RagDBPath:       "rag_nomic_v1.db",
    },
}
```

***

### 3. Explicit ingestion step

    harvey ingest --embedding-model nomic-embed-text

*   Generates embeddings
*   Stores them in SQLite
*   Can be run offline / batch

***

### 4. Enforce embedding consistency

Strict runtime check:

```go
if embedder.Name() != r.embeddingModel {
    return errors.New("embedding model mismatch")
}
```

Prevents:

*   Silent retrieval failures
*   Mixed embedding spaces

***

# ⚙️ Go Module Design (`package harvey`)

## 📁 File Structure

    harvey/
      rag_support.go
      rag_support_test.go

***

# 📦 `rag_support.go` (Design Overview)

## ✅ Responsibilities

*   Manage SQLite-based RAG index
*   Store embeddings as BLOBs
*   Provide ingest + query APIs
*   Compute cosine similarity in Go

***

## ✅ Interfaces

```go
type Embedder interface {
    Embed(text string) ([]float64, error)
    Name() string
}
```

Allows:

*   Ollama embedder
*   Mock embedder (for tests)
*   Future providers

***

## ✅ Core Types

```go
type RagStore struct {
    db             *sql.DB
    embeddingModel string
}

type Chunk struct {
    ID      int64
    Content string
}
```

***

## ✅ SQLite Schema

```sql
CREATE TABLE IF NOT EXISTS chunks (
    id INTEGER PRIMARY KEY,
    content TEXT NOT NULL,
    embedding BLOB NOT NULL
);
```

Future extensions:

```sql
source_id TEXT,
chunk_index INTEGER,
tags TEXT
```

***

## ✅ Initialization

```go
func NewRagStore(dbPath, embeddingModel string) (*RagStore, error)
```

Uses:

```go
import _ "github.com/glebarez/go-sqlite"
```

Driver:

```go
sql.Open("sqlite", dbPath)
```

***

## ✅ Ingest Flow

```go
func (r *RagStore) Ingest(texts []string, embedder Embedder) error
```

Steps:

1.  Validate embedding model
2.  Generate embeddings
3.  Serialize vectors
4.  Store in SQLite (transaction)

***

## ✅ Query Flow

```go
func (r *RagStore) Query(query string, embedder Embedder, topK int) ([]Chunk, error)
```

Steps:

1.  Embed query
2.  Load all stored embeddings
3.  Compute cosine similarity
4.  Sort results
5.  Return top-K chunks

***

## ✅ Cosine Similarity

```go
func cosineSimilarity(a, b []float64) float64
```

*   Pure Go
*   Works for small-medium datasets

***

## ✅ Serialization

Binary format:

    [int32 length][float64...]

Functions:

```go
serialize([]float64) []byte
deserialize([]byte) []float64
```

***

# 🧪 `rag_support_test.go`

## ✅ Uses mock embedder

```go
type mockEmbedder struct {
    name string
}
```

Ensures:

*   Deterministic embeddings
*   No dependency on Ollama

***

## ✅ Test Coverage

### 1. Ingest + query works

*   Inserts multiple documents
*   Queries for relevant content
*   Verifies correct retrieval

***

### 2. Embedding mismatch protection

Ensures:

*   Cannot ingest with wrong model
*   Cannot query with wrong model

***

# 🔄 Runtime Flow in Harvey

## ✅ Query execution

    User selects model (granite4)
        ↓
    Lookup ModelConfig
        ↓
    Get embedding model (nomic-embed-text)
        ↓
    Load corresponding RAG DB
        ↓
    Embed query
        ↓
    Retrieve top-K chunks
        ↓
    Inject into prompt
        ↓
    Call Ollama (granite4)

***

# ⚠️ Known Trade-offs

## ✅ Pros

*   Simple (no vector DB)
*   Deterministic
*   Fully local
*   Easy to debug
*   Loose coupling to Ollama

***

## ⚠️ Cons

*   Linear scan (O(n)) query time
*   No ANN indexing
*   Duplicate embeddings if multiple models used
*   Requires re-ingestion if embedding model changes

***

# 🚀 Future Enhancements

## 🔧 Performance

*   Replace sort with heap (top-K)
*   Add approximate search (later)

## 🧠 Retrieval quality

*   Add chunking (critical)
*   Add metadata filtering
*   Add reranking layer

## ⚙️ Storage

*   Add vector dimension column
*   Add embedding versioning
*   Migrate to pgvector or Qdrant if needed

***

# ❓ Open Questions

These will influence your next steps:

1.  **How large is your knowledge base?**
    *   <10k chunks → current design is perfect
    *   > 100k → may need indexing soon

2.  **Will users switch models frequently?**
    *   If yes → shared embedding index is important

3.  **Do you already have document chunking?**
    *   If not, this is the next critical feature

4.  **Should ingestion be automatic or manual?**
    *   CLI-driven vs background processing

5.  **Do you want offline-only operation?**
    *   Affects embedding strategy + caching

***

# ✅ TL;DR

*   Use **embedding-model–scoped SQLite RAG indexes**
*   Map **generation models → embedding model**
*   Implement **ingest + query pipeline**
*   Enforce **embedding consistency strictly**
*   Start simple; scale later if needed

***

If you want, I can next:

*   Add a **real Ollama embedder implementation**
*   Design a **chunking pipeline for your KB**
*   Or integrate this directly into Harvey’s request lifecycle end-to-end


