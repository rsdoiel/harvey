%harvey(7) user manual | version 0.0.3 0969704
% R. S. Doiel
% 2026-05-12

# NAME

RAG — Retrieval-Augmented Generation

# SYNOPSIS

/rag <list|new NAME|switch NAME|drop NAME|setup|ingest PATH|status|query TEXT|on|off>

# DESCRIPTION

RAG lets Harvey find relevant snippets from a local knowledge store and
inject them into the context window before each prompt is sent to the model.
This grounds the model's replies in documents you have ingested, reducing
hallucination and allowing it to answer questions about your own codebase,
notes, and reference material without needing those files to be manually
read into context with /read.

When RAG is on, every user prompt is silently augmented with a short block
of matching text retrieved from the store. Only chunks that score above the
relevance threshold (0.3 cosine similarity) are injected; if nothing scores
high enough, the prompt is sent unmodified.

# NAMED STORES

Harvey supports multiple named RAG stores. Each store is an independent
SQLite database bound to one embedding model — you cannot mix vectors from
different embedding models in the same store, and Harvey enforces this at
every ingest and query operation.

Named stores let you maintain separate, topically focused knowledge bases
and switch between them as your work changes:

  golang        Go standard library docs, idioms, and project code
  deno          Deno/TypeScript docs and project source
  web-frontend  MDN references, CSS specs, web-component guides
  writing       Style guides, project drafts, editorial notes
  research-X    Papers and notes for a specific research topic

Only the active store is open at any time, so inactive stores consume no
memory. The active store can be changed with /rag use at any time.

On storage-constrained hardware (e.g. a Raspberry Pi), keep stores small
and topical: a focused 5 000-chunk store retrieves better than a bloated
50 000-chunk general store, and it fits in RAM.

# EMBEDDING MODEL CHOICE

RAG depends on a separate embedding model — a small neural network that
converts text to vectors. The quality of retrieval depends heavily on the
embedding model used. The models are ranked here from best to least suitable:

  nomic-embed-text        (~274 MB) best general-purpose retrieval
  mxbai-embed-large       (~670 MB) high quality, larger
  bge-small-en-v1.5        (~46 MB) small but retrieval-optimised
  bge-m3                  (~1.2 GB) multilingual
  all-minilm-l6-v2         (~46 MB) avoid — similarity-tuned, not retrieval-tuned

The critical distinction: models like all-MiniLM were trained on
sentence-similarity tasks (NLI, STS), not on document retrieval. On standard
retrieval benchmarks (MTEB), all-MiniLM-L6-v2 scores around 56% while
bge-small-en-v1.5 scores around 62% and nomic-embed-text around 68%. Use a
retrieval-optimised model whenever possible.

The /rag new wizard detects which embedding models are installed and
proposes the best available one, preferring nomic-embed-text > mxbai-embed-large
> bge- > all-minilm. If none are installed, it prints a list of recommended
models you can pull with /ollama pull.

Each store is bound to one embedding model at creation time. If you want to
try a different embedding model for the same topic, create a new store and
re-ingest the documents.

# WORKFLOW — FIRST STORE

~~~
  # Step 1 — choose an embedding model (one-time)
  /ollama pull nomic-embed-text

  # Step 2 — create and name a store
  /rag new golang

  # Step 3 — ingest the documents you want Harvey to retrieve from
  /rag ingest agents/
  /rag ingest HARVEY.md
  /rag ingest docs/

  # Step 4 — verify retrieval quality before trusting answers
  /rag query what license does Harvey use?
  /rag query how does routing work?

  # RAG is now on automatically — ask questions normally
  what license does Harvey use?
~~~

# WORKFLOW — MULTIPLE STORES

~~~
  # Create a writing store alongside the golang store
  /rag new writing

  # Ingest style guides and project drafts
  /rag ingest ~/writing/style-guide.md
  /rag ingest ~/projects/novel/drafts/

  # Switch back to golang when you return to code
  /rag use golang

  # See all registered stores
  /rag list
~~~

# DIAGNOSING POOR RETRIEVAL

Use /rag query to inspect what the store would return for a given question
before sending it to the model. The output shows each chunk with its cosine
similarity score (0.0–1.0) and source file:

~~~
  /rag query what license does Harvey use?

  Top 5 result(s) for "what license does Harvey use?":

    [1] score=0.712  source=/home/user/Laboratory/harvey/LICENSE
        GNU AFFERO GENERAL PUBLIC LICENSE...

    [2] score=0.431  source=/home/user/Laboratory/harvey/README.md
        Harvey is licensed under AGPL-3.0...
~~~

Scores below 0.3 are dropped from the injected context. If the top scores
are all low (< 0.3) for a question you expect the store to answer, consider:

  1. Switching to a better embedding model (see Embedding Model Choice above)
     then creating a new store and re-ingesting.
  2. Ingesting the missing documents with /rag ingest PATH.
  3. Rephrasing the question to be closer to the language used in the docs.

# SUBCOMMANDS

/rag list
  List all registered stores with a * marking the active one.

/rag new NAME
  Interactive wizard to create a named store. Detects installed embedding
  models, proposes the best one, shows the proposed generation-model →
  embedding-model mapping, and asks for confirmation. Creates the database
  at agents/rag/NAME.db and saves the config to agents/harvey.yaml.
  The new store is immediately set as active.

/rag use NAME
  Close the currently open store and activate NAME. The change is persisted
  to agents/harvey.yaml.

/rag drop NAME
  Remove a store from the registry after confirmation. The .db file is NOT
  deleted automatically — the path is printed so you can remove it manually.

/rag setup
  Backward-compatible alias for /rag new using the current active store
  name, or "default" when no store is configured. Use /rag new NAME to
  give the store a meaningful name.

/rag ingest PATH [PATH...]
  Reads each file or directory (recursively), splits text into
  paragraph-sized chunks (~500 characters each), embeds them using the
  active store's embedding model, and stores the vectors in the database.
  Re-ingest any file after it changes to keep the store current.
  Supported text formats: .md, .txt, .go, .ts, and most plain-text files.

/rag query TEXT
  Retrieves the top 5 matching chunks for TEXT from the active store and
  prints each one with its cosine similarity score and source path.

/rag status
  Shows the active store (database path, embedding model, chunk count) and
  a summary list of all registered stores.

/rag on
  Enable automatic context injection for the current session.

/rag off
  Disable automatic context injection for the current session.
  The database and configuration are preserved; /rag on re-enables it.

# CONFIGURATION

RAG configuration is persisted in agents/harvey.yaml. Example with two stores:

~~~yaml
  rag:
    enabled: true
    active: golang
    stores:
      - name: golang
        db_path: agents/rag/golang.db
        embedding_model: nomic-embed-text
        model_map:
          llama3.1:latest: nomic-embed-text
          granite3.3:2b:   nomic-embed-text
      - name: writing
        db_path: agents/rag/writing.db
        embedding_model: nomic-embed-text
~~~

Each store has its own db_path and embedding_model. The model_map lets
different generation models share the same embedding model; entries are
populated automatically by /rag new.

Old single-store configurations (db_path at the top level of rag:) are
automatically migrated to a store named "default" on first load.

